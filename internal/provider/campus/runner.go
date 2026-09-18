package campus

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soundadam/soundprobe/internal/helper"
	"github.com/soundadam/soundprobe/internal/model"
	"github.com/soundadam/soundprobe/internal/provider"
)

const (
	HelperName          = "librespeed-cli"
	HelperVersion       = "v1.0.13-campus.1"
	IPv4ServerID        = "1"
	IPv6ServerID        = "2"
	ConcurrentRequests  = 3
	MeasurementDuration = 10
)

const pinnedServerListJSON = `[
  {
    "id": 1,
    "name": "NJU Speed Test v4",
    "server": "http://speed.nju.edu.cn",
    "dlURL": "/backend/garbage.php",
    "ulURL": "/backend/empty.php",
    "pingURL": "/backend/empty.php",
    "getIpURL": "/backend/getIP.php"
  },
  {
    "id": 2,
    "name": "NJU Speed Test v6",
    "server": "http://speed6.nju.edu.cn",
    "dlURL": "/backend/garbage.php",
    "ulURL": "/backend/empty.php",
    "pingURL": "/backend/empty.php",
    "getIpURL": "/backend/getIP.php"
  }
]`

var versionPattern = regexp.MustCompile(`(?m)^librespeed-cli\s+(v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?)\s`)

type HelperResolver interface {
	Resolve(string) (helper.Resolved, error)
}

type Config struct {
	Provider   model.Provider
	Label      string
	Family     string
	ServerName string
	ServerURL  string
}

type Runner struct {
	Resolver       HelperResolver
	Config         Config
	Timeout        time.Duration
	VersionTimeout time.Duration
	Now            func() time.Time

	prepareMu sync.Mutex
	prepared  *preparedHelper
}

type preparedHelper struct {
	resolved helper.Resolved
	version  string
}

type selectedTarget struct {
	provider     model.Provider
	label        string
	family       string
	serverID     string
	expectedHost string
	serverList   []byte
}

func New(resolver HelperResolver) *Runner {
	return &Runner{Resolver: resolver}
}

func NewTarget(resolver HelperResolver, config Config) *Runner {
	return &Runner{Resolver: resolver, Config: config}
}

func (runner *Runner) Preflight(ctx context.Context, request provider.Request) error {
	runner.setDefaults()
	if _, err := runner.selectedTarget(request); err != nil {
		return err
	}
	_, _, err := runner.prepareHelper(ctx)
	return err
}

func (runner *Runner) Measure(ctx context.Context, request provider.Request) (model.Measurement, error) {
	runner.setDefaults()
	target, err := runner.selectedTarget(request)
	if err != nil {
		return model.Measurement{}, err
	}
	resolved, helperVersion, err := runner.prepareHelper(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return cancelledMeasurement(target.provider, target.label, target.family, "", 0), nil
		}
		return model.Measurement{}, err
	}
	request.Report(provider.ProgressEvent{
		Provider: target.provider,
		Phase:    provider.ProgressMeasuring,
		Server:   target.expectedHost,
	})

	measurementCtx, cancel := context.WithTimeout(ctx, runner.Timeout)
	defer cancel()
	startedAt := runner.Now()
	if err := validateServerList(target.serverList, target.serverID, target.expectedHost); err != nil {
		return model.Measurement{}, fmt.Errorf("validate pinned LibreSpeed target %s: %w", target.label, err)
	}
	args := []string{
		"--local-json", "-",
		"--server", target.serverID,
		"--duration", fmt.Sprintf("%d", MeasurementDuration),
		"--concurrent", fmt.Sprintf("%d", ConcurrentRequests),
		"--no-icmp",
		"--telemetry-level", "disabled",
		"--json",
		"--progress-json",
		"--" + target.family,
	}
	command := exec.CommandContext(measurementCtx, resolved.Path, args...)
	stdout := provider.NewCappedBuffer(128 * 1024)
	stderr := provider.NewCappedBuffer(16 * 1024)
	command.Stdin = bytes.NewReader(target.serverList)
	command.Stdout = stdout
	progressOutput, pipeErr := command.StderrPipe()
	if pipeErr != nil {
		return model.Measurement{}, fmt.Errorf("open LibreSpeed progress output: %w", pipeErr)
	}
	err = command.Start()
	var progressErr error
	if err == nil {
		progressErr = scanProgressOutput(progressOutput, stderr, target.provider, target.expectedHost, request)
		if progressErr != nil {
			_ = command.Process.Kill()
		}
		err = command.Wait()
	}
	durationMS := provider.ElapsedMS(startedAt, runner.Now())
	if progressErr != nil {
		return model.Measurement{}, fmt.Errorf("parse LibreSpeed progress output: %w", progressErr)
	}

	if err != nil {
		if errors.Is(measurementCtx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return cancelledMeasurement(target.provider, target.label, target.family, helperVersion, durationMS), nil
		}
		if errors.Is(measurementCtx.Err(), context.DeadlineExceeded) {
			return failedMeasurement(target.provider, target.label, target.family, helperVersion, durationMS, model.FailureStageTimeout, "timeout", target.label+" measurement timed out"), nil
		}
		stage, code := classifyFailure(stderr.String())
		message := provider.SanitizeMessage(stderr.String(), provider.MessageLimit)
		if message == "" {
			message = "LibreSpeed helper exited before producing a result"
		}
		if stage == model.FailureStageHelper {
			return model.Measurement{}, fmt.Errorf("LibreSpeed helper failed: %s", message)
		}
		return failedMeasurement(target.provider, target.label, target.family, helperVersion, durationMS, stage, code, message), nil
	}

	measurement, err := parseResult(stdout.Bytes(), target.provider, target.family, helperVersion, durationMS)
	if errors.Is(err, errNoResult) {
		return failedMeasurement(target.provider, target.label, target.family, helperVersion, durationMS, model.FailureStageConnect, "server_unreachable", target.label+" server did not produce a measurement"), nil
	}
	if err != nil {
		return model.Measurement{}, fmt.Errorf("parse LibreSpeed helper output: %w", err)
	}
	if measurement.ServerFQDN == nil || !strings.EqualFold(*measurement.ServerFQDN, target.expectedHost) {
		return model.Measurement{}, fmt.Errorf("LibreSpeed helper returned an unexpected server for %s", target.label)
	}
	return measurement, nil
}

func scanProgressOutput(input io.Reader, errorsOutput io.Writer, measurementProvider model.Provider, server string, request provider.Request) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 16*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		recognized, err := consumeProgressLine(line, measurementProvider, server, request)
		if err != nil {
			return err
		}
		if recognized {
			continue
		}
		_, _ = errorsOutput.Write(line)
		_, _ = errorsOutput.Write([]byte{'\n'})
	}
	return scanner.Err()
}

func (runner *Runner) selectedTarget(request provider.Request) (selectedTarget, error) {
	if runner.Config.Provider != "" {
		config := runner.Config
		if !model.ProviderValid(config.Provider) || model.ProviderMethod(config.Provider) != model.MethodLibreSpeedThreeStream {
			return selectedTarget{}, fmt.Errorf("invalid LibreSpeed provider %q", config.Provider)
		}
		if config.Family != "ipv4" && config.Family != "ipv6" {
			return selectedTarget{}, fmt.Errorf("invalid LibreSpeed family %q", config.Family)
		}
		serverURL, err := url.Parse(config.ServerURL)
		if err != nil || serverURL.Hostname() == "" {
			return selectedTarget{}, fmt.Errorf("invalid LibreSpeed server URL %q", config.ServerURL)
		}
		label := config.Label
		if label == "" {
			label = string(config.Provider)
		}
		serverName := config.ServerName
		if serverName == "" {
			serverName = label
		}
		serverList, err := json.Marshal([]map[string]any{{
			"id": 1, "name": serverName, "server": strings.TrimRight(config.ServerURL, "/"),
			"dlURL": "/backend/garbage.php", "ulURL": "/backend/empty.php",
			"pingURL": "/backend/empty.php", "getIpURL": "/backend/getIP.php",
		}})
		if err != nil {
			return selectedTarget{}, fmt.Errorf("encode pinned LibreSpeed target: %w", err)
		}
		return selectedTarget{
			provider: config.Provider, label: label, family: config.Family,
			serverID: "1", expectedHost: serverURL.Hostname(), serverList: serverList,
		}, nil
	}

	family, serverID, expectedHost, err := selectedServer(request.IPFamily)
	if err != nil {
		return selectedTarget{}, err
	}
	return selectedTarget{
		provider: model.ProviderCampus, label: "NJU Campus · " + strings.ToUpper(family),
		family: family, serverID: serverID, expectedHost: expectedHost,
		serverList: []byte(pinnedServerListJSON),
	}, nil
}

func (runner *Runner) prepareHelper(ctx context.Context) (helper.Resolved, string, error) {
	runner.prepareMu.Lock()
	defer runner.prepareMu.Unlock()
	if runner.prepared != nil {
		return runner.prepared.resolved, runner.prepared.version, nil
	}
	if runner.Resolver == nil {
		return helper.Resolved{}, "", fmt.Errorf("%w: LibreSpeed helper resolver is not configured", provider.ErrUnavailable)
	}
	resolved, err := runner.Resolver.Resolve(HelperName)
	if err != nil {
		return helper.Resolved{}, "", fmt.Errorf("%w: resolve LibreSpeed helper: %v", provider.ErrUnavailable, err)
	}
	version, err := runner.probeVersion(ctx, resolved.Path)
	if err != nil {
		return helper.Resolved{}, "", err
	}
	runner.prepared = &preparedHelper{resolved: resolved, version: version}
	return resolved, version, nil
}

func (runner *Runner) setDefaults() {
	if runner.Timeout <= 0 {
		runner.Timeout = 60 * time.Second
	}
	if runner.VersionTimeout <= 0 {
		runner.VersionTimeout = 5 * time.Second
	}
	if runner.Now == nil {
		runner.Now = time.Now
	}
}

type serverListEntry struct {
	ID     int    `json:"id"`
	Server string `json:"server"`
}

func validateServerList(data []byte, selectedID, expectedHost string) error {
	var entries []serverListEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	wantedID, err := strconv.Atoi(selectedID)
	if err != nil {
		return fmt.Errorf("invalid selected server ID %q", selectedID)
	}
	found := false
	for _, entry := range entries {
		if entry.ID != wantedID {
			continue
		}
		if found {
			return fmt.Errorf("server ID %d is duplicated", wantedID)
		}
		serverURL, err := url.Parse(entry.Server)
		if err != nil || serverURL.Hostname() == "" {
			return fmt.Errorf("server ID %d has invalid URL %q", wantedID, entry.Server)
		}
		if serverURL.Scheme != "http" && serverURL.Scheme != "https" {
			return fmt.Errorf("server ID %d has unsupported URL scheme %q", wantedID, serverURL.Scheme)
		}
		if serverURL.User != nil {
			return fmt.Errorf("server ID %d URL must not contain user information", wantedID)
		}
		if !strings.EqualFold(serverURL.Hostname(), expectedHost) {
			return fmt.Errorf("server ID %d resolves to unexpected host %q", wantedID, serverURL.Hostname())
		}
		found = true
	}
	if !found {
		return fmt.Errorf("server ID %d is missing", wantedID)
	}
	return nil
}

func (runner *Runner) probeVersion(ctx context.Context, path string) (string, error) {
	versionCtx, cancel := context.WithTimeout(ctx, runner.VersionTimeout)
	defer cancel()
	command := exec.CommandContext(versionCtx, path, "--version")
	stdout := provider.NewCappedBuffer(16 * 1024)
	stderr := provider.NewCappedBuffer(4 * 1024)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if errors.Is(versionCtx.Err(), context.Canceled) {
			return "", context.Canceled
		}
		if errors.Is(versionCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%w: LibreSpeed version probe timed out", provider.ErrUnavailable)
		}
		message := provider.SanitizeMessage(stderr.String(), provider.MessageLimit)
		if message == "" {
			message = "helper could not report its version"
		}
		return "", fmt.Errorf("%w: LibreSpeed version probe failed: %s", provider.ErrUnavailable, message)
	}
	matches := versionPattern.FindSubmatch(stdout.Bytes())
	if len(matches) != 2 {
		return "", fmt.Errorf("%w: LibreSpeed version output is unrecognized", provider.ErrUnavailable)
	}
	version := string(matches[1])
	if version[0] != 'v' {
		version = "v" + version
	}
	if version != HelperVersion {
		return "", fmt.Errorf("%w: LibreSpeed helper version %s does not match required %s", provider.ErrUnavailable, version, HelperVersion)
	}
	return version, nil
}

func selectedServer(requestedFamily string) (family, serverID, expectedHost string, err error) {
	switch requestedFamily {
	case "", "ipv4":
		return "ipv4", IPv4ServerID, "speed.nju.edu.cn", nil
	case "ipv6":
		return "ipv6", IPv6ServerID, "speed6.nju.edu.cn", nil
	default:
		return "", "", "", fmt.Errorf("unsupported campus IP family %q", requestedFamily)
	}
}

func failedMeasurement(measurementProvider model.Provider, label, family, version string, durationMS int64, stage model.FailureStage, code, message string) model.Measurement {
	zero := 0.0
	return model.Measurement{
		Provider:      measurementProvider,
		Method:        model.MethodLibreSpeedThreeStream,
		Status:        model.ProviderStatusFailed,
		IPFamily:      model.Pointer(family),
		ServerName:    model.Pointer(label),
		DownloadMbps:  model.Pointer(zero),
		UploadMbps:    model.Pointer(zero),
		DurationMS:    model.Pointer(durationMS),
		Concurrency:   model.Pointer(ConcurrentRequests),
		HelperVersion: optionalVersion(version),
		Failure:       &model.Failure{Stage: stage, Code: code, Message: message},
	}
}

func cancelledMeasurement(measurementProvider model.Provider, label, family, version string, durationMS int64) model.Measurement {
	return model.Measurement{
		Provider:      measurementProvider,
		Method:        model.MethodLibreSpeedThreeStream,
		Status:        model.ProviderStatusCancelled,
		IPFamily:      model.Pointer(family),
		ServerName:    model.Pointer(label),
		DurationMS:    model.Pointer(durationMS),
		Concurrency:   model.Pointer(ConcurrentRequests),
		HelperVersion: optionalVersion(version),
		Failure:       &model.Failure{Stage: model.FailureStageCancelled, Code: "cancelled", Message: label + " measurement was cancelled"},
	}
}

func optionalVersion(version string) *string {
	if version == "" {
		return nil
	}
	return model.Pointer(version)
}

func classifyFailure(message string) (model.FailureStage, string) {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "no such host"), strings.Contains(lower, "temporary failure in name resolution"), strings.Contains(lower, "name or service not known"):
		return model.FailureStageDNS, "dns_failure"
	case strings.Contains(lower, "failed to get download speed"):
		return model.FailureStageDownload, "download_failure"
	case strings.Contains(lower, "failed to get upload speed"):
		return model.FailureStageUpload, "upload_failure"
	case strings.Contains(lower, "failed to get ping and jitter"),
		strings.Contains(lower, "failed to get ip info"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "connection reset by peer"),
		strings.Contains(lower, "connection timed out"),
		strings.Contains(lower, "i/o timeout"),
		strings.Contains(lower, "tls handshake timeout"),
		strings.Contains(lower, "network is unreachable"),
		strings.Contains(lower, "no route to host"),
		strings.Contains(lower, "host is down"),
		strings.Contains(lower, "not responding"):
		return model.FailureStageConnect, "connect_failure"
	default:
		return model.FailureStageHelper, "helper_failure"
	}
}

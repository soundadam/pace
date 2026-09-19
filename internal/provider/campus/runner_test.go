package campus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/soundadam/soundprobe/internal/helper"
	"github.com/soundadam/soundprobe/internal/model"
	"github.com/soundadam/soundprobe/internal/provider"
)

func TestRunnerMeasuresIPv4WithExactArguments(t *testing.T) {
	runner, argsPath := newFakeRunner(t, "testdata/librespeed-success-ipv4.json", HelperVersion, 0, "")
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Status != model.ProviderStatusSuccess {
		t.Fatalf("status = %q", measurement.Status)
	}
	if measurement.IPFamily == nil || *measurement.IPFamily != "ipv4" {
		t.Fatalf("family = %v", measurement.IPFamily)
	}
	gotArgs := readArgs(t, argsPath)
	wantArgs := []string{
		"--local-json", "-",
		"--server", "1",
		"--duration", "10",
		"--concurrent", "3",
		"--no-icmp",
		"--telemetry-level", "disabled",
		"--json",
		"--progress-json",
		"--ipv4",
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	assertServerList(t, argsPath, njuCampusConfig("ipv4"))
}

func TestRunnerMeasuresIPv6WithLocalServerList(t *testing.T) {
	runner, argsPath := newFakeRunner(t, "testdata/librespeed-success-ipv6.json", HelperVersion, 0, "")
	runner.Config = njuCampusConfig("ipv6")
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Provider != model.ProviderNJUCampusIPv6 {
		t.Fatalf("provider = %q", measurement.Provider)
	}
	if measurement.ServerFQDN == nil || *measurement.ServerFQDN != "speed6.nju.edu.cn" {
		t.Fatalf("server = %v", measurement.ServerFQDN)
	}
	gotArgs := readArgs(t, argsPath)
	if !containsPair(gotArgs, "--local-json", "-") || !containsPair(gotArgs, "--server", "1") || !contains(gotArgs, "--ipv6") || contains(gotArgs, "--ipv4") || contains(gotArgs, "--server-json") {
		t.Fatalf("IPv6 arguments = %#v", gotArgs)
	}
	assertServerList(t, argsPath, njuCampusConfig("ipv6"))
}

func TestRunnerRejectsZeroConfig(t *testing.T) {
	runner, argsPath := newFakeRunner(t, "testdata/librespeed-success-ipv4.json", HelperVersion, 0, "")
	runner.Config = Config{}

	preflightErr := runner.Preflight(context.Background(), provider.Request{Command: model.CommandCampus})
	if preflightErr == nil || !strings.Contains(preflightErr.Error(), "no station configured") {
		t.Fatalf("preflight error = %v", preflightErr)
	}

	_, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err == nil || !strings.Contains(err.Error(), "no station configured") {
		t.Fatalf("measure error = %v", err)
	}
	if _, statErr := os.Stat(argsPath); !os.IsNotExist(statErr) {
		t.Fatalf("helper was invoked for a zero Config: %v", statErr)
	}
}

func TestTargetRunnerUsesPinnedExternalStationIdentity(t *testing.T) {
	output := `[{"server":{"name":"QLU","url":"https://speed.qlu.edu.cn"},"client":{"ip":"203.0.113.42"},"bytes_sent":1000,"bytes_received":2000,"ping":10,"jitter":1,"download":20,"upload":10}]`
	runner, argsPath := newFakeRunner(t, "", HelperVersion, 0, output)
	runner.Config = Config{
		Provider:   model.ProviderQLUIPv4,
		Label:      "QLU · IPv4",
		Family:     "ipv4",
		ServerName: "QLU",
		ServerURL:  "https://speed.qlu.edu.cn",
	}
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandDomestic})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Provider != model.ProviderQLUIPv4 || measurement.ServerFQDN == nil || *measurement.ServerFQDN != "speed.qlu.edu.cn" {
		t.Fatalf("measurement = %#v", measurement)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(argsPath), "stdin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"server":"https://speed.qlu.edu.cn"`) {
		t.Fatalf("server list = %s", data)
	}
	if !contains(readArgs(t, argsPath), "--ipv4") {
		t.Fatalf("args = %#v", readArgs(t, argsPath))
	}
}

func TestRunnerReportsProgressJSON(t *testing.T) {
	runner, _ := newFakeRunner(t, "testdata/librespeed-success-ipv4.json", HelperVersion, 0, "")
	t.Setenv("SOUNDPROBE_FAKE_PROGRESS", strings.Join([]string{
		`{"type":"progress","test":"download","elapsed_ms":1000,"bytes":6250000,"mbps":50}`,
		`{"type":"progress","test":"upload","elapsed_ms":1000,"bytes":625000,"mbps":5}`,
	}, "\n"))
	var events []provider.ProgressEvent
	_, err := runner.Measure(context.Background(), provider.Request{
		Command: model.CommandCampus,
		Progress: func(event provider.ProgressEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var download, upload *provider.ProgressEvent
	for index := range events {
		event := &events[index]
		if event.LiveMbps == nil {
			continue
		}
		switch event.Test {
		case "download":
			download = event
		case "upload":
			upload = event
		}
	}
	if download == nil || download.Phase != provider.ProgressDownloading || *download.LiveMbps != 50 {
		t.Fatalf("download progress = %#v", download)
	}
	if upload == nil || upload.Phase != provider.ProgressUploading || *upload.LiveMbps != 5 {
		t.Fatalf("upload progress = %#v", upload)
	}
}

func TestRunnerRejectsMalformedProgressJSON(t *testing.T) {
	runner, _ := newFakeRunner(t, "testdata/librespeed-success-ipv4.json", HelperVersion, 0, "")
	t.Setenv("SOUNDPROBE_FAKE_PROGRESS", `{"type":"progress","test":"download","elapsed_ms":1000,"bytes":1,"mbps":1,"extra":true}`)
	_, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err == nil || !strings.Contains(err.Error(), "parse LibreSpeed progress output") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerRejectsWrongHelperVersion(t *testing.T) {
	runner, _ := newFakeRunner(t, "testdata/librespeed-success-ipv4.json", "v1.0.13", 0, "")
	_, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if !errors.Is(err, provider.ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestRunnerTimesOutVersionProbe(t *testing.T) {
	runner, _ := newFakeRunner(t, "testdata/librespeed-success-ipv4.json", HelperVersion, 0, "")
	t.Setenv("SOUNDPROBE_FAKE_VERSION_SLEEP", "1")
	runner.VersionTimeout = 25 * time.Millisecond
	_, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if !errors.Is(err, provider.ErrUnavailable) || !strings.Contains(err.Error(), "version probe timed out") {
		t.Fatalf("error = %v, want timed-out ErrUnavailable", err)
	}
}

func TestRunnerMapsEmptyResultToConnectFailure(t *testing.T) {
	runner, _ := newFakeRunner(t, "", HelperVersion, 0, "[]")
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageConnect || measurement.Failure.Code != "server_unreachable" {
		t.Fatalf("failure = %#v", measurement.Failure)
	}
}

func TestRunnerRejectsMalformedHelperOutput(t *testing.T) {
	runner, _ := newFakeRunner(t, "", HelperVersion, 0, "not-json")
	_, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err == nil || !strings.Contains(err.Error(), "parse LibreSpeed helper output") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerClassifiesDownloadFailure(t *testing.T) {
	runner, _ := newFakeRunner(t, "", HelperVersion, 1, "Failed to get download speed: connection reset by peer")
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageDownload || measurement.Failure.Code != "download_failure" {
		t.Fatalf("failure = %#v", measurement.Failure)
	}
	if measurement.DownloadMbps == nil || *measurement.DownloadMbps != 0 || measurement.UploadMbps == nil || *measurement.UploadMbps != 0 {
		t.Fatalf("failed speeds = %v/%v", measurement.DownloadMbps, measurement.UploadMbps)
	}
}

func TestRunnerClassifiesConnectionReset(t *testing.T) {
	message := `Get "http://speed.nju.edu.cn/backend/empty.php": read tcp4 198.51.100.10:50681->203.0.113.20:80: read: connection reset by peer`
	runner, _ := newFakeRunner(t, "", HelperVersion, 1, message)
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageConnect || measurement.Failure.Code != "connect_failure" {
		t.Fatalf("failure = %#v", measurement.Failure)
	}
	if measurement.DownloadMbps == nil || *measurement.DownloadMbps != 0 || measurement.UploadMbps == nil || *measurement.UploadMbps != 0 {
		t.Fatalf("failed speeds = %v/%v", measurement.DownloadMbps, measurement.UploadMbps)
	}
}

func TestValidateServerListRejectsUnexpectedSelectedServer(t *testing.T) {
	err := validateServerList([]byte(`[{"id":1,"server":"http://speed.nju.edu.cn"}]`), "1", "speed6.nju.edu.cn")
	if err == nil || !strings.Contains(err.Error(), "unexpected host") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateServerListRejectsUnsafeScheme(t *testing.T) {
	err := validateServerList([]byte(`[{"id":1,"server":"ftp://speed.nju.edu.cn"}]`), "1", "speed.nju.edu.cn")
	if err == nil || !strings.Contains(err.Error(), "unsupported URL scheme") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerTimesOutMeasurement(t *testing.T) {
	runner, _ := newFakeRunner(t, "", HelperVersion, 0, "")
	t.Setenv("SOUNDPROBE_FAKE_SLEEP", "1")
	runner.Timeout = 25 * time.Millisecond
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageTimeout {
		t.Fatalf("failure = %#v", measurement.Failure)
	}
}

func TestRunnerPreservesCancellation(t *testing.T) {
	runner, _ := newFakeRunner(t, "", HelperVersion, 0, "")
	t.Setenv("SOUNDPROBE_FAKE_SLEEP", "5")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel()
	}()
	measurement, err := runner.Measure(ctx, provider.Request{Command: model.CommandCampus})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Status != model.ProviderStatusCancelled || measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageCancelled {
		t.Fatalf("measurement = %#v", measurement)
	}
}

func newFakeRunner(t *testing.T, fixture, version string, exitCode int, literalOutput string) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	executable := filepath.Join(root, "prefix", "bin", "soundprobe")
	helperPath := filepath.Join(root, "prefix", "libexec", "soundprobe", HelperName)
	argsPath := filepath.Join(root, "args.txt")
	stdinPath := filepath.Join(root, "stdin.json")
	writeTestExecutable(t, executable, "#!/bin/sh\nexit 0\n")

	fixturePath := ""
	if fixture != "" {
		absolute, err := filepath.Abs(fixture)
		if err != nil {
			t.Fatal(err)
		}
		fixturePath = absolute
	}
	script := `#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  if [ -n "${SOUNDPROBE_FAKE_VERSION_SLEEP:-}" ]; then
    exec sleep "$SOUNDPROBE_FAKE_VERSION_SLEEP"
  fi
  printf 'librespeed-cli %s (built on test)\n'
  exit 0
fi
printf '%%s\n' "$@" > "$SOUNDPROBE_FAKE_ARGS"
cat > "$SOUNDPROBE_FAKE_STDIN"
if [ -n "${SOUNDPROBE_FAKE_SLEEP:-}" ]; then
  exec sleep "$SOUNDPROBE_FAKE_SLEEP"
fi
if [ -n "${SOUNDPROBE_FAKE_PROGRESS:-}" ]; then
  printf '%%s\n' "$SOUNDPROBE_FAKE_PROGRESS" >&2
fi
if [ -n "%s" ]; then
  cat "%s"
else
  printf '%%s' '%s'
fi
if [ %d -ne 0 ]; then
  printf '%%s\n' '%s' >&2
fi
exit %d
`
	script = fmt.Sprintf(script, version, fixturePath, fixturePath, literalOutput, exitCode, literalOutput, exitCode)
	writeTestExecutable(t, helperPath, script)
	t.Setenv("SOUNDPROBE_FAKE_ARGS", argsPath)
	t.Setenv("SOUNDPROBE_FAKE_STDIN", stdinPath)

	clock := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	return &Runner{
		Config: njuCampusConfig("ipv4"),
		Resolver: helper.Resolver{
			ExecutablePath:   executable,
			WorkingDirectory: root,
			LookupPath: func(string) (string, error) {
				return "", errors.New("not found")
			},
		},
		VersionTimeout: 30 * time.Second,
		Now: func() time.Time {
			clock = clock.Add(250 * time.Millisecond)
			return clock
		},
	}, argsPath
}

// njuCampusConfig mirrors the NJU campus station specs that the registry in
// internal/target feeds to campus.NewTarget.
func njuCampusConfig(family string) Config {
	if family == "ipv6" {
		return Config{
			Provider:   model.ProviderNJUCampusIPv6,
			Label:      "NJU Campus · IPv6",
			Family:     "ipv6",
			ServerName: "NJU Campus IPv6",
			ServerURL:  "http://speed6.nju.edu.cn",
		}
	}
	return Config{
		Provider:   model.ProviderNJUCampusIPv4,
		Label:      "NJU Campus · IPv4",
		Family:     "ipv4",
		ServerName: "NJU Campus IPv4",
		ServerURL:  "http://speed.nju.edu.cn",
	}
}

// assertServerList pins the single-entry LibreSpeed server list that the
// configured station is rendered into and piped to the helper on stdin.
func assertServerList(t *testing.T, argsPath string, config Config) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(argsPath), "stdin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("stdin JSON: %v", err)
	}
	want := []any{map[string]any{
		"id":       float64(1),
		"name":     config.ServerName,
		"server":   config.ServerURL,
		"dlURL":    "/backend/garbage.php",
		"ulURL":    "/backend/empty.php",
		"pingURL":  "/backend/empty.php",
		"getIpURL": "/backend/getIP.php",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stdin = %#v, want %#v", got, want)
	}
}

func writeTestExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func readArgs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsPair(values []string, first, second string) bool {
	for index := 0; index+1 < len(values); index++ {
		if values[index] == first && values[index+1] == second {
			return true
		}
	}
	return false
}

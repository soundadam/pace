package provider

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/soundadam/pace/internal/id"
	"github.com/soundadam/pace/internal/model"
)

var ErrUnavailable = errors.New("measurement provider unavailable")

type ProgressPhase string

const (
	ProgressWaiting     ProgressPhase = "waiting"
	ProgressStarting    ProgressPhase = "starting"
	ProgressConnecting  ProgressPhase = "connecting"
	ProgressMeasuring   ProgressPhase = "measuring"
	ProgressDownloading ProgressPhase = "downloading"
	ProgressUploading   ProgressPhase = "uploading"
	ProgressComplete    ProgressPhase = "complete"
	ProgressFailed      ProgressPhase = "failed"
	ProgressCancelled   ProgressPhase = "cancelled"
)

type ProgressEvent struct {
	Provider     model.Provider
	Phase        ProgressPhase
	Test         string
	Server       string
	LiveMbps     *float64
	DownloadMbps *float64
	UploadMbps   *float64
	Message      string
	Network      *model.NetworkContext
}

type ProgressSink func(ProgressEvent)

type Request struct {
	Command  model.Command
	Targets  []model.Provider
	IPFamily string
	Network  *model.NetworkContext
	Label    *string
	Note     *string
	Progress ProgressSink
	prepared bool
}

func (request Request) Report(event ProgressEvent) {
	if request.Progress != nil {
		request.Progress(event)
	}
}

type Runner interface {
	Run(context.Context, Request) (model.RunSummary, error)
}

type PreflightRunner interface {
	Preflight(context.Context, Request) error
}

// RequestPreparer performs provider preflight and removes optional providers
// that are not available on the current host.  The CLI uses this hook before
// opening the progress renderer so an unavailable Apple/Ookla helper cannot
// abort an otherwise usable campus + M-Lab run.
type RequestPreparer interface {
	Prepare(context.Context, Request) (Request, error)
}

type MeasurementProvider interface {
	Measure(context.Context, Request) (model.Measurement, error)
}

type ProviderPreflight interface {
	Preflight(context.Context, Request) error
}

type SummaryRunner struct {
	ToolVersion string
	Campus      MeasurementProvider
	MLab        MeasurementProvider
	Apple       MeasurementProvider
	Ookla       MeasurementProvider
	Providers   map[model.Provider]MeasurementProvider
	Now         func() time.Time
	NewRunID    func() (string, error)
	Snapshot    func() model.NetworkContext
}

func (runner SummaryRunner) Preflight(ctx context.Context, request Request) error {
	_, err := runner.Prepare(ctx, request)
	return err
}

// Prepare validates the selected providers before a run starts.  Apple
// networkQuality and the Ookla CLI are optional cross-platform integrations:
// for the combined run command an unavailable optional helper is removed from
// the request, while an explicit `apple`/`ookla` command still fails closed.
// This keeps the base education-network product usable on Linux and Windows
// and with a conflicting Python `speedtest-cli` installed on macOS.
func (runner SummaryRunner) Prepare(ctx context.Context, request Request) (Request, error) {
	runner.setDefaults()
	entries, err := runner.providersFor(request)
	if err != nil && request.Command == model.CommandRun && len(request.Targets) > 0 {
		// Resolve explicit targets one at a time so a missing optional helper can
		// be removed without masking a missing required provider.
		filtered := make([]model.Provider, 0, len(request.Targets))
		for _, kind := range request.Targets {
			candidate := request
			candidate.Targets = []model.Provider{kind}
			candidateEntries, candidateErr := runner.providersFor(candidate)
			if candidateErr != nil {
				if isOptionalUnavailable(kind, candidateErr) {
					continue
				}
				return Request{}, candidateErr
			}
			if len(candidateEntries) == 1 {
				filtered = append(filtered, kind)
			}
		}
		if len(filtered) == 0 {
			return Request{}, err
		}
		request.Targets = filtered
		entries, err = runner.providersFor(request)
	}
	if err != nil {
		return Request{}, err
	}

	kept := make([]providerEntry, 0, len(entries))
	for _, entry := range entries {
		if preflight, ok := entry.provider.(ProviderPreflight); ok {
			providerRequest := request
			providerRequest.Targets = []model.Provider{entry.kind}
			if err := preflight.Preflight(ctx, providerRequest); err != nil {
				if isOptionalUnavailableForCommand(request.Command, entry.kind, err) {
					continue
				}
				return Request{}, err
			}
		}
		kept = append(kept, entry)
	}
	if len(kept) == 0 {
		return Request{}, fmt.Errorf("%w: no selected provider is available", ErrUnavailable)
	}

	providers := make([]model.Provider, len(kept))
	for index, entry := range kept {
		providers[index] = entry.kind
	}
	request.Targets = providers
	request.prepared = true
	return request, nil
}

func isOptionalProvider(kind model.Provider) bool {
	return kind == model.ProviderApple || kind == model.ProviderOokla
}

func isOptionalUnavailable(kind model.Provider, err error) bool {
	return isOptionalProvider(kind) && errors.Is(err, ErrUnavailable)
}

func isOptionalUnavailableForCommand(command model.Command, kind model.Provider, err error) bool {
	return command == model.CommandRun && isOptionalUnavailable(kind, err)
}

func (runner SummaryRunner) Run(ctx context.Context, request Request) (model.RunSummary, error) {
	runner.setDefaults()
	if !request.prepared {
		prepared, err := runner.Prepare(ctx, request)
		if err != nil {
			return model.RunSummary{}, err
		}
		request = prepared
	}
	providers, err := runner.providersFor(request)
	if err != nil {
		return model.RunSummary{}, err
	}

	startedAt := runner.Now()
	runID, err := runner.NewRunID()
	if err != nil {
		return model.RunSummary{}, fmt.Errorf("generate run ID: %w", err)
	}
	network := runner.Snapshot()
	request.Network = &network
	request.Report(ProgressEvent{Network: &network})

	measurements := make([]model.Measurement, 0, len(providers))
	for index, entry := range providers {
		request.Report(ProgressEvent{Provider: entry.kind, Phase: ProgressStarting})
		providerRequest := request
		providerRequest.Targets = []model.Provider{entry.kind}
		measurement, err := entry.provider.Measure(ctx, providerRequest)
		if err != nil {
			request.Report(ProgressEvent{Provider: entry.kind, Phase: ProgressFailed, Message: err.Error()})
			return model.RunSummary{}, err
		}
		measurements = append(measurements, measurement)
		request.Report(progressFromMeasurement(measurement))
		if measurement.Status == model.ProviderStatusCancelled {
			for _, remaining := range providers[index+1:] {
				measurements = append(measurements, skippedMeasurement(remaining.kind))
			}
			break
		}
	}

	targets := cloneProviders(request.Targets)
	if len(targets) == 0 {
		targets = make([]model.Provider, len(providers))
		for index, entry := range providers {
			targets[index] = entry.kind
		}
	}
	summary := model.RunSummary{
		SchemaVersion: model.SchemaVersion,
		RunID:         runID,
		ToolVersion:   runner.ToolVersion,
		StartedAt:     startedAt,
		EndedAt:       runner.Now(),
		Command:       request.Command,
		Targets:       targets,
		Status:        model.DeriveRunStatus(measurements),
		Label:         request.Label,
		Note:          request.Note,
		Network:       network,
		Measurements:  measurements,
	}
	if err := summary.Validate(); err != nil {
		return model.RunSummary{}, fmt.Errorf("validate generated summary: %w", err)
	}
	return summary, nil
}

func progressFromMeasurement(measurement model.Measurement) ProgressEvent {
	phase := ProgressComplete
	message := ""
	switch measurement.Status {
	case model.ProviderStatusFailed:
		phase = ProgressFailed
		if measurement.Failure != nil {
			message = measurement.Failure.Message
		}
	case model.ProviderStatusCancelled:
		phase = ProgressCancelled
	case model.ProviderStatusSkipped:
		phase = ProgressWaiting
	}
	server := ""
	if measurement.ServerFQDN != nil {
		server = *measurement.ServerFQDN
	}
	return ProgressEvent{
		Provider:     measurement.Provider,
		Phase:        phase,
		Server:       server,
		DownloadMbps: measurement.DownloadMbps,
		UploadMbps:   measurement.UploadMbps,
		Message:      message,
	}
}

func (runner *SummaryRunner) setDefaults() {
	if runner.ToolVersion == "" {
		runner.ToolVersion = "dev"
	}
	if runner.Now == nil {
		runner.Now = time.Now
	}
	if runner.NewRunID == nil {
		runner.NewRunID = id.NewRunID
	}
	if runner.Snapshot == nil {
		runner.Snapshot = func() model.NetworkContext {
			return model.NetworkContext{
				OS:           runtime.GOOS,
				Architecture: runtime.GOARCH,
			}
		}
	}
}

type providerEntry struct {
	kind     model.Provider
	provider MeasurementProvider
}

func (runner SummaryRunner) providersFor(request Request) ([]providerEntry, error) {
	entries := make([]providerEntry, 0, 8)
	appendProvider := func(kind model.Provider, implementation MeasurementProvider) error {
		if implementation == nil {
			return fmt.Errorf("%w: %s", ErrUnavailable, kind)
		}
		entries = append(entries, providerEntry{kind: kind, provider: implementation})
		return nil
	}

	if len(request.Targets) > 0 {
		seen := make(map[model.Provider]struct{}, len(request.Targets))
		for _, kind := range request.Targets {
			if !model.ProviderValid(kind) {
				return nil, fmt.Errorf("unsupported measurement target %q", kind)
			}
			if _, exists := seen[kind]; exists {
				return nil, fmt.Errorf("duplicate measurement target %q", kind)
			}
			seen[kind] = struct{}{}
			implementation := runner.Providers[kind]
			if kind == model.ProviderMLab && implementation == nil {
				implementation = runner.MLab
			}
			if kind == model.ProviderCampus && implementation == nil {
				implementation = runner.Campus
			}
			if kind == model.ProviderApple && implementation == nil {
				implementation = runner.Apple
			}
			if kind == model.ProviderOokla && implementation == nil {
				implementation = runner.Ookla
			}
			if err := appendProvider(kind, implementation); err != nil {
				return nil, err
			}
		}
		return entries, nil
	}

	switch request.Command {
	case model.CommandCampus:
		if err := appendProvider(model.ProviderCampus, runner.Campus); err != nil {
			return nil, err
		}
	case model.CommandMLab:
		if err := appendProvider(model.ProviderMLab, runner.MLab); err != nil {
			return nil, err
		}
	case model.CommandRun:
		if err := appendProvider(model.ProviderCampus, runner.Campus); err != nil {
			return nil, err
		}
		if err := appendProvider(model.ProviderMLab, runner.MLab); err != nil {
			return nil, err
		}
		// Keep the legacy two-provider fallback usable for callers that build a
		// SummaryRunner without optional helpers, while the production runner
		// includes Apple in its default plan.
		if runner.Apple != nil {
			if err := appendProvider(model.ProviderApple, runner.Apple); err != nil {
				return nil, err
			}
		}
	case model.CommandApple:
		if err := appendProvider(model.ProviderApple, runner.Apple); err != nil {
			return nil, err
		}
	case model.CommandOokla:
		if err := appendProvider(model.ProviderOokla, runner.Ookla); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported measurement command %q", request.Command)
	}
	return entries, nil
}

func skippedMeasurement(provider model.Provider) model.Measurement {
	return model.Measurement{
		Provider: provider,
		Method:   model.ProviderMethod(provider),
		Status:   model.ProviderStatusSkipped,
	}
}

func cloneProviders(providers []model.Provider) []model.Provider {
	if len(providers) == 0 {
		return nil
	}
	result := make([]model.Provider, len(providers))
	copy(result, providers)
	return result
}

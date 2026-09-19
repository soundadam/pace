package provider

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/soundadam/soundprobe/internal/model"
)

type fakeMeasurementProvider struct {
	calls       int
	measurement model.Measurement
	err         error
}

func (provider *fakeMeasurementProvider) Measure(context.Context, Request) (model.Measurement, error) {
	provider.calls++
	return provider.measurement, provider.err
}

func TestSummaryRunnerBuildsCampusSummary(t *testing.T) {
	campus := &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderNJUCampusIPv4)}
	runner := testSummaryRunner(map[model.Provider]MeasurementProvider{model.ProviderNJUCampusIPv4: campus})
	summary, err := runner.Run(context.Background(), Request{
		Command: model.CommandCampus,
		Targets: []model.Provider{model.ProviderNJUCampusIPv4},
		Label:   model.Pointer("office"),
		Note:    model.Pointer("wired"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != model.RunStatusSuccess || len(summary.Measurements) != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	if summary.Measurements[0].Provider != model.ProviderNJUCampusIPv4 {
		t.Fatalf("provider = %q", summary.Measurements[0].Provider)
	}
	if summary.Label == nil || *summary.Label != "office" || summary.Note == nil || *summary.Note != "wired" {
		t.Fatalf("metadata = %v/%v", summary.Label, summary.Note)
	}
	if summary.Network.OS != "testOS" || summary.Network.Architecture != "testArch" {
		t.Fatalf("network = %#v", summary.Network)
	}
}

func TestSummaryRunnerRejectsAPlanWithoutTargets(t *testing.T) {
	campus := &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderNJUCampusIPv4)}
	runner := testSummaryRunner(map[model.Provider]MeasurementProvider{model.ProviderNJUCampusIPv4: campus})
	_, err := runner.Run(context.Background(), Request{Command: model.CommandCampus})
	if err == nil {
		t.Fatal("Run() accepted a request without an ordered target plan")
	}
	if campus.calls != 0 {
		t.Fatalf("campus calls = %d, want 0", campus.calls)
	}
}

func TestSummaryRunnerPreflightsEveryPlannedTarget(t *testing.T) {
	campus := &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderNJUCampusIPv4)}
	runner := testSummaryRunner(map[model.Provider]MeasurementProvider{model.ProviderNJUCampusIPv4: campus})
	_, err := runner.Run(context.Background(), Request{
		Command: model.CommandRun,
		Targets: []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderMLab},
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if campus.calls != 0 {
		t.Fatalf("campus calls = %d, want 0", campus.calls)
	}
}

func TestSummaryRunnerRunsProvidersSequentially(t *testing.T) {
	order := []string{}
	campus := measurementProviderFunc(func(context.Context, Request) (model.Measurement, error) {
		order = append(order, "campus")
		return successfulMeasurement(model.ProviderNJUCampusIPv4), nil
	})
	mlab := measurementProviderFunc(func(context.Context, Request) (model.Measurement, error) {
		order = append(order, "mlab")
		return successfulMeasurement(model.ProviderMLab), nil
	})
	runner := testSummaryRunner(map[model.Provider]MeasurementProvider{
		model.ProviderNJUCampusIPv4: campus,
		model.ProviderMLab:          mlab,
	})
	runner.Snapshot = func() model.NetworkContext {
		order = append(order, "snapshot")
		return model.NetworkContext{OS: "testOS", Architecture: "testArch"}
	}
	summary, err := runner.Run(context.Background(), Request{
		Command: model.CommandRun,
		Targets: []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderMLab},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 || order[0] != "snapshot" || order[1] != "campus" || order[2] != "mlab" {
		t.Fatalf("order = %#v", order)
	}
	if summary.Status != model.RunStatusSuccess {
		t.Fatalf("status = %q", summary.Status)
	}
}

func TestSummaryRunnerKeepsPlannedTargetOrder(t *testing.T) {
	want := []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderMLab, model.ProviderApple}
	providers := map[model.Provider]MeasurementProvider{}
	for _, kind := range want {
		providers[kind] = &fakeMeasurementProvider{measurement: successfulMeasurement(kind)}
	}
	runner := testSummaryRunner(providers)
	summary, err := runner.Run(context.Background(), Request{Command: model.CommandRun, Targets: want})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Targets) != len(want) || len(summary.Measurements) != len(want) {
		t.Fatalf("summary = %#v", summary)
	}
	for index := range want {
		if summary.Targets[index] != want[index] || summary.Measurements[index].Provider != want[index] {
			t.Fatalf("target[%d] = %q/%q, want %q", index, summary.Targets[index], summary.Measurements[index].Provider, want[index])
		}
	}
}

func TestSummaryRunnerPrepareDropsUnavailableOptionalProvider(t *testing.T) {
	apple := &preflightMeasurementProvider{
		fakeMeasurementProvider: fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderApple)},
		preflightErr:            fmt.Errorf("%w: Apple networkQuality is available on macOS only", ErrUnavailable),
	}
	runner := testSummaryRunner(map[model.Provider]MeasurementProvider{
		model.ProviderNJUCampusIPv4: &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderNJUCampusIPv4)},
		model.ProviderMLab:          &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderMLab)},
		model.ProviderApple:         apple,
	})
	prepared, err := runner.Prepare(context.Background(), Request{
		Command: model.CommandRun,
		Targets: []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderMLab, model.ProviderApple},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderMLab}
	if fmt.Sprint(prepared.Targets) != fmt.Sprint(want) {
		t.Fatalf("prepared targets = %#v, want %#v", prepared.Targets, want)
	}
}

func TestSummaryRunnerPrepareKeepsExplicitOptionalFailureClosed(t *testing.T) {
	ookla := &preflightMeasurementProvider{
		fakeMeasurementProvider: fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderOokla)},
		preflightErr:            fmt.Errorf("%w: official Ookla CLI was not found", ErrUnavailable),
	}
	runner := testSummaryRunner(map[model.Provider]MeasurementProvider{model.ProviderOokla: ookla})
	_, err := runner.Prepare(context.Background(), Request{
		Command: model.CommandOokla,
		Targets: []model.Provider{model.ProviderOokla},
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestSummaryRunnerSkipsNextProviderAfterCancellation(t *testing.T) {
	campus := &fakeMeasurementProvider{measurement: model.Measurement{
		Provider: model.ProviderNJUCampusIPv4,
		Method:   model.MethodLibreSpeedThreeStream,
		Status:   model.ProviderStatusCancelled,
		Failure: &model.Failure{
			Stage:   model.FailureStageCancelled,
			Code:    "cancelled",
			Message: "cancelled",
		},
	}}
	mlab := &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderMLab)}
	runner := testSummaryRunner(map[model.Provider]MeasurementProvider{
		model.ProviderNJUCampusIPv4: campus,
		model.ProviderMLab:          mlab,
	})
	summary, err := runner.Run(context.Background(), Request{
		Command: model.CommandRun,
		Targets: []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderMLab},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mlab.calls != 0 {
		t.Fatalf("M-Lab calls = %d, want 0", mlab.calls)
	}
	if summary.Status != model.RunStatusCancelled || summary.Measurements[1].Status != model.ProviderStatusSkipped {
		t.Fatalf("summary = %#v", summary)
	}
}

type measurementProviderFunc func(context.Context, Request) (model.Measurement, error)

func (function measurementProviderFunc) Measure(ctx context.Context, request Request) (model.Measurement, error) {
	return function(ctx, request)
}

type preflightMeasurementProvider struct {
	fakeMeasurementProvider
	preflightErr error
}

func (provider *preflightMeasurementProvider) Preflight(context.Context, Request) error {
	return provider.preflightErr
}

func testSummaryRunner(providers map[model.Provider]MeasurementProvider) SummaryRunner {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	return SummaryRunner{
		ToolVersion: "test",
		Providers:   providers,
		Now: func() time.Time {
			now = now.Add(time.Second)
			return now
		},
		NewRunID: func() (string, error) {
			return "00000000-0000-4000-8000-000000000002", nil
		},
		Snapshot: func() model.NetworkContext {
			return model.NetworkContext{OS: "testOS", Architecture: "testArch"}
		},
	}
}

func successfulMeasurement(provider model.Provider) model.Measurement {
	return model.Measurement{
		Provider:     provider,
		Method:       model.ProviderMethod(provider),
		Status:       model.ProviderStatusSuccess,
		DownloadMbps: model.Pointer(100.0),
		UploadMbps:   model.Pointer(50.0),
	}
}

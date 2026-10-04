package provider

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/soundadam/pace/internal/model"
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
	campus := &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderCampus)}
	runner := testSummaryRunner(campus, nil)
	summary, err := runner.Run(context.Background(), Request{
		Command: model.CommandCampus,
		Label:   model.Pointer("office"),
		Note:    model.Pointer("wired"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != model.RunStatusSuccess || len(summary.Measurements) != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	if summary.Label == nil || *summary.Label != "office" || summary.Note == nil || *summary.Note != "wired" {
		t.Fatalf("metadata = %v/%v", summary.Label, summary.Note)
	}
	if summary.Network.OS != "testOS" || summary.Network.Architecture != "testArch" {
		t.Fatalf("network = %#v", summary.Network)
	}
}

func TestSummaryRunnerPreflightsRunProviders(t *testing.T) {
	campus := &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderCampus)}
	runner := testSummaryRunner(campus, nil)
	_, err := runner.Run(context.Background(), Request{Command: model.CommandRun})
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
		return successfulMeasurement(model.ProviderCampus), nil
	})
	mlab := measurementProviderFunc(func(context.Context, Request) (model.Measurement, error) {
		order = append(order, "mlab")
		return successfulMeasurement(model.ProviderMLab), nil
	})
	runner := testSummaryRunner(campus, mlab)
	runner.Snapshot = func() model.NetworkContext {
		order = append(order, "snapshot")
		return model.NetworkContext{OS: "testOS", Architecture: "testArch"}
	}
	summary, err := runner.Run(context.Background(), Request{Command: model.CommandRun})
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

func TestSummaryRunnerIncludesConfiguredAppleInDefaultRun(t *testing.T) {
	campus := &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderCampus)}
	mlab := &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderMLab)}
	apple := &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderApple)}
	runner := testSummaryRunner(campus, mlab)
	runner.Apple = apple
	summary, err := runner.Run(context.Background(), Request{Command: model.CommandRun})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Provider{model.ProviderCampus, model.ProviderMLab, model.ProviderApple}
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
	campus := &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderCampus)}
	mlab := &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderMLab)}
	apple := &preflightMeasurementProvider{
		fakeMeasurementProvider: fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderApple)},
		preflightErr:            fmt.Errorf("%w: Apple networkQuality is available on macOS only", ErrUnavailable),
	}
	runner := testSummaryRunner(campus, mlab)
	runner.Apple = apple
	prepared, err := runner.Prepare(context.Background(), Request{
		Command: model.CommandRun,
		Targets: []model.Provider{model.ProviderCampus, model.ProviderMLab, model.ProviderApple},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Provider{model.ProviderCampus, model.ProviderMLab}
	if fmt.Sprint(prepared.Targets) != fmt.Sprint(want) {
		t.Fatalf("prepared targets = %#v, want %#v", prepared.Targets, want)
	}
}

func TestSummaryRunnerPrepareKeepsExplicitOptionalFailureClosed(t *testing.T) {
	ookla := &preflightMeasurementProvider{
		fakeMeasurementProvider: fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderOokla)},
		preflightErr:            fmt.Errorf("%w: official Ookla CLI was not found", ErrUnavailable),
	}
	runner := testSummaryRunner(nil, nil)
	runner.Ookla = ookla
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
		Provider: model.ProviderCampus,
		Method:   model.MethodLibreSpeedThreeStream,
		Status:   model.ProviderStatusCancelled,
		Failure: &model.Failure{
			Stage:   model.FailureStageCancelled,
			Code:    "cancelled",
			Message: "cancelled",
		},
	}}
	mlab := &fakeMeasurementProvider{measurement: successfulMeasurement(model.ProviderMLab)}
	runner := testSummaryRunner(campus, mlab)
	summary, err := runner.Run(context.Background(), Request{Command: model.CommandRun})
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

func testSummaryRunner(campus, mlab MeasurementProvider) SummaryRunner {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	return SummaryRunner{
		ToolVersion: "test",
		Campus:      campus,
		MLab:        mlab,
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
	method := model.MethodLibreSpeedThreeStream
	if provider == model.ProviderMLab {
		method = model.MethodNDT7SingleStream
	}
	if provider == model.ProviderApple {
		method = model.MethodAppleNetworkQuality
	}
	if provider == model.ProviderOokla {
		method = model.MethodOoklaSpeedtest
	}
	return model.Measurement{
		Provider:     provider,
		Method:       method,
		Status:       model.ProviderStatusSuccess,
		DownloadMbps: model.Pointer(100.0),
		UploadMbps:   model.Pointer(50.0),
	}
}

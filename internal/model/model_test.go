package model

import (
	"testing"
	"time"
)

func TestDeriveRunStatus(t *testing.T) {
	tests := []struct {
		name         string
		measurements []Measurement
		want         RunStatus
	}{
		{name: "no measurements", want: RunStatusFailed},
		{name: "all success", measurements: []Measurement{{Status: ProviderStatusSuccess}, {Status: ProviderStatusSuccess}}, want: RunStatusSuccess},
		{name: "partial", measurements: []Measurement{{Status: ProviderStatusSuccess}, {Status: ProviderStatusFailed}}, want: RunStatusPartial},
		{name: "all failed", measurements: []Measurement{{Status: ProviderStatusFailed}, {Status: ProviderStatusFailed}}, want: RunStatusFailed},
		{name: "cancelled wins", measurements: []Measurement{{Status: ProviderStatusSuccess}, {Status: ProviderStatusCancelled}}, want: RunStatusCancelled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DeriveRunStatus(test.measurements); got != test.want {
				t.Fatalf("DeriveRunStatus() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSkippedMeasurementRequiresNullSpeeds(t *testing.T) {
	now := testTime()
	summary := RunSummary{
		SchemaVersion: SchemaVersion,
		RunID:         "run-1",
		ToolVersion:   "dev",
		StartedAt:     now,
		EndedAt:       now,
		Command:       CommandMLab,
		Targets:       []Provider{ProviderMLab},
		Status:        RunStatusFailed,
		Measurements: []Measurement{{
			Provider:     ProviderMLab,
			Method:       "ndt7-single-stream",
			Status:       ProviderStatusSkipped,
			DownloadMbps: Pointer(0.0),
		}},
	}
	if err := summary.Validate(); err == nil {
		t.Fatal("Validate() succeeded for skipped measurement with a speed")
	}
}

func TestRunStatusMustMatchMeasurements(t *testing.T) {
	now := testTime()
	summary := RunSummary{
		SchemaVersion: SchemaVersion,
		RunID:         "run-1",
		ToolVersion:   "dev",
		StartedAt:     now,
		EndedAt:       now,
		Command:       CommandCampus,
		Targets:       []Provider{ProviderNJUCampusIPv4},
		Status:        RunStatusPartial,
		Measurements: []Measurement{{
			Provider:     ProviderNJUCampusIPv4,
			Method:       "librespeed-three-stream",
			Status:       ProviderStatusSuccess,
			DownloadMbps: Pointer(100.0),
			UploadMbps:   Pointer(50.0),
		}},
	}
	if err := summary.Validate(); err == nil {
		t.Fatal("Validate() succeeded for inconsistent run status")
	}
}

func TestMeasurementMetadataValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Measurement)
	}{
		{name: "negative bytes", mutate: func(measurement *Measurement) { measurement.DownloadBytes = Pointer(int64(-1)) }},
		{name: "negative duration", mutate: func(measurement *Measurement) { measurement.DurationMS = Pointer(int64(-1)) }},
		{name: "zero concurrency", mutate: func(measurement *Measurement) { measurement.Concurrency = Pointer(0) }},
		{name: "invalid family", mutate: func(measurement *Measurement) { measurement.IPFamily = Pointer("ipv5") }},
		{name: "empty helper version", mutate: func(measurement *Measurement) { measurement.HelperVersion = Pointer("") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := testTime()
			measurement := Measurement{
				Provider:     ProviderNJUCampusIPv4,
				Method:       MethodLibreSpeedThreeStream,
				Status:       ProviderStatusSuccess,
				DownloadMbps: Pointer(100.0),
				UploadMbps:   Pointer(50.0),
			}
			test.mutate(&measurement)
			summary := RunSummary{
				SchemaVersion: SchemaVersion,
				RunID:         "run-1",
				ToolVersion:   "dev",
				StartedAt:     now,
				EndedAt:       now,
				Command:       CommandCampus,
				Targets:       []Provider{ProviderNJUCampusIPv4},
				Status:        RunStatusSuccess,
				Measurements:  []Measurement{measurement},
			}
			if err := summary.Validate(); err == nil {
				t.Fatal("Validate() succeeded for invalid metadata")
			}
		})
	}
}

func TestFailedMeasurementRequiresZeroOrMeasuredSpeeds(t *testing.T) {
	now := testTime()
	summary := RunSummary{
		SchemaVersion: SchemaVersion,
		RunID:         "run-1",
		ToolVersion:   "dev",
		StartedAt:     now,
		EndedAt:       now,
		Command:       CommandCampus,
		Targets:       []Provider{ProviderNJUCampusIPv4},
		Status:        RunStatusFailed,
		Measurements: []Measurement{{
			Provider: ProviderNJUCampusIPv4,
			Method:   "librespeed-three-stream",
			Status:   ProviderStatusFailed,
			Failure: &Failure{
				Stage:   FailureStageConnect,
				Code:    "unreachable",
				Message: "campus endpoint was unreachable",
			},
		}},
	}
	if err := summary.Validate(); err == nil {
		t.Fatal("Validate() succeeded for failed measurement with null speeds")
	}
}

func TestRunSummaryAcceptsExplicitMultiTargetPlan(t *testing.T) {
	now := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
	providers := []Provider{ProviderNJUEdgeIPv4, ProviderNJUEdgeIPv6, ProviderMLab}
	measurements := make([]Measurement, 0, len(providers))
	for _, measurementProvider := range providers {
		measurements = append(measurements, Measurement{
			Provider:     measurementProvider,
			Method:       ProviderMethod(measurementProvider),
			Status:       ProviderStatusSuccess,
			DownloadMbps: Pointer(10.0),
			UploadMbps:   Pointer(5.0),
		})
	}
	summary := RunSummary{
		SchemaVersion: SchemaVersion,
		RunID:         "00000000-0000-4000-8000-000000000099",
		ToolVersion:   "test",
		StartedAt:     now,
		EndedAt:       now.Add(time.Second),
		Command:       CommandRun,
		Targets:       providers,
		Status:        RunStatusSuccess,
		Measurements:  measurements,
	}
	if err := summary.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRunSummaryRequiresAnOrderedTargetPlan(t *testing.T) {
	now := testTime()
	summary := RunSummary{
		SchemaVersion: SchemaVersion,
		RunID:         "plan-less-run",
		ToolVersion:   "test",
		StartedAt:     now,
		EndedAt:       now.Add(time.Second),
		Command:       CommandCampus,
		Status:        RunStatusSuccess,
		Measurements: []Measurement{{
			Provider:     ProviderNJUCampusIPv4,
			Method:       MethodLibreSpeedThreeStream,
			Status:       ProviderStatusSuccess,
			DownloadMbps: Pointer(100.0),
			UploadMbps:   Pointer(50.0),
		}},
	}
	if err := summary.Validate(); err == nil {
		t.Fatal("Validate() accepted a summary without a target plan")
	}
}

func TestRunSummaryRejectsSupersededSchemaVersion(t *testing.T) {
	now := testTime()
	summary := RunSummary{
		SchemaVersion: 1,
		RunID:         "legacy-campus-run",
		ToolVersion:   "0.1.3",
		StartedAt:     now,
		EndedAt:       now.Add(time.Second),
		Command:       CommandCampus,
		Targets:       []Provider{ProviderNJUCampusIPv4},
		Status:        RunStatusSuccess,
		Measurements: []Measurement{{
			Provider:     ProviderNJUCampusIPv4,
			Method:       MethodLibreSpeedThreeStream,
			Status:       ProviderStatusSuccess,
			DownloadMbps: Pointer(100.0),
			UploadMbps:   Pointer(50.0),
		}},
	}
	if err := summary.Validate(); err == nil {
		t.Fatal("Validate() accepted a schema-v1 summary")
	}
}

func TestRunSummaryRejectsRetiredCampusProvider(t *testing.T) {
	now := testTime()
	summary := RunSummary{
		SchemaVersion: SchemaVersion,
		RunID:         "legacy-campus-run",
		ToolVersion:   "0.1.3",
		StartedAt:     now,
		EndedAt:       now.Add(time.Second),
		Command:       CommandCampus,
		Targets:       []Provider{Provider("campus")},
		Status:        RunStatusSuccess,
		Measurements: []Measurement{{
			Provider:     Provider("campus"),
			Method:       MethodLibreSpeedThreeStream,
			Status:       ProviderStatusSuccess,
			DownloadMbps: Pointer(100.0),
			UploadMbps:   Pointer(50.0),
		}},
	}
	if err := summary.Validate(); err == nil {
		t.Fatal("Validate() accepted the retired campus provider ID")
	}
}

func TestRunSummaryRejectsMeasurementOrderMismatch(t *testing.T) {
	now := testTime()
	summary := RunSummary{
		SchemaVersion: SchemaVersion,
		RunID:         "ordered-targets",
		ToolVersion:   "test",
		StartedAt:     now,
		EndedAt:       now.Add(time.Second),
		Command:       CommandRun,
		Targets:       []Provider{ProviderNJUEdgeIPv4, ProviderMLab},
		Status:        RunStatusSuccess,
		Measurements: []Measurement{
			{Provider: ProviderMLab, Method: MethodNDT7SingleStream, Status: ProviderStatusSuccess, DownloadMbps: Pointer(1.0), UploadMbps: Pointer(1.0)},
			{Provider: ProviderNJUEdgeIPv4, Method: MethodLibreSpeedThreeStream, Status: ProviderStatusSuccess, DownloadMbps: Pointer(1.0), UploadMbps: Pointer(1.0)},
		},
	}
	if err := summary.Validate(); err == nil {
		t.Fatal("Validate() accepted measurements in a different order from targets")
	}
}

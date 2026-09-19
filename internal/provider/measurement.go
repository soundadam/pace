package provider

import "github.com/soundadam/soundprobe/internal/model"

// Attempt describes the measurement a provider was running when it failed or
// was cancelled.  Every field is optional: a provider fills in what it actually
// knows and leaves the rest zero, so an unknown value is recorded as JSON null
// rather than as an invented default.
type Attempt struct {
	Provider      model.Provider
	IPFamily      string
	ServerName    string
	Concurrency   int
	HelperVersion string
	DurationMS    int64
}

// FailedMeasurement builds the single failed-measurement shape shared by every
// provider.  A failed measurement always reports zero throughput and zero
// transferred bytes — the attempt happened and moved nothing — so `--json`
// consumers can read the same fields regardless of which provider failed.
func FailedMeasurement(attempt Attempt, stage model.FailureStage, code, message string) model.Measurement {
	measurement := attempt.measurement(model.ProviderStatusFailed)
	measurement.DownloadMbps = model.Pointer(0.0)
	measurement.UploadMbps = model.Pointer(0.0)
	measurement.DownloadBytes = model.Pointer(int64(0))
	measurement.UploadBytes = model.Pointer(int64(0))
	measurement.Failure = &model.Failure{Stage: stage, Code: code, Message: message}
	return measurement
}

// CancelledMeasurement builds the single cancelled-measurement shape shared by
// every provider.  Nothing was completed, so throughput and byte counters stay
// null instead of claiming a measured zero.
func CancelledMeasurement(attempt Attempt, message string) model.Measurement {
	measurement := attempt.measurement(model.ProviderStatusCancelled)
	measurement.Failure = &model.Failure{
		Stage:   model.FailureStageCancelled,
		Code:    "cancelled",
		Message: message,
	}
	return measurement
}

// SkippedMeasurement builds the placeholder recorded for a target that never
// started because an earlier target was cancelled.
func SkippedMeasurement(measurementProvider model.Provider) model.Measurement {
	return Attempt{Provider: measurementProvider}.measurement(model.ProviderStatusSkipped)
}

func (attempt Attempt) measurement(status model.ProviderStatus) model.Measurement {
	measurement := model.Measurement{
		Provider: attempt.Provider,
		Method:   model.ProviderMethod(attempt.Provider),
		Status:   status,
	}
	if attempt.IPFamily != "" {
		measurement.IPFamily = model.Pointer(attempt.IPFamily)
	}
	if attempt.ServerName != "" {
		measurement.ServerName = model.Pointer(attempt.ServerName)
	}
	if attempt.Concurrency > 0 {
		measurement.Concurrency = model.Pointer(attempt.Concurrency)
	}
	if attempt.HelperVersion != "" {
		measurement.HelperVersion = model.Pointer(attempt.HelperVersion)
	}
	if status != model.ProviderStatusSkipped {
		measurement.DurationMS = model.Pointer(attempt.DurationMS)
	}
	return measurement
}

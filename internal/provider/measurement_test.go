package provider

import (
	"encoding/json"
	"testing"

	"github.com/soundadam/soundprobe/internal/model"
)

// A failed measurement must read the same way whichever provider produced it:
// zero throughput, zero bytes, a duration, and a failure object.
func TestFailedMeasurementShapeIsProviderIndependent(t *testing.T) {
	for _, attempt := range []Attempt{
		{Provider: model.ProviderNJUCampusIPv4, IPFamily: "ipv4", ServerName: "NJU Campus · IPV4", Concurrency: 3, HelperVersion: "v1.0.13-campus.1", DurationMS: 1200},
		{Provider: model.ProviderMLab, Concurrency: 1, HelperVersion: "v0.10.1", DurationMS: 1200},
		{Provider: model.ProviderApple, ServerName: "Apple networkQuality", HelperVersion: "system", DurationMS: 1200},
		{Provider: model.ProviderOokla, HelperVersion: "1.2.0", DurationMS: 1200},
	} {
		t.Run(string(attempt.Provider), func(t *testing.T) {
			measurement := FailedMeasurement(attempt, model.FailureStageConnect, "connect_failure", "server refused the connection")
			if measurement.Method != model.ProviderMethod(attempt.Provider) {
				t.Fatalf("method = %q", measurement.Method)
			}
			if measurement.Status != model.ProviderStatusFailed {
				t.Fatalf("status = %q", measurement.Status)
			}
			for name, value := range map[string]*float64{
				"downloadMbps": measurement.DownloadMbps,
				"uploadMbps":   measurement.UploadMbps,
			} {
				if value == nil || *value != 0 {
					t.Fatalf("%s = %v, want 0", name, value)
				}
			}
			for name, value := range map[string]*int64{
				"downloadBytes": measurement.DownloadBytes,
				"uploadBytes":   measurement.UploadBytes,
				"durationMs":    measurement.DurationMS,
			} {
				if value == nil {
					t.Fatalf("%s is null, want a value", name)
				}
			}
			if *measurement.DownloadBytes != 0 || *measurement.UploadBytes != 0 {
				t.Fatalf("bytes = %d/%d, want 0/0", *measurement.DownloadBytes, *measurement.UploadBytes)
			}
			if measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageConnect {
				t.Fatalf("failure = %#v", measurement.Failure)
			}
			assertMeasurementKeys(t, measurement)
		})
	}
}

// A cancelled measurement completed nothing, so speeds and byte counters stay
// null rather than claiming a measured zero.
func TestCancelledMeasurementReportsNoThroughput(t *testing.T) {
	measurement := CancelledMeasurement(Attempt{Provider: model.ProviderMLab, Concurrency: 1, DurationMS: 30}, "M-Lab measurement was cancelled")
	if measurement.Status != model.ProviderStatusCancelled {
		t.Fatalf("status = %q", measurement.Status)
	}
	if measurement.DownloadMbps != nil || measurement.UploadMbps != nil {
		t.Fatalf("speeds = %v/%v, want null", measurement.DownloadMbps, measurement.UploadMbps)
	}
	if measurement.DownloadBytes != nil || measurement.UploadBytes != nil {
		t.Fatalf("bytes = %v/%v, want null", measurement.DownloadBytes, measurement.UploadBytes)
	}
	if measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageCancelled {
		t.Fatalf("failure = %#v", measurement.Failure)
	}
}

func TestSkippedMeasurementHasNoAttemptData(t *testing.T) {
	measurement := SkippedMeasurement(model.ProviderApple)
	if measurement.Status != model.ProviderStatusSkipped {
		t.Fatalf("status = %q", measurement.Status)
	}
	if measurement.DownloadMbps != nil || measurement.UploadMbps != nil || measurement.DurationMS != nil {
		t.Fatalf("measurement = %#v", measurement)
	}
}

// The `--json` contract promises a fixed set of measurement keys.  Guard it
// here so a provider-specific field cannot quietly disappear from a failure.
func assertMeasurementKeys(t *testing.T, measurement model.Measurement) {
	t.Helper()
	data, err := json.Marshal(measurement)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"provider", "method", "status", "ipFamily", "serverName", "serverFqdn",
		"serverAddress", "clientPublicIp", "pingMs", "jitterMs", "downloadMbps",
		"uploadMbps", "downloadBytes", "uploadBytes", "durationMs",
		"concurrency", "helperVersion", "failure",
	} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("measurement JSON is missing %q: %s", key, data)
		}
	}
}

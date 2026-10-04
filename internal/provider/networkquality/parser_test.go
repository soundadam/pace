package networkquality

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soundadam/pace/internal/model"
)

func TestParseSuccessJSON(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "success.json"))
	if err != nil {
		t.Fatal(err)
	}
	measurement, err := parseResult(data, 1234)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Provider != model.ProviderApple || measurement.Method != model.MethodAppleNetworkQuality || measurement.Status != model.ProviderStatusSuccess {
		t.Fatalf("measurement identity = %#v", measurement)
	}
	if measurement.DownloadMbps == nil || *measurement.DownloadMbps != 125 || measurement.UploadMbps == nil || *measurement.UploadMbps != 25 {
		t.Fatalf("throughput = %v/%v", measurement.DownloadMbps, measurement.UploadMbps)
	}
	if measurement.ServerFQDN != nil {
		t.Fatalf("interface was incorrectly stored as server FQDN: %v", measurement.ServerFQDN)
	}
	if measurement.ResponsivenessRPM == nil || *measurement.ResponsivenessRPM != 400.375 {
		t.Fatalf("responsiveness = %v", measurement.ResponsivenessRPM)
	}
	if measurement.UploadResponsivenessRPM == nil || *measurement.UploadResponsivenessRPM != 380.25 || measurement.DownloadResponsivenessRPM == nil || *measurement.DownloadResponsivenessRPM != 420.5 {
		t.Fatalf("directional responsiveness = %v/%v", measurement.UploadResponsivenessRPM, measurement.DownloadResponsivenessRPM)
	}
}

func TestParseErrorJSONProducesAttemptedFailure(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "error.json"))
	if err != nil {
		t.Fatal(err)
	}
	measurement, err := parseResult(data, 20)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Status != model.ProviderStatusFailed || measurement.DownloadMbps == nil || *measurement.DownloadMbps != 0 || measurement.UploadMbps == nil || *measurement.UploadMbps != 0 {
		t.Fatalf("measurement = %#v", measurement)
	}
	if measurement.Failure == nil || measurement.Failure.Code != "network_unavailable" {
		t.Fatalf("failure = %#v", measurement.Failure)
	}
}

func TestParseRejectsIncompleteAndTrailingJSON(t *testing.T) {
	if _, err := parseResult([]byte(`{"dl_throughput": 1}`), 1); err == nil || !strings.Contains(err.Error(), "missing download or upload") {
		t.Fatalf("incomplete result error = %v", err)
	}
	if _, err := parseResult([]byte(`{"dl_throughput": 1, "ul_throughput": 1} {}`), 1); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing result error = %v", err)
	}
}

package mlab

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/soundadam/pace/internal/model"
	"github.com/soundadam/pace/internal/provider"
)

func TestAccumulatorBuildsSuccessfulMeasurement(t *testing.T) {
	file, err := os.Open("testdata/ndt7-success.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var progress []provider.ProgressEvent
	request := provider.Request{Progress: func(event provider.ProgressEvent) {
		progress = append(progress, event)
	}}
	var parsed accumulator
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if err := parsed.consume(scanner.Bytes(), request); err != nil {
			t.Fatal(err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	measurement, err := parsed.measurement(HelperVersion, 25_000)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Status != model.ProviderStatusSuccess {
		t.Fatalf("status = %q", measurement.Status)
	}
	if measurement.DownloadMbps == nil || *measurement.DownloadMbps != 80 {
		t.Fatalf("download = %v", measurement.DownloadMbps)
	}
	if measurement.UploadMbps == nil || *measurement.UploadMbps != 40 {
		t.Fatalf("upload = %v", measurement.UploadMbps)
	}
	if measurement.DownloadBytes == nil || *measurement.DownloadBytes != 100000000 {
		t.Fatalf("download bytes = %v", measurement.DownloadBytes)
	}
	if measurement.UploadBytes == nil || *measurement.UploadBytes != 50000000 {
		t.Fatalf("upload bytes = %v", measurement.UploadBytes)
	}
	if measurement.ServerFQDN == nil || *measurement.ServerFQDN != "ndt-mlab1.example.net" {
		t.Fatalf("server FQDN = %v", measurement.ServerFQDN)
	}
	if measurement.ClientPublicIP == nil || *measurement.ClientPublicIP != "198.51.100.42" {
		t.Fatalf("client IP = %v", measurement.ClientPublicIP)
	}
	if measurement.IPFamily == nil || *measurement.IPFamily != "ipv4" {
		t.Fatalf("IP family = %v", measurement.IPFamily)
	}
	if len(progress) < 4 {
		t.Fatalf("progress events = %d", len(progress))
	}
	foundLive := false
	for _, event := range progress {
		if event.LiveMbps != nil && *event.LiveMbps > 0 {
			foundLive = true
		}
	}
	if !foundLive {
		t.Fatal("no live-rate progress event was emitted")
	}
}

func TestAccumulatorPreservesPartialFailure(t *testing.T) {
	data, err := os.ReadFile("testdata/ndt7-upload-failure.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var parsed accumulator
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if err := parsed.consume([]byte(line), provider.Request{}); err != nil {
			t.Fatal(err)
		}
	}
	measurement, err := parsed.measurement(HelperVersion, 30_000)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Status != model.ProviderStatusFailed {
		t.Fatalf("status = %q", measurement.Status)
	}
	if measurement.DownloadMbps == nil || *measurement.DownloadMbps != 80 {
		t.Fatalf("download = %v", measurement.DownloadMbps)
	}
	if measurement.UploadMbps == nil || *measurement.UploadMbps != 0 {
		t.Fatalf("upload = %v", measurement.UploadMbps)
	}
	if measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageUpload {
		t.Fatalf("failure = %#v", measurement.Failure)
	}
}

func TestAccumulatorRejectsMalformedOutput(t *testing.T) {
	var parsed accumulator
	if err := parsed.consume([]byte(`not-json`), provider.Request{}); err == nil {
		t.Fatal("consume() succeeded for malformed JSON")
	}
	if _, err := parsed.measurement(HelperVersion, 1); err == nil {
		t.Fatal("measurement() succeeded without a summary or error")
	}
}

func TestLiveMeasurement(t *testing.T) {
	tests := []struct {
		name      string
		value     eventValue
		wantRate  float64
		wantBytes int64
		wantOK    bool
	}{
		{
			name: "download client app info",
			value: eventValue{
				Test:    "download",
				Origin:  "client",
				AppInfo: &appInfo{ElapsedTime: 10_000_000, NumBytes: 100_000_000},
			},
			wantRate:  80,
			wantBytes: 100_000_000,
			wantOK:    true,
		},
		{
			name: "upload server tcp received",
			value: eventValue{
				Test:   "upload",
				Origin: "server",
				TCPInfo: &tcpInfo{
					ElapsedTime:   10_000_000,
					BytesReceived: 50_000_000,
					BytesSent:     60_000_000,
				},
			},
			wantRate:  40,
			wantBytes: 50_000_000,
			wantOK:    true,
		},
		{
			name: "download server tcp info is irrelevant",
			value: eventValue{
				Test:   "download",
				Origin: "server",
				TCPInfo: &tcpInfo{
					ElapsedTime:   10_000_000,
					BytesReceived: 0,
					BytesSent:     100_000_000,
				},
			},
		},
		{
			name: "upload client app info is irrelevant",
			value: eventValue{
				Test:    "upload",
				Origin:  "client",
				AppInfo: &appInfo{ElapsedTime: 10_000_000, NumBytes: 50_000_000},
			},
		},
		{
			name: "zero download bytes",
			value: eventValue{
				Test:    "download",
				Origin:  "client",
				AppInfo: &appInfo{ElapsedTime: 10_000_000, NumBytes: 0},
			},
		},
		{
			name: "zero upload bytes received does not fall back to sent",
			value: eventValue{
				Test:   "upload",
				Origin: "server",
				TCPInfo: &tcpInfo{
					ElapsedTime:   10_000_000,
					BytesReceived: 0,
					BytesSent:     50_000_000,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rate, bytes, ok := liveMeasurement(test.value)
			if rate != test.wantRate || bytes != test.wantBytes || ok != test.wantOK {
				t.Fatalf("rate=%v bytes=%d ok=%v, want rate=%v bytes=%d ok=%v", rate, bytes, ok, test.wantRate, test.wantBytes, test.wantOK)
			}
		})
	}
}

func TestAccumulatorIgnoresIrrelevantAndNonIncreasingLiveMeasurements(t *testing.T) {
	lines := []string{
		`{"Key":"measurement","Value":{"AppInfo":{"ElapsedTime":1000000,"NumBytes":10000000},"Origin":"client","Test":"download"}}`,
		`{"Key":"measurement","Value":{"TCPInfo":{"ElapsedTime":1000000,"BytesReceived":0,"BytesSent":0},"Origin":"server","Test":"download"}}`,
		`{"Key":"measurement","Value":{"AppInfo":{"ElapsedTime":2000000,"NumBytes":10000000},"Origin":"client","Test":"download"}}`,
		`{"Key":"measurement","Value":{"AppInfo":{"ElapsedTime":3000000,"NumBytes":9000000},"Origin":"client","Test":"download"}}`,
		`{"Key":"measurement","Value":{"TCPInfo":{"ElapsedTime":1000000,"BytesReceived":5000000,"BytesSent":6000000},"Origin":"server","Test":"upload"}}`,
		`{"Key":"measurement","Value":{"TCPInfo":{"ElapsedTime":2000000,"BytesReceived":5000000,"BytesSent":7000000},"Origin":"server","Test":"upload"}}`,
		`{"Key":"measurement","Value":{"TCPInfo":{"ElapsedTime":3000000,"BytesReceived":4000000,"BytesSent":8000000},"Origin":"server","Test":"upload"}}`,
	}
	var progress []provider.ProgressEvent
	request := provider.Request{Progress: func(event provider.ProgressEvent) {
		progress = append(progress, event)
	}}
	var parsed accumulator
	for _, line := range lines {
		if err := parsed.consume([]byte(line), request); err != nil {
			t.Fatal(err)
		}
	}

	var liveRates []float64
	for _, event := range progress {
		if event.LiveMbps != nil {
			liveRates = append(liveRates, *event.LiveMbps)
		}
	}
	if len(liveRates) != 2 || liveRates[0] != 80 || liveRates[1] != 40 {
		t.Fatalf("live rates = %v, want [80 40]", liveRates)
	}
	if parsed.downloadBytes != 10_000_000 {
		t.Fatalf("download bytes = %d, want 10000000", parsed.downloadBytes)
	}
	if parsed.uploadBytes != 5_000_000 {
		t.Fatalf("upload bytes = %d, want 5000000", parsed.uploadBytes)
	}
}

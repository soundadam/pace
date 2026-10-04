package ookla

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soundadam/pace/internal/model"
)

func TestParseOfficialJSON(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "success.json"))
	if err != nil {
		t.Fatal(err)
	}
	measurement, err := parseResult(data, "1.2.0", 2345)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Provider != model.ProviderOokla || measurement.Status != model.ProviderStatusSuccess {
		t.Fatalf("measurement identity = %#v", measurement)
	}
	if measurement.DownloadMbps == nil || *measurement.DownloadMbps != 50 || measurement.UploadMbps == nil || *measurement.UploadMbps != 10 {
		t.Fatalf("speeds = %v/%v", measurement.DownloadMbps, measurement.UploadMbps)
	}
	if measurement.ServerID == nil || *measurement.ServerID != 30852 || measurement.ServerSponsor == nil || *measurement.ServerSponsor != "Duke Kunshan University" {
		t.Fatalf("server metadata = %v/%v", measurement.ServerID, measurement.ServerSponsor)
	}
	if measurement.ServerFQDN == nil || *measurement.ServerFQDN != "speedtest.dukekunshan.edu.cn" {
		t.Fatalf("server FQDN = %v", measurement.ServerFQDN)
	}
	if measurement.IPFamily == nil || *measurement.IPFamily != "ipv4" {
		t.Fatalf("IP family = %v", measurement.IPFamily)
	}
}

func TestParseErrorJSONProducesFailure(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "error.json"))
	if err != nil {
		t.Fatal(err)
	}
	measurement, err := parseResult(data, "1.2.0", 5)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Status != model.ProviderStatusFailed || measurement.DownloadMbps == nil || *measurement.DownloadMbps != 0 || measurement.UploadMbps == nil || *measurement.UploadMbps != 0 {
		t.Fatalf("measurement = %#v", measurement)
	}
	if measurement.Failure == nil || !strings.Contains(measurement.Failure.Message, "license") {
		t.Fatalf("failure = %#v", measurement.Failure)
	}
}

func TestStripPortHandlesHostForms(t *testing.T) {
	for input, want := range map[string]string{
		"speed.example:8080":         "speed.example",
		"[2001:db8::1]:8080":         "2001:db8::1",
		"https://speed.example:8080": "speed.example",
	} {
		if got := stripPort(input); got != want {
			t.Fatalf("stripPort(%q) = %q, want %q", input, got, want)
		}
	}
}

package campus

import (
	"os"
	"testing"

	"github.com/soundadam/pace/internal/model"
)

func TestParseResult(t *testing.T) {
	data, err := os.ReadFile("testdata/librespeed-success-ipv4.json")
	if err != nil {
		t.Fatal(err)
	}
	measurement, err := parseResult(data, model.ProviderCampus, "ipv4", HelperVersion, 21_500)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.DownloadMbps == nil || *measurement.DownloadMbps != 876.54 {
		t.Fatalf("download = %v", measurement.DownloadMbps)
	}
	if measurement.UploadMbps == nil || *measurement.UploadMbps != 345.67 {
		t.Fatalf("upload = %v", measurement.UploadMbps)
	}
	if measurement.DownloadBytes == nil || *measurement.DownloadBytes != 987654321 {
		t.Fatalf("download bytes = %v", measurement.DownloadBytes)
	}
	if measurement.UploadBytes == nil || *measurement.UploadBytes != 123456789 {
		t.Fatalf("upload bytes = %v", measurement.UploadBytes)
	}
	if measurement.ServerFQDN == nil || *measurement.ServerFQDN != "speed.nju.edu.cn" {
		t.Fatalf("server FQDN = %v", measurement.ServerFQDN)
	}
	if measurement.ClientPublicIP == nil || *measurement.ClientPublicIP != "203.0.113.42" {
		t.Fatalf("client IP = %v", measurement.ClientPublicIP)
	}
	if measurement.HelperVersion == nil || *measurement.HelperVersion != HelperVersion {
		t.Fatalf("helper version = %v", measurement.HelperVersion)
	}
}

func TestParseResultAllowsMissingClientIP(t *testing.T) {
	data, err := os.ReadFile("testdata/librespeed-success-no-client-ip.json")
	if err != nil {
		t.Fatal(err)
	}
	measurement, err := parseResult(data, model.ProviderCampus, "ipv4", HelperVersion, 21_500)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.ClientPublicIP != nil {
		t.Fatalf("client IP = %v, want nil", measurement.ClientPublicIP)
	}
	if measurement.DownloadMbps == nil || *measurement.DownloadMbps != 31.29 {
		t.Fatalf("download = %v", measurement.DownloadMbps)
	}
	if measurement.UploadMbps == nil || *measurement.UploadMbps != 3.96 {
		t.Fatalf("upload = %v", measurement.UploadMbps)
	}
}

func TestParseResultRejectsMalformedAndMultipleResults(t *testing.T) {
	tests := []struct {
		name   string
		family string
		data   string
	}{
		{name: "malformed", data: `{not-json}`},
		{name: "empty", data: `[]`},
		{name: "multiple", data: `[{},{}]`},
		{name: "negative speed", data: `[{"server":{"name":"NJU","url":"http://speed.nju.edu.cn"},"client":{"ip":"203.0.113.1"},"download":-1,"upload":2}]`},
		{name: "invalid client IP", data: `[{"server":{"name":"NJU","url":"http://speed.nju.edu.cn"},"client":{"ip":"not-an-ip"},"download":1,"upload":2}]`},
		{name: "IPv4 fallback in IPv6 result", family: "ipv6", data: `[{"server":{"name":"NJU","url":"http://speed6.nju.edu.cn"},"client":{"ip":"203.0.113.1"},"download":1,"upload":2}]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			family := test.family
			if family == "" {
				family = "ipv4"
			}
			if _, err := parseResult([]byte(test.data), model.ProviderCampus, family, HelperVersion, 1); err == nil {
				t.Fatal("parseResult() succeeded")
			}
		})
	}
}

package campus

import (
	"strings"
	"testing"

	"github.com/soundadam/soundprobe/internal/model"
	"github.com/soundadam/soundprobe/internal/provider"
)

func TestConsumeProgressLineReportsDownloadAndUpload(t *testing.T) {
	var events []provider.ProgressEvent
	request := provider.Request{Progress: func(event provider.ProgressEvent) {
		events = append(events, event)
	}}
	for _, line := range []string{
		`{"type":"progress","test":"download","elapsed_ms":1000,"bytes":6250000,"mbps":50}`,
		`{"type":"progress","test":"upload","elapsed_ms":2000,"bytes":1250000,"mbps":5}`,
	} {
		recognized, err := consumeProgressLine([]byte(line), model.ProviderNJUCampusIPv4, "speed.nju.edu.cn", request)
		if err != nil {
			t.Fatal(err)
		}
		if !recognized {
			t.Fatalf("line was not recognized: %s", line)
		}
	}
	if len(events) != 2 {
		t.Fatalf("events = %d", len(events))
	}
	if events[0].Phase != provider.ProgressDownloading || events[0].LiveMbps == nil || *events[0].LiveMbps != 50 {
		t.Fatalf("download event = %#v", events[0])
	}
	if events[1].Phase != provider.ProgressUploading || events[1].LiveMbps == nil || *events[1].LiveMbps != 5 {
		t.Fatalf("upload event = %#v", events[1])
	}
}

func TestConsumeProgressLineIgnoresOrdinaryStderrAndZeroSample(t *testing.T) {
	recognized, err := consumeProgressLine([]byte("Failed to get download speed: reset"), model.ProviderNJUCampusIPv4, "speed.nju.edu.cn", provider.Request{})
	if err != nil || recognized {
		t.Fatalf("ordinary stderr recognized=%v err=%v", recognized, err)
	}
	var event provider.ProgressEvent
	recognized, err = consumeProgressLine(
		[]byte(`{"type":"progress","test":"download","elapsed_ms":200,"bytes":0,"mbps":0}`),
		model.ProviderNJUCampusIPv4,
		"speed.nju.edu.cn",
		provider.Request{Progress: func(value provider.ProgressEvent) { event = value }},
	)
	if err != nil || !recognized {
		t.Fatalf("zero progress recognized=%v err=%v", recognized, err)
	}
	if event.Phase != provider.ProgressDownloading || event.LiveMbps != nil {
		t.Fatalf("zero event = %#v", event)
	}
}

func TestConsumeProgressLineRejectsMalformedProtocol(t *testing.T) {
	for _, line := range []string{
		`{"type":"progress","test":"download","elapsed_ms":0,"bytes":1,"mbps":1}`,
		`{"type":"progress","test":"other","elapsed_ms":1,"bytes":1,"mbps":1}`,
		`{"type":"progress","test":"download","elapsed_ms":1,"bytes":1,"mbps":1,"extra":true}`,
	} {
		recognized, err := consumeProgressLine([]byte(line), model.ProviderNJUCampusIPv4, "speed.nju.edu.cn", provider.Request{})
		if !recognized || err == nil {
			t.Fatalf("line=%s recognized=%v err=%v", line, recognized, err)
		}
	}
}

func TestScanProgressOutputSeparatesErrors(t *testing.T) {
	input := strings.NewReader("{\"type\":\"progress\",\"test\":\"download\",\"elapsed_ms\":1000,\"bytes\":6250000,\"mbps\":50}\nFailed to get download speed: reset\n")
	errorsOutput := provider.NewCappedBuffer(1024)
	var events []provider.ProgressEvent
	err := scanProgressOutput(input, errorsOutput, model.ProviderNJUCampusIPv4, "speed.nju.edu.cn", provider.Request{Progress: func(event provider.ProgressEvent) {
		events = append(events, event)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].LiveMbps == nil || *events[0].LiveMbps != 50 {
		t.Fatalf("events = %#v", events)
	}
	if errorsOutput.String() != "Failed to get download speed: reset\n" {
		t.Fatalf("errors = %q", errorsOutput.String())
	}
}

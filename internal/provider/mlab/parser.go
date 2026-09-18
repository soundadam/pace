package mlab

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"

	"github.com/soundadam/soundprobe/internal/model"
	"github.com/soundadam/soundprobe/internal/provider"
)

type eventEnvelope struct {
	Key   string          `json:"Key"`
	Value json.RawMessage `json:"Value"`
}

type eventValue struct {
	Test           string          `json:"Test"`
	Failure        string          `json:"Failure"`
	Server         string          `json:"Server"`
	Origin         string          `json:"Origin"`
	AppInfo        *appInfo        `json:"AppInfo"`
	TCPInfo        *tcpInfo        `json:"TCPInfo"`
	ConnectionInfo *connectionInfo `json:"ConnectionInfo"`
}

type appInfo struct {
	ElapsedTime int64 `json:"ElapsedTime"`
	NumBytes    int64 `json:"NumBytes"`
}

type tcpInfo struct {
	ElapsedTime   int64 `json:"ElapsedTime"`
	BytesReceived int64 `json:"BytesReceived"`
	BytesSent     int64 `json:"BytesSent"`
}

type connectionInfo struct {
	Client string `json:"Client"`
	Server string `json:"Server"`
	UUID   string `json:"UUID"`
}

type valueUnitPair struct {
	Value float64 `json:"Value"`
	Unit  string  `json:"Unit"`
}

type subtestSummary struct {
	UUID           string        `json:"UUID"`
	Throughput     valueUnitPair `json:"Throughput"`
	Latency        valueUnitPair `json:"Latency"`
	Retransmission valueUnitPair `json:"Retransmission"`
}

type ndtSummary struct {
	ServerFQDN string          `json:"ServerFQDN"`
	ServerIP   string          `json:"ServerIP"`
	ClientIP   string          `json:"ClientIP"`
	Download   *subtestSummary `json:"Download"`
	Upload     *subtestSummary `json:"Upload"`
}

type ndtFailure struct {
	test    string
	message string
}

type accumulator struct {
	summary       *ndtSummary
	failures      []ndtFailure
	serverFQDN    string
	downloadBytes int64
	uploadBytes   int64
	sawEvent      bool
}

func (accumulator *accumulator) consume(line []byte, request provider.Request) error {
	line = []byte(strings.TrimSpace(string(line)))
	if len(line) == 0 {
		return nil
	}

	var envelope eventEnvelope
	if err := json.Unmarshal(line, &envelope); err != nil {
		return fmt.Errorf("decode ndt7 JSON line: %w", err)
	}
	if envelope.Key == "" {
		var summary ndtSummary
		if err := json.Unmarshal(line, &summary); err != nil {
			return fmt.Errorf("decode ndt7 summary: %w", err)
		}
		if summary.ServerFQDN == "" && summary.Download == nil && summary.Upload == nil {
			return errors.New("ndt7 output is neither an event nor a summary")
		}
		accumulator.summary = &summary
		accumulator.sawEvent = true
		return nil
	}

	accumulator.sawEvent = true
	var value eventValue
	if len(envelope.Value) > 0 && string(envelope.Value) != "null" {
		if err := json.Unmarshal(envelope.Value, &value); err != nil {
			return fmt.Errorf("decode ndt7 %s event: %w", envelope.Key, err)
		}
	}

	switch envelope.Key {
	case "starting":
		request.Report(provider.ProgressEvent{
			Provider: model.ProviderMLab,
			Phase:    provider.ProgressConnecting,
			Test:     value.Test,
		})
	case "connected":
		if value.Server != "" {
			accumulator.serverFQDN = value.Server
		}
		phase := provider.ProgressDownloading
		if value.Test == "upload" {
			phase = provider.ProgressUploading
		}
		request.Report(provider.ProgressEvent{
			Provider: model.ProviderMLab,
			Phase:    phase,
			Test:     value.Test,
			Server:   value.Server,
		})
	case "measurement":
		rate, bytes, ok := liveMeasurement(value)
		phase := provider.ProgressDownloading
		if value.Test == "upload" {
			phase = provider.ProgressUploading
		}
		event := provider.ProgressEvent{
			Provider: model.ProviderMLab,
			Phase:    phase,
			Test:     value.Test,
		}
		if ok {
			switch value.Test {
			case "download":
				if bytes > accumulator.downloadBytes {
					accumulator.downloadBytes = bytes
					event.LiveMbps = model.Pointer(rate)
				}
			case "upload":
				if bytes > accumulator.uploadBytes {
					accumulator.uploadBytes = bytes
					event.LiveMbps = model.Pointer(rate)
				}
			}
		}
		request.Report(event)
	case "error":
		message := provider.SanitizeMessage(value.Failure, provider.MessageLimit)
		if message == "" {
			message = "M-Lab test failed"
		}
		accumulator.failures = append(accumulator.failures, ndtFailure{test: value.Test, message: message})
		request.Report(provider.ProgressEvent{
			Provider: model.ProviderMLab,
			Phase:    provider.ProgressFailed,
			Test:     value.Test,
			Message:  message,
		})
	case "complete":
		// The final summary contains the durable values. Completion events are
		// intentionally not persisted.
	default:
		// Ignore forward-compatible events while retaining strict JSON parsing.
	}
	return nil
}

func liveMeasurement(value eventValue) (float64, int64, bool) {
	switch {
	case value.Test == "download" && value.Origin == "client" && value.AppInfo != nil && value.AppInfo.ElapsedTime > 0 && value.AppInfo.NumBytes > 0:
		return 8 * float64(value.AppInfo.NumBytes) / float64(value.AppInfo.ElapsedTime), value.AppInfo.NumBytes, true
	case value.Test == "upload" && value.Origin == "server" && value.TCPInfo != nil && value.TCPInfo.ElapsedTime > 0 && value.TCPInfo.BytesReceived > 0:
		return 8 * float64(value.TCPInfo.BytesReceived) / float64(value.TCPInfo.ElapsedTime), value.TCPInfo.BytesReceived, true
	}
	return 0, 0, false
}

func (accumulator *accumulator) measurement(helperVersion string, durationMS int64) (model.Measurement, error) {
	if accumulator.summary == nil {
		if len(accumulator.failures) == 0 {
			return model.Measurement{}, errors.New("ndt7 output is missing the final summary")
		}
		return accumulator.failedMeasurement(helperVersion, durationMS, nil, nil), nil
	}

	summary := accumulator.summary
	if summary.ServerFQDN == "" {
		summary.ServerFQDN = accumulator.serverFQDN
	}
	if summary.ServerFQDN == "" {
		return model.Measurement{}, errors.New("ndt7 summary is missing server FQDN")
	}
	if summary.ClientIP != "" && net.ParseIP(summary.ClientIP) == nil {
		return model.Measurement{}, fmt.Errorf("ndt7 summary contains invalid client IP %q", summary.ClientIP)
	}
	if summary.ServerIP != "" && net.ParseIP(summary.ServerIP) == nil {
		return model.Measurement{}, fmt.Errorf("ndt7 summary contains invalid server IP %q", summary.ServerIP)
	}

	download, downloadPresent, err := summaryRate(summary.Download)
	if err != nil {
		return model.Measurement{}, fmt.Errorf("invalid ndt7 download summary: %w", err)
	}
	upload, uploadPresent, err := summaryRate(summary.Upload)
	if err != nil {
		return model.Measurement{}, fmt.Errorf("invalid ndt7 upload summary: %w", err)
	}

	if len(accumulator.failures) > 0 || !downloadPresent || !uploadPresent {
		return accumulator.failedMeasurement(helperVersion, durationMS, model.Pointer(download), model.Pointer(upload)), nil
	}

	concurrency := 1
	measurement := model.Measurement{
		Provider:      model.ProviderMLab,
		Method:        model.MethodNDT7SingleStream,
		Status:        model.ProviderStatusSuccess,
		ServerName:    model.Pointer(summary.ServerFQDN),
		ServerFQDN:    model.Pointer(summary.ServerFQDN),
		DownloadMbps:  model.Pointer(download),
		UploadMbps:    model.Pointer(upload),
		DownloadBytes: model.Pointer(accumulator.downloadBytes),
		UploadBytes:   model.Pointer(accumulator.uploadBytes),
		DurationMS:    model.Pointer(durationMS),
		Concurrency:   model.Pointer(concurrency),
		HelperVersion: model.Pointer(helperVersion),
	}
	if summary.ServerIP != "" {
		measurement.ServerAddress = model.Pointer(summary.ServerIP)
	}
	if summary.ClientIP != "" {
		measurement.ClientPublicIP = model.Pointer(summary.ClientIP)
		measurement.IPFamily = model.Pointer(ipFamily(summary.ClientIP))
	}
	return measurement, nil
}

func (accumulator *accumulator) failedMeasurement(helperVersion string, durationMS int64, download, upload *float64) model.Measurement {
	zero := 0.0
	if download == nil {
		download = model.Pointer(zero)
	}
	if upload == nil {
		upload = model.Pointer(zero)
	}
	failure := ndtFailure{message: "M-Lab result was incomplete"}
	if len(accumulator.failures) > 0 {
		failure = accumulator.failures[0]
	} else if accumulator.summary != nil && accumulator.summary.Download == nil {
		failure.test = "download"
	} else if accumulator.summary != nil && accumulator.summary.Upload == nil {
		failure.test = "upload"
	}
	stage, code := classifyFailure(failure.test, failure.message)
	concurrency := 1
	measurement := model.Measurement{
		Provider:      model.ProviderMLab,
		Method:        model.MethodNDT7SingleStream,
		Status:        model.ProviderStatusFailed,
		DownloadMbps:  download,
		UploadMbps:    upload,
		DownloadBytes: model.Pointer(accumulator.downloadBytes),
		UploadBytes:   model.Pointer(accumulator.uploadBytes),
		DurationMS:    model.Pointer(durationMS),
		Concurrency:   model.Pointer(concurrency),
		HelperVersion: model.Pointer(helperVersion),
		Failure: &model.Failure{
			Stage:   stage,
			Code:    code,
			Message: failure.message,
		},
	}
	if accumulator.summary != nil {
		if accumulator.summary.ServerFQDN != "" {
			measurement.ServerName = model.Pointer(accumulator.summary.ServerFQDN)
			measurement.ServerFQDN = model.Pointer(accumulator.summary.ServerFQDN)
		}
		if accumulator.summary.ServerIP != "" {
			measurement.ServerAddress = model.Pointer(accumulator.summary.ServerIP)
		}
		if accumulator.summary.ClientIP != "" {
			measurement.ClientPublicIP = model.Pointer(accumulator.summary.ClientIP)
			measurement.IPFamily = model.Pointer(ipFamily(accumulator.summary.ClientIP))
		}
	}
	return measurement
}

func summaryRate(summary *subtestSummary) (float64, bool, error) {
	if summary == nil {
		return 0, false, nil
	}
	if summary.Throughput.Unit != "Mbit/s" {
		return 0, false, fmt.Errorf("unexpected throughput unit %q", summary.Throughput.Unit)
	}
	value := summary.Throughput.Value
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, false, errors.New("throughput must be finite and non-negative")
	}
	return value, true, nil
}

func ipFamily(address string) string {
	ip := net.ParseIP(address)
	if ip != nil && ip.To4() == nil {
		return "ipv6"
	}
	return "ipv4"
}

func classifyFailure(test, message string) (model.FailureStage, string) {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "deadline exceeded"), strings.Contains(lower, "timeout"), strings.Contains(lower, "timed out"):
		return model.FailureStageTimeout, "timeout"
	case strings.Contains(lower, "no such host"), strings.Contains(lower, "name resolution"), strings.Contains(lower, "lookup "):
		return model.FailureStageDNS, "dns_failure"
	case test == "download":
		return model.FailureStageDownload, "download_failure"
	case test == "upload":
		return model.FailureStageUpload, "upload_failure"
	default:
		return model.FailureStageConnect, "connect_failure"
	}
}

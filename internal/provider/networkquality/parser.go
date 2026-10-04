package networkquality

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/soundadam/pace/internal/model"
)

type report struct {
	BaseRTT          *float64 `json:"base_rtt"`
	DLFlows          *int     `json:"dl_flows"`
	DLResponsiveness *float64 `json:"dl_responsiveness"`
	DLThroughput     *float64 `json:"dl_throughput"`
	ULFlows          *int     `json:"ul_flows"`
	ULResponsiveness *float64 `json:"ul_responsiveness"`
	ULThroughput     *float64 `json:"ul_throughput"`
	Responsiveness   *float64 `json:"responsiveness"`
	InterfaceName    string   `json:"interface_name"`
	IPFamily         string   `json:"ip_family"`
	OSVersion        string   `json:"os_version"`
	ErrorCode        string   `json:"error_code"`
	ErrorDomain      string   `json:"error_domain"`
	Error            string   `json:"error"`
}

func parseResult(data []byte, durationMS int64) (model.Measurement, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return model.Measurement{}, errors.New("networkQuality produced no JSON output")
	}

	var parsed report
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&parsed); err != nil {
		return model.Measurement{}, fmt.Errorf("decode networkQuality JSON: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return model.Measurement{}, err
	}
	if parsed.DLThroughput == nil && parsed.ULThroughput == nil && parsed.ErrorCode == "" && parsed.ErrorDomain == "" && parsed.Error == "" {
		return model.Measurement{}, errors.New("networkQuality JSON contains no result fields")
	}
	if parsed.ErrorCode == "" && parsed.ErrorDomain == "" && parsed.Error == "" && (parsed.DLThroughput == nil || parsed.ULThroughput == nil) {
		return model.Measurement{}, errors.New("networkQuality JSON is missing download or upload result")
	}

	measurement := model.Measurement{
		Provider:      model.ProviderApple,
		Method:        model.MethodAppleNetworkQuality,
		Status:        model.ProviderStatusSuccess,
		ServerName:    model.Pointer("Apple networkQuality"),
		DurationMS:    model.Pointer(durationMS),
		HelperVersion: model.Pointer("system"),
	}
	if parsed.OSVersion != "" {
		measurement.HelperVersion = model.Pointer(parsed.OSVersion)
	}
	measurement.PingMS = parsed.BaseRTT
	if parsed.IPFamily == "ipv4" || parsed.IPFamily == "ipv6" {
		measurement.IPFamily = model.Pointer(parsed.IPFamily)
	}
	measurement.ResponsivenessRPM = parsed.Responsiveness
	measurement.UploadResponsivenessRPM = parsed.ULResponsiveness
	measurement.DownloadResponsivenessRPM = parsed.DLResponsiveness
	if parsed.DLFlows != nil && *parsed.DLFlows > 0 {
		measurement.Concurrency = parsed.DLFlows
	}
	if parsed.DLThroughput != nil {
		value := *parsed.DLThroughput / 1_000_000
		measurement.DownloadMbps = model.Pointer(value)
	}
	if parsed.ULThroughput != nil {
		value := *parsed.ULThroughput / 1_000_000
		measurement.UploadMbps = model.Pointer(value)
	}

	if parsed.ErrorCode != "" || parsed.Error != "" || parsed.ErrorDomain != "" {
		measurement.Status = model.ProviderStatusFailed
		measurement.DownloadMbps = zeroIfNil(measurement.DownloadMbps)
		measurement.UploadMbps = zeroIfNil(measurement.UploadMbps)
		code := parsed.ErrorCode
		if code == "" {
			code = "networkquality_error"
		}
		message := strings.TrimSpace(parsed.Error)
		if message == "" {
			message = strings.TrimSpace(parsed.ErrorDomain)
		}
		if message == "" {
			message = "Apple networkQuality did not complete"
		}
		measurement.Failure = &model.Failure{Stage: model.FailureStageHelper, Code: code, Message: message}
	}
	return measurement, nil
}

func failedMeasurement(durationMS int64, code, message string) model.Measurement {
	return model.Measurement{
		Provider:      model.ProviderApple,
		Method:        model.MethodAppleNetworkQuality,
		Status:        model.ProviderStatusFailed,
		ServerName:    model.Pointer("Apple networkQuality"),
		DownloadMbps:  model.Pointer(0.0),
		UploadMbps:    model.Pointer(0.0),
		DurationMS:    model.Pointer(durationMS),
		HelperVersion: model.Pointer("system"),
		Failure:       &model.Failure{Stage: model.FailureStageHelper, Code: code, Message: message},
	}
}

func cancelledMeasurement(durationMS int64) model.Measurement {
	return model.Measurement{
		Provider:      model.ProviderApple,
		Method:        model.MethodAppleNetworkQuality,
		Status:        model.ProviderStatusCancelled,
		ServerName:    model.Pointer("Apple networkQuality"),
		DurationMS:    model.Pointer(durationMS),
		HelperVersion: model.Pointer("system"),
		Failure:       &model.Failure{Stage: model.FailureStageCancelled, Code: "cancelled", Message: "Apple networkQuality was cancelled"},
	}
}

func zeroIfNil(value *float64) *float64 {
	if value == nil {
		return model.Pointer(0.0)
	}
	return value
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("networkQuality JSON contains trailing data")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode trailing networkQuality JSON: %w", err)
	}
	return nil
}

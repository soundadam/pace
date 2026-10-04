package ookla

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/soundadam/pace/internal/model"
)

type result struct {
	Ping struct {
		Jitter  *float64 `json:"jitter"`
		Latency *float64 `json:"latency"`
	} `json:"ping"`
	Download struct {
		Bandwidth *float64 `json:"bandwidth"`
		Bytes     *int64   `json:"bytes"`
		Elapsed   *int64   `json:"elapsed"`
	} `json:"download"`
	Upload struct {
		Bandwidth *float64 `json:"bandwidth"`
		Bytes     *int64   `json:"bytes"`
		Elapsed   *int64   `json:"elapsed"`
	} `json:"upload"`
	Interface struct {
		Name       string `json:"name"`
		ExternalIP string `json:"externalIp"`
	} `json:"interface"`
	Server struct {
		ID       *int64 `json:"id"`
		Host     string `json:"host"`
		Name     string `json:"name"`
		Location string `json:"location"`
		Country  string `json:"country"`
		Sponsor  string `json:"sponsor"`
		IP       string `json:"ip"`
	} `json:"server"`
	PacketLoss *float64 `json:"packetLoss"`
	Error      string   `json:"error"`
}

func parseResult(data []byte, helperVersion string, durationMS int64) (model.Measurement, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return model.Measurement{}, errors.New("Ookla CLI produced no JSON output")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var parsed result
	if err := decoder.Decode(&parsed); err != nil {
		return model.Measurement{}, fmt.Errorf("decode Ookla JSON: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return model.Measurement{}, err
	}
	if parsed.Download.Bandwidth == nil && parsed.Upload.Bandwidth == nil && parsed.Error == "" {
		return model.Measurement{}, errors.New("Ookla JSON contains no result fields")
	}
	if parsed.Error != "" {
		code := "ookla_error"
		if lower := strings.ToLower(parsed.Error); strings.Contains(lower, "license") || strings.Contains(lower, "gdpr") {
			code = "license_required"
		}
		measurement := failedMeasurement(helperVersion, durationMS, model.FailureStageHelper, code, compact(parsed.Error))
		measurement.PingMS = parsed.Ping.Latency
		measurement.JitterMS = parsed.Ping.Jitter
		if parsed.Server.ID != nil {
			measurement.ServerID = parsed.Server.ID
		}
		if parsed.Server.Sponsor != "" {
			measurement.ServerSponsor = model.Pointer(parsed.Server.Sponsor)
		}
		if parsed.Server.Host != "" {
			if fqdn := stripPort(parsed.Server.Host); fqdn != "" {
				measurement.ServerFQDN = model.Pointer(fqdn)
			}
		}
		if parsed.Server.IP != "" {
			measurement.ServerAddress = model.Pointer(parsed.Server.IP)
		}
		if parsed.Server.Name != "" {
			measurement.ServerName = model.Pointer(parsed.Server.Name)
		}
		if parsed.Interface.ExternalIP != "" {
			measurement.ClientPublicIP = model.Pointer(parsed.Interface.ExternalIP)
			if family := ipFamily(parsed.Interface.ExternalIP); family != "" {
				measurement.IPFamily = model.Pointer(family)
			}
		}
		return measurement, nil
	}

	measurement := model.Measurement{
		Provider:      model.ProviderOokla,
		Method:        model.MethodOoklaSpeedtest,
		Status:        model.ProviderStatusSuccess,
		DurationMS:    model.Pointer(durationMS),
		HelperVersion: model.Pointer(helperVersion),
		PingMS:        parsed.Ping.Latency,
		JitterMS:      parsed.Ping.Jitter,
		DownloadBytes: parsed.Download.Bytes,
		UploadBytes:   parsed.Upload.Bytes,
	}
	if parsed.Download.Bandwidth != nil {
		measurement.DownloadMbps = model.Pointer(*parsed.Download.Bandwidth * 8 / 1_000_000)
	}
	if parsed.Upload.Bandwidth != nil {
		measurement.UploadMbps = model.Pointer(*parsed.Upload.Bandwidth * 8 / 1_000_000)
	}
	if parsed.Server.ID != nil {
		measurement.ServerID = parsed.Server.ID
	}
	serverName := parsed.Server.Name
	if serverName == "" {
		serverName = parsed.Server.Location
	}
	if serverName != "" {
		measurement.ServerName = model.Pointer(serverName)
	}
	if parsed.Server.Sponsor != "" {
		measurement.ServerSponsor = model.Pointer(parsed.Server.Sponsor)
	}
	if parsed.Server.Host != "" {
		fqdn := stripPort(parsed.Server.Host)
		if fqdn != "" {
			measurement.ServerFQDN = model.Pointer(fqdn)
		}
	}
	if parsed.Server.IP != "" {
		measurement.ServerAddress = model.Pointer(parsed.Server.IP)
	}
	if parsed.Interface.ExternalIP != "" {
		measurement.ClientPublicIP = model.Pointer(parsed.Interface.ExternalIP)
		measurement.IPFamily = model.Pointer(ipFamily(parsed.Interface.ExternalIP))
	}

	if measurement.DownloadMbps == nil || measurement.UploadMbps == nil {
		return model.Measurement{}, errors.New("Ookla JSON is missing download or upload result")
	}
	return measurement, nil
}

func failedMeasurement(helperVersion string, durationMS int64, stage model.FailureStage, code, message string) model.Measurement {
	return model.Measurement{
		Provider:      model.ProviderOokla,
		Method:        model.MethodOoklaSpeedtest,
		Status:        model.ProviderStatusFailed,
		DownloadMbps:  model.Pointer(0.0),
		UploadMbps:    model.Pointer(0.0),
		DurationMS:    model.Pointer(durationMS),
		HelperVersion: model.Pointer(helperVersion),
		Failure:       &model.Failure{Stage: stage, Code: code, Message: message},
	}
}

func cancelledMeasurement(helperVersion string, durationMS int64) model.Measurement {
	return model.Measurement{
		Provider:      model.ProviderOokla,
		Method:        model.MethodOoklaSpeedtest,
		Status:        model.ProviderStatusCancelled,
		DurationMS:    model.Pointer(durationMS),
		HelperVersion: model.Pointer(helperVersion),
		Failure:       &model.Failure{Stage: model.FailureStageCancelled, Code: "cancelled", Message: "Ookla Speedtest was cancelled"},
	}
}

func zeroIfNil(value *float64) *float64 {
	if value == nil {
		return model.Pointer(0.0)
	}
	return value
}

func stripPort(host string) string {
	host = strings.TrimSpace(host)
	if strings.Contains(host, "://") {
		host = strings.TrimPrefix(host, "http://")
		host = strings.TrimPrefix(host, "https://")
	}
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		return strings.Trim(parsed, "[]")
	}
	if strings.Count(host, ":") == 1 {
		parts := strings.SplitN(host, ":", 2)
		if _, err := strconv.Atoi(parts[1]); err == nil {
			return parts[0]
		}
	}
	return strings.Trim(host, "[]")
}

func ipFamily(value string) string {
	if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil {
		if ip.To4() != nil {
			return "ipv4"
		}
		return "ipv6"
	}
	return ""
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("Ookla JSON contains trailing data")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode trailing Ookla JSON: %w", err)
	}
	return nil
}

func compact(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 240 {
		return value[:240]
	}
	return value
}

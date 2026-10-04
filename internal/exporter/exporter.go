package exporter

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/soundadam/pace/internal/model"
)

func Write(path, format string, summaries []model.RunSummary) error {
	if path == "" {
		return errors.New("output path is required")
	}
	if format != "jsonl" && format != "csv" {
		return fmt.Errorf("unsupported export format %q", format)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open export output: %w", err)
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("set export output mode: %w", err)
	}

	switch format {
	case "jsonl":
		encoder := json.NewEncoder(file)
		for _, summary := range summaries {
			if err := encoder.Encode(summary); err != nil {
				return fmt.Errorf("write JSONL export: %w", err)
			}
		}
	case "csv":
		writer := csv.NewWriter(file)
		header := []string{
			"run_id", "started_at", "ended_at", "command", "run_status", "label", "note",
			"target", "method", "measurement_status", "ip_family", "server_name", "server_fqdn",
			"ping_ms", "jitter_ms", "download_mbps", "upload_mbps", "download_bytes", "upload_bytes",
			"duration_ms", "concurrency", "helper_version", "failure_stage", "failure_code", "failure_message",
			"server_id", "server_sponsor", "responsiveness_rpm", "upload_responsiveness_rpm", "download_responsiveness_rpm",
		}
		if err := writer.Write(header); err != nil {
			return fmt.Errorf("write CSV header: %w", err)
		}
		for _, summary := range summaries {
			for _, measurement := range summary.Measurements {
				failureStage, failureCode, failureMessage := "", "", ""
				if measurement.Failure != nil {
					failureStage = string(measurement.Failure.Stage)
					failureCode = measurement.Failure.Code
					failureMessage = measurement.Failure.Message
				}
				row := []string{
					summary.RunID,
					summary.StartedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
					summary.EndedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
					string(summary.Command),
					string(summary.Status),
					stringValue(summary.Label),
					stringValue(summary.Note),
					string(measurement.Provider),
					measurement.Method,
					string(measurement.Status),
					stringValue(measurement.IPFamily),
					stringValue(measurement.ServerName),
					stringValue(measurement.ServerFQDN),
					floatValue(measurement.PingMS),
					floatValue(measurement.JitterMS),
					floatValue(measurement.DownloadMbps),
					floatValue(measurement.UploadMbps),
					int64Value(measurement.DownloadBytes),
					int64Value(measurement.UploadBytes),
					int64Value(measurement.DurationMS),
					intValue(measurement.Concurrency),
					stringValue(measurement.HelperVersion),
					failureStage,
					failureCode,
					failureMessage,
					int64Value(measurement.ServerID),
					stringValue(measurement.ServerSponsor),
					floatValue(measurement.ResponsivenessRPM),
					floatValue(measurement.UploadResponsivenessRPM),
					floatValue(measurement.DownloadResponsivenessRPM),
				}
				if err := writer.Write(row); err != nil {
					return fmt.Errorf("write CSV row: %w", err)
				}
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return fmt.Errorf("flush CSV export: %w", err)
		}
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync export output: %w", err)
	}
	return nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func floatValue(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}

func int64Value(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func intValue(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

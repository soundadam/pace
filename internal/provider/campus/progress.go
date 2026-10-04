package campus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"

	"github.com/soundadam/pace/internal/model"
	"github.com/soundadam/pace/internal/provider"
)

const libreSpeedProgressType = "progress"

type libreSpeedProgress struct {
	Type      string  `json:"type"`
	Test      string  `json:"test"`
	ElapsedMS int64   `json:"elapsed_ms"`
	Bytes     uint64  `json:"bytes"`
	Mbps      float64 `json:"mbps"`
}

func consumeProgressLine(line []byte, measurementProvider model.Provider, server string, request provider.Request) (bool, error) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return true, nil
	}

	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil || envelope.Type != libreSpeedProgressType {
		return false, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	var progress libreSpeedProgress
	if err := decoder.Decode(&progress); err != nil {
		return true, fmt.Errorf("decode progress JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return true, err
	}
	if progress.ElapsedMS <= 0 {
		return true, fmt.Errorf("progress elapsed_ms must be positive")
	}
	if math.IsNaN(progress.Mbps) || math.IsInf(progress.Mbps, 0) || progress.Mbps < 0 {
		return true, fmt.Errorf("progress mbps must be finite and non-negative")
	}

	phase := provider.ProgressDownloading
	switch progress.Test {
	case "download":
	case "upload":
		phase = provider.ProgressUploading
	default:
		return true, fmt.Errorf("unsupported progress test %q", progress.Test)
	}
	event := provider.ProgressEvent{
		Provider: measurementProvider,
		Phase:    phase,
		Test:     progress.Test,
		Server:   server,
	}
	if progress.Bytes > 0 && progress.Mbps > 0 {
		event.LiveMbps = model.Pointer(progress.Mbps)
	}
	request.Report(event)
	return true, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("progress JSON contains trailing data")
		}
		return fmt.Errorf("decode trailing progress JSON: %w", err)
	}
	return nil
}

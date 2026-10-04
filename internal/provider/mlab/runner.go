package mlab

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/soundadam/pace/internal/helper"
	"github.com/soundadam/pace/internal/model"
	"github.com/soundadam/pace/internal/provider"
)

const (
	HelperName            = "ndt7-client"
	HelperVersion         = "v0.10.1"
	ClientName            = "pace"
	HelperInternalTimeout = 55 * time.Second
)

type HelperResolver interface {
	Resolve(string) (helper.Resolved, error)
}

type Runner struct {
	Resolver HelperResolver
	Timeout  time.Duration
	Now      func() time.Time

	prepareMu sync.Mutex
	prepared  *preparedHelper
}

type preparedHelper struct {
	resolved helper.Resolved
	version  string
}

func New(resolver HelperResolver) *Runner {
	return &Runner{Resolver: resolver}
}

func (runner *Runner) Preflight(ctx context.Context, _ provider.Request) error {
	runner.setDefaults()
	if err := ctx.Err(); err != nil {
		return err
	}
	_, _, err := runner.prepareHelper()
	return err
}

func (runner *Runner) Measure(ctx context.Context, request provider.Request) (model.Measurement, error) {
	runner.setDefaults()
	resolved, helperVersion, err := runner.prepareHelper()
	if err != nil {
		return model.Measurement{}, err
	}

	measurementCtx, cancel := context.WithTimeout(ctx, runner.Timeout)
	defer cancel()
	startedAt := runner.Now()
	args := []string{
		"-format=json",
		"-client-name=" + ClientName,
		"-timeout=" + HelperInternalTimeout.String(),
		"-scheme=wss",
		"-download=true",
		"-upload=true",
	}
	command := exec.CommandContext(measurementCtx, resolved.Path, args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return model.Measurement{}, fmt.Errorf("open ndt7 stdout: %w", err)
	}
	stderr := newCappedBuffer(32 * 1024)
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return model.Measurement{}, fmt.Errorf("start ndt7 helper: %w", err)
	}

	var parsed accumulator
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if err := parsed.consume(scanner.Bytes(), request); err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return model.Measurement{}, fmt.Errorf("parse ndt7 helper output: %w", err)
		}
	}
	scanErr := scanner.Err()
	waitErr := command.Wait()
	durationMS := runner.Now().Sub(startedAt).Milliseconds()
	if durationMS < 0 {
		durationMS = 0
	}

	if errors.Is(measurementCtx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return cancelledMeasurement(helperVersion, durationMS), nil
	}
	if errors.Is(measurementCtx.Err(), context.DeadlineExceeded) {
		return failedMeasurement(helperVersion, durationMS, model.FailureStageTimeout, "timeout", "M-Lab measurement timed out"), nil
	}
	if scanErr != nil {
		return model.Measurement{}, fmt.Errorf("read ndt7 helper output: %w", scanErr)
	}

	measurement, parseErr := parsed.measurement(helperVersion, durationMS)
	if waitErr != nil {
		if parseErr == nil && measurement.Status == model.ProviderStatusFailed {
			return measurement, nil
		}
		message := sanitizeFailure(stderr.String())
		if message == "" {
			message = "ndt7 helper exited without a complete result"
		}
		return model.Measurement{}, fmt.Errorf("ndt7 helper failed: %s", message)
	}
	if parseErr != nil {
		return model.Measurement{}, fmt.Errorf("parse ndt7 helper output: %w", parseErr)
	}
	return measurement, nil
}

func (runner *Runner) prepareHelper() (helper.Resolved, string, error) {
	runner.prepareMu.Lock()
	defer runner.prepareMu.Unlock()
	if runner.prepared != nil {
		return runner.prepared.resolved, runner.prepared.version, nil
	}
	if runner.Resolver == nil {
		return helper.Resolved{}, "", fmt.Errorf("%w: ndt7 helper resolver is not configured", provider.ErrUnavailable)
	}
	resolved, err := runner.Resolver.Resolve(HelperName)
	if err != nil {
		return helper.Resolved{}, "", fmt.Errorf("%w: resolve ndt7 helper: %v", provider.ErrUnavailable, err)
	}
	version, err := helper.ReadVersionManifest(resolved.Path)
	if err != nil {
		return helper.Resolved{}, "", fmt.Errorf("%w: %v", provider.ErrUnavailable, err)
	}
	if version != HelperVersion {
		return helper.Resolved{}, "", fmt.Errorf("%w: ndt7 helper version %s does not match required %s", provider.ErrUnavailable, version, HelperVersion)
	}
	runner.prepared = &preparedHelper{resolved: resolved, version: version}
	return resolved, version, nil
}

func (runner *Runner) setDefaults() {
	if runner.Timeout <= 0 {
		runner.Timeout = 60 * time.Second
	}
	if runner.Now == nil {
		runner.Now = time.Now
	}
}

func failedMeasurement(version string, durationMS int64, stage model.FailureStage, code, message string) model.Measurement {
	zero := 0.0
	concurrency := 1
	return model.Measurement{
		Provider:      model.ProviderMLab,
		Method:        model.MethodNDT7SingleStream,
		Status:        model.ProviderStatusFailed,
		DownloadMbps:  model.Pointer(zero),
		UploadMbps:    model.Pointer(zero),
		DownloadBytes: model.Pointer(int64(0)),
		UploadBytes:   model.Pointer(int64(0)),
		DurationMS:    model.Pointer(durationMS),
		Concurrency:   model.Pointer(concurrency),
		HelperVersion: model.Pointer(version),
		Failure: &model.Failure{
			Stage:   stage,
			Code:    code,
			Message: message,
		},
	}
}

func cancelledMeasurement(version string, durationMS int64) model.Measurement {
	concurrency := 1
	return model.Measurement{
		Provider:      model.ProviderMLab,
		Method:        model.MethodNDT7SingleStream,
		Status:        model.ProviderStatusCancelled,
		DurationMS:    model.Pointer(durationMS),
		Concurrency:   model.Pointer(concurrency),
		HelperVersion: model.Pointer(version),
		Failure: &model.Failure{
			Stage:   model.FailureStageCancelled,
			Code:    "cancelled",
			Message: "M-Lab measurement was cancelled",
		},
	}
}

type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit}
}

func (buffer *cappedBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = buffer.buffer.Write(data)
	}
	return originalLength, nil
}

func (buffer *cappedBuffer) String() string {
	return strings.TrimSpace(buffer.buffer.String())
}

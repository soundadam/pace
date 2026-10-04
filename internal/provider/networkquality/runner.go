package networkquality

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/soundadam/pace/internal/model"
	"github.com/soundadam/pace/internal/provider"
)

const (
	HelperName     = "networkQuality"
	DefaultPath    = "/usr/bin/networkQuality"
	DefaultTimeout = 70 * time.Second
)

type Runner struct {
	Path    string
	Timeout time.Duration
	Now     func() time.Time
}

func New() *Runner {
	path := strings.TrimSpace(os.Getenv("PACE_NETWORKQUALITY_PATH"))
	if path == "" && runtime.GOOS == "darwin" {
		path = DefaultPath
	}
	return &Runner{Path: path}
}

func (runner *Runner) Preflight(ctx context.Context, _ provider.Request) error {
	runner.setDefaults()
	path, err := runner.resolvePath()
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, path, "-h")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: run %s -h: %v", provider.ErrUnavailable, HelperName, err)
	}
	if !strings.Contains(strings.ToLower(string(output)), "networkquality") {
		return fmt.Errorf("%w: %s is not Apple networkQuality", provider.ErrUnavailable, path)
	}
	return nil
}

func (runner *Runner) Measure(ctx context.Context, request provider.Request) (model.Measurement, error) {
	runner.setDefaults()
	path, err := runner.resolvePath()
	if err != nil {
		return model.Measurement{}, err
	}

	measurementCtx, cancel := context.WithTimeout(ctx, runner.Timeout)
	defer cancel()
	startedAt := runner.Now()
	args := []string{"-c", "-s"}
	if request.Network != nil && request.Network.ActiveInterface != nil && strings.TrimSpace(*request.Network.ActiveInterface) != "" {
		args = append(args, "-I", strings.TrimSpace(*request.Network.ActiveInterface))
	}
	request.Report(provider.ProgressEvent{Provider: model.ProviderApple, Phase: provider.ProgressMeasuring})

	command := exec.CommandContext(measurementCtx, path, args...)
	var stdout, stderr cappedBuffer
	stdout.Limit = 2 * 1024 * 1024
	stderr.Limit = 32 * 1024
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		durationMS := elapsedMS(startedAt, runner.Now())
		if errors.Is(measurementCtx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return cancelledMeasurement(durationMS), nil
		}
		if errors.Is(measurementCtx.Err(), context.DeadlineExceeded) {
			return timeoutMeasurement(durationMS), nil
		}
		if stdout.Len() > 0 {
			if measurement, parseErr := parseResult(stdout.Bytes(), durationMS); parseErr == nil {
				return measurement, nil
			}
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = "Apple networkQuality exited without a result"
		}
		return failedMeasurement(durationMS, "helper_exit", compact(message)), nil
	}

	durationMS := elapsedMS(startedAt, runner.Now())
	measurement, parseErr := parseResult(stdout.Bytes(), durationMS)
	if parseErr != nil {
		message := parseErr.Error()
		if stderr.String() != "" {
			message = compact(strings.TrimSpace(stderr.String()))
		}
		return failedMeasurement(durationMS, "invalid_output", message), nil
	}
	return measurement, nil
}

func (runner *Runner) resolvePath() (string, error) {
	path := strings.TrimSpace(runner.Path)
	if path == "" {
		if runtime.GOOS != "darwin" {
			return "", fmt.Errorf("%w: Apple networkQuality is available on macOS only", provider.ErrUnavailable)
		}
		path = DefaultPath
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: %s was not found at %s", provider.ErrUnavailable, HelperName, path)
	}
	if err != nil {
		return "", fmt.Errorf("%w: inspect %s: %v", provider.ErrUnavailable, path, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%w: %s is not executable", provider.ErrUnavailable, path)
	}
	return path, nil
}

func (runner *Runner) setDefaults() {
	if runner.Timeout <= 0 {
		runner.Timeout = DefaultTimeout
	}
	if runner.Now == nil {
		runner.Now = time.Now
	}
}

func elapsedMS(start, end time.Time) int64 {
	duration := end.Sub(start).Milliseconds()
	if duration < 0 {
		return 0
	}
	return duration
}

func timeoutMeasurement(durationMS int64) model.Measurement {
	measurement := failedMeasurement(durationMS, "timeout", "Apple networkQuality timed out")
	measurement.Failure.Stage = model.FailureStageTimeout
	return measurement
}

func compact(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 240 {
		return value[:240]
	}
	return value
}

type cappedBuffer struct {
	bytes.Buffer
	Limit int
}

func (buffer *cappedBuffer) Write(data []byte) (int, error) {
	if buffer.Limit <= 0 {
		return len(data), nil
	}
	remaining := buffer.Limit - buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			_, _ = buffer.Buffer.Write(data[:remaining])
		} else {
			_, _ = buffer.Buffer.Write(data)
		}
	}
	return len(data), nil
}

package mlab

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/soundadam/pace/internal/helper"
	"github.com/soundadam/pace/internal/model"
	"github.com/soundadam/pace/internal/provider"
)

func TestRunnerMeasuresWithExactArguments(t *testing.T) {
	runner, argsPath := newFakeRunner(t, "testdata/ndt7-success.jsonl", HelperVersion, 0)
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandMLab})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Status != model.ProviderStatusSuccess {
		t.Fatalf("status = %q", measurement.Status)
	}
	got := readArgs(t, argsPath)
	want := []string{
		"-format=json",
		"-client-name=pace",
		"-timeout=55s",
		"-scheme=wss",
		"-download=true",
		"-upload=true",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestRunnerRejectsWrongVersionManifest(t *testing.T) {
	runner, _ := newFakeRunner(t, "testdata/ndt7-success.jsonl", "v0.10.0", 0)
	_, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandMLab})
	if !errors.Is(err, provider.ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestRunnerReturnsProviderFailureFromNonzeroHelper(t *testing.T) {
	runner, _ := newFakeRunner(t, "testdata/ndt7-upload-failure.jsonl", HelperVersion, 1)
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandMLab})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Status != model.ProviderStatusFailed || measurement.Failure == nil {
		t.Fatalf("measurement = %#v", measurement)
	}
}

func TestRunnerTimesOut(t *testing.T) {
	runner, _ := newFakeRunner(t, "", HelperVersion, 0)
	t.Setenv("PACE_FAKE_SLEEP", "1")
	runner.Timeout = 25 * time.Millisecond
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandMLab})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageTimeout {
		t.Fatalf("failure = %#v", measurement.Failure)
	}
}

func TestRunnerPreservesCancellation(t *testing.T) {
	runner, _ := newFakeRunner(t, "", HelperVersion, 0)
	t.Setenv("PACE_FAKE_SLEEP", "5")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel()
	}()
	measurement, err := runner.Measure(ctx, provider.Request{Command: model.CommandMLab})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Status != model.ProviderStatusCancelled || measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageCancelled {
		t.Fatalf("measurement = %#v", measurement)
	}
}

func newFakeRunner(t *testing.T, fixture, version string, exitCode int) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	executable := filepath.Join(root, "prefix", "bin", "pace")
	helperPath := filepath.Join(root, "prefix", "libexec", "pace", HelperName)
	argsPath := filepath.Join(root, "args.txt")
	writeExecutable(t, executable, "#!/bin/sh\nexit 0\n")

	fixturePath := ""
	if fixture != "" {
		absolute, err := filepath.Abs(fixture)
		if err != nil {
			t.Fatal(err)
		}
		fixturePath = absolute
	}
	script := `#!/bin/sh
printf '%%s\n' "$@" > "$PACE_FAKE_ARGS"
if [ -n "${PACE_FAKE_SLEEP:-}" ]; then
  exec sleep "$PACE_FAKE_SLEEP"
fi
if [ -n "%s" ]; then
  cat "%s"
fi
exit %d
`
	script = fmt.Sprintf(script, fixturePath, fixturePath, exitCode)
	writeExecutable(t, helperPath, script)
	if err := os.WriteFile(helperPath+".version", []byte(version+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACE_FAKE_ARGS", argsPath)

	clock := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	return &Runner{
		Resolver: helper.Resolver{
			ExecutablePath:   executable,
			WorkingDirectory: root,
			LookupPath: func(string) (string, error) {
				return "", errors.New("not found")
			},
		},
		Now: func() time.Time {
			clock = clock.Add(250 * time.Millisecond)
			return clock
		},
	}, argsPath
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func readArgs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}

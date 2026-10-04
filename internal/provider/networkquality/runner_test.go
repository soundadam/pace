package networkquality

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/soundadam/pace/internal/model"
	"github.com/soundadam/pace/internal/provider"
)

func TestRunnerPreflightAndMeasureBindInterface(t *testing.T) {
	root := t.TempDir()
	argsPath := filepath.Join(root, "args")
	fixture, err := os.ReadFile(filepath.Join("testdata", "success.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "networkQuality")
	script := "#!/bin/sh\n" +
		"if [ \"${1:-}\" = \"-h\" ]; then echo 'networkQuality help'; exit 0; fi\n" +
		"printf '%s\\n' \"$*\" > \"$PACE_ARGS\"\n" +
		"cat \"$PACE_FIXTURE\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(root, "success.json")
	if err := os.WriteFile(fixturePath, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACE_ARGS", argsPath)
	t.Setenv("PACE_FIXTURE", fixturePath)
	runner := &Runner{Path: path}
	if err := runner.Preflight(context.Background(), provider.Request{}); err != nil {
		t.Fatal(err)
	}
	interfaceName := "utun6"
	measurement, err := runner.Measure(context.Background(), provider.Request{Network: &model.NetworkContext{ActiveInterface: &interfaceName}})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Status != model.ProviderStatusSuccess {
		t.Fatalf("status = %q", measurement.Status)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(args)
	for _, expected := range []string{"-c", "-s", "-I utun6"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("args %q missing %q", text, expected)
		}
	}
}

func TestRunnerRejectsNonAppleHelper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "networkQuality")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho python-helper\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Path: path}
	if err := runner.Preflight(context.Background(), provider.Request{}); err == nil || !strings.Contains(err.Error(), "not Apple networkQuality") {
		t.Fatalf("preflight error = %v", err)
	}
}

func TestRunnerMapsTimeoutToStableFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "networkQuality")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Path: path, Timeout: 20 * time.Millisecond}
	measurement, err := runner.Measure(context.Background(), provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Status != model.ProviderStatusFailed || measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageTimeout {
		t.Fatalf("measurement = %#v", measurement)
	}
}

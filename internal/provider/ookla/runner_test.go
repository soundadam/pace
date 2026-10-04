package ookla

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soundadam/pace/internal/model"
	"github.com/soundadam/pace/internal/provider"
)

func TestRunnerAcceptsOfficialCLIAndDoesNotAutoAcceptTerms(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "speedtest")
	argsPath := filepath.Join(root, "args")
	fixturePath := filepath.Join(root, "success.json")
	fixture, err := os.ReadFile(filepath.Join("testdata", "success.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"if [ \"${1:-}\" = \"--version\" ]; then echo 'Speedtest by Ookla 1.2.0.84'; exit 0; fi\n" +
		"printf '%s\\n' \"$*\" > \"$PACE_ARGS\"\n" +
		"cat \"$PACE_FIXTURE\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
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
	if !strings.Contains(text, "--format=json") || !strings.Contains(text, "--interface=utun6") {
		t.Fatalf("args = %q", text)
	}
	if strings.Contains(text, "accept-license") || strings.Contains(text, "accept-gdpr") {
		t.Fatalf("runner unexpectedly accepted terms: %q", text)
	}
}

func TestRunnerRejectsPythonSpeedtestCLI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "speedtest")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo 'speedtest-cli 2.1.3'; fi\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Path: path}
	if err := runner.Preflight(context.Background(), provider.Request{}); err == nil || !strings.Contains(err.Error(), "official Ookla") {
		t.Fatalf("preflight error = %v", err)
	}
}

func TestRunnerSkipsPythonCandidateAndUsesOfficialCandidate(t *testing.T) {
	root := t.TempDir()
	pythonPath := filepath.Join(root, "python-speedtest")
	officialPath := filepath.Join(root, "official-speedtest")
	pythonScript := "#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo 'speedtest-cli 2.1.4'; fi\n"
	officialScript := "#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo 'Speedtest by Ookla 1.2.0'; fi\n"
	if err := os.WriteFile(pythonPath, []byte(pythonScript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(officialPath, []byte(officialScript), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{LookupAllPath: func(string) ([]string, error) {
		return []string{pythonPath, officialPath}, nil
	}}
	if err := runner.Preflight(context.Background(), provider.Request{}); err != nil {
		t.Fatalf("preflight error = %v", err)
	}
	path, version, err := runner.resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if path != officialPath || version != "1.2.0" {
		t.Fatalf("resolved = (%q, %q), want (%q, %q)", path, version, officialPath, "1.2.0")
	}
}

func TestRunnerUsesExplicitOoklaPathOverride(t *testing.T) {
	root := t.TempDir()
	officialPath := filepath.Join(root, "official-speedtest")
	if err := os.WriteFile(officialPath, []byte("#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo 'Speedtest by Ookla 1.2.0'; fi\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACE_OOKLA_PATH", officialPath)
	runner := &Runner{LookupAllPath: func(string) ([]string, error) {
		return nil, os.ErrNotExist
	}}
	if err := runner.Preflight(context.Background(), provider.Request{}); err != nil {
		t.Fatalf("preflight error = %v", err)
	}
}

func TestRunnerReportsMissingOptionalHelper(t *testing.T) {
	runner := &Runner{LookupPath: func(string) (string, error) {
		return "", os.ErrNotExist
	}}
	if err := runner.Preflight(context.Background(), provider.Request{}); err == nil || !strings.Contains(err.Error(), "official Ookla CLI") {
		t.Fatalf("preflight error = %v", err)
	}
}

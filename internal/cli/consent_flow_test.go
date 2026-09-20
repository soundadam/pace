package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soundadam/soundprobe/internal/consent"
	"github.com/soundadam/soundprobe/internal/model"
	"github.com/soundadam/soundprobe/internal/preferences"
	"github.com/soundadam/soundprobe/internal/provider"
	"github.com/soundadam/soundprobe/internal/target"
)

// runCountingRunner distinguishes "Preflight ran" from "the measurement ran".
// Preflight deliberately happens before the consent prompt, so fakeRunner's
// recorded request is set either way.
type runCountingRunner struct {
	fakeRunner
	runs int
}

func (runner *runCountingRunner) Run(ctx context.Context, request provider.Request) (model.RunSummary, error) {
	runner.runs++
	return runner.fakeRunner.Run(ctx, request)
}

// M-Lab publishes measurement data, so consent must be recorded before the
// run starts and must never be inferred.  These tests pin the three answers a
// terminal can give: accept, anything else, and end-of-input.

func TestMLabRunPromptsForConsentAndProceedsOnAccept(t *testing.T) {
	runner := &fakeRunner{summary: successfulSummary(model.CommandMLab)}
	app, stdout, stderr := newTestApp(t, runner)
	app.In = strings.NewReader("accept\n")
	if exitCode := app.Execute(context.Background(), []string{"mlab", "--no-save"}); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	record, accepted, err := app.Consent.Status()
	if err != nil || !accepted {
		t.Fatalf("Status() = %t, %v; want the run to have recorded consent", accepted, err)
	}
	if record.PolicyVersion != consent.PolicyVersion {
		t.Fatalf("recorded policy = %q", record.PolicyVersion)
	}
	// The disclosure the user consents to has to be on screen before the
	// prompt, not buried in the docs.
	for _, want := range []string{
		"M-Lab measurement consent",
		"public IP address",
		"publishes and retains experiment data indefinitely",
		consent.PolicyURL,
		"Type accept to continue:",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, missing %q", stdout.String(), want)
		}
	}
}

func TestMLabRunStopsWhenConsentIsNotGiven(t *testing.T) {
	for _, test := range []struct {
		name  string
		stdin string
	}{
		{name: "declined", stdin: "no\n"},
		{name: "accepted with different case", stdin: "ACCEPT\n"},
		{name: "end of input", stdin: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &runCountingRunner{fakeRunner: fakeRunner{summary: successfulSummary(model.CommandMLab)}}
			app, _, stderr := newTestApp(t, runner)
			app.In = strings.NewReader(test.stdin)
			if exitCode := app.Execute(context.Background(), []string{"mlab", "--no-save"}); exitCode != 1 {
				t.Fatalf("exit code = %d, want 1", exitCode)
			}
			if !strings.Contains(stderr.String(), "consent was not accepted") {
				t.Fatalf("stderr = %q", stderr.String())
			}
			if _, accepted, _ := app.Consent.Status(); accepted {
				t.Fatal("consent was recorded although it was not given")
			}
			if runner.runs != 0 {
				t.Fatal("the measurement ran without consent")
			}
		})
	}
}

func TestConsentAcceptRequiresATerminal(t *testing.T) {
	app, _, stderr := newTestApp(t, &fakeRunner{})
	app.StdinTTY = false
	app.In = strings.NewReader("accept\n")
	if exitCode := app.Execute(context.Background(), []string{"consent", "accept"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "requires an interactive terminal") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, accepted, _ := app.Consent.Status(); accepted {
		t.Fatal("consent was recorded from a pipe")
	}
}

func TestConsentAcceptReportsAWriteFailure(t *testing.T) {
	app, _, stderr := newTestApp(t, &fakeRunner{})
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	app.Consent = consent.New(filepath.Join(blocker, "consent.json"))
	app.In = strings.NewReader("accept\n")
	if exitCode := app.Execute(context.Background(), []string{"consent", "accept"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "consent directory") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestConsentRevokeReportsAFailure(t *testing.T) {
	app, stdout, _ := newTestApp(t, &fakeRunner{})
	// A non-empty directory where the consent file should be: Remove fails.
	directory := filepath.Join(t.TempDir(), "consent.json")
	if err := os.MkdirAll(filepath.Join(directory, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	app.Consent = consent.New(directory)
	if exitCode := app.Execute(context.Background(), []string{"consent", "revoke", "--json"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if code := decodeJSONError(t, stdout).Error.Code; code != "consent_error" {
		t.Fatalf("error code = %q", code)
	}
}

// --- history storage failures ------------------------------------------

// A history directory the process cannot enumerate is a real failure, not an
// empty list: reporting zero runs would look like the user's data vanished.
func TestHistoryCommandsReportStorageFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "history", args: []string{"history", "--json"}},
		{name: "last", args: []string{"last", "--json"}},
		{name: "export", args: []string{"export", "--format", "csv", "--output", "/dev/null", "--json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, stdout, _ := newTestApp(t, &fakeRunner{})
			// A regular file where the history directory belongs.
			if err := os.WriteFile(app.History.HistoryDir, []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			if exitCode := app.Execute(context.Background(), test.args); exitCode != 1 {
				t.Fatalf("exit code = %d, want 1", exitCode)
			}
			if code := decodeJSONError(t, stdout).Error.Code; code != "storage_error" {
				t.Fatalf("error code = %q", code)
			}
		})
	}
}

func TestHistoryRejectsANegativeLimit(t *testing.T) {
	app, stdout, _ := newTestApp(t, &fakeRunner{})
	if exitCode := app.Execute(context.Background(), []string{"history", "--limit=-1", "--json"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if code := decodeJSONError(t, stdout).Error.Code; code != "storage_error" {
		t.Fatalf("error code = %q", code)
	}
}

func TestShowWithoutAHistoryStoreFails(t *testing.T) {
	app, stdout, _ := newTestApp(t, &fakeRunner{})
	app.History = nil
	if exitCode := app.Execute(context.Background(), []string{"show", "any-id", "--json"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if code := decodeJSONError(t, stdout).Error.Code; code != "storage_error" {
		t.Fatalf("error code = %q", code)
	}
}

// --- option plumbing ----------------------------------------------------

// --label and --note must reach the runner verbatim, and must stay nil when
// unset so the saved summary records null rather than an empty string.
func TestLabelAndNoteReachTheRunner(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		runner := &fakeRunner{summary: successfulSummary(model.CommandCampus)}
		app, _, stderr := newTestApp(t, runner)
		args := []string{"campus", "--label", "dorm-wifi", "--note", "after maintenance", "--no-save"}
		if exitCode := app.Execute(context.Background(), args); exitCode != 0 {
			t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
		}
		if runner.request.Label == nil || *runner.request.Label != "dorm-wifi" {
			t.Fatalf("label = %v", runner.request.Label)
		}
		if runner.request.Note == nil || *runner.request.Note != "after maintenance" {
			t.Fatalf("note = %v", runner.request.Note)
		}
	})
	t.Run("unset", func(t *testing.T) {
		runner := &fakeRunner{summary: successfulSummary(model.CommandCampus)}
		app, _, stderr := newTestApp(t, runner)
		if exitCode := app.Execute(context.Background(), []string{"campus", "--no-save"}); exitCode != 0 {
			t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
		}
		if runner.request.Label != nil || runner.request.Note != nil {
			t.Fatalf("label = %v, note = %v; want both nil", runner.request.Label, runner.request.Note)
		}
	})
}

// The --ipv4/--ipv6 shortcuts have to resolve to a family before the plan is
// built, otherwise `campus --ipv6` quietly measures the IPv4 service.
func TestAddressFamilyShortcutsSelectTheRightProvider(t *testing.T) {
	for _, test := range []struct {
		args []string
		want model.Provider
	}{
		{args: []string{"campus", "--no-save"}, want: model.ProviderNJUCampusIPv4},
		{args: []string{"campus", "--ipv4", "--no-save"}, want: model.ProviderNJUCampusIPv4},
		{args: []string{"campus", "--ipv6", "--no-save"}, want: model.ProviderNJUCampusIPv6},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			runner := &fakeRunner{summary: summaryForProviders(model.CommandCampus, []model.Provider{test.want})}
			app, _, stderr := newTestApp(t, runner)
			if exitCode := app.Execute(context.Background(), test.args); exitCode != 0 {
				t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
			}
			if len(runner.request.Targets) != 1 || runner.request.Targets[0] != test.want {
				t.Fatalf("targets = %#v, want [%s]", runner.request.Targets, test.want)
			}
		})
	}
}

// --- Ookla repair -------------------------------------------------------

// Without Homebrew soundprobe must not offer to run anything: it points at the
// official download and stops.
func TestOoklaRepairRunsNothingWithoutHomebrew(t *testing.T) {
	runner := &repairableFakeRunner{fakeRunner: fakeRunner{summary: summaryForProviders(model.CommandOokla, []model.Provider{model.ProviderOokla})}}
	app, stdout, _ := newTestApp(t, runner)
	app.StdoutTTY = true
	app.LookupCommand = func(string) (string, error) { return "", fmt.Errorf("executable file not found") }
	called := false
	app.RunCommand = func(context.Context, string, []string, io.Writer, io.Writer) error {
		called = true
		return nil
	}
	if exitCode := app.Execute(context.Background(), []string{"ookla", "--no-save"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if called {
		t.Fatal("an install command ran on a machine without Homebrew")
	}
	if !strings.Contains(stdout.String(), "Homebrew was not found") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

// A failed install must stop after the first failing command and print the
// manual recovery steps rather than pressing on.
func TestOoklaRepairStopsAndExplainsWhenInstallFails(t *testing.T) {
	runner := &repairableFakeRunner{fakeRunner: fakeRunner{summary: summaryForProviders(model.CommandOokla, []model.Provider{model.ProviderOokla})}}
	app, stdout, _ := newTestApp(t, runner)
	app.StdoutTTY = true
	app.In = strings.NewReader("\n")
	app.LookupCommand = func(string) (string, error) { return "/opt/homebrew/bin/brew", nil }
	var commands []string
	app.RunCommand = func(_ context.Context, name string, args []string, _, _ io.Writer) error {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		return fmt.Errorf("Error: speedtest is already installed")
	}
	if exitCode := app.Execute(context.Background(), []string{"ookla", "--no-save"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if len(commands) != 1 {
		t.Fatalf("ran %d commands after the first failed: %#v", len(commands), commands)
	}
	if !strings.Contains(stdout.String(), "Official Ookla installation failed") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "you have confirmed it is safe") {
		t.Fatalf("stdout did not print the manual recovery steps: %q", stdout.String())
	}
	// Only the second Prepare would have happened on success.
	if runner.prepares != 1 {
		t.Fatalf("prepares = %d, want 1", runner.prepares)
	}
}

// --- preferences on the bare path --------------------------------------

// loadOrConfigurePreferences must surface a save failure instead of running
// the measurement with a configuration that was never persisted.
func TestBareTTYReportsAPreferencesSaveFailure(t *testing.T) {
	app, _, stderr := newTestApp(t, &fakeRunner{})
	app.StdoutTTY = true
	app.Preferences = preferences.New(filepath.Join(t.TempDir(), "preferences.json"))
	app.SetupFactory = func(context.Context, io.Reader, io.Writer, string, preferences.Config) (preferences.Config, error) {
		return preferences.Config{SchemaVersion: preferences.SchemaVersion, DailyStations: nil}, nil
	}
	app.ConfiguredSelectorFactory = func(context.Context, io.Reader, io.Writer, string, preferences.Config) (target.Plan, error) {
		t.Fatal("the selector opened although preferences could not be saved")
		return target.Plan{}, nil
	}
	if exitCode := app.Execute(context.Background(), nil); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "at least one daily station") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// Without a preferences store at all, the bare TTY path still works and uses
// the plain selector.
func TestBareTTYWithoutPreferencesUsesThePlainSelector(t *testing.T) {
	providers := []model.Provider{model.ProviderNJUCampusIPv4}
	runner := &fakeRunner{summary: summaryForProviders(model.CommandRun, providers)}
	app, _, stderr := newTestApp(t, runner)
	app.StdoutTTY = true
	app.Preferences = nil
	configuredCalled := false
	app.ConfiguredSelectorFactory = func(context.Context, io.Reader, io.Writer, string, preferences.Config) (target.Plan, error) {
		configuredCalled = true
		return target.Plan{}, nil
	}
	app.SelectorFactory = func(context.Context, io.Reader, io.Writer, string) (target.Plan, error) {
		return target.Plan{StationIDs: []string{"nju-campus"}, Family: target.FamilyIPv4, Providers: providers}, nil
	}
	app.ProgressFactory = func(io.Writer, string, []model.Provider) (progressRenderer, error) {
		return &fakeProgressRenderer{}, nil
	}
	if exitCode := app.Execute(context.Background(), nil); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	if configuredCalled {
		t.Fatal("the preference-aware selector ran without a preferences store")
	}
}

func TestMeasurementErrorCodeDistinguishesUnavailability(t *testing.T) {
	if got := measurementErrorCode(fmt.Errorf("%w: no helper", provider.ErrUnavailable)); got != "measurement_unavailable" {
		t.Fatalf("code = %q", got)
	}
	if got := measurementErrorCode(fmt.Errorf("boom")); got != "measurement_error" {
		t.Fatalf("code = %q", got)
	}
}

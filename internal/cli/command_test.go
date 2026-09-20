package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/soundadam/soundprobe/internal/consent"
	"github.com/soundadam/soundprobe/internal/model"
	"github.com/soundadam/soundprobe/internal/preferences"
	"github.com/soundadam/soundprobe/internal/provider"
	"github.com/soundadam/soundprobe/internal/target"
	"github.com/soundadam/soundprobe/internal/ui"
)

// plainRunner implements only provider.Runner: no Preflight, no Prepare.
// `doctor` needs a PreflightRunner and must say so rather than panic.
type plainRunner struct {
	summary model.RunSummary
	err     error
}

func (runner *plainRunner) Run(context.Context, provider.Request) (model.RunSummary, error) {
	return runner.summary, runner.err
}

// failingWriter stands in for a stdout that goes away mid-write (a closed
// pipe).  Every command must report a non-zero exit rather than claim success
// for a document nobody received.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stdout is closed") }

// decodeSingleJSON asserts the invariant the whole --json contract rests on:
// stdout holds exactly one JSON document, with no ANSI escapes and nothing
// before or after it.  Decoding the entire buffer (rather than searching it
// for a substring) is what makes a stray warning or prompt on stdout a test
// failure.
func decodeSingleJSON(t *testing.T, stdout *bytes.Buffer, into any) {
	t.Helper()
	raw := stdout.Bytes()
	if bytes.Contains(raw, []byte("\x1b[")) {
		t.Fatalf("JSON output contains ANSI escapes: %q", raw)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(into); err != nil {
		t.Fatalf("stdout is not a single JSON document: %v (stdout = %q)", err, raw)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		t.Fatalf("extra output followed the JSON document: %q", raw)
	}
}

// errorPayload is the machine-readable error envelope every command emits in
// JSON mode.
type errorPayload struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeJSONError(t *testing.T, stdout *bytes.Buffer) errorPayload {
	t.Helper()
	var payload errorPayload
	decodeSingleJSON(t, stdout, &payload)
	return payload
}

func staticProber(results ...target.ProbeResult) func(context.Context, time.Duration) []target.ProbeResult {
	return func(context.Context, time.Duration) []target.ProbeResult {
		return results
	}
}

// --- executeStations ---------------------------------------------------

// `stations` opens sockets to every known station in production.  The command
// must go through App.ProbeStations so tests (and CI) can run it without
// touching the network at all.
func TestStationsUsesInjectedProberAndNeverTheNetwork(t *testing.T) {
	app, stdout, stderr := newTestApp(t, &fakeRunner{})
	var gotTimeout time.Duration
	var hadDeadline bool
	app.ProbeStations = func(ctx context.Context, timeout time.Duration) []target.ProbeResult {
		gotTimeout = timeout
		_, hadDeadline = ctx.Deadline()
		return []target.ProbeResult{
			{StationID: "nju-campus", Family: "ipv4", Status: target.ProbeReachable, LatencyMS: model.Pointer(3.4)},
			{StationID: "mlab", Family: "auto", Status: target.ProbeAutomatic, Message: "automatic server selection"},
		}
	}
	if exitCode := app.Execute(context.Background(), []string{"stations", "--json"}); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	var results []target.ProbeResult
	decodeSingleJSON(t, stdout, &results)
	if len(results) != 2 || results[0].StationID != "nju-campus" || results[1].Status != target.ProbeAutomatic {
		t.Fatalf("results = %#v", results)
	}
	if gotTimeout != 1500*time.Millisecond {
		t.Fatalf("per-probe timeout = %s, want 1.5s", gotTimeout)
	}
	if !hadDeadline {
		t.Fatal("the probe context carried no deadline; a hung station would hang the command")
	}
}

func TestStationsHumanTableFormatsLatency(t *testing.T) {
	app, stdout, stderr := newTestApp(t, &fakeRunner{})
	app.ProbeStations = staticProber(
		target.ProbeResult{StationID: "nju-campus", Family: "ipv4", Status: target.ProbeReachable, LatencyMS: model.Pointer(3.4)},
		target.ProbeResult{StationID: "qlu", Family: "ipv4", Status: target.ProbeUnreachable, LatencyMS: nil, Message: "dial tcp: i/o timeout"},
		target.ProbeResult{StationID: "nju-edge", Family: "ipv4", Status: target.ProbeUnsupported, Message: "web-only station"},
	)
	if exitCode := app.Execute(context.Background(), []string{"stations"}); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	assertGolden(t, "stations_table.golden", stdout.String())
}

func TestStationsRejectsPositionalArguments(t *testing.T) {
	app, stdout, _ := newTestApp(t, &fakeRunner{})
	probed := false
	app.ProbeStations = func(context.Context, time.Duration) []target.ProbeResult {
		probed = true
		return nil
	}
	if exitCode := app.Execute(context.Background(), []string{"stations", "mlab", "--json"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if probed {
		t.Fatal("an argument error still ran the reachability probes")
	}
	if code := decodeJSONError(t, stdout).Error.Code; code != "invalid_arguments" {
		t.Fatalf("error code = %q", code)
	}
}

// setDefaults must leave the command usable when nothing was injected, so the
// seam cannot silently disable `stations` in production.
func TestProbeStationsDefaultsToTheRealProber(t *testing.T) {
	app := &App{}
	app.setDefaults()
	if app.ProbeStations == nil {
		t.Fatal("setDefaults left ProbeStations nil")
	}
}

// --- executeConsentStatus / executeConsentRevoke -----------------------

func TestConsentStatusNotAccepted(t *testing.T) {
	app, stdout, stderr := newTestApp(t, &fakeRunner{})
	if exitCode := app.Execute(context.Background(), []string{"consent", "status", "--json"}); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	var payload struct {
		Accepted      bool            `json:"accepted"`
		PolicyVersion string          `json:"policyVersion"`
		PolicyURL     string          `json:"policyUrl"`
		Record        json.RawMessage `json:"record"`
	}
	decodeSingleJSON(t, stdout, &payload)
	if payload.Accepted {
		t.Fatal("accepted = true with no consent file")
	}
	if payload.PolicyVersion != consent.PolicyVersion || payload.PolicyURL != consent.PolicyURL {
		t.Fatalf("payload = %#v", payload)
	}
	// There is no record to report, so the key must be absent rather than
	// present with a zero timestamp.
	if payload.Record != nil {
		t.Fatalf("record = %s, want the key to be omitted", payload.Record)
	}
}

func TestConsentStatusAcceptedIncludesTheRecord(t *testing.T) {
	app, stdout, stderr := newTestApp(t, &fakeRunner{})
	if _, err := app.Consent.Accept(app.Version, app.Now()); err != nil {
		t.Fatal(err)
	}
	if exitCode := app.Execute(context.Background(), []string{"consent", "status", "--json"}); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	var payload struct {
		Accepted bool            `json:"accepted"`
		Record   *consent.Record `json:"record"`
	}
	decodeSingleJSON(t, stdout, &payload)
	if !payload.Accepted || payload.Record == nil {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.Record.PolicyVersion != consent.PolicyVersion || payload.Record.ToolVersion != "test" {
		t.Fatalf("record = %#v", payload.Record)
	}
}

func TestConsentStatusHumanOutput(t *testing.T) {
	for _, test := range []struct {
		name   string
		accept bool
		want   string
	}{
		{name: "not accepted", accept: false, want: "is not accepted for current policy"},
		{name: "accepted", accept: true, want: "consent accepted ("},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, stdout, stderr := newTestApp(t, &fakeRunner{})
			if test.accept {
				if _, err := app.Consent.Accept(app.Version, app.Now()); err != nil {
					t.Fatal(err)
				}
			}
			if exitCode := app.Execute(context.Background(), []string{"consent", "status"}); exitCode != 0 {
				t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), test.want) {
				t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), test.want)
			}
			// The policy URL is the actionable part; it must always be shown.
			if !strings.Contains(stdout.String(), consent.PolicyURL) {
				t.Fatalf("stdout = %q, missing the policy URL", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("consent status wrote to stderr: %q", stderr.String())
			}
		})
	}
}

func TestConsentStatusReportsAnUnreadableRecord(t *testing.T) {
	app, stdout, _ := newTestApp(t, &fakeRunner{})
	if err := os.MkdirAll(filepath.Dir(app.Consent.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app.Consent.Path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if exitCode := app.Execute(context.Background(), []string{"consent", "status", "--json"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if code := decodeJSONError(t, stdout).Error.Code; code != "consent_error" {
		t.Fatalf("error code = %q, want consent_error", code)
	}
}

func TestConsentRevokeClearsAnAcceptedRecord(t *testing.T) {
	app, stdout, stderr := newTestApp(t, &fakeRunner{})
	if _, err := app.Consent.Accept(app.Version, app.Now()); err != nil {
		t.Fatal(err)
	}
	if exitCode := app.Execute(context.Background(), []string{"consent", "revoke", "--json"}); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	var payload map[string]bool
	decodeSingleJSON(t, stdout, &payload)
	if !payload["revoked"] {
		t.Fatalf("payload = %#v", payload)
	}
	if _, accepted, err := app.Consent.Status(); err != nil || accepted {
		t.Fatalf("Status() after revoke = %t, %v; want not accepted", accepted, err)
	}
}

// Revoking when nothing was ever accepted is a no-op, not an error: scripts
// run `consent revoke` to reach a known state.
func TestConsentRevokeWithNothingAcceptedSucceeds(t *testing.T) {
	app, stdout, stderr := newTestApp(t, &fakeRunner{})
	if exitCode := app.Execute(context.Background(), []string{"consent", "revoke"}); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "M-Lab consent revoked.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("consent revoke wrote to stderr: %q", stderr.String())
	}
}

func TestConsentCommandsFailWhenTheStoreIsMissing(t *testing.T) {
	for _, args := range [][]string{
		{"consent", "status", "--json"},
		{"consent", "accept", "--json"},
		{"consent", "revoke", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			app, stdout, _ := newTestApp(t, &fakeRunner{})
			app.Consent = nil
			if exitCode := app.Execute(context.Background(), args); exitCode != 1 {
				t.Fatalf("exit code = %d, want 1", exitCode)
			}
			if code := decodeJSONError(t, stdout).Error.Code; code != "consent_error" {
				t.Fatalf("error code = %q, want consent_error", code)
			}
		})
	}
}

// --- executeSetup ------------------------------------------------------

func setupApp(t *testing.T) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	app, stdout, stderr := newTestApp(t, &fakeRunner{})
	app.StdinTTY = true
	app.StdoutTTY = true
	app.Preferences = preferences.New(filepath.Join(t.TempDir(), "preferences.json"))
	return app, stdout, stderr
}

func TestSetupRefusesNonInteractiveInvocations(t *testing.T) {
	for _, test := range []struct {
		name      string
		args      []string
		stdinTTY  bool
		stdoutTTY bool
	}{
		{name: "json mode", args: []string{"setup", "--json"}, stdinTTY: true, stdoutTTY: true},
		{name: "piped stdin", args: []string{"setup"}, stdinTTY: false, stdoutTTY: true},
		{name: "redirected stdout", args: []string{"setup"}, stdinTTY: true, stdoutTTY: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, stdout, stderr := setupApp(t)
			app.StdinTTY = test.stdinTTY
			app.StdoutTTY = test.stdoutTTY
			called := false
			app.SetupFactory = func(context.Context, io.Reader, io.Writer, string, preferences.Config) (preferences.Config, error) {
				called = true
				return preferences.DefaultConfig(), nil
			}
			if exitCode := app.Execute(context.Background(), test.args); exitCode != 1 {
				t.Fatalf("exit code = %d, want 1", exitCode)
			}
			if called {
				t.Fatal("the interactive setup wizard ran without a terminal")
			}
			if test.args[len(test.args)-1] == "--json" {
				payload := decodeJSONError(t, stdout)
				if payload.Error.Code != "setup_requires_interaction" {
					t.Fatalf("error code = %q", payload.Error.Code)
				}
			} else {
				// Without --json the diagnostic belongs on stderr.
				if stdout.Len() != 0 {
					t.Fatalf("setup wrote a diagnostic to stdout: %q", stdout.String())
				}
				if !strings.Contains(stderr.String(), "setup requires an interactive terminal") {
					t.Fatalf("stderr = %q", stderr.String())
				}
			}
		})
	}
}

func TestSetupSavesTheChosenStations(t *testing.T) {
	app, stdout, stderr := setupApp(t)
	var seeded preferences.Config
	app.SetupFactory = func(_ context.Context, _ io.Reader, _ io.Writer, _ string, current preferences.Config) (preferences.Config, error) {
		seeded = current
		return preferences.Config{SchemaVersion: preferences.SchemaVersion, DailyStations: []string{"tongji", "qlu"}}, nil
	}
	if exitCode := app.Execute(context.Background(), []string{"setup"}); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	// With no file yet, the wizard opens on the defaults.
	if fmt.Sprint(seeded.DailyStations) != fmt.Sprint(preferences.DefaultConfig().DailyStations) {
		t.Fatalf("wizard seeded with %#v, want the defaults", seeded)
	}
	if !strings.Contains(stdout.String(), "Daily stations saved: tongji, qlu") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	config, exists, err := app.Preferences.Load()
	if err != nil || !exists {
		t.Fatalf("Load() = %#v, %t, %v", config, exists, err)
	}
	if fmt.Sprint(config.DailyStations) != "[tongji qlu]" {
		t.Fatalf("saved stations = %#v", config.DailyStations)
	}
}

// Re-running setup must open the wizard on the stations already configured,
// not silently reset the user to the defaults.
func TestSetupSeedsTheWizardWithTheExistingConfiguration(t *testing.T) {
	app, _, stderr := setupApp(t)
	if err := app.Preferences.Save(preferences.Config{SchemaVersion: preferences.SchemaVersion, DailyStations: []string{"tongji"}}); err != nil {
		t.Fatal(err)
	}
	var seeded preferences.Config
	app.SetupFactory = func(_ context.Context, _ io.Reader, _ io.Writer, _ string, current preferences.Config) (preferences.Config, error) {
		seeded = current
		return current, nil
	}
	if exitCode := app.Execute(context.Background(), []string{"setup"}); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	if fmt.Sprint(seeded.DailyStations) != "[tongji]" {
		t.Fatalf("wizard seeded with %#v, want the saved stations", seeded)
	}
}

// The hard invariant: a preferences file this build cannot use (older schema,
// malformed JSON) means "not configured yet".  `setup` must open the wizard on
// the defaults and overwrite the file, never fail with preferences_error.
// TestBareTTYRerunsSetupOverLegacySchemaPreferences guards the same rule for
// the bare-invocation path.
func TestSetupTreatsAnUnusablePreferencesFileAsNotConfigured(t *testing.T) {
	for _, test := range []struct {
		name     string
		contents string
	}{
		{name: "legacy schema", contents: `{"schemaVersion":1,"language":"zh-CN","dailyStations":["tongji"]}`},
		{name: "future schema", contents: fmt.Sprintf(`{"schemaVersion":%d,"dailyStations":["tongji"]}`, preferences.SchemaVersion+1)},
		{name: "malformed json", contents: `{"schemaVersion":`},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, stdout, stderr := setupApp(t)
			if err := os.WriteFile(app.Preferences.Path, []byte(test.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			var seeded preferences.Config
			app.SetupFactory = func(_ context.Context, _ io.Reader, _ io.Writer, _ string, current preferences.Config) (preferences.Config, error) {
				seeded = current
				return preferences.Config{SchemaVersion: preferences.SchemaVersion, DailyStations: []string{"mlab"}}, nil
			}
			if exitCode := app.Execute(context.Background(), []string{"setup"}); exitCode != 0 {
				t.Fatalf("an unusable preferences file hard-failed setup: exit code = %d, stderr = %q", exitCode, stderr.String())
			}
			if fmt.Sprint(seeded.DailyStations) != fmt.Sprint(preferences.DefaultConfig().DailyStations) {
				t.Fatalf("wizard seeded with %#v, want the defaults", seeded)
			}
			if !strings.Contains(stdout.String(), "Daily stations saved: mlab") {
				t.Fatalf("stdout = %q", stdout.String())
			}
			// The unusable file was replaced, so the next load succeeds.
			config, exists, err := app.Preferences.Load()
			if err != nil || !exists || config.SchemaVersion != preferences.SchemaVersion {
				t.Fatalf("Load() after setup = %#v, %t, %v", config, exists, err)
			}
		})
	}
}

// A genuine I/O failure is not "not configured yet" and must still be
// reported, so the ErrUnusable exemption stays as narrow as it is documented.
func TestSetupReportsAGenuinePreferencesReadFailure(t *testing.T) {
	app, _, stderr := setupApp(t)
	directory := filepath.Join(t.TempDir(), "preferences.json")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	app.Preferences = preferences.New(directory)
	called := false
	app.SetupFactory = func(context.Context, io.Reader, io.Writer, string, preferences.Config) (preferences.Config, error) {
		called = true
		return preferences.DefaultConfig(), nil
	}
	if exitCode := app.Execute(context.Background(), []string{"setup"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if called {
		t.Fatal("setup ran the wizard over an unreadable preferences path")
	}
	if !strings.Contains(stderr.String(), "read preferences") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestSetupCancellationReturns130AndSavesNothing(t *testing.T) {
	app, stdout, _ := setupApp(t)
	app.SetupFactory = func(context.Context, io.Reader, io.Writer, string, preferences.Config) (preferences.Config, error) {
		return preferences.Config{}, ui.ErrSetupCancelled
	}
	if exitCode := app.Execute(context.Background(), []string{"setup"}); exitCode != 130 {
		t.Fatalf("exit code = %d, want 130", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("a cancelled setup still printed to stdout: %q", stdout.String())
	}
	if _, exists, err := app.Preferences.Load(); exists || err != nil {
		t.Fatalf("a cancelled setup wrote preferences: exists = %t, err = %v", exists, err)
	}
}

func TestSetupReportsWizardAndSaveFailures(t *testing.T) {
	t.Run("wizard error", func(t *testing.T) {
		app, _, stderr := setupApp(t)
		app.SetupFactory = func(context.Context, io.Reader, io.Writer, string, preferences.Config) (preferences.Config, error) {
			return preferences.Config{}, errors.New("terminal went away")
		}
		if exitCode := app.Execute(context.Background(), []string{"setup"}); exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
		if !strings.Contains(stderr.String(), "terminal went away") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})
	t.Run("save error", func(t *testing.T) {
		app, _, stderr := setupApp(t)
		// A config the wizard should never produce; Save rejects it, and the
		// command must surface that instead of claiming success.
		app.SetupFactory = func(context.Context, io.Reader, io.Writer, string, preferences.Config) (preferences.Config, error) {
			return preferences.Config{SchemaVersion: 0, DailyStations: []string{"tongji"}}, nil
		}
		if exitCode := app.Execute(context.Background(), []string{"setup"}); exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
		if !strings.Contains(stderr.String(), "preferences schema") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})
}

func TestSetupWithoutAPreferencesStoreFails(t *testing.T) {
	app, _, stderr := setupApp(t)
	app.Preferences = nil
	if exitCode := app.Execute(context.Background(), []string{"setup"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "preferences store is not configured") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestSetupRejectsPositionalArguments(t *testing.T) {
	app, stdout, _ := setupApp(t)
	if exitCode := app.Execute(context.Background(), []string{"setup", "tongji", "--json"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if code := decodeJSONError(t, stdout).Error.Code; code != "invalid_arguments" {
		t.Fatalf("error code = %q", code)
	}
}

// --- handleError and the exit-code contract ----------------------------

// The documented exit codes are 0 success, 1 pre-measurement failure,
// 2 partial or failed measurements, 130 cancelled.  Anything that changes
// these breaks every script and CI job that consumes soundprobe.
func TestDocumentedExitCodes(t *testing.T) {
	t.Run("0 on success", func(t *testing.T) {
		app, _, stderr := newTestApp(t, &fakeRunner{summary: successfulSummary(model.CommandCampus)})
		if exitCode := app.Execute(context.Background(), []string{"campus", "--no-save"}); exitCode != 0 {
			t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
		}
	})
	t.Run("1 before any measurement runs", func(t *testing.T) {
		runner := &fakeRunner{preflightErr: errors.New("helper missing"), summary: successfulSummary(model.CommandCampus)}
		app, stdout, _ := newTestApp(t, runner)
		if exitCode := app.Execute(context.Background(), []string{"campus", "--json", "--no-save"}); exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
		if code := decodeJSONError(t, stdout).Error.Code; code != "measurement_error" {
			t.Fatalf("error code = %q", code)
		}
	})
	t.Run("2 when every measurement failed", func(t *testing.T) {
		summary := successfulSummary(model.CommandCampus)
		summary.Status = model.RunStatusFailed
		summary.Measurements[0].Status = model.ProviderStatusFailed
		app, _, stderr := newTestApp(t, &fakeRunner{summary: summary})
		if exitCode := app.Execute(context.Background(), []string{"campus", "--no-save"}); exitCode != 2 {
			t.Fatalf("exit code = %d, want 2, stderr = %q", exitCode, stderr.String())
		}
	})
	t.Run("130 when the run was cancelled", func(t *testing.T) {
		summary := successfulSummary(model.CommandCampus)
		summary.Status = model.RunStatusCancelled
		summary.Measurements[0].Status = model.ProviderStatusCancelled
		app, _, stderr := newTestApp(t, &fakeRunner{summary: summary})
		if exitCode := app.Execute(context.Background(), []string{"campus", "--no-save"}); exitCode != 130 {
			t.Fatalf("exit code = %d, want 130, stderr = %q", exitCode, stderr.String())
		}
	})
	t.Run("130 when the station selector was cancelled", func(t *testing.T) {
		app, _, _ := newTestApp(t, &fakeRunner{})
		app.StdoutTTY = true
		app.SelectorFactory = func(context.Context, io.Reader, io.Writer, string) (target.Plan, error) {
			return target.Plan{}, ui.ErrSelectionCancelled
		}
		if exitCode := app.Execute(context.Background(), nil); exitCode != 130 {
			t.Fatalf("exit code = %d, want 130", exitCode)
		}
	})
}

// handleError is fang's error sink: it maps cobra parsing failures onto the
// same machine-readable envelope every other command uses.
func TestHandleErrorMapsParsingFailuresInJSONMode(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		code string
	}{
		{name: "unknown command", args: []string{"speedtest", "--json"}, code: "unknown_command"},
		{name: "unknown flag", args: []string{"campus", "--turbo", "--json"}, code: "invalid_arguments"},
		{name: "bad flag value", args: []string{"history", "--limit", "many", "--json"}, code: "invalid_arguments"},
		{name: "unexpected argument", args: []string{"campus", "extra", "--json"}, code: "invalid_arguments"},
		{name: "missing run id", args: []string{"show", "--json"}, code: "invalid_arguments"},
		{name: "consent without a subcommand", args: []string{"consent", "--json"}, code: "invalid_arguments"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, stdout, _ := newTestApp(t, &fakeRunner{})
			if exitCode := app.Execute(context.Background(), test.args); exitCode != 1 {
				t.Fatalf("exit code = %d, want 1", exitCode)
			}
			payload := decodeJSONError(t, stdout)
			if payload.Error.Code != test.code {
				t.Fatalf("error code = %q, want %q (stdout = %q)", payload.Error.Code, test.code, stdout.String())
			}
			// "unknown command X, did you mean...?" is multi-line; the
			// machine-readable message must stay on one line.
			if strings.Contains(payload.Error.Message, "\n") {
				t.Fatalf("error message is multi-line: %q", payload.Error.Message)
			}
			if payload.Error.Message == "" {
				t.Fatal("error message is empty")
			}
		})
	}
}

// Without --json the styled human error goes to stderr; stdout must stay
// empty so `soundprobe ... > file` never captures an error banner.
func TestParsingErrorsKeepStdoutClean(t *testing.T) {
	app, stdout, stderr := newTestApp(t, &fakeRunner{})
	if exitCode := app.Execute(context.Background(), []string{"speedtest"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("a parsing error wrote to stdout: %q", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("a parsing error produced no diagnostic on stderr")
	}
}

// --- the --json contract, end to end -----------------------------------

// Every command must put exactly one JSON document on stdout in JSON mode, on
// both the success and the error path.  decodeSingleJSON fails if anything at
// all leaks onto stdout alongside it.
func TestJSONModeAlwaysEmitsExactlyOneDocument(t *testing.T) {
	savedRun := successfulSummary(model.CommandCampus)
	exportDirectory := t.TempDir()
	for _, test := range []struct {
		name      string
		args      []string
		exitCode  int
		errorCode string // empty for a success path
		prepare   func(t *testing.T, app *App)
	}{
		{name: "version", args: []string{"version", "--json"}},
		{name: "doctor", args: []string{"doctor", "--json"}},
		{name: "stations", args: []string{"stations", "--json"}, prepare: func(_ *testing.T, app *App) {
			app.ProbeStations = staticProber(target.ProbeResult{StationID: "mlab", Family: "auto", Status: target.ProbeAutomatic})
		}},
		{name: "history empty", args: []string{"history", "--json"}},
		{name: "history populated", args: []string{"history", "--json"}, prepare: seedRun(savedRun)},
		{name: "last", args: []string{"last", "--json"}, prepare: seedRun(savedRun)},
		{name: "show", args: []string{"show", savedRun.RunID, "--json"}, prepare: seedRun(savedRun)},
		{
			name:    "export",
			args:    []string{"export", "--format", "csv", "--output", filepath.Join(exportDirectory, "runs.csv"), "--json"},
			prepare: seedRun(savedRun),
		},
		{name: "consent status", args: []string{"consent", "status", "--json"}},
		{name: "consent revoke", args: []string{"consent", "revoke", "--json"}},
		{name: "measurement", args: []string{"campus", "--json", "--no-save"}, prepare: func(_ *testing.T, app *App) {
			app.Runner = &fakeRunner{summary: successfulSummary(model.CommandCampus)}
		}},

		{name: "last with no history", args: []string{"last", "--json"}, exitCode: 1, errorCode: "no_history"},
		{name: "show a missing run", args: []string{"show", "00000000-0000-4000-8000-0000000000ff", "--json"}, exitCode: 1, errorCode: "storage_error"},
		{name: "export without flags", args: []string{"export", "--json"}, exitCode: 1, errorCode: "invalid_arguments"},
		{name: "export to an unwritable path", args: []string{"export", "--format", "csv", "--output", filepath.Join(exportDirectory, "missing", "runs.csv"), "--json"}, exitCode: 1, errorCode: "export_error"},
		{name: "invalid family", args: []string{"run", "--family", "ipv7", "--json"}, exitCode: 1, errorCode: "invalid_arguments"},
		{name: "unknown station", args: []string{"run", "--targets", "not-a-station", "--json"}, exitCode: 1, errorCode: "invalid_arguments"},
		{name: "consent accept", args: []string{"consent", "accept", "--json"}, exitCode: 1, errorCode: "consent_requires_interaction"},
		{name: "setup", args: []string{"setup", "--json"}, exitCode: 1, errorCode: "setup_requires_interaction"},
		{name: "mlab without consent", args: []string{"mlab", "--json", "--no-save"}, exitCode: 1, errorCode: "consent_required"},
		{name: "unknown command", args: []string{"speedtest", "--json"}, exitCode: 1, errorCode: "unknown_command"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, stdout, _ := newTestApp(t, &fakeRunner{summary: successfulSummary(model.CommandCampus)})
			if test.prepare != nil {
				test.prepare(t, app)
			}
			if exitCode := app.Execute(context.Background(), test.args); exitCode != test.exitCode {
				t.Fatalf("exit code = %d, want %d (stdout = %q)", exitCode, test.exitCode, stdout.String())
			}
			if test.errorCode == "" {
				var anything any
				decodeSingleJSON(t, stdout, &anything)
				return
			}
			if code := decodeJSONError(t, stdout).Error.Code; code != test.errorCode {
				t.Fatalf("error code = %q, want %q", code, test.errorCode)
			}
		})
	}
}

func seedRun(summary model.RunSummary) func(t *testing.T, app *App) {
	return func(t *testing.T, app *App) {
		t.Helper()
		if err := app.History.Save(summary); err != nil {
			t.Fatal(err)
		}
	}
}

// --- stdout / stderr separation ----------------------------------------

// Warnings about skipped history files are diagnostics.  They must never land
// on stdout, or `soundprobe history --json | jq` breaks.
func TestWarningsStayOffStdoutInJSONMode(t *testing.T) {
	app, stdout, stderr := newTestApp(t, &fakeRunner{})
	current := writeHistory(t, app)
	if exitCode := app.Execute(context.Background(), []string{"history", "--json"}); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	var listed []model.RunSummary
	decodeSingleJSON(t, stdout, &listed)
	if len(listed) != 1 || listed[0].RunID != current.RunID {
		t.Fatalf("history = %#v", listed)
	}
	if !strings.Contains(stderr.String(), "skipped 1 unreadable history file") {
		t.Fatalf("the skip warning did not reach stderr: %q", stderr.String())
	}
}

// The same holds for the "optional target removed" notice emitted mid-run.
func TestOptionalTargetNoticeStaysOffStdout(t *testing.T) {
	providers := []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderMLab}
	runner := &preparingFakeRunner{fakeRunner: fakeRunner{summary: summaryForProviders(model.CommandRun, providers)}}
	app, stdout, stderr := newTestApp(t, runner)
	if _, err := app.Consent.Accept(app.Version, app.Now()); err != nil {
		t.Fatal(err)
	}
	if exitCode := app.Execute(context.Background(), []string{"run", "--targets", "nju-campus,mlab,ookla", "--no-save"}); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), "optional target") {
		t.Fatalf("the optional-target notice leaked onto stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "optional target") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// Truncation of the skipped-file list keeps the warning to one readable line.
func TestUnreadableHistoryWarningIsTruncated(t *testing.T) {
	app, _, stderr := newTestApp(t, &fakeRunner{})
	app.reportUnreadableHistory(nil)
	if stderr.Len() != 0 {
		t.Fatalf("an empty skip list produced a warning: %q", stderr.String())
	}
	app.reportUnreadableHistory([]string{"a.json", "b.json", "c.json", "d.json", "e.json"})
	warning := stderr.String()
	if !strings.Contains(warning, "skipped 5 unreadable history file(s)") {
		t.Fatalf("stderr = %q", warning)
	}
	if !strings.Contains(warning, "and 2 more") {
		t.Fatalf("the list was not truncated: %q", warning)
	}
	if strings.Contains(warning, "d.json") {
		t.Fatalf("more than three files were named: %q", warning)
	}
	if strings.Count(strings.TrimSuffix(warning, "\n"), "\n") != 0 {
		t.Fatalf("the warning spans several lines: %q", warning)
	}
}

// --- writers that fail --------------------------------------------------

// If the JSON document cannot be written, the exit code must say so.
func TestJSONEncodeFailureIsNotReportedAsSuccess(t *testing.T) {
	for _, test := range []struct {
		name    string
		args    []string
		prepare func(app *App)
	}{
		{name: "version", args: []string{"version", "--json"}},
		{name: "history", args: []string{"history", "--json"}},
		{name: "doctor", args: []string{"doctor", "--json"}},
		{name: "stations", args: []string{"stations", "--json"}, prepare: func(app *App) {
			app.ProbeStations = staticProber()
		}},
		{name: "last", args: []string{"last", "--json"}, prepare: func(app *App) {
			if err := app.History.Save(successfulSummary(model.CommandCampus)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "show", args: []string{"show", successfulSummary(model.CommandCampus).RunID, "--json"}, prepare: func(app *App) {
			if err := app.History.Save(successfulSummary(model.CommandCampus)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "measurement", args: []string{"campus", "--json", "--no-save"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, _, _ := newTestApp(t, &fakeRunner{summary: successfulSummary(model.CommandCampus)})
			app.Out = failingWriter{}
			if test.prepare != nil {
				test.prepare(app)
			}
			if exitCode := app.Execute(context.Background(), test.args); exitCode == 0 {
				t.Fatal("exit code = 0 although the JSON document could not be written")
			}
		})
	}
}

// --- doctor -------------------------------------------------------------

func TestDoctorRequiresAPreflightRunner(t *testing.T) {
	app, stdout, _ := newTestApp(t, &plainRunner{})
	if exitCode := app.Execute(context.Background(), []string{"doctor", "--json"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if code := decodeJSONError(t, stdout).Error.Code; code != "internal_error" {
		t.Fatalf("error code = %q", code)
	}
}

// A required provider that is not ready makes doctor exit 1; an optional one
// that is missing must not.
func TestDoctorExitCodeTracksRequiredProvidersOnly(t *testing.T) {
	t.Run("required provider down", func(t *testing.T) {
		app, stdout, _ := newTestApp(t, &fakeRunner{preflightErr: errors.New("network is unreachable")})
		if exitCode := app.Execute(context.Background(), []string{"doctor", "--json"}); exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
		var payload struct {
			Ready         bool `json:"ready"`
			CombinedReady bool `json:"combinedReady"`
		}
		decodeSingleJSON(t, stdout, &payload)
		if payload.Ready || payload.CombinedReady {
			t.Fatalf("payload = %#v", payload)
		}
	})
	t.Run("human output and preferences", func(t *testing.T) {
		app, stdout, stderr := newTestApp(t, &fakeRunner{})
		app.Preferences = preferences.New(filepath.Join(t.TempDir(), "preferences.json"))
		if err := app.Preferences.Save(preferences.Config{SchemaVersion: preferences.SchemaVersion, DailyStations: []string{"tongji"}}); err != nil {
			t.Fatal(err)
		}
		if exitCode := app.Execute(context.Background(), []string{"doctor"}); exitCode != 0 {
			t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
		}
		for _, want := range []string{"soundprobe test diagnostics", "Campus", "M-Lab", "Consent", "History"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("stdout = %q, missing %q", stdout.String(), want)
			}
		}
	})
	t.Run("json reports the preferences state", func(t *testing.T) {
		app, stdout, _ := newTestApp(t, &fakeRunner{})
		path := filepath.Join(t.TempDir(), "preferences.json")
		app.Preferences = preferences.New(path)
		if exitCode := app.Execute(context.Background(), []string{"doctor", "--json"}); exitCode != 0 {
			t.Fatalf("exit code = %d", exitCode)
		}
		var payload struct {
			PreferencesPath string `json:"preferencesPath"`
			SetupComplete   bool   `json:"setupComplete"`
		}
		decodeSingleJSON(t, stdout, &payload)
		if payload.PreferencesPath != path || payload.SetupComplete {
			t.Fatalf("payload = %#v", payload)
		}
	})
}

// --- measurement plan edge cases ---------------------------------------

func TestMeasurementFailsWhenTheRendererCannotStart(t *testing.T) {
	app, _, stderr := newTestApp(t, &fakeRunner{summary: successfulSummary(model.CommandCampus)})
	app.StdoutTTY = true
	app.ProgressFactory = func(io.Writer, string, []model.Provider) (progressRenderer, error) {
		return nil, errors.New("not a terminal after all")
	}
	if exitCode := app.Execute(context.Background(), []string{"campus", "--no-save"}); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "start interactive renderer") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestMeasurementFailsWithoutARunnerOrHistoryStore(t *testing.T) {
	t.Run("no runner", func(t *testing.T) {
		app, stdout, _ := newTestApp(t, nil)
		if exitCode := app.Execute(context.Background(), []string{"campus", "--json", "--no-save"}); exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
		if code := decodeJSONError(t, stdout).Error.Code; code != "internal_error" {
			t.Fatalf("error code = %q", code)
		}
	})
	t.Run("no history store", func(t *testing.T) {
		app, stdout, _ := newTestApp(t, &fakeRunner{summary: successfulSummary(model.CommandCampus)})
		app.History = nil
		if exitCode := app.Execute(context.Background(), []string{"campus", "--json"}); exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
		if code := decodeJSONError(t, stdout).Error.Code; code != "storage_error" {
			t.Fatalf("error code = %q", code)
		}
	})
	t.Run("runner failure", func(t *testing.T) {
		app, stdout, _ := newTestApp(t, &fakeRunner{err: fmt.Errorf("%w: helper vanished", provider.ErrUnavailable)})
		if exitCode := app.Execute(context.Background(), []string{"campus", "--json", "--no-save"}); exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
		if code := decodeJSONError(t, stdout).Error.Code; code != "measurement_unavailable" {
			t.Fatalf("error code = %q", code)
		}
	})
}

// The runner may return a summary with unset envelope fields; the CLI fills
// them in so nothing incomplete reaches history or stdout.
func TestMeasurementBackfillsTheSummaryEnvelope(t *testing.T) {
	runner := &fakeRunner{summary: model.RunSummary{
		RunID:     "00000000-0000-4000-8000-0000000000aa",
		StartedAt: fixedTime(),
		EndedAt:   fixedTime().Add(time.Second),
		Measurements: []model.Measurement{{
			Provider:     model.ProviderNJUCampusIPv4,
			Method:       model.ProviderMethod(model.ProviderNJUCampusIPv4),
			Status:       model.ProviderStatusSuccess,
			DownloadMbps: model.Pointer(10.0),
			UploadMbps:   model.Pointer(5.0),
		}},
	}}
	app, stdout, stderr := newTestApp(t, runner)
	if exitCode := app.Execute(context.Background(), []string{"campus", "--json", "--no-save"}); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	var decoded model.RunSummary
	decodeSingleJSON(t, stdout, &decoded)
	if decoded.SchemaVersion != model.SchemaVersion {
		t.Fatalf("schema version = %d", decoded.SchemaVersion)
	}
	if decoded.ToolVersion != "test" || decoded.Command != model.CommandCampus {
		t.Fatalf("envelope = %#v", decoded)
	}
	if decoded.Status != model.RunStatusSuccess || len(decoded.Targets) != 1 {
		t.Fatalf("derived status = %q, targets = %#v", decoded.Status, decoded.Targets)
	}
}

// --- executeBare --------------------------------------------------------

// Piped or redirected, the bare command must never open a selector: it runs
// the default IPv4 plan instead.
func TestBareNonInteractiveRunsTheDefaultPlan(t *testing.T) {
	providers := []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderMLab, model.ProviderApple}
	runner := &fakeRunner{summary: summaryForProviders(model.CommandRun, providers)}
	app, _, stderr := newTestApp(t, runner)
	app.StdoutTTY = false
	if _, err := app.Consent.Accept(app.Version, app.Now()); err != nil {
		t.Fatal(err)
	}
	app.SelectorFactory = func(context.Context, io.Reader, io.Writer, string) (target.Plan, error) {
		t.Fatal("the interactive selector opened without a terminal")
		return target.Plan{}, nil
	}
	if exitCode := app.Execute(context.Background(), nil); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	if runner.request.Command != model.CommandRun {
		t.Fatalf("command = %q", runner.request.Command)
	}
}

// --json on a TTY must behave exactly like a pipe.
func TestBareJSONOnATTYDoesNotOpenTheSelector(t *testing.T) {
	providers := []model.Provider{model.ProviderNJUCampusIPv4, model.ProviderMLab, model.ProviderApple}
	app, stdout, stderr := newTestApp(t, &fakeRunner{summary: summaryForProviders(model.CommandRun, providers)})
	app.StdoutTTY = true
	if _, err := app.Consent.Accept(app.Version, app.Now()); err != nil {
		t.Fatal(err)
	}
	app.SelectorFactory = func(context.Context, io.Reader, io.Writer, string) (target.Plan, error) {
		t.Fatal("the interactive selector opened in JSON mode")
		return target.Plan{}, nil
	}
	app.ProgressFactory = func(io.Writer, string, []model.Provider) (progressRenderer, error) {
		t.Fatal("the progress renderer opened in JSON mode")
		return nil, nil
	}
	if exitCode := app.Execute(context.Background(), []string{"--json"}); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	var decoded model.RunSummary
	decodeSingleJSON(t, stdout, &decoded)
}

func TestBareTTYReportsSelectorAndPreferencesFailures(t *testing.T) {
	t.Run("selector error", func(t *testing.T) {
		app, _, stderr := newTestApp(t, &fakeRunner{})
		app.StdoutTTY = true
		app.SelectorFactory = func(context.Context, io.Reader, io.Writer, string) (target.Plan, error) {
			return target.Plan{}, errors.New("terminal resize storm")
		}
		if exitCode := app.Execute(context.Background(), nil); exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
		if !strings.Contains(stderr.String(), "terminal resize storm") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})
	t.Run("preferences error", func(t *testing.T) {
		app, _, stderr := newTestApp(t, &fakeRunner{})
		app.StdoutTTY = true
		directory := filepath.Join(t.TempDir(), "preferences.json")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		app.Preferences = preferences.New(directory)
		if exitCode := app.Execute(context.Background(), nil); exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
		if !strings.Contains(stderr.String(), "read preferences") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})
}

// --- helper formatting --------------------------------------------------

func TestFormatCommandQuotesArgumentsThatNeedIt(t *testing.T) {
	for _, test := range []struct {
		command []string
		want    string
	}{
		{command: []string{"brew", "install", "speedtest"}, want: "brew install speedtest"},
		{command: []string{"brew", "install", "a formula"}, want: `brew install "a formula"`},
		{command: []string{"echo", `it's`}, want: `echo "it's"`},
		{command: nil, want: ""},
	} {
		if got := formatCommand(test.command); got != test.want {
			t.Fatalf("formatCommand(%#v) = %q, want %q", test.command, got, test.want)
		}
	}
}

func TestSplitCommaList(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "a,b", want: "[a b]"},
		{input: " a , b ,, ", want: "[a b]"},
		{input: ",,,", want: "[]"},
	} {
		if got := fmt.Sprint(splitCommaList(test.input)); got != test.want {
			t.Fatalf("splitCommaList(%q) = %s, want %s", test.input, got, test.want)
		}
	}
}

func TestExtractGlobalJSONStripsTheFlagAnywhere(t *testing.T) {
	args, jsonMode := extractGlobalJSON([]string{"run", "--json", "--targets", "mlab"})
	if !jsonMode || fmt.Sprint(args) != "[run --targets mlab]" {
		t.Fatalf("args = %v, json = %t", args, jsonMode)
	}
	args, jsonMode = extractGlobalJSON([]string{"run"})
	if jsonMode || fmt.Sprint(args) != "[run]" {
		t.Fatalf("args = %v, json = %t", args, jsonMode)
	}
}

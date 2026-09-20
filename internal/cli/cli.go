// Package cli implements the soundprobe command-line application.
//
// cli.go holds the App type and its wiring, the shared command options, and the
// setup/doctor/preferences cluster.  The rest of the package is split by
// concern: root.go builds the cobra tree, measure.go runs measurement plans,
// history.go serves saved runs, consent.go handles M-Lab consent, install.go
// offers the Ookla helper repair, and render.go/style.go produce human output.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/soundadam/soundprobe/internal/consent"
	"github.com/soundadam/soundprobe/internal/model"
	"github.com/soundadam/soundprobe/internal/preferences"
	"github.com/soundadam/soundprobe/internal/provider"
	"github.com/soundadam/soundprobe/internal/storage"
	"github.com/soundadam/soundprobe/internal/target"
	"github.com/soundadam/soundprobe/internal/ui"
)

type progressRenderer interface {
	Update(provider.ProgressEvent)
	Close() error
}

type App struct {
	In                        io.Reader
	Out                       io.Writer
	Err                       io.Writer
	StdinTTY                  bool
	StdoutTTY                 bool
	Version                   string
	Runner                    provider.Runner
	History                   *storage.Store
	Consent                   *consent.Store
	Now                       func() time.Time
	ProgressFactory           func(io.Writer, string, []model.Provider) (progressRenderer, error)
	SelectorFactory           func(context.Context, io.Reader, io.Writer, string) (target.Plan, error)
	ConfiguredSelectorFactory func(context.Context, io.Reader, io.Writer, string, preferences.Config) (target.Plan, error)
	SetupFactory              func(context.Context, io.Reader, io.Writer, string, preferences.Config) (preferences.Config, error)
	Preferences               *preferences.Store
	LookupCommand             func(string) (string, error)
	RunCommand                func(context.Context, string, []string, io.Writer, io.Writer) error
	// ProbeStations backs `soundprobe stations`.  It is a field so that tests
	// can exercise the command without the real reachability probes, which
	// open sockets to every known station.
	ProbeStations func(context.Context, time.Duration) []target.ProbeResult
}

type commandOptions struct {
	label   string
	note    string
	targets string
	family  string
	noSave  bool
	ipv4    bool
	ipv6    bool
}

// normalize resolves the --ipv4/--ipv6 shortcuts into a family and validates
// the result.
func (options *commandOptions) normalize() error {
	if options.ipv4 && options.ipv6 {
		return errors.New("--ipv4 and --ipv6 are mutually exclusive")
	}
	if options.ipv6 {
		options.family = string(target.FamilyIPv6)
	} else if options.ipv4 {
		options.family = string(target.FamilyIPv4)
	}
	family := target.Family(options.family)
	if family != target.FamilyIPv4 && family != target.FamilyIPv6 && family != target.FamilyDual {
		return errors.New("--family must be ipv4, ipv6, or dual")
	}
	return nil
}

func (app *App) setDefaults() {
	if app.In == nil {
		app.In = strings.NewReader("")
	}
	if app.Out == nil {
		app.Out = io.Discard
	}
	if app.Err == nil {
		app.Err = io.Discard
	}
	if app.Version == "" {
		app.Version = "dev"
	}
	if app.Now == nil {
		app.Now = time.Now
	}
	if app.ProgressFactory == nil {
		app.ProgressFactory = func(output io.Writer, version string, providers []model.Provider) (progressRenderer, error) {
			return ui.NewProgressRenderer(output, version, providers)
		}
	}
	if app.SelectorFactory == nil {
		app.SelectorFactory = ui.SelectPlan
	}
	if app.ConfiguredSelectorFactory == nil {
		app.ConfiguredSelectorFactory = ui.SelectPlanConfigured
	}
	if app.SetupFactory == nil {
		app.SetupFactory = ui.Configure
	}
	if app.LookupCommand == nil {
		app.LookupCommand = exec.LookPath
	}
	if app.RunCommand == nil {
		app.RunCommand = runCommand
	}
	if app.ProbeStations == nil {
		app.ProbeStations = target.ProbeAll
	}
}

// extractGlobalJSON strips the global --json flag from anywhere in the
// argument list, mirroring the historical CLI behavior.
func extractGlobalJSON(args []string) ([]string, bool) {
	filtered := make([]string, 0, len(args))
	jsonMode := false
	for _, arg := range args {
		if arg == "--json" {
			jsonMode = true
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered, jsonMode
}

// executeBare handles `soundprobe` without a subcommand: an interactive
// station selector on a terminal, or the default plan otherwise.
func (app *App) executeBare(ctx context.Context, jsonMode bool) int {
	if app.StdinTTY && app.StdoutTTY && !jsonMode {
		config, err := app.loadOrConfigurePreferences(ctx)
		if err != nil {
			if errors.Is(err, ui.ErrSetupCancelled) {
				return 130
			}
			return app.fail(false, "preferences_error", err.Error(), 1)
		}
		var plan target.Plan
		if app.Preferences != nil {
			plan, err = app.ConfiguredSelectorFactory(ctx, app.In, app.Out, app.Version, config)
		} else {
			plan, err = app.SelectorFactory(ctx, app.In, app.Out, app.Version)
		}
		if err != nil {
			if errors.Is(err, ui.ErrSelectionCancelled) {
				return 130
			}
			return app.fail(false, "selector_error", err.Error(), 1)
		}
		return app.executeMeasurementPlan(ctx, model.CommandRun, commandOptions{}, plan, false)
	}
	return app.runMeasurement(ctx, model.CommandRun, commandOptions{family: string(target.FamilyIPv4)}, jsonMode)
}

func (app *App) loadOrConfigurePreferences(ctx context.Context) (preferences.Config, error) {
	if app.Preferences == nil {
		return preferences.DefaultConfig(), nil
	}
	config, exists, err := app.Preferences.Load()
	if err != nil && !errors.Is(err, preferences.ErrUnusable) {
		return preferences.Config{}, err
	}
	if exists {
		return config, nil
	}
	// A missing file, and equally one that is malformed or written by another
	// schema version, means "not configured yet": run setup and overwrite it.
	config, err = app.SetupFactory(ctx, app.In, app.Out, app.Version, preferences.DefaultConfig())
	if err != nil {
		return preferences.Config{}, err
	}
	if err := app.Preferences.Save(config); err != nil {
		return preferences.Config{}, err
	}
	return config, nil
}

func (app *App) executeSetup(ctx context.Context, jsonMode bool) int {
	if jsonMode || !app.StdinTTY || !app.StdoutTTY {
		return app.fail(jsonMode, "setup_requires_interaction", "setup requires an interactive terminal", 1)
	}
	if app.Preferences == nil {
		return app.fail(false, "preferences_error", "preferences store is not configured", 1)
	}
	current, exists, err := app.Preferences.Load()
	if err != nil && !errors.Is(err, preferences.ErrUnusable) {
		return app.fail(false, "preferences_error", err.Error(), 1)
	}
	if !exists {
		current = preferences.DefaultConfig()
	}
	config, err := app.SetupFactory(ctx, app.In, app.Out, app.Version, current)
	if err != nil {
		if errors.Is(err, ui.ErrSetupCancelled) {
			return 130
		}
		return app.fail(false, "preferences_error", err.Error(), 1)
	}
	if err := app.Preferences.Save(config); err != nil {
		return app.fail(false, "preferences_error", err.Error(), 1)
	}
	return app.writeValue(false, config, "Daily stations saved: "+strings.Join(config.DailyStations, ", "))
}

func (app *App) executeDoctor(ctx context.Context, jsonMode bool) int {
	preflight, ok := app.Runner.(provider.PreflightRunner)
	if !ok {
		return app.fail(jsonMode, "internal_error", "measurement runner does not support diagnostics", 1)
	}

	checks := map[string]string{}
	optionalChecks := map[string]string{}
	ready := true
	for _, check := range []struct {
		name     string
		command  model.Command
		provider model.Provider
	}{
		{name: "campus", command: model.CommandCampus, provider: model.ProviderNJUCampusIPv4},
		{name: "mlab", command: model.CommandMLab, provider: model.ProviderMLab},
	} {
		err := preflight.Preflight(ctx, provider.Request{Command: check.command, Targets: []model.Provider{check.provider}})
		if err != nil {
			checks[check.name] = err.Error()
			ready = false
		} else {
			checks[check.name] = "ready"
		}
	}
	for _, check := range []struct {
		name     string
		command  model.Command
		provider model.Provider
	}{
		{name: "apple", command: model.CommandApple, provider: model.ProviderApple},
		{name: "ookla", command: model.CommandOokla, provider: model.ProviderOokla},
	} {
		err := preflight.Preflight(ctx, provider.Request{Command: check.command, Targets: []model.Provider{check.provider}})
		if err != nil {
			optionalChecks[check.name] = err.Error()
		} else {
			optionalChecks[check.name] = "ready"
		}
	}

	consentAccepted := false
	if app.Consent != nil {
		_, consentAccepted, _ = app.Consent.Status()
	}
	historyPath := ""
	if app.History != nil {
		historyPath = app.History.HistoryDir
	}
	combinedReady := ready && consentAccepted
	payload := map[string]any{
		"version":           app.Version,
		"ready":             ready,
		"combinedReady":     combinedReady,
		"providers":         checks,
		"optionalProviders": optionalChecks,
		"consentAccepted":   consentAccepted,
		"historyPath":       historyPath,
	}
	if app.Preferences != nil {
		config, exists, preferencesErr := app.Preferences.Load()
		payload["preferencesPath"] = app.Preferences.Path
		payload["setupComplete"] = exists && preferencesErr == nil
		if preferencesErr == nil && exists {
			payload["dailyStations"] = config.DailyStations
		}
	}
	if jsonMode {
		if err := json.NewEncoder(app.Out).Encode(payload); err != nil {
			return 1
		}
	} else {
		app.renderDoctor(checks, optionalChecks, consentAccepted, combinedReady, historyPath)
	}
	if !ready {
		return 1
	}
	return 0
}

// fail reports an error: one JSON document on stdout in JSON mode, a plain
// line on stderr otherwise.
func (app *App) fail(jsonMode bool, code, message string, exitCode int) int {
	if jsonMode {
		_ = json.NewEncoder(app.Out).Encode(map[string]any{
			"error": map[string]string{"code": code, "message": message},
		})
	} else {
		fmt.Fprintf(app.Err, "soundprobe: %s\n", message)
	}
	return exitCode
}

func (app *App) writeValue(jsonMode bool, value any, plain string) int {
	if jsonMode {
		if err := json.NewEncoder(app.Out).Encode(value); err != nil {
			return 1
		}
		return 0
	}
	if plain != "" {
		fmt.Fprintln(app.Out, plain)
	}
	return 0
}

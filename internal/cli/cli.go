package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/soundadam/soundprobe/internal/consent"
	"github.com/soundadam/soundprobe/internal/exporter"
	"github.com/soundadam/soundprobe/internal/model"
	"github.com/soundadam/soundprobe/internal/preferences"
	"github.com/soundadam/soundprobe/internal/provider"
	"github.com/soundadam/soundprobe/internal/provider/ookla"
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

// runMeasurement validates the parsed options, resolves the target plan, and
// executes it.
func (app *App) runMeasurement(ctx context.Context, command model.Command, options commandOptions, jsonMode bool) int {
	if err := options.normalize(); err != nil {
		return app.fail(jsonMode, "invalid_arguments", err.Error(), 1)
	}
	plan, err := resolvePlan(command, options)
	if err != nil {
		return app.fail(jsonMode, "invalid_arguments", err.Error(), 1)
	}
	return app.executeMeasurementPlan(ctx, command, options, plan, jsonMode)
}

func (app *App) executeMeasurementPlan(ctx context.Context, command model.Command, options commandOptions, plan target.Plan, jsonMode bool) int {
	if app.Runner == nil {
		return app.fail(jsonMode, "internal_error", "measurement runner is not configured", 1)
	}
	request := provider.Request{
		Command: command,
		Targets: append([]model.Provider(nil), plan.Providers...),
		Label:   optionalString(options.label),
		Note:    optionalString(options.note),
	}
	initialRequest := request
	requestedProviders := append([]model.Provider(nil), request.Targets...)
	prepared := false
	if preparer, ok := app.Runner.(provider.RequestPreparer); ok {
		var err error
		request, err = preparer.Prepare(ctx, request)
		if err != nil && command == model.CommandOokla && !jsonMode && app.StdinTTY && app.StdoutTTY && errors.Is(err, provider.ErrUnavailable) {
			repaired, repairErr := app.offerOoklaInstall(ctx, err)
			if repairErr != nil {
				return app.fail(false, "measurement_unavailable", repairErr.Error(), 1)
			}
			if repaired {
				// The repair is optional.  If the user declines, repeat the original
				// error below; if it succeeds, preflight again against the newly
				// installed helper before opening the progress renderer.
				request = initialRequest
				request, err = preparer.Prepare(ctx, request)
			}
		}
		if err != nil {
			return app.fail(jsonMode, measurementErrorCode(err), err.Error(), 1)
		}
		prepared = true
		if !jsonMode {
			for _, requested := range requestedProviders {
				if containsProvider(request.Targets, requested) {
					continue
				}
				fmt.Fprintf(app.Err, "soundprobe: optional target %s is unavailable; continuing without it (see `soundprobe doctor --json`)\n", target.Label(requested))
			}
		}
		// Optional helpers may be removed during preparation. Keep consent,
		// progress, and the persisted target order aligned with the actual run.
		plan.Providers = append([]model.Provider(nil), request.Targets...)
		plan.StationIDs = target.StationIDs(request.Targets)
	}
	if !prepared {
		if preflight, ok := app.Runner.(provider.PreflightRunner); ok {
			if err := preflight.Preflight(ctx, request); err != nil {
				return app.fail(jsonMode, measurementErrorCode(err), err.Error(), 1)
			}
		}
	}
	if target.NeedsMLab(plan.Providers) {
		if exitCode := app.ensureMLabConsent(jsonMode); exitCode != 0 {
			return exitCode
		}
	}

	var progress progressRenderer
	var err error
	if app.StdoutTTY && !jsonMode {
		progress, err = app.ProgressFactory(app.Out, app.Version, plan.Providers)
		if err != nil {
			return app.fail(false, "renderer_error", fmt.Sprintf("start interactive renderer: %v", err), 1)
		}
		request.Progress = progress.Update
	}
	closeProgress := func() error {
		if progress == nil {
			return nil
		}
		err := progress.Close()
		progress = nil
		return err
	}

	summary, runErr := app.Runner.Run(ctx, request)
	if runErr != nil {
		_ = closeProgress()
		return app.fail(jsonMode, measurementErrorCode(runErr), runErr.Error(), 1)
	}
	if summary.SchemaVersion == 0 {
		summary.SchemaVersion = model.SchemaVersion
	}
	if summary.ToolVersion == "" {
		summary.ToolVersion = app.Version
	}
	if summary.Command == "" {
		summary.Command = command
	}
	if len(summary.Targets) == 0 {
		summary.Targets = append([]model.Provider(nil), plan.Providers...)
	}
	if summary.Status == "" {
		summary.Status = model.DeriveRunStatus(summary.Measurements)
	}

	if !options.noSave {
		if app.History == nil {
			_ = closeProgress()
			return app.fail(jsonMode, "storage_error", "history store is not configured", 1)
		}
		if err := app.History.Save(summary); err != nil {
			_ = closeProgress()
			return app.fail(jsonMode, "storage_error", err.Error(), 1)
		}
	}
	if err := closeProgress(); err != nil {
		return app.fail(jsonMode, "renderer_error", err.Error(), 1)
	}
	if jsonMode {
		if err := json.NewEncoder(app.Out).Encode(summary); err != nil {
			return 1
		}
	} else {
		app.renderSummary(summary)
	}
	return summary.ExitCode()
}

func runCommand(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

// offerOoklaInstall provides a narrow, explicit repair path for the common
// case where Homebrew's Python speedtest-cli occupies the speedtest name.
// soundprobe never installs this helper during a combined/default run, never
// executes a shell, and never removes an existing formula automatically.
func (app *App) offerOoklaInstall(ctx context.Context, cause error) (bool, error) {
	fmt.Fprintf(app.Out, "Ookla provider unavailable: %s\n\n", cause)
	fmt.Fprintln(app.Out, "soundprobe does not maintain or bundle the Ookla protocol.")
	fmt.Fprintf(app.Out, "Official download: %s\n", ookla.OfficialInstallURL)

	if _, err := app.LookupCommand("brew"); err != nil {
		fmt.Fprintln(app.Out, "Homebrew was not found, so no command will be run automatically.")
		fmt.Fprintln(app.Out, "Install the official CLI from the page above, then retry `soundprobe ookla`.")
		return false, nil
	}

	fmt.Fprintln(app.Out, "Detected Homebrew.  The following official setup commands will run only after you press Enter:")
	for _, command := range ookla.HomebrewInstallCommands() {
		fmt.Fprintf(app.Out, "  %s\n", formatCommand(command))
	}
	fmt.Fprintln(app.Out, "Existing speedtest/speedtest-cli packages will not be uninstalled automatically.")
	fmt.Fprintln(app.Out, "Press Enter to execute, or any other key then Enter to cancel:")

	reader := bufio.NewReader(app.In)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read Ookla install choice: %w", err)
	}
	if strings.TrimRight(answer, "\r\n") != "" {
		fmt.Fprintln(app.Out, "Ookla installation cancelled.")
		return false, nil
	}

	installCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	for _, command := range ookla.HomebrewInstallCommands() {
		if err := app.RunCommand(installCtx, command[0], command[1:], app.Out, app.Err); err != nil {
			fmt.Fprintf(app.Out, "Official Ookla installation failed: %v\n", err)
			fmt.Fprintln(app.Out, "If Homebrew reports a conflict and you have confirmed it is safe, run manually:")
			for _, recovery := range ookla.HomebrewConflictCommands() {
				fmt.Fprintf(app.Out, "  %s\n", formatCommand(recovery))
			}
			return false, nil
		}
	}
	fmt.Fprintln(app.Out, "Official Ookla CLI installation completed; checking it now.")
	return true, nil
}

func formatCommand(command []string) string {
	parts := make([]string, 0, len(command))
	for _, part := range command {
		if strings.ContainsAny(part, " \t\n\"'") {
			parts = append(parts, fmt.Sprintf("%q", part))
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}

// resolvePlan turns a command and its flags into an ordered target plan.  The
// station registry owns which stations a command runs and which it accepts; the
// CLI only supplies the explicit --targets override and the address family.
func resolvePlan(command model.Command, options commandOptions) (target.Plan, error) {
	var ids []string
	if strings.TrimSpace(options.targets) != "" {
		ids = splitCommaList(options.targets)
	}
	return target.PlanForCommand(command, ids, target.Family(options.family))
}

func splitCommaList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func (app *App) executeStations(ctx context.Context, jsonMode bool) int {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	results := target.ProbeAll(probeCtx, 1500*time.Millisecond)
	cancel()
	if jsonMode {
		if err := json.NewEncoder(app.Out).Encode(results); err != nil {
			return 1
		}
		return 0
	}
	out, styles := app.humanOutput()
	rows := [][]string{{
		styles.Header("STATION"),
		styles.Header("FAMILY"),
		styles.Header("STATUS"),
		styles.Header("LATENCY"),
		styles.Header("DETAIL"),
	}}
	for _, result := range results {
		latency := "—"
		if result.LatencyMS != nil {
			latency = fmt.Sprintf("%.0f ms", *result.LatencyMS)
		}
		rows = append(rows, []string{
			result.StationID,
			fmt.Sprint(result.Family),
			styles.Status(string(result.Status)),
			latency,
			styles.Dim(result.Message),
		})
	}
	writeAligned(out, rows)
	return 0
}

// readHistory lists saved runs newest first, skipping every file this build
// cannot read instead of failing the whole command.  History accumulates across
// releases and is never pruned automatically, so a summary written by an older,
// incompatible schema version must not hide every other run.  Unreadable files
// are left untouched on disk and reported once on stderr, which keeps `--json`
// stdout a single document.
func (app *App) readHistory(limit int) ([]model.RunSummary, []string, error) {
	if app.History == nil || app.History.HistoryDir == "" {
		return nil, nil, errors.New("history store is not configured")
	}
	if limit < 0 {
		return nil, nil, errors.New("history limit must be non-negative")
	}
	entries, err := os.ReadDir(app.History.HistoryDir)
	if errors.Is(err, fs.ErrNotExist) {
		return []model.RunSummary{}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read history directory: %w", err)
	}
	summaries := make([]model.RunSummary, 0, len(entries))
	unreadable := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		summary, err := app.History.Load(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			unreadable = append(unreadable, entry.Name())
			continue
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(first, second int) bool {
		return summaries[first].StartedAt.After(summaries[second].StartedAt)
	})
	if limit > 0 && len(summaries) > limit {
		summaries = summaries[:limit]
	}
	return summaries, unreadable, nil
}

func (app *App) reportUnreadableHistory(unreadable []string) {
	if len(unreadable) == 0 {
		return
	}
	shown := unreadable
	suffix := ""
	if len(shown) > 3 {
		shown = shown[:3]
		suffix = fmt.Sprintf(" and %d more", len(unreadable)-3)
	}
	fmt.Fprintf(app.Err, "soundprobe: skipped %d unreadable history file(s), schema version %d is required: %s%s\n",
		len(unreadable), model.SchemaVersion, strings.Join(shown, ", "), suffix)
}

func (app *App) executeHistory(limit int, jsonMode bool) int {
	summaries, unreadable, err := app.readHistory(limit)
	if err != nil {
		return app.fail(jsonMode, "storage_error", err.Error(), 1)
	}
	app.reportUnreadableHistory(unreadable)
	if jsonMode {
		if err := json.NewEncoder(app.Out).Encode(summaries); err != nil {
			return 1
		}
		return 0
	}
	app.renderHistory(summaries)
	return 0
}

func (app *App) executeLast(jsonMode bool) int {
	summaries, unreadable, err := app.readHistory(1)
	if err != nil {
		return app.fail(jsonMode, "storage_error", err.Error(), 1)
	}
	app.reportUnreadableHistory(unreadable)
	if len(summaries) == 0 {
		return app.fail(jsonMode, "no_history", "no saved runs", 1)
	}
	if jsonMode {
		if err := json.NewEncoder(app.Out).Encode(summaries[0]); err != nil {
			return 1
		}
	} else {
		app.renderSummary(summaries[0])
	}
	return 0
}

func (app *App) executeShow(runID string, jsonMode bool) int {
	if app.History == nil {
		return app.fail(jsonMode, "storage_error", "history store is not configured", 1)
	}
	summary, err := app.History.Load(runID)
	if err != nil {
		return app.fail(jsonMode, "storage_error", err.Error(), 1)
	}
	if jsonMode {
		if err := json.NewEncoder(app.Out).Encode(summary); err != nil {
			return 1
		}
	} else {
		app.renderSummary(summary)
	}
	return 0
}

func (app *App) executeExport(format, output string, jsonMode bool) int {
	if (format != "jsonl" && format != "csv") || output == "" {
		return app.fail(jsonMode, "invalid_arguments", "export requires --format jsonl|csv and --output PATH", 1)
	}
	summaries, unreadable, err := app.readHistory(0)
	if err != nil {
		return app.fail(jsonMode, "storage_error", err.Error(), 1)
	}
	app.reportUnreadableHistory(unreadable)
	if err := exporter.Write(output, format, summaries); err != nil {
		return app.fail(jsonMode, "export_error", err.Error(), 1)
	}
	return app.writeValue(jsonMode, map[string]any{
		"format": format,
		"output": output,
		"runs":   len(summaries),
	}, fmt.Sprintf("Exported %d runs to %s", len(summaries), output))
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

func (app *App) renderDoctor(checks, optionalChecks map[string]string, consentAccepted, combinedReady bool, historyPath string) {
	out, styles := app.humanOutput()
	readiness := func(value string, optional bool) string {
		if value == "ready" {
			return styles.OK(value)
		}
		if optional {
			return styles.Warn(value)
		}
		return styles.Bad(value)
	}
	boolWord := func(value bool) string {
		if value {
			return styles.OK("true")
		}
		return styles.Warn("false")
	}
	fmt.Fprintln(out, styles.Title(fmt.Sprintf("soundprobe %s diagnostics", app.Version)))
	writeAligned(out, [][]string{
		{"Campus", readiness(checks["campus"], false)},
		{"M-Lab", readiness(checks["mlab"], false)},
		{"Apple", readiness(optionalChecks["apple"], true)},
		{"Ookla", readiness(optionalChecks["ookla"], true)},
		{"Consent", boolWord(consentAccepted)},
		{"Combined", boolWord(combinedReady)},
		{"History", styles.Dim(historyPath)},
	})
}

func (app *App) executeConsentStatus(jsonMode bool) int {
	if app.Consent == nil {
		return app.fail(jsonMode, "consent_error", "consent store is not configured", 1)
	}
	record, accepted, err := app.Consent.Status()
	if err != nil {
		return app.fail(jsonMode, "consent_error", err.Error(), 1)
	}
	if jsonMode {
		payload := map[string]any{
			"accepted":      accepted,
			"policyVersion": consent.PolicyVersion,
			"policyUrl":     consent.PolicyURL,
		}
		if !record.AcceptedAt.IsZero() {
			payload["record"] = record
		}
		return app.writeValue(true, payload, "")
	}
	out, styles := app.humanOutput()
	if accepted {
		fmt.Fprintln(out, styles.OK(fmt.Sprintf("M-Lab consent accepted (%s at %s).", record.PolicyVersion, record.AcceptedAt.Format(time.RFC3339))))
	} else {
		fmt.Fprintln(out, styles.Warn(fmt.Sprintf("M-Lab consent is not accepted for current policy %s.", consent.PolicyVersion)))
	}
	fmt.Fprintf(out, "Policy: %s\n", styles.Accent(consent.PolicyURL))
	return 0
}

func (app *App) executeConsentAccept(jsonMode bool) int {
	if app.Consent == nil {
		return app.fail(jsonMode, "consent_error", "consent store is not configured", 1)
	}
	if jsonMode {
		return app.fail(true, "consent_requires_interaction", "consent accept is interactive and unavailable in JSON mode", 1)
	}
	return app.promptAndAcceptConsent(false)
}

func (app *App) executeConsentRevoke(jsonMode bool) int {
	if app.Consent == nil {
		return app.fail(jsonMode, "consent_error", "consent store is not configured", 1)
	}
	if err := app.Consent.Revoke(); err != nil {
		return app.fail(jsonMode, "consent_error", err.Error(), 1)
	}
	return app.writeValue(jsonMode, map[string]bool{"revoked": true}, "M-Lab consent revoked.")
}

func (app *App) ensureMLabConsent(jsonMode bool) int {
	if app.Consent == nil {
		return app.fail(jsonMode, "consent_error", "consent store is not configured", 1)
	}
	_, accepted, err := app.Consent.Status()
	if err != nil {
		return app.fail(jsonMode, "consent_error", err.Error(), 1)
	}
	if accepted {
		return 0
	}
	if jsonMode || !app.StdinTTY {
		return app.fail(jsonMode, "consent_required", "M-Lab consent is required; run `soundprobe consent accept` interactively", 1)
	}
	return app.promptAndAcceptConsent(jsonMode)
}

func (app *App) promptAndAcceptConsent(jsonMode bool) int {
	if !app.StdinTTY {
		return app.fail(jsonMode, "consent_requires_interaction", "consent acceptance requires an interactive terminal", 1)
	}
	out, styles := app.humanOutput()
	fmt.Fprintln(out, styles.Title("M-Lab measurement consent"))
	fmt.Fprintln(out, "M-Lab collects the ISP-provided public IP address and measurement results.")
	fmt.Fprintln(out, "M-Lab publishes and retains experiment data indefinitely.")
	fmt.Fprintf(out, "Policy %s: %s\n", consent.PolicyVersion, styles.Accent(consent.PolicyURL))
	fmt.Fprint(out, styles.Title("Type accept to continue: "))
	scanner := bufio.NewScanner(app.In)
	if !scanner.Scan() {
		return app.fail(jsonMode, "consent_declined", "consent was not accepted", 1)
	}
	if strings.TrimSpace(scanner.Text()) != "accept" {
		return app.fail(jsonMode, "consent_declined", "consent was not accepted", 1)
	}
	record, err := app.Consent.Accept(app.Version, app.Now())
	if err != nil {
		return app.fail(jsonMode, "consent_error", err.Error(), 1)
	}
	fmt.Fprintln(out, styles.OK(fmt.Sprintf("M-Lab consent recorded for policy %s.", record.PolicyVersion)))
	return 0
}

func (app *App) renderSummary(summary model.RunSummary) {
	out, styles := app.humanOutput()
	fmt.Fprintf(out, "%s · %s · %s\n",
		styles.Title("soundprobe "+summary.ToolVersion),
		styles.Status(string(summary.Status)),
		styles.Dim(formatDuration(summary.EndedAt.Sub(summary.StartedAt))),
	)
	fmt.Fprintln(out, styles.Dim("Run "+summary.RunID))
	if network := formatNetworkContext(summary.Network); network != "" {
		fmt.Fprintln(out, styles.Dim("Network "+network))
	}
	rows := [][]string{{
		styles.Header("TARGET"),
		styles.Header("METHOD"),
		styles.Header("DOWNLOAD"),
		styles.Header("UPLOAD"),
		styles.Header("SERVER"),
		styles.Header("STATUS"),
	}}
	for _, measurement := range summary.Measurements {
		rows = append(rows, []string{
			target.Label(measurement.Provider),
			styles.Dim(fmt.Sprint(measurement.Method)),
			formatMbps(measurement.DownloadMbps),
			formatMbps(measurement.UploadMbps),
			styles.Dim(measurementServer(measurement)),
			styles.Status(string(measurement.Status)),
		})
	}
	writeAligned(out, rows)
	for _, measurement := range summary.Measurements {
		if measurement.Failure != nil {
			fmt.Fprintln(out, styles.Bad(fmt.Sprintf("%s error [%s/%s]: %s",
				measurement.Provider,
				measurement.Failure.Stage,
				measurement.Failure.Code,
				measurement.Failure.Message,
			)))
		}
	}
}

func (app *App) renderHistory(summaries []model.RunSummary) {
	out, styles := app.humanOutput()
	if len(summaries) == 0 {
		fmt.Fprintln(out, "No saved runs.")
		return
	}
	rows := [][]string{{
		styles.Header("RUN ID"),
		styles.Header("STARTED"),
		styles.Header("COMMAND"),
		styles.Header("STATUS"),
		styles.Header("LABEL"),
	}}
	for _, summary := range summaries {
		rows = append(rows, []string{
			summary.RunID,
			summary.StartedAt.Local().Format("2006-01-02 15:04:05"),
			string(summary.Command),
			styles.Status(string(summary.Status)),
			valueOrEmpty(summary.Label),
		})
	}
	writeAligned(out, rows)
}

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

func measurementErrorCode(err error) string {
	if errors.Is(err, provider.ErrUnavailable) {
		return "measurement_unavailable"
	}
	return "measurement_error"
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func containsProvider(providers []model.Provider, wanted model.Provider) bool {
	for _, provider := range providers {
		if provider == wanted {
			return true
		}
	}
	return false
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func formatMbps(value *float64) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f Mbps", *value)
}

func formatNetworkContext(network model.NetworkContext) string {
	parts := make([]string, 0, 3)
	if network.ActiveInterface != nil && *network.ActiveInterface != "" {
		parts = append(parts, *network.ActiveInterface)
	}
	if network.InterfaceKind != nil && *network.InterfaceKind != "" {
		parts = append(parts, *network.InterfaceKind)
	}
	if network.SSID != nil && *network.SSID != "" {
		parts = append(parts, *network.SSID)
	}
	return strings.Join(parts, " · ")
}

func measurementServer(measurement model.Measurement) string {
	sponsor := valueOrEmpty(measurement.ServerSponsor)
	if measurement.ServerFQDN != nil && *measurement.ServerFQDN != "" {
		if sponsor != "" {
			return *measurement.ServerFQDN + " · " + sponsor
		}
		return *measurement.ServerFQDN
	}
	if measurement.ServerName != nil && *measurement.ServerName != "" {
		if sponsor != "" && *measurement.ServerName != sponsor {
			return *measurement.ServerName + " · " + sponsor
		}
		return *measurement.ServerName
	}
	if measurement.ServerAddress != nil && *measurement.ServerAddress != "" {
		return *measurement.ServerAddress
	}
	return "—"
}

func formatDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	if duration < time.Second {
		return fmt.Sprintf("%d ms", duration.Milliseconds())
	}
	return duration.Round(100 * time.Millisecond).String()
}

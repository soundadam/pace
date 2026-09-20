package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/soundadam/soundprobe/internal/model"
	"github.com/soundadam/soundprobe/internal/provider"
	"github.com/soundadam/soundprobe/internal/target"
)

// runMeasurement validates the parsed options, resolves the target plan, and
// executes it.  The station registry owns which stations a command runs and
// which it accepts; the CLI only supplies the explicit --targets override and
// the address family.
func (app *App) runMeasurement(ctx context.Context, command model.Command, options commandOptions, jsonMode bool) int {
	if err := options.normalize(); err != nil {
		return app.fail(jsonMode, "invalid_arguments", err.Error(), 1)
	}
	var ids []string
	if strings.TrimSpace(options.targets) != "" {
		ids = splitCommaList(options.targets)
	}
	plan, err := target.PlanForCommand(command, ids, target.Family(options.family))
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

func (app *App) executeStations(ctx context.Context, jsonMode bool) int {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	results := app.ProbeStations(probeCtx, 1500*time.Millisecond)
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

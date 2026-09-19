package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/soundadam/soundprobe/internal/exporter"
	"github.com/soundadam/soundprobe/internal/model"
)

// The history commands read whatever this build can read and say so when a file
// is skipped.  storage.Store.List owns the skipping; the CLI only decides where
// the warning goes, which is always stderr so that `--json` keeps a single
// document on stdout.

// reportUnreadableHistory names the files List skipped, on stderr.
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
	summaries, unreadable, err := app.History.List(limit)
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
	summaries, unreadable, err := app.History.List(1)
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

// executeShow fails when the named run is unreadable.  A run the user asked for
// by ID is the one case with nothing to degrade to, so reporting the error
// beats printing nothing.
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
	summaries, unreadable, err := app.History.List(0)
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

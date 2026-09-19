package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/soundadam/soundprobe/internal/model"
	"github.com/soundadam/soundprobe/internal/target"
)

// This file holds every human-readable renderer and the value formatters they
// share.  JSON output is produced at the call sites; nothing here writes to
// stdout in JSON mode.

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

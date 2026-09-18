package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/soundadam/soundprobe/internal/model"
	"github.com/soundadam/soundprobe/internal/target"
)

// Help groups shown by `soundprobe --help`.
const (
	groupMeasure = "measure"
	groupRecords = "records"
	groupConfig  = "config"
)

// execution carries per-invocation state across cobra handlers. Command
// handlers record their exit code here and return nil, so cobra-level errors
// are limited to argument and flag parsing.
type execution struct {
	app  *App
	json bool
	code int
}

// Execute parses args and runs the selected command, returning the process
// exit code. Measurement commands return summary.ExitCode(), cancellations
// return 130, and failures return 1.
func (app *App) Execute(ctx context.Context, args []string) int {
	app.setDefaults()
	args, jsonMode := extractGlobalJSON(args)
	state := &execution{app: app, json: jsonMode}
	root := app.newRootCommand(state)
	root.SetArgs(args)
	root.SetIn(app.In)
	root.SetOut(app.Out)
	root.SetErr(app.Err)
	if err := fang.Execute(ctx, root,
		fang.WithVersion(app.Version),
		fang.WithErrorHandler(state.handleError),
	); err != nil {
		return 1
	}
	return state.code
}

// handleError renders cobra parsing errors (unknown commands, bad flags,
// unexpected arguments). In JSON mode it emits the machine-readable error
// document on stdout; otherwise fang prints a styled message on stderr.
func (state *execution) handleError(w io.Writer, styles fang.Styles, err error) {
	if state.json {
		code := "invalid_arguments"
		if strings.HasPrefix(err.Error(), "unknown command") {
			code = "unknown_command"
		}
		// Keep the machine-readable message single-line; suggestions such as
		// "Did you mean this?" belong to the styled human output only.
		message, _, _ := strings.Cut(err.Error(), "\n")
		state.app.fail(true, code, message, 1)
		return
	}
	fang.DefaultErrorHandler(w, styles, err)
}

func (app *App) newRootCommand(state *execution) *cobra.Command {
	root := &cobra.Command{
		Use:   "soundprobe",
		Short: "Education-network-first speed tests with public references",
		Long: `soundprobe measures education-network paths first, with public references.

Run it without arguments in a terminal to pick stations interactively. In
scripts and pipes it runs the default plan (campus + M-Lab + Apple), and the
global --json flag prints a single machine-readable document on stdout.`,
		Example: `  # interactive station picker (TTY) or the default plan (scripts)
  soundprobe

  # measure specific stations over both address families
  soundprobe run --targets nju-campus,qlu --family dual

  # machine-readable output for automation
  soundprobe run --json --no-save`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			state.code = app.executeBare(cmd.Context(), state.json)
			return nil
		},
	}
	root.PersistentFlags().BoolVar(&state.json, "json", state.json, "print a single JSON document on stdout")
	root.AddGroup(
		&cobra.Group{ID: groupMeasure, Title: "measure"},
		&cobra.Group{ID: groupRecords, Title: "results & history"},
		&cobra.Group{ID: groupConfig, Title: "setup & diagnostics"},
	)

	root.AddCommand(
		app.newMeasureCommand(state, model.CommandRun,
			"Run the default measurement plan",
			`Run measures the default plan: the NJU campus station, M-Lab NDT7, and
Apple networkQuality. Use --targets and --family to measure any combination
of stations instead (list IDs with "soundprobe stations").`,
			`  # default plan (campus + M-Lab + Apple)
  soundprobe run

  # pick stations and address families explicitly
  soundprobe run --targets nju-campus,qlu --family dual

  # label a run without saving it to history
  soundprobe run --label dorm-wifi --no-save`),
		app.newMeasureCommand(state, model.CommandCampus,
			"Measure the NJU campus station",
			`Campus measures the on-campus NJU speed station. The IPv4 service is used
by default; pass --ipv6 to use the IPv6 service instead.`,
			`  soundprobe campus
  soundprobe campus --ipv6 --label dorm-wifi`),
		app.newMeasureCommand(state, model.CommandEdge,
			"Measure the NJU edge station",
			`Edge targets the NJU edge speed station. This station is currently
web-only; the command reports the browser URLs to use instead.`,
			`  soundprobe edge`),
		app.newMeasureCommand(state, model.CommandDomestic,
			"Measure domestic education stations",
			`Domestic measures publicly reachable domestic education stations over
IPv4. It defaults to tongji and qlu; restrict or reorder the set with
--targets (cernet, qlu, tongji).`,
			`  soundprobe domestic
  soundprobe domestic --targets tongji`),
		app.newMeasureCommand(state, model.CommandMLab,
			"Measure with M-Lab NDT7",
			`MLab runs an NDT7 measurement against Measurement Lab. M-Lab publishes
measurement data, so a one-time consent is required; grant it with
"soundprobe consent accept".`,
			`  soundprobe mlab
  soundprobe mlab --note "after maintenance"`),
		app.newMeasureCommand(state, model.CommandApple,
			"Measure with Apple networkQuality",
			`Apple runs the networkQuality helper that ships with macOS and reports
its responsiveness-oriented results alongside throughput.`,
			`  soundprobe apple`),
		app.newMeasureCommand(state, model.CommandOokla,
			"Measure with the official Ookla Speedtest CLI",
			`Ookla runs the official Ookla Speedtest CLI if it is installed. When the
helper is missing, soundprobe explains how to install it and, on Homebrew
systems, offers to run the official setup commands after confirmation.`,
			`  soundprobe ookla`),
		app.newStationsCommand(state),
		app.newHistoryCommand(state),
		app.newLastCommand(state),
		app.newShowCommand(state),
		app.newExportCommand(state),
		app.newConsentCommand(state),
		app.newSetupCommand(state),
		app.newDoctorCommand(state),
		app.newVersionCommand(state),
	)
	root.SetHelpCommandGroupID(groupConfig)
	root.SetCompletionCommandGroupID(groupConfig)
	return root
}

func (app *App) newMeasureCommand(state *execution, command model.Command, short, long, example string) *cobra.Command {
	options := &commandOptions{family: string(target.FamilyIPv4)}
	cmd := &cobra.Command{
		Use:     string(command),
		Short:   short,
		Long:    long,
		Example: example,
		GroupID: groupMeasure,
		Args:    noUnexpectedArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			state.code = app.runMeasurement(cmd.Context(), command, *options, state.json)
			return nil
		},
	}
	flags := cmd.Flags()
	if command == model.CommandRun || command == model.CommandDomestic {
		flags.StringVar(&options.targets, "targets", "", `comma-separated station IDs (see "soundprobe stations")`)
		flags.StringVar(&options.family, "family", string(target.FamilyIPv4), "address family: ipv4, ipv6, or dual")
	}
	if command == model.CommandCampus || command == model.CommandEdge {
		flags.BoolVar(&options.ipv4, "ipv4", false, "use the IPv4 service")
		flags.BoolVar(&options.ipv6, "ipv6", false, "use the IPv6 service")
	}
	flags.StringVar(&options.label, "label", "", "attach a short label to the saved run")
	flags.StringVar(&options.note, "note", "", "attach a free-form note to the saved run")
	flags.BoolVar(&options.noSave, "no-save", false, "do not persist the result to history")
	return cmd
}

func (app *App) newStationsCommand(state *execution) *cobra.Command {
	return &cobra.Command{
		Use:   "stations",
		Short: "List stations and probe reachability",
		Long: `Stations lists every known speed station and probes its reachability from
the current network, including stations that are web-only or automatic.`,
		GroupID: groupMeasure,
		Args:    rejectArgs("stations does not accept arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			state.code = app.executeStations(cmd.Context(), state.json)
			return nil
		},
	}
}

func (app *App) newHistoryCommand(state *execution) *cobra.Command {
	limit := 20
	cmd := &cobra.Command{
		Use:     "history",
		Short:   "List saved runs",
		Long:    `History lists saved runs, newest first.`,
		Example: `  soundprobe history --limit 5`,
		GroupID: groupRecords,
		Args:    rejectArgs("history does not accept positional arguments"),
		RunE: func(*cobra.Command, []string) error {
			state.code = app.executeHistory(limit, state.json)
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum number of runs; zero means all")
	return cmd
}

func (app *App) newLastCommand(state *execution) *cobra.Command {
	return &cobra.Command{
		Use:     "last",
		Short:   "Show the most recent saved run",
		Long:    `Last renders the most recent saved run.`,
		GroupID: groupRecords,
		Args:    rejectArgs("last does not accept arguments"),
		RunE: func(*cobra.Command, []string) error {
			state.code = app.executeLast(state.json)
			return nil
		},
	}
}

func (app *App) newShowCommand(state *execution) *cobra.Command {
	return &cobra.Command{
		Use:     "show RUN_ID",
		Short:   "Show a saved run by ID",
		Long:    `Show renders a saved run identified by its RUN_ID (see "soundprobe history").`,
		GroupID: groupRecords,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("show requires exactly one RUN_ID")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			state.code = app.executeShow(args[0], state.json)
			return nil
		},
	}
}

func (app *App) newExportCommand(state *execution) *cobra.Command {
	var format, output string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export saved runs as JSONL or CSV",
		Long:  `Export writes every saved run to a file, one of --format jsonl or csv.`,
		Example: `  soundprobe export --format jsonl --output runs.jsonl
  soundprobe export --format csv --output runs.csv`,
		GroupID: groupRecords,
		Args:    rejectArgs("export requires --format jsonl|csv and --output PATH"),
		RunE: func(*cobra.Command, []string) error {
			state.code = app.executeExport(format, output, state.json)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "export format: jsonl or csv")
	cmd.Flags().StringVar(&output, "output", "", "destination file path")
	return cmd
}

func (app *App) newConsentCommand(state *execution) *cobra.Command {
	consentArgs := rejectArgs("consent requires status, accept, or revoke")
	cmd := &cobra.Command{
		Use:   "consent",
		Short: "Manage M-Lab measurement consent",
		Long: `Consent manages the one-time M-Lab data-collection consent. M-Lab collects
the ISP-provided public IP address and measurement results, and publishes
and retains experiment data indefinitely.`,
		GroupID: groupConfig,
		RunE: func(*cobra.Command, []string) error {
			state.code = app.fail(state.json, "invalid_arguments", "consent requires status, accept, or revoke", 1)
			return nil
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show the current consent state",
			Args:  consentArgs,
			RunE: func(*cobra.Command, []string) error {
				state.code = app.executeConsentStatus(state.json)
				return nil
			},
		},
		&cobra.Command{
			Use:   "accept",
			Short: "Review the policy and record consent",
			Args:  consentArgs,
			RunE: func(*cobra.Command, []string) error {
				state.code = app.executeConsentAccept(state.json)
				return nil
			},
		},
		&cobra.Command{
			Use:   "revoke",
			Short: "Revoke previously granted consent",
			Args:  consentArgs,
			RunE: func(*cobra.Command, []string) error {
				state.code = app.executeConsentRevoke(state.json)
				return nil
			},
		},
	)
	return cmd
}

func (app *App) newSetupCommand(state *execution) *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Configure daily stations",
		Long: `Setup opens the interactive first-run configuration to choose the
daily station plan. It requires a terminal.`,
		GroupID: groupConfig,
		Args:    rejectArgs("setup does not accept arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			state.code = app.executeSetup(cmd.Context(), state.json)
			return nil
		},
	}
}

func (app *App) newDoctorCommand(state *execution) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check helpers, consent, and configuration",
		Long: `Doctor verifies that measurement helpers are available, reports optional
providers, and shows consent and storage locations.`,
		GroupID: groupConfig,
		Args:    rejectArgs("doctor does not accept arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			state.code = app.executeDoctor(cmd.Context(), state.json)
			return nil
		},
	}
}

func (app *App) newVersionCommand(state *execution) *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Print the soundprobe version",
		GroupID: groupConfig,
		Args:    rejectArgs("version does not accept arguments"),
		RunE: func(*cobra.Command, []string) error {
			state.code = app.writeValue(state.json, map[string]string{"version": app.Version}, "soundprobe "+app.Version)
			return nil
		},
	}
}

// noUnexpectedArgs rejects positional arguments on measurement commands with
// the historical error message.
func noUnexpectedArgs(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("unexpected arguments: %s", strings.Join(args, " "))
}

// rejectArgs rejects any positional arguments with a fixed message.
func rejectArgs(message string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		return errors.New(message)
	}
}

package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/soundadam/soundprobe/internal/provider/ookla"
)

// runCommand executes a helper installation command, streaming its output to
// the caller's writers.  Tests replace App.RunCommand with a recorder.
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

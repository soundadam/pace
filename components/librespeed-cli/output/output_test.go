package output

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestStreamSeparation pins the routing rule the --progress-json contract
// depends on: machine-readable data goes to stdout and everything else to
// stderr, never the other way round.
func TestStreamSeparation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		quiet   bool
		debug   bool
		wantOut string
		wantUI  string
	}{
		{
			name:    "verbose",
			wantOut: "data\n",
			wantUI:  "ui\nerror\n\n",
		},
		{
			name:    "quiet suppresses only WriteUI",
			quiet:   true,
			wantOut: "data\n",
			wantUI:  "error\n\n",
		},
		{
			name:    "debug adds WriteDebug",
			debug:   true,
			wantOut: "data\n",
			wantUI:  "ui\ndebug\nerror\n\n",
		},
		{
			name:    "quiet and debug together",
			quiet:   true,
			debug:   true,
			wantOut: "data\n",
			wantUI:  "debug\nerror\n\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, ui bytes.Buffer
			writer := New(tc.debug, tc.quiet)
			writer.out, writer.ui = &out, &ui

			writer.WriteOut("%s\n", "data")
			writer.WriteUI("%s\n", "ui")
			writer.WriteDebug("%s\n", "debug")
			writer.WriteError("%s\n", "error")
			writer.WriteUIBlank()

			if out.String() != tc.wantOut {
				t.Errorf("stdout = %q, want %q", out.String(), tc.wantOut)
			}
			if ui.String() != tc.wantUI {
				t.Errorf("stderr = %q, want %q", ui.String(), tc.wantUI)
			}
		})
	}
}

// TestModeToggles covers the package-level setters used by the flag handling.
func TestModeToggles(t *testing.T) {
	defer func() {
		SetQuiet(false)
		SetDebug(false)
	}()

	SetQuiet(true)
	SetDebug(true)
	if !IsQuiet() {
		t.Error("IsQuiet() = false after SetQuiet(true)")
	}
	if !IsDebug() {
		t.Error("IsDebug() = false after SetDebug(true)")
	}

	SetQuiet(false)
	SetDebug(false)
	if IsQuiet() || IsDebug() {
		t.Error("toggles did not reset")
	}
}

// TestPackageLevelWrappers covers the package-level entry points, which are
// what the rest of the program actually calls.
func TestPackageLevelWrappers(t *testing.T) {
	var out, ui bytes.Buffer
	restore := Redirect(&out, &ui)
	defer restore()

	SetQuiet(false)
	SetDebug(true)
	defer SetDebug(false)

	WriteOut("out")
	WriteUI("ui")
	WriteDebug("debug")
	WriteError("error")
	WriteUIBlank()

	if out.String() != "out" {
		t.Errorf("stdout = %q, want %q", out.String(), "out")
	}
	if ui.String() != "uidebugerror\n" {
		t.Errorf("stderr = %q, want %q", ui.String(), "uidebugerror\n")
	}
}

// TestFatalExits pins the process contract of Fatal and Fatalf: the message
// goes to stderr with a trailing newline and the process exits with status 1.
// Several argument-validation paths (an unsupported --telemetry-level, an
// unreadable --ca-cert, no reachable server) end here, so the exit status is
// part of the CLI's interface.
func TestFatalExits(t *testing.T) {
	if mode := os.Getenv("LIBRESPEED_CLI_TEST_FATAL"); mode != "" {
		switch mode {
		case "fatal":
			Fatal("boom")
		case "fatalf":
			Fatalf("boom %d", 7)
		}
		return
	}

	for _, tc := range []struct {
		mode string
		want string
	}{
		{"fatal", "boom\n"},
		{"fatalf", "boom 7\n"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=TestFatalExits")
			command.Env = append(os.Environ(), "LIBRESPEED_CLI_TEST_FATAL="+tc.mode)
			var stderr bytes.Buffer
			command.Stderr = &stderr
			err := command.Run()

			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("subprocess error = %v, want an exit error", err)
			}
			if got := exitErr.ExitCode(); got != 1 {
				t.Errorf("exit code = %d, want 1", got)
			}
			if !strings.HasSuffix(stderr.String(), tc.want) {
				t.Errorf("stderr = %q, want it to end with %q", stderr.String(), tc.want)
			}
		})
	}
}

// TestRedirectRestores pins that the test seam leaves the package-level writer
// exactly as it found it.
func TestRedirectRestores(t *testing.T) {
	originalOut, originalUI := Default.out, Default.ui

	var out, ui bytes.Buffer
	restore := Redirect(&out, &ui)
	WriteOut("data")
	WriteError("error")

	if out.String() != "data" {
		t.Errorf("redirected stdout = %q, want %q", out.String(), "data")
	}
	if ui.String() != "error" {
		t.Errorf("redirected stderr = %q, want %q", ui.String(), "error")
	}

	restore()
	if Default.out != originalOut || Default.ui != originalUI {
		t.Error("Redirect did not restore the original writers")
	}
}

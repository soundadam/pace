package speedtest

import (
	"bytes"
	"net/http"
	"os"
	"testing"

	"github.com/gocarina/gocsv"
	"github.com/urfave/cli/v2"

	"github.com/librespeed/speedtest-cli/defs"
	"github.com/librespeed/speedtest-cli/output"
)

// testFlags mirrors the flag set declared in main.go. SpeedTest reads flags by
// name, so an unregistered flag would silently read back as a zero value and
// change behaviour (for example --concurrent defaulting to 0). Keep this in
// sync with main.go.
func testFlags() []cli.Flag {
	return []cli.Flag{
		cli.HelpFlag,
		&cli.BoolFlag{Name: defs.OptionVersion},
		&cli.BoolFlag{Name: defs.OptionIPv4, Aliases: []string{defs.OptionIPv4Alt}},
		&cli.BoolFlag{Name: defs.OptionIPv6, Aliases: []string{defs.OptionIPv6Alt}},
		&cli.BoolFlag{Name: defs.OptionNoDownload},
		&cli.BoolFlag{Name: defs.OptionNoUpload},
		&cli.BoolFlag{Name: defs.OptionNoICMP},
		&cli.IntFlag{Name: defs.OptionConcurrent, Value: 3},
		&cli.BoolFlag{Name: defs.OptionBytes},
		&cli.BoolFlag{Name: defs.OptionMebiBytes},
		&cli.StringFlag{Name: defs.OptionDistance, Value: "km"},
		&cli.BoolFlag{Name: defs.OptionShare},
		&cli.BoolFlag{Name: defs.OptionSimple},
		&cli.BoolFlag{Name: defs.OptionCSV},
		&cli.StringFlag{Name: defs.OptionCSVDelimiter, Value: ","},
		&cli.BoolFlag{Name: defs.OptionCSVHeader},
		&cli.BoolFlag{Name: defs.OptionJSON},
		&cli.BoolFlag{Name: defs.OptionProgressJSON},
		&cli.StringFlag{Name: defs.OptionProxy},
		&cli.BoolFlag{Name: defs.OptionList},
		&cli.IntSliceFlag{Name: defs.OptionServer},
		&cli.IntSliceFlag{Name: defs.OptionExclude},
		&cli.StringFlag{Name: defs.OptionServerJSON},
		&cli.StringFlag{Name: defs.OptionLocalJSON},
		&cli.StringFlag{Name: defs.OptionSource},
		&cli.StringFlag{Name: defs.OptionInterface},
		&cli.IntFlag{Name: defs.OptionTimeout, Value: 15},
		&cli.IntFlag{Name: defs.OptionDuration, Value: 15},
		&cli.IntFlag{Name: defs.OptionChunks, Value: 100},
		&cli.IntFlag{Name: defs.OptionUploadSize, Value: 1024},
		&cli.BoolFlag{Name: defs.OptionSecure},
		&cli.BoolFlag{Name: defs.OptionInsecure},
		&cli.StringFlag{Name: defs.OptionCACert},
		&cli.BoolFlag{Name: defs.OptionSkipCertVerify},
		&cli.BoolFlag{Name: defs.OptionNoPreAllocate},
		&cli.BoolFlag{Name: defs.OptionDebug, Aliases: []string{"verbose"}},
		&cli.StringFlag{Name: defs.OptionTelemetryJSON},
		&cli.StringFlag{Name: defs.OptionTelemetryLevel},
		&cli.StringFlag{Name: defs.OptionTelemetryServer},
		&cli.StringFlag{Name: defs.OptionTelemetryPath},
		&cli.StringFlag{Name: defs.OptionTelemetryShare},
		&cli.StringFlag{Name: defs.OptionTelemetryExtra},
		&cli.IntFlag{Name: defs.OptionFwmark, Value: 0},
	}
}

type runResult struct {
	// Out is everything the run routed to the stdout role, including the
	// bytes helper.go writes straight to os.Stdout.
	Out string
	// UI is everything the run routed to the stderr role: the
	// --progress-json event stream, errors and debug output.
	UI  string
	Err error
}

// runSpeedTest invokes the real SpeedTest action through urfave/cli with the
// given arguments, capturing both output streams and restoring every global
// SpeedTest mutates. Tests using it must not call t.Parallel.
func runSpeedTest(t *testing.T, args ...string) runResult {
	t.Helper()

	previousTransport := http.DefaultClient.Transport
	previousTimeout := http.DefaultClient.Timeout
	previousSeparator := gocsv.TagSeparator
	previousStdout := os.Stdout
	t.Cleanup(func() {
		http.DefaultClient.Transport = previousTransport
		http.DefaultClient.Timeout = previousTimeout
		gocsv.TagSeparator = previousSeparator
		os.Stdout = previousStdout
		output.SetQuiet(false)
		output.SetDebug(false)
	})

	// helper.go writes the final CSV/JSON document straight to os.Stdout, so
	// capture the file as well as the output package's writers.
	stdoutFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("create temp stdout: %v", err)
	}
	defer stdoutFile.Close()
	os.Stdout = stdoutFile

	var out, ui bytes.Buffer
	restore := output.Redirect(&out, &ui)

	app := &cli.App{
		Name:     "librespeed-cli",
		Action:   SpeedTest,
		HideHelp: true,
		Flags:    testFlags(),
		Writer:   &out,
	}
	runErr := app.Run(append([]string{"librespeed-cli"}, args...))
	restore()

	direct, err := os.ReadFile(stdoutFile.Name())
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}

	return runResult{Out: out.String() + string(direct), UI: ui.String(), Err: runErr}
}

// writeServerList writes a LibreSpeed server list to a temp file and returns
// its path, for use with --local-json.
func writeServerList(t *testing.T, body string) string {
	t.Helper()
	path := t.TempDir() + "/servers.json"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write server list: %v", err)
	}
	return path
}

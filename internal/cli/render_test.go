package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/soundadam/soundprobe/internal/model"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite internal/cli/testdata golden files")

// TestMain pins the process timezone to UTC.  renderHistory formats
// StartedAt with .Local(), so without this the golden files would only
// match on a machine whose TZ happens to be UTC.  The assignment happens
// before any test goroutine starts, so it is safe under -race.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\n--- got ---\n%s", path, err, got)
	}
	if got != string(want) {
		t.Fatalf("%s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

// renderApp returns an App whose human output lands in the returned buffer
// with styling disabled, which is how soundprobe renders into a pipe.
func renderApp(t *testing.T) (*App, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	app := &App{Out: out, Err: &bytes.Buffer{}, Version: "test", StdoutTTY: false}
	return app, out
}

func fixedTime() time.Time {
	return time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
}

// --- renderHistory -----------------------------------------------------

func TestRenderHistoryEmpty(t *testing.T) {
	app, out := renderApp(t)
	app.renderHistory(nil)
	if out.String() != "No saved runs.\n" {
		t.Fatalf("empty history output = %q", out.String())
	}
}

// The history table is the one place where run IDs, commands and labels of
// very different widths share a column.  The golden file pins the padding.
func TestRenderHistoryGolden(t *testing.T) {
	started := fixedTime()
	summaries := []model.RunSummary{
		{
			RunID:     "00000000-0000-4000-8000-000000000001",
			StartedAt: started,
			Command:   model.CommandRun,
			Status:    model.RunStatusSuccess,
			Label:     model.Pointer("dorm-wifi"),
		},
		{
			RunID:     "short-id",
			StartedAt: started.Add(90 * time.Minute),
			Command:   model.CommandDomestic,
			Status:    model.RunStatusPartial,
			Label:     nil,
		},
		{
			RunID:     "00000000-0000-4000-8000-000000000003",
			StartedAt: started.Add(3 * time.Hour),
			Command:   model.CommandMLab,
			Status:    model.RunStatusFailed,
			Label:     model.Pointer("a considerably longer label than the rest"),
		},
		{
			RunID:     "00000000-0000-4000-8000-000000000004",
			StartedAt: started.Add(4 * time.Hour),
			Command:   model.CommandOokla,
			Status:    model.RunStatusCancelled,
			Label:     model.Pointer(""),
		},
	}
	app, out := renderApp(t)
	app.renderHistory(summaries)
	assertGolden(t, "history_table.golden", out.String())
}

// --- renderDoctor ------------------------------------------------------

func TestRenderDoctorGolden(t *testing.T) {
	for _, test := range []struct {
		name            string
		checks          map[string]string
		optional        map[string]string
		consentAccepted bool
		combinedReady   bool
		historyPath     string
		golden          string
	}{
		{
			name:            "all ready",
			checks:          map[string]string{"campus": "ready", "mlab": "ready"},
			optional:        map[string]string{"apple": "ready", "ookla": "ready"},
			consentAccepted: true,
			combinedReady:   true,
			historyPath:     "/home/user/.config/soundprobe/history",
			golden:          "doctor_ready.golden",
		},
		{
			// A missing optional helper must read as a warning, not a
			// failure, and must not flip `ready`.
			name:            "missing optional helper",
			checks:          map[string]string{"campus": "ready", "mlab": "ready"},
			optional:        map[string]string{"apple": "networkQuality is only available on macOS", "ookla": "speedtest helper not found in PATH"},
			consentAccepted: false,
			combinedReady:   false,
			historyPath:     "/home/user/.config/soundprobe/history",
			golden:          "doctor_missing_helper.golden",
		},
		{
			// A required provider failing, and an empty history path: both
			// have burned us before by producing a ragged table.
			name:            "required provider down",
			checks:          map[string]string{"campus": "dial tcp 1.2.3.4:80: connect: network is unreachable", "mlab": "ready"},
			optional:        map[string]string{"apple": "ready", "ookla": "ready"},
			consentAccepted: true,
			combinedReady:   false,
			historyPath:     "",
			golden:          "doctor_provider_down.golden",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, out := renderApp(t)
			app.renderDoctor(test.checks, test.optional, test.consentAccepted, test.combinedReady, test.historyPath)
			assertGolden(t, test.golden, out.String())
		})
	}
}

// --- renderSummary -----------------------------------------------------

func TestRenderSummaryGolden(t *testing.T) {
	started := fixedTime()
	for _, test := range []struct {
		name    string
		summary model.RunSummary
		golden  string
	}{
		{
			// A partial run: one measurement carries a failure object, which
			// must be printed under the table, and the other has null
			// throughput, which must render as an em dash rather than 0.00.
			name: "partial with failure detail",
			summary: model.RunSummary{
				ToolVersion: "1.2.3",
				RunID:       "00000000-0000-4000-8000-000000000001",
				StartedAt:   started,
				EndedAt:     started.Add(12 * time.Second),
				Command:     model.CommandRun,
				Status:      model.RunStatusPartial,
				Network: model.NetworkContext{
					ActiveInterface: model.Pointer("en0"),
					InterfaceKind:   model.Pointer("wifi"),
					SSID:            model.Pointer("eduroam"),
				},
				Measurements: []model.Measurement{
					{
						Provider:     model.ProviderNJUCampusIPv4,
						Method:       "librespeed-three-stream",
						Status:       model.ProviderStatusSuccess,
						DownloadMbps: model.Pointer(942.5),
						UploadMbps:   model.Pointer(7.25),
						ServerFQDN:   model.Pointer("speed.nju.edu.cn"),
					},
					{
						Provider:     model.ProviderMLab,
						Method:       "ndt7",
						Status:       model.ProviderStatusFailed,
						DownloadMbps: nil,
						UploadMbps:   nil,
						Failure: &model.Failure{
							Stage:   model.FailureStageConnect,
							Code:    "unreachable",
							Message: "M-Lab server was unreachable",
						},
					},
				},
			},
			golden: "summary_partial.golden",
		},
		{
			// A cancelled run with no measurements at all: the table must
			// still be a well-formed header row, and no network line appears
			// when every network field is nil.
			name: "cancelled with no measurements",
			summary: model.RunSummary{
				ToolVersion:  "1.2.3",
				RunID:        "00000000-0000-4000-8000-000000000002",
				StartedAt:    started,
				EndedAt:      started.Add(430 * time.Millisecond),
				Command:      model.CommandRun,
				Status:       model.RunStatusCancelled,
				Measurements: nil,
			},
			golden: "summary_cancelled.golden",
		},
		{
			// EndedAt before StartedAt (a clock step mid-run) must clamp to
			// "0 ms" rather than printing a negative duration.
			name: "negative duration clamps",
			summary: model.RunSummary{
				ToolVersion: "1.2.3",
				RunID:       "00000000-0000-4000-8000-000000000003",
				StartedAt:   started,
				EndedAt:     started.Add(-5 * time.Second),
				Command:     model.CommandOokla,
				Status:      model.RunStatusSuccess,
				Network:     model.NetworkContext{InterfaceKind: model.Pointer("ethernet")},
				Measurements: []model.Measurement{
					{
						Provider:      model.ProviderOokla,
						Method:        "ookla-speedtest",
						Status:        model.ProviderStatusSuccess,
						DownloadMbps:  model.Pointer(0.0),
						UploadMbps:    model.Pointer(1234.567),
						ServerName:    model.Pointer("Nanjing"),
						ServerSponsor: model.Pointer("China Telecom"),
					},
				},
			},
			golden: "summary_negative_duration.golden",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, out := renderApp(t)
			app.renderSummary(test.summary)
			assertGolden(t, test.golden, out.String())
		})
	}
}

// --- value formatters --------------------------------------------------

func TestMeasurementServer(t *testing.T) {
	for _, test := range []struct {
		name        string
		measurement model.Measurement
		want        string
	}{
		{name: "nothing known", measurement: model.Measurement{}, want: "—"},
		{
			name:        "fqdn alone",
			measurement: model.Measurement{ServerFQDN: model.Pointer("ndt.mlab.example")},
			want:        "ndt.mlab.example",
		},
		{
			name:        "fqdn with sponsor",
			measurement: model.Measurement{ServerFQDN: model.Pointer("ndt.mlab.example"), ServerSponsor: model.Pointer("M-Lab")},
			want:        "ndt.mlab.example · M-Lab",
		},
		{
			// An empty-string pointer is not the same as a present value; it
			// must fall through to the next candidate.
			name:        "empty fqdn falls through to name",
			measurement: model.Measurement{ServerFQDN: model.Pointer(""), ServerName: model.Pointer("Nanjing")},
			want:        "Nanjing",
		},
		{
			name:        "name with distinct sponsor",
			measurement: model.Measurement{ServerName: model.Pointer("Nanjing"), ServerSponsor: model.Pointer("China Telecom")},
			want:        "Nanjing · China Telecom",
		},
		{
			// Ookla frequently reports the sponsor as the server name; the
			// renderer must not print "China Telecom · China Telecom".
			name:        "name equal to sponsor is not repeated",
			measurement: model.Measurement{ServerName: model.Pointer("China Telecom"), ServerSponsor: model.Pointer("China Telecom")},
			want:        "China Telecom",
		},
		{
			name:        "address as last resort",
			measurement: model.Measurement{ServerName: model.Pointer(""), ServerAddress: model.Pointer("203.0.113.9")},
			want:        "203.0.113.9",
		},
		{
			name:        "empty address falls back to dash",
			measurement: model.Measurement{ServerAddress: model.Pointer("")},
			want:        "—",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := measurementServer(test.measurement); got != test.want {
				t.Fatalf("measurementServer() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFormatNetworkContext(t *testing.T) {
	for _, test := range []struct {
		name    string
		network model.NetworkContext
		want    string
	}{
		{name: "empty", network: model.NetworkContext{}, want: ""},
		{
			name:    "empty strings are dropped",
			network: model.NetworkContext{ActiveInterface: model.Pointer(""), InterfaceKind: model.Pointer(""), SSID: model.Pointer("")},
			want:    "",
		},
		{
			name:    "interface only",
			network: model.NetworkContext{ActiveInterface: model.Pointer("en0")},
			want:    "en0",
		},
		{
			name:    "wired has no ssid",
			network: model.NetworkContext{ActiveInterface: model.Pointer("en0"), InterfaceKind: model.Pointer("ethernet")},
			want:    "en0 · ethernet",
		},
		{
			name:    "full wifi context",
			network: model.NetworkContext{ActiveInterface: model.Pointer("en0"), InterfaceKind: model.Pointer("wifi"), SSID: model.Pointer("eduroam")},
			want:    "en0 · wifi · eduroam",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := formatNetworkContext(test.network); got != test.want {
				t.Fatalf("formatNetworkContext() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFormatMbpsAndValueOrEmpty(t *testing.T) {
	if got := formatMbps(nil); got != "—" {
		t.Fatalf("formatMbps(nil) = %q", got)
	}
	if got := formatMbps(model.Pointer(0.0)); got != "0.00 Mbps" {
		t.Fatalf("formatMbps(0) = %q, want a zero reading, not a dash", got)
	}
	if got := formatMbps(model.Pointer(942.456)); got != "942.46 Mbps" {
		t.Fatalf("formatMbps(942.456) = %q", got)
	}
	if got := valueOrEmpty(nil); got != "" {
		t.Fatalf("valueOrEmpty(nil) = %q", got)
	}
	if got := valueOrEmpty(model.Pointer("label")); got != "label" {
		t.Fatalf("valueOrEmpty() = %q", got)
	}
}

func TestFormatDuration(t *testing.T) {
	for _, test := range []struct {
		duration time.Duration
		want     string
	}{
		{duration: -3 * time.Second, want: "0 ms"},
		{duration: 0, want: "0 ms"},
		{duration: 430 * time.Millisecond, want: "430 ms"},
		{duration: 999 * time.Millisecond, want: "999 ms"},
		{duration: time.Second, want: "1s"},
		{duration: 12*time.Second + 40*time.Millisecond, want: "12s"},
		{duration: 90 * time.Second, want: "1m30s"},
	} {
		if got := formatDuration(test.duration); got != test.want {
			t.Fatalf("formatDuration(%s) = %q, want %q", test.duration, got, test.want)
		}
	}
}

// Styling is only enabled on a TTY; every helper must be a no-op otherwise so
// that redirected output stays byte-for-byte plain.
func TestStyleSetDisabledIsIdentity(t *testing.T) {
	styles := newStyleSet(false)
	for _, text := range []string{"ready", "failed", "partial", "cancelled", "anything"} {
		for name, got := range map[string]string{
			"Title":  styles.Title(text),
			"Header": styles.Header(text),
			"Dim":    styles.Dim(text),
			"OK":     styles.OK(text),
			"Warn":   styles.Warn(text),
			"Bad":    styles.Bad(text),
			"Accent": styles.Accent(text),
			"Status": styles.Status(text),
		} {
			if got != text {
				t.Fatalf("%s(%q) = %q, want the input unchanged", name, text, got)
			}
		}
	}
}

// Status must color every documented state word and pass anything else
// through untouched, so an unknown status never disappears from the table.
func TestStyleSetStatusCoversEveryStateWord(t *testing.T) {
	styles := newStyleSet(true)
	for _, word := range []string{
		"success", "reachable", "ready",
		"partial", "skipped", "automatic",
		"failed", "unreachable",
		"cancelled", "unsupported",
	} {
		styled := styles.Status(word)
		if styled == word {
			t.Fatalf("Status(%q) was not styled", word)
		}
		if !bytes.Contains([]byte(styled), []byte(word)) {
			t.Fatalf("Status(%q) = %q dropped the word", word, styled)
		}
	}
	if got := styles.Status("brand-new-state"); got != "brand-new-state" {
		t.Fatalf("Status() of an unknown word = %q, want it unchanged", got)
	}
	if got := styles.Title(""); got != "" {
		t.Fatalf("styling an empty string produced %q", got)
	}
}

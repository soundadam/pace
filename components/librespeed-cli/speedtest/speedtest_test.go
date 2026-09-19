package speedtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/librespeed/speedtest-cli/report"
)

// TestArgumentValidation pins the argument combinations SpeedTest rejects
// before it does anything else, including the exact message. soundprobe always
// passes --json alongside --progress-json; the first case is what enforces it.
func TestArgumentValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			"progress-json requires json",
			[]string{"--progress-json"},
			"--progress-json requires --json",
		},
		{
			"progress-json is not satisfied by csv",
			[]string{"--progress-json", "--csv"},
			"--progress-json requires --json",
		},
		{
			"source and interface are incompatible",
			[]string{"--source", "127.0.0.1", "--interface", "lo0"},
			"incompatible options 'source' and 'interface'",
		},
		{
			"concurrent must be at least one",
			[]string{"--concurrent", "0"},
			"invalid concurrent requests setting",
		},
		{
			"concurrent cannot be negative",
			[]string{"--concurrent", "-1"},
			"invalid concurrent requests setting",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := runSpeedTest(t, tc.args...)
			if result.Err == nil {
				t.Fatalf("args %v were accepted, want %q", tc.args, tc.want)
			}
			if result.Err.Error() != tc.want {
				t.Errorf("error = %q, want %q", result.Err, tc.want)
			}
		})
	}
}

// TestVersionAndHelpShortCircuit pins that the informational flags return
// before any network setup happens.
func TestVersionAndHelpShortCircuit(t *testing.T) {
	t.Run("version", func(t *testing.T) {
		result := runSpeedTest(t, "--version")
		if result.Err != nil {
			t.Fatalf("--version: %v", result.Err)
		}
		for _, want := range []string{
			"https://github.com/librespeed/speedtest-cli",
			"Licensed under GNU Lesser General Public License v3.0",
			"LibreSpeed\tCopyright (C) 2016-2020 Federico Dossena",
		} {
			if !strings.Contains(result.Out, want) {
				t.Errorf("--version output is missing %q:\n%s", want, result.Out)
			}
		}
	})

	t.Run("csv-header", func(t *testing.T) {
		result := runSpeedTest(t, "--csv-header")
		if result.Err != nil {
			t.Fatalf("--csv-header: %v", result.Err)
		}
		want := "Timestamp,Server Name,Address,Ping,Jitter,Download,Upload,Share,IP\n"
		if result.Out != want {
			t.Errorf("--csv-header output = %q, want %q", result.Out, want)
		}
	})

	t.Run("csv-header honours the delimiter", func(t *testing.T) {
		result := runSpeedTest(t, "--csv-header", "--csv-delimiter", ";")
		if result.Err != nil {
			t.Fatalf("--csv-header: %v", result.Err)
		}
		if !strings.HasPrefix(result.Out, "Timestamp;Server Name;") {
			t.Errorf("--csv-delimiter was not applied: %q", result.Out)
		}
	})
}

func TestTelemetryJSONFileErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		result := runSpeedTest(t, "--telemetry-json", t.TempDir()+"/absent.json")
		if result.Err == nil {
			t.Fatal("want an error for a missing telemetry JSON file")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		result := runSpeedTest(t, "--telemetry-json", writeServerList(t, "not json"))
		if result.Err == nil {
			t.Fatal("want an error for an unparseable telemetry JSON file")
		}
	})
}

// measurementBackend starts an in-process LibreSpeed backend and returns a
// --local-json body pinned to it.
func measurementBackend(t *testing.T) (string, *httptest.Server) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/empty.php", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/garbage.php", func(w http.ResponseWriter, r *http.Request) {
		block := make([]byte, 32*1024)
		for {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			if _, err := w.Write(block); err != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	})
	mux.HandleFunc("/getIP.php", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"processedString":"203.0.113.1 - Example ISP","rawIspInfo":{"ip":"203.0.113.1","org":"AS64496 Example"}}`)
	})

	backend := httptest.NewServer(mux)
	t.Cleanup(backend.Close)

	list := fmt.Sprintf(`[{"id":1,"name":"in-process","server":%q,"dlURL":"garbage.php","ulURL":"empty.php","pingURL":"empty.php","getIpURL":"getIP.php"}]`, backend.URL)
	return writeServerList(t, list), backend
}

func decodeProgressStream(t *testing.T, stream string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(stream, "\n"), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("stderr line %q is not JSON: %v\nfull stream:\n%s", line, err, stream)
		}
		events = append(events, event)
	}
	return events
}

// TestEndToEndProgressAndReport is the full contract soundprobe relies on: the
// --progress-json stream on stderr, the final JSON document on stdout, and the
// strict separation between the two.
func TestEndToEndProgressAndReport(t *testing.T) {
	list, _ := measurementBackend(t)

	result := runSpeedTest(t,
		"--local-json", list,
		"--server", "1",
		"--no-icmp",
		"--duration", "1",
		"--concurrent", "1",
		"--upload-size", "64",
		"--telemetry-level", "disabled",
		"--json",
		"--progress-json",
	)
	if result.Err != nil {
		t.Fatalf("measurement run: %v\nstderr:\n%s", result.Err, result.UI)
	}

	t.Run("stdout carries exactly one JSON report", func(t *testing.T) {
		var reports []report.JSONReport
		if err := json.Unmarshal([]byte(result.Out), &reports); err != nil {
			t.Fatalf("stdout is not a JSON report array: %v\n%s", err, result.Out)
		}
		if len(reports) != 1 {
			t.Fatalf("got %d reports, want 1", len(reports))
		}
		got := reports[0]
		if got.Download <= 0 {
			t.Errorf("download = %v, want > 0", got.Download)
		}
		if got.Upload <= 0 {
			t.Errorf("upload = %v, want > 0", got.Upload)
		}
		if got.BytesReceived == 0 {
			t.Error("bytes_received = 0")
		}
		if got.BytesSent == 0 {
			t.Error("bytes_sent = 0")
		}
		if got.Share != "" {
			t.Errorf("share = %q, want empty with telemetry disabled", got.Share)
		}
		if got.Server.Name != "in-process" {
			t.Errorf("server name = %q, want %q", got.Server.Name, "in-process")
		}
		if got.Client.IP != "203.0.113.1" {
			t.Errorf("client ip = %q, want 203.0.113.1", got.Client.IP)
		}
	})

	t.Run("no progress event leaks onto stdout", func(t *testing.T) {
		if strings.Contains(result.Out, `"type":"progress"`) {
			t.Errorf("stdout contains progress events:\n%s", result.Out)
		}
		if strings.Contains(result.Out, "\n") && !strings.HasSuffix(result.Out, "]") {
			t.Errorf("stdout is not a single JSON document:\n%q", result.Out)
		}
	})

	t.Run("stderr carries only progress events", func(t *testing.T) {
		events := decodeProgressStream(t, result.UI)
		if len(events) < 4 {
			t.Fatalf("got %d progress events over two 1s phases, want at least 4:\n%s", len(events), result.UI)
		}
		for i, event := range events {
			if event["type"] != "progress" {
				t.Errorf("event %d: type = %v, want \"progress\"", i, event["type"])
			}
		}
	})

	t.Run("download events precede upload events", func(t *testing.T) {
		events := decodeProgressStream(t, result.UI)
		seenUpload := false
		sawDownload := false
		for i, event := range events {
			switch event["test"] {
			case "download":
				sawDownload = true
				if seenUpload {
					t.Fatalf("event %d is a download sample after the upload phase started", i)
				}
			case "upload":
				seenUpload = true
			default:
				t.Fatalf("event %d has an unknown test %v; the only values are \"download\" and \"upload\"", i, event["test"])
			}
		}
		if !sawDownload {
			t.Error("no download samples")
		}
		if !seenUpload {
			t.Error("no upload samples")
		}
	})

	t.Run("elapsed_ms restarts per phase", func(t *testing.T) {
		events := decodeProgressStream(t, result.UI)
		var lastDownload, firstUpload float64 = 0, -1
		for _, event := range events {
			elapsed, ok := event["elapsed_ms"].(float64)
			if !ok {
				t.Fatalf("elapsed_ms is not a number in %v", event)
			}
			if event["test"] == "download" {
				lastDownload = elapsed
			} else if firstUpload < 0 {
				firstUpload = elapsed
			}
		}
		if firstUpload < 0 {
			t.Skip("no upload samples to compare")
		}
		if firstUpload >= lastDownload {
			t.Errorf("first upload elapsed_ms %v is not below the last download elapsed_ms %v; each phase uses its own counter clock", firstUpload, lastDownload)
		}
	})
}

// TestTelemetryDisabledSendsNothing pins that --telemetry-level disabled means
// no telemetry request at all, which is the flag soundprobe always passes.
func TestTelemetryDisabledSendsNothing(t *testing.T) {
	var hits atomic.Int64
	telemetry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "id 12345")
	}))
	defer telemetry.Close()

	list, _ := measurementBackend(t)
	result := runSpeedTest(t,
		"--local-json", list,
		"--server", "1",
		"--no-icmp",
		"--no-upload",
		"--duration", "1",
		"--concurrent", "1",
		"--telemetry-level", "disabled",
		"--telemetry-server", telemetry.URL,
		"--json",
	)
	if result.Err != nil {
		t.Fatalf("measurement run: %v", result.Err)
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("telemetry server received %d requests, want 0", got)
	}

	var reports []report.JSONReport
	if err := json.Unmarshal([]byte(result.Out), &reports); err != nil {
		t.Fatalf("decode report: %v\n%s", err, result.Out)
	}
	if len(reports) != 1 || reports[0].Share != "" {
		t.Errorf("share link = %q, want empty", reports[0].Share)
	}
}

// TestTelemetryEnabledSendsResults is the control for the test above: with a
// non-disabled level the helper does contact the telemetry server, so the
// "disabled" assertion is meaningful rather than vacuous.
func TestTelemetryEnabledSendsResults(t *testing.T) {
	var hits atomic.Int64
	var gotExtra atomic.Value
	telemetry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			gotExtra.Store(r.FormValue("extra"))
		}
		_, _ = io.WriteString(w, "id 12345")
	}))
	defer telemetry.Close()

	list, _ := measurementBackend(t)
	result := runSpeedTest(t,
		"--local-json", list,
		"--server", "1",
		"--no-icmp",
		"--no-upload",
		"--duration", "1",
		"--concurrent", "1",
		"--telemetry-level", "basic",
		"--telemetry-server", telemetry.URL,
		"--telemetry-extra", "campus-test",
		"--json",
	)
	if result.Err != nil {
		t.Fatalf("measurement run: %v", result.Err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("telemetry server received %d requests, want 1", got)
	}
	if extra, _ := gotExtra.Load().(string); !strings.Contains(extra, "campus-test") || !strings.Contains(extra, "in-process") {
		t.Errorf("telemetry extra = %q, want the server name and --telemetry-extra value", extra)
	}

	var reports []report.JSONReport
	if err := json.Unmarshal([]byte(result.Out), &reports); err != nil {
		t.Fatalf("decode report: %v\n%s", err, result.Out)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if !strings.Contains(reports[0].Share, "id=12345") {
		t.Errorf("share link = %q, want it to carry the telemetry id", reports[0].Share)
	}
}

// TestCSVReportOutput pins the --csv path, which shares the measurement code
// with --json and takes priority over it.
func TestCSVReportOutput(t *testing.T) {
	list, _ := measurementBackend(t)

	result := runSpeedTest(t,
		"--local-json", list,
		"--server", "1",
		"--no-icmp",
		"--no-upload",
		"--duration", "1",
		"--concurrent", "1",
		"--telemetry-level", "disabled",
		"--csv",
		"--json",
	)
	if result.Err != nil {
		t.Fatalf("measurement run: %v", result.Err)
	}
	if strings.HasPrefix(result.Out, "[") {
		t.Errorf("--csv did not take priority over --json:\n%s", result.Out)
	}
	fields := strings.Split(result.Out, ",")
	if len(fields) != 9 {
		t.Errorf("got %d CSV fields, want 9:\n%s", len(fields), result.Out)
	}
	if !strings.Contains(result.Out, "in-process") {
		t.Errorf("CSV row is missing the server name:\n%s", result.Out)
	}
}

// TestUnreachableServerProducesEmptyReport pins what soundprobe sees when the
// pinned station is down: an empty JSON array, a zero exit status, and no
// progress events.
func TestUnreachableServerProducesEmptyReport(t *testing.T) {
	const downServer = `[{"id":1,"name":"down","server":"http://127.0.0.1:1","dlURL":"garbage.php","ulURL":"empty.php","pingURL":"empty.php","getIpURL":"getIP.php"}]`

	result := runSpeedTest(t,
		"--local-json", writeServerList(t, downServer),
		"--server", "1",
		"--no-icmp",
		"--duration", "1",
		"--telemetry-level", "disabled",
		"--json",
		"--progress-json",
	)
	if result.Err != nil {
		t.Fatalf("run against a dead server: %v", result.Err)
	}
	if result.Out != "null" && result.Out != "[]" {
		t.Errorf("stdout = %q, want an empty JSON document", result.Out)
	}
	if strings.Contains(result.UI, `"type":"progress"`) {
		t.Errorf("progress events were emitted for a dead server:\n%s", result.UI)
	}
}

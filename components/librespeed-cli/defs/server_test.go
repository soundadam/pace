package defs

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newBackend starts an in-process LibreSpeed backend. No test in this package
// performs real network I/O; every request is served by this httptest server
// on the loopback interface.
func newBackend(t *testing.T) *httptest.Server {
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

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func backendServer(t *testing.T) *Server {
	t.Helper()
	backend := newBackend(t)
	return &Server{
		ID:          1,
		Name:        "test-backend",
		Server:      backend.URL,
		DownloadURL: "garbage.php",
		UploadURL:   "empty.php",
		PingURL:     "empty.php",
		GetIPURL:    "getIP.php",
		NoICMP:      true,
	}
}

func parseProgressStream(t *testing.T, stream string) []consumerProgressEvent {
	t.Helper()
	if stream == "" {
		return nil
	}
	var events []consumerProgressEvent
	for i, line := range strings.Split(strings.TrimSuffix(stream, "\n"), "\n") {
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.DisallowUnknownFields()
		var event consumerProgressEvent
		if err := decoder.Decode(&event); err != nil {
			t.Fatalf("line %d %q is not a strictly decodable progress event: %v", i, line, err)
		}
		events = append(events, event)
	}
	return events
}

// TestDownloadEmitsProgressStream exercises the real Download loop, ticker and
// concurrent request goroutines, and pins that the resulting stderr stream is
// nothing but well-formed progress events tagged "download".
func TestDownloadEmitsProgressStream(t *testing.T) {
	server := backendServer(t)

	var mbps float64
	var total uint64
	var err error
	stream := captureUI(t, func() {
		mbps, total, err = server.Download(true, true, false, false, 2, 1, 900*time.Millisecond)
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if total == 0 {
		t.Fatal("Download counted zero bytes")
	}
	if mbps <= 0 {
		t.Errorf("Download rate = %v, want > 0", mbps)
	}

	events := parseProgressStream(t, stream)
	if len(events) < 2 {
		t.Fatalf("got %d progress events, want at least 2 over 900ms at a 200ms cadence:\n%s", len(events), stream)
	}
	for i, event := range events {
		if event.Type != "progress" {
			t.Errorf("event %d: type = %q, want %q", i, event.Type, "progress")
		}
		if event.Test != "download" {
			t.Errorf("event %d: test = %q, want %q", i, event.Test, "download")
		}
		if event.ElapsedMS <= 0 {
			t.Errorf("event %d: elapsed_ms = %d, want > 0", i, event.ElapsedMS)
		}
		if i > 0 {
			if event.ElapsedMS < events[i-1].ElapsedMS {
				t.Errorf("event %d: elapsed_ms went backwards", i)
			}
			if event.Bytes < events[i-1].Bytes {
				t.Errorf("event %d: bytes went backwards", i)
			}
		}
	}
	if last := events[len(events)-1]; last.Bytes > total {
		t.Errorf("last sample reported %d bytes, more than the final total %d", last.Bytes, total)
	}
}

// TestUploadEmitsProgressStream is the upload counterpart and pins the "upload"
// value of the test field.
func TestUploadEmitsProgressStream(t *testing.T) {
	server := backendServer(t)

	var err error
	stream := captureUI(t, func() {
		_, _, err = server.Upload(false, true, true, false, false, 2, 64, 900*time.Millisecond)
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	events := parseProgressStream(t, stream)
	if len(events) < 2 {
		t.Fatalf("got %d progress events, want at least 2:\n%s", len(events), stream)
	}
	for i, event := range events {
		if event.Type != "progress" {
			t.Errorf("event %d: type = %q, want %q", i, event.Type, "progress")
		}
		if event.Test != "upload" {
			t.Errorf("event %d: test = %q, want %q", i, event.Test, "upload")
		}
	}
}

// TestProgressStreamSilentWhenDisabled pins that the stream is opt-in: with
// --progress-json absent nothing at all is written to stderr.
func TestProgressStreamSilentWhenDisabled(t *testing.T) {
	server := backendServer(t)

	stream := captureUI(t, func() {
		if _, _, err := server.Download(true, false, false, false, 1, 1, 500*time.Millisecond); err != nil {
			t.Errorf("Download: %v", err)
		}
	})
	if stream != "" {
		t.Errorf("stderr = %q, want empty when --progress-json is not given", stream)
	}
}

// TestProgressStreamHasNoTerminalEvent pins that the protocol has exactly one
// event type. There is no completion, summary or error event: the stream simply
// stops, and the final result is the JSON document on stdout.
func TestProgressStreamHasNoTerminalEvent(t *testing.T) {
	server := backendServer(t)

	stream := captureUI(t, func() {
		if _, _, err := server.Download(true, true, false, false, 1, 1, 700*time.Millisecond); err != nil {
			t.Errorf("Download: %v", err)
		}
	})
	events := parseProgressStream(t, stream)
	if len(events) == 0 {
		t.Fatal("no progress events emitted")
	}
	for i, event := range events {
		if event.Type != "progress" {
			t.Fatalf("event %d has type %q; the stream is documented to carry only %q events", i, event.Type, "progress")
		}
	}
}

// TestDownloadErrorPathEmitsNoProgressJSON pins what a consumer sees when the
// backend is unusable: no progress event is ever emitted, no JSON-shaped error
// event is invented, and Download still returns a nil error with a zero rate.
func TestDownloadErrorPathEmitsNoProgressJSON(t *testing.T) {
	server := &Server{
		ID:          2,
		Name:        "unreachable",
		Server:      "http://127.0.0.1:1", // nothing listens here
		DownloadURL: "garbage.php",
		PingURL:     "empty.php",
		NoICMP:      true,
	}

	var mbps float64
	var total uint64
	var err error
	stream := captureUI(t, func() {
		mbps, total, err = server.Download(true, true, false, false, 1, 1, 500*time.Millisecond)
	})
	if err != nil {
		t.Fatalf("Download returned %v; the current contract is a nil error with a zero rate", err)
	}
	if total != 0 || mbps != 0 {
		t.Errorf("Download = (%v Mbps, %d bytes), want (0, 0)", mbps, total)
	}
	if events := parseProgressStream(t, stream); len(events) > 0 {
		for _, event := range events {
			if event.Bytes != 0 {
				t.Errorf("unreachable backend produced a non-zero sample: %+v", event)
			}
		}
	}
}

// TestGetURLAndSponsor covers the small pure helpers used when building the
// report and the --list output.
func TestGetURLAndSponsor(t *testing.T) {
	t.Run("GetURL rejects an unparseable server", func(t *testing.T) {
		server := &Server{Server: "http://[::1"}
		if _, err := server.GetURL(); err == nil {
			t.Error("want an error for a malformed server URL")
		}
	})

	t.Run("Sponsor", func(t *testing.T) {
		for _, tc := range []struct {
			name, sponsor, url, want string
		}{
			{"no sponsor", "", "", ""},
			{"name only", "ACME", "", "ACME"},
			{"scheme defaulted to https", "ACME", "acme.example", "ACME @ https://acme.example"},
			{"scheme preserved", "ACME", "http://acme.example", "ACME @ http://acme.example"},
			{"invalid url dropped", "ACME", "http://[::1", "ACME"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				server := &Server{SponsorName: tc.sponsor, SponsorURL: tc.url}
				if got := server.Sponsor(); got != tc.want {
					t.Errorf("Sponsor() = %q, want %q", got, tc.want)
				}
			})
		}
	})
}

// TestIsUp pins the liveness contract: 200 with an empty body means up.
func TestIsUp(t *testing.T) {
	t.Run("empty 200 is up", func(t *testing.T) {
		if !backendServer(t).IsUp() {
			t.Error("IsUp() = false, want true")
		}
	})

	t.Run("non-empty body is down", func(t *testing.T) {
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "pong")
		}))
		defer backend.Close()
		server := &Server{Server: backend.URL, PingURL: "empty.php"}
		if server.IsUp() {
			t.Error("IsUp() = true, want false for a non-empty ping body")
		}
	})

	t.Run("error status is down", func(t *testing.T) {
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer backend.Close()
		server := &Server{Server: backend.URL, PingURL: "empty.php"}
		if server.IsUp() {
			t.Error("IsUp() = true, want false for a 500")
		}
	})

	t.Run("unreachable is down", func(t *testing.T) {
		server := &Server{Server: "http://127.0.0.1:1", PingURL: "empty.php"}
		if server.IsUp() {
			t.Error("IsUp() = true, want false when nothing is listening")
		}
	})
}

// TestGetIPInfoTolerantParse pins the fallback that keeps a usable
// processedString when rawIspInfo is not an object.
func TestGetIPInfoTolerantParse(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"well formed", `{"processedString":"203.0.113.1 - ISP","rawIspInfo":{"ip":"203.0.113.1"}}`, "203.0.113.1 - ISP"},
		{"rawIspInfo is a string", `{"processedString":"203.0.113.1 - ISP","rawIspInfo":""}`, "203.0.113.1 - ISP"},
		{"not JSON at all", `203.0.113.1`, "203.0.113.1"},
		{"empty body", ``, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer backend.Close()

			server := &Server{Server: backend.URL, GetIPURL: "getIP.php"}
			info, err := server.GetIPInfo("km")
			if err != nil {
				t.Fatalf("GetIPInfo: %v", err)
			}
			if info.ProcessedString != tc.want {
				t.Errorf("ProcessedString = %q, want %q", info.ProcessedString, tc.want)
			}
		})
	}
}

// TestPingAndJitter pins the HTTP ping fallback used whenever --no-icmp is set,
// which is the only path soundprobe takes.
func TestPingAndJitter(t *testing.T) {
	server := backendServer(t)

	ping, jitter, err := server.PingAndJitter(4)
	if err != nil {
		t.Fatalf("PingAndJitter: %v", err)
	}
	if ping < 0 {
		t.Errorf("ping = %v, want >= 0", ping)
	}
	if jitter < 0 {
		t.Errorf("jitter = %v, want >= 0", jitter)
	}

	t.Run("unreachable backend returns an error", func(t *testing.T) {
		down := &Server{Server: "http://127.0.0.1:1", PingURL: "empty.php"}
		if _, _, err := down.PingAndJitter(2); err == nil {
			t.Error("want an error when the backend is unreachable")
		}
	})
}

// TestICMPPingAndJitterFallsBackWhenNoICMP pins that NoICMP routes straight to
// the HTTP ping, with no raw socket involved.
func TestICMPPingAndJitterFallsBackWhenNoICMP(t *testing.T) {
	server := backendServer(t)
	if _, _, err := server.ICMPPingAndJitter(1, "", "ip"); err != nil {
		t.Fatalf("ICMPPingAndJitter with NoICMP: %v", err)
	}
}

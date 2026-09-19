package speedtest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/librespeed/speedtest-cli/defs"
)

func TestHumanizeMbps(t *testing.T) {
	for _, tc := range []struct {
		name string
		mbps float64
		mebi bool
		want string
	}{
		{"bytes per second", 0.004, false, "500.00 bytes/s"},
		{"kilobytes per second", 4, false, "500.00 KB/s"},
		{"megabytes per second", 8000, false, "1000.00 MB/s"},
		{"gigabytes per second", 80000, false, "10.00 GB/s"},
		{"mebibyte base", 8192, true, "1024.00 MB/s"},
		{"zero", 0, false, "0.00 bytes/s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := humanizeMbps(tc.mbps, tc.mebi); got != tc.want {
				t.Errorf("humanizeMbps(%v, %v) = %q, want %q", tc.mbps, tc.mebi, got, tc.want)
			}
		})
	}
}

// TestSendTelemetry covers the telemetry upload itself: the multipart fields
// it sends and how it reports a malformed server response.
func TestSendTelemetry(t *testing.T) {
	t.Run("posts every field and builds a share link", func(t *testing.T) {
		var form map[string][]string
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
			}
			form = r.MultipartForm.Value
			_, _ = io.WriteString(w, "id abc123")
		}))
		defer backend.Close()

		server := defs.TelemetryServer{
			Level:  defs.TelemetryLevelBasic,
			Server: backend.URL,
			Path:   "/results/telemetry.php",
			Share:  "/results/",
		}
		info := &defs.GetIPResult{ProcessedString: "203.0.113.1 - ISP"}
		extra := defs.TelemetryExtra{ServerName: "in-process", Extra: "campus"}

		link, err := sendTelemetry(server, info, 100, 50, 1.25, 0.5, "log body", extra)
		if err != nil {
			t.Fatalf("sendTelemetry: %v", err)
		}
		if !strings.HasSuffix(link, "/results/?id=abc123") {
			t.Errorf("share link = %q, want it to end with /results/?id=abc123", link)
		}

		for field, want := range map[string]string{
			"dl":     "100.00",
			"ul":     "50.00",
			"ping":   "1.25",
			"jitter": "0.50",
			"log":    "log body",
		} {
			values, ok := form[field]
			if !ok || len(values) == 0 {
				t.Errorf("multipart field %q is missing", field)
				continue
			}
			if values[0] != want {
				t.Errorf("multipart field %q = %q, want %q", field, values[0], want)
			}
		}
		if values := form["ispinfo"]; len(values) == 0 || !strings.Contains(values[0], "203.0.113.1") {
			t.Errorf("ispinfo field = %v, want the processed string", values)
		}
		if values := form["extra"]; len(values) == 0 || !strings.Contains(values[0], "in-process") {
			t.Errorf("extra field = %v, want the server name", values)
		}
	})

	t.Run("malformed response is an error", func(t *testing.T) {
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "nope")
		}))
		defer backend.Close()

		server := defs.TelemetryServer{Level: defs.TelemetryLevelBasic, Server: backend.URL, Path: "/t", Share: "/s"}
		if _, err := sendTelemetry(server, &defs.GetIPResult{}, 0, 0, 0, 0, "", defs.TelemetryExtra{}); err == nil {
			t.Fatal("want an error for a response that is not \"id <value>\"")
		}
	})

	t.Run("unreachable telemetry server is an error", func(t *testing.T) {
		server := defs.TelemetryServer{Level: defs.TelemetryLevelBasic, Server: "http://127.0.0.1:1", Path: "/t", Share: "/s"}
		if _, err := sendTelemetry(server, &defs.GetIPResult{}, 0, 0, 0, 0, "", defs.TelemetryExtra{}); err == nil {
			t.Fatal("want an error when the telemetry server is unreachable")
		}
	})
}

// TestNewDialerAddressBound covers the --source binding helper.
func TestNewDialerAddressBound(t *testing.T) {
	t.Run("valid loopback address", func(t *testing.T) {
		dialer, err := newDialerAddressBound("127.0.0.1", "ip4")
		if err != nil {
			t.Fatalf("newDialerAddressBound: %v", err)
		}
		if dialer.LocalAddr == nil {
			t.Fatal("LocalAddr is nil")
		}
		if got := dialer.LocalAddr.String(); !strings.HasPrefix(got, "127.0.0.1") {
			t.Errorf("LocalAddr = %q, want 127.0.0.1", got)
		}
	})

	t.Run("unusable address", func(t *testing.T) {
		if _, err := newDialerAddressBound("not an address", "ip"); err == nil {
			t.Fatal("want an error for an unresolvable source address")
		}
	})
}

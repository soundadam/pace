package speedtest

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/librespeed/speedtest-cli/report"
)

// backendHandler serves the endpoints a LibreSpeed backend needs for a
// ping-only run.
func backendHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/empty.php", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/getIP.php", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"processedString":"203.0.113.1 - Example ISP","rawIspInfo":{"ip":"203.0.113.1"}}`)
	})
	return mux
}

func listEntry(id int, name, url string) string {
	return fmt.Sprintf(`{"id":%d,"name":%q,"server":%q,"dlURL":"garbage.php","ulURL":"empty.php","pingURL":"empty.php","getIpURL":"getIP.php"}`, id, name, url)
}

// TestAutomaticServerSelection covers the path taken when --server is not
// given: every server in the list is pinged concurrently and the fastest one is
// measured. soundprobe always pins a server, so this is the branch its usage
// never exercises.
func TestAutomaticServerSelection(t *testing.T) {
	first := httptest.NewServer(backendHandler())
	defer first.Close()
	second := httptest.NewServer(backendHandler())
	defer second.Close()
	dead := "http://127.0.0.1:1"

	list := "[" + strings.Join([]string{
		listEntry(1, "alpha", first.URL),
		listEntry(2, "bravo", second.URL),
		listEntry(3, "dead", dead),
	}, ",") + "]"

	result := runSpeedTest(t,
		"--local-json", writeServerList(t, list),
		"--no-icmp",
		"--no-download",
		"--no-upload",
		"--telemetry-level", "disabled",
		"--json",
	)
	if result.Err != nil {
		t.Fatalf("automatic selection run: %v\nstderr:\n%s", result.Err, result.UI)
	}

	var reports []report.JSONReport
	if err := json.Unmarshal([]byte(result.Out), &reports); err != nil {
		t.Fatalf("decode report: %v\n%s", err, result.Out)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if name := reports[0].Server.Name; name != "alpha" && name != "bravo" {
		t.Errorf("selected server = %q, want one of the reachable backends", name)
	}
	if reports[0].Download != 0 || reports[0].Upload != 0 {
		t.Errorf("--no-download/--no-upload still produced rates: %+v", reports[0])
	}
}

// TestExcludeDuringAutomaticSelection pins that --exclude removes a server from
// the candidate pool.
func TestExcludeDuringAutomaticSelection(t *testing.T) {
	first := httptest.NewServer(backendHandler())
	defer first.Close()
	second := httptest.NewServer(backendHandler())
	defer second.Close()

	list := "[" + strings.Join([]string{
		listEntry(1, "alpha", first.URL),
		listEntry(2, "bravo", second.URL),
	}, ",") + "]"

	result := runSpeedTest(t,
		"--local-json", writeServerList(t, list),
		"--exclude", "1",
		"--no-icmp",
		"--no-download",
		"--no-upload",
		"--telemetry-level", "disabled",
		"--json",
	)
	if result.Err != nil {
		t.Fatalf("run: %v", result.Err)
	}

	var reports []report.JSONReport
	if err := json.Unmarshal([]byte(result.Out), &reports); err != nil {
		t.Fatalf("decode report: %v\n%s", err, result.Out)
	}
	if len(reports) != 1 || reports[0].Server.Name != "bravo" {
		t.Errorf("selected %+v, want the non-excluded server bravo", reports)
	}
}

// TestRemoteServerList covers the --server-json path, including the
// /.well-known/librespeed retry.
func TestRemoteServerList(t *testing.T) {
	t.Run("fetches and uses a remote list", func(t *testing.T) {
		backend := httptest.NewServer(backendHandler())
		defer backend.Close()

		list := "[" + listEntry(1, "remote", backend.URL) + "]"
		directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, list)
		}))
		defer directory.Close()

		result := runSpeedTest(t, "--server-json", directory.URL, "--list")
		if result.Err != nil {
			t.Fatalf("--server-json: %v", result.Err)
		}
		if !strings.Contains(result.Out, "remote") {
			t.Errorf("--list output is missing the remote server:\n%s", result.Out)
		}
	})

	t.Run("retries the well-known path then fails", func(t *testing.T) {
		var paths []string
		directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.Path)
			_, _ = io.WriteString(w, "<html>not a list</html>")
		}))
		defer directory.Close()

		result := runSpeedTest(t, "--server-json", directory.URL, "--list")
		if result.Err == nil {
			t.Fatal("want an error when the remote list is unusable")
		}
		if len(paths) != 2 {
			t.Fatalf("directory received %v, want a retry on /.well-known/librespeed", paths)
		}
		if paths[1] != "/.well-known/librespeed" {
			t.Errorf("retry path = %q, want /.well-known/librespeed", paths[1])
		}
	})
}

// TestForceIPv4 covers the --ipv4 dialer branch.
func TestForceIPv4(t *testing.T) {
	backend := httptest.NewServer(backendHandler())
	defer backend.Close()

	list := "[" + listEntry(1, "alpha", backend.URL) + "]"
	result := runSpeedTest(t,
		"--local-json", writeServerList(t, list),
		"--server", "1",
		"--ipv4",
		"--no-icmp",
		"--no-download",
		"--no-upload",
		"--telemetry-level", "disabled",
		"--json",
	)
	if result.Err != nil {
		t.Fatalf("--ipv4 run: %v", result.Err)
	}
	if !strings.Contains(result.Out, `"name":"alpha"`) {
		t.Errorf("report is missing the server:\n%s", result.Out)
	}
}

// TestSecureAndCertificateHandling covers --secure with a TLS backend, both
// with --skip-cert-verify and with an explicit --ca-cert bundle.
func TestSecureAndCertificateHandling(t *testing.T) {
	backend := httptest.NewTLSServer(backendHandler())
	defer backend.Close()

	// "//host:port" lets --secure choose the scheme.
	hostPort := strings.TrimPrefix(backend.URL, "https://")
	list := "[" + listEntry(1, "tls", "//"+hostPort+"/") + "]"

	t.Run("skip-cert-verify", func(t *testing.T) {
		result := runSpeedTest(t,
			"--local-json", writeServerList(t, list),
			"--server", "1",
			"--secure",
			"--skip-cert-verify",
			"--no-icmp",
			"--no-download",
			"--no-upload",
			"--telemetry-level", "disabled",
			"--json",
		)
		if result.Err != nil {
			t.Fatalf("--secure --skip-cert-verify: %v", result.Err)
		}
		if !strings.Contains(result.Out, `"url":"https://`) {
			t.Errorf("--secure did not force https:\n%s", result.Out)
		}
	})

	t.Run("ca-cert bundle", func(t *testing.T) {
		bundle := t.TempDir() + "/ca.pem"
		encoded := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: backend.Certificate().Raw})
		if err := os.WriteFile(bundle, encoded, 0o600); err != nil {
			t.Fatalf("write bundle: %v", err)
		}

		result := runSpeedTest(t,
			"--local-json", writeServerList(t, list),
			"--server", "1",
			"--secure",
			"--ca-cert", bundle,
			"--no-icmp",
			"--no-download",
			"--no-upload",
			"--telemetry-level", "disabled",
			"--json",
		)
		if result.Err != nil {
			t.Fatalf("--ca-cert: %v", result.Err)
		}
		var reports []report.JSONReport
		if err := json.Unmarshal([]byte(result.Out), &reports); err != nil {
			t.Fatalf("decode report: %v\n%s", err, result.Out)
		}
		if len(reports) != 1 {
			t.Fatalf("got %d reports, want 1; the supplied CA bundle should have been trusted", len(reports))
		}
	})
}

// TestTelemetryJSONFileOverridesFlags pins that --telemetry-json takes
// precedence over the individual telemetry flags.
func TestTelemetryJSONFileOverridesFlags(t *testing.T) {
	telemetry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "id from-file")
	}))
	defer telemetry.Close()
	backend := httptest.NewServer(backendHandler())
	defer backend.Close()

	settings := fmt.Sprintf(`{"telemetryLevel":"basic","server":%q,"path":"/results/telemetry.php","shareURL":"/results/"}`, telemetry.URL)
	settingsPath := t.TempDir() + "/telemetry.json"
	if err := os.WriteFile(settingsPath, []byte(settings), 0o600); err != nil {
		t.Fatalf("write telemetry settings: %v", err)
	}

	list := "[" + listEntry(1, "alpha", backend.URL) + "]"
	result := runSpeedTest(t,
		"--local-json", writeServerList(t, list),
		"--server", "1",
		"--telemetry-json", settingsPath,
		"--no-icmp",
		"--no-download",
		"--no-upload",
		"--json",
	)
	if result.Err != nil {
		t.Fatalf("--telemetry-json run: %v", result.Err)
	}

	var reports []report.JSONReport
	if err := json.Unmarshal([]byte(result.Out), &reports); err != nil {
		t.Fatalf("decode report: %v\n%s", err, result.Out)
	}
	if len(reports) != 1 || !strings.Contains(reports[0].Share, "id=from-file") {
		t.Errorf("share = %q, want the telemetry id from the settings file", reports[0].Share)
	}
}

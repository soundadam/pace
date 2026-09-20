package speedtest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/librespeed/speedtest-cli/defs"
)

const threeServers = `[
  {"id":1,"name":"alpha","server":"//alpha.example/","dlURL":"garbage.php","ulURL":"empty.php","pingURL":"empty.php","getIpURL":"getIP.php"},
  {"id":2,"name":"bravo","server":"https://bravo.example/","dlURL":"garbage.php","ulURL":"empty.php","pingURL":"empty.php","getIpURL":"getIP.php"},
  {"id":3,"name":"charlie","server":"http://charlie.example/","dlURL":"garbage.php","ulURL":"empty.php","pingURL":"empty.php","getIpURL":"getIP.php"}
]`

func serverIDs(servers []defs.Server) []int {
	ids := make([]int, 0, len(servers))
	for _, server := range servers {
		ids = append(ids, server.ID)
	}
	return ids
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestPreprocessServersScheme pins how --secure/--insecure rewrite the scheme
// of every entry, including the scheme-relative form used by the public list.
func TestPreprocessServersScheme(t *testing.T) {
	for _, tc := range []struct {
		name  string
		force int
		want  []string
	}{
		{"default leaves explicit schemes and defaults the rest to http", forceNothing, []string{"http://alpha.example/", "https://bravo.example/", "http://charlie.example/"}},
		{"secure forces https", forceHttps, []string{"https://alpha.example/", "https://bravo.example/", "https://charlie.example/"}},
		{"insecure forces http", forceHttp, []string{"http://alpha.example/", "http://bravo.example/", "http://charlie.example/"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			servers, err := getLocalServersReader(tc.force, io.NopCloser(strings.NewReader(threeServers)), nil, nil, true)
			if err != nil {
				t.Fatalf("getLocalServersReader: %v", err)
			}
			if len(servers) != len(tc.want) {
				t.Fatalf("got %d servers, want %d", len(servers), len(tc.want))
			}
			for i, want := range tc.want {
				if servers[i].Server != want {
					t.Errorf("server %d = %q, want %q", i, servers[i].Server, want)
				}
			}
		})
	}
}

// TestPreprocessServersFiltering pins --server and --exclude selection.
func TestPreprocessServersFiltering(t *testing.T) {
	load := func(t *testing.T, excludes, specific []int, filter bool) ([]defs.Server, error) {
		t.Helper()
		return getLocalServersReader(forceNothing, io.NopCloser(strings.NewReader(threeServers)), excludes, specific, filter)
	}

	t.Run("no filter keeps everything", func(t *testing.T) {
		servers, err := load(t, nil, nil, true)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got := serverIDs(servers); !equalInts(got, []int{1, 2, 3}) {
			t.Errorf("ids = %v, want [1 2 3]", got)
		}
	})

	t.Run("exclude drops the listed ids", func(t *testing.T) {
		servers, err := load(t, []int{2}, nil, true)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got := serverIDs(servers); !equalInts(got, []int{1, 3}) {
			t.Errorf("ids = %v, want [1 3]", got)
		}
	})

	t.Run("server keeps only the listed ids", func(t *testing.T) {
		servers, err := load(t, nil, []int{3, 1}, true)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		// Selection preserves list order, not argument order.
		if got := serverIDs(servers); !equalInts(got, []int{1, 3}) {
			t.Errorf("ids = %v, want [1 3]", got)
		}
	})

	t.Run("server -1 means every server", func(t *testing.T) {
		servers, err := load(t, nil, []int{-1}, true)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got := serverIDs(servers); !equalInts(got, []int{1, 2, 3}) {
			t.Errorf("ids = %v, want [1 2 3]", got)
		}
	})

	t.Run("unknown server id is an error", func(t *testing.T) {
		_, err := load(t, nil, []int{99}, true)
		if err == nil {
			t.Fatal("want an error for an unknown server id")
		}
		if got, want := err.Error(), "specified server(s) not found: [99]"; got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
	})

	t.Run("exclude and server together are rejected", func(t *testing.T) {
		if _, err := load(t, []int{1}, []int{2}, true); err == nil {
			t.Fatal("want an error when both --exclude and --server are given")
		}
	})

	t.Run("filter disabled bypasses selection", func(t *testing.T) {
		servers, err := load(t, []int{1, 2, 3}, nil, false)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got := serverIDs(servers); !equalInts(got, []int{1, 2, 3}) {
			t.Errorf("ids = %v, want all three (--list ignores filters)", got)
		}
	})

	t.Run("malformed server url is an error", func(t *testing.T) {
		_, err := getLocalServersReader(forceNothing, io.NopCloser(strings.NewReader(`[{"id":1,"server":"http://[::1"}]`)), nil, nil, true)
		if err == nil {
			t.Fatal("want an error for a malformed server URL")
		}
	})
}

func TestGetLocalServers(t *testing.T) {
	t.Run("reads a file", func(t *testing.T) {
		path := writeServerList(t, threeServers)
		servers, err := getLocalServers(forceNothing, path, nil, nil, true)
		if err != nil {
			t.Fatalf("getLocalServers: %v", err)
		}
		if len(servers) != 3 {
			t.Errorf("got %d servers, want 3", len(servers))
		}
	})

	t.Run("missing file is an error", func(t *testing.T) {
		if _, err := getLocalServers(forceNothing, t.TempDir()+"/absent.json", nil, nil, true); !os.IsNotExist(err) {
			t.Errorf("error = %v, want a not-exist error", err)
		}
	})

	t.Run("invalid JSON is an error", func(t *testing.T) {
		if _, err := getLocalServers(forceNothing, writeServerList(t, "not json"), nil, nil, true); err == nil {
			t.Fatal("want a JSON error")
		}
	})

	t.Run("closes the reader", func(t *testing.T) {
		reader := &recordingReadCloser{Reader: strings.NewReader(threeServers)}
		if _, err := getLocalServersReader(forceNothing, reader, nil, nil, true); err != nil {
			t.Fatalf("getLocalServersReader: %v", err)
		}
		if !reader.closed {
			t.Error("the reader was not closed")
		}
	})
}

type recordingReadCloser struct {
	io.Reader
	closed bool
}

func (r *recordingReadCloser) Close() error {
	r.closed = true
	return nil
}

// TestGetServerList exercises the remote list path against an in-process
// server; no request leaves the loopback interface.
func TestGetServerList(t *testing.T) {
	t.Run("fetches and preprocesses", func(t *testing.T) {
		var gotUserAgent string
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUserAgent = r.Header.Get("User-Agent")
			_, _ = io.WriteString(w, threeServers)
		}))
		defer backend.Close()

		servers, err := getServerList(forceHttps, backend.URL, nil, nil, true)
		if err != nil {
			t.Fatalf("getServerList: %v", err)
		}
		if len(servers) != 3 {
			t.Fatalf("got %d servers, want 3", len(servers))
		}
		if gotUserAgent != defs.UserAgent {
			t.Errorf("User-Agent = %q, want %q", gotUserAgent, defs.UserAgent)
		}
		if servers[0].Server != "https://alpha.example/" {
			t.Errorf("scheme was not forced: %q", servers[0].Server)
		}
	})

	t.Run("exclude and server together are rejected before any request", func(t *testing.T) {
		if _, err := getServerList(forceNothing, "http://127.0.0.1:1", []int{1}, []int{2}, true); err == nil {
			t.Fatal("want an error when both --exclude and --server are given")
		} else if got, want := err.Error(), "either --exclude or --server can be used"; got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
	})

	t.Run("unreachable list is an error", func(t *testing.T) {
		if _, err := getServerList(forceNothing, "http://127.0.0.1:1", nil, nil, true); err == nil {
			t.Fatal("want an error when the list host is unreachable")
		}
	})

	t.Run("invalid JSON is an error", func(t *testing.T) {
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "<html>not a server list</html>")
		}))
		defer backend.Close()

		if _, err := getServerList(forceNothing, backend.URL, nil, nil, true); err == nil {
			t.Fatal("want a JSON error")
		}
	})

	t.Run("an error status is rejected even when the body parses", func(t *testing.T) {
		// A captive portal or a moved list can answer an error status with a
		// body that still unmarshals into a server list. The status decides.
		for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusMovedPermanently} {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, threeServers)
			}))

			_, err := getServerList(forceNothing, backend.URL, nil, nil, true)
			backend.Close()
			if err == nil {
				t.Errorf("HTTP %d was accepted, want an error", status)
				continue
			}
			if want := fmt.Sprintf("HTTP %d", status); !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to name %q", err, want)
			}
		}
	})

	t.Run("a non-200 success status is accepted", func(t *testing.T) {
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, threeServers)
		}))
		defer backend.Close()

		servers, err := getServerList(forceNothing, backend.URL, nil, nil, true)
		if err != nil {
			t.Fatalf("getServerList: %v", err)
		}
		if len(servers) != 3 {
			t.Errorf("got %d servers, want 3", len(servers))
		}
	})
}

func TestContains(t *testing.T) {
	if !contains([]int{1, 2, 3}, 2) {
		t.Error("contains([1 2 3], 2) = false")
	}
	if contains([]int{1, 2, 3}, 4) {
		t.Error("contains([1 2 3], 4) = true")
	}
	if contains(nil, 1) {
		t.Error("contains(nil, 1) = true")
	}
}

// TestListOutput pins the --list rendering, which is parsed by humans and by
// the repo's fixture scripts.
func TestListOutput(t *testing.T) {
	const withSponsor = `[{"id":7,"name":"delta","server":"http://delta.example/","sponsorName":"ACME","sponsorURL":"acme.example"}]`

	result := runSpeedTest(t, "--local-json", writeServerList(t, withSponsor), "--list")
	if result.Err != nil {
		t.Fatalf("--list: %v", result.Err)
	}
	want := "7: delta (http://delta.example/)  [Sponsor: ACME @ https://acme.example]\n"
	if result.Out != want {
		t.Errorf("stdout = %q, want %q", result.Out, want)
	}
}

// TestLocalJSONFromStdin pins the "--local-json -" form, which is how
// soundprobe pins its campus server definitions.
func TestLocalJSONFromStdin(t *testing.T) {
	previousStdin := os.Stdin
	t.Cleanup(func() { os.Stdin = previousStdin })

	path := writeServerList(t, threeServers)
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer file.Close()
	os.Stdin = file

	result := runSpeedTest(t, "--local-json", "-", "--list")
	if result.Err != nil {
		t.Fatalf("--local-json -: %v", result.Err)
	}
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		if !strings.Contains(result.Out, name) {
			t.Errorf("stdout is missing %q:\n%s", name, result.Out)
		}
	}
}

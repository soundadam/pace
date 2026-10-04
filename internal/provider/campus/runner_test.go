package campus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/soundadam/pace/internal/helper"
	"github.com/soundadam/pace/internal/model"
	"github.com/soundadam/pace/internal/provider"
)

func TestRunnerMeasuresIPv4WithExactArguments(t *testing.T) {
	runner, argsPath := newFakeRunner(t, "testdata/librespeed-success-ipv4.json", HelperVersion, 0, "")
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Status != model.ProviderStatusSuccess {
		t.Fatalf("status = %q", measurement.Status)
	}
	if measurement.IPFamily == nil || *measurement.IPFamily != "ipv4" {
		t.Fatalf("family = %v", measurement.IPFamily)
	}
	gotArgs := readArgs(t, argsPath)
	wantArgs := []string{
		"--local-json", "-",
		"--server", IPv4ServerID,
		"--duration", "10",
		"--concurrent", "3",
		"--no-icmp",
		"--telemetry-level", "disabled",
		"--json",
		"--progress-json",
		"--ipv4",
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	assertFakeServerList(t, argsPath)
}

func TestRunnerMeasuresIPv6WithoutFallback(t *testing.T) {
	runner, argsPath := newFakeRunner(t, "testdata/librespeed-success-ipv6.json", HelperVersion, 0, "")
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus, IPFamily: "ipv6"})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.ServerFQDN == nil || *measurement.ServerFQDN != "speed6.nju.edu.cn" {
		t.Fatalf("server = %v", measurement.ServerFQDN)
	}
	gotArgs := readArgs(t, argsPath)
	if !containsPair(gotArgs, "--local-json", "-") || !containsPair(gotArgs, "--server", IPv6ServerID) || !contains(gotArgs, "--ipv6") || contains(gotArgs, "--ipv4") || contains(gotArgs, "--server-json") {
		t.Fatalf("IPv6 arguments = %#v", gotArgs)
	}
	assertFakeServerList(t, argsPath)
}

func TestTargetRunnerUsesPinnedExternalStationIdentity(t *testing.T) {
	output := `[{"server":{"name":"Tongji","url":"https://dev.tongji.edu.cn/speedtest"},"client":{"ip":"203.0.113.42"},"bytes_sent":1000,"bytes_received":2000,"ping":10,"jitter":1,"download":20,"upload":10}]`
	runner, argsPath := newFakeRunner(t, "", HelperVersion, 0, output)
	runner.Config = Config{
		Provider:   model.ProviderTongjiIPv4,
		Label:      "Tongji · IPv4",
		Family:     "ipv4",
		ServerName: "Tongji",
		ServerURL:  "https://dev.tongji.edu.cn/speedtest",
	}
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandDomestic})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Provider != model.ProviderTongjiIPv4 || measurement.ServerFQDN == nil || *measurement.ServerFQDN != "dev.tongji.edu.cn" {
		t.Fatalf("measurement = %#v", measurement)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(argsPath), "stdin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"server":"https://dev.tongji.edu.cn/speedtest"`) {
		t.Fatalf("server list = %s", data)
	}
	if !contains(readArgs(t, argsPath), "--ipv4") {
		t.Fatalf("args = %#v", readArgs(t, argsPath))
	}
}

func TestRunnerReportsProgressJSON(t *testing.T) {
	runner, _ := newFakeRunner(t, "testdata/librespeed-success-ipv4.json", HelperVersion, 0, "")
	t.Setenv("PACE_FAKE_PROGRESS", strings.Join([]string{
		`{"type":"progress","test":"download","elapsed_ms":1000,"bytes":6250000,"mbps":50}`,
		`{"type":"progress","test":"upload","elapsed_ms":1000,"bytes":625000,"mbps":5}`,
	}, "\n"))
	var events []provider.ProgressEvent
	_, err := runner.Measure(context.Background(), provider.Request{
		Command: model.CommandCampus,
		Progress: func(event provider.ProgressEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var download, upload *provider.ProgressEvent
	for index := range events {
		event := &events[index]
		if event.LiveMbps == nil {
			continue
		}
		switch event.Test {
		case "download":
			download = event
		case "upload":
			upload = event
		}
	}
	if download == nil || download.Phase != provider.ProgressDownloading || *download.LiveMbps != 50 {
		t.Fatalf("download progress = %#v", download)
	}
	if upload == nil || upload.Phase != provider.ProgressUploading || *upload.LiveMbps != 5 {
		t.Fatalf("upload progress = %#v", upload)
	}
}

func TestRunnerRejectsMalformedProgressJSON(t *testing.T) {
	runner, _ := newFakeRunner(t, "testdata/librespeed-success-ipv4.json", HelperVersion, 0, "")
	t.Setenv("PACE_FAKE_PROGRESS", `{"type":"progress","test":"download","elapsed_ms":1000,"bytes":1,"mbps":1,"extra":true}`)
	_, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err == nil || !strings.Contains(err.Error(), "parse LibreSpeed progress output") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerRejectsWrongHelperVersion(t *testing.T) {
	runner, _ := newFakeRunner(t, "testdata/librespeed-success-ipv4.json", "v1.0.13", 0, "")
	_, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if !errors.Is(err, provider.ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestRunnerTimesOutVersionProbe(t *testing.T) {
	runner, _ := newFakeRunner(t, "testdata/librespeed-success-ipv4.json", HelperVersion, 0, "")
	t.Setenv("PACE_FAKE_VERSION_SLEEP", "1")
	runner.VersionTimeout = 25 * time.Millisecond
	_, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if !errors.Is(err, provider.ErrUnavailable) || !strings.Contains(err.Error(), "version probe timed out") {
		t.Fatalf("error = %v, want timed-out ErrUnavailable", err)
	}
}

func TestRunnerMapsEmptyResultToConnectFailure(t *testing.T) {
	runner, _ := newFakeRunner(t, "", HelperVersion, 0, "[]")
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageConnect || measurement.Failure.Code != "server_unreachable" {
		t.Fatalf("failure = %#v", measurement.Failure)
	}
}

func TestRunnerRejectsMalformedHelperOutput(t *testing.T) {
	runner, _ := newFakeRunner(t, "", HelperVersion, 0, "not-json")
	_, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err == nil || !strings.Contains(err.Error(), "parse LibreSpeed helper output") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerClassifiesDownloadFailure(t *testing.T) {
	runner, _ := newFakeRunner(t, "", HelperVersion, 1, "Failed to get download speed: connection reset by peer")
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageDownload || measurement.Failure.Code != "download_failure" {
		t.Fatalf("failure = %#v", measurement.Failure)
	}
	if measurement.DownloadMbps == nil || *measurement.DownloadMbps != 0 || measurement.UploadMbps == nil || *measurement.UploadMbps != 0 {
		t.Fatalf("failed speeds = %v/%v", measurement.DownloadMbps, measurement.UploadMbps)
	}
}

func TestRunnerClassifiesConnectionReset(t *testing.T) {
	message := `Get "http://speed.nju.edu.cn/backend/empty.php": read tcp4 198.51.100.10:50681->203.0.113.20:80: read: connection reset by peer`
	runner, _ := newFakeRunner(t, "", HelperVersion, 1, message)
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageConnect || measurement.Failure.Code != "connect_failure" {
		t.Fatalf("failure = %#v", measurement.Failure)
	}
	if measurement.DownloadMbps == nil || *measurement.DownloadMbps != 0 || measurement.UploadMbps == nil || *measurement.UploadMbps != 0 {
		t.Fatalf("failed speeds = %v/%v", measurement.DownloadMbps, measurement.UploadMbps)
	}
}

func TestValidateServerListRejectsUnexpectedSelectedServer(t *testing.T) {
	err := validateServerList([]byte(`[{"id":2,"server":"http://speed.nju.edu.cn"}]`), IPv6ServerID, "speed6.nju.edu.cn")
	if err == nil || !strings.Contains(err.Error(), "unexpected host") {
		t.Fatalf("error = %v", err)
	}
}

func TestPinnedServerListCoversBothFamilies(t *testing.T) {
	if err := validateServerList([]byte(pinnedServerListJSON), IPv4ServerID, "speed.nju.edu.cn"); err != nil {
		t.Fatal(err)
	}
	if err := validateServerList([]byte(pinnedServerListJSON), IPv6ServerID, "speed6.nju.edu.cn"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateServerListRejectsUnsafeScheme(t *testing.T) {
	err := validateServerList([]byte(`[{"id":1,"server":"ftp://speed.nju.edu.cn"}]`), IPv4ServerID, "speed.nju.edu.cn")
	if err == nil || !strings.Contains(err.Error(), "unsupported URL scheme") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerTimesOutMeasurement(t *testing.T) {
	runner, _ := newFakeRunner(t, "", HelperVersion, 0, "")
	t.Setenv("PACE_FAKE_SLEEP", "1")
	runner.Timeout = 25 * time.Millisecond
	measurement, err := runner.Measure(context.Background(), provider.Request{Command: model.CommandCampus})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageTimeout {
		t.Fatalf("failure = %#v", measurement.Failure)
	}
}

func TestRunnerPreservesCancellation(t *testing.T) {
	runner, _ := newFakeRunner(t, "", HelperVersion, 0, "")
	t.Setenv("PACE_FAKE_SLEEP", "5")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel()
	}()
	measurement, err := runner.Measure(ctx, provider.Request{Command: model.CommandCampus})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Status != model.ProviderStatusCancelled || measurement.Failure == nil || measurement.Failure.Stage != model.FailureStageCancelled {
		t.Fatalf("measurement = %#v", measurement)
	}
}

func newFakeRunner(t *testing.T, fixture, version string, exitCode int, literalOutput string) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	executable := filepath.Join(root, "prefix", "bin", "pace")
	helperPath := filepath.Join(root, "prefix", "libexec", "pace", HelperName)
	argsPath := filepath.Join(root, "args.txt")
	stdinPath := filepath.Join(root, "stdin.json")
	writeTestExecutable(t, executable, "#!/bin/sh\nexit 0\n")

	fixturePath := ""
	if fixture != "" {
		absolute, err := filepath.Abs(fixture)
		if err != nil {
			t.Fatal(err)
		}
		fixturePath = absolute
	}
	script := `#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  if [ -n "${PACE_FAKE_VERSION_SLEEP:-}" ]; then
    exec sleep "$PACE_FAKE_VERSION_SLEEP"
  fi
  printf 'librespeed-cli %s (built on test)\n'
  exit 0
fi
printf '%%s\n' "$@" > "$PACE_FAKE_ARGS"
cat > "$PACE_FAKE_STDIN"
if [ -n "${PACE_FAKE_SLEEP:-}" ]; then
  exec sleep "$PACE_FAKE_SLEEP"
fi
if [ -n "${PACE_FAKE_PROGRESS:-}" ]; then
  printf '%%s\n' "$PACE_FAKE_PROGRESS" >&2
fi
if [ -n "%s" ]; then
  cat "%s"
else
  printf '%%s' '%s'
fi
if [ %d -ne 0 ]; then
  printf '%%s\n' '%s' >&2
fi
exit %d
`
	script = fmt.Sprintf(script, version, fixturePath, fixturePath, literalOutput, exitCode, literalOutput, exitCode)
	writeTestExecutable(t, helperPath, script)
	t.Setenv("PACE_FAKE_ARGS", argsPath)
	t.Setenv("PACE_FAKE_STDIN", stdinPath)

	clock := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	return &Runner{
		Resolver: helper.Resolver{
			ExecutablePath:   executable,
			WorkingDirectory: root,
			LookupPath: func(string) (string, error) {
				return "", errors.New("not found")
			},
		},
		VersionTimeout: 30 * time.Second,
		Now: func() time.Time {
			clock = clock.Add(250 * time.Millisecond)
			return clock
		},
	}, argsPath
}

func assertFakeServerList(t *testing.T, argsPath string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(argsPath), "stdin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("stdin JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(pinnedServerListJSON), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stdin = %#v, want %#v", got, want)
	}
}

func writeTestExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func readArgs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsPair(values []string, first, second string) bool {
	for index := 0; index+1 < len(values); index++ {
		if values[index] == first && values[index+1] == second {
			return true
		}
	}
	return false
}

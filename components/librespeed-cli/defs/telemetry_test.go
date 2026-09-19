package defs

import (
	"strings"
	"testing"
)

// TestTelemetryLevelMapping pins the level -> integer mapping. helper.go only
// contacts the telemetry server when GetLevel() > 0, so "disabled" mapping to 0
// is what makes --telemetry-level disabled actually disable telemetry. An
// unknown or empty level falls through to disabled, which is the safe default.
func TestTelemetryLevelMapping(t *testing.T) {
	for _, tc := range []struct {
		level string
		want  int
	}{
		{TelemetryLevelDisabled, 0},
		{TelemetryLevelBasic, 1},
		{TelemetryLevelFull, 2},
		{TelemetryLevelDebug, 3},
		{"", 0},
		{"nonsense", 0},
	} {
		t.Run(tc.level, func(t *testing.T) {
			server := TelemetryServer{Level: tc.level}
			if got := server.GetLevel(); got != tc.want {
				t.Errorf("GetLevel() for %q = %d, want %d", tc.level, got, tc.want)
			}
		})
	}
}

func TestTelemetryLevelPredicates(t *testing.T) {
	server := TelemetryServer{Level: TelemetryLevelDisabled}
	if !server.Disabled() || server.Basic() || server.Full() || server.Debug() {
		t.Errorf("predicates for %q are wrong", server.Level)
	}
	server.Level = TelemetryLevelFull
	if server.Disabled() || server.Basic() || !server.Full() || server.Debug() {
		t.Errorf("predicates for %q are wrong", server.Level)
	}
}

// TestTelemetryLogRespectsLevel pins that the log body sent with telemetry is
// empty below level "full", so a basic-level run does not ship timing traces.
func TestTelemetryLogRespectsLevel(t *testing.T) {
	for _, tc := range []struct {
		name      string
		level     int
		wantLogf  bool
		wantVerbf bool
	}{
		{"disabled", 0, false, false},
		{"basic", 1, false, false},
		{"full", 2, true, false},
		{"debug", 3, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log TelemetryLog
			log.SetLevel(tc.level)
			log.Logf("plain %s", "entry")
			log.Warnf("warned %s", "entry")
			log.Verbosef("verbose %s", "entry")

			body := log.String()
			if got := strings.Contains(body, "plain entry"); got != tc.wantLogf {
				t.Errorf("Logf recorded = %v, want %v", got, tc.wantLogf)
			}
			if got := strings.Contains(body, "WARN: warned entry"); got != tc.wantLogf {
				t.Errorf("Warnf recorded = %v, want %v", got, tc.wantLogf)
			}
			if got := strings.Contains(body, "verbose entry"); got != tc.wantVerbf {
				t.Errorf("Verbosef recorded = %v, want %v", got, tc.wantVerbf)
			}
		})
	}
}

func TestTelemetryServerURLs(t *testing.T) {
	server := TelemetryServer{
		Server: "https://example.invalid/base",
		Path:   "/results/telemetry.php",
		Share:  "/results/",
	}

	path, err := server.GetPath()
	if err != nil {
		t.Fatalf("GetPath: %v", err)
	}
	if got, want := path.String(), "https://example.invalid/base/results/telemetry.php"; got != want {
		t.Errorf("GetPath() = %q, want %q", got, want)
	}

	share, err := server.GetShare()
	if err != nil {
		t.Fatalf("GetShare: %v", err)
	}
	if got, want := share.String(), "https://example.invalid/base/results/"; got != want {
		t.Errorf("GetShare() = %q, want %q", got, want)
	}

	broken := TelemetryServer{Server: "http://[::1"}
	if _, err := broken.GetPath(); err == nil {
		t.Error("GetPath() on a malformed server URL: want an error")
	}
	if _, err := broken.GetShare(); err == nil {
		t.Error("GetShare() on a malformed server URL: want an error")
	}
}

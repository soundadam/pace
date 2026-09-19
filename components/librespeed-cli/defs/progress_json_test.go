package defs

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/librespeed/speedtest-cli/output"
)

// consumerProgressEvent is a byte-for-byte replica of the struct the consuming
// process (soundprobe's campus runner) decodes each `--progress-json` line
// into. It is decoded here with DisallowUnknownFields, exactly as the consumer
// does, so that adding, renaming, removing or retyping any field of
// progressJSONReport fails in this module instead of silently breaking the
// consumer at run time.
type consumerProgressEvent struct {
	Type      string  `json:"type"`
	Test      string  `json:"test"`
	ElapsedMS int64   `json:"elapsed_ms"`
	Bytes     uint64  `json:"bytes"`
	Mbps      float64 `json:"mbps"`
}

// captureUI runs fn with the package-level output writer redirected and
// returns everything written to the UI (stderr) stream.
func captureUI(t *testing.T, fn func()) string {
	t.Helper()
	var ui bytes.Buffer
	restore := output.Redirect(io.Discard, &ui)
	defer restore()
	fn()
	return ui.String()
}

// counterAt returns a counter whose test clock started `elapsed` ago and which
// has observed `total` bytes.
func counterAt(elapsed time.Duration, total int) *BytesCounter {
	counter := NewCounter()
	counter.Start()
	counter.start = time.Now().Add(-elapsed)
	if total > 0 {
		_, _ = counter.Write(make([]byte, total))
	}
	return counter
}

// TestProgressJSONWireContractFieldSet pins the exact JSON object shape of a
// progress event: the key set, the key names and the JSON type of each value.
func TestProgressJSONWireContractFieldSet(t *testing.T) {
	out := captureUI(t, func() {
		writeProgressJSON("download", counterAt(2*time.Second, 250000))
	})

	line := strings.TrimSuffix(out, "\n")
	var generic map[string]any
	if err := json.Unmarshal([]byte(line), &generic); err != nil {
		t.Fatalf("progress event is not a JSON object: %v (line %q)", err, line)
	}

	want := map[string]string{
		"type":       "string",
		"test":       "string",
		"elapsed_ms": "number",
		"bytes":      "number",
		"mbps":       "number",
	}
	if len(generic) != len(want) {
		t.Fatalf("progress event has %d fields %v, want exactly %d %v", len(generic), keysOf(generic), len(want), keysOf(want))
	}
	for key, kind := range want {
		value, ok := generic[key]
		if !ok {
			t.Fatalf("progress event is missing field %q (got %v)", key, keysOf(generic))
		}
		switch kind {
		case "string":
			if _, ok := value.(string); !ok {
				t.Errorf("field %q = %#v, want a JSON string", key, value)
			}
		case "number":
			if _, ok := value.(float64); !ok {
				t.Errorf("field %q = %#v, want a JSON number", key, value)
			}
		}
	}

	if generic["type"] != "progress" {
		t.Errorf(`type = %v, want "progress"`, generic["type"])
	}
	if generic["test"] != "download" {
		t.Errorf(`test = %v, want "download"`, generic["test"])
	}
}

// TestProgressJSONDecodesUnderConsumerStrictDecoder is the direct guard for the
// consumer: it decodes with DisallowUnknownFields and requires the line to hold
// exactly one JSON value.
func TestProgressJSONDecodesUnderConsumerStrictDecoder(t *testing.T) {
	for _, test := range []string{"download", "upload"} {
		out := captureUI(t, func() {
			writeProgressJSON(test, counterAt(1500*time.Millisecond, 187500))
		})
		line := strings.TrimSuffix(out, "\n")

		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.DisallowUnknownFields()
		var event consumerProgressEvent
		if err := decoder.Decode(&event); err != nil {
			t.Fatalf("strict decode of %q: %v", line, err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			t.Fatalf("line %q carries trailing data (err=%v)", line, err)
		}

		if event.Type != "progress" {
			t.Errorf("Type = %q, want %q", event.Type, "progress")
		}
		if event.Test != test {
			t.Errorf("Test = %q, want %q", event.Test, test)
		}
		if event.ElapsedMS <= 0 {
			t.Errorf("ElapsedMS = %d, want > 0 (the consumer rejects non-positive values)", event.ElapsedMS)
		}
		if event.Bytes != 187500 {
			t.Errorf("Bytes = %d, want 187500", event.Bytes)
		}
		if math.IsNaN(event.Mbps) || math.IsInf(event.Mbps, 0) || event.Mbps < 0 {
			t.Errorf("Mbps = %v, want finite and non-negative", event.Mbps)
		}
	}
}

// TestProgressJSONIsOneObjectPerLine pins the framing: each emission is a
// single compact JSON object terminated by exactly one newline, with no
// newline inside the object, so a line-oriented consumer never has to buffer
// across writes.
func TestProgressJSONIsOneObjectPerLine(t *testing.T) {
	counter := counterAt(time.Second, 125000)
	out := captureUI(t, func() {
		writeProgressJSON("download", counter)
		writeProgressJSON("download", counter)
		writeProgressJSON("upload", counter)
	})

	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("stream %q does not end with a newline", out)
	}
	if strings.Contains(out, "\r") {
		t.Errorf("stream contains a carriage return: %q", out)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(lines), out)
	}
	for i, line := range lines {
		if line != strings.TrimSpace(line) {
			t.Errorf("line %d has surrounding whitespace: %q", i, line)
		}
		var event consumerProgressEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Errorf("line %d is not a progress object: %v (%q)", i, err, line)
		}
	}
	if got := lines[2]; !strings.Contains(got, `"test":"upload"`) {
		t.Errorf("third line = %q, want the upload event", got)
	}
}

// TestProgressJSONMbpsIsMegabitsPerSecond pins the unit of the mbps field:
// bytes / seconds / 125000, i.e. megabits per second with a decimal (1e6)
// megabit, independent of how many samples have been emitted.
func TestProgressJSONMbpsIsMegabitsPerSecond(t *testing.T) {
	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		bytes   int
		want    float64
	}{
		{"one megabit per second", 2 * time.Second, 250000, 1},
		{"eight megabits per second", time.Second, 1000000, 8},
		{"no bytes yet", time.Second, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := captureUI(t, func() {
				writeProgressJSON("download", counterAt(tc.elapsed, tc.bytes))
			})
			var event consumerProgressEvent
			if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &event); err != nil {
				t.Fatalf("decode %q: %v", out, err)
			}
			// The counter clock is read at emit time, so allow a small
			// tolerance for the time spent between counterAt and the emit.
			if math.Abs(event.Mbps-tc.want) > tc.want*0.05+1e-9 {
				t.Errorf("mbps = %v, want ~%v", event.Mbps, tc.want)
			}
			if event.Bytes != uint64(tc.bytes) {
				t.Errorf("bytes = %d, want %d", event.Bytes, tc.bytes)
			}
		})
	}
}

// TestProgressJSONMbpsIgnoresMebibyteFlag pins that --mebibytes does not change
// the progress stream's unit, even though it does change the final report's
// AvgMbps. The consumer assumes a single fixed unit.
func TestProgressJSONMbpsIgnoresMebibyteFlag(t *testing.T) {
	counter := counterAt(time.Second, 131072)
	counter.SetMebi(true)

	out := captureUI(t, func() { writeProgressJSON("download", counter) })
	var event consumerProgressEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &event); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}

	want := 131072.0 / 125000.0
	if math.Abs(event.Mbps-want) > want*0.05 {
		t.Errorf("mbps = %v, want ~%v (decimal megabits, not mebibits)", event.Mbps, want)
	}
	if mebi := counter.AvgMbps(); math.Abs(mebi-1) > 0.05 {
		t.Fatalf("precondition: AvgMbps with --mebibytes = %v, want ~1", mebi)
	}
}

// TestProgressJSONNeverEmitsNonPositiveElapsed pins the producer invariant that
// matches the consumer's validation: a sample is emitted only when it can carry
// a positive elapsed_ms.
func TestProgressJSONNeverEmitsNonPositiveElapsed(t *testing.T) {
	t.Run("clock in the future emits nothing", func(t *testing.T) {
		if out := captureUI(t, func() {
			writeProgressJSON("download", counterAt(-time.Hour, 1000))
		}); out != "" {
			t.Errorf("emitted %q for a negative elapsed time, want nothing", out)
		}
	})

	t.Run("sub-millisecond sample emits nothing", func(t *testing.T) {
		out := captureUI(t, func() {
			writeProgressJSON("download", counterAt(100*time.Microsecond, 1000))
		})
		if out == "" {
			return
		}
		// If the machine stalled long enough for the sample to cross a
		// millisecond it may legitimately be emitted, but never with a
		// zero elapsed_ms.
		var event consumerProgressEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &event); err != nil {
			t.Fatalf("decode %q: %v", out, err)
		}
		if event.ElapsedMS <= 0 {
			t.Errorf("emitted elapsed_ms = %d, want > 0 (the consumer treats this as a fatal error)", event.ElapsedMS)
		}
	})
}

// TestProgressJSONSamplesAreMonotonic pins the ordering guarantee within a
// single test phase: elapsed_ms is non-decreasing and bytes is non-decreasing.
func TestProgressJSONSamplesAreMonotonic(t *testing.T) {
	counter := counterAt(time.Second, 1000)
	out := captureUI(t, func() {
		for i := 0; i < 5; i++ {
			writeProgressJSON("download", counter)
			_, _ = counter.Write(make([]byte, 1000))
			time.Sleep(2 * time.Millisecond)
		}
	})

	var previous consumerProgressEvent
	for i, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		var event consumerProgressEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if i > 0 {
			if event.ElapsedMS < previous.ElapsedMS {
				t.Errorf("line %d: elapsed_ms went backwards %d -> %d", i, previous.ElapsedMS, event.ElapsedMS)
			}
			if event.Bytes < previous.Bytes {
				t.Errorf("line %d: bytes went backwards %d -> %d", i, previous.Bytes, event.Bytes)
			}
		}
		previous = event
	}
}

// TestProgressJSONTicker pins the sampling cadence and the disabled path.
func TestProgressJSONTicker(t *testing.T) {
	t.Run("interval", func(t *testing.T) {
		if progressJSONInterval != 200*time.Millisecond {
			t.Errorf("progressJSONInterval = %v, want 200ms", progressJSONInterval)
		}
	})

	t.Run("disabled yields a nil ticker and a nil channel", func(t *testing.T) {
		ticker, channel := progressJSONTicker(false)
		if ticker != nil {
			t.Errorf("ticker = %v, want nil", ticker)
		}
		if channel != nil {
			t.Fatalf("channel = %v, want nil", channel)
		}
		// A nil channel blocks forever, so the select case in Download and
		// Upload never fires when --progress-json is absent.
		select {
		case <-channel:
			t.Error("received from the nil progress channel")
		case <-time.After(50 * time.Millisecond):
		}
	})

	t.Run("enabled fires repeatedly", func(t *testing.T) {
		ticker, channel := progressJSONTicker(true)
		if ticker == nil || channel == nil {
			t.Fatal("enabled ticker must be non-nil")
		}
		defer ticker.Stop()
		for i := 0; i < 2; i++ {
			select {
			case <-channel:
			case <-time.After(2 * time.Second):
				t.Fatalf("tick %d did not arrive", i)
			}
		}
	})
}

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

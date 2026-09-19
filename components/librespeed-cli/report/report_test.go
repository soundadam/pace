package report

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/gocarina/gocsv"
	"github.com/librespeed/speedtest-cli/defs"
)

// TestJSONReportKeys pins the key names of the final JSON document, which is
// the result contract the calling process decodes.
func TestJSONReportKeys(t *testing.T) {
	body, err := json.Marshal(JSONReport{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(body, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{
		"timestamp", "server", "client",
		"bytes_sent", "bytes_received",
		"ping", "jitter", "upload", "download", "share",
	} {
		if _, ok := generic[key]; !ok {
			t.Errorf("JSON report is missing the key %q", key)
		}
	}

	server, ok := generic["server"].(map[string]any)
	if !ok {
		t.Fatalf("server = %#v, want an object", generic["server"])
	}
	for _, key := range []string{"name", "url"} {
		if _, ok := server[key]; !ok {
			t.Errorf("server object is missing the key %q", key)
		}
	}
}

// TestJSONReportClientIsInlined pins that the client's IP information is
// flattened into the client object rather than nested one level deeper.
func TestJSONReportClientIsInlined(t *testing.T) {
	rep := JSONReport{Client: Client{IPInfoResponse: defs.IPInfoResponse{
		IP:           "203.0.113.1",
		Organization: "AS64496 Example",
	}}}

	body, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Client struct {
			IP  string `json:"ip"`
			Org string `json:"org"`
		} `json:"client"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Client.IP != "203.0.113.1" {
		t.Errorf("client.ip = %q, want 203.0.113.1", decoded.Client.IP)
	}
	if decoded.Client.Org != "AS64496 Example" {
		t.Errorf("client.org = %q, want %q", decoded.Client.Org, "AS64496 Example")
	}

	// readme is dropped when empty so the report stays compact.
	if bytes.Contains(body, []byte(`"readme"`)) {
		t.Errorf("empty readme was serialised: %s", body)
	}
}

// TestCSVReportColumns pins the CSV column order and headers.
func TestCSVReportColumns(t *testing.T) {
	previous := gocsv.TagSeparator
	defer func() { gocsv.TagSeparator = previous }()
	gocsv.TagSeparator = ","

	rows := []CSVReport{{
		Timestamp: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
		Name:      "in-process",
		Address:   "http://127.0.0.1/",
		Ping:      1.5,
		Jitter:    0.25,
		Download:  100,
		Upload:    50,
		Share:     "",
		IP:        "203.0.113.1",
	}}

	body, err := gocsv.MarshalString(&rows)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	header := "Timestamp,Server Name,Address,Ping,Jitter,Download,Upload,Share,IP"
	if got := body[:len(header)]; got != header {
		t.Errorf("header = %q, want %q", got, header)
	}
}

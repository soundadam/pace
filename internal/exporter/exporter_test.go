package exporter

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/soundadam/pace/internal/model"
)

func TestWriteJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.jsonl")
	summaries := []model.RunSummary{testSummary()}
	if err := Write(path, "jsonl", summaries); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var decoded model.RunSummary
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("decode JSONL: %v", err)
	}
	if decoded.RunID != summaries[0].RunID {
		t.Fatalf("run ID = %q, want %q", decoded.RunID, summaries[0].RunID)
	}
}

func TestWriteCSVPreservesNullAsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.csv")
	summary := testSummary()
	summary.Measurements[0].UploadMbps = nil
	if err := Write(path, "csv", []model.RunSummary{summary}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("rows = %d, want 2", len(records))
	}
	if records[1][16] != "" {
		t.Fatalf("measurement upload = %q, want empty for null", records[1][16])
	}
	if records[1][7] != string(model.ProviderCampus) {
		t.Fatalf("target = %q", records[1][7])
	}
	if records[0][len(records[0])-5] != "server_id" || records[0][len(records[0])-1] != "download_responsiveness_rpm" {
		t.Fatalf("new metadata headers missing: %#v", records[0])
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("export mode = %o, want 600", info.Mode().Perm())
	}
}

func TestInvalidFormatDoesNotTruncateExistingOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, "xml", nil); err == nil {
		t.Fatal("Write() succeeded with unsupported format")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("existing output = %q, want keep", data)
	}
}

func testSummary() model.RunSummary {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	return model.RunSummary{
		SchemaVersion: model.SchemaVersion,
		RunID:         "00000000-0000-4000-8000-000000000001",
		ToolVersion:   "test",
		StartedAt:     now,
		EndedAt:       now.Add(time.Second),
		Command:       model.CommandCampus,
		Status:        model.RunStatusSuccess,
		Measurements: []model.Measurement{{
			Provider:     model.ProviderCampus,
			Method:       "librespeed-three-stream",
			Status:       model.ProviderStatusSuccess,
			DownloadMbps: model.Pointer(100.0),
			UploadMbps:   model.Pointer(50.0),
		}},
	}
}

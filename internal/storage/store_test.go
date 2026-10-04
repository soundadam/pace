package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/soundadam/pace/internal/model"
)

func TestSaveLoadAndModes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "history", "v1")
	store := New(root)
	summary := testSummary("run-1", time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC))

	if err := store.Save(summary); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	directoryInfo, err := os.Stat(root)
	if err != nil {
		t.Fatalf("Stat(history) error = %v", err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("history mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(filepath.Join(root, "run-1.json"))
	if err != nil {
		t.Fatalf("Stat(summary) error = %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("summary mode = %o, want 600", got)
	}

	loaded, err := store.Load("run-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.RunID != summary.RunID || loaded.Status != summary.Status {
		t.Fatalf("Load() = %#v, want %#v", loaded, summary)
	}
}

func TestListNewestFirstAndLimit(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "history"))
	older := testSummary("older", time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC))
	newer := testSummary("newer", time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC))
	if err := store.Save(older); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(newer); err != nil {
		t.Fatal(err)
	}

	items, err := store.List(1)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].RunID != "newer" {
		t.Fatalf("List(1) = %#v, want newest only", items)
	}
}

func testSummary(runID string, startedAt time.Time) model.RunSummary {
	return model.RunSummary{
		SchemaVersion: model.SchemaVersion,
		RunID:         runID,
		ToolVersion:   "test",
		StartedAt:     startedAt,
		EndedAt:       startedAt.Add(time.Second),
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

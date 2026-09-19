package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/soundadam/soundprobe/internal/configdir"
	"github.com/soundadam/soundprobe/internal/model"
)

var safeRunID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Store struct {
	HistoryDir string
}

func New(historyDir string) *Store {
	return &Store{HistoryDir: historyDir}
}

func DefaultHistoryDir() (string, error) {
	return configdir.Path("history", "v2")
}

func (store *Store) Save(summary model.RunSummary) error {
	if store == nil || store.HistoryDir == "" {
		return errors.New("history store is not configured")
	}
	if err := summary.Validate(); err != nil {
		return fmt.Errorf("validate summary: %w", err)
	}
	if !safeRunID.MatchString(summary.RunID) {
		return errors.New("run ID contains unsafe path characters")
	}

	if err := os.MkdirAll(store.HistoryDir, 0o700); err != nil {
		return fmt.Errorf("create history directory: %w", err)
	}
	if err := os.Chmod(store.HistoryDir, 0o700); err != nil {
		return fmt.Errorf("set history directory mode: %w", err)
	}

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("encode summary: %w", err)
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(store.HistoryDir, "."+summary.RunID+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary summary: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}

	if err := temporary.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("set temporary summary mode: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temporary summary: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temporary summary: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close temporary summary: %w", err)
	}

	finalPath := store.path(summary.RunID)
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("replace summary atomically: %w", err)
	}
	if err := os.Chmod(finalPath, 0o600); err != nil {
		return fmt.Errorf("set summary mode: %w", err)
	}
	if err := syncDirectory(store.HistoryDir); err != nil {
		return err
	}
	return nil
}

func (store *Store) Load(runID string) (model.RunSummary, error) {
	if store == nil || store.HistoryDir == "" {
		return model.RunSummary{}, errors.New("history store is not configured")
	}
	if !safeRunID.MatchString(runID) {
		return model.RunSummary{}, errors.New("run ID contains unsafe path characters")
	}

	data, err := os.ReadFile(store.path(runID))
	if err != nil {
		return model.RunSummary{}, fmt.Errorf("read summary: %w", err)
	}
	var summary model.RunSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return model.RunSummary{}, fmt.Errorf("decode summary: %w", err)
	}
	if err := summary.Validate(); err != nil {
		return model.RunSummary{}, fmt.Errorf("validate summary: %w", err)
	}
	return summary, nil
}

// List returns saved runs newest first, together with the names of the files it
// could not read.  History accumulates across releases and is never pruned
// automatically, so a summary written by an older, incompatible schema version
// must not hide every other run: unreadable files are skipped and named rather
// than failing the call.  They are left untouched on disk, and naming them is
// the caller's job, which keeps a `--json` stdout a single document.
//
// A limit of zero returns every readable run.
func (store *Store) List(limit int) ([]model.RunSummary, []string, error) {
	if store == nil || store.HistoryDir == "" {
		return nil, nil, errors.New("history store is not configured")
	}
	if limit < 0 {
		return nil, nil, errors.New("history limit must be non-negative")
	}

	entries, err := os.ReadDir(store.HistoryDir)
	if errors.Is(err, os.ErrNotExist) {
		return []model.RunSummary{}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read history directory: %w", err)
	}

	summaries := make([]model.RunSummary, 0, len(entries))
	skipped := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		runID := strings.TrimSuffix(entry.Name(), ".json")
		summary, err := store.Load(runID)
		if err != nil {
			skipped = append(skipped, entry.Name())
			continue
		}
		summaries = append(summaries, summary)
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].StartedAt.After(summaries[j].StartedAt)
	})
	if limit > 0 && len(summaries) > limit {
		summaries = summaries[:limit]
	}
	return summaries, skipped, nil
}

func (store *Store) path(runID string) string {
	return filepath.Join(store.HistoryDir, runID+".json")
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open history directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync history directory: %w", err)
	}
	return nil
}

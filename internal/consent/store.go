package consent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	PolicyVersion = "v5-2026-05-03"
	PolicyURL     = "https://www.measurementlab.net/privacy/"
)

type Record struct {
	SchemaVersion int       `json:"schemaVersion"`
	Provider      string    `json:"provider"`
	PolicyVersion string    `json:"policyVersion"`
	AcceptedAt    time.Time `json:"acceptedAt"`
	ToolVersion   string    `json:"toolVersion"`
}

type Store struct {
	Path string
}

func New(path string) *Store {
	return &Store{Path: path}
}

func DefaultPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(configDir, "soundprobe", "consent.json"), nil
}

func (store *Store) Status() (Record, bool, error) {
	if store == nil || store.Path == "" {
		return Record{}, false, errors.New("consent store is not configured")
	}
	data, err := os.ReadFile(store.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("read consent: %w", err)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, false, fmt.Errorf("decode consent: %w", err)
	}
	if record.SchemaVersion != 1 || record.Provider != "mlab" || record.PolicyVersion == "" || record.AcceptedAt.IsZero() {
		return Record{}, false, errors.New("consent record is invalid")
	}
	return record, record.PolicyVersion == PolicyVersion, nil
}

func (store *Store) Accept(toolVersion string, acceptedAt time.Time) (Record, error) {
	if store == nil || store.Path == "" {
		return Record{}, errors.New("consent store is not configured")
	}
	record := Record{
		SchemaVersion: 1,
		Provider:      "mlab",
		PolicyVersion: PolicyVersion,
		AcceptedAt:    acceptedAt.UTC(),
		ToolVersion:   toolVersion,
	}
	if err := writeAtomic(store.Path, record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (store *Store) Revoke() error {
	if store == nil || store.Path == "" {
		return errors.New("consent store is not configured")
	}
	err := os.Remove(store.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove consent: %w", err)
	}
	return nil
}

func writeAtomic(path string, record Record) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create consent directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("set consent directory mode: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode consent: %w", err)
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(directory, ".consent.*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary consent: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("set temporary consent mode: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temporary consent: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temporary consent: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close temporary consent: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("replace consent atomically: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set consent mode: %w", err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open consent directory for sync: %w", err)
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return fmt.Errorf("sync consent directory: %w", err)
	}
	return nil
}

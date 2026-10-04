package preferences

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/soundadam/pace/internal/target"
)

const SchemaVersion = 1

type Language string

const (
	LanguageChinese Language = "zh-CN"
	LanguageEnglish Language = "en"
)

type Config struct {
	SchemaVersion int      `json:"schemaVersion"`
	Language      Language `json:"language"`
	DailyStations []string `json:"dailyStations"`
}

type Store struct {
	Path string
}

func New(path string) *Store { return &Store{Path: path} }

func DefaultPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(configDir, "pace", "preferences.json"), nil
}

func DefaultConfig() Config {
	stations := []string{"nju-campus", "mlab"}
	if runtime.GOOS == "darwin" {
		stations = append(stations, "apple")
	}
	return Config{SchemaVersion: SchemaVersion, Language: LanguageChinese, DailyStations: stations}
}

func (config Config) Validate() error {
	if config.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported preferences schema %d", config.SchemaVersion)
	}
	if config.Language != LanguageChinese && config.Language != LanguageEnglish {
		return fmt.Errorf("unsupported language %q", config.Language)
	}
	if len(config.DailyStations) == 0 {
		return errors.New("at least one daily station is required")
	}
	seen := map[string]struct{}{}
	for _, id := range config.DailyStations {
		station, ok := target.StationByID(id)
		if !ok || !station.TerminalSupported || !station.DailyEligible {
			return fmt.Errorf("unsupported daily station %q", id)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate daily station %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (store *Store) Load() (Config, bool, error) {
	if store == nil || store.Path == "" {
		return Config{}, false, errors.New("preferences store is not configured")
	}
	data, err := os.ReadFile(store.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("read preferences: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, false, fmt.Errorf("decode preferences: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, false, fmt.Errorf("validate preferences: %w", err)
	}
	return config, true, nil
}

func (store *Store) Save(config Config) error {
	if store == nil || store.Path == "" {
		return errors.New("preferences store is not configured")
	}
	if err := config.Validate(); err != nil {
		return err
	}
	directory := filepath.Dir(store.Path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create preferences directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("set preferences directory mode: %w", err)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preferences: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(directory, ".preferences.*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary preferences: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = temporary.Close(); _ = os.Remove(temporaryPath) }
	if err := temporary.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("set temporary preferences mode: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temporary preferences: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temporary preferences: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close temporary preferences: %w", err)
	}
	if err := os.Rename(temporaryPath, store.Path); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("replace preferences atomically: %w", err)
	}
	if err := os.Chmod(store.Path, 0o600); err != nil {
		return fmt.Errorf("set preferences mode: %w", err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open preferences directory for sync: %w", err)
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return fmt.Errorf("sync preferences directory: %w", err)
	}
	return nil
}

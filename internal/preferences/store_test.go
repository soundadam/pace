package preferences

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTripAndModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app", "preferences.json")
	store := New(path)
	config := Config{SchemaVersion: SchemaVersion, DailyStations: []string{"tongji", "mlab"}}
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}
	loaded, exists, err := store.Load()
	if err != nil || !exists {
		t.Fatalf("Load() = %#v, %t, %v", loaded, exists, err)
	}
	if len(loaded.DailyStations) != 2 || loaded.DailyStations[0] != "tongji" {
		t.Fatalf("loaded = %#v", loaded)
	}
	for _, item := range []struct {
		path string
		mode os.FileMode
	}{{filepath.Dir(path), 0o700}, {path, 0o600}} {
		info, err := os.Stat(item.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != item.mode {
			t.Fatalf("%s mode = %o, want %o", item.path, info.Mode().Perm(), item.mode)
		}
	}
}

func TestLoadReportsLegacySchemaAndGarbageAsUnusable(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
	}{
		{name: "legacy schema", content: `{"schemaVersion":1,"language":"zh-CN","dailyStations":["tongji"]}`},
		{name: "malformed json", content: "{not json"},
		{name: "empty stations", content: `{"schemaVersion":2,"dailyStations":[]}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "preferences.json")
			if err := os.WriteFile(path, []byte(testCase.content), 0o600); err != nil {
				t.Fatal(err)
			}
			config, exists, err := New(path).Load()
			if exists {
				t.Fatalf("exists = true for unusable preferences: %#v", config)
			}
			if !errors.Is(err, ErrUnusable) {
				t.Fatalf("err = %v, want ErrUnusable", err)
			}
		})
	}
}

func TestLoadReportsMissingFileAsNotConfigured(t *testing.T) {
	config, exists, err := New(filepath.Join(t.TempDir(), "preferences.json")).Load()
	if exists || err != nil {
		t.Fatalf("Load() = %#v, %t, %v", config, exists, err)
	}
}

func TestDefaultPathIsUnderConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(configDir, "soundprobe", "preferences.json")

	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != want {
		t.Fatalf("DefaultPath() = %q, want %q", path, want)
	}
}

func TestConfigRejectsWebOnlyStation(t *testing.T) {
	config := DefaultConfig()
	config.DailyStations = []string{"nju-edge"}
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() accepted web-only station")
	}
}

func TestConfigRejectsUnavailableCERNETStation(t *testing.T) {
	config := DefaultConfig()
	config.DailyStations = []string{"cernet"}
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() accepted unavailable CERNET station")
	}
}

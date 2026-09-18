package consent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcceptStatusAndRevoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app", "consent.json")
	store := New(path)
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)

	if _, accepted, err := store.Status(); err != nil || accepted {
		t.Fatalf("initial Status() = accepted %v, error %v", accepted, err)
	}
	record, err := store.Accept("test", now)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if record.PolicyVersion != PolicyVersion {
		t.Fatalf("policy version = %q, want %q", record.PolicyVersion, PolicyVersion)
	}
	_, accepted, err := store.Status()
	if err != nil || !accepted {
		t.Fatalf("Status() after accept = accepted %v, error %v", accepted, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("consent mode = %o, want 600", info.Mode().Perm())
	}
	if err := store.Revoke(); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if _, accepted, err := store.Status(); err != nil || accepted {
		t.Fatalf("Status() after revoke = accepted %v, error %v", accepted, err)
	}
}

func TestDefaultPathIsAlwaysCurrentPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(configDir, "soundprobe", "consent.json")

	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != want {
		t.Fatalf("DefaultPath() = %q, want %q", path, want)
	}

	// A pre-rename njuprobe consent file must not influence the result.
	legacy := filepath.Join(configDir, "njuprobe", "consent.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err = DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != want {
		t.Fatalf("DefaultPath() with legacy file present = %q, want %q", path, want)
	}
}

func TestStatusWithOnlyLegacyConsentIsNotAcceptedWithoutError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(configDir, "njuprobe", "consent.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	record := []byte(`{"schemaVersion":1,"provider":"mlab","policyVersion":"` + PolicyVersion + `","acceptedAt":"2026-07-21T08:00:00Z","toolVersion":"legacy"}` + "\n")
	if err := os.WriteFile(legacy, record, 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	_, accepted, err := New(path).Status()
	if err != nil {
		t.Fatalf("Status() error = %v, want nil", err)
	}
	if accepted {
		t.Fatal("Status() = accepted, want re-prompt for legacy-only consent")
	}
}

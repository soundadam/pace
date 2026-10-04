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

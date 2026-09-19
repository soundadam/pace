package configdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathJoinsBeneathUserConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	// Derive the expectation from os.UserConfigDir so it holds on every
	// platform rather than assuming the Unix layout.
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		elements []string
		want     string
	}{
		{name: "root", elements: nil, want: filepath.Join(configDir, "soundprobe")},
		{name: "file", elements: []string{"consent.json"}, want: filepath.Join(configDir, "soundprobe", "consent.json")},
		{name: "nested", elements: []string{"history", "v2"}, want: filepath.Join(configDir, "soundprobe", "history", "v2")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path, err := Path(test.elements...)
			if err != nil {
				t.Fatalf("Path(%q) error = %v", test.elements, err)
			}
			if path != test.want {
				t.Fatalf("Path(%q) = %q, want %q", test.elements, path, test.want)
			}
		})
	}
}

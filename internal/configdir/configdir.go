// Package configdir resolves paths inside soundprobe's per-user configuration
// directory. It is the single definition of that directory: every store that
// keeps state on disk derives its default location from Path, so the stores
// cannot drift apart.
package configdir

import (
	"fmt"
	"os"
	"path/filepath"
)

// appDirName is soundprobe's directory inside the user configuration
// directory. It is written exactly once, here.
const appDirName = "soundprobe"

// Path joins elements beneath soundprobe's configuration directory. With no
// elements it returns the directory itself. The user configuration directory
// is whatever os.UserConfigDir reports for the platform, so the result honours
// XDG_CONFIG_HOME on Unix and AppData on Windows.
func Path(elements ...string) (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(append([]string{configDir, appDirName}, elements...)...), nil
}

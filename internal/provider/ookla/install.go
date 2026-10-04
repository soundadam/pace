package ookla

// HomebrewInstallCommands returns the official Homebrew setup sequence for
// the external Ookla CLI.  The commands are data rather than a shell string
// so callers can display and execute them without invoking a shell or
// interpolating user-controlled input.
func HomebrewInstallCommands() [][]string {
	return [][]string{
		{"brew", "tap", "teamookla/speedtest"},
		{"brew", "update"},
		{"brew", "install", "speedtest", "--force"},
	}
}

// HomebrewConflictCommands are intentionally not executed by pace.
// Removing formulas is destructive and may remove a user's unrelated tool;
// these commands are shown only as a manual recovery option if installation
// reports a file/formula conflict.
func HomebrewConflictCommands() [][]string {
	return [][]string{
		{"brew", "uninstall", "speedtest", "--force"},
		{"brew", "uninstall", "speedtest-cli", "--force"},
		{"brew", "install", "speedtest", "--force"},
	}
}

package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// docs/reference/cli.mdx is the user-facing command reference.  Nothing used
// to check it against the cobra tree, and it drifted.  This test walks the
// real tree and compares it with the command synopsis block in that file.
//
// WHAT IT GUARANTEES, in both directions:
//   - every command in the cobra tree has a line in the synopsis block, and
//     every command named there still exists;
//   - every flag a command defines appears on that command's synopsis line,
//     and every flag written there still exists on that command.
//
// WHAT IT DOES NOT GUARANTEE:
//   - the prose, the tables and the examples elsewhere in the document are
//     not parsed at all.  A stale default, an outdated exit-code table or a
//     wrong description will not be caught here;
//   - flag *types*, defaults, shorthands and help strings are not compared;
//   - placeholders (LIST, RUN_ID, PATH, ipv4|ipv6|dual) are ignored, so a
//     changed value vocabulary is not caught;
//   - cobra's own `help` and `completion` commands are excluded: they are
//     generated, not part of soundprobe's documented surface.
//
// The test only reads the document.  When it fails, fix whichever side is
// wrong by hand.

const cliDocPath = "../../docs/reference/cli.mdx"

// globalFlags are documented once in prose rather than on every synopsis
// line, so they are accepted on any command and required on none.
var globalFlags = map[string]bool{"json": true, "help": true}

// documentedCommand is one line of the synopsis block.
type documentedCommand struct {
	flags map[string]bool
	line  string
}

// parseCLIDoc extracts the first ```text block that follows the "## 命令总览"
// (command overview) heading.  Parsing is deliberately conservative: anything
// it does not recognise as a command line is skipped rather than guessed at.
func parseCLIDoc(t *testing.T, path string) map[string]documentedCommand {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")

	inOverview := false
	inBlock := false
	var block []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			if inBlock {
				break
			}
			inOverview = strings.Contains(trimmed, "命令总览")
			continue
		}
		if !inOverview {
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			if inBlock {
				break
			}
			inBlock = true
			continue
		}
		if inBlock {
			block = append(block, trimmed)
		}
	}
	if len(block) == 0 {
		t.Fatalf("%s: no command synopsis block found under the 命令总览 heading", path)
	}

	documented := map[string]documentedCommand{}
	for _, line := range block {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if fields[0] != "soundprobe" {
			t.Fatalf("%s: synopsis line does not start with soundprobe: %q", path, line)
		}
		if len(fields) == 1 {
			documented[""] = documentedCommand{flags: map[string]bool{}, line: line}
			continue
		}
		name := fields[1]
		rest := fields[2:]

		// `soundprobe consent status|accept|revoke` documents subcommands, but
		// `soundprobe campus [--ipv4|--ipv6]` documents alternative flags. The
		// brackets must come off before the "--" test: a bracketed alternation
		// starts with "[", so testing the raw token reads it as a subcommand
		// list and invents commands named "[--ipv4" and "--ipv6]".
		if len(rest) > 0 {
			if first := strings.Trim(rest[0], "[]"); strings.Contains(first, "|") && !strings.HasPrefix(first, "--") {
				// The parent is documented by this same line: it exists only to
				// group these subcommands and takes no flags of its own, so it
				// needs no synopsis line separate from theirs.
				documented[name] = documentedCommand{flags: map[string]bool{}, line: line}
				for _, sub := range strings.Split(first, "|") {
					documented[name+" "+sub] = documentedCommand{flags: map[string]bool{}, line: line}
				}
				continue
			}
		}
		entry := documentedCommand{flags: map[string]bool{}, line: line}
		for _, token := range rest {
			token = strings.Trim(token, "[]")
			if !strings.HasPrefix(token, "--") {
				continue
			}
			// `--ipv4|--ipv6` documents two flags on one token.
			for _, part := range strings.Split(token, "|") {
				if strings.HasPrefix(part, "--") {
					entry.flags[strings.TrimPrefix(part, "--")] = true
				}
			}
		}
		documented[name] = entry
	}
	return documented
}

// realCommand describes one node of the cobra tree.
type realCommand struct {
	flags map[string]bool
}

// flagUsageLine matches the "--name" at the start of one pflag usage entry.
// Reading the names out of FlagUsages keeps this test from importing pflag
// directly, which would turn an indirect module requirement into a direct
// one just to run a test.
var flagUsageLine = regexp.MustCompile(`(?m)^\s*(?:-[a-zA-Z], )?--([a-zA-Z0-9][a-zA-Z0-9-]*)`)

func localFlagNames(cmd *cobra.Command) map[string]bool {
	names := map[string]bool{}
	for _, match := range flagUsageLine.FindAllStringSubmatch(cmd.LocalFlags().FlagUsages(), -1) {
		if globalFlags[match[1]] {
			continue
		}
		names[match[1]] = true
	}
	return names
}

// walkCommandTree collects every reachable command, keyed by its path below
// the root ("" for the root itself, "consent status" for a subcommand).
func walkCommandTree(root *cobra.Command) map[string]realCommand {
	found := map[string]realCommand{}
	var walk func(prefix string, cmd *cobra.Command)
	walk = func(prefix string, cmd *cobra.Command) {
		found[prefix] = realCommand{flags: localFlagNames(cmd)}
		for _, child := range cmd.Commands() {
			name := child.Name()
			if name == "help" || name == "completion" {
				continue
			}
			childPath := name
			if prefix != "" {
				childPath = prefix + " " + name
			}
			walk(childPath, child)
		}
	}
	walk("", root)
	return found
}

func TestCLIDocumentationMatchesTheCommandTree(t *testing.T) {
	app := &App{}
	app.setDefaults()
	root := app.newRootCommand(&execution{app: app})

	real := walkCommandTree(root)
	documented := parseCLIDoc(t, filepath.FromSlash(cliDocPath))

	for _, name := range sortedKeys(real) {
		entry, ok := documented[name]
		if !ok {
			t.Errorf("command %q exists but is not in the synopsis block of %s; add a line for it",
				displayName(name), cliDocPath)
			continue
		}
		for _, flag := range sortedKeys(real[name].flags) {
			if !entry.flags[flag] {
				t.Errorf("command %q accepts --%s but %s does not document it on %q",
					displayName(name), flag, cliDocPath, entry.line)
			}
		}
	}

	for _, name := range sortedKeys(documented) {
		entry, ok := real[name]
		if !ok {
			t.Errorf("%s documents command %q, which no longer exists (line %q)",
				cliDocPath, displayName(name), documented[name].line)
			continue
		}
		for _, flag := range sortedKeys(documented[name].flags) {
			if !entry.flags[flag] && !globalFlags[flag] {
				t.Errorf("%s documents --%s on %q, but that command has no such flag (line %q)",
					cliDocPath, flag, displayName(name), documented[name].line)
			}
		}
	}
}

// The parser is the part of this test most likely to quietly stop working:
// if it ever matched nothing, the drift check above would pass vacuously.
func TestCLIDocParserFindsTheWholeSynopsisBlock(t *testing.T) {
	documented := parseCLIDoc(t, filepath.FromSlash(cliDocPath))
	for _, want := range []string{"", "run", "campus", "stations", "export", "consent status", "consent revoke", "version"} {
		if _, ok := documented[want]; !ok {
			t.Fatalf("the parser did not find %q in %s; it is no longer reading the document", displayName(want), cliDocPath)
		}
	}
	if !documented["run"].flags["targets"] || !documented["campus"].flags["ipv6"] {
		t.Fatalf("the parser did not extract flags: run = %#v, campus = %#v", documented["run"].flags, documented["campus"].flags)
	}

	// The cobra side reads flag names out of FlagUsages; if that ever stops
	// matching, the drift check would pass with an empty flag set.
	app := &App{}
	app.setDefaults()
	real := walkCommandTree(app.newRootCommand(&execution{app: app}))
	if !real["run"].flags["targets"] || !real["run"].flags["no-save"] || !real["campus"].flags["ipv6"] {
		t.Fatalf("flag names were not read off the cobra tree: run = %#v, campus = %#v", real["run"].flags, real["campus"].flags)
	}
	if len(real["consent status"].flags) != 0 {
		t.Fatalf("consent status reported unexpected local flags: %#v", real["consent status"].flags)
	}
}

func displayName(name string) string {
	if name == "" {
		return "soundprobe"
	}
	return "soundprobe " + name
}

func sortedKeys[V any](in map[string]V) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

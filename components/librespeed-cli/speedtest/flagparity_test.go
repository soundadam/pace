package speedtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestHarnessFlagsMatchMain guards the fidelity of the test harness: SpeedTest
// reads flags by name, so a flag that main.go registers but the harness does
// not would read back as a zero value under test and hide a behaviour change.
// It parses main.go rather than importing it, because the flag set is built
// inside func main.
func TestHarnessFlagsMatchMain(t *testing.T) {
	constants := parseOptionConstants(t, "../defs/options.go")
	declared := parseFlagNames(t, "../main.go", constants)
	registered := map[string]bool{}
	for _, flag := range testFlags() {
		names := flag.Names()
		if len(names) == 0 {
			continue
		}
		registered[names[0]] = true
	}

	// cli.HelpFlag is registered by value in both places and carries no
	// Name: literal for the parser to find.
	delete(registered, "help")

	var missing, extra []string
	for name := range declared {
		if !registered[name] {
			missing = append(missing, name)
		}
	}
	for name := range registered {
		if !declared[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("main.go registers flags the test harness does not: %v (add them to testFlags)", missing)
	}
	if len(extra) > 0 {
		t.Errorf("the test harness registers flags main.go does not: %v", extra)
	}
}

// parseOptionConstants maps the Option* identifiers to their string values.
func parseOptionConstants(t *testing.T, path string) map[string]string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	constants := map[string]string{}
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || len(spec.Values) != 1 {
			return true
		}
		literal, ok := spec.Values[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}
		constants[spec.Names[0].Name] = value
		return true
	})

	if len(constants) == 0 {
		t.Fatalf("no option constants found in %s", path)
	}
	return constants
}

// parseFlagNames collects every `Name:` value from the flag literals in main.go.
func parseFlagNames(t *testing.T, path string, constants map[string]string) map[string]bool {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	names := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		composite, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		// Only cli.<Kind>Flag literals; skip the cli.App literal, whose
		// Name is the program name rather than a flag.
		selector, ok := composite.Type.(*ast.SelectorExpr)
		if !ok || !strings.HasSuffix(selector.Sel.Name, "Flag") {
			return true
		}
		for _, element := range composite.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := pair.Key.(*ast.Ident)
			if !ok || key.Name != "Name" {
				continue
			}
			switch value := pair.Value.(type) {
			case *ast.SelectorExpr:
				if resolved, ok := constants[value.Sel.Name]; ok {
					names[resolved] = true
				} else {
					t.Errorf("main.go references an unknown option constant %q", value.Sel.Name)
				}
			case *ast.BasicLit:
				if literal, err := strconv.Unquote(value.Value); err == nil {
					names[literal] = true
				}
			}
		}
		return true
	})

	if len(names) == 0 {
		t.Fatalf("no flags found in %s", path)
	}
	return names
}

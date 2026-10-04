package helper

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var ErrNotFound = errors.New("helper executable not found")

type Source string

const (
	SourceLibexec    Source = "libexec"
	SourceRepository Source = "repository"
	SourcePATH       Source = "path"
)

type Resolved struct {
	Path   string
	Source Source
}

type Resolver struct {
	ExecutablePath   string
	WorkingDirectory string
	LookupPath       func(string) (string, error)
}

func NewResolver() Resolver {
	return Resolver{}
}

func (resolver Resolver) Resolve(name string) (Resolved, error) {
	if name == "" || filepath.Base(name) != name {
		return Resolved{}, errors.New("helper name must be a base filename")
	}

	executablePath, err := resolver.executablePath()
	if err != nil {
		return Resolved{}, err
	}
	libexecPath := filepath.Clean(filepath.Join(filepath.Dir(executablePath), "..", "libexec", "pace", name))
	if resolved, exists, err := resolveCandidate(libexecPath, SourceLibexec); err != nil {
		return Resolved{}, err
	} else if exists {
		return resolved, nil
	}

	workingDirectory, err := resolver.workingDirectory()
	if err != nil {
		return Resolved{}, err
	}
	roots := repositoryRoots(workingDirectory, filepath.Dir(executablePath))
	for _, root := range roots {
		candidate := filepath.Join(root, ".tools", "bin", name)
		if resolved, exists, err := resolveCandidate(candidate, SourceRepository); err != nil {
			return Resolved{}, err
		} else if exists {
			return resolved, nil
		}
	}

	lookup := resolver.LookupPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	path, err := lookup(name)
	if err != nil {
		return Resolved{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	resolved, exists, err := resolveCandidate(path, SourcePATH)
	if err != nil {
		return Resolved{}, err
	}
	if !exists {
		return Resolved{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return resolved, nil
}

func (resolver Resolver) executablePath() (string, error) {
	path := resolver.ExecutablePath
	if path == "" {
		var err error
		path, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve pace executable: %w", err)
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve pace executable path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve pace executable symlinks: %w", err)
	}
	return resolved, nil
}

func (resolver Resolver) workingDirectory() (string, error) {
	path := resolver.WorkingDirectory
	if path == "" {
		var err error
		path, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve working directory path: %w", err)
	}
	return absolute, nil
}

func resolveCandidate(path string, source Source) (Resolved, bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Resolved{}, false, nil
	}
	if err != nil {
		return Resolved{}, false, fmt.Errorf("inspect %s helper %q: %w", source, path, err)
	}
	if !info.Mode().IsRegular() {
		return Resolved{}, false, fmt.Errorf("%s helper %q is not a regular file", source, path)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return Resolved{}, false, fmt.Errorf("%s helper %q is not executable", source, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Resolved{}, false, fmt.Errorf("resolve %s helper path: %w", source, err)
	}
	return Resolved{Path: absolute, Source: source}, true, nil
}

func repositoryRoots(paths ...string) []string {
	seen := map[string]struct{}{}
	var roots []string
	for _, path := range paths {
		for directory := path; ; directory = filepath.Dir(directory) {
			if isPaceModule(directory) {
				if _, exists := seen[directory]; !exists {
					seen[directory] = struct{}{}
					roots = append(roots, directory)
				}
				break
			}
			parent := filepath.Dir(directory)
			if parent == directory {
				break
			}
		}
	}
	return roots
}

func isPaceModule(directory string) bool {
	data, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "module github.com/soundadam/pace")
}

// ReadVersionManifest reads the required sidecar version for helpers that do
// not expose a machine-readable version command. The manifest path is the
// executable path with ".version" appended.
func ReadVersionManifest(executablePath string) (string, error) {
	data, err := os.ReadFile(executablePath + ".version")
	if err != nil {
		return "", fmt.Errorf("read helper version manifest: %w", err)
	}
	version := strings.TrimSpace(string(data))
	if version == "" || strings.ContainsAny(version, "\r\n\t ") {
		return "", errors.New("helper version manifest is invalid")
	}
	return version, nil
}

package helper

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolverPrecedence(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "prefix", "bin", "pace")
	libexec := filepath.Join(root, "prefix", "libexec", "pace", "librespeed-cli")
	repository := filepath.Join(root, "repo")
	repositoryHelper := filepath.Join(repository, ".tools", "bin", "librespeed-cli")
	pathHelper := filepath.Join(root, "path", "librespeed-cli")

	writeExecutable(t, executable)
	writeExecutable(t, libexec)
	writeExecutable(t, repositoryHelper)
	writeExecutable(t, pathHelper)
	if err := os.WriteFile(filepath.Join(repository, "go.mod"), []byte("module github.com/soundadam/pace\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	resolver := Resolver{
		ExecutablePath:   executable,
		WorkingDirectory: repository,
		LookupPath: func(string) (string, error) {
			return pathHelper, nil
		},
	}

	resolved, err := resolver.Resolve("librespeed-cli")
	if err != nil {
		t.Fatal(err)
	}
	canonicalLibexec, err := filepath.EvalSymlinks(libexec)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != canonicalLibexec || resolved.Source != SourceLibexec {
		t.Fatalf("resolved = %#v, want libexec %q", resolved, canonicalLibexec)
	}

	if err := os.Remove(libexec); err != nil {
		t.Fatal(err)
	}
	resolved, err = resolver.Resolve("librespeed-cli")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != repositoryHelper || resolved.Source != SourceRepository {
		t.Fatalf("resolved = %#v, want repository %q", resolved, repositoryHelper)
	}

	if err := os.Remove(repositoryHelper); err != nil {
		t.Fatal(err)
	}
	resolved, err = resolver.Resolve("librespeed-cli")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != pathHelper || resolved.Source != SourcePATH {
		t.Fatalf("resolved = %#v, want PATH %q", resolved, pathHelper)
	}
}

func TestResolverFollowsExecutableSymlinkToLibexec(t *testing.T) {
	root := t.TempDir()
	cellarExecutable := filepath.Join(root, "Cellar", "pace", "0.1.0", "bin", "pace")
	linkedExecutable := filepath.Join(root, "bin", "pace")
	libexec := filepath.Join(root, "Cellar", "pace", "0.1.0", "libexec", "pace", "librespeed-cli")
	writeExecutable(t, cellarExecutable)
	writeExecutable(t, libexec)
	if err := os.MkdirAll(filepath.Dir(linkedExecutable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cellarExecutable, linkedExecutable); err != nil {
		t.Fatal(err)
	}

	resolver := Resolver{
		ExecutablePath:   linkedExecutable,
		WorkingDirectory: root,
		LookupPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
	}
	resolved, err := resolver.Resolve("librespeed-cli")
	if err != nil {
		t.Fatal(err)
	}
	canonicalLibexec, err := filepath.EvalSymlinks(libexec)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != canonicalLibexec || resolved.Source != SourceLibexec {
		t.Fatalf("resolved = %#v, want libexec %q", resolved, canonicalLibexec)
	}
}

func TestResolverRejectsBrokenHigherPrecedenceHelper(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "prefix", "bin", "pace")
	libexec := filepath.Join(root, "prefix", "libexec", "pace", "librespeed-cli")
	pathHelper := filepath.Join(root, "path", "librespeed-cli")
	writeExecutable(t, executable)
	writeExecutable(t, pathHelper)
	if err := os.MkdirAll(filepath.Dir(libexec), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(libexec, []byte("not executable"), 0o600); err != nil {
		t.Fatal(err)
	}

	resolver := Resolver{
		ExecutablePath:   executable,
		WorkingDirectory: root,
		LookupPath: func(string) (string, error) {
			return pathHelper, nil
		},
	}
	_, err := resolver.Resolve("librespeed-cli")
	if err == nil {
		t.Fatal("Resolve() succeeded with a broken libexec helper")
	}
}

func TestResolverReturnsNotFound(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "bin", "pace")
	writeExecutable(t, executable)
	resolver := Resolver{
		ExecutablePath:   executable,
		WorkingDirectory: root,
		LookupPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
	}
	_, err := resolver.Resolve("librespeed-cli")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestReadVersionManifest(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "ndt7-client")
	writeExecutable(t, executable)
	if err := os.WriteFile(executable+".version", []byte("v0.10.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	version, err := ReadVersionManifest(executable)
	if err != nil {
		t.Fatal(err)
	}
	if version != "v0.10.1" {
		t.Fatalf("version = %q", version)
	}
	if err := os.WriteFile(executable+".version", []byte("invalid version\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadVersionManifest(executable); err == nil {
		t.Fatal("ReadVersionManifest() accepted whitespace")
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

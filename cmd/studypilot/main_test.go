package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Arameair/studypilot/internal/workspace"
)

func TestHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no arguments"},
		{name: "help", args: []string{"help"}},
		{name: "long help", args: []string{"--help"}},
		{name: "short help", args: []string{"-h"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := runForTest(test.args)
			if code != 0 {
				t.Errorf("run() code = %d, want 0", code)
			}
			if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "studypilot init --dry-run") {
				t.Errorf("stdout does not contain usage: %q", stdout)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	code, stdout, stderr := runForTest([]string{"version"})
	if code != 0 {
		t.Errorf("run() code = %d, want 0", code)
	}
	if stdout != "StudyPilot dev\n" {
		t.Errorf("stdout = %q, want %q", stdout, "StudyPilot dev\n")
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestDefaultDryRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "Documents", "StudyPilot")

	code, stdout, stderr := runForTest([]string{"init", "--dry-run"})
	if code != 0 {
		t.Fatalf("run() code = %d, want 0; stderr = %q", code, stderr)
	}
	for _, required := range []string{
		root,
		filepath.Join(root, "Learning-Vault-Private"),
		filepath.Join(root, "IT-Knowledge-Portfolio"),
		"CREATE DIRECTORY",
		"CREATE FILE",
		"Dry run complete:",
		"operations planned.",
		"No files or directories were created.",
	} {
		if !strings.Contains(stdout, required) {
			t.Errorf("stdout does not contain %q", required)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	assertPathDoesNotExist(t, root)
}

func TestCustomRootDryRunForms(t *testing.T) {
	tests := []struct {
		name string
		args func(string) []string
	}{
		{name: "dry run then root", args: func(root string) []string { return []string{"init", "--dry-run", "--root", root} }},
		{name: "root then dry run", args: func(root string) []string { return []string{"init", "--root", root, "--dry-run"} }},
		{name: "equals syntax", args: func(root string) []string { return []string{"init", "--dry-run", "--root=" + root} }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "not-created")
			code, stdout, stderr := runForTest(test.args(root))
			if code != 0 {
				t.Fatalf("run() code = %d, want 0; stderr = %q", code, stderr)
			}
			if !strings.Contains(stdout, root) {
				t.Errorf("stdout does not contain custom root %q", root)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			assertPathDoesNotExist(t, root)
		})
	}
}

func TestRealInitialization(t *testing.T) {
	root := filepath.Join(t.TempDir(), "StudyPilot")
	code, stdout, stderr := runForTest([]string{"init", "--root", root})
	if code != 0 {
		t.Fatalf("run() code = %d, want 0; stderr = %q", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	for _, required := range []string{"CREATED", "Initialization complete:", "Created: 26", "Skipped: 0", "Conflicts: 0"} {
		if !strings.Contains(stdout, required) {
			t.Errorf("stdout does not contain %q", required)
		}
	}
	assertContractTree(t, filepath.Join(root, "Learning-Vault-Private"), workspace.PrivateVaultContract())
	assertContractTree(t, filepath.Join(root, "IT-Knowledge-Portfolio"), workspace.PublicPortfolioContract())
	assertNoGitRepositories(t, root)
}

func TestRepeatedInitialization(t *testing.T) {
	root := filepath.Join(t.TempDir(), "StudyPilot")
	if code, _, stderr := runForTest([]string{"init", "--root", root}); code != 0 {
		t.Fatalf("first run() code = %d; stderr = %q", code, stderr)
	}
	before := readContractFiles(t, root)

	code, stdout, stderr := runForTest([]string{"init", "--root=" + root})
	if code != 0 {
		t.Fatalf("second run() code = %d, want 0; stderr = %q", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	for _, required := range []string{"Created: 0", "Skipped: 26", "Conflicts: 0"} {
		if !strings.Contains(stdout, required) {
			t.Errorf("stdout does not contain %q", required)
		}
	}
	if after := readContractFiles(t, root); !stringMapsEqual(before, after) {
		t.Error("repeated initialization changed file contents")
	}
}

func TestInitializationConflictingFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "StudyPilot")
	path := filepath.Join(root, "Learning-Vault-Private", "README.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("create conflict parent: %v", err)
	}
	original := "original user content"
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatalf("create conflict: %v", err)
	}

	code, stdout, stderr := runForTest([]string{"init", "--root", root})
	if code != 1 {
		t.Errorf("run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "CONFLICT") || !strings.Contains(stderr, path) {
		t.Errorf("stderr does not identify conflict: %q", stderr)
	}
	if !strings.Contains(stdout, "Conflicts: 1") {
		t.Errorf("stdout does not contain conflict summary: %q", stdout)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read conflict: %v", err)
	}
	if string(contents) != original {
		t.Errorf("conflicting content was overwritten: %q", contents)
	}
	if _, err := os.Stat(filepath.Join(root, "IT-Knowledge-Portfolio", "README.md")); err != nil {
		t.Errorf("unrelated safe portfolio content was not created: %v", err)
	}
}

func TestInitializationTypeConflicts(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string) string
	}{
		{
			name: "file where directory required",
			prepare: func(t *testing.T, root string) string {
				t.Helper()
				if err := os.Mkdir(root, 0o750); err != nil {
					t.Fatalf("create root: %v", err)
				}
				path := filepath.Join(root, "Learning-Vault-Private")
				if err := os.WriteFile(path, []byte("preserve"), 0o640); err != nil {
					t.Fatalf("create conflicting file: %v", err)
				}
				return path
			},
		},
		{
			name: "directory where file required",
			prepare: func(t *testing.T, root string) string {
				t.Helper()
				path := filepath.Join(root, "Learning-Vault-Private", "README.md")
				if err := os.MkdirAll(path, 0o750); err != nil {
					t.Fatalf("create conflicting directory: %v", err)
				}
				return path
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "StudyPilot")
			path := test.prepare(t, root)
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatalf("stat conflict before init: %v", err)
			}
			var contentBefore []byte
			if !before.IsDir() {
				contentBefore, err = os.ReadFile(path)
				if err != nil {
					t.Fatalf("read conflicting file before init: %v", err)
				}
			}
			code, _, stderr := runForTest([]string{"init", "--root", root})
			if code != 1 {
				t.Errorf("run() code = %d, want 1", code)
			}
			if !strings.Contains(stderr, "CONFLICT") || !strings.Contains(stderr, path) {
				t.Errorf("stderr does not identify conflict: %q", stderr)
			}
			after, err := os.Lstat(path)
			if err != nil {
				t.Fatalf("stat conflict after init: %v", err)
			}
			if before.IsDir() != after.IsDir() {
				t.Error("conflicting path type changed")
			}
			if !before.IsDir() {
				contentAfter, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read conflicting file after init: %v", err)
				}
				if !bytes.Equal(contentBefore, contentAfter) {
					t.Error("conflicting file content changed")
				}
			}
		})
	}
}

func TestInitializationRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	root := filepath.Join(t.TempDir(), "StudyPilot")
	outside := t.TempDir()
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatalf("create root: %v", err)
	}
	path := filepath.Join(root, "Learning-Vault-Private")
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlink creation unsupported: %v", err)
	}

	code, _, stderr := runForTest([]string{"init", "--root", root})
	if code != 1 {
		t.Errorf("run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "CONFLICT") || !strings.Contains(stderr, "unsafe symlink") {
		t.Errorf("stderr does not identify symlink conflict: %q", stderr)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("read outside path: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("initialization wrote through symlink: %v", entries)
	}
}

func TestDefaultRealInitializationUsesControlledHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "Documents", "StudyPilot")
	code, _, stderr := runForTest([]string{"init"})
	if code != 0 {
		t.Fatalf("run() code = %d, want 0; stderr = %q", code, stderr)
	}
	assertContractTree(t, filepath.Join(root, "Learning-Vault-Private"), workspace.PrivateVaultContract())
	assertContractTree(t, filepath.Join(root, "IT-Knowledge-Portfolio"), workspace.PublicPortfolioContract())
}

func TestInvalidUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "unknown command", args: []string{"unknown"}},
		{name: "unknown init flag", args: []string{"init", "--dry-run", "--unknown"}},
		{name: "missing root value", args: []string{"init", "--dry-run", "--root"}},
		{name: "duplicate root", args: []string{"init", "--dry-run", "--root", "/tmp/one", "--root=/tmp/two"}},
		{name: "duplicate dry run", args: []string{"init", "--dry-run", "--dry-run"}},
		{name: "unexpected positional argument", args: []string{"init", "--dry-run", "unexpected"}},
		{name: "version argument", args: []string{"version", "unexpected"}},
		{name: "help argument", args: []string{"help", "unexpected"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, _, stderr := runForTest(test.args)
			if code != 2 {
				t.Errorf("run() code = %d, want 2", code)
			}
			if strings.TrimSpace(stderr) == "" {
				t.Error("stderr is empty, want usage error")
			}
		})
	}
}

func TestInvalidRootIsRuntimeFailure(t *testing.T) {
	code, _, stderr := runForTest([]string{"init", "--dry-run", "--root", "~someone/StudyPilot"})
	if code != 1 {
		t.Errorf("run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "resolve workspace paths") {
		t.Errorf("stderr = %q", stderr)
	}
}

func runForTest(args []string) (int, string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func assertPathDoesNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("path %q exists after dry run or returned unexpected error: %v", path, err)
	}
}

func assertContractTree(t *testing.T, root string, contract workspace.VaultContract) {
	t.Helper()
	for _, directory := range contract.Directories {
		info, err := os.Stat(filepath.Join(root, directory))
		if err != nil {
			t.Errorf("required directory %q: %v", directory, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("required path %q is not a directory", directory)
		}
	}
	for _, file := range contract.Files {
		contents, err := os.ReadFile(filepath.Join(root, file.Path))
		if err != nil {
			t.Errorf("required file %q: %v", file.Path, err)
			continue
		}
		if string(contents) != file.Content {
			t.Errorf("required file %q content differs", file.Path)
		}
	}
}

func assertNoGitRepositories(t *testing.T, root string) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(root, ".git"),
		filepath.Join(root, "Learning-Vault-Private", ".git"),
		filepath.Join(root, "IT-Knowledge-Portfolio", ".git"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("Git repository path %q exists or returned unexpected error: %v", path, err)
		}
	}
}

func readContractFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	contents := make(map[string]string)
	for _, vault := range []struct {
		root     string
		contract workspace.VaultContract
	}{
		{root: filepath.Join(root, "Learning-Vault-Private"), contract: workspace.PrivateVaultContract()},
		{root: filepath.Join(root, "IT-Knowledge-Portfolio"), contract: workspace.PublicPortfolioContract()},
	} {
		for _, file := range vault.contract.Files {
			path := filepath.Join(vault.root, file.Path)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %q: %v", path, err)
			}
			contents[path] = string(data)
		}
	}
	return contents
}

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

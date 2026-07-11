package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestInitWithoutDryRunIsRefused(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	code, stdout, stderr := runForTest([]string{"init", "--root", root})
	if code != 2 {
		t.Errorf("run() code = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "Initialization execution is not enabled yet") ||
		!strings.Contains(stderr, "Use --dry-run to inspect the plan") {
		t.Errorf("stderr does not contain refusal: %q", stderr)
	}
	assertPathDoesNotExist(t, root)
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

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
			if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "studypilot init [--dry-run]") ||
				!strings.Contains(stdout, "studypilot course create") || !strings.Contains(stdout, "studypilot module create") {
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
		"Learning-Vault-Private",
		"IT-Knowledge-Portfolio",
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
	assertOutputHidesPrivatePaths(t, stdout, stderr, root, home)
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
			assertOutputHidesPrivatePaths(t, stdout, stderr, root, filepath.Dir(root))
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
	assertOutputHidesPrivatePaths(t, stdout, stderr, root, filepath.Dir(root))
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
	if !strings.Contains(stderr, "CONFLICT") || strings.Contains(stderr, path) {
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
			if !strings.Contains(stderr, "CONFLICT") || strings.Contains(stderr, path) {
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
	if !strings.Contains(stderr, "CONFLICT") || !strings.Contains(stderr, "unsafe managed filesystem state") {
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
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	root := filepath.Join(home, "Documents", "StudyPilot")
	code, _, stderr := runForTest([]string{"init"})
	if code != 0 {
		t.Fatalf("run() code = %d, want 0; stderr = %q", code, stderr)
	}
	assertContractTree(t, filepath.Join(root, "Learning-Vault-Private"), workspace.PrivateVaultContract())
	assertContractTree(t, filepath.Join(root, "IT-Knowledge-Portfolio"), workspace.PublicPortfolioContract())
}

func TestCourseCreateAndDryRun(t *testing.T) {
	setFixedClock(t)
	tests := []struct {
		name   string
		dryRun bool
	}{
		{name: "create"},
		{name: "dry run", dryRun: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "StudyPilot")
			initializeForCLI(t, root)
			args := []string{"course", "create", "--name=TCM Practical Help Desk", "--root", root}
			if test.dryRun {
				args = append(args, "--dry-run")
			}
			code, stdout, stderr := runForTest(args)
			if code != 0 {
				t.Fatalf("run() code = %d, want 0; stderr = %q", code, stderr)
			}
			courseRoot := filepath.Join(root, "Learning-Vault-Private", "01 Courses", "TCM Practical Help Desk")
			assertOutputHidesPrivatePaths(t, stdout, stderr, root, filepath.Dir(root))
			if test.dryRun {
				if !strings.Contains(stdout, "CREATE DIRECTORY") || !strings.Contains(stdout, "Dry run complete: 8 operations planned") {
					t.Errorf("dry-run stdout = %q", stdout)
				}
				assertPathDoesNotExist(t, courseRoot)
				return
			}
			if stderr != "" || !strings.Contains(stdout, "Course creation complete:") || !strings.Contains(stdout, "Created: 8") {
				t.Errorf("stdout/stderr = %q / %q", stdout, stderr)
			}
			overview, err := os.ReadFile(filepath.Join(courseRoot, "Course Overview.md"))
			if err != nil {
				t.Fatalf("read course overview: %v", err)
			}
			if !strings.Contains(string(overview), "created: 2026-07-11") {
				t.Errorf("course overview date is not controlled: %q", overview)
			}
		})
	}
}

func TestModuleCreateAndDryRun(t *testing.T) {
	setFixedClock(t)
	tests := []struct {
		name   string
		dryRun bool
	}{
		{name: "create"},
		{name: "dry run", dryRun: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "StudyPilot")
			initializeForCLI(t, root)
			createCourseForCLI(t, root)
			args := []string{
				"module", "create", "--course", "TCM Practical Help Desk",
				"--number=3", "--name", "Windows Services", "--root=" + root,
			}
			if test.dryRun {
				args = append(args, "--dry-run")
			}
			code, stdout, stderr := runForTest(args)
			if code != 0 {
				t.Fatalf("run() code = %d, want 0; stderr = %q", code, stderr)
			}
			moduleRoot := filepath.Join(root, "Learning-Vault-Private", "01 Courses", "TCM Practical Help Desk", "Modules", "03 - Windows Services")
			assertOutputHidesPrivatePaths(t, stdout, stderr, root, filepath.Dir(root))
			if test.dryRun {
				if !strings.Contains(stdout, "Dry run complete: 11 operations planned") {
					t.Errorf("dry-run stdout = %q", stdout)
				}
				assertPathDoesNotExist(t, moduleRoot)
				return
			}
			if stderr != "" || !strings.Contains(stdout, "Module creation complete:") || !strings.Contains(stdout, "Created: 11") {
				t.Errorf("stdout/stderr = %q / %q", stdout, stderr)
			}
			for _, relative := range []string{"Sessions", "Notes", "Transcripts", filepath.Join("Assets", "Screenshots"), filepath.Join("Assets", "Audio"), filepath.Join("Assets", "Video"), filepath.Join("Assets", "Documents")} {
				if info, err := os.Stat(filepath.Join(moduleRoot, relative)); err != nil || !info.IsDir() {
					t.Errorf("module directory %q missing: %v", relative, err)
				}
			}
		})
	}
}

func TestModuleCreateRequiresCourse(t *testing.T) {
	root := filepath.Join(t.TempDir(), "StudyPilot")
	initializeForCLI(t, root)
	code, _, stderr := runForTest([]string{"module", "create", "--course", "Missing", "--number", "1", "--name", "Intro", "--root", root})
	if code != 1 {
		t.Errorf("run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "construct module plan") || !strings.Contains(stderr, "required managed resource is unavailable") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestCourseConflictExitCode(t *testing.T) {
	setFixedClock(t)
	root := filepath.Join(t.TempDir(), "StudyPilot")
	initializeForCLI(t, root)
	createCourseForCLI(t, root)
	overview := filepath.Join(root, "Learning-Vault-Private", "01 Courses", "TCM Practical Help Desk", "Course Overview.md")
	if err := os.WriteFile(overview, []byte("preserve"), 0o640); err != nil {
		t.Fatalf("replace test overview: %v", err)
	}
	code, _, stderr := runForTest([]string{"course", "create", "--name", "TCM Practical Help Desk", "--root", root})
	if code != 1 || !strings.Contains(stderr, "CONFLICT") {
		t.Errorf("code/stderr = %d / %q", code, stderr)
	}
	contents, err := os.ReadFile(overview)
	if err != nil || string(contents) != "preserve" {
		t.Errorf("conflicting overview changed: %q / %v", contents, err)
	}
}

func TestModuleDuplicateNumberExitCode(t *testing.T) {
	root := filepath.Join(t.TempDir(), "StudyPilot")
	initializeForCLI(t, root)
	createCourseForCLI(t, root)
	args := []string{"module", "create", "--course", "TCM Practical Help Desk", "--number", "3", "--name", "First", "--root", root}
	if code, _, stderr := runForTest(args); code != 0 {
		t.Fatalf("first module = %d/%q", code, stderr)
	}
	args = []string{"module", "create", "--course", "TCM Practical Help Desk", "--number", "3", "--name", "Second", "--root", root}
	code, _, stderr := runForTest(args)
	if code != 1 || !strings.Contains(stderr, "managed resource conflicts with existing state") {
		t.Errorf("duplicate module = %d/%q", code, stderr)
	}
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
		{name: "course missing name", args: []string{"course", "create"}},
		{name: "course duplicate name", args: []string{"course", "create", "--name", "One", "--name", "Two"}},
		{name: "course unknown flag", args: []string{"course", "create", "--name", "One", "--unknown"}},
		{name: "module missing course", args: []string{"module", "create", "--number", "1", "--name", "One"}},
		{name: "module missing number", args: []string{"module", "create", "--course", "Course", "--name", "One"}},
		{name: "module invalid number", args: []string{"module", "create", "--course", "Course", "--number", "0", "--name", "One"}},
		{name: "module noninteger number", args: []string{"module", "create", "--course", "Course", "--number", "nope", "--name", "One"}},
		{name: "module duplicate number", args: []string{"module", "create", "--course", "Course", "--number", "1", "--number", "2", "--name", "One"}},
		{name: "module unexpected argument", args: []string{"module", "create", "--course", "Course", "--number", "1", "--name", "One", "extra"}},
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

func assertOutputHidesPrivatePaths(t *testing.T, stdout, stderr string, privatePaths ...string) {
	t.Helper()
	combined := stdout + stderr
	for _, privatePath := range privatePaths {
		if privatePath != "" && strings.Contains(combined, privatePath) {
			t.Errorf("CLI output disclosed private path %q: %q", privatePath, combined)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.Contains(combined, home) {
		t.Errorf("CLI output disclosed user home directory: %q", combined)
	}
	for _, unsafe := range []string{"panic:", "goroutine ", "stack trace", "process_id", "process handle"} {
		if strings.Contains(strings.ToLower(combined), unsafe) {
			t.Errorf("CLI output disclosed %q: %q", unsafe, combined)
		}
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

func setFixedClock(t *testing.T) {
	t.Helper()
	previous := now
	now = func() time.Time { return time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = previous })
}

func initializeForCLI(t *testing.T, root string) {
	t.Helper()
	if code, _, stderr := runForTest([]string{"init", "--root", root}); code != 0 {
		t.Fatalf("initialize workspace code = %d; stderr = %q", code, stderr)
	}
}

func createCourseForCLI(t *testing.T, root string) {
	t.Helper()
	if code, _, stderr := runForTest([]string{"course", "create", "--name", "TCM Practical Help Desk", "--root", root}); code != 0 {
		t.Fatalf("create course code = %d; stderr = %q", code, stderr)
	}
}

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	localconfig "github.com/Arameair/studypilot/internal/config"
)

func TestPersistentSetupCommandDryRunThenInitialize(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config", "config.json")
	store, err := localconfig.NewStore(configPath)
	if err != nil {
		t.Fatal(err)
	}
	original := newLocalConfigStore
	newLocalConfigStore = func() (*localconfig.Store, error) { return store, nil }
	t.Cleanup(func() { newLocalConfigStore = original })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := filepath.Join(t.TempDir(), "Example User", "Documents", "vaults 學習")

	code, stdout, stderr := runForTest([]string{"setup", "--root", root, "--dry-run"})
	if code != 0 || stderr != "" || !strings.Contains(stdout, root) || !strings.Contains(stdout, "No files or configuration were written.") {
		t.Fatalf("dry run code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry run created root: %v", err)
	}
	if _, err := store.Load(); !errors.Is(err, localconfig.ErrMissing) {
		t.Fatalf("dry run wrote config: %v", err)
	}

	code, stdout, stderr = runForTest([]string{"setup", "--root", root})
	if code != 0 || stderr != "" || !strings.Contains(stdout, "persistent default") {
		t.Fatalf("setup code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	value, err := store.Load()
	if err != nil || value.WorkspaceRoot != root {
		t.Fatalf("config = %+v, %v", value, err)
	}
}

func TestPersistentSetupCommandFailureDoesNotReplaceConfig(t *testing.T) {
	store, err := localconfig.NewStore(filepath.Join(t.TempDir(), "config", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	original := newLocalConfigStore
	newLocalConfigStore = func() (*localconfig.Store, error) { return store, nil }
	t.Cleanup(func() { newLocalConfigStore = original })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	valid := filepath.Join(t.TempDir(), "valid")
	if code, _, stderr := runForTest([]string{"setup", "--root", valid}); code != 0 {
		t.Fatalf("initial setup: %s", stderr)
	}
	before, _ := store.Load()
	conflicting := filepath.Join(t.TempDir(), "conflicting")
	if err := os.Mkdir(conflicting, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conflicting, "existing.txt"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runForTest([]string{"setup", "--root", conflicting}); code == 0 {
		t.Fatal("conflicting setup succeeded")
	}
	after, _ := store.Load()
	if after != before {
		t.Fatalf("configuration changed after failure: before=%+v after=%+v", before, after)
	}
}

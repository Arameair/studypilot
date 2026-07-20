//go:build windows

package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultStoreUsesWindowsUserConfigDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AppData", root)
	t.Setenv("APPDATA", root)
	store, err := DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	if store.Path() != filepath.Join(root, "StudyPilot", "config.json") {
		t.Fatalf("Path() = %q", store.Path())
	}
}

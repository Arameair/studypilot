//go:build linux

package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultStoreUsesLinuxUserConfigDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	store, err := DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	if store.Path() != filepath.Join(root, "StudyPilot", "config.json") {
		t.Fatalf("Path() = %q", store.Path())
	}
}

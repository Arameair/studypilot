package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingConfiguration(t *testing.T) {
	store := testStore(t, filepath.Join(t.TempDir(), "missing", "config.json"))
	if _, err := store.Load(); !errors.Is(err, ErrMissing) {
		t.Fatalf("Load() error = %v, want ErrMissing", err)
	}
	if _, err := os.Stat(filepath.Dir(store.Path())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load created configuration directory: %v", err)
	}
}

func TestSaveAndLoadConfiguration(t *testing.T) {
	for _, name := range []string{"path with spaces", "Unicode-學習"} {
		t.Run(name, func(t *testing.T) {
			store := testStore(t, filepath.Join(t.TempDir(), "config dir", "config.json"))
			root := filepath.Join(t.TempDir(), name)
			want := Config{SchemaVersion: SchemaVersion, WorkspaceRoot: root}
			if err := store.Save(want); err != nil {
				t.Fatalf("Save() error = %v", err)
			}
			got, err := store.Load()
			if err != nil || got != want {
				t.Fatalf("Load() = %#v, %v; want %#v", got, err, want)
			}
		})
	}
}

func TestLoadRejectsInvalidDocuments(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	tests := []struct {
		name string
		data string
		err  error
	}{
		{name: "empty", data: "", err: ErrInvalid},
		{name: "malformed", data: "{", err: ErrInvalid},
		{name: "unknown field", data: `{"schema_version":1,"workspace_root":"` + escaped(root) + `","extra":true}`, err: ErrInvalid},
		{name: "unknown schema", data: `{"schema_version":2,"workspace_root":"` + escaped(root) + `"}`, err: ErrUnsupported},
		{name: "relative root", data: `{"schema_version":1,"workspace_root":"relative"}`, err: ErrInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			store := testStore(t, path)
			if _, err := store.Load(); !errors.Is(err, test.err) {
				t.Fatalf("Load() error = %v, want %v", err, test.err)
			}
		})
	}
}

func TestLoadRejectsMaximumSizeExceeded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", MaxFileSize+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore(t, path).Load(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Load() error = %v, want ErrInvalid", err)
	}
}

func TestFailedSavePreservesPreviousConfiguration(t *testing.T) {
	store := testStore(t, filepath.Join(t.TempDir(), "config", "config.json"))
	previous := Config{SchemaVersion: SchemaVersion, WorkspaceRoot: filepath.Join(t.TempDir(), "previous")}
	if err := store.Save(previous); err != nil {
		t.Fatal(err)
	}
	store.replace = func(string, string) error { return errors.New("injected replacement failure") }
	next := Config{SchemaVersion: SchemaVersion, WorkspaceRoot: filepath.Join(t.TempDir(), "next")}
	if err := store.Save(next); err == nil {
		t.Fatal("Save() error = nil, want injected failure")
	}
	got, err := store.Load()
	if err != nil || got != previous {
		t.Fatalf("previous config = %#v, %v", got, err)
	}
}

func TestConfigurationDirectorySymlinkIsRejected(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "config-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	store := testStore(t, filepath.Join(link, "config.json"))
	if err := store.Save(Config{SchemaVersion: SchemaVersion, WorkspaceRoot: filepath.Join(t.TempDir(), "root")}); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("Save() error = %v, want ErrUnsafe", err)
	}
}

func testStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func escaped(value string) string {
	return strings.ReplaceAll(value, `\`, `\\`)
}

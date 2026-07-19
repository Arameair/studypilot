//go:build windows

package platformfs

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLinkTestFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("link inspection"), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWindowsHardLinkCountTransitions(t *testing.T) {
	path := writeLinkTestFile(t, "normal file.txt")
	multiple, err := HasMultipleHardLinks(path)
	if err != nil || multiple {
		t.Fatalf("normal file: multiple=%v err=%v", multiple, err)
	}
	alias := filepath.Join(filepath.Dir(path), "second name.txt")
	if err = os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	multiple, err = HasMultipleHardLinks(path)
	if err != nil || !multiple {
		t.Fatalf("hardlinked file: multiple=%v err=%v", multiple, err)
	}
	if err = os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	multiple, err = HasMultipleHardLinks(path)
	if err != nil || multiple {
		t.Fatalf("restored single link: multiple=%v err=%v", multiple, err)
	}
}

func TestWindowsHardLinkInspectionSupportsUnicodeAndSpaces(t *testing.T) {
	for _, name := range []string{"path with spaces.txt", "Unicode café 日本語 🚀.txt"} {
		t.Run(name, func(t *testing.T) {
			path := writeLinkTestFile(t, name)
			multiple, err := HasMultipleHardLinks(path)
			if err != nil || multiple {
				t.Fatalf("multiple=%v err=%v", multiple, err)
			}
		})
	}
}

func TestWindowsHardLinkInspectionFailsClosed(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.txt")
	if multiple, err := HasMultipleHardLinks(missing); err == nil || multiple {
		t.Fatalf("missing path: multiple=%v err=%v", multiple, err)
	}
	if multiple, err := HasMultipleHardLinks(t.TempDir()); err == nil || multiple {
		t.Fatalf("directory: multiple=%v err=%v", multiple, err)
	}
}

func TestWindowsHardLinkInspectionRejectsSymlink(t *testing.T) {
	target := writeLinkTestFile(t, "target.txt")
	link := filepath.Join(filepath.Dir(target), "target-link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if multiple, err := HasMultipleHardLinks(link); err == nil || multiple {
		t.Fatalf("symlink: multiple=%v err=%v", multiple, err)
	}
}

func TestWindowsHardLinkInspectionClosesHandle(t *testing.T) {
	path := writeLinkTestFile(t, "handle cleanup.txt")
	if _, err := HasMultipleHardLinks(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("inspected file remained locked: %v", err)
	}
}

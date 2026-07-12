package backend

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Arameair/studypilot/internal/workspace"
)

func TestSegmentAuthorityAcceptsValidSession(t *testing.T) {
	paths, sessionRoot := newSession(t)
	authority, err := NewSegmentAuthority(paths, sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if authority.SegmentsDir() != filepath.Join(sessionRoot, "Segments") {
		t.Fatalf("segments dir = %s", authority.SegmentsDir())
	}
	target, err := authority.Resolve("001-audio.wav")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(target) != authority.SegmentsDir() {
		t.Fatalf("resolved outside Segments: %s", target)
	}
}

func TestSegmentAuthorityRejectsUnsafeSessions(t *testing.T) {
	paths, sessionRoot := newSession(t)
	tests := []struct {
		name string
		root string
	}{
		{"traversal", filepath.Join(sessionRoot, "..", "..", "escape")},
		{"portfolio", filepath.Join(paths.Portfolio, "01 Projects", "Modules", "01 - M", "Sessions", "001 - S")},
		{"outside courses", filepath.Join(paths.Private, "Sessions", "001 - S")},
		{"wrong shape", filepath.Join(paths.Private, "01 Courses", "Course", "Notmodules", "01 - M", "Sessions", "001 - S")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSegmentAuthority(paths, test.root); CodeOf(err) != ErrorUnsafePath && CodeOf(err) != ErrorInvalidRequest {
				t.Fatalf("expected rejection, got %v", err)
			}
		})
	}
}

func TestSegmentAuthorityRequiresMarker(t *testing.T) {
	paths, sessionRoot := newSession(t)
	if err := os.Remove(filepath.Join(sessionRoot, sessionMarkerName)); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSegmentAuthority(paths, sessionRoot); CodeOf(err) != ErrorUnsafePath {
		t.Fatalf("missing marker = %v", err)
	}
}

func TestSegmentAuthorityRejectsSymlinkedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require privilege on windows")
	}
	paths, err := workspace.PathsFromRoot(filepath.Join(t.TempDir(), "StudyPilot"))
	if err != nil {
		t.Fatal(err)
	}
	realModules := filepath.Join(t.TempDir(), "realmodules")
	if err := os.MkdirAll(realModules, 0o750); err != nil {
		t.Fatal(err)
	}
	courseRoot := filepath.Join(paths.Private, "01 Courses", "Course")
	if err := os.MkdirAll(courseRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	// Symlink the Modules directory to a location outside the vault.
	if err := os.Symlink(realModules, filepath.Join(courseRoot, "Modules")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	sessionRoot := filepath.Join(courseRoot, "Modules", "01 - Module", "Sessions", "001 - Session")
	if err := os.MkdirAll(sessionRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionRoot, sessionMarkerName), []byte("{}"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSegmentAuthority(paths, sessionRoot); CodeOf(err) != ErrorUnsafePath {
		t.Fatalf("symlinked parent = %v", err)
	}
}

func TestResolveRejectsUnsafeNames(t *testing.T) {
	paths, sessionRoot := newSession(t)
	authority, err := NewSegmentAuthority(paths, sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "../escape.wav", "sub/dir.wav", `back\slash.wav`, "/absolute.wav", "..", "."} {
		if _, err := authority.Resolve(name); CodeOf(err) != ErrorUnsafePath {
			t.Errorf("Resolve(%q) = %v, want unsafe", name, err)
		}
	}
}

func TestEnsureSegmentsDirRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require privilege on windows")
	}
	paths, sessionRoot := newSession(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(sessionRoot, "Segments")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	authority, err := NewSegmentAuthority(paths, sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.EnsureSegmentsDir(); CodeOf(err) != ErrorUnsafePath {
		t.Fatalf("symlinked Segments = %v", err)
	}
}

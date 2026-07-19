package backend

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Arameair/studypilot/internal/platformfs"
	"github.com/Arameair/studypilot/internal/workspace"
)

// segmentsDirName is the only directory beneath a session that the backend may
// write recording output into.
const segmentsDirName = "Segments"

// sessionMarkerName proves a directory is a managed StudyPilot session.
const sessionMarkerName = ".studypilot-session.json"

// SegmentAuthority is an opaque capability constraining all recording output to
// one validated session's Segments directory. It refuses public-portfolio
// paths, traversal, symlinked parents or Segments directories, and sibling
// session writes. It mirrors the private-vault scoping the filesystem package
// enforces without weakening that package's own mutation or plan authority.
type SegmentAuthority struct {
	paths       workspace.Paths
	sessionRoot string
	segmentsDir string
}

// NewSegmentAuthority validates that sessionRoot is a real managed session
// beneath the private vault and returns an authority scoped to its Segments
// directory. It performs read-only checks and creates nothing.
func NewSegmentAuthority(paths workspace.Paths, sessionRoot string) (SegmentAuthority, error) {
	fail := func(message string) (SegmentAuthority, error) {
		return SegmentAuthority{}, newError(ErrorUnsafePath, "authority", message, nil)
	}
	if err := paths.Validate(); err != nil {
		return SegmentAuthority{}, newError(ErrorInvalidRequest, "authority", "invalid workspace paths", err)
	}
	if containsParentTraversal(sessionRoot) {
		return fail("session path contains parent traversal")
	}
	sessionRoot = filepath.Clean(sessionRoot)
	coursesRoot := filepath.Join(paths.Private, "01 Courses")
	if !strictlyWithin(coursesRoot, sessionRoot) {
		return fail("session is not within the private courses root")
	}
	if sessionRoot == filepath.Clean(paths.Portfolio) || strictlyWithin(paths.Portfolio, sessionRoot) {
		return fail("session resolves into the public portfolio")
	}
	// Enforce the .../<course>/Modules/<module>/Sessions/<session> shape.
	sessionsDir := filepath.Dir(sessionRoot)
	moduleRoot := filepath.Dir(sessionsDir)
	modulesDir := filepath.Dir(moduleRoot)
	courseRoot := filepath.Dir(modulesDir)
	if filepath.Base(sessionsDir) != "Sessions" || filepath.Base(modulesDir) != "Modules" || filepath.Dir(courseRoot) != filepath.Clean(coursesRoot) {
		return fail("session is not a valid module session directory")
	}
	// Reject a symlink anywhere in the chain from the courses root down.
	for _, component := range []string{courseRoot, modulesDir, moduleRoot, sessionsDir, sessionRoot} {
		if err := rejectSymlink(component); err != nil {
			return SegmentAuthority{}, err
		}
	}
	// Require the managed-session marker so arbitrary directories are refused.
	marker := filepath.Join(sessionRoot, sessionMarkerName)
	info, err := os.Lstat(marker)
	reparse, reparseErr := platformfs.PathHasReparsePoint(marker)
	if err != nil || reparseErr != nil || reparse || !info.Mode().IsRegular() {
		return fail("session directory has no managed session marker")
	}
	return SegmentAuthority{paths: paths, sessionRoot: sessionRoot, segmentsDir: filepath.Join(sessionRoot, segmentsDirName)}, nil
}

// SegmentsDir is the absolute path of the authorized Segments directory.
func (a SegmentAuthority) SegmentsDir() string { return a.segmentsDir }

// SessionRoot is the absolute validated session root.
func (a SegmentAuthority) SessionRoot() string { return a.sessionRoot }

// Resolve validates a single relative filename and returns its absolute path
// beneath the Segments directory. It rejects empty names, path separators,
// traversal, absolute inputs, control characters, and any symlinked Segments
// directory. It does not create anything.
func (a SegmentAuthority) Resolve(name string) (string, error) {
	fail := func(message string) (string, error) {
		return "", newError(ErrorUnsafePath, "authority", message, nil)
	}
	if name == "" || len(name) > 128 {
		return fail("segment file name is empty or too long")
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, filepath.Separator) {
		return fail("segment file name must be a single relative name")
	}
	if name == "." || name == ".." || containsControlRune(name) {
		return fail("segment file name is unsafe")
	}
	if err := rejectSymlink(a.segmentsDir); err != nil {
		return "", err
	}
	target := filepath.Join(a.segmentsDir, name)
	if !strictlyWithin(a.segmentsDir, target) {
		return fail("segment path escapes the Segments directory")
	}
	return target, nil
}

// EnsureSegmentsDir creates the Segments directory if absent, refusing to
// create through or over a symlink.
func (a SegmentAuthority) EnsureSegmentsDir() error {
	if err := rejectSymlink(a.sessionRoot); err != nil {
		return err
	}
	info, err := os.Lstat(a.segmentsDir)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return newError(ErrorUnsafePath, "authority", "Segments directory is a symlink", nil)
		}
		if !info.IsDir() {
			return newError(ErrorUnsafePath, "authority", "Segments path is not a directory", nil)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return newError(ErrorInternal, "authority", "inspect Segments directory", err)
	}
	if err := os.Mkdir(a.segmentsDir, 0o750); err != nil {
		if os.IsExist(err) {
			info, inspectErr := os.Lstat(a.segmentsDir)
			if inspectErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				reparse, reparseErr := platformfs.PathHasReparsePoint(a.segmentsDir)
				if reparseErr == nil && !reparse {
					return nil
				}
			}
		}
		return newError(ErrorInternal, "authority", "create Segments directory", err)
	}
	return nil
}

// rejectSymlink returns an unsafe-path error if the path exists as a symlink. A
// missing path is acceptable (the caller may create it safely).
func rejectSymlink(path string) error {
	reparse, reparseErr := platformfs.PathHasReparsePoint(path)
	if reparseErr != nil && !os.IsNotExist(reparseErr) {
		return newError(ErrorInternal, "authority", "inspect path", reparseErr)
	}
	if reparse {
		return newError(ErrorUnsafePath, "authority", "path is a symlink or reparse point", nil)
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return newError(ErrorInternal, "authority", "inspect path", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return newError(ErrorUnsafePath, "authority", "path is a symlink", nil)
	}
	return nil
}

func containsControlRune(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// strictlyWithin reports whether child is a strict descendant of parent, after
// cleaning both. Mirrors the filesystem package's boundary check.
func strictlyWithin(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if parent == child {
		return false
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func containsParentTraversal(path string) bool {
	for _, element := range strings.Split(filepath.ToSlash(path), "/") {
		if element == ".." {
			return true
		}
	}
	return false
}

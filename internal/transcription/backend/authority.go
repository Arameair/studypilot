package backend

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Arameair/studypilot/internal/platformfs"
	"github.com/Arameair/studypilot/internal/workspace"
)

const transcriptsDirName = "Transcripts"
const sessionMarkerName = ".studypilot-session.json"

type ArtifactAuthority struct {
	paths                       workspace.Paths
	sessionRoot, transcriptsDir string
}

func NewArtifactAuthority(paths workspace.Paths, sessionRoot string) (ArtifactAuthority, error) {
	fail := func(message string) (ArtifactAuthority, error) {
		return ArtifactAuthority{}, newError(ErrorUnsafePath, "artifact_authority", false, message, nil)
	}
	if err := paths.Validate(); err != nil {
		return fail("workspace paths are invalid")
	}
	if strings.Contains(filepath.ToSlash(sessionRoot), "../") {
		return fail("session path contains traversal")
	}
	sessionRoot = filepath.Clean(sessionRoot)
	courses := filepath.Join(paths.Private, "01 Courses")
	if !strictlyWithin(courses, sessionRoot) || sessionRoot == filepath.Clean(paths.Portfolio) || strictlyWithin(paths.Portfolio, sessionRoot) {
		return fail("session is outside the private courses boundary")
	}
	sessions := filepath.Dir(sessionRoot)
	module := filepath.Dir(sessions)
	modules := filepath.Dir(module)
	course := filepath.Dir(modules)
	if filepath.Base(sessions) != "Sessions" || filepath.Base(modules) != "Modules" || filepath.Dir(course) != filepath.Clean(courses) {
		return fail("session directory shape is invalid")
	}
	for _, p := range []string{course, modules, module, sessions, sessionRoot} {
		if err := rejectSymlink(p); err != nil {
			return ArtifactAuthority{}, err
		}
	}
	info, err := os.Lstat(filepath.Join(sessionRoot, sessionMarkerName))
	if err != nil || !info.Mode().IsRegular() {
		return fail("managed session marker is missing")
	}
	return ArtifactAuthority{paths: paths, sessionRoot: sessionRoot, transcriptsDir: filepath.Join(sessionRoot, transcriptsDirName)}, nil
}
func (a ArtifactAuthority) SessionRoot() string    { return a.sessionRoot }
func (a ArtifactAuthority) TranscriptsDir() string { return a.transcriptsDir }
func (a ArtifactAuthority) EnsureDir() error {
	if err := rejectSymlink(a.sessionRoot); err != nil {
		return err
	}
	info, err := os.Lstat(a.transcriptsDir)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return newError(ErrorUnsafePath, "artifact_authority", false, "Transcripts path is unsafe", nil)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return newError(ErrorInternal, "artifact_authority", false, "inspect Transcripts directory", err)
	}
	if err := os.Mkdir(a.transcriptsDir, 0o750); err != nil {
		return newError(ErrorArtifactWriteFailed, "artifact_authority", false, "create Transcripts directory", err)
	}
	return nil
}
func (a ArtifactAuthority) Resolve(relative string) (string, error) {
	if filepath.IsAbs(relative) || strings.Contains(relative, `\`) || containsControl(relative) {
		return "", newError(ErrorUnsafePath, "artifact_authority", false, "artifact path is unsafe", nil)
	}
	clean := filepath.ToSlash(filepath.Clean(relative))
	if clean != relative || !strings.HasPrefix(clean, "Transcripts/") || strings.Contains(strings.TrimPrefix(clean, "Transcripts/"), "/") {
		return "", newError(ErrorUnsafePath, "artifact_authority", false, "artifact path is outside Transcripts", nil)
	}
	if err := rejectSymlink(a.transcriptsDir); err != nil {
		return "", err
	}
	target := filepath.Join(a.sessionRoot, filepath.FromSlash(clean))
	if !strictlyWithin(a.transcriptsDir, target) {
		return "", newError(ErrorUnsafePath, "artifact_authority", false, "artifact path escapes Transcripts", nil)
	}
	if info, err := os.Lstat(target); err == nil {
		multiple, linkErr := platformfs.HasMultipleHardLinks(target)
		if info.Mode()&os.ModeSymlink != 0 || linkErr != nil || multiple {
			return "", newError(ErrorUnsafePath, "artifact_authority", false, "artifact target is linked", nil)
		}
	} else if !os.IsNotExist(err) {
		return "", newError(ErrorInternal, "artifact_authority", false, "inspect artifact target", err)
	}
	return target, nil
}
func rejectSymlink(p string) error {
	info, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return newError(ErrorInternal, "artifact_authority", false, "inspect path", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return newError(ErrorUnsafePath, "artifact_authority", false, "path is a symlink", nil)
	}
	return nil
}
func strictlyWithin(parent, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func containsControl(v string) bool {
	for _, r := range v {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

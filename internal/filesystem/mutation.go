package filesystem

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Arameair/studypilot/internal/platformfs"
	"github.com/Arameair/studypilot/internal/workspace"
)

// MutationScope identifies the private metadata boundary carried by an authority.
type MutationScope string

const (
	MutationScopeWorkspace MutationScope = "workspace"
	MutationScopeCourse    MutationScope = "course"
	MutationScopeModule    MutationScope = "module"
	MutationScopeSession   MutationScope = "session"
)

var (
	ErrInvalidMutation  = errors.New("invalid mutation")
	ErrUnauthorized     = errors.New("unauthorized mutation target")
	ErrUnmanagedTarget  = errors.New("unmanaged mutation target")
	ErrTargetNotFound   = errors.New("mutation target not found")
	ErrTargetNotRegular = errors.New("mutation target is not a regular file")
	ErrStateMismatch    = errors.New("mutation expected state mismatch")
)

// MutationStage identifies the last stage reached by a failed mutation.
type MutationStage string

const (
	MutationStageValidation      MutationStage = "validation"
	MutationStageRead            MutationStage = "read"
	MutationStageComparison      MutationStage = "comparison"
	MutationStageTemporaryCreate MutationStage = "temporary_create"
	MutationStageWrite           MutationStage = "write"
	MutationStageFileSync        MutationStage = "file_sync"
	MutationStageReplace         MutationStage = "replace"
	MutationStageDirectorySync   MutationStage = "directory_sync"
	MutationStageCleanup         MutationStage = "cleanup"
)

// MutationError records whether replacement occurred before a stage failed.
type MutationError struct {
	Stage    MutationStage
	Replaced bool
	Cause    error
}

func (e *MutationError) Error() string {
	return fmt.Sprintf("mutation %s failed (replaced=%t): %v", e.Stage, e.Replaced, e.Cause)
}

func (e *MutationError) Unwrap() error { return e.Cause }

// MutationAuthority is an opaque capability for one exact managed root.
// Values must be obtained from the constructors in this package.
type MutationAuthority struct {
	paths       workspace.Paths
	scope       MutationScope
	allowedRoot string
	parentRoot  string
	marker      string
}

func NewWorkspaceMutationAuthority(paths workspace.Paths) (MutationAuthority, error) {
	if err := paths.Validate(); err != nil {
		return MutationAuthority{}, fmt.Errorf("%w: %v", ErrInvalidMutation, err)
	}
	return MutationAuthority{paths: paths, scope: MutationScopeWorkspace, allowedRoot: filepath.Clean(paths.Root)}, nil
}

func NewCourseMutationAuthority(paths workspace.Paths, courseRoot string) (MutationAuthority, error) {
	if err := paths.Validate(); err != nil {
		return MutationAuthority{}, fmt.Errorf("%w: %v", ErrInvalidMutation, err)
	}
	if containsParentTraversal(courseRoot) {
		return MutationAuthority{}, ErrUnauthorized
	}
	courseRoot = filepath.Clean(courseRoot)
	if !strictlyWithin(filepath.Join(paths.Private, "01 Courses"), courseRoot) {
		return MutationAuthority{}, ErrUnauthorized
	}
	a := MutationAuthority{paths: paths, scope: MutationScopeCourse, allowedRoot: courseRoot, marker: courseMetadataFileName}
	if err := a.revalidate(); err != nil {
		return MutationAuthority{}, err
	}
	return a, nil
}

func NewModuleMutationAuthority(paths workspace.Paths, courseRoot, moduleRoot string) (MutationAuthority, error) {
	if containsParentTraversal(courseRoot) || containsParentTraversal(moduleRoot) {
		return MutationAuthority{}, ErrUnauthorized
	}
	course, err := NewCourseMutationAuthority(paths, courseRoot)
	if err != nil {
		return MutationAuthority{}, err
	}
	moduleRoot = filepath.Clean(moduleRoot)
	if !strictlyWithin(filepath.Join(course.allowedRoot, "Modules"), moduleRoot) {
		return MutationAuthority{}, ErrUnauthorized
	}
	a := MutationAuthority{paths: paths, scope: MutationScopeModule, allowedRoot: moduleRoot, parentRoot: course.allowedRoot, marker: moduleMetadataFileName}
	if err := a.revalidate(); err != nil {
		return MutationAuthority{}, err
	}
	return a, nil
}

func NewSessionMutationAuthority(paths workspace.Paths, moduleRoot, sessionRoot string) (MutationAuthority, error) {
	if err := paths.Validate(); err != nil {
		return MutationAuthority{}, fmt.Errorf("%w: %v", ErrInvalidMutation, err)
	}
	if containsParentTraversal(moduleRoot) || containsParentTraversal(sessionRoot) {
		return MutationAuthority{}, ErrUnauthorized
	}
	moduleRoot, sessionRoot = filepath.Clean(moduleRoot), filepath.Clean(sessionRoot)
	if !strictlyWithin(filepath.Join(paths.Private, "01 Courses"), moduleRoot) ||
		!strictlyWithin(filepath.Join(moduleRoot, "Sessions"), sessionRoot) {
		return MutationAuthority{}, ErrUnauthorized
	}
	a := MutationAuthority{paths: paths, scope: MutationScopeSession, allowedRoot: sessionRoot, parentRoot: moduleRoot, marker: sessionMetadataFileName}
	if err := a.revalidate(); err != nil {
		return MutationAuthority{}, err
	}
	return a, nil
}

const (
	courseMetadataFileName  = ".studypilot-course.json"
	moduleMetadataFileName  = ".studypilot-module.json"
	sessionMetadataFileName = ".studypilot-session.json"
	runtimeMetadataFileName = ".studypilot-runtime.json"
)

func (a MutationAuthority) revalidate() error {
	if a.allowedRoot == "" || a.scope == "" {
		return ErrInvalidMutation
	}
	if err := a.paths.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMutation, err)
	}
	if a.allowedRoot == filepath.Clean(a.paths.Portfolio) || strictlyWithin(a.paths.Portfolio, a.allowedRoot) {
		return ErrUnauthorized
	}
	switch a.scope {
	case MutationScopeWorkspace:
		if a.allowedRoot != filepath.Clean(a.paths.Root) {
			return ErrUnauthorized
		}
	case MutationScopeCourse:
		if !strictlyWithin(filepath.Join(a.paths.Private, "01 Courses"), a.allowedRoot) {
			return ErrUnauthorized
		}
	case MutationScopeModule:
		if !strictlyWithin(filepath.Join(a.paths.Private, "01 Courses"), a.parentRoot) ||
			!strictlyWithin(filepath.Join(a.parentRoot, "Modules"), a.allowedRoot) {
			return ErrUnauthorized
		}
		if _, _, err := readRegularManaged(filepath.Join(a.parentRoot, courseMetadataFileName)); err != nil {
			return err
		}
	case MutationScopeSession:
		courseRoot := filepath.Dir(filepath.Dir(a.parentRoot))
		if !strictlyWithin(filepath.Join(a.paths.Private, "01 Courses"), courseRoot) ||
			!strictlyWithin(filepath.Join(courseRoot, "Modules"), a.parentRoot) ||
			!strictlyWithin(filepath.Join(a.parentRoot, "Sessions"), a.allowedRoot) {
			return ErrUnauthorized
		}
		courseContent, _, err := readRegularManaged(filepath.Join(courseRoot, courseMetadataFileName))
		if err != nil {
			return err
		}
		moduleContent, _, err := readRegularManaged(filepath.Join(a.parentRoot, moduleMetadataFileName))
		if err != nil {
			return err
		}
		sessionContent, _, err := readRegularManaged(filepath.Join(a.allowedRoot, sessionMetadataFileName))
		if err != nil {
			return err
		}
		if err := validateSessionAuthorityMetadata(courseContent, moduleContent, sessionContent, filepath.Base(courseRoot), filepath.Base(a.parentRoot), filepath.Base(a.allowedRoot)); err != nil {
			return err
		}
	default:
		return ErrInvalidMutation
	}
	if err := checkMutationPath(a.allowedRoot); err != nil {
		return err
	}
	if a.marker != "" {
		if _, _, err := readRegularManaged(filepath.Join(a.allowedRoot, a.marker)); err != nil {
			return err
		}
	}
	return nil
}

func validateSessionAuthorityMetadata(courseContent, moduleContent, sessionContent []byte, courseDirectory, moduleDirectory, sessionDirectory string) error {
	var courseValue struct {
		SchemaVersion int    `json:"schema_version"`
		ID            string `json:"id"`
		DirectoryName string `json:"directory_name"`
	}
	var moduleValue struct {
		SchemaVersion int    `json:"schema_version"`
		ID            string `json:"id"`
		CourseID      string `json:"course_id"`
		DirectoryName string `json:"directory_name"`
	}
	var sessionValue struct {
		SchemaVersion int    `json:"schema_version"`
		ID            string `json:"id"`
		CourseID      string `json:"course_id"`
		ModuleID      string `json:"module_id"`
		DirectoryName string `json:"directory_name"`
	}
	if json.Unmarshal(courseContent, &courseValue) != nil || json.Unmarshal(moduleContent, &moduleValue) != nil || json.Unmarshal(sessionContent, &sessionValue) != nil ||
		courseValue.SchemaVersion != 1 || courseValue.ID == "" || courseValue.DirectoryName != courseDirectory ||
		moduleValue.SchemaVersion != 1 || moduleValue.ID == "" || moduleValue.CourseID != courseValue.ID || moduleValue.DirectoryName != moduleDirectory ||
		sessionValue.SchemaVersion != 1 || sessionValue.ID == "" || sessionValue.CourseID != courseValue.ID || sessionValue.ModuleID != moduleValue.ID || sessionValue.DirectoryName != sessionDirectory {
		return ErrInvalidMutation
	}
	return nil
}

// ExpectedState is the immutable state against which a mutation is compared.
type ExpectedState struct {
	ContentHash [32]byte
	Size        int64
}

// ManagedFile is a safe snapshot of a managed metadata file.
type ManagedFile struct {
	Path     string
	Content  []byte
	SHA256   string
	Mode     fs.FileMode
	Size     int64
	expected ExpectedState
}

func (m ManagedFile) ExpectedState() ExpectedState { return m.expected }

// ManagedFileState is a content-free snapshot used for reconciliation.
type ManagedFileState struct {
	Path   string
	SHA256 string
	Mode   fs.FileMode
	Size   int64
}

// Mutation is an opaque, validated replacement request.
type Mutation struct {
	authority   MutationAuthority
	target      string
	expected    ExpectedState
	replacement []byte
	mode        fs.FileMode
}

func NewMutation(authority MutationAuthority, target string, expected ExpectedState, replacement []byte, mode fs.FileMode) (Mutation, error) {
	if containsParentTraversal(target) {
		return Mutation{}, ErrUnauthorized
	}
	m := Mutation{
		authority: authority, target: filepath.Clean(target), expected: expected,
		replacement: append([]byte(nil), replacement...), mode: mode.Perm(),
	}
	if err := m.validate(); err != nil {
		return Mutation{}, err
	}
	return m, nil
}

func (m Mutation) validate() error {
	if err := m.authority.revalidate(); err != nil {
		return err
	}
	if !filepath.IsAbs(m.target) || containsParentTraversal(m.target) ||
		m.target == m.authority.allowedRoot || !strictlyWithin(m.authority.allowedRoot, m.target) {
		return ErrUnauthorized
	}
	if !m.authority.manages(m.target) {
		return ErrUnmanagedTarget
	}
	// Managed metadata must remain owner-readable/writable, non-executable, and inaccessible to others.
	if m.mode&0o600 != 0o600 || m.mode&0o027 != 0 || m.mode&0o111 != 0 {
		return ErrInvalidMutation
	}
	return nil
}

func (a MutationAuthority) manages(target string) bool {
	if filepath.Dir(target) != a.allowedRoot {
		return false
	}
	name := filepath.Base(target)
	switch a.scope {
	case MutationScopeWorkspace:
		return name == runtimeMetadataFileName
	case MutationScopeCourse:
		return name == courseMetadataFileName
	case MutationScopeModule:
		return name == moduleMetadataFileName
	case MutationScopeSession:
		return name == sessionMetadataFileName || name == runtimeMetadataFileName
	default:
		return false
	}
}

func checkMutationPath(path string) error {
	if !filepath.IsAbs(path) || containsParentTraversal(path) {
		return ErrUnsafePath
	}
	if err := checkPathForSymlinks(path); err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafePath, err)
	}
	return nil
}

func readRegularManaged(path string) ([]byte, fs.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, ErrTargetNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, ErrUnsafePath
	}
	if !info.Mode().IsRegular() {
		return nil, nil, ErrTargetNotRegular
	}
	multiple, linkErr := platformfs.HasMultipleHardLinks(path)
	if linkErr != nil || multiple {
		return nil, nil, fmt.Errorf("%w: hard-linked target", ErrUnsafePath)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	multiple, linkErr = platformfs.HasMultipleHardLinks(path)
	if !os.SameFile(info, opened) || !opened.Mode().IsRegular() || linkErr != nil || multiple {
		return nil, nil, ErrUnsafePath
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(opened, after) || after.Size() != int64(len(content)) {
		return nil, nil, ErrStateMismatch
	}
	return content, after, nil
}

func hashState(content []byte) ExpectedState {
	return ExpectedState{ContentHash: sha256.Sum256(content), Size: int64(len(content))}
}

func hashString(hash [32]byte) string { return fmt.Sprintf("%x", hash) }

func contextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

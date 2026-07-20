package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Arameair/studypilot/internal/platformfs"
)

var (
	ErrInvalidSetupRoot = errors.New("invalid workspace setup root")
	ErrUnsafeSetupRoot  = errors.New("unsafe workspace setup root")
)

// SetupDisposition is a stable description of an inspected setup target.
type SetupDisposition string

const (
	SetupNonexistent SetupDisposition = "nonexistent"
	SetupEmpty       SetupDisposition = "empty"
	SetupAdoptable   SetupDisposition = "adoptable"
	SetupConflicting SetupDisposition = "conflicting"
	SetupUnsafe      SetupDisposition = "unsafe"
)

// SetupInspection describes a candidate without modifying it.
type SetupInspection struct {
	Paths         Paths
	Disposition   SetupDisposition
	Exists        bool
	Writable      bool
	Initialized   bool
	CanInitialize bool
}

// InspectSetupRoot validates and inspects a user-selected absolute workspace
// root. home and sourceRoot are injected authority boundaries.
func InspectSetupRoot(root, home, sourceRoot string) (SetupInspection, error) {
	if strings.TrimSpace(root) == "" {
		return SetupInspection{}, fmt.Errorf("%w: root is required", ErrInvalidSetupRoot)
	}
	if containsParentTraversal(root) {
		return SetupInspection{}, fmt.Errorf("%w: parent traversal is not allowed", ErrInvalidSetupRoot)
	}
	if !filepath.IsAbs(root) {
		return SetupInspection{}, fmt.Errorf("%w: root must be absolute", ErrInvalidSetupRoot)
	}
	clean := filepath.Clean(root)
	if isFilesystemRoot(clean) {
		return SetupInspection{}, fmt.Errorf("%w: filesystem root is not allowed", ErrInvalidSetupRoot)
	}
	if samePath(clean, home) {
		return SetupInspection{}, fmt.Errorf("%w: the user home directory is not allowed", ErrInvalidSetupRoot)
	}
	if sourceRoot != "" && (samePath(clean, sourceRoot) || isStrictlyWithin(sourceRoot, clean)) {
		return SetupInspection{}, fmt.Errorf("%w: source repository containment is not allowed", ErrInvalidSetupRoot)
	}
	paths, err := PathsFromRoot(clean)
	if err != nil {
		return SetupInspection{}, fmt.Errorf("%w: resolve workspace paths", ErrInvalidSetupRoot)
	}
	unsafe, err := platformfs.PathHasReparsePoint(clean)
	if err != nil {
		return SetupInspection{}, fmt.Errorf("%w: inspect root", ErrUnsafeSetupRoot)
	}
	if unsafe {
		return SetupInspection{Paths: paths, Disposition: SetupUnsafe}, fmt.Errorf("%w: reparse points are not allowed", ErrUnsafeSetupRoot)
	}
	info, err := os.Lstat(clean)
	if errors.Is(err, fs.ErrNotExist) {
		return SetupInspection{
			Paths:         paths,
			Disposition:   SetupNonexistent,
			Writable:      nearestParentWritable(clean),
			CanInitialize: nearestParentWritable(clean),
		}, nil
	}
	if err != nil {
		return SetupInspection{}, fmt.Errorf("%w: inspect root", ErrUnsafeSetupRoot)
	}
	if !info.IsDir() {
		return SetupInspection{Paths: paths, Exists: true, Disposition: SetupConflicting}, fmt.Errorf("%w: root must be a directory", ErrInvalidSetupRoot)
	}
	writable := directoryWritable(info)
	entries, err := os.ReadDir(clean)
	if err != nil {
		return SetupInspection{}, fmt.Errorf("%w: inspect root contents", ErrUnsafeSetupRoot)
	}
	afterRoot, err := os.Lstat(clean)
	if err != nil || afterRoot.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, afterRoot) {
		return SetupInspection{}, fmt.Errorf("%w: root identity changed during inspection", ErrUnsafeSetupRoot)
	}
	if len(entries) == 0 {
		return SetupInspection{Paths: paths, Exists: true, Writable: writable, Disposition: SetupEmpty, CanInitialize: writable}, nil
	}
	if err := validateInitializedWorkspace(paths); err != nil {
		return SetupInspection{Paths: paths, Exists: true, Writable: writable, Disposition: SetupConflicting}, nil
	}
	return SetupInspection{Paths: paths, Exists: true, Writable: writable, Initialized: true, Disposition: SetupAdoptable, CanInitialize: writable}, nil
}

func validateInitializedWorkspace(paths Paths) error {
	for _, item := range []struct {
		root     string
		contract VaultContract
	}{
		{paths.Private, PrivateVaultContract()},
		{paths.Portfolio, PublicPortfolioContract()},
	} {
		unsafe, err := platformfs.PathHasReparsePoint(item.root)
		if err != nil || unsafe {
			return ErrUnsafeSetupRoot
		}
		info, err := os.Lstat(item.root)
		if err != nil || !info.IsDir() {
			return ErrInvalidSetupRoot
		}
		for _, directory := range item.contract.Directories {
			path := filepath.Join(item.root, directory)
			info, err := os.Lstat(path)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return ErrInvalidSetupRoot
			}
		}
		for _, expected := range item.contract.Files {
			path := filepath.Join(item.root, expected.Path)
			before, err := os.Lstat(path)
			if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
				return ErrInvalidSetupRoot
			}
			multiple, err := platformfs.HasMultipleHardLinks(path)
			if err != nil || multiple {
				return ErrUnsafeSetupRoot
			}
			data, err := os.ReadFile(path)
			after, afterErr := os.Lstat(path)
			if err != nil || afterErr != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, after) || string(data) != expected.Content {
				return ErrInvalidSetupRoot
			}
		}
	}
	return nil
}

func isFilesystemRoot(path string) bool {
	clean := filepath.Clean(path)
	return filepath.Dir(clean) == clean
}

func samePath(a, b string) bool {
	if strings.TrimSpace(b) == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func nearestParentWritable(path string) bool {
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			return info.IsDir() && directoryWritable(info)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
	}
}

func directoryWritable(info fs.FileInfo) bool {
	return info.Mode().Perm()&0o200 != 0
}

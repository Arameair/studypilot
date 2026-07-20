package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	privateVaultName = "Learning-Vault-Private"
	portfolioName    = "IT-Knowledge-Portfolio"
)

// Paths identifies a StudyPilot root and its two operationally separate vaults.
type Paths struct {
	Root      string
	Private   string
	Portfolio string
}

// DefaultPaths returns the default StudyPilot paths beneath the current user's
// home directory. It performs no filesystem writes.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("determine user home directory: %w", err)
	}

	return PathsFromRoot(filepath.Join(home, "Documents", "StudyPilot"))
}

// ProposedPaths returns the editable first-run proposal beneath the current
// user's Documents directory. It performs no filesystem writes and is not an
// implicit selection.
func ProposedPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("determine user home directory: %w", err)
	}
	return PathsFromRoot(filepath.Join(home, "Documents", "vaults"))
}

// PathsFromRoot resolves root and constructs the private and portfolio paths
// beneath it. It performs no filesystem writes.
func PathsFromRoot(root string) (Paths, error) {
	if strings.TrimSpace(root) == "" {
		return Paths{}, errors.New("workspace root must not be empty")
	}

	expanded, err := expandHome(root)
	if err != nil {
		return Paths{}, err
	}

	absolute, err := filepath.Abs(filepath.Clean(expanded))
	if err != nil {
		return Paths{}, fmt.Errorf("resolve workspace root: %w", err)
	}

	paths := Paths{
		Root:      absolute,
		Private:   filepath.Join(absolute, privateVaultName),
		Portfolio: filepath.Join(absolute, portfolioName),
	}
	if err := paths.Validate(); err != nil {
		return Paths{}, err
	}
	return paths, nil
}

// Validate checks path separation and containment lexically. It does not
// inspect the filesystem or require any path to exist.
func (p Paths) Validate() error {
	paths := []struct {
		name  string
		value string
	}{
		{name: "root", value: p.Root},
		{name: "private", value: p.Private},
		{name: "portfolio", value: p.Portfolio},
	}

	for _, path := range paths {
		if strings.TrimSpace(path.value) == "" {
			return fmt.Errorf("%s path must not be empty", path.name)
		}
		if !filepath.IsAbs(path.value) {
			return fmt.Errorf("%s path must be absolute", path.name)
		}
		if containsParentTraversal(path.value) {
			return fmt.Errorf("%s path contains unresolved parent traversal", path.name)
		}
	}

	if filepath.Clean(p.Root) == filepath.Clean(p.Private) ||
		filepath.Clean(p.Root) == filepath.Clean(p.Portfolio) {
		return errors.New("workspace root must differ from both vault paths")
	}
	if filepath.Clean(p.Private) == filepath.Clean(p.Portfolio) {
		return errors.New("private and portfolio paths must differ")
	}
	if !isStrictlyWithin(p.Root, p.Private) {
		return errors.New("private path must be inside workspace root")
	}
	if !isStrictlyWithin(p.Root, p.Portfolio) {
		return errors.New("portfolio path must be inside workspace root")
	}
	if isStrictlyWithin(p.Private, p.Portfolio) || isStrictlyWithin(p.Portfolio, p.Private) {
		return errors.New("private and portfolio paths must not be nested")
	}

	return nil
}

func expandHome(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("determine user home directory: %w", err)
		}
		return home, nil
	}

	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("determine user home directory: %w", err)
		}
		return filepath.Join(home, strings.TrimLeft(path[1:], `/\`)), nil
	}
	if strings.HasPrefix(path, "~") {
		return "", errors.New("only the current user's home directory may be expanded")
	}
	return path, nil
}

func containsParentTraversal(path string) bool {
	for _, part := range strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if part == ".." {
			return true
		}
	}
	return false
}

func isStrictlyWithin(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil || relative == "." || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

package course

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	studyfs "github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/workspace"
)

const coursesDirectory = "01 Courses"

// NewCoursePlan returns a deterministic, private-only course creation plan.
func NewCoursePlan(paths workspace.Paths, name string, date time.Time) (studyfs.Plan, error) {
	if err := paths.Validate(); err != nil {
		return studyfs.Plan{}, fmt.Errorf("validate workspace paths: %w", err)
	}
	base := filepath.Join(paths.Private, coursesDirectory)
	if err := requireDirectory(paths.Private, ErrMissingPrivateVault); err != nil {
		return studyfs.Plan{}, err
	}
	if err := requireDirectory(base, ErrMissingPrivateVault); err != nil {
		return studyfs.Plan{}, err
	}

	courseName, err := normalizeName(name)
	if err != nil {
		return studyfs.Plan{}, fmt.Errorf("%w: course name", err)
	}
	root := filepath.Join(base, courseName.Display)
	operations := []studyfs.Operation{
		directory(root),
		directory(filepath.Join(root, "Course Assets")),
		directory(filepath.Join(root, "Course Assets", "Documents")),
		directory(filepath.Join(root, "Course Assets", "Reference")),
		directory(filepath.Join(root, "Course Assets", "Screenshots")),
		directory(filepath.Join(root, "Modules")),
		file(filepath.Join(root, "Course Overview.md"), courseOverview(courseName, date)),
	}
	plan := studyfs.Plan{Root: root, Scope: studyfs.PlanScopeCourse, Operations: operations}
	if err := validatePrivatePlan(plan, paths); err != nil {
		return studyfs.Plan{}, err
	}
	return plan, nil
}

func courseOverview(name normalizedName, date time.Time) string {
	day := date.Format("2006-01-02")
	return fmt.Sprintf(`---
id: course-%s
type: course
visibility: private
course: %s
title: %s
status: active
created: %s
updated: %s
---

# %s

## Purpose

## Progress

## Modules

## Open Questions

## Related Knowledge
`, name.Slug, name.Slug, name.Display, day, day, name.Display)
}

func validatePrivatePlan(plan studyfs.Plan, paths workspace.Paths) error {
	if err := plan.Validate(); err != nil {
		return fmt.Errorf("validate plan: %w", err)
	}
	for _, operation := range plan.Operations {
		if !strictlyWithin(paths.Private, operation.Path) {
			return fmt.Errorf("operation %q is outside private vault", operation.Path)
		}
		if operation.Path == paths.Portfolio || strictlyWithin(paths.Portfolio, operation.Path) {
			return fmt.Errorf("operation %q enters public portfolio", operation.Path)
		}
	}
	return nil
}

func requireDirectory(path string, sentinel error) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s does not exist", sentinel, path)
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is a symlink", sentinel, path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", sentinel, path)
	}
	return nil
}

func directory(path string) studyfs.Operation {
	return studyfs.Operation{Kind: studyfs.OperationCreateDirectory, Path: path}
}

func file(path, content string) studyfs.Operation {
	return studyfs.Operation{Kind: studyfs.OperationCreateFile, Path: path, Content: content}
}

func strictlyWithin(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

package course

import (
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	studyfs "github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/workspace"
)

// NewModulePlan returns a deterministic module plan beneath an existing course.
func NewModulePlan(paths workspace.Paths, courseName string, moduleNumber int, moduleName string, date time.Time) (studyfs.Plan, error) {
	if err := paths.Validate(); err != nil {
		return studyfs.Plan{}, fmt.Errorf("validate workspace paths: %w", err)
	}
	course, err := normalizeName(courseName)
	if err != nil {
		return studyfs.Plan{}, fmt.Errorf("%w: course name", err)
	}
	module, err := normalizeName(moduleName)
	if err != nil {
		return studyfs.Plan{}, fmt.Errorf("%w: module name", err)
	}
	if moduleNumber <= 0 {
		return studyfs.Plan{}, ErrInvalidModuleNumber
	}

	courseRoot := filepath.Join(paths.Private, coursesDirectory, course.Display)
	modulesRoot := filepath.Join(courseRoot, "Modules")
	if err := requireDirectory(courseRoot, ErrMissingCourse); err != nil {
		return studyfs.Plan{}, err
	}
	if err := requireDirectory(modulesRoot, ErrMissingCourse); err != nil {
		return studyfs.Plan{}, err
	}

	number := fmt.Sprintf("%02d", moduleNumber)
	root := filepath.Join(modulesRoot, number+" - "+module.Display)
	operations := []studyfs.Operation{
		directory(root),
		directory(filepath.Join(root, "Assets")),
		directory(filepath.Join(root, "Assets", "Audio")),
		directory(filepath.Join(root, "Assets", "Documents")),
		directory(filepath.Join(root, "Assets", "Screenshots")),
		directory(filepath.Join(root, "Assets", "Video")),
		directory(filepath.Join(root, "Notes")),
		directory(filepath.Join(root, "Sessions")),
		directory(filepath.Join(root, "Transcripts")),
		file(filepath.Join(root, "Module Overview.md"), moduleOverview(course, module, moduleNumber, number, date)),
	}
	plan := studyfs.Plan{Root: root, Scope: studyfs.PlanScopeModule, Operations: operations}
	if err := validatePrivatePlan(plan, paths); err != nil {
		return studyfs.Plan{}, err
	}
	return plan, nil
}

func moduleOverview(course, module normalizedName, moduleNumber int, paddedNumber string, date time.Time) string {
	day := date.Format("2006-01-02")
	return fmt.Sprintf(`---
id: module-%s-%s-%s
type: module
visibility: private
course: %s
module: %s
module_number: %s
title: %s
status: active
created: %s
updated: %s
---

# %s - %s

## Learning Objectives

## Key Topics

## Questions

## Sessions

## Draft Knowledge

## Assets
`, course.Slug, paddedNumber, module.Slug, course.Slug, module.Slug,
		strconv.Itoa(moduleNumber), module.Display, day, day, paddedNumber, module.Display)
}

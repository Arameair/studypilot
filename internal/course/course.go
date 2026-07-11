package course

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	studyfs "github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/workspace"
)

const coursesDirectory = "01 Courses"

func NewCoursePlan(paths workspace.Paths, name string, date time.Time) (studyfs.Plan, error) {
	return NewCoursePlanWithID(paths, name, date, DefaultIDGenerator)
}

func NewCoursePlanWithID(paths workspace.Paths, name string, date time.Time, generate IDGenerator) (studyfs.Plan, error) {
	if generate == nil {
		return studyfs.Plan{}, errors.New("ID generator is required")
	}
	requested, err := normalizeName(name)
	if err != nil {
		return studyfs.Plan{}, fmt.Errorf("%w: course name", err)
	}
	records, err := scanCourses(paths)
	if err != nil {
		return studyfs.Plan{}, err
	}
	var existing *CourseRecord
	ids := make(map[string]bool)
	for index := range records {
		record := records[index]
		if ids[record.Metadata.ID] {
			return studyfs.Plan{}, fmt.Errorf("%w: duplicate course ID %s", ErrAmbiguous, record.Metadata.ID)
		}
		ids[record.Metadata.ID] = true
		normalized, _ := normalizeName(record.Metadata.DisplayName)
		if record.Metadata.DisplayName == requested.Display && record.Metadata.Slug == requested.Slug {
			existing = &records[index]
			continue
		}
		if record.Metadata.Slug == requested.Slug || normalized.Key == requested.Key {
			return studyfs.Plan{}, fmt.Errorf("%w: course name or slug already exists", ErrCollision)
		}
	}

	var metadata CourseMetadata
	var metadataContent string
	if existing != nil {
		metadata = existing.Metadata
		metadataContent = existing.raw
	} else {
		id, err := generate("course")
		if err != nil {
			return studyfs.Plan{}, err
		}
		if id == "" || ids[id] {
			return studyfs.Plan{}, fmt.Errorf("%w: generated course ID %q", ErrCollision, id)
		}
		metadata = CourseMetadata{
			SchemaVersion: metadataSchemaVersion, ID: id, DisplayName: requested.Display,
			Slug: requested.Slug, DirectoryName: requested.Display, CreatedAt: date, UpdatedAt: date,
		}
		metadataContent, err = encodeMetadata(metadata)
		if err != nil {
			return studyfs.Plan{}, err
		}
	}
	root := filepath.Join(paths.Private, coursesDirectory, metadata.DirectoryName)
	operations := []studyfs.Operation{
		directory(root), directory(filepath.Join(root, "Course Assets")),
		directory(filepath.Join(root, "Course Assets", "Documents")),
		directory(filepath.Join(root, "Course Assets", "Reference")),
		directory(filepath.Join(root, "Course Assets", "Screenshots")),
		directory(filepath.Join(root, "Modules")),
		file(filepath.Join(root, courseMetadataFile), metadataContent),
		file(filepath.Join(root, "Course Overview.md"), courseOverview(metadata)),
	}
	return studyfs.NewCourseScopedPlan(paths, root, operations)
}

func courseOverview(metadata CourseMetadata) string {
	created := metadata.CreatedAt.Format(time.RFC3339)
	updated := metadata.UpdatedAt.Format(time.RFC3339)
	return fmt.Sprintf(`---
id: %s
type: course
visibility: private
course_id: %s
slug: %s
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
`, metadata.ID, metadata.ID, metadata.Slug, metadata.DisplayName, created, updated, metadata.DisplayName)
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

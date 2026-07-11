package course

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	studyfs "github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/workspace"
)

func NewModulePlan(paths workspace.Paths, courseQuery string, moduleNumber int, moduleName string, date time.Time) (studyfs.Plan, error) {
	return NewModulePlanWithID(paths, courseQuery, moduleNumber, moduleName, date, DefaultIDGenerator)
}

func NewModulePlanWithID(paths workspace.Paths, courseQuery string, moduleNumber int, moduleName string, date time.Time, generate IDGenerator) (studyfs.Plan, error) {
	if moduleNumber <= 0 {
		return studyfs.Plan{}, ErrInvalidModuleNumber
	}
	if generate == nil {
		return studyfs.Plan{}, errors.New("ID generator is required")
	}
	requested, err := normalizeName(moduleName)
	if err != nil {
		return studyfs.Plan{}, fmt.Errorf("%w: module name", err)
	}
	course, err := FindCourse(paths, courseQuery)
	if err != nil {
		return studyfs.Plan{}, err
	}
	records, err := scanModules(course)
	if err != nil {
		return studyfs.Plan{}, err
	}
	var existing *ModuleRecord
	ids := make(map[string]bool)
	for index := range records {
		record := records[index]
		if ids[record.Metadata.ID] {
			return studyfs.Plan{}, fmt.Errorf("%w: duplicate module ID %s", ErrAmbiguous, record.Metadata.ID)
		}
		ids[record.Metadata.ID] = true
		normalized, _ := normalizeName(record.Metadata.DisplayName)
		if record.Metadata.Number == moduleNumber && record.Metadata.DisplayName == requested.Display && record.Metadata.Slug == requested.Slug {
			existing = &records[index]
			continue
		}
		if record.Metadata.Number == moduleNumber {
			return studyfs.Plan{}, fmt.Errorf("%w: module number %d already exists", ErrCollision, moduleNumber)
		}
		if record.Metadata.Slug == requested.Slug || normalized.Key == requested.Key {
			return studyfs.Plan{}, fmt.Errorf("%w: module name or slug already exists", ErrCollision)
		}
	}

	var metadata ModuleMetadata
	var metadataContent string
	if existing != nil {
		metadata = existing.Metadata
		metadataContent = existing.raw
	} else {
		id, err := generate("module")
		if err != nil {
			return studyfs.Plan{}, err
		}
		if id == "" || ids[id] {
			return studyfs.Plan{}, fmt.Errorf("%w: generated module ID %q", ErrCollision, id)
		}
		directoryName := fmt.Sprintf("%02d - %s", moduleNumber, requested.Display)
		metadata = ModuleMetadata{
			SchemaVersion: metadataSchemaVersion, ID: id, CourseID: course.Metadata.ID,
			Number: moduleNumber, DisplayName: requested.Display, Slug: requested.Slug,
			DirectoryName: directoryName, CreatedAt: date, UpdatedAt: date,
		}
		metadataContent, err = encodeMetadata(metadata)
		if err != nil {
			return studyfs.Plan{}, err
		}
	}
	root := filepath.Join(course.Path, "Modules", metadata.DirectoryName)
	operations := []studyfs.Operation{
		directory(root), directory(filepath.Join(root, "Assets")),
		directory(filepath.Join(root, "Assets", "Audio")), directory(filepath.Join(root, "Assets", "Documents")),
		directory(filepath.Join(root, "Assets", "Screenshots")), directory(filepath.Join(root, "Assets", "Video")),
		directory(filepath.Join(root, "Notes")), directory(filepath.Join(root, "Sessions")), directory(filepath.Join(root, "Transcripts")),
		file(filepath.Join(root, moduleMetadataFile), metadataContent),
		file(filepath.Join(root, "Module Overview.md"), moduleOverview(metadata)),
	}
	return studyfs.NewModuleScopedPlan(paths, course.Path, root, operations)
}

func moduleOverview(metadata ModuleMetadata) string {
	created := metadata.CreatedAt.Format(time.RFC3339)
	updated := metadata.UpdatedAt.Format(time.RFC3339)
	return fmt.Sprintf(`---
id: %s
type: module
visibility: private
course_id: %s
module_id: %s
module_number: %s
slug: %s
title: %s
status: active
created: %s
updated: %s
---

# %02d - %s

## Learning Objectives

## Key Topics

## Questions

## Sessions

## Draft Knowledge

## Assets
`, metadata.ID, metadata.CourseID, metadata.ID, strconv.Itoa(metadata.Number), metadata.Slug,
		metadata.DisplayName, created, updated, metadata.Number, metadata.DisplayName)
}

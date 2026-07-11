package course

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Arameair/studypilot/internal/workspace"
)

const (
	metadataSchemaVersion = 1
	courseMetadataFile    = ".studypilot-course.json"
	moduleMetadataFile    = ".studypilot-module.json"
)

type IDGenerator func(kind string) (string, error)

type CourseMetadata struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	DisplayName   string    `json:"display_name"`
	Slug          string    `json:"slug"`
	DirectoryName string    `json:"directory_name"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ModuleMetadata struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	CourseID      string    `json:"course_id"`
	Number        int       `json:"number"`
	DisplayName   string    `json:"display_name"`
	Slug          string    `json:"slug"`
	DirectoryName string    `json:"directory_name"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CourseRecord struct {
	Metadata CourseMetadata
	Path     string
	raw      string
}

type ModuleRecord struct {
	Metadata ModuleMetadata
	Path     string
	raw      string
}

func DefaultIDGenerator(kind string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return kind + "-" + hex.EncodeToString(value[:]), nil
}

func FindCourse(paths workspace.Paths, query string) (CourseRecord, error) {
	records, err := scanCourses(paths)
	if err != nil {
		return CourseRecord{}, err
	}
	var matches []CourseRecord
	for _, record := range records {
		if query == record.Metadata.ID || query == record.Metadata.DisplayName || query == record.Metadata.Slug {
			matches = append(matches, record)
		}
	}
	if len(matches) == 0 {
		return CourseRecord{}, ErrMissingCourse
	}
	if len(matches) != 1 {
		return CourseRecord{}, fmt.Errorf("%w: course query %q", ErrAmbiguous, query)
	}
	return matches[0], nil
}

func FindModule(course CourseRecord, query string) (ModuleRecord, error) {
	records, err := scanModules(course)
	if err != nil {
		return ModuleRecord{}, err
	}
	var matches []ModuleRecord
	for _, record := range records {
		if query == record.Metadata.ID || query == record.Metadata.DisplayName || query == record.Metadata.Slug || query == fmt.Sprint(record.Metadata.Number) {
			matches = append(matches, record)
		}
	}
	if len(matches) == 0 {
		return ModuleRecord{}, ErrMissingCourse
	}
	if len(matches) != 1 {
		return ModuleRecord{}, fmt.Errorf("%w: module query %q", ErrAmbiguous, query)
	}
	return matches[0], nil
}

func scanCourses(paths workspace.Paths) ([]CourseRecord, error) {
	if err := paths.Validate(); err != nil {
		return nil, err
	}
	base := filepath.Join(paths.Private, coursesDirectory)
	if err := requireDirectory(base, ErrMissingPrivateVault); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	records := make([]CourseRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(base, entry.Name())
		record, err := readCourseRecord(path)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func scanModules(course CourseRecord) ([]ModuleRecord, error) {
	base := filepath.Join(course.Path, "Modules")
	if err := requireDirectory(base, ErrMissingCourse); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	records := make([]ModuleRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		record, err := readModuleRecord(filepath.Join(base, entry.Name()), course.Metadata.ID)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func readCourseRecord(path string) (CourseRecord, error) {
	raw, err := readMetadata(filepath.Join(path, courseMetadataFile))
	if errors.Is(err, fs.ErrNotExist) {
		return CourseRecord{}, fmt.Errorf("%w: %s", ErrUnmanagedDirectory, path)
	}
	if err != nil {
		return CourseRecord{}, err
	}
	var metadata CourseMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return CourseRecord{}, fmt.Errorf("%w: %s: %v", ErrMalformedMetadata, path, err)
	}
	if err := validateCourseMetadata(metadata, filepath.Base(path)); err != nil {
		return CourseRecord{}, err
	}
	return CourseRecord{Metadata: metadata, Path: path, raw: raw}, nil
}

func readModuleRecord(path, courseID string) (ModuleRecord, error) {
	raw, err := readMetadata(filepath.Join(path, moduleMetadataFile))
	if errors.Is(err, fs.ErrNotExist) {
		return ModuleRecord{}, fmt.Errorf("%w: %s", ErrUnmanagedDirectory, path)
	}
	if err != nil {
		return ModuleRecord{}, err
	}
	var metadata ModuleMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return ModuleRecord{}, fmt.Errorf("%w: %s: %v", ErrMalformedMetadata, path, err)
	}
	if err := validateModuleMetadata(metadata, filepath.Base(path), courseID); err != nil {
		return ModuleRecord{}, err
	}
	return ModuleRecord{Metadata: metadata, Path: path, raw: raw}, nil
}

func validateCourseMetadata(metadata CourseMetadata, directory string) error {
	name, err := normalizeName(metadata.DisplayName)
	if metadata.SchemaVersion != metadataSchemaVersion || err != nil || metadata.ID == "" || !strings.HasPrefix(metadata.ID, "course-") ||
		metadata.Slug != name.Slug || metadata.DirectoryName != directory || metadata.CreatedAt.IsZero() || metadata.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: course metadata for %s", ErrMalformedMetadata, directory)
	}
	return nil
}

func validateModuleMetadata(metadata ModuleMetadata, directory, courseID string) error {
	name, err := normalizeName(metadata.DisplayName)
	wantDirectory := fmt.Sprintf("%02d - %s", metadata.Number, metadata.DisplayName)
	if metadata.SchemaVersion != metadataSchemaVersion || err != nil || metadata.ID == "" || !strings.HasPrefix(metadata.ID, "module-") ||
		metadata.CourseID != courseID || metadata.Number <= 0 || metadata.Slug != name.Slug || metadata.DirectoryName != directory ||
		metadata.DirectoryName != wantDirectory || metadata.CreatedAt.IsZero() || metadata.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: module metadata for %s", ErrMalformedMetadata, directory)
	}
	return nil
}

func readMetadata(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: unsafe metadata path %s", ErrMalformedMetadata, path)
	}
	data, err := os.ReadFile(path)
	return string(data), err
}

func encodeMetadata(value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(append(data, '\n')), nil
}

package course

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	studyfs "github.com/Arameair/studypilot/internal/filesystem"
)

func TestCourseLookupByMetadataFields(t *testing.T) {
	paths := initializedWorkspace(t)
	plan, err := NewCoursePlanWithID(paths, "TCM Practical Help Desk", fixedDate, fixedID("course-immutable"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studyfs.Execute(plan); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"course-immutable", "TCM Practical Help Desk", "tcm-practical-help-desk"} {
		record, err := FindCourse(paths, query)
		if err != nil || record.Metadata.ID != "course-immutable" {
			t.Errorf("FindCourse(%q) = %#v/%v", query, record, err)
		}
	}
}

func TestUnmanagedCourseDirectoryRejected(t *testing.T) {
	paths := initializedWorkspace(t)
	if err := os.Mkdir(filepath.Join(paths.Private, coursesDirectory, "Legacy Course"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCoursePlanWithID(paths, "New Course", fixedDate, fixedID("course-new")); !errors.Is(err, ErrUnmanagedDirectory) {
		t.Fatalf("error = %v, want unmanaged directory", err)
	}
}

func TestMalformedAndUnsupportedMetadataRejected(t *testing.T) {
	tests := []struct{ name, content string }{
		{name: "malformed", content: "{"},
		{name: "unsupported schema", content: `{"schema_version":2}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := initializedWorkspace(t)
			root := filepath.Join(paths.Private, coursesDirectory, "Managed")
			if err := os.Mkdir(root, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, courseMetadataFile), []byte(test.content), 0o640); err != nil {
				t.Fatal(err)
			}
			if _, err := NewCoursePlanWithID(paths, "New", fixedDate, fixedID("course-new")); !errors.Is(err, ErrMalformedMetadata) {
				t.Fatalf("error = %v, want malformed metadata", err)
			}
		})
	}
}

func TestAmbiguousCourseLookupRejected(t *testing.T) {
	paths := initializedWorkspace(t)
	for _, name := range []string{"One", "Two"} {
		root := filepath.Join(paths.Private, coursesDirectory, name)
		if err := os.Mkdir(root, 0o750); err != nil {
			t.Fatal(err)
		}
		meta := CourseMetadata{SchemaVersion: 1, ID: "course-duplicate", DisplayName: name, Slug: slugify(name), DirectoryName: name, CreatedAt: fixedDate, UpdatedAt: fixedDate}
		data, _ := json.Marshal(meta)
		if err := os.WriteFile(filepath.Join(root, courseMetadataFile), data, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := FindCourse(paths, "course-duplicate"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("error = %v, want ambiguity", err)
	}
}

func TestMetadataConflictPreserved(t *testing.T) {
	paths := initializedWorkspace(t)
	plan, err := NewCoursePlanWithID(paths, "Course", fixedDate, fixedID("course-id"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(plan.Root, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(plan.Root, courseMetadataFile)
	if err := os.WriteFile(path, []byte("preserve"), 0o640); err != nil {
		t.Fatal(err)
	}
	report, err := studyfs.Execute(plan)
	if err != nil || !report.HasConflicts() {
		t.Fatalf("report/error = %#v/%v", report, err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "preserve" {
		t.Fatal("metadata conflict overwritten")
	}
}

func TestUnmanagedAndUnsupportedModuleMetadataRejected(t *testing.T) {
	tests := []struct {
		name    string
		content *string
	}{
		{name: "missing metadata"},
		{name: "unsupported schema", content: stringPointer(`{"schema_version":2}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := initializedWorkspace(t)
			coursePlan, _ := NewCoursePlanWithID(paths, "Course", fixedDate, fixedID("course-parent"))
			if _, err := studyfs.Execute(coursePlan); err != nil {
				t.Fatal(err)
			}
			moduleRoot := filepath.Join(coursePlan.Root, "Modules", "01 - Legacy")
			if err := os.Mkdir(moduleRoot, 0o750); err != nil {
				t.Fatal(err)
			}
			if test.content != nil {
				if err := os.WriteFile(filepath.Join(moduleRoot, moduleMetadataFile), []byte(*test.content), 0o640); err != nil {
					t.Fatal(err)
				}
			}
			_, err := NewModulePlanWithID(paths, "course-parent", 2, "New", fixedDate, fixedID("module-new"))
			if test.content == nil && !errors.Is(err, ErrUnmanagedDirectory) {
				t.Fatalf("error = %v", err)
			}
			if test.content != nil && !errors.Is(err, ErrMalformedMetadata) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

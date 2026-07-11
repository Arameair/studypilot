package session

import (
	"errors"
	"testing"
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

func validMetadata() Metadata {
	return Metadata{SchemaVersion: 1, ID: "session-abc", CourseID: "course-abc", ModuleID: "module-abc", Number: 1, DisplayName: "First Session", Slug: "first-session", DirectoryName: "001 - First Session", CreatedAt: time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)}
}

func TestMetadataValidation(t *testing.T) {
	valid := validMetadata()
	if err := valid.Validate(valid.CourseID, valid.ModuleID, valid.DirectoryName); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name                          string
		mutate                        func(*Metadata)
		courseID, moduleID, directory string
	}{
		{"invalid ID", func(m *Metadata) { m.ID = "slug-date" }, valid.CourseID, valid.ModuleID, valid.DirectoryName},
		{"parent course", func(*Metadata) {}, "course-other", valid.ModuleID, valid.DirectoryName},
		{"parent module", func(*Metadata) {}, valid.CourseID, "module-other", valid.DirectoryName},
		{"zero number", func(m *Metadata) { m.Number = 0 }, valid.CourseID, valid.ModuleID, valid.DirectoryName},
		{"zero timestamp", func(m *Metadata) { m.CreatedAt = time.Time{} }, valid.CourseID, valid.ModuleID, valid.DirectoryName},
		{"future schema", func(m *Metadata) { m.SchemaVersion = 2 }, valid.CourseID, valid.ModuleID, valid.DirectoryName},
		{"directory mismatch", func(*Metadata) {}, valid.CourseID, valid.ModuleID, "other"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.mutate(&value)
			if err := value.Validate(test.courseID, test.moduleID, test.directory); !errors.Is(err, ErrInvalidMetadata) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestRuntimeStateIdentityAndSnapshotValidation(t *testing.T) {
	metadata := validMetadata()
	snapshot := studyruntime.Snapshot{SchemaVersion: 1, CourseID: metadata.CourseID, CourseName: "Course", ModuleID: metadata.ModuleID, ModuleNumber: 1, ModuleName: "Module", SessionID: metadata.ID, SessionNumber: metadata.Number, SessionTitle: metadata.DisplayName, SessionStatus: studyruntime.SessionStatusPlanned, CaptureStatus: studyruntime.CaptureStatusUnavailable, TranscriptionStatus: studyruntime.TranscriptionStatusNotStarted, FilesystemStatus: studyruntime.FilesystemStatusReady, PublicationStatus: studyruntime.PublicationStatusPrivate, UpdatedAt: metadata.CreatedAt}
	state := RuntimeState{SchemaVersion: 1, SessionID: metadata.ID, Revision: 1, Snapshot: snapshot}
	if err := state.Validate(metadata); err != nil {
		t.Fatal(err)
	}
	state.SessionID = "session-other"
	if err := state.Validate(metadata); !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("got %v", err)
	}
}

package session

import (
	"errors"
	"fmt"
	"strings"
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

const (
	MetadataSchemaVersion = 1
	RuntimeSchemaVersion  = 1
)

var (
	ErrInvalidMetadata   = errors.New("invalid session metadata")
	ErrSessionNotFound   = errors.New("session not found")
	ErrSessionConflict   = errors.New("session conflict")
	ErrDuplicateNumber   = errors.New("duplicate session number")
	ErrInvalidTransition = errors.New("invalid session transition")
	ErrMalformedState    = errors.New("malformed session state")
	ErrIdentityMismatch  = errors.New("session identity mismatch")
)

type Metadata struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	CourseID      string    `json:"course_id"`
	ModuleID      string    `json:"module_id"`
	Number        int       `json:"number"`
	DisplayName   string    `json:"display_name"`
	Slug          string    `json:"slug"`
	DirectoryName string    `json:"directory_name"`
	CreatedAt     time.Time `json:"created_at"`
}

func (m Metadata) Validate(courseID, moduleID, directory string) error {
	if m.SchemaVersion != MetadataSchemaVersion || !strings.HasPrefix(m.ID, "session-") || m.ID == "session-" ||
		m.CourseID == "" || m.ModuleID == "" || m.CourseID != courseID || m.ModuleID != moduleID || m.Number <= 0 ||
		strings.TrimSpace(m.DisplayName) == "" || m.Slug == "" || m.DirectoryName == "" || m.CreatedAt.IsZero() {
		return ErrInvalidMetadata
	}
	if directory != "" && m.DirectoryName != directory {
		return ErrInvalidMetadata
	}
	wantName, wantSlug, err := sessionNames(m.Number, m.DisplayName)
	if err != nil || wantName != m.DirectoryName || wantSlug != m.Slug {
		return ErrInvalidMetadata
	}
	return nil
}

type RuntimeState struct {
	SchemaVersion int                   `json:"schema_version"`
	SessionID     string                `json:"session_id"`
	Revision      uint64                `json:"revision"`
	Snapshot      studyruntime.Snapshot `json:"snapshot"`
}

func (s RuntimeState) Validate(metadata Metadata) error {
	if s.SchemaVersion != RuntimeSchemaVersion || s.SessionID != metadata.ID || s.Revision == 0 {
		return ErrIdentityMismatch
	}
	if s.Snapshot.SessionID != metadata.ID || s.Snapshot.CourseID != metadata.CourseID || s.Snapshot.ModuleID != metadata.ModuleID ||
		s.Snapshot.SessionNumber != metadata.Number || s.Snapshot.SessionTitle != metadata.DisplayName {
		return ErrIdentityMismatch
	}
	if err := s.Snapshot.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedState, err)
	}
	return nil
}

func validateSnapshotTransitions(current, next studyruntime.Snapshot) error {
	checks := []error{
		studyruntime.ValidateSessionTransition(current.SessionStatus, next.SessionStatus),
		studyruntime.ValidateCaptureTransition(current.CaptureStatus, next.CaptureStatus),
		studyruntime.ValidateTranscriptionTransition(current.TranscriptionStatus, next.TranscriptionStatus),
		studyruntime.ValidateFilesystemTransition(current.FilesystemStatus, next.FilesystemStatus),
		studyruntime.ValidatePublicationTransition(current.PublicationStatus, next.PublicationStatus),
	}
	for _, err := range checks {
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidTransition, err)
		}
	}
	return nil
}

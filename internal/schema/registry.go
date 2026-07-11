package schema

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

type Definition struct {
	Type           DocumentType
	CurrentVersion Version
	MinimumVersion Version
	ManagedFields  []string
	ManagedRegions []string
	Reserved       bool
	Validate       func(Document) error
}

type Registry struct{ definitions map[DocumentType]Definition }

func NewRegistry(definitions ...Definition) (*Registry, error) {
	registry := &Registry{definitions: make(map[DocumentType]Definition, len(definitions))}
	for _, definition := range definitions {
		if definition.Type == "" || definition.MinimumVersion < 1 || definition.CurrentVersion < definition.MinimumVersion {
			return nil, fmt.Errorf("%w: %s", ErrInvalidDefinition, definition.Type)
		}
		if _, exists := registry.definitions[definition.Type]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateDefinition, definition.Type)
		}
		definition.ManagedFields = slices.Clone(definition.ManagedFields)
		definition.ManagedRegions = slices.Clone(definition.ManagedRegions)
		registry.definitions[definition.Type] = definition
	}
	return registry, nil
}

func DefaultRegistry() *Registry {
	commonMarkdown := []string{"schema_version", "id", "course_id", "module_id", "session_id", "type", "visibility", "status", "created", "updated"}
	registry, err := NewRegistry(
		Definition{Type: DocumentCourseMetadata, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: []string{"schema_version", "id", "display_name", "slug", "directory_name", "created_at", "updated_at"}, Validate: validateJSONObject},
		Definition{Type: DocumentModuleMetadata, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: []string{"schema_version", "id", "course_id", "number", "display_name", "slug", "directory_name", "created_at", "updated_at"}, Validate: validateJSONObject},
		Definition{Type: DocumentCourseOverview, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: commonMarkdown},
		Definition{Type: DocumentModuleOverview, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: commonMarkdown},
		Definition{Type: DocumentSessionMetadata, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: []string{"schema_version", "id", "course_id", "module_id", "number", "display_name", "slug", "directory_name", "created_at"}, Validate: validateSessionMetadata},
		Definition{Type: DocumentSessionNote, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: commonMarkdown, ManagedRegions: []string{"session-summary"}, Reserved: true},
		Definition{Type: DocumentTranscript, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: []string{"schema_version", "session_id", "type", "visibility"}, Reserved: true},
		Definition{Type: DocumentRuntimeState, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: []string{"schema_version", "session_id", "revision", "snapshot"}, Validate: validateRuntimeState},
	)
	if err != nil {
		panic(err)
	}
	return registry
}

func validateSessionMetadata(document Document) error {
	var value struct {
		SchemaVersion int       `json:"schema_version"`
		ID            string    `json:"id"`
		CourseID      string    `json:"course_id"`
		ModuleID      string    `json:"module_id"`
		DisplayName   string    `json:"display_name"`
		Slug          string    `json:"slug"`
		DirectoryName string    `json:"directory_name"`
		Number        int       `json:"number"`
		CreatedAt     time.Time `json:"created_at"`
	}
	if err := json.Unmarshal(document.Content, &value); err != nil {
		return err
	}
	if value.SchemaVersion != 1 || !strings.HasPrefix(value.ID, "session-") || value.ID == "session-" || value.CourseID == "" || value.ModuleID == "" || value.Number <= 0 || value.DisplayName == "" || value.Slug == "" || value.DirectoryName == "" || value.CreatedAt.IsZero() {
		return fmt.Errorf("invalid session metadata")
	}
	return nil
}

func validateRuntimeState(document Document) error {
	var value struct {
		SchemaVersion int                   `json:"schema_version"`
		SessionID     string                `json:"session_id"`
		Revision      uint64                `json:"revision"`
		Snapshot      studyruntime.Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(document.Content, &value); err != nil {
		return err
	}
	if value.SchemaVersion != 1 || value.SessionID == "" || value.Revision == 0 || value.Snapshot.SessionID != value.SessionID {
		return fmt.Errorf("invalid runtime state identity")
	}
	return value.Snapshot.Validate()
}

func validateJSONObject(document Document) error {
	var value map[string]any
	if err := json.Unmarshal(document.Content, &value); err != nil {
		return err
	}
	if value == nil {
		return fmt.Errorf("document must be a JSON object")
	}
	return nil
}

func (r *Registry) Definition(documentType DocumentType) (Definition, bool) {
	definition, ok := r.definitions[documentType]
	definition.ManagedFields = slices.Clone(definition.ManagedFields)
	definition.ManagedRegions = slices.Clone(definition.ManagedRegions)
	return definition, ok
}

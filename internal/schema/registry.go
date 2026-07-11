package schema

import (
	"encoding/json"
	"fmt"
	"slices"
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
		Definition{Type: DocumentSessionMetadata, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: commonMarkdown, Reserved: true, Validate: validateJSONObject},
		Definition{Type: DocumentSessionNote, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: commonMarkdown, ManagedRegions: []string{"session-summary"}, Reserved: true},
		Definition{Type: DocumentTranscript, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: []string{"schema_version", "session_id", "type", "visibility"}, Reserved: true},
		Definition{Type: DocumentRuntimeState, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: []string{"schema_version"}, Reserved: true, Validate: validateJSONObject},
	)
	if err != nil {
		panic(err)
	}
	return registry
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

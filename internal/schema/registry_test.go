package schema

import (
	"errors"
	"testing"
)

func TestRegistryValidationAndFutureVersion(t *testing.T) {
	valid := Definition{Type: DocumentCourseMetadata, MinimumVersion: 1, CurrentVersion: 1, ManagedFields: []string{"schema_version"}}
	registry, err := NewRegistry(valid)
	if err != nil {
		t.Fatal(err)
	}
	if definition, ok := registry.Definition(DocumentCourseMetadata); !ok || definition.CurrentVersion != 1 {
		t.Fatalf("definition=%+v ok=%v", definition, ok)
	}
	if _, err := NewRegistry(valid, valid); !errors.Is(err, ErrDuplicateDefinition) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := NewRegistry(Definition{Type: DocumentCourseMetadata, MinimumVersion: 0, CurrentVersion: 1}); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("invalid: %v", err)
	}
	if _, err := ParseDocument(".studypilot-course.json", []byte(`{"schema_version":2}`), valid); !errors.Is(err, ErrUnsupportedFutureVersion) {
		t.Fatalf("future: %v", err)
	}
}

func TestSessionAndRuntimeSchemasAreImplementedAndValidated(t *testing.T) {
	registry := DefaultRegistry()
	for _, documentType := range []DocumentType{DocumentSessionMetadata, DocumentRuntimeState} {
		definition, ok := registry.Definition(documentType)
		if !ok || definition.Reserved || definition.CurrentVersion != 1 || definition.Validate == nil {
			t.Fatalf("definition %s=%+v ok=%v", documentType, definition, ok)
		}
	}
	definition, _ := registry.Definition(DocumentSessionMetadata)
	valid := []byte(`{"schema_version":1,"id":"session-one","course_id":"course-one","module_id":"module-one","number":1,"display_name":"One","slug":"one","directory_name":"001 - One","created_at":"2026-07-11T12:00:00Z"}`)
	if _, err := ParseDocument(".studypilot-session.json", valid, definition); err != nil {
		t.Fatal(err)
	}
	future := []byte(`{"schema_version":2}`)
	if _, err := ParseDocument(".studypilot-session.json", future, definition); !errors.Is(err, ErrUnsupportedFutureVersion) {
		t.Fatalf("future: %v", err)
	}
}

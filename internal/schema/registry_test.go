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

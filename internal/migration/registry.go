package migration

import (
	"fmt"

	"github.com/Arameair/studypilot/internal/schema"
)

type edge struct {
	from, to  schema.Version
	migration Migration
}
type Registry struct {
	schemas *schema.Registry
	edges   map[schema.DocumentType]map[schema.Version]edge
}

func NewRegistry(schemas *schema.Registry, migrations ...Migration) (*Registry, error) {
	if schemas == nil {
		return nil, fmt.Errorf("%w: nil schema registry", ErrInvalidMigration)
	}
	r := &Registry{schemas: schemas, edges: map[schema.DocumentType]map[schema.Version]edge{}}
	for _, item := range migrations {
		if item != nil && item.ToVersion() <= item.FromVersion() {
			return nil, schema.ErrMigrationCycle
		}
		if item == nil || item.FromVersion() < 1 || item.ToVersion() != item.FromVersion()+1 {
			return nil, fmt.Errorf("%w: migrations must be sequential", ErrInvalidMigration)
		}
		definition, ok := schemas.Definition(item.DocumentType())
		if !ok {
			return nil, fmt.Errorf("%w: unknown type %s", ErrInvalidMigration, item.DocumentType())
		}
		if item.ToVersion() > definition.CurrentVersion {
			return nil, fmt.Errorf("%w: edge beyond current version", ErrInvalidMigration)
		}
		if r.edges[item.DocumentType()] == nil {
			r.edges[item.DocumentType()] = map[schema.Version]edge{}
		}
		if _, exists := r.edges[item.DocumentType()][item.FromVersion()]; exists {
			return nil, schema.ErrDuplicateMigration
		}
		r.edges[item.DocumentType()][item.FromVersion()] = edge{item.FromVersion(), item.ToVersion(), item}
	}
	for _, documentType := range []schema.DocumentType{
		schema.DocumentCourseMetadata, schema.DocumentModuleMetadata, schema.DocumentCourseOverview, schema.DocumentModuleOverview,
		schema.DocumentSessionMetadata, schema.DocumentSessionNote, schema.DocumentTranscript, schema.DocumentRuntimeState,
	} {
		definition, ok := schemas.Definition(documentType)
		if !ok {
			continue
		}
		seen := map[schema.Version]bool{}
		for version := definition.MinimumVersion; version < definition.CurrentVersion; {
			if seen[version] {
				return nil, schema.ErrMigrationCycle
			}
			seen[version] = true
			next, exists := r.edges[documentType][version]
			if !exists {
				return nil, fmt.Errorf("%w: %s v%d", schema.ErrMissingMigrationPath, documentType, version)
			}
			version = next.to
		}
	}
	return r, nil
}

func (r *Registry) migration(documentType schema.DocumentType, from schema.Version) (Migration, bool) {
	edge, ok := r.edges[documentType][from]
	return edge.migration, ok
}

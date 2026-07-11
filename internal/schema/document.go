// Package schema defines versioned, content-preserving StudyPilot document contracts.
package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

type DocumentType string

const (
	DocumentCourseMetadata  DocumentType = "course_metadata"
	DocumentModuleMetadata  DocumentType = "module_metadata"
	DocumentCourseOverview  DocumentType = "course_overview"
	DocumentModuleOverview  DocumentType = "module_overview"
	DocumentSessionMetadata DocumentType = "session_metadata"
	DocumentSessionNote     DocumentType = "session_note"
	DocumentTranscript      DocumentType = "transcript"
	DocumentRuntimeState    DocumentType = "runtime_state"
)

type Version int

var (
	ErrInvalidDefinition        = errors.New("invalid schema definition")
	ErrDuplicateDefinition      = errors.New("duplicate schema definition")
	ErrDuplicateMigration       = errors.New("duplicate migration edge")
	ErrMissingMigrationPath     = errors.New("missing migration path")
	ErrMigrationCycle           = errors.New("migration cycle")
	ErrUnsupportedFutureVersion = errors.New("unsupported future schema version")
	ErrUnsupportedVersion       = errors.New("unsupported schema version")
	ErrUnknownDocument          = errors.New("unknown managed document")
	ErrMalformedDocument        = errors.New("malformed managed document")
	ErrDuplicateManagedKey      = errors.New("duplicate managed frontmatter key")
	ErrMalformedManagedRegion   = errors.New("malformed managed region")
)

type Format string

const (
	FormatJSON     Format = "json"
	FormatMarkdown Format = "markdown"
)

// Document carries bytes in memory. Reports must use hashes rather than Content.
type Document struct {
	Type    DocumentType
	Version Version
	Format  Format
	Content []byte
}

func DetectType(path string) (DocumentType, Format, error) {
	switch filepath.Base(path) {
	case ".studypilot-course.json":
		return DocumentCourseMetadata, FormatJSON, nil
	case ".studypilot-module.json":
		return DocumentModuleMetadata, FormatJSON, nil
	case ".studypilot-session.json":
		return DocumentSessionMetadata, FormatJSON, nil
	case ".studypilot-runtime.json":
		return DocumentRuntimeState, FormatJSON, nil
	case "Course Overview.md":
		return DocumentCourseOverview, FormatMarkdown, nil
	case "Module Overview.md":
		return DocumentModuleOverview, FormatMarkdown, nil
	case "Session.md":
		return DocumentSessionNote, FormatMarkdown, nil
	case "Transcript.md":
		return DocumentTranscript, FormatMarkdown, nil
	default:
		return "", "", ErrUnknownDocument
	}
}

func ParseDocument(path string, content []byte, definition Definition) (Document, error) {
	typeName, format, err := DetectType(path)
	if err != nil || typeName != definition.Type {
		return Document{}, ErrUnknownDocument
	}
	var version Version
	switch format {
	case FormatJSON:
		var envelope struct {
			SchemaVersion int `json:"schema_version"`
		}
		if err := json.Unmarshal(content, &envelope); err != nil || envelope.SchemaVersion < 1 {
			return Document{}, fmt.Errorf("%w: invalid JSON schema_version", ErrMalformedDocument)
		}
		version = Version(envelope.SchemaVersion)
	case FormatMarkdown:
		markdown, err := ParseMarkdown(content, definition.ManagedFields, definition.ManagedRegions)
		if err != nil {
			return Document{}, err
		}
		value, ok := markdown.Value("schema_version")
		if !ok {
			// Course and module overview files committed before schema migration
			// support are the implicit version-1 Markdown format.
			version = 1
			break
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsed < 1 {
			return Document{}, fmt.Errorf("%w: invalid schema_version", ErrMalformedDocument)
		}
		version = Version(parsed)
	}
	if version > definition.CurrentVersion {
		return Document{}, fmt.Errorf("%w: %s v%d", ErrUnsupportedFutureVersion, typeName, version)
	}
	if version < definition.MinimumVersion {
		return Document{}, fmt.Errorf("%w: %s v%d", ErrUnsupportedVersion, typeName, version)
	}
	document := Document{Type: typeName, Version: version, Format: format, Content: bytes.Clone(content)}
	if definition.Validate != nil {
		if err := definition.Validate(document); err != nil {
			return Document{}, fmt.Errorf("%w: %v", ErrMalformedDocument, err)
		}
	}
	return document, nil
}

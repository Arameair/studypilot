package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/Arameair/studypilot/internal/course"
)

// ScanIssueKind classifies why one session directory could not be loaded as a
// healthy record. Kinds are stable, UI-neutral strings.
type ScanIssueKind string

const (
	ScanIssueUnmanaged         ScanIssueKind = "unmanaged"
	ScanIssueMalformedMetadata ScanIssueKind = "malformed_metadata"
	ScanIssueMalformedRuntime  ScanIssueKind = "malformed_runtime"
	ScanIssueDuplicateNumber   ScanIssueKind = "duplicate_number"
	ScanIssueDuplicateID       ScanIssueKind = "duplicate_id"
	ScanIssueMissingRuntime    ScanIssueKind = "missing_runtime"
	ScanIssueIdentityMismatch  ScanIssueKind = "identity_mismatch"
	ScanIssueUnsafePath        ScanIssueKind = "unsafe_path"
	ScanIssueUnsupportedSchema ScanIssueKind = "unsupported_schema"
)

// ScanIssue names one problematic session directory beneath a module's
// Sessions directory. It carries no file contents; Path is the directory name
// relative to Sessions, and SafeName is a display-sanitized copy of that name.
type ScanIssue struct {
	Path     string
	Kind     ScanIssueKind
	Message  string
	SafeName string
}

// ScanResult is the tolerant, read-only view of a module's sessions. Healthy
// records are returned even when sibling directories are malformed; every
// problematic directory is reported as an issue. It performs no repair and
// produces no mutation authority for malformed entries.
type ScanResult struct {
	Records []Record
	Issues  []ScanIssue
}

// Scan reads every session directory beneath a module tolerantly. Unlike the
// write-path scan, one malformed, unmanaged, duplicated, or unsafe sibling does
// not fail the whole module: healthy sessions are still returned and each broken
// directory is reported. Symlinks are never followed and unrelated regular
// files are ignored. It is strictly read-only.
func (r *Repository) Scan(ctx context.Context, courseID, moduleID string) (ScanResult, error) {
	if err := ctx.Err(); err != nil {
		return ScanResult{}, err
	}
	parentCourse, parentModule, err := r.resolveModule(courseID, moduleID)
	if err != nil {
		return ScanResult{}, err
	}
	sessionsRoot := filepath.Join(parentModule.Path, "Sessions")
	entries, err := os.ReadDir(sessionsRoot)
	if err != nil {
		return ScanResult{}, err
	}

	var issues []ScanIssue
	type candidate struct {
		name   string
		record Record
	}
	var candidates []candidate
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return ScanResult{}, err
		}
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 {
			issues = append(issues, scanIssue(name, ScanIssueUnsafePath, "session path is a symlink"))
			continue
		}
		if !entry.IsDir() {
			continue
		}
		sessionRoot := filepath.Join(sessionsRoot, name)
		record, kind, message := r.scanEntry(ctx, sessionRoot, parentCourse, parentModule)
		if kind != "" {
			issues = append(issues, scanIssue(name, kind, message))
			continue
		}
		candidates = append(candidates, candidate{name: name, record: record})
	}

	// Duplicate numbers or IDs are ambiguous: report every affected directory
	// and never return an affected entry as a healthy record.
	numberGroups := map[int][]int{}
	idGroups := map[string][]int{}
	for i, c := range candidates {
		numberGroups[c.record.Metadata.Number] = append(numberGroups[c.record.Metadata.Number], i)
		idGroups[c.record.Metadata.ID] = append(idGroups[c.record.Metadata.ID], i)
	}
	ambiguous := make([]bool, len(candidates))
	for _, indexes := range numberGroups {
		if len(indexes) > 1 {
			for _, i := range indexes {
				issues = append(issues, scanIssue(candidates[i].name, ScanIssueDuplicateNumber, "duplicate session number"))
				ambiguous[i] = true
			}
		}
	}
	for _, indexes := range idGroups {
		if len(indexes) > 1 {
			for _, i := range indexes {
				issues = append(issues, scanIssue(candidates[i].name, ScanIssueDuplicateID, "duplicate session id"))
				ambiguous[i] = true
			}
		}
	}

	result := ScanResult{}
	for i, c := range candidates {
		if ambiguous[i] {
			continue
		}
		result.Records = append(result.Records, c.record)
	}
	result.Issues = issues
	sort.Slice(result.Records, func(i, j int) bool {
		if result.Records[i].Metadata.Number != result.Records[j].Metadata.Number {
			return result.Records[i].Metadata.Number < result.Records[j].Metadata.Number
		}
		return result.Records[i].Metadata.ID < result.Records[j].Metadata.ID
	})
	sort.Slice(result.Issues, func(i, j int) bool {
		if result.Issues[i].Path != result.Issues[j].Path {
			return result.Issues[i].Path < result.Issues[j].Path
		}
		return result.Issues[i].Kind < result.Issues[j].Kind
	})
	return result, nil
}

var errNotRegularFile = errors.New("not a regular file")

// scanEntry classifies one session directory without failing the caller. A
// non-empty ScanIssueKind means the directory is problematic; an empty kind
// means the returned Record is healthy. Classification reads files directly and
// read-only so that a malformed managed session is distinguished from an
// unmanaged directory; only a confirmed-healthy entry is re-read through the
// authoritative Load path. It never repairs or writes.
func (r *Repository) scanEntry(ctx context.Context, sessionRoot string, parentCourse course.CourseRecord, parentModule course.ModuleRecord) (Record, ScanIssueKind, string) {
	metadataBytes, err := safeReadFile(filepath.Join(sessionRoot, sessionMetadataName))
	if err != nil {
		switch {
		case os.IsNotExist(err):
			return Record{}, ScanIssueUnmanaged, "session directory has no session metadata"
		case errors.Is(err, errNotRegularFile):
			return Record{}, ScanIssueUnsafePath, "session metadata is not a regular file"
		default:
			return Record{}, ScanIssueMalformedMetadata, "session metadata could not be read"
		}
	}
	if version, ok := schemaVersionOf(metadataBytes); ok && version != MetadataSchemaVersion {
		return Record{}, ScanIssueUnsupportedSchema, "unsupported session metadata schema"
	}
	metadata, err := decodeMetadata(metadataBytes)
	if err != nil {
		return Record{}, ScanIssueMalformedMetadata, "session metadata is malformed"
	}
	if metadata.CourseID != parentCourse.Metadata.ID || metadata.ModuleID != parentModule.Metadata.ID {
		return Record{}, ScanIssueIdentityMismatch, "session identity does not match module"
	}
	if err := metadata.Validate(parentCourse.Metadata.ID, parentModule.Metadata.ID, filepath.Base(sessionRoot)); err != nil {
		return Record{}, ScanIssueIdentityMismatch, "session directory name does not match identity"
	}
	runtimeBytes, err := safeReadFile(filepath.Join(sessionRoot, runtimeStateName))
	if err != nil {
		switch {
		case os.IsNotExist(err):
			return Record{}, ScanIssueMissingRuntime, "session has no runtime state"
		case errors.Is(err, errNotRegularFile):
			return Record{}, ScanIssueUnsafePath, "runtime state is not a regular file"
		default:
			return Record{}, ScanIssueMalformedRuntime, "runtime state could not be read"
		}
	}
	if version, ok := schemaVersionOf(runtimeBytes); ok && version != RuntimeSchemaVersion {
		return Record{}, ScanIssueUnsupportedSchema, "unsupported runtime state schema"
	}
	runtimeState, err := decodeRuntime(runtimeBytes)
	if err != nil {
		return Record{}, ScanIssueMalformedRuntime, "runtime state is malformed"
	}
	if err := runtimeState.Validate(metadata); err != nil {
		return Record{}, ScanIssueMalformedRuntime, "runtime state is invalid for this session"
	}
	if err := validateRuntimeContext(runtimeState, r.paths, parentCourse, parentModule, metadata); err != nil {
		return Record{}, ScanIssueIdentityMismatch, "runtime context does not match module"
	}
	// The entry is well-formed; obtain an authoritative record (with the exact
	// mutation-expected state) through the same read path writers use.
	record, err := r.Load(ctx, sessionRoot)
	if err != nil {
		return Record{}, ScanIssueUnmanaged, "session directory is not a managed session"
	}
	return record, "", ""
}

// safeReadFile reads a regular file, refusing to follow symlinks or read
// non-regular targets. A missing file returns an os.IsNotExist error.
func safeReadFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errNotRegularFile
	}
	return os.ReadFile(path)
}

// schemaVersionOf reads only the schema_version field so malformed content can
// be distinguished from a well-formed but unsupported schema version.
func schemaVersionOf(content []byte) (int, bool) {
	var probe struct {
		SchemaVersion int `json:"schema_version"`
	}
	if json.Unmarshal(content, &probe) != nil {
		return 0, false
	}
	return probe.SchemaVersion, true
}

func scanIssue(name string, kind ScanIssueKind, message string) ScanIssue {
	return ScanIssue{Path: name, Kind: kind, Message: message, SafeName: safeName(name)}
}

// safeName strips control characters from a directory name so a hostile or
// corrupt name cannot inject escape sequences into rendered output.
func safeName(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
}

package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/filesystem"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
	"github.com/Arameair/studypilot/internal/workspace"
)

const (
	sessionMetadataName = ".studypilot-session.json"
	runtimeStateName    = ".studypilot-runtime.json"
)

type Clock func() time.Time
type IDGenerator func() (string, error)

type mutationEngine interface {
	Read(context.Context, filesystem.MutationAuthority, string) (filesystem.ManagedFile, error)
	Inspect(context.Context, filesystem.MutationAuthority, string) (filesystem.ManagedFileState, error)
	Apply(context.Context, filesystem.Mutation) (filesystem.MutationResult, error)
}

type Repository struct {
	paths      workspace.Paths
	mutations  mutationEngine
	clock      Clock
	generateID IDGenerator
}

func NewRepository(paths workspace.Paths, clock Clock, generateID IDGenerator) (*Repository, error) {
	if err := paths.Validate(); err != nil {
		return nil, err
	}
	if clock == nil {
		clock = time.Now
	}
	if generateID == nil {
		generateID = defaultID
	}
	return &Repository{paths: paths, mutations: filesystem.NewMutationExecutor(), clock: clock, generateID: generateID}, nil
}

type Record struct {
	Root              string
	Metadata          Metadata
	Runtime           RuntimeState
	MetadataHash      string
	RuntimeHash       string
	DurabilityWarning bool
	runtimeExpected   filesystem.ExpectedState
}

type createSpec struct {
	metadata                  *Metadata
	courseID, moduleID, title string
	snapshot                  *studyruntime.Snapshot
	requestedID               string
}

// Create allocates immutable identity and the next module-local number.
func (r *Repository) Create(ctx context.Context, courseID, moduleID, title string, snapshot *studyruntime.Snapshot) (Record, error) {
	return r.create(ctx, createSpec{courseID: courseID, moduleID: moduleID, title: title, snapshot: snapshot})
}

// CreateWithID allocates a number while using a caller-provided immutable ID.
func (r *Repository) CreateWithID(ctx context.Context, courseID, moduleID, title, id string, snapshot *studyruntime.Snapshot) (Record, error) {
	return r.create(ctx, createSpec{courseID: courseID, moduleID: moduleID, title: title, requestedID: id, snapshot: snapshot})
}

// CreateWithMetadata supports idempotent retries with a previously allocated identity.
func (r *Repository) CreateWithMetadata(ctx context.Context, metadata Metadata, snapshot *studyruntime.Snapshot) (Record, error) {
	return r.create(ctx, createSpec{metadata: &metadata, courseID: metadata.CourseID, moduleID: metadata.ModuleID, title: metadata.DisplayName, snapshot: snapshot})
}

func (r *Repository) create(ctx context.Context, spec createSpec) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	parentCourse, parentModule, err := r.resolveModule(spec.courseID, spec.moduleID)
	if err != nil {
		return Record{}, err
	}
	sessionsRoot := filepath.Join(parentModule.Path, "Sessions")
	release, err := acquireCreationLock(sessionsRoot)
	if err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrSessionConflict, err)
	}
	defer release()
	existing, err := r.scanMetadata(ctx, sessionsRoot, parentModule.Path, parentCourse.Metadata.ID, parentModule.Metadata.ID)
	if err != nil {
		return Record{}, err
	}

	metadata := Metadata{}
	if spec.metadata != nil {
		metadata = *spec.metadata
		if err := metadata.Validate(parentCourse.Metadata.ID, parentModule.Metadata.ID, metadata.DirectoryName); err != nil {
			return Record{}, err
		}
		for _, found := range existing {
			if found.Number == metadata.Number && found.ID != metadata.ID {
				return Record{}, ErrDuplicateNumber
			}
			if found.ID == metadata.ID {
				if found != metadata {
					return Record{}, ErrSessionConflict
				}
				loaded, err := r.Load(ctx, filepath.Join(sessionsRoot, found.DirectoryName))
				if err != nil {
					return Record{}, err
				}
				expectedSnapshot := initialSnapshot(r.paths, parentCourse, parentModule, metadata, metadata.CreatedAt)
				if spec.snapshot != nil {
					expectedSnapshot = *spec.snapshot
				}
				expectedRuntime := RuntimeState{SchemaVersion: RuntimeSchemaVersion, SessionID: metadata.ID, Revision: 1, Snapshot: expectedSnapshot}
				if !bytes.Equal(mustEncode(loaded.Runtime), mustEncode(expectedRuntime)) {
					return Record{}, fmt.Errorf("%w: existing runtime differs from creation request", ErrSessionConflict)
				}
				return loaded, nil
			}
		}
	} else {
		if spec.requestedID != "" {
			for _, found := range existing {
				if found.ID == spec.requestedID {
					if found.DisplayName != strings.TrimSpace(spec.title) {
						return Record{}, ErrSessionConflict
					}
					return r.Load(ctx, filepath.Join(sessionsRoot, found.DirectoryName))
				}
			}
		}
		number, err := nextNumber(existing)
		if err != nil {
			return Record{}, err
		}
		directory, slug, err := sessionNames(number, spec.title)
		if err != nil {
			return Record{}, err
		}
		id := spec.requestedID
		if id == "" {
			id, err = r.generateID()
			if err != nil {
				return Record{}, err
			}
		}
		metadata = Metadata{SchemaVersion: MetadataSchemaVersion, ID: id, CourseID: parentCourse.Metadata.ID, ModuleID: parentModule.Metadata.ID, Number: number, DisplayName: strings.TrimSpace(spec.title), Slug: slug, DirectoryName: directory, CreatedAt: r.clock().UTC()}
		if err := metadata.Validate(parentCourse.Metadata.ID, parentModule.Metadata.ID, directory); err != nil {
			return Record{}, err
		}
	}

	snapshot := initialSnapshot(r.paths, parentCourse, parentModule, metadata, metadata.CreatedAt)
	if spec.snapshot != nil {
		snapshot = *spec.snapshot
	}
	runtimeState := RuntimeState{SchemaVersion: RuntimeSchemaVersion, SessionID: metadata.ID, Revision: 1, Snapshot: snapshot}
	if err := runtimeState.Validate(metadata); err != nil {
		return Record{}, err
	}
	if err := validateRuntimeContext(runtimeState, r.paths, parentCourse, parentModule, metadata); err != nil {
		return Record{}, err
	}
	metadataBytes, err := encodeJSON(metadata)
	if err != nil {
		return Record{}, err
	}
	runtimeBytes, err := encodeJSON(runtimeState)
	if err != nil {
		return Record{}, err
	}
	sessionRoot := filepath.Join(sessionsRoot, metadata.DirectoryName)
	operations := []filesystem.Operation{
		{Kind: filesystem.OperationCreateDirectory, Path: parentModule.Path},
		{Kind: filesystem.OperationCreateDirectory, Path: sessionsRoot},
		{Kind: filesystem.OperationCreateDirectory, Path: sessionRoot},
	}
	for _, relative := range []string{"Segments", "Notes", "Assets", filepath.Join("Assets", "Screenshots"), filepath.Join("Assets", "Audio"), filepath.Join("Assets", "Video"), filepath.Join("Assets", "Documents")} {
		operations = append(operations, filesystem.Operation{Kind: filesystem.OperationCreateDirectory, Path: filepath.Join(sessionRoot, relative)})
	}
	operations = append(operations,
		filesystem.Operation{Kind: filesystem.OperationCreateFile, Path: filepath.Join(sessionRoot, sessionMetadataName), Content: string(metadataBytes)},
		filesystem.Operation{Kind: filesystem.OperationCreateFile, Path: filepath.Join(sessionRoot, runtimeStateName), Content: string(runtimeBytes)},
	)
	plan, err := filesystem.NewModuleScopedPlan(r.paths, parentCourse.Path, parentModule.Path, operations)
	if err != nil {
		return Record{}, err
	}
	report, err := filesystem.Execute(plan)
	if err != nil {
		return Record{}, err
	}
	if report.HasConflicts() {
		record, loadErr := r.Load(ctx, sessionRoot)
		if loadErr == nil && record.Metadata == metadata && record.Runtime.Revision == 1 && bytes.Equal(runtimeBytes, mustEncode(record.Runtime)) {
			return record, nil
		}
		return Record{}, fmt.Errorf("%w: session creation encountered existing content", ErrSessionConflict)
	}
	return r.Load(ctx, sessionRoot)
}

func (r *Repository) Load(ctx context.Context, sessionRoot string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	moduleRoot := filepath.Dir(filepath.Dir(filepath.Clean(sessionRoot)))
	authority, err := filesystem.NewSessionMutationAuthority(r.paths, moduleRoot, sessionRoot)
	if err != nil {
		return Record{}, classifyLoadError(err)
	}
	metadataFile, err := r.mutations.Read(ctx, authority, filepath.Join(sessionRoot, sessionMetadataName))
	if err != nil {
		return Record{}, classifyLoadError(err)
	}
	metadata, err := decodeMetadata(metadataFile.Content)
	if err != nil {
		return Record{}, err
	}
	parentCourse, parentModule, err := r.resolveModule(metadata.CourseID, metadata.ModuleID)
	if err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrIdentityMismatch, err)
	}
	if parentModule.Path != moduleRoot || metadata.Validate(parentCourse.Metadata.ID, parentModule.Metadata.ID, filepath.Base(sessionRoot)) != nil {
		return Record{}, ErrIdentityMismatch
	}
	runtimeFile, err := r.mutations.Read(ctx, authority, filepath.Join(sessionRoot, runtimeStateName))
	if err != nil {
		return Record{}, classifyLoadError(err)
	}
	runtimeState, err := decodeRuntime(runtimeFile.Content)
	if err != nil {
		return Record{}, err
	}
	if err := runtimeState.Validate(metadata); err != nil {
		return Record{}, err
	}
	if err := validateRuntimeContext(runtimeState, r.paths, parentCourse, parentModule, metadata); err != nil {
		return Record{}, err
	}
	return Record{Root: filepath.Clean(sessionRoot), Metadata: metadata, Runtime: runtimeState, MetadataHash: metadataFile.SHA256, RuntimeHash: runtimeFile.SHA256, runtimeExpected: runtimeFile.ExpectedState()}, nil
}

// Find resolves a session by immutable ID, exact title, slug, or number within
// one immutable course/module context.
func (r *Repository) Find(ctx context.Context, courseID, moduleID, reference string) (Record, error) {
	root, _, err := r.Locate(ctx, courseID, moduleID, reference)
	if err != nil {
		return Record{}, err
	}
	return r.Load(ctx, root)
}

// Locate resolves metadata without requiring a valid runtime file.
func (r *Repository) Locate(ctx context.Context, courseID, moduleID, reference string) (string, Metadata, error) {
	parentCourse, parentModule, err := r.resolveModule(courseID, moduleID)
	if err != nil {
		return "", Metadata{}, err
	}
	metadata, err := r.scanMetadata(ctx, filepath.Join(parentModule.Path, "Sessions"), parentModule.Path, parentCourse.Metadata.ID, parentModule.Metadata.ID)
	if err != nil {
		return "", Metadata{}, err
	}
	var matches []Metadata
	for _, candidate := range metadata {
		if reference == candidate.ID || reference == candidate.DisplayName || reference == candidate.Slug || reference == strconv.Itoa(candidate.Number) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return "", Metadata{}, ErrSessionNotFound
	}
	if len(matches) != 1 {
		return "", Metadata{}, ErrAmbiguousSession
	}
	return filepath.Join(parentModule.Path, "Sessions", matches[0].DirectoryName), matches[0], nil
}

type RuntimeUpdate struct {
	ExpectedRevision uint64
	Next             studyruntime.Snapshot
}

func (r *Repository) UpdateRuntime(ctx context.Context, record Record, update RuntimeUpdate) (Record, error) {
	current, err := r.Load(ctx, record.Root)
	if err != nil {
		return Record{}, err
	}
	if current.Metadata.ID != record.Metadata.ID || current.MetadataHash != record.MetadataHash {
		return Record{}, ErrIdentityMismatch
	}
	if update.ExpectedRevision != current.Runtime.Revision || record.RuntimeHash != current.RuntimeHash {
		return Record{}, fmt.Errorf("%w: stale revision or content hash", ErrSessionConflict)
	}
	if err := update.Next.Validate(); err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrMalformedState, err)
	}
	proposed := RuntimeState{SchemaVersion: RuntimeSchemaVersion, SessionID: current.Metadata.ID, Revision: current.Runtime.Revision + 1, Snapshot: update.Next}
	if err := proposed.Validate(current.Metadata); err != nil {
		return Record{}, err
	}
	parentCourse, parentModule, err := r.resolveModule(current.Metadata.CourseID, current.Metadata.ModuleID)
	if err != nil {
		return Record{}, err
	}
	if err := validateRuntimeContext(proposed, r.paths, parentCourse, parentModule, current.Metadata); err != nil {
		return Record{}, err
	}
	if proposed.Snapshot.UpdatedAt.Before(current.Runtime.Snapshot.UpdatedAt) {
		return Record{}, fmt.Errorf("%w: update time moved backwards", ErrInvalidTransition)
	}
	if err := validateSnapshotTransitions(current.Runtime.Snapshot, proposed.Snapshot); err != nil {
		return Record{}, err
	}
	content, err := encodeJSON(proposed)
	if err != nil {
		return Record{}, err
	}
	moduleRoot := filepath.Dir(filepath.Dir(current.Root))
	authority, err := filesystem.NewSessionMutationAuthority(r.paths, moduleRoot, current.Root)
	if err != nil {
		return Record{}, err
	}
	mutation, err := filesystem.NewMutation(authority, filepath.Join(current.Root, runtimeStateName), current.runtimeExpected, content, 0o640)
	if err != nil {
		return Record{}, err
	}
	result, applyErr := r.mutations.Apply(ctx, mutation)
	if applyErr != nil {
		if errors.Is(applyErr, filesystem.ErrStateMismatch) {
			return Record{}, fmt.Errorf("%w: %w", ErrSessionConflict, applyErr)
		}
		var mutationErr *filesystem.MutationError
		if !errors.As(applyErr, &mutationErr) || !mutationErr.Replaced {
			return Record{}, applyErr
		}
		state, inspectErr := r.mutations.Inspect(context.Background(), authority, filepath.Join(current.Root, runtimeStateName))
		if inspectErr != nil || state.SHA256 != result.CurrentHash {
			return Record{}, errors.Join(applyErr, inspectErr)
		}
		installed, loadErr := r.Load(context.Background(), current.Root)
		if loadErr != nil {
			return Record{}, errors.Join(applyErr, loadErr)
		}
		installed.DurabilityWarning = true
		return installed, nil
	}
	return r.Load(ctx, current.Root)
}

func (r *Repository) resolveModule(courseID, moduleID string) (course.CourseRecord, course.ModuleRecord, error) {
	parentCourse, err := course.FindCourse(r.paths, courseID)
	if err != nil {
		return course.CourseRecord{}, course.ModuleRecord{}, fmt.Errorf("%w: %v", ErrInvalidMetadata, err)
	}
	parentModule, err := course.FindModule(parentCourse, moduleID)
	if err != nil {
		return course.CourseRecord{}, course.ModuleRecord{}, fmt.Errorf("%w: %v", ErrInvalidMetadata, err)
	}
	return parentCourse, parentModule, nil
}

func initialSnapshot(paths workspace.Paths, parentCourse course.CourseRecord, parentModule course.ModuleRecord, metadata Metadata, now time.Time) studyruntime.Snapshot {
	return studyruntime.Snapshot{SchemaVersion: studyruntime.SnapshotSchemaVersion, WorkspaceRoot: paths.Root, CourseID: metadata.CourseID, CourseName: parentCourse.Metadata.DisplayName, ModuleID: metadata.ModuleID, ModuleNumber: parentModule.Metadata.Number, ModuleName: parentModule.Metadata.DisplayName, SessionID: metadata.ID, SessionNumber: metadata.Number, SessionTitle: metadata.DisplayName, SessionStatus: studyruntime.SessionStatusPlanned, CaptureStatus: studyruntime.CaptureStatusUnavailable, TranscriptionStatus: studyruntime.TranscriptionStatusNotStarted, FilesystemStatus: studyruntime.FilesystemStatusReady, PublicationStatus: studyruntime.PublicationStatusPrivate, UpdatedAt: now}
}

func validateRuntimeContext(state RuntimeState, paths workspace.Paths, parentCourse course.CourseRecord, parentModule course.ModuleRecord, metadata Metadata) error {
	snapshot := state.Snapshot
	if snapshot.WorkspaceRoot != paths.Root || snapshot.CourseID != parentCourse.Metadata.ID || snapshot.CourseName != parentCourse.Metadata.DisplayName ||
		snapshot.ModuleID != parentModule.Metadata.ID || snapshot.ModuleNumber != parentModule.Metadata.Number || snapshot.ModuleName != parentModule.Metadata.DisplayName ||
		snapshot.SessionID != metadata.ID || snapshot.SessionNumber != metadata.Number || snapshot.SessionTitle != metadata.DisplayName {
		return ErrIdentityMismatch
	}
	return nil
}

func (r *Repository) scanMetadata(ctx context.Context, sessionsRoot, moduleRoot, courseID, moduleID string) ([]Metadata, error) {
	entries, err := os.ReadDir(sessionsRoot)
	if err != nil {
		return nil, err
	}
	result := make([]Metadata, 0, len(entries))
	numbers := map[int]bool{}
	ids := map[string]bool{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, filesystem.ErrUnsafePath
		}
		if !entry.IsDir() {
			continue
		}
		sessionRoot := filepath.Join(sessionsRoot, entry.Name())
		authority, err := filesystem.NewSessionMutationAuthority(r.paths, moduleRoot, sessionRoot)
		if err != nil {
			return nil, fmt.Errorf("%w: unmanaged or unsafe session directory %s: %v", ErrMalformedState, entry.Name(), err)
		}
		managed, err := r.mutations.Read(ctx, authority, filepath.Join(sessionRoot, sessionMetadataName))
		if err != nil {
			return nil, err
		}
		metadata, err := decodeMetadata(managed.Content)
		if err != nil {
			return nil, err
		}
		if err := metadata.Validate(courseID, moduleID, entry.Name()); err != nil {
			return nil, err
		}
		if numbers[metadata.Number] {
			return nil, ErrDuplicateNumber
		}
		if ids[metadata.ID] {
			return nil, ErrSessionConflict
		}
		numbers[metadata.Number], ids[metadata.ID] = true, true
		result = append(result, metadata)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Number != result[j].Number {
			return result[i].Number < result[j].Number
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func nextNumber(records []Metadata) (int, error) {
	max := 0
	seen := map[int]bool{}
	for _, record := range records {
		if record.Number <= 0 || seen[record.Number] {
			return 0, ErrDuplicateNumber
		}
		seen[record.Number] = true
		if record.Number > max {
			max = record.Number
		}
	}
	return max + 1, nil
}

func acquireCreationLock(sessionsRoot string) (func(), error) {
	info, err := os.Lstat(sessionsRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, filesystem.ErrUnsafePath
	}
	path := filepath.Join(sessionsRoot, ".studypilot-session-create.lock")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
}

func encodeJSON(value any) ([]byte, error) {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}
func mustEncode(value any) []byte { content, _ := encodeJSON(value); return content }
func decodeMetadata(content []byte) (Metadata, error) {
	var value Metadata
	if err := json.Unmarshal(content, &value); err != nil {
		return Metadata{}, fmt.Errorf("%w: invalid metadata JSON", ErrMalformedState)
	}
	if value.SchemaVersion != MetadataSchemaVersion {
		return Metadata{}, fmt.Errorf("%w: unsupported metadata schema", ErrMalformedState)
	}
	if err := value.Validate(value.CourseID, value.ModuleID, value.DirectoryName); err != nil {
		return Metadata{}, err
	}
	return value, nil
}
func decodeRuntime(content []byte) (RuntimeState, error) {
	var value RuntimeState
	if err := json.Unmarshal(content, &value); err != nil {
		return RuntimeState{}, fmt.Errorf("%w: invalid runtime JSON", ErrMalformedState)
	}
	if value.SchemaVersion != RuntimeSchemaVersion {
		return RuntimeState{}, fmt.Errorf("%w: unsupported runtime schema", ErrMalformedState)
	}
	return value, nil
}
func classifyLoadError(err error) error {
	if errors.Is(err, filesystem.ErrTargetNotFound) {
		return fmt.Errorf("%w: %v", ErrSessionNotFound, err)
	}
	return err
}
func defaultID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "session-" + hex.EncodeToString(value[:]), nil
}

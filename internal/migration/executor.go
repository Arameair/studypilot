package migration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/schema"
	"github.com/Arameair/studypilot/internal/workspace"
)

var ErrDurabilityUncertain = errors.New("migration replacement occurred but durability is uncertain")

type ApplyResult struct {
	Applied    bool
	BackupPath string
	Record     HistoryRecord
	Mutation   filesystem.MutationResult
}

type mutationEngine interface {
	Read(context.Context, filesystem.MutationAuthority, string) (filesystem.ManagedFile, error)
	Apply(context.Context, filesystem.Mutation) (filesystem.MutationResult, error)
	Inspect(context.Context, filesystem.MutationAuthority, string) (filesystem.ManagedFileState, error)
}

type Executor struct {
	schemas   *schema.Registry
	mutations mutationEngine
	now       func() time.Time
}

func NewExecutor(schemas *schema.Registry) (*Executor, error) {
	if schemas == nil {
		return nil, ErrInvalidMigration
	}
	return &Executor{schemas: schemas, mutations: filesystem.NewMutationExecutor(), now: time.Now}, nil
}

// Apply executes an already planned private metadata migration. Review plans
// require explicit approval; manual and public plans are never written.
func (e *Executor) Apply(ctx context.Context, paths workspace.Paths, plan Plan, approveReview bool) (ApplyResult, error) {
	if err := paths.Validate(); err != nil {
		return ApplyResult{}, err
	}
	if plan.Status == StatusManual || plan.Safety == SafetyManual {
		return ApplyResult{}, ErrManualMigration
	}
	if plan.Safety == SafetyReview && !approveReview {
		return ApplyResult{}, ErrManualMigration
	}
	if !within(paths.Private, plan.Path) || within(paths.Portfolio, plan.Path) {
		return ApplyResult{}, ErrPublicMigration
	}
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, err
	}
	if len(plan.BeforeHash) != 64 || len(plan.AfterHash) != 64 || hash(plan.input) != plan.BeforeHash || hash(plan.output) != plan.AfterHash {
		return ApplyResult{}, ErrInvalidMigration
	}
	if plan.Status == StatusCurrent && plan.BeforeHash == plan.AfterHash && plan.ToVersion == plan.FromVersion {
		return ApplyResult{}, nil
	}
	if plan.ToVersion <= plan.FromVersion {
		return ApplyResult{}, ErrInvalidMigration
	}

	authority, err := authorityFor(paths, plan)
	if err != nil {
		return ApplyResult{}, err
	}
	managed, err := e.mutations.Read(ctx, authority, plan.Path)
	if err != nil {
		return ApplyResult{}, err
	}
	if managed.SHA256 != plan.BeforeHash {
		return ApplyResult{}, ErrStalePlan
	}
	definition, ok := e.schemas.Definition(plan.DocumentType)
	if !ok {
		return ApplyResult{}, schema.ErrUnknownDocument
	}
	if _, err := schema.ParseDocument(plan.Path, plan.output, definition); err != nil {
		return ApplyResult{}, fmt.Errorf("validate migration output: %w", err)
	}

	migrationID := fmt.Sprintf("%s-v%d-v%d-%s", plan.DocumentType, plan.FromVersion, plan.ToVersion, plan.BeforeHash[:12])
	backupPath, err := createBackup(paths, migrationID, plan.Path, managed.Content)
	if err != nil {
		return ApplyResult{}, err
	}
	result := ApplyResult{BackupPath: backupPath}
	mutation, err := filesystem.NewMutation(authority, plan.Path, managed.ExpectedState(), plan.output, managed.Mode)
	if err != nil {
		return result, err
	}
	mutationResult, applyErr := e.mutations.Apply(ctx, mutation)
	result.Mutation = mutationResult

	reconciled := false
	if applyErr != nil {
		var mutationErr *filesystem.MutationError
		if !errors.As(applyErr, &mutationErr) || !mutationErr.Replaced {
			return result, applyErr
		}
		state, inspectErr := e.mutations.Inspect(context.Background(), authority, plan.Path)
		if inspectErr != nil || state.SHA256 != plan.AfterHash {
			return result, &ApplyError{Stage: "reconciliation", Replaced: true, Cause: errors.Join(ErrDurabilityUncertain, applyErr, inspectErr)}
		}
		reconciled = true
	}
	installed, err := e.mutations.Read(context.Background(), authority, plan.Path)
	if err != nil {
		return result, &ApplyError{Stage: "validation", Replaced: true, Cause: err}
	}
	if installed.SHA256 != plan.AfterHash {
		return result, &ApplyError{Stage: "validation", Replaced: true, Cause: ErrStalePlan}
	}
	if _, err := schema.ParseDocument(plan.Path, installed.Content, definition); err != nil {
		return result, &ApplyError{Stage: "validation", Replaced: true, Cause: fmt.Errorf("validate installed migration: %w", err)}
	}

	record := HistoryRecord{SchemaVersion: 1, MigrationID: migrationID, DocumentType: plan.DocumentType, Path: relativePrivatePath(paths, plan.Path), FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, BeforeHash: plan.BeforeHash, AfterHash: plan.AfterHash, AppliedAt: e.now().UTC(), Result: "applied"}
	if reconciled {
		record.Result = "applied_directory_sync_uncertain"
	}
	if err := writeHistoryRecord(paths, migrationID, record); err != nil {
		return result, &ApplyError{Stage: "history", Replaced: true, Cause: err}
	}
	result.Applied, result.Record = true, record
	if reconciled {
		return result, &ApplyError{Stage: "directory_sync", Replaced: true, Cause: errors.Join(ErrDurabilityUncertain, applyErr)}
	}
	return result, nil
}

func authorityFor(paths workspace.Paths, plan Plan) (filesystem.MutationAuthority, error) {
	switch plan.DocumentType {
	case schema.DocumentCourseMetadata:
		return filesystem.NewCourseMutationAuthority(paths, filepath.Dir(plan.Path))
	case schema.DocumentModuleMetadata:
		moduleRoot := filepath.Dir(plan.Path)
		courseRoot := filepath.Dir(filepath.Dir(moduleRoot))
		return filesystem.NewModuleMutationAuthority(paths, courseRoot, moduleRoot)
	case schema.DocumentRuntimeState:
		if within(paths.Private, plan.Path) {
			sessionRoot := filepath.Dir(plan.Path)
			moduleRoot := filepath.Dir(filepath.Dir(sessionRoot))
			return filesystem.NewSessionMutationAuthority(paths, moduleRoot, sessionRoot)
		}
		return filesystem.NewWorkspaceMutationAuthority(paths)
	default:
		return filesystem.MutationAuthority{}, filesystem.ErrUnmanagedTarget
	}
}

func createBackup(paths workspace.Paths, migrationID, source string, content []byte) (string, error) {
	relative := relativePrivatePath(paths, source)
	if relative == "" || strings.HasPrefix(relative, "..") {
		return "", ErrPublicMigration
	}
	root := filepath.Join(paths.Private, ".studypilot", "migrations", "backups", migrationID)
	target := filepath.Join(root, relative)
	if err := safeMkdirAll(paths.Private, filepath.Dir(target), 0o700); err != nil {
		return "", err
	}
	if err := createExclusiveSyncedFile(target, content, 0o600); err != nil {
		return "", err
	}
	return target, nil
}

func writeHistoryRecord(paths workspace.Paths, migrationID string, record HistoryRecord) error {
	directory := filepath.Join(paths.Private, ".studypilot", "migrations", "records", migrationID)
	if err := safeMkdirAll(paths.Private, directory, 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return createExclusiveSyncedFile(filepath.Join(directory, "record.json"), content, 0o600)
}

func createExclusiveSyncedFile(path string, content []byte, mode fs.FileMode) (returnErr error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	remove := true
	closed := false
	defer func() {
		if !closed {
			if closeErr := file.Close(); returnErr == nil && closeErr != nil {
				returnErr = closeErr
			}
		}
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	remove = false
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func safeMkdirAll(root, target string, mode fs.FileMode) error {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return filesystem.ErrUnsafePath
	}
	current := filepath.Clean(root)
	if info, err := os.Lstat(current); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return filesystem.ErrUnsafePath
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.Mkdir(current, mode); err != nil {
				return err
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return filesystem.ErrUnsafePath
		}
	}
	return nil
}

func relativePrivatePath(paths workspace.Paths, path string) string {
	relative, err := filepath.Rel(paths.Private, path)
	if err != nil {
		return ""
	}
	return relative
}

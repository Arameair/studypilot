package filesystem

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// MutationResult describes a successfully published replacement.
type MutationResult struct {
	Path         string
	PreviousHash string
	CurrentHash  string
	BytesWritten int64
	Mode         fs.FileMode
}

type temporaryFile interface {
	io.Writer
	Chmod(fs.FileMode) error
	Sync() error
	Close() error
	Name() string
}

type syncedDirectory interface {
	Sync() error
	Close() error
}

type mutationOperations struct {
	createTemp func(string, string) (temporaryFile, error)
	rename     func(string, string) error
	remove     func(string) error
	openDir    func(string) (syncedDirectory, error)
}

func operatingSystemMutationOperations() mutationOperations {
	return mutationOperations{
		createTemp: func(dir, pattern string) (temporaryFile, error) { return os.CreateTemp(dir, pattern) },
		rename:     os.Rename,
		remove:     os.Remove,
		openDir: func(path string) (syncedDirectory, error) {
			return os.Open(path)
		},
	}
}

type pathLock struct {
	token chan struct{}
	refs  int
}

type pathLockManager struct {
	mu    sync.Mutex
	locks map[string]*pathLock
}

func newPathLockManager() *pathLockManager {
	return &pathLockManager{locks: make(map[string]*pathLock)}
}

func (m *pathLockManager) acquire(ctx context.Context, path string) (func(), error) {
	m.mu.Lock()
	lock := m.locks[path]
	if lock == nil {
		lock = &pathLock{token: make(chan struct{}, 1)}
		lock.token <- struct{}{}
		m.locks[path] = lock
	}
	lock.refs++
	m.mu.Unlock()

	select {
	case <-ctx.Done():
		m.dropReference(path, lock)
		return nil, ctx.Err()
	case <-lock.token:
		return func() {
			lock.token <- struct{}{}
			m.dropReference(path, lock)
		}, nil
	}
}

func (m *pathLockManager) dropReference(path string, lock *pathLock) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock.refs--
	if lock.refs == 0 {
		delete(m.locks, path)
	}
}

// MutationExecutor reads and replaces allowlisted metadata. Its locks protect
// cooperating goroutines in this process; callers must not infer a cross-process lock.
type MutationExecutor struct {
	operations mutationOperations
	locks      *pathLockManager
}

func NewMutationExecutor() *MutationExecutor {
	return &MutationExecutor{operations: operatingSystemMutationOperations(), locks: newPathLockManager()}
}

// Read returns a defensive snapshot of one authority-managed metadata file.
func (e *MutationExecutor) Read(ctx context.Context, authority MutationAuthority, target string) (ManagedFile, error) {
	if err := contextError(ctx); err != nil {
		return ManagedFile{}, &MutationError{Stage: MutationStageRead, Cause: err}
	}
	if err := validateManagedTarget(authority, target); err != nil {
		return ManagedFile{}, &MutationError{Stage: MutationStageValidation, Cause: err}
	}
	content, info, err := readRegularManaged(filepath.Clean(target))
	if err != nil {
		return ManagedFile{}, &MutationError{Stage: MutationStageRead, Cause: err}
	}
	if err := contextError(ctx); err != nil {
		return ManagedFile{}, &MutationError{Stage: MutationStageRead, Cause: err}
	}
	state := hashState(content)
	return ManagedFile{
		Path: filepath.Clean(target), Content: append([]byte(nil), content...),
		SHA256: hashString(state.ContentHash), Mode: info.Mode().Perm(), Size: state.Size, expected: state,
	}, nil
}

// Inspect returns a content-free state snapshot suitable for crash reconciliation.
func (e *MutationExecutor) Inspect(ctx context.Context, authority MutationAuthority, target string) (ManagedFileState, error) {
	managed, err := e.Read(ctx, authority, target)
	if err != nil {
		return ManagedFileState{}, err
	}
	return ManagedFileState{Path: managed.Path, SHA256: managed.SHA256, Mode: managed.Mode, Size: managed.Size}, nil
}

func validateManagedTarget(authority MutationAuthority, target string) error {
	if err := authority.revalidate(); err != nil {
		return err
	}
	if !filepath.IsAbs(target) || containsParentTraversal(target) {
		return ErrUnauthorized
	}
	target = filepath.Clean(target)
	if target == authority.allowedRoot || !strictlyWithin(authority.allowedRoot, target) {
		return ErrUnauthorized
	}
	if !authority.manages(target) {
		return ErrUnmanagedTarget
	}
	if err := checkMutationPath(target); err != nil {
		return err
	}
	return nil
}

// Apply atomically replaces a managed file only if its hash and size still
// match the state observed by Read. The replacement and directory are synced
// in that order. A directory-sync error reports Replaced=true.
func (e *MutationExecutor) Apply(ctx context.Context, mutation Mutation) (result MutationResult, returnErr error) {
	if err := mutation.validate(); err != nil {
		return result, &MutationError{Stage: MutationStageValidation, Cause: err}
	}
	release, err := e.locks.acquire(ctx, mutation.target)
	if err != nil {
		return result, &MutationError{Stage: MutationStageValidation, Cause: err}
	}
	defer release()

	if err := contextError(ctx); err != nil {
		return result, &MutationError{Stage: MutationStageValidation, Cause: err}
	}
	if err := validateManagedTarget(mutation.authority, mutation.target); err != nil {
		return result, &MutationError{Stage: MutationStageValidation, Cause: err}
	}
	previous, info, err := readRegularManaged(mutation.target)
	if err != nil {
		return result, &MutationError{Stage: MutationStageRead, Cause: err}
	}
	previousState := hashState(previous)
	if previousState != mutation.expected {
		return result, &MutationError{Stage: MutationStageComparison, Cause: ErrStateMismatch}
	}

	temporary, err := e.operations.createTemp(filepath.Dir(mutation.target), ".studypilot-mutation-*")
	if err != nil {
		return result, &MutationError{Stage: MutationStageTemporaryCreate, Cause: err}
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			if err := temporary.Close(); err != nil && returnErr == nil {
				returnErr = &MutationError{Stage: MutationStageCleanup, Cause: err}
			}
		}
		if err := e.operations.remove(temporaryPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			cleanup := &MutationError{Stage: MutationStageCleanup, Replaced: mutationWasReplaced(returnErr, result), Cause: err}
			if returnErr == nil {
				returnErr = cleanup
			} else {
				returnErr = errors.Join(returnErr, cleanup)
			}
		}
	}()

	if err := temporary.Chmod(mutation.mode); err != nil {
		return result, &MutationError{Stage: MutationStageWrite, Cause: err}
	}
	if err := writeAll(temporary, mutation.replacement); err != nil {
		return result, &MutationError{Stage: MutationStageWrite, Cause: err}
	}
	if err := temporary.Sync(); err != nil {
		return result, &MutationError{Stage: MutationStageFileSync, Cause: err}
	}
	if err := temporary.Close(); err != nil {
		return result, &MutationError{Stage: MutationStageFileSync, Cause: err}
	}
	closed = true
	if err := contextError(ctx); err != nil {
		return result, &MutationError{Stage: MutationStageReplace, Cause: err}
	}

	// Narrow the check-to-replace window after all potentially slow writes.
	if err := validateManagedTarget(mutation.authority, mutation.target); err != nil {
		return result, &MutationError{Stage: MutationStageReplace, Cause: err}
	}
	current, currentInfo, err := readRegularManaged(mutation.target)
	if err != nil {
		return result, &MutationError{Stage: MutationStageReplace, Cause: err}
	}
	if hashState(current) != mutation.expected || !os.SameFile(info, currentInfo) {
		return result, &MutationError{Stage: MutationStageComparison, Cause: ErrStateMismatch}
	}
	if err := e.operations.rename(temporaryPath, mutation.target); err != nil {
		return result, &MutationError{Stage: MutationStageReplace, Cause: err}
	}

	newState := hashState(mutation.replacement)
	result = MutationResult{
		Path: mutation.target, PreviousHash: hashString(previousState.ContentHash),
		CurrentHash: hashString(newState.ContentHash), BytesWritten: int64(len(mutation.replacement)), Mode: mutation.mode,
	}
	directory, err := e.operations.openDir(filepath.Dir(mutation.target))
	if err != nil {
		return result, &MutationError{Stage: MutationStageDirectorySync, Replaced: true, Cause: err}
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return result, &MutationError{Stage: MutationStageDirectorySync, Replaced: true, Cause: err}
	}
	if err := directory.Close(); err != nil {
		return result, &MutationError{Stage: MutationStageDirectorySync, Replaced: true, Cause: err}
	}
	return result, nil
}

func writeAll(writer io.Writer, content []byte) error {
	for len(content) != 0 {
		written, err := writer.Write(content)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(content) {
			return io.ErrShortWrite
		}
		content = content[written:]
	}
	return nil
}

func mutationWasReplaced(err error, result MutationResult) bool {
	if result.Path != "" {
		return true
	}
	var mutationErr *MutationError
	return errors.As(err, &mutationErr) && mutationErr.Replaced
}

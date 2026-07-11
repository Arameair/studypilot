package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func courseMutationSetup(t *testing.T) (mutationFixture, MutationAuthority, string, ManagedFile) {
	t.Helper()
	fixture := newMutationFixture(t)
	authority, err := NewCourseMutationAuthority(fixture.paths, fixture.course)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(fixture.course, courseMetadataFileName)
	managed, err := NewMutationExecutor().Read(context.Background(), authority, target)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, authority, target, managed
}

func TestApplyMutationSuccessfullyReplacesCompleteFile(t *testing.T) {
	fixture, authority, target, managed := courseMutationSetup(t)
	replacement := []byte(`{"kind":"course","updated":true}`)
	mutation, err := NewMutation(authority, target, managed.ExpectedState(), replacement, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	replacement[0] = 'X'
	result, err := NewMutationExecutor().Apply(context.Background(), mutation)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"course","updated":true}`
	content, err := os.ReadFile(target)
	if err != nil || string(content) != want {
		t.Fatalf("content %q, error %v", content, err)
	}
	info, _ := os.Stat(target)
	if result.Path != target || result.PreviousHash != managed.SHA256 || result.CurrentHash != hashString(hashState([]byte(want)).ContentHash) || result.BytesWritten != int64(len(want)) || info.Mode().Perm() != 0o640 {
		t.Fatalf("unexpected result %+v mode %v", result, info.Mode().Perm())
	}
	assertDirectoryEntries(t, fixture.course, []string{courseMetadataFileName, "Modules"})
}

func TestApplyMutationRejectsStaleStateBeforeCreatingTemp(t *testing.T) {
	fixture, authority, target, managed := courseMutationSetup(t)
	stale := managed.ExpectedState()
	stale.ContentHash[0] ^= 0xff
	mutation, err := NewMutation(authority, target, stale, []byte("replacement"), 0o640)
	if err != nil {
		t.Fatal(err)
	}
	executor := NewMutationExecutor()
	created := false
	executor.operations.createTemp = func(string, string) (temporaryFile, error) {
		created = true
		return nil, errors.New("must not run")
	}
	if _, err := executor.Apply(context.Background(), mutation); !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("got %v", err)
	}
	if created {
		t.Fatal("temporary file was created")
	}
	content, _ := os.ReadFile(target)
	if string(content) != string(managed.Content) {
		t.Fatal("original changed")
	}
	assertDirectoryEntries(t, fixture.course, []string{courseMetadataFileName, "Modules"})
}

func TestConcurrentMutationsFromSameState(t *testing.T) {
	fixture, authority, target, managed := courseMutationSetup(t)
	executor := NewMutationExecutor()
	replacements := [][]byte{[]byte("first"), []byte("second")}
	mutations := make([]Mutation, 2)
	for i := range mutations {
		var err error
		mutations[i], err = NewMutation(authority, target, managed.ExpectedState(), replacements[i], 0o640)
		if err != nil {
			t.Fatal(err)
		}
	}
	type outcome struct {
		index  int
		result MutationResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	var start sync.WaitGroup
	start.Add(1)
	for i := range mutations {
		go func(index int) {
			start.Wait()
			result, err := executor.Apply(context.Background(), mutations[index])
			outcomes <- outcome{index, result, err}
		}(i)
	}
	start.Done()
	successes, mismatches, winner := 0, 0, -1
	for range 2 {
		outcome := <-outcomes
		if outcome.err == nil {
			successes++
			winner = outcome.index
		} else if errors.Is(outcome.err, ErrStateMismatch) {
			mismatches++
		} else {
			t.Fatalf("unexpected error: %v", outcome.err)
		}
	}
	content, _ := os.ReadFile(target)
	if successes != 1 || mismatches != 1 || string(content) != string(replacements[winner]) {
		t.Fatalf("success=%d mismatch=%d winner=%d content=%q", successes, mismatches, winner, content)
	}
	assertDirectoryEntries(t, fixture.course, []string{courseMetadataFileName, "Modules"})
}

var errInjected = errors.New("injected filesystem failure")
var errCleanupInjected = errors.New("injected cleanup failure")

type injectedTemporary struct {
	*os.File
	writeErr, syncErr error
	afterSync         func()
}

func (f *injectedTemporary) Write(content []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.File.Write(content)
}

func (f *injectedTemporary) Sync() error {
	if f.afterSync != nil {
		f.afterSync()
	}
	if f.syncErr != nil {
		return f.syncErr
	}
	return f.File.Sync()
}

type injectedDirectory struct{ syncErr error }

func (d injectedDirectory) Sync() error  { return d.syncErr }
func (d injectedDirectory) Close() error { return nil }

func TestMutationFailureStagesAndCleanup(t *testing.T) {
	tests := []struct {
		name      string
		stage     MutationStage
		configure func(*MutationExecutor, context.CancelFunc)
	}{
		{"temp create", MutationStageTemporaryCreate, func(e *MutationExecutor, _ context.CancelFunc) {
			e.operations.createTemp = func(string, string) (temporaryFile, error) { return nil, errInjected }
		}},
		{"write", MutationStageWrite, func(e *MutationExecutor, _ context.CancelFunc) {
			base := e.operations.createTemp
			e.operations.createTemp = func(dir, pattern string) (temporaryFile, error) {
				file, err := base(dir, pattern)
				if err != nil {
					return nil, err
				}
				return &injectedTemporary{File: file.(*os.File), writeErr: errInjected}, nil
			}
		}},
		{"file sync", MutationStageFileSync, func(e *MutationExecutor, _ context.CancelFunc) {
			base := e.operations.createTemp
			e.operations.createTemp = func(dir, pattern string) (temporaryFile, error) {
				file, err := base(dir, pattern)
				if err != nil {
					return nil, err
				}
				return &injectedTemporary{File: file.(*os.File), syncErr: errInjected}, nil
			}
		}},
		{"rename", MutationStageReplace, func(e *MutationExecutor, _ context.CancelFunc) {
			e.operations.rename = func(string, string) error { return errInjected }
		}},
		{"cancel before replace", MutationStageReplace, func(e *MutationExecutor, cancel context.CancelFunc) {
			base := e.operations.createTemp
			e.operations.createTemp = func(dir, pattern string) (temporaryFile, error) {
				file, err := base(dir, pattern)
				if err != nil {
					return nil, err
				}
				return &injectedTemporary{File: file.(*os.File), afterSync: cancel}, nil
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, authority, target, managed := courseMutationSetup(t)
			mutation, _ := NewMutation(authority, target, managed.ExpectedState(), []byte("new"), 0o640)
			executor := NewMutationExecutor()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			test.configure(executor, cancel)
			_, err := executor.Apply(ctx, mutation)
			var mutationErr *MutationError
			if !errors.As(err, &mutationErr) || mutationErr.Stage != test.stage || mutationErr.Replaced {
				t.Fatalf("got %v", err)
			}
			content, _ := os.ReadFile(target)
			if string(content) != string(managed.Content) {
				t.Fatal("original changed before replacement")
			}
			assertDirectoryEntries(t, fixture.course, []string{courseMetadataFileName, "Modules"})
		})
	}
}

func TestDirectorySyncFailureReportsReplacement(t *testing.T) {
	_, authority, target, managed := courseMutationSetup(t)
	mutation, _ := NewMutation(authority, target, managed.ExpectedState(), []byte("installed"), 0o640)
	executor := NewMutationExecutor()
	executor.operations.openDir = func(string) (syncedDirectory, error) { return injectedDirectory{syncErr: errInjected}, nil }
	result, err := executor.Apply(context.Background(), mutation)
	var mutationErr *MutationError
	if !errors.As(err, &mutationErr) || mutationErr.Stage != MutationStageDirectorySync || !mutationErr.Replaced || result.CurrentHash == "" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	content, _ := os.ReadFile(target)
	if string(content) != "installed" {
		t.Fatalf("replacement not authoritative: %q", content)
	}
}

func TestCleanupFailureDoesNotHidePrimaryFailure(t *testing.T) {
	_, authority, target, managed := courseMutationSetup(t)
	mutation, _ := NewMutation(authority, target, managed.ExpectedState(), []byte("new"), 0o640)
	executor := NewMutationExecutor()
	base := executor.operations.createTemp
	executor.operations.createTemp = func(dir, pattern string) (temporaryFile, error) {
		file, err := base(dir, pattern)
		if err != nil {
			return nil, err
		}
		return &injectedTemporary{File: file.(*os.File), writeErr: errInjected}, nil
	}
	executor.operations.remove = func(path string) error {
		_ = os.Remove(path)
		return errCleanupInjected
	}
	_, err := executor.Apply(context.Background(), mutation)
	if !errors.Is(err, errInjected) || !errors.Is(err, errCleanupInjected) {
		t.Fatalf("joined error did not preserve causes: %v", err)
	}
}

func TestInspectSupportsReconciliationStates(t *testing.T) {
	_, authority, target, old := courseMutationSetup(t)
	executor := NewMutationExecutor()
	oldState, err := executor.Inspect(context.Background(), authority, target)
	if err != nil || oldState.SHA256 != old.SHA256 {
		t.Fatalf("old state: %+v %v", oldState, err)
	}
	for _, content := range []string{"new", "unexpected-third-state"} {
		if err := os.WriteFile(target, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
		state, err := executor.Inspect(context.Background(), authority, target)
		if err != nil || state.SHA256 != hashString(hashState([]byte(content)).ContentHash) {
			t.Fatalf("content %q state %+v error %v", content, state, err)
		}
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Inspect(context.Background(), authority, target); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("missing: %v", err)
	}
	other := filepath.Join(filepath.Dir(target), "other")
	if err := os.WriteFile(other, []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, target); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Inspect(context.Background(), authority, target); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("unsafe: %v", err)
	}
}

func assertDirectoryEntries(t *testing.T, directory string, want []string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		seen[entry.Name()] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("entries=%v want=%v", seen, want)
	}
	for _, name := range want {
		if !seen[name] {
			t.Fatalf("missing entry %q in %v", name, seen)
		}
	}
}

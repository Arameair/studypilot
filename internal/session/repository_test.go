package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/filesystem"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
	"github.com/Arameair/studypilot/internal/workspace"
)

type repositoryFixture struct {
	repository         *Repository
	paths              workspace.Paths
	courseID, moduleID string
	moduleRoot         string
	now                time.Time
}

func newRepositoryFixture(t *testing.T) repositoryFixture {
	t.Helper()
	paths, err := workspace.PathsFromRoot(filepath.Join(t.TempDir(), "StudyPilot"))
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := filesystem.NewPlan(paths)
	if report, err := filesystem.Execute(plan); err != nil || report.HasConflicts() {
		t.Fatalf("workspace: %+v %v", report, err)
	}
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	coursePlan, err := course.NewCoursePlanWithID(paths, "Synthetic Course", now, func(string) (string, error) { return "course-test", nil })
	if err != nil {
		t.Fatal(err)
	}
	if report, err := filesystem.Execute(coursePlan); err != nil || report.HasConflicts() {
		t.Fatalf("course: %+v %v", report, err)
	}
	modulePlan, err := course.NewModulePlanWithID(paths, "course-test", 1, "Synthetic Module", now, func(string) (string, error) { return "module-test", nil })
	if err != nil {
		t.Fatal(err)
	}
	if report, err := filesystem.Execute(modulePlan); err != nil || report.HasConflicts() {
		t.Fatalf("module: %+v %v", report, err)
	}
	sequence := 0
	repository, err := NewRepository(paths, func() time.Time { return now.Add(time.Duration(sequence) * time.Second) }, func() (string, error) { sequence++; return fmt.Sprintf("session-%03d", sequence), nil })
	if err != nil {
		t.Fatal(err)
	}
	return repositoryFixture{repository: repository, paths: paths, courseID: "course-test", moduleID: "module-test", moduleRoot: modulePlan.Root, now: now}
}

func TestCreateAndLoadSessionRepository(t *testing.T) {
	fixture := newRepositoryFixture(t)
	record, err := fixture.repository.Create(context.Background(), fixture.courseID, fixture.moduleID, "First Session", nil)
	if err != nil {
		t.Fatal(err)
	}
	if record.Metadata.Number != 1 || record.Metadata.DirectoryName != "001 - First Session" || record.Runtime.Revision != 1 {
		t.Fatalf("record=%+v", record)
	}
	if record.Runtime.Snapshot.SessionStatus != studyruntime.SessionStatusPlanned || record.Runtime.Snapshot.CaptureStatus != studyruntime.CaptureStatusUnavailable || record.Runtime.Snapshot.FilesystemStatus != studyruntime.FilesystemStatusReady {
		t.Fatalf("snapshot=%+v", record.Runtime.Snapshot)
	}
	want := []string{".studypilot-runtime.json", ".studypilot-session.json", "Assets", "Assets/Audio", "Assets/Documents", "Assets/Screenshots", "Assets/Video", "Notes", "Segments"}
	for _, relative := range want {
		if _, err := os.Stat(filepath.Join(record.Root, filepath.FromSlash(relative))); err != nil {
			t.Errorf("missing %s: %v", relative, err)
		}
	}
	if _, err := os.Stat(filepath.Join(record.Root, "Session.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Session.md should be deferred")
	}
	loaded, err := fixture.repository.Load(context.Background(), record.Root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MetadataHash == "" || loaded.RuntimeHash == "" || loaded.Metadata.ID != record.Metadata.ID {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestCreateWithMetadataIsIdempotentAndConflictsSafely(t *testing.T) {
	fixture := newRepositoryFixture(t)
	metadata := Metadata{SchemaVersion: 1, ID: "session-fixed", CourseID: fixture.courseID, ModuleID: fixture.moduleID, Number: 1, DisplayName: "Fixed", Slug: "fixed", DirectoryName: "001 - Fixed", CreatedAt: fixture.now}
	first, err := fixture.repository.CreateWithMetadata(context.Background(), metadata, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.repository.CreateWithMetadata(context.Background(), metadata, nil)
	if err != nil || second.MetadataHash != first.MetadataHash {
		t.Fatalf("second=%+v error=%v", second, err)
	}
	if _, err := fixture.repository.UpdateRuntime(context.Background(), second, RuntimeUpdate{ExpectedRevision: 1, Next: activeSnapshot(second, fixture.now.Add(time.Minute))}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repository.CreateWithMetadata(context.Background(), metadata, nil); !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("conflicting managed runtime: %v", err)
	}
	conflict := metadata
	conflict.ID = "session-other"
	if _, err := fixture.repository.CreateWithMetadata(context.Background(), conflict, nil); !errors.Is(err, ErrDuplicateNumber) {
		t.Fatalf("duplicate: %v", err)
	}
	unmanaged := filepath.Join(fixture.moduleRoot, "Sessions", "002 - Unmanaged")
	if err := os.Mkdir(unmanaged, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repository.Create(context.Background(), fixture.courseID, fixture.moduleID, "Next", nil); !errors.Is(err, ErrMalformedState) {
		t.Fatalf("unmanaged: %v", err)
	}
}

func TestSessionNumberingUsesMetadataNotEnumerationOrder(t *testing.T) {
	fixture := newRepositoryFixture(t)
	third := Metadata{SchemaVersion: 1, ID: "session-third", CourseID: fixture.courseID, ModuleID: fixture.moduleID, Number: 3, DisplayName: "Third", Slug: "third", DirectoryName: "003 - Third", CreatedAt: fixture.now}
	if _, err := fixture.repository.CreateWithMetadata(context.Background(), third, nil); err != nil {
		t.Fatal(err)
	}
	first := Metadata{SchemaVersion: 1, ID: "session-first", CourseID: fixture.courseID, ModuleID: fixture.moduleID, Number: 1, DisplayName: "First", Slug: "first", DirectoryName: "001 - First", CreatedAt: fixture.now}
	if _, err := fixture.repository.CreateWithMetadata(context.Background(), first, nil); err != nil {
		t.Fatal(err)
	}
	next, err := fixture.repository.Create(context.Background(), fixture.courseID, fixture.moduleID, "Fourth", nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.Metadata.Number != 4 {
		t.Fatalf("number=%d", next.Metadata.Number)
	}
}

func TestLoadRejectsMissingMalformedMismatchedAndSymlinkedState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, Record)
		want   error
	}{
		{"missing metadata", func(t *testing.T, r Record) {
			if err := os.Remove(filepath.Join(r.Root, sessionMetadataName)); err != nil {
				t.Fatal(err)
			}
		}, ErrSessionNotFound},
		{"missing runtime", func(t *testing.T, r Record) {
			if err := os.Remove(filepath.Join(r.Root, runtimeStateName)); err != nil {
				t.Fatal(err)
			}
		}, ErrSessionNotFound},
		{"malformed runtime", func(t *testing.T, r Record) {
			if err := os.WriteFile(filepath.Join(r.Root, runtimeStateName), []byte("{"), 0o640); err != nil {
				t.Fatal(err)
			}
		}, ErrMalformedState},
		{"invalid snapshot", func(t *testing.T, r Record) {
			state := r.Runtime
			state.Snapshot.SessionStatus = "invalid"
			content, _ := encodeJSON(state)
			if err := os.WriteFile(filepath.Join(r.Root, runtimeStateName), content, 0o640); err != nil {
				t.Fatal(err)
			}
		}, ErrMalformedState},
		{"unsupported runtime schema", func(t *testing.T, r Record) {
			state := r.Runtime
			state.SchemaVersion = 2
			content, _ := encodeJSON(state)
			if err := os.WriteFile(filepath.Join(r.Root, runtimeStateName), content, 0o640); err != nil {
				t.Fatal(err)
			}
		}, ErrMalformedState},
		{"identity mismatch", func(t *testing.T, r Record) {
			state := r.Runtime
			state.SessionID = "session-other"
			content, _ := encodeJSON(state)
			if err := os.WriteFile(filepath.Join(r.Root, runtimeStateName), content, 0o640); err != nil {
				t.Fatal(err)
			}
		}, ErrIdentityMismatch},
		{"symlink runtime", func(t *testing.T, r Record) {
			target := filepath.Join(r.Root, "other")
			_ = os.WriteFile(target, []byte("{}"), 0o640)
			_ = os.Remove(filepath.Join(r.Root, runtimeStateName))
			if err := os.Symlink(target, filepath.Join(r.Root, runtimeStateName)); err != nil {
				t.Fatal(err)
			}
		}, filesystem.ErrUnsafePath},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRepositoryFixture(t)
			record, _ := fixture.repository.Create(context.Background(), fixture.courseID, fixture.moduleID, "Session", nil)
			test.mutate(t, record)
			if _, err := fixture.repository.Load(context.Background(), record.Root); !errors.Is(err, test.want) {
				t.Fatalf("got %v want %v", err, test.want)
			}
		})
	}
}

func activeSnapshot(record Record, at time.Time) studyruntime.Snapshot {
	next := record.Runtime.Snapshot
	next.SessionStatus = studyruntime.SessionStatusActive
	next.UpdatedAt = at
	return next
}

func TestUpdateRuntimeRevisionHashTransitionAndIndependentStatuses(t *testing.T) {
	fixture := newRepositoryFixture(t)
	record, _ := fixture.repository.Create(context.Background(), fixture.courseID, fixture.moduleID, "Session", nil)
	metadataBefore, _ := os.ReadFile(filepath.Join(record.Root, sessionMetadataName))
	next := activeSnapshot(record, fixture.now.Add(time.Minute))
	next.CaptureStatus = studyruntime.CaptureStatusFailed
	updated, err := fixture.repository.UpdateRuntime(context.Background(), record, RuntimeUpdate{ExpectedRevision: 1, Next: next})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Runtime.Revision != 2 || updated.RuntimeHash == record.RuntimeHash || updated.Runtime.Snapshot.SessionStatus != studyruntime.SessionStatusActive || updated.Runtime.Snapshot.CaptureStatus != studyruntime.CaptureStatusFailed {
		t.Fatalf("updated=%+v", updated)
	}
	metadataAfter, _ := os.ReadFile(filepath.Join(record.Root, sessionMetadataName))
	if string(metadataBefore) != string(metadataAfter) {
		t.Fatal("immutable metadata changed")
	}
	if _, err := os.Stat(filepath.Join(record.Root, "Session.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Markdown was created or modified")
	}
	if _, err := fixture.repository.UpdateRuntime(context.Background(), record, RuntimeUpdate{ExpectedRevision: 1, Next: next}); !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("stale: %v", err)
	}
}

func TestUpdateRejectsInvalidTransitions(t *testing.T) {
	fixture := newRepositoryFixture(t)
	record, _ := fixture.repository.Create(context.Background(), fixture.courseID, fixture.moduleID, "Session", nil)
	active, err := fixture.repository.UpdateRuntime(context.Background(), record, RuntimeUpdate{ExpectedRevision: 1, Next: activeSnapshot(record, fixture.now.Add(time.Minute))})
	if err != nil {
		t.Fatal(err)
	}
	completed := active.Runtime.Snapshot
	completed.SessionStatus = studyruntime.SessionStatusCompleted
	completed.UpdatedAt = fixture.now.Add(2 * time.Minute)
	terminal, err := fixture.repository.UpdateRuntime(context.Background(), active, RuntimeUpdate{ExpectedRevision: 2, Next: completed})
	if err != nil {
		t.Fatal(err)
	}
	back := terminal.Runtime.Snapshot
	back.SessionStatus = studyruntime.SessionStatusActive
	back.UpdatedAt = fixture.now.Add(3 * time.Minute)
	if _, err := fixture.repository.UpdateRuntime(context.Background(), terminal, RuntimeUpdate{ExpectedRevision: 3, Next: back}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("transition: %v", err)
	}
}

func TestConcurrentUpdatesHaveOneWinner(t *testing.T) {
	fixture := newRepositoryFixture(t)
	record, _ := fixture.repository.Create(context.Background(), fixture.courseID, fixture.moduleID, "Session", nil)
	results := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(1)
	for index := range 2 {
		go func(i int) {
			start.Wait()
			next := activeSnapshot(record, fixture.now.Add(time.Duration(i+1)*time.Minute))
			_, err := fixture.repository.UpdateRuntime(context.Background(), record, RuntimeUpdate{ExpectedRevision: 1, Next: next})
			results <- err
		}(index)
	}
	start.Done()
	successes, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else if errors.Is(err, ErrSessionConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected: %v", err)
		}
	}
	final, err := fixture.repository.Load(context.Background(), record.Root)
	if err != nil {
		t.Fatal(err)
	}
	if successes != 1 || conflicts != 1 || final.Runtime.Revision != 2 || final.Runtime.Snapshot.SessionStatus != studyruntime.SessionStatusActive {
		t.Fatalf("success=%d conflicts=%d final=%+v", successes, conflicts, final)
	}
}

func TestConcurrentCreationNeverDuplicatesNumbers(t *testing.T) {
	fixture := newRepositoryFixture(t)
	type outcome struct {
		record Record
		err    error
	}
	results := make(chan outcome, 2)
	var start sync.WaitGroup
	start.Add(1)
	for index := range 2 {
		go func(i int) {
			start.Wait()
			record, err := fixture.repository.Create(context.Background(), fixture.courseID, fixture.moduleID, fmt.Sprintf("Concurrent %d", i), nil)
			results <- outcome{record, err}
		}(index)
	}
	start.Done()
	records := []Record{}
	for range 2 {
		result := <-results
		if result.err == nil {
			records = append(records, result.record)
		} else if !errors.Is(result.err, ErrSessionConflict) {
			t.Fatalf("unexpected: %v", result.err)
		}
	}
	if len(records) == 2 && records[0].Metadata.Number == records[1].Metadata.Number {
		t.Fatalf("duplicate numbers: %+v", records)
	}
	if len(records) == 0 {
		t.Fatal("no creation succeeded")
	}
}

func contentHash(content []byte) string { return fmt.Sprintf("%x", sha256.Sum256(content)) }

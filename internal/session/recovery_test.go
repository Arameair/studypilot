package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/filesystem"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

func TestRecoveryInspectionStates(t *testing.T) {
	tests := []struct {
		name     string
		status   studyruntime.SessionStatus
		want     RecoveryStatus
		terminal bool
	}{
		{"planned", studyruntime.SessionStatusPlanned, RecoveryClean, false},
		{"active", studyruntime.SessionStatusActive, RecoveryIncomplete, false},
		{"interrupted", studyruntime.SessionStatusInterrupted, RecoveryIncomplete, false},
		{"recovering", studyruntime.SessionStatusRecovering, RecoveryIncomplete, false},
		{"completed", studyruntime.SessionStatusCompleted, RecoveryClean, true},
		{"abandoned", studyruntime.SessionStatusAbandoned, RecoveryClean, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRepositoryFixture(t)
			metadata := Metadata{SchemaVersion: 1, ID: "session-" + test.name, CourseID: fixture.courseID, ModuleID: fixture.moduleID, Number: 1, DisplayName: "Status", Slug: "status", DirectoryName: "001 - Status", CreatedAt: fixture.now}
			snapshot := initialSnapshot(fixture.paths, mustCourse(t, fixture), mustModule(t, fixture), metadata, fixture.now)
			snapshot.SessionStatus = test.status
			record, err := fixture.repository.CreateWithMetadata(context.Background(), metadata, &snapshot)
			if err != nil {
				t.Fatal(err)
			}
			inspection := fixture.repository.Inspect(context.Background(), record.Root)
			if inspection.Status != test.want || inspection.Terminal != test.terminal {
				t.Fatalf("inspection=%+v", inspection)
			}
		})
	}
}

func mustCourse(t *testing.T, fixture repositoryFixture) course.CourseRecord {
	t.Helper()
	value, err := course.FindCourse(fixture.paths, fixture.courseID)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func mustModule(t *testing.T, fixture repositoryFixture) course.ModuleRecord {
	t.Helper()
	parent := mustCourse(t, fixture)
	value, err := course.FindModule(parent, fixture.moduleID)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestInspectMissingAndMalformedRuntime(t *testing.T) {
	fixture := newRepositoryFixture(t)
	record, _ := fixture.repository.Create(context.Background(), fixture.courseID, fixture.moduleID, "Session", nil)
	if err := os.Remove(filepath.Join(record.Root, runtimeStateName)); err != nil {
		t.Fatal(err)
	}
	if inspection := fixture.repository.Inspect(context.Background(), record.Root); inspection.Status != RecoveryMissingRuntime {
		t.Fatalf("missing=%+v", inspection)
	}
	if err := os.WriteFile(filepath.Join(record.Root, runtimeStateName), []byte("{"), 0o640); err != nil {
		t.Fatal(err)
	}
	if inspection := fixture.repository.Inspect(context.Background(), record.Root); inspection.Status != RecoveryMalformed {
		t.Fatalf("malformed=%+v", inspection)
	}
}

func TestInspectReconcilesExpectedOldAndIntendedHashes(t *testing.T) {
	fixture := newRepositoryFixture(t)
	record, _ := fixture.repository.Create(context.Background(), fixture.courseID, fixture.moduleID, "Session", nil)
	next := activeSnapshot(record, fixture.now.Add(time.Minute))
	intended := RuntimeState{SchemaVersion: 1, SessionID: record.Metadata.ID, Revision: 2, Snapshot: next}
	content, _ := encodeJSON(intended)
	intendedHash := contentHash(content)
	if inspection := fixture.repository.Inspect(context.Background(), record.Root, record.RuntimeHash, intendedHash); inspection.Status != RecoveryClean {
		t.Fatalf("old=%+v", inspection)
	}
	if err := os.WriteFile(filepath.Join(record.Root, runtimeStateName), content, 0o640); err != nil {
		t.Fatal(err)
	}
	if inspection := fixture.repository.Inspect(context.Background(), record.Root, record.RuntimeHash, intendedHash); inspection.Status != RecoveryUncertain || inspection.Record == nil || inspection.Record.Runtime.Revision != 2 {
		t.Fatalf("intended=%+v", inspection)
	}
}

func TestDiscoverIncompleteStableAndReportsIssues(t *testing.T) {
	fixture := newRepositoryFixture(t)
	statuses := []studyruntime.SessionStatus{studyruntime.SessionStatusCompleted, studyruntime.SessionStatusRecovering, studyruntime.SessionStatusPlanned, studyruntime.SessionStatusAbandoned, studyruntime.SessionStatusActive, studyruntime.SessionStatusInterrupted}
	for index, status := range statuses {
		number := index + 1
		title := "Session " + string(rune('A'+index))
		directory, slug, _ := sessionNames(number, title)
		metadata := Metadata{SchemaVersion: 1, ID: "session-discovery-" + slug, CourseID: fixture.courseID, ModuleID: fixture.moduleID, Number: number, DisplayName: title, Slug: slug, DirectoryName: directory, CreatedAt: fixture.now}
		snapshot := initialSnapshot(fixture.paths, mustCourse(t, fixture), mustModule(t, fixture), metadata, fixture.now)
		snapshot.SessionStatus = status
		if _, err := fixture.repository.CreateWithMetadata(context.Background(), metadata, &snapshot); err != nil {
			t.Fatal(err)
		}
	}
	unmanaged := filepath.Join(fixture.moduleRoot, "Sessions", "999 - Broken")
	if err := os.Mkdir(unmanaged, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.moduleRoot, "Sessions", "unrelated.txt"), []byte("safe"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(fixture.moduleRoot, "Sessions", "998 - Link")
	if err := os.Symlink(unmanaged, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	discovery, err := fixture.repository.DiscoverIncomplete(context.Background(), fixture.courseID, fixture.moduleID)
	if err != nil {
		t.Fatal(err)
	}
	wantNumbers := []int{2, 3, 5, 6}
	if len(discovery.Records) != len(wantNumbers) {
		t.Fatalf("records=%+v", discovery.Records)
	}
	for index, want := range wantNumbers {
		if discovery.Records[index].Metadata.Number != want {
			t.Fatalf("order=%+v", discovery.Records)
		}
	}
	if len(discovery.Issues) != 2 {
		t.Fatalf("issues=%+v", discovery.Issues)
	}
}

type uncertainEngine struct {
	reads    []filesystem.ManagedFile
	intended filesystem.ManagedFileState
}

func (e *uncertainEngine) Read(context.Context, filesystem.MutationAuthority, string) (filesystem.ManagedFile, error) {
	result := e.reads[0]
	e.reads = e.reads[1:]
	return result, nil
}
func (e *uncertainEngine) Inspect(context.Context, filesystem.MutationAuthority, string) (filesystem.ManagedFileState, error) {
	return e.intended, nil
}
func (e *uncertainEngine) Apply(context.Context, filesystem.Mutation) (filesystem.MutationResult, error) {
	return filesystem.MutationResult{Path: e.intended.Path, CurrentHash: e.intended.SHA256, BytesWritten: e.intended.Size, Mode: 0o640}, &filesystem.MutationError{Stage: filesystem.MutationStageDirectorySync, Replaced: true, Cause: errors.New("synthetic sync failure")}
}

func TestUpdateReconcilesUncertainReplacement(t *testing.T) {
	fixture := newRepositoryFixture(t)
	record, _ := fixture.repository.Create(context.Background(), fixture.courseID, fixture.moduleID, "Session", nil)
	next := activeSnapshot(record, fixture.now.Add(time.Minute))
	proposed := RuntimeState{SchemaVersion: 1, SessionID: record.Metadata.ID, Revision: 2, Snapshot: next}
	proposedBytes, _ := encodeJSON(proposed)
	metadataBytes, _ := encodeJSON(record.Metadata)
	metadataManaged := filesystem.ManagedFile{Path: filepath.Join(record.Root, sessionMetadataName), Content: metadataBytes, SHA256: contentHash(metadataBytes), Mode: 0o640, Size: int64(len(metadataBytes))}
	currentBytes, _ := encodeJSON(record.Runtime)
	currentManaged := filesystem.ManagedFile{Path: filepath.Join(record.Root, runtimeStateName), Content: currentBytes, SHA256: contentHash(currentBytes), Mode: 0o640, Size: int64(len(currentBytes))}
	proposedManaged := filesystem.ManagedFile{Path: filepath.Join(record.Root, runtimeStateName), Content: proposedBytes, SHA256: contentHash(proposedBytes), Mode: 0o640, Size: int64(len(proposedBytes))}
	fixture.repository.mutations = &uncertainEngine{reads: []filesystem.ManagedFile{metadataManaged, currentManaged, metadataManaged, proposedManaged}, intended: filesystem.ManagedFileState{Path: proposedManaged.Path, SHA256: proposedManaged.SHA256, Mode: 0o640, Size: proposedManaged.Size}}
	installed, err := fixture.repository.UpdateRuntime(context.Background(), record, RuntimeUpdate{ExpectedRevision: 1, Next: next})
	if err != nil {
		t.Fatal(err)
	}
	if !installed.DurabilityWarning || installed.Runtime.Revision != 2 {
		t.Fatalf("installed=%+v", installed)
	}
}

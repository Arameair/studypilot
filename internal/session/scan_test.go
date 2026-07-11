package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

// writeRawSession injects a fully-formed session directory without going through
// the write-path allocation checks, so tests can construct sibling states (such
// as duplicate numbers) that Create would otherwise reject.
func writeRawSession(t *testing.T, fixture repositoryFixture, metadata Metadata, snapshot studyruntime.Snapshot) string {
	t.Helper()
	sessionRoot := filepath.Join(fixture.moduleRoot, "Sessions", metadata.DirectoryName)
	if err := os.MkdirAll(sessionRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	runtimeState := RuntimeState{SchemaVersion: RuntimeSchemaVersion, SessionID: metadata.ID, Revision: 1, Snapshot: snapshot}
	metadataBytes, err := encodeJSON(metadata)
	if err != nil {
		t.Fatal(err)
	}
	runtimeBytes, err := encodeJSON(runtimeState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionRoot, sessionMetadataName), metadataBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionRoot, runtimeStateName), runtimeBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	return sessionRoot
}

func sessionMetadataFor(t *testing.T, fixture repositoryFixture, id string, number int, title string) Metadata {
	t.Helper()
	directory, slug, err := sessionNames(number, title)
	if err != nil {
		t.Fatal(err)
	}
	return Metadata{SchemaVersion: MetadataSchemaVersion, ID: id, CourseID: fixture.courseID, ModuleID: fixture.moduleID, Number: number, DisplayName: title, Slug: slug, DirectoryName: directory, CreatedAt: fixture.now}
}

func snapshotFor(t *testing.T, fixture repositoryFixture, metadata Metadata, status studyruntime.SessionStatus) studyruntime.Snapshot {
	t.Helper()
	snapshot := initialSnapshot(fixture.paths, mustCourse(t, fixture), mustModule(t, fixture), metadata, fixture.now)
	snapshot.SessionStatus = status
	return snapshot
}

func TestScanReturnsHealthyRecordsDespiteSiblingIssues(t *testing.T) {
	fixture := newRepositoryFixture(t)
	sessionsRoot := filepath.Join(fixture.moduleRoot, "Sessions")

	// Two valid sessions written through the ordinary write path.
	activeMeta := sessionMetadataFor(t, fixture, "session-active", 1, "Service Troubleshooting")
	if _, err := fixture.repository.CreateWithMetadata(context.Background(), activeMeta, ptr(snapshotFor(t, fixture, activeMeta, studyruntime.SessionStatusActive))); err != nil {
		t.Fatal(err)
	}
	plannedMeta := sessionMetadataFor(t, fixture, "session-planned", 2, "Windows Services Review")
	if _, err := fixture.repository.CreateWithMetadata(context.Background(), plannedMeta, ptr(snapshotFor(t, fixture, plannedMeta, studyruntime.SessionStatusPlanned))); err != nil {
		t.Fatal(err)
	}
	// A valid session whose runtime file we then remove.
	missingMeta := sessionMetadataFor(t, fixture, "session-missing", 4, "Runtime Gone")
	missingRoot, err := fixture.repository.CreateWithMetadata(context.Background(), missingMeta, ptr(snapshotFor(t, fixture, missingMeta, studyruntime.SessionStatusPlanned)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(missingRoot.Root, runtimeStateName)); err != nil {
		t.Fatal(err)
	}

	// Malformed metadata injected directly.
	malformedRoot := filepath.Join(sessionsRoot, "003 - Bad Metadata")
	if err := os.MkdirAll(malformedRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(malformedRoot, sessionMetadataName), []byte("{ not json"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(malformedRoot, runtimeStateName), []byte("{}"), 0o640); err != nil {
		t.Fatal(err)
	}

	// Unsafe symlink.
	if err := os.Symlink(malformedRoot, filepath.Join(sessionsRoot, "005 - Link")); err != nil {
		t.Fatal(err)
	}

	// Two sibling directories that each validate but claim the same number.
	dupA := sessionMetadataFor(t, fixture, "session-dup-a", 6, "Duplicate A")
	writeRawSession(t, fixture, dupA, snapshotFor(t, fixture, dupA, studyruntime.SessionStatusPlanned))
	dupB := sessionMetadataFor(t, fixture, "session-dup-b", 6, "Duplicate B")
	writeRawSession(t, fixture, dupB, snapshotFor(t, fixture, dupB, studyruntime.SessionStatusPlanned))

	// Snapshot on-disk state to later prove the scan mutated nothing.
	before := treeSnapshot(t, sessionsRoot)

	result, err := fixture.repository.Scan(context.Background(), fixture.courseID, fixture.moduleID)
	if err != nil {
		t.Fatal(err)
	}

	// Healthy sessions (1 and 2) remain visible; the duplicates are excluded.
	if len(result.Records) != 2 {
		t.Fatalf("healthy records = %+v", result.Records)
	}
	if result.Records[0].Metadata.Number != 1 || result.Records[1].Metadata.Number != 2 {
		t.Fatalf("unstable healthy order: %+v", result.Records)
	}

	got := map[ScanIssueKind]int{}
	for _, issue := range result.Issues {
		got[issue.Kind]++
		if issue.Path == "" || issue.SafeName == "" {
			t.Fatalf("issue missing directory identity: %+v", issue)
		}
	}
	for kind, want := range map[ScanIssueKind]int{
		ScanIssueMalformedMetadata: 1,
		ScanIssueMissingRuntime:    1,
		ScanIssueUnsafePath:        1,
		ScanIssueDuplicateNumber:   2,
	} {
		if got[kind] != want {
			t.Fatalf("issue kind %s = %d, want %d (issues=%+v)", kind, got[kind], want, result.Issues)
		}
	}
	if len(result.Issues) != 5 {
		t.Fatalf("total issues = %d, want 5 (issues=%+v)", len(result.Issues), result.Issues)
	}

	// Ordering is stable across repeated scans.
	again, err := fixture.repository.Scan(context.Background(), fixture.courseID, fixture.moduleID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Issues) != len(result.Issues) {
		t.Fatalf("issue count changed: %d vs %d", len(again.Issues), len(result.Issues))
	}
	for i := range result.Issues {
		if again.Issues[i] != result.Issues[i] {
			t.Fatalf("issue ordering unstable at %d: %+v vs %+v", i, again.Issues[i], result.Issues[i])
		}
	}

	// No file contents leak into issue output.
	for _, issue := range result.Issues {
		if len(issue.Message) > 120 || containsAny(issue.Message, "{", "}", "session-active", "session-dup") {
			t.Fatalf("issue message leaks content: %q", issue.Message)
		}
	}

	// The scan modified nothing on disk.
	if after := treeSnapshot(t, sessionsRoot); !equalTrees(before, after) {
		t.Fatal("scan modified session directory contents")
	}

	// Write operations still fail closed while the module is ambiguous/unsafe.
	if _, err := fixture.repository.Create(context.Background(), fixture.courseID, fixture.moduleID, "New Session", nil); err == nil {
		t.Fatal("write operation unexpectedly succeeded on malformed module")
	}
}

func TestScanReportsDuplicateIDs(t *testing.T) {
	fixture := newRepositoryFixture(t)
	a := sessionMetadataFor(t, fixture, "session-shared", 1, "First")
	writeRawSession(t, fixture, a, snapshotFor(t, fixture, a, studyruntime.SessionStatusPlanned))
	b := sessionMetadataFor(t, fixture, "session-shared", 2, "Second")
	writeRawSession(t, fixture, b, snapshotFor(t, fixture, b, studyruntime.SessionStatusPlanned))

	result, err := fixture.repository.Scan(context.Background(), fixture.courseID, fixture.moduleID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 0 {
		t.Fatalf("duplicate-id sessions should not be healthy: %+v", result.Records)
	}
	duplicates := 0
	for _, issue := range result.Issues {
		if issue.Kind == ScanIssueDuplicateID {
			duplicates++
		}
	}
	if duplicates != 2 {
		t.Fatalf("duplicate id issues = %d, want 2 (issues=%+v)", duplicates, result.Issues)
	}
}

func ptr(snapshot studyruntime.Snapshot) *studyruntime.Snapshot { return &snapshot }

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		for i := 0; i+len(needle) <= len(value); i++ {
			if value[i:i+len(needle)] == needle {
				return true
			}
		}
	}
	return false
}

type fileState struct {
	content []byte
	mode    os.FileMode
}

func treeSnapshot(t *testing.T, root string) map[string]fileState {
	t.Helper()
	states := map[string]fileState{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		states[path] = fileState{content: content, mode: info.Mode()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return states
}

func equalTrees(a, b map[string]fileState) bool {
	if len(a) != len(b) {
		return false
	}
	for path, state := range a {
		other, ok := b[path]
		if !ok || other.mode != state.mode || string(other.content) != string(state.content) {
			return false
		}
	}
	return true
}

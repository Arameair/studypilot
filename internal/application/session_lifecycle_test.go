package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
	"github.com/Arameair/studypilot/internal/session"
	"github.com/Arameair/studypilot/internal/workspace"
)

type lifecycleFixture struct {
	service *Service
	root    string
	now     time.Time
}

func newLifecycleFixture(t *testing.T) lifecycleFixture {
	t.Helper()
	now := fixedDate
	var mu sync.Mutex
	sequence := 0
	generate := func(kind string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		sequence++
		return fmt.Sprintf("%s-lifecycle-%03d", kind, sequence), nil
	}
	service := newTestService(t, func() time.Time { return now }, generate)
	root := testRoot(t)
	initWorkspace(t, service, root)
	if result, err := service.CreateCourse(context.Background(), CourseCreateRequest{Root: root, Name: "Lifecycle Course"}); err != nil || result.Conflicts != 0 {
		t.Fatalf("course=%+v %v", result, err)
	}
	if result, err := service.CreateModule(context.Background(), ModuleCreateRequest{Root: root, CourseRef: "Lifecycle Course", Number: 1, Name: "Lifecycle Module"}); err != nil || result.Conflicts != 0 {
		t.Fatalf("module=%+v %v", result, err)
	}
	return lifecycleFixture{service: service, root: root, now: now}
}

func (f lifecycleFixture) create(t *testing.T, title, key string) SessionResult {
	t.Helper()
	result, err := f.service.CreateSession(context.Background(), CreateSessionRequest{Root: f.root, CourseRef: "Lifecycle Course", ModuleRef: "Lifecycle Module", Title: title, IdempotencyKey: key})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func updateRequest(f lifecycleFixture, result SessionResult) UpdateSessionRequest {
	return UpdateSessionRequest{Root: f.root, CourseRef: result.CourseID, ModuleRef: result.ModuleID, SessionRef: result.ID, ExpectedRevision: result.Revision}
}

func TestCreateSessionPlannedAndIdempotentByExplicitKey(t *testing.T) {
	fixture := newLifecycleFixture(t)
	first := fixture.create(t, "First Session", "request-001")
	second := fixture.create(t, "First Session", "request-001")
	if first.ID != second.ID || first.Number != second.Number || first.Revision != 1 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if first.Snapshot.SessionStatus != studyruntime.SessionStatusPlanned || first.Snapshot.CaptureStatus != studyruntime.CaptureStatusUnavailable || first.Snapshot.CurrentSegment != 0 {
		t.Fatalf("snapshot=%+v", first.Snapshot)
	}
	third := fixture.create(t, "First Session", "")
	if third.ID == first.ID || third.Number == first.Number {
		t.Fatal("unkeyed creation did not create a distinct intentional session")
	}
}

func TestCreateSessionValidationAndLookupErrors(t *testing.T) {
	fixture := newLifecycleFixture(t)
	tests := []struct {
		name    string
		request CreateSessionRequest
		kind    ErrorKind
	}{
		{"missing course", CreateSessionRequest{Root: fixture.root, CourseRef: "missing", ModuleRef: "Lifecycle Module", Title: "Valid"}, ErrorNotFound},
		{"missing module", CreateSessionRequest{Root: fixture.root, CourseRef: "Lifecycle Course", ModuleRef: "missing", Title: "Valid"}, ErrorNotFound},
		{"invalid title", CreateSessionRequest{Root: fixture.root, CourseRef: "Lifecycle Course", ModuleRef: "Lifecycle Module", Title: "  "}, ErrorInvalidInput},
		{"invalid key", CreateSessionRequest{Root: fixture.root, CourseRef: "Lifecycle Course", ModuleRef: "Lifecycle Module", Title: "Valid", IdempotencyKey: "bad\nkey"}, ErrorInvalidInput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := fixture.service.CreateSession(context.Background(), test.request)
			if Classify(err) != test.kind {
				t.Fatalf("error=%v kind=%s", err, Classify(err))
			}
		})
	}
}

func TestLifecycleStartInterruptRecoverResumeAndComplete(t *testing.T) {
	fixture := newLifecycleFixture(t)
	created := fixture.create(t, "Lifecycle", "lifecycle")
	started, err := fixture.service.StartSession(context.Background(), updateRequest(fixture, created))
	if err != nil {
		t.Fatal(err)
	}
	if started.Revision != 2 || started.Snapshot.SessionStatus != studyruntime.SessionStatusActive || started.Snapshot.SessionStartedAt == nil || started.Snapshot.CaptureStatus != created.Snapshot.CaptureStatus {
		t.Fatalf("started=%+v", started)
	}
	transcription := started.Snapshot.TranscriptionStatus
	interrupted, err := fixture.service.InterruptSession(context.Background(), InterruptSessionRequest{UpdateSessionRequest: updateRequest(fixture, started), Reason: "route changed"})
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.Snapshot.SessionStatus != studyruntime.SessionStatusInterrupted || interrupted.Snapshot.CaptureStatus != started.Snapshot.CaptureStatus || interrupted.Snapshot.TranscriptionStatus != transcription {
		t.Fatalf("interrupted=%+v", interrupted)
	}
	recovering, err := fixture.service.BeginSessionRecovery(context.Background(), updateRequest(fixture, interrupted))
	if err != nil {
		t.Fatal(err)
	}
	if recovering.Snapshot.SessionStatus != studyruntime.SessionStatusRecovering || recovering.Snapshot.SessionStartedAt == nil {
		t.Fatalf("recovering=%+v", recovering)
	}
	resumed, err := fixture.service.ResumeSession(context.Background(), updateRequest(fixture, recovering))
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Snapshot.SessionStatus != studyruntime.SessionStatusActive || resumed.Snapshot.SessionStartedAt == nil {
		t.Fatalf("resumed=%+v", resumed)
	}
	completed, err := fixture.service.CompleteSession(context.Background(), CompleteSessionRequest{UpdateSessionRequest: updateRequest(fixture, resumed)})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Snapshot.SessionStatus != studyruntime.SessionStatusCompleted || completed.Snapshot.TranscriptionStatus != transcription {
		t.Fatalf("completed=%+v", completed)
	}
	again, err := fixture.service.CompleteSession(context.Background(), CompleteSessionRequest{UpdateSessionRequest: updateRequest(fixture, completed)})
	if err != nil || again.Revision != completed.Revision {
		t.Fatalf("idempotent complete=%+v %v", again, err)
	}
	if _, err := fixture.service.StartSession(context.Background(), updateRequest(fixture, completed)); Classify(err) != ErrorConflict {
		t.Fatalf("terminal start: %v", err)
	}
}

func TestInterruptedMayResumeDirectlyOrComplete(t *testing.T) {
	fixture := newLifecycleFixture(t)
	for _, action := range []string{"resume", "complete"} {
		t.Run(action, func(t *testing.T) {
			created := fixture.create(t, "Session "+action, action)
			active, _ := fixture.service.StartSession(context.Background(), updateRequest(fixture, created))
			interrupted, _ := fixture.service.InterruptSession(context.Background(), InterruptSessionRequest{UpdateSessionRequest: updateRequest(fixture, active)})
			if action == "resume" {
				result, err := fixture.service.ResumeSession(context.Background(), updateRequest(fixture, interrupted))
				if err != nil || result.Snapshot.SessionStatus != studyruntime.SessionStatusActive {
					t.Fatalf("%+v %v", result, err)
				}
			} else {
				result, err := fixture.service.CompleteSession(context.Background(), CompleteSessionRequest{UpdateSessionRequest: updateRequest(fixture, interrupted)})
				if err != nil || result.Snapshot.SessionStatus != studyruntime.SessionStatusCompleted {
					t.Fatalf("%+v %v", result, err)
				}
			}
		})
	}
}

func TestAbandonPermittedOriginsAndRetainsRecord(t *testing.T) {
	fixture := newLifecycleFixture(t)
	for _, origin := range []string{"planned", "active", "interrupted", "recovering"} {
		t.Run(origin, func(t *testing.T) {
			current := fixture.create(t, "Abandon "+origin, origin)
			if origin != "planned" {
				current, _ = fixture.service.StartSession(context.Background(), updateRequest(fixture, current))
			}
			if origin == "interrupted" || origin == "recovering" {
				current, _ = fixture.service.InterruptSession(context.Background(), InterruptSessionRequest{UpdateSessionRequest: updateRequest(fixture, current)})
			}
			if origin == "recovering" {
				current, _ = fixture.service.BeginSessionRecovery(context.Background(), updateRequest(fixture, current))
			}
			abandoned, err := fixture.service.AbandonSession(context.Background(), AbandonSessionRequest{UpdateSessionRequest: updateRequest(fixture, current), Reason: "explicit"})
			if err != nil || abandoned.Snapshot.SessionStatus != studyruntime.SessionStatusAbandoned {
				t.Fatalf("%+v %v", abandoned, err)
			}
			loaded, err := fixture.service.GetSession(context.Background(), SessionReferenceRequest{Root: fixture.root, CourseRef: abandoned.CourseID, ModuleRef: abandoned.ModuleID, SessionRef: abandoned.ID})
			if err != nil || loaded.ID != abandoned.ID {
				t.Fatalf("record deleted: %+v %v", loaded, err)
			}
			if _, err := fixture.service.AbandonSession(context.Background(), AbandonSessionRequest{UpdateSessionRequest: updateRequest(fixture, abandoned)}); Classify(err) != ErrorConflict {
				t.Fatalf("terminal abandon: %v", err)
			}
		})
	}
}

func TestInvalidOriginsReasonsAndStaleRevision(t *testing.T) {
	fixture := newLifecycleFixture(t)
	planned := fixture.create(t, "Validation", "validation")
	if _, err := fixture.service.InterruptSession(context.Background(), InterruptSessionRequest{UpdateSessionRequest: updateRequest(fixture, planned)}); Classify(err) != ErrorConflict {
		t.Fatalf("planned interrupt: %v", err)
	}
	if _, err := fixture.service.CompleteSession(context.Background(), CompleteSessionRequest{UpdateSessionRequest: updateRequest(fixture, planned)}); Classify(err) != ErrorConflict {
		t.Fatalf("planned complete: %v", err)
	}
	badReason := strings.Repeat("x", 501)
	if _, err := fixture.service.AbandonSession(context.Background(), AbandonSessionRequest{UpdateSessionRequest: updateRequest(fixture, planned), Reason: badReason}); Classify(err) != ErrorInvalidInput {
		t.Fatalf("reason: %v", err)
	}
	stale := updateRequest(fixture, planned)
	stale.ExpectedRevision = 99
	if _, err := fixture.service.StartSession(context.Background(), stale); Classify(err) != ErrorConflict {
		t.Fatalf("stale: %v", err)
	}
}

func TestIncompleteListFiltersAndStableOrdering(t *testing.T) {
	fixture := newLifecycleFixture(t)
	planned := fixture.create(t, "Planned", "p")
	active := fixture.create(t, "Active", "a")
	active, _ = fixture.service.StartSession(context.Background(), updateRequest(fixture, active))
	interrupted := fixture.create(t, "Interrupted", "i")
	interrupted, _ = fixture.service.StartSession(context.Background(), updateRequest(fixture, interrupted))
	interrupted, _ = fixture.service.InterruptSession(context.Background(), InterruptSessionRequest{UpdateSessionRequest: updateRequest(fixture, interrupted)})
	recovering := fixture.create(t, "Recovering", "r")
	recovering, _ = fixture.service.StartSession(context.Background(), updateRequest(fixture, recovering))
	recovering, _ = fixture.service.InterruptSession(context.Background(), InterruptSessionRequest{UpdateSessionRequest: updateRequest(fixture, recovering)})
	recovering, _ = fixture.service.BeginSessionRecovery(context.Background(), updateRequest(fixture, recovering))
	completed := fixture.create(t, "Completed", "c")
	completed, _ = fixture.service.StartSession(context.Background(), updateRequest(fixture, completed))
	_, _ = fixture.service.CompleteSession(context.Background(), CompleteSessionRequest{UpdateSessionRequest: updateRequest(fixture, completed)})
	abandoned := fixture.create(t, "Abandoned", "b")
	_, _ = fixture.service.AbandonSession(context.Background(), AbandonSessionRequest{UpdateSessionRequest: updateRequest(fixture, abandoned)})
	list, err := fixture.service.ListIncompleteSessions(context.Background(), ListIncompleteSessionsRequest{Root: fixture.root, CourseRef: planned.CourseID, ModuleRef: planned.ModuleID})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("list=%+v", list)
	}
	for index := 1; index < len(list); index++ {
		if list[index-1].Number >= list[index].Number {
			t.Fatalf("unstable order: %+v", list)
		}
	}
}

func TestGetAndInspectSessionAreReadOnly(t *testing.T) {
	fixture := newLifecycleFixture(t)
	created := fixture.create(t, "Inspect", "inspect")
	active, _ := fixture.service.StartSession(context.Background(), updateRequest(fixture, created))
	request := SessionReferenceRequest{Root: fixture.root, CourseRef: active.CourseID, ModuleRef: active.ModuleID, SessionRef: active.ID}
	loaded, err := fixture.service.GetSession(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := fixture.service.InspectSession(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != active.Revision || inspection.Session.ID != active.ID || inspection.RecoveryState != string(session.RecoveryIncomplete) || !inspection.Recoverable || len(inspection.Issues) != 0 {
		t.Fatalf("loaded=%+v inspection=%+v", loaded, inspection)
	}
	after, _ := fixture.service.GetSession(context.Background(), request)
	if after.Revision != active.Revision {
		t.Fatal("inspection mutated session")
	}
}

func TestConcurrentApplicationStartsHaveOneWinner(t *testing.T) {
	fixture := newLifecycleFixture(t)
	created := fixture.create(t, "Concurrent", "concurrent")
	request := updateRequest(fixture, created)
	results := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(1)
	for range 2 {
		go func() {
			start.Wait()
			_, err := fixture.service.StartSession(context.Background(), request)
			results <- err
		}()
	}
	start.Done()
	success, conflict := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if Classify(err) == ErrorConflict {
			conflict++
		} else {
			t.Fatalf("unexpected %v", err)
		}
	}
	loaded, _ := fixture.service.GetSession(context.Background(), SessionReferenceRequest{Root: fixture.root, CourseRef: created.CourseID, ModuleRef: created.ModuleID, SessionRef: created.ID})
	if success != 1 || conflict != 1 || loaded.Revision != 2 {
		t.Fatalf("success=%d conflict=%d loaded=%+v", success, conflict, loaded)
	}
}

func TestInspectModuleSessionsToleratesMalformedSibling(t *testing.T) {
	fixture := newLifecycleFixture(t)
	created := fixture.create(t, "Healthy", "healthy")

	sessionsRoot := filepath.Join(fixture.root, "Learning-Vault-Private", "01 Courses", "Lifecycle Course", "Modules", "01 - Lifecycle Module", "Sessions")
	broken := filepath.Join(sessionsRoot, "099 - Broken")
	if err := os.MkdirAll(broken, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, ".studypilot-session.json"), []byte("{ not json"), 0o640); err != nil {
		t.Fatal(err)
	}

	result, err := fixture.service.InspectModuleSessions(context.Background(), InspectModuleRequest{Root: fixture.root, CourseRef: created.CourseID, ModuleRef: created.ModuleID})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) != 1 || result.Sessions[0].ID != created.ID {
		t.Fatalf("healthy session hidden: %+v", result.Sessions)
	}
	if len(result.Issues) != 1 || result.Issues[0].Kind != string(session.ScanIssueMalformedMetadata) || result.Issues[0].Directory != "099 - Broken" {
		t.Fatalf("issue not reported: %+v", result.Issues)
	}
	// A write operation on the same module still fails closed.
	if _, err := fixture.service.CreateSession(context.Background(), CreateSessionRequest{Root: fixture.root, CourseRef: created.CourseID, ModuleRef: created.ModuleID, Title: "Blocked"}); Classify(err) == "" {
		t.Fatal("write operation unexpectedly succeeded on malformed module")
	}
}

type fakeLifecycleRepository struct {
	record     session.Record
	inspection session.Inspection
	updates    int
}

func (f *fakeLifecycleRepository) Create(context.Context, string, string, string, *studyruntime.Snapshot) (session.Record, error) {
	return f.record, nil
}
func (f *fakeLifecycleRepository) CreateWithID(context.Context, string, string, string, string, *studyruntime.Snapshot) (session.Record, error) {
	return f.record, nil
}
func (f *fakeLifecycleRepository) Find(context.Context, string, string, string) (session.Record, error) {
	return f.record, nil
}
func (f *fakeLifecycleRepository) Locate(context.Context, string, string, string) (string, session.Metadata, error) {
	return f.record.Root, f.record.Metadata, nil
}
func (f *fakeLifecycleRepository) Load(context.Context, string) (session.Record, error) {
	return f.record, nil
}
func (f *fakeLifecycleRepository) UpdateRuntime(_ context.Context, record session.Record, update session.RuntimeUpdate) (session.Record, error) {
	f.updates++
	record.Runtime.Revision++
	record.Runtime.Snapshot = update.Next
	return record, nil
}
func (f *fakeLifecycleRepository) Inspect(context.Context, string, ...string) session.Inspection {
	return f.inspection
}
func (f *fakeLifecycleRepository) DiscoverIncomplete(context.Context, string, string) (session.Discovery, error) {
	return session.Discovery{}, nil
}
func (f *fakeLifecycleRepository) Scan(context.Context, string, string) (session.ScanResult, error) {
	return session.ScanResult{}, nil
}

func TestActiveCaptureAndUncertainRecoveryAreRejected(t *testing.T) {
	fixture := newLifecycleFixture(t)
	created := fixture.create(t, "Fake", "fake")
	record := session.Record{Root: "synthetic", Metadata: session.Metadata{ID: created.ID, CourseID: created.CourseID, ModuleID: created.ModuleID, Number: created.Number, DisplayName: created.Title}, Runtime: session.RuntimeState{Revision: created.Revision, Snapshot: created.Snapshot}}
	record.Runtime.Snapshot.SessionStatus = studyruntime.SessionStatusActive
	record.Runtime.Snapshot.CaptureStatus = studyruntime.CaptureStatusRecording
	fake := &fakeLifecycleRepository{record: record, inspection: session.Inspection{Status: session.RecoveryUncertain}}
	service, _ := NewService(Dependencies{Now: fixedClock(fixture.now), GenerateID: prefixedID("fake"), SessionRepositories: func(workspace.Paths, session.Clock, session.IDGenerator) (SessionRepository, error) { return fake, nil }})
	ref := UpdateSessionRequest{Root: fixture.root, CourseRef: created.CourseID, ModuleRef: created.ModuleID, SessionRef: created.ID, ExpectedRevision: 1}
	if _, err := service.InterruptSession(context.Background(), InterruptSessionRequest{UpdateSessionRequest: ref}); Classify(err) != ErrorConflict || fake.updates != 0 {
		t.Fatalf("recording interrupt=%v updates=%d", err, fake.updates)
	}
	if _, err := service.CompleteSession(context.Background(), CompleteSessionRequest{UpdateSessionRequest: ref}); Classify(err) != ErrorConflict || fake.updates != 0 {
		t.Fatalf("recording complete=%v updates=%d", err, fake.updates)
	}
	fake.record.Runtime.Snapshot.SessionStatus = studyruntime.SessionStatusInterrupted
	fake.record.Runtime.Snapshot.CaptureStatus = studyruntime.CaptureStatusUnavailable
	if _, err := service.BeginSessionRecovery(context.Background(), ref); Classify(err) != ErrorConflict || fake.updates != 0 {
		t.Fatalf("uncertain recovery=%v updates=%d", err, fake.updates)
	}
}

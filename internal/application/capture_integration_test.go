package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/capture"
	"github.com/Arameair/studypilot/internal/capture/backend"
	"github.com/Arameair/studypilot/internal/session"
	"github.com/Arameair/studypilot/internal/workspace"
)

type captureFixture struct {
	service *Service
	root    string
	session SessionResult
}

func newCaptureFixture(t *testing.T, sessions SessionRepositoryFactory) captureFixture {
	t.Helper()
	now := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	sequence := 0
	var mu sync.Mutex
	ids := func(kind string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		sequence++
		return kind + "-capture-" + string(rune('a'+sequence)), nil
	}
	captureFactory := func(paths workspace.Paths, name string, resolve func(string) (string, error)) (CaptureService, error) {
		if name != "synthetic" {
			return nil, capture.NewError(capture.ErrorUnavailable, capture.OpStart, false, capture.OutcomeNotStarted, "unknown capture backend", nil)
		}
		raw, err := backend.NewSyntheticBackend(backend.SyntheticConfig{Paths: paths, Clock: func() time.Time { return now }, NewSegmentID: func() (string, error) {
			mu.Lock()
			defer mu.Unlock()
			sequence++
			return "segment-capture-" + string(rune('a'+sequence)), nil
		}})
		if err != nil {
			return nil, err
		}
		service, err := backend.NewBackendService(raw, resolve)
		if err == nil {
			service.SetClock(func() time.Time { return now })
			service.SetCaptureIDGenerator(func() (capture.CaptureID, error) { return "capture-integration", nil })
		}
		return service, err
	}
	service, err := NewService(Dependencies{Now: func() time.Time { return now }, GenerateID: ids, SessionRepositories: sessions, CaptureServices: captureFactory})
	if err != nil {
		t.Fatal(err)
	}
	root := testRoot(t)
	initWorkspace(t, service, root)
	if _, err := service.CreateCourse(context.Background(), CourseCreateRequest{Root: root, Name: "Capture Course"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateModule(context.Background(), ModuleCreateRequest{Root: root, CourseRef: "Capture Course", Number: 1, Name: "Capture Module"}); err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateSession(context.Background(), CreateSessionRequest{Root: root, CourseRef: "Capture Course", ModuleRef: "Capture Module", Title: "Capture Session", IdempotencyKey: "capture"})
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.StartSession(context.Background(), updateRequest(lifecycleFixture{root: root}, created))
	if err != nil {
		t.Fatal(err)
	}
	return captureFixture{service: service, root: root, session: started}
}

func captureReq(f captureFixture, revision uint64) CaptureRequest {
	return CaptureRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, ExpectedRevision: revision}
}

func TestSyntheticCaptureEndToEndAndExplicitCompletion(t *testing.T) {
	f := newCaptureFixture(t, nil)
	originalStatus := f.session.Snapshot.SessionStatus
	transcription := f.session.Snapshot.TranscriptionStatus
	publication := f.session.Snapshot.PublicationStatus
	started, err := f.service.StartCapture(context.Background(), StartCaptureRequest{CaptureRequest: captureReq(f, f.session.Revision), Backend: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	if started.CaptureStatus != "recording" || started.Revision != f.session.Revision+1 {
		t.Fatalf("start=%+v", started)
	}
	paused, err := f.service.PauseCapture(context.Background(), captureReq(f, started.Revision))
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := workspace.PathsFromRoot(f.root)
	record, _ := f.service.GetSession(context.Background(), SessionReferenceRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID})
	sessionRoot := filepath.Join(paths.Private, "01 Courses", "Capture Course", "Modules", "01 - Capture Module", "Sessions", record.DirectoryName)
	firstWAV := filepath.Join(sessionRoot, "Segments", "001-audio.wav")
	firstManifest := filepath.Join(sessionRoot, "Segments", "001-segment.json")
	wavBefore, _ := os.ReadFile(firstWAV)
	manifestBefore, _ := os.ReadFile(firstManifest)
	resumed, err := f.service.ResumeCapture(context.Background(), ResumeCaptureRequest{CaptureRequest: captureReq(f, paused.Revision)})
	if err != nil {
		t.Fatal(err)
	}
	wavAfter, _ := os.ReadFile(firstWAV)
	manifestAfter, _ := os.ReadFile(firstManifest)
	if string(wavBefore) != string(wavAfter) || string(manifestBefore) != string(manifestAfter) {
		t.Fatal("resume altered finalized first segment")
	}
	stopped, err := f.service.StopCapture(context.Background(), captureReq(f, resumed.Revision))
	if err != nil {
		t.Fatal(err)
	}
	if stopped.CaptureStatus != "stopped" {
		t.Fatalf("stop=%+v", stopped)
	}
	loaded, err := f.service.GetSession(context.Background(), SessionReferenceRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Snapshot.SessionStatus != originalStatus || loaded.Snapshot.TranscriptionStatus != transcription || loaded.Snapshot.PublicationStatus != publication || len(loaded.Snapshot.Segments) != 2 {
		t.Fatalf("loaded=%+v", loaded)
	}
	for _, name := range []string{"001-audio.wav", "001-segment.json", "002-audio.wav", "002-segment.json"} {
		if _, err := os.Stat(filepath.Join(sessionRoot, "Segments", name)); err != nil {
			t.Errorf("missing %s", name)
		}
	}
	inspection, err := f.service.InspectCapture(context.Background(), InspectCaptureRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, Backend: "synthetic"})
	if err != nil || len(inspection.Finalized) != 2 || len(inspection.Issues) != 0 {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
	completed, err := f.service.CompleteSession(context.Background(), CompleteSessionRequest{UpdateSessionRequest: UpdateSessionRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, ExpectedRevision: loaded.Revision}})
	if err != nil || completed.Snapshot.SessionStatus != "completed" {
		t.Fatalf("complete=%+v %v", completed, err)
	}
}

func TestCaptureStaleRevisionAndConcurrency(t *testing.T) {
	f := newCaptureFixture(t, nil)
	request := StartCaptureRequest{CaptureRequest: captureReq(f, f.session.Revision), Backend: "synthetic"}
	results := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(1)
	for range 2 {
		go func() { start.Wait(); _, err := f.service.StartCapture(context.Background(), request); results <- err }()
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
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

func TestCaptureShutdownPreservesRuntimeAndRequiresRecovery(t *testing.T) {
	f := newCaptureFixture(t, nil)
	started, err := f.service.StartCapture(context.Background(), StartCaptureRequest{CaptureRequest: captureReq(f, f.session.Revision), Backend: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.service.GetSession(context.Background(), SessionReferenceRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.ShutdownCapture(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.GetSession(context.Background(), SessionReferenceRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID})
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision != started.Revision || after.Revision != before.Revision || after.Snapshot.SessionStatus != before.Snapshot.SessionStatus || after.Snapshot.CaptureStatus != before.Snapshot.CaptureStatus {
		t.Fatalf("shutdown mutated runtime: before=%+v after=%+v", before, after)
	}
	inspection, err := f.service.InspectCapture(context.Background(), InspectCaptureRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, Backend: "synthetic"})
	if err != nil || len(inspection.Partial) != 1 || len(inspection.Issues) == 0 || !inspection.Recoverable {
		t.Fatalf("shutdown inspection=%+v err=%v", inspection, err)
	}
}

func TestPauseStopRaceHasOneAuthoritativeOutcome(t *testing.T) {
	f := newCaptureFixture(t, nil)
	started, err := f.service.StartCapture(context.Background(), StartCaptureRequest{CaptureRequest: captureReq(f, f.session.Revision), Backend: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	request := captureReq(f, started.Revision)
	results := make(chan error, 2)
	var gate sync.WaitGroup
	gate.Add(1)
	go func() { gate.Wait(); _, err := f.service.PauseCapture(context.Background(), request); results <- err }()
	go func() { gate.Wait(); _, err := f.service.StopCapture(context.Background(), request); results <- err }()
	gate.Done()
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
	loaded, _ := f.service.GetSession(context.Background(), SessionReferenceRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID})
	if success != 1 || conflict != 1 || loaded.Revision != started.Revision+1 {
		t.Fatalf("success=%d conflict=%d loaded=%+v", success, conflict, loaded)
	}
}

type failingSessionRepository struct {
	SessionRepository
	mu   sync.Mutex
	fail bool
}

func (f *failingSessionRepository) UpdateRuntime(ctx context.Context, r session.Record, u session.RuntimeUpdate) (session.Record, error) {
	f.mu.Lock()
	fail := f.fail
	f.mu.Unlock()
	if fail {
		return session.Record{}, errors.New("synthetic persistence failure")
	}
	return f.SessionRepository.UpdateRuntime(ctx, r, u)
}
func (f *failingSessionRepository) setFail()              { f.mu.Lock(); f.fail = true; f.mu.Unlock() }
func (f *failingSessionRepository) setFailure(value bool) { f.mu.Lock(); f.fail = value; f.mu.Unlock() }

func TestBackendSuccessRuntimeFailureIsUncertainAndInspectable(t *testing.T) {
	var wrapper *failingSessionRepository
	factory := func(paths workspace.Paths, clock session.Clock, id session.IDGenerator) (SessionRepository, error) {
		real, err := session.NewRepository(paths, clock, id)
		if err != nil {
			return nil, err
		}
		wrapper = &failingSessionRepository{SessionRepository: real}
		return wrapper, nil
	}
	f := newCaptureFixture(t, factory)
	wrapper.setFail()
	_, err := f.service.StartCapture(context.Background(), StartCaptureRequest{CaptureRequest: captureReq(f, f.session.Revision), Backend: "synthetic"})
	if !errors.Is(err, ErrCapturePersistenceUncertain) {
		t.Fatalf("error=%v", err)
	}
	inspection, inspectErr := f.service.InspectCapture(context.Background(), InspectCaptureRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, Backend: "synthetic"})
	if inspectErr != nil || len(inspection.Partial) == 0 || len(inspection.Issues) == 0 {
		t.Fatalf("inspection=%+v err=%v", inspection, inspectErr)
	}
}

func TestFinalizingBackendOutcomesSurviveRuntimeFailures(t *testing.T) {
	for _, operation := range []string{"pause", "resume", "stop"} {
		t.Run(operation, func(t *testing.T) {
			var wrapper *failingSessionRepository
			factory := func(paths workspace.Paths, clock session.Clock, id session.IDGenerator) (SessionRepository, error) {
				real, err := session.NewRepository(paths, clock, id)
				wrapper = &failingSessionRepository{SessionRepository: real}
				return wrapper, err
			}
			f := newCaptureFixture(t, factory)
			started, err := f.service.StartCapture(context.Background(), StartCaptureRequest{CaptureRequest: captureReq(f, f.session.Revision), Backend: "synthetic"})
			if err != nil {
				t.Fatal(err)
			}
			var operationErr error
			switch operation {
			case "pause":
				wrapper.setFailure(true)
				_, operationErr = f.service.PauseCapture(context.Background(), captureReq(f, started.Revision))
			case "resume":
				paused, pauseErr := f.service.PauseCapture(context.Background(), captureReq(f, started.Revision))
				if pauseErr != nil {
					t.Fatal(pauseErr)
				}
				wrapper.setFailure(true)
				_, operationErr = f.service.ResumeCapture(context.Background(), ResumeCaptureRequest{CaptureRequest: captureReq(f, paused.Revision)})
			case "stop":
				wrapper.setFailure(true)
				_, operationErr = f.service.StopCapture(context.Background(), captureReq(f, started.Revision))
			}
			if !errors.Is(operationErr, ErrCapturePersistenceUncertain) {
				t.Fatalf("error=%v", operationErr)
			}
			wrapper.setFailure(false)
			inspection, inspectErr := f.service.InspectCapture(context.Background(), InspectCaptureRequest{Root: f.root, CourseRef: f.session.CourseID, ModuleRef: f.session.ModuleID, SessionRef: f.session.ID, Backend: "synthetic"})
			if inspectErr != nil || len(inspection.Issues) == 0 {
				t.Fatalf("inspection=%+v error=%v", inspection, inspectErr)
			}
		})
	}
}

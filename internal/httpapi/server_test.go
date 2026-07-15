package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/application"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
	"github.com/Arameair/studypilot/internal/session"
	"github.com/Arameair/studypilot/internal/studyartifact"
)

type fakeApplication struct {
	Application
	mu             sync.Mutex
	revision       uint64
	dashboardReads atomic.Int64
}

func (f *fakeApplication) GetDashboard(context.Context, application.DashboardRequest) (application.DashboardResult, error) {
	f.dashboardReads.Add(1)
	return application.DashboardResult{Courses: 1, Modules: 1, CourseModules: []application.ModuleSummary{{ID: "module-1", CourseID: "course-1", Name: "Module", Number: 1, Sessions: 1}}, UnfinishedSessions: []application.SessionSummary{{ID: "session-1", CourseID: "course-1", ModuleID: "module-1", Title: "Session", Number: 1, SessionStatus: studyruntime.SessionStatusPlanned, CaptureStatus: studyruntime.CaptureStatusReady, Revision: 1}}}, nil
}
func (f *fakeApplication) ListCourses(context.Context, application.ListCoursesRequest) ([]application.CourseSummary, error) {
	return []application.CourseSummary{{ID: "course-1", Name: "Course", Slug: "course", Modules: 1}}, nil
}
func (f *fakeApplication) ListModules(context.Context, application.ListModulesRequest) ([]application.ModuleSummary, error) {
	return []application.ModuleSummary{{ID: "module-1", CourseID: "course-1", Name: "Module", Slug: "module", Number: 1, Sessions: 1}}, nil
}
func (f *fakeApplication) InspectModuleSessions(context.Context, application.InspectModuleRequest) (application.SessionScanResult, error) {
	return application.SessionScanResult{Sessions: []application.SessionSummary{{ID: "session-1", CourseID: "course-1", ModuleID: "module-1", Title: "Session", Number: 1, SessionStatus: studyruntime.SessionStatusPlanned, CaptureStatus: studyruntime.CaptureStatusReady, Revision: 1}}, Issues: []application.SessionScanIssue{}}, nil
}
func (f *fakeApplication) GetSessionWorkspace(context.Context, application.SessionWorkspaceRequest) (application.SessionWorkspaceResult, error) {
	snapshot := studyruntime.Snapshot{SessionStatus: studyruntime.SessionStatusPlanned, CaptureStatus: studyruntime.CaptureStatusReady, TranscriptionStatus: studyruntime.TranscriptionStatusNotStarted, Segments: []studyruntime.SegmentSummary{}}
	return application.SessionWorkspaceResult{Course: application.CourseSummary{ID: "course-1", Name: "Course", Slug: "course"}, Module: application.ModuleSummary{ID: "module-1", CourseID: "course-1", Name: "Module", Number: 1}, Session: application.SessionResult{ID: "session-1", CourseID: "course-1", ModuleID: "module-1", Title: "Session", Number: 1, Revision: 1, Snapshot: snapshot}, Controls: application.SessionControls{StartSession: true}, Artifacts: []studyartifact.Record{}, ArtifactIssues: []studyartifact.Issue{}}, nil
}
func (f *fakeApplication) StartSession(_ context.Context, request application.UpdateSessionRequest) (application.SessionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.revision == 0 {
		f.revision = 1
	}
	if request.ExpectedRevision != f.revision {
		return application.SessionResult{}, session.ErrSessionConflict
	}
	f.revision++
	return application.SessionResult{ID: "session-1", Revision: f.revision, Snapshot: studyruntime.Snapshot{SessionStatus: studyruntime.SessionStatusActive, CaptureStatus: studyruntime.CaptureStatusReady, TranscriptionStatus: studyruntime.TranscriptionStatusNotStarted}}, nil
}
func (f *fakeApplication) CompleteSession(context.Context, application.CompleteSessionRequest) (application.SessionResult, error) {
	return application.SessionResult{ID: "session-1", Revision: 2, Snapshot: studyruntime.Snapshot{SessionStatus: studyruntime.SessionStatusCompleted, CaptureStatus: studyruntime.CaptureStatusStopped, TranscriptionStatus: studyruntime.TranscriptionStatusNotStarted}}, nil
}
func (f *fakeApplication) InspectCapture(context.Context, application.InspectCaptureRequest) (application.CaptureInspectionResult, error) {
	return application.CaptureInspectionResult{SessionID: "session-1", RuntimeStatus: studyruntime.CaptureStatusReady, Revision: 1, Finalized: []application.CaptureSegmentResult{}, Partial: []application.CaptureSegmentResult{}, Issues: []application.CaptureIssue{}}, nil
}
func (f *fakeApplication) StartCapture(context.Context, application.StartCaptureRequest) (application.CaptureResult, error) {
	return application.CaptureResult{Operation: "capture_start", SessionID: "session-1", CaptureStatus: studyruntime.CaptureStatusRecording, Revision: 2}, nil
}
func (f *fakeApplication) PauseCapture(context.Context, application.CaptureRequest) (application.CaptureResult, error) {
	return application.CaptureResult{Operation: "capture_pause", SessionID: "session-1", CaptureStatus: studyruntime.CaptureStatusPaused, Revision: 2}, nil
}
func (f *fakeApplication) ResumeCapture(context.Context, application.ResumeCaptureRequest) (application.CaptureResult, error) {
	return application.CaptureResult{Operation: "capture_resume", SessionID: "session-1", CaptureStatus: studyruntime.CaptureStatusRecording, Revision: 2}, nil
}
func (f *fakeApplication) StopCapture(context.Context, application.CaptureRequest) (application.CaptureResult, error) {
	return application.CaptureResult{Operation: "capture_stop", SessionID: "session-1", CaptureStatus: studyruntime.CaptureStatusStopped, Revision: 2}, nil
}
func (f *fakeApplication) InspectTranscription(context.Context, application.InspectTranscriptionRequest) (application.TranscriptionInspectionResult, error) {
	return application.TranscriptionInspectionResult{SessionID: "session-1", Revision: 1, AggregateStatus: studyruntime.TranscriptionStatusNotStarted, RuntimeStates: []studyruntime.SegmentTranscriptionState{}, Issues: []application.TranscriptionInspectionIssue{}}, nil
}
func (f *fakeApplication) ExecuteTranscription(context.Context, application.ExecuteTranscriptionRequest) (application.ExecuteTranscriptionResult, error) {
	return application.ExecuteTranscriptionResult{JobID: "transcription-job-11111111111111111111111111111111", SegmentID: "segment-1", JobStatus: "completed", QueueStatus: "terminal", RuntimeRevision: 5, Completed: true}, nil
}
func (f *fakeApplication) ListStudyArtifacts(context.Context, application.ListStudyArtifactsRequest) (application.StudyArtifactListResult, error) {
	return application.StudyArtifactListResult{Revision: 1, Artifacts: []studyartifact.Record{}}, nil
}
func (f *fakeApplication) InspectStudyArtifacts(context.Context, application.InspectStudyArtifactsRequest) (application.StudyArtifactInspectionResult, error) {
	return application.StudyArtifactInspectionResult{Revision: 1, Artifacts: []studyartifact.Record{}, Issues: []studyartifact.Issue{}}, nil
}
func (f *fakeApplication) RefreshStudyArtifactIndex(context.Context, application.RefreshStudyArtifactIndexRequest) (application.StudyArtifactRefreshResult, error) {
	return application.StudyArtifactRefreshResult{Revision: 2, Artifacts: []studyartifact.Record{}, Issues: []studyartifact.Issue{}}, nil
}
func (f *fakeApplication) CreateModuleNotes(context.Context, application.CreateModuleNotesRequest) (application.StudyArtifactMutationResult, error) {
	return application.StudyArtifactMutationResult{Revision: 2}, nil
}
func (f *fakeApplication) CreateSessionNotes(context.Context, application.CreateSessionNotesRequest) (application.StudyArtifactMutationResult, error) {
	return application.StudyArtifactMutationResult{Revision: 2}, nil
}

func newTestHandler(t *testing.T, service Application) http.Handler {
	t.Helper()
	handler, err := New(service, Config{Root: "/private/root", CaptureBackend: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}
func request(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:8765"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestHealthAndSecurityHeaders(t *testing.T) {
	recorder := request(t, newTestHandler(t, &fakeApplication{}), http.MethodGet, "/api/v1/health", "")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"api_version":"v1"`) {
		t.Fatal(recorder.Code, recorder.Body.String())
	}
	for _, header := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Cache-Control"} {
		if recorder.Header().Get(header) == "" {
			t.Errorf("missing %s", header)
		}
	}
	if strings.Contains(recorder.Body.String(), "/private/root") {
		t.Fatal("health leaked root")
	}
}

func TestFrontendIsEmbeddedLocalAndPredictable(t *testing.T) {
	handler := newTestHandler(t, &fakeApplication{})
	index := request(t, handler, http.MethodGet, "/", "")
	script := request(t, handler, http.MethodGet, "/app.js", "")
	missing := request(t, handler, http.MethodGet, "/unknown", "")
	apiMissing := request(t, handler, http.MethodGet, "/api/v1/unknown", "")
	if index.Code != 200 || !strings.Contains(index.Body.String(), "Start recording") || !strings.Contains(index.Body.String(), "Create session notes") {
		t.Fatal(index.Code)
	}
	if script.Code != 200 || strings.Contains(script.Body.String(), "https://") || strings.Contains(script.Body.String(), "http://") || strings.Contains(index.Body.String(), "cdn") {
		t.Fatal("frontend contains external resource")
	}
	if missing.Code != 404 || apiMissing.Code != 404 || strings.Contains(apiMissing.Body.String(), "<!doctype") {
		t.Fatal("unexpected fallback behavior")
	}
}

func TestAPIRouteCoverage(t *testing.T) {
	handler := newTestHandler(t, &fakeApplication{})
	tests := []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/v1/dashboard", "", 200}, {"GET", "/api/v1/courses", "", 200}, {"GET", "/api/v1/courses/course-1/modules", "", 200}, {"GET", "/api/v1/courses/course-1/modules/module-1/sessions", "", 200}, {"GET", "/api/v1/sessions/course-1/module-1/session-1", "", 200},
		{"POST", "/api/v1/sessions/course-1/module-1/session-1/complete", `{"expected_revision":1}`, 200}, {"GET", "/api/v1/sessions/course-1/module-1/session-1/capture", "", 200}, {"POST", "/api/v1/sessions/course-1/module-1/session-1/capture/start", `{"expected_revision":1}`, 200}, {"POST", "/api/v1/sessions/course-1/module-1/session-1/capture/pause", `{"expected_revision":1}`, 200}, {"POST", "/api/v1/sessions/course-1/module-1/session-1/capture/resume", `{"expected_revision":1}`, 200}, {"POST", "/api/v1/sessions/course-1/module-1/session-1/capture/stop", `{"expected_revision":1}`, 200},
		{"GET", "/api/v1/sessions/course-1/module-1/session-1/transcription", "", 200}, {"POST", "/api/v1/sessions/course-1/module-1/session-1/transcription/execute", `{"segment_id":"segment-1","backend":"synthetic","model":"synthetic/deterministic","language":"en","max_attempts":3,"expected_revision":1}`, 200},
		{"GET", "/api/v1/courses/course-1/modules/module-1/artifacts", "", 200}, {"GET", "/api/v1/courses/course-1/modules/module-1/artifacts/inspect", "", 200}, {"POST", "/api/v1/courses/course-1/modules/module-1/artifacts/refresh", `{"expected_artifact_revision":1}`, 200}, {"POST", "/api/v1/courses/course-1/modules/module-1/notes/module", `{"title":"Module Notes","expected_artifact_revision":1}`, 201}, {"POST", "/api/v1/sessions/course-1/module-1/session-1/notes/session", `{"title":"Session Notes","expected_artifact_revision":1}`, 201},
	}
	for _, test := range tests {
		t.Run(test.method+test.path, func(t *testing.T) {
			recorder := request(t, handler, test.method, test.path, test.body)
			if recorder.Code != test.status {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body)
			}
			if strings.Contains(recorder.Body.String(), "/private/root") || strings.Contains(recorder.Body.String(), "transcript body") {
				t.Fatal("private data leak")
			}
		})
	}
}

func TestStrictRequestsSafeErrorsAndOrigin(t *testing.T) {
	handler := newTestHandler(t, &fakeApplication{})
	tests := []struct {
		name, method, path, body, contentType, origin string
		want                                          int
	}{
		{"invalid json", "POST", "/api/v1/sessions/course-1/module-1/session-1/start", "{", "application/json", "", 400},
		{"unknown field", "POST", "/api/v1/sessions/course-1/module-1/session-1/start", `{"expected_revision":1,"secret":"x"}`, "application/json", "", 400},
		{"wrong content type", "POST", "/api/v1/sessions/course-1/module-1/session-1/start", `{"expected_revision":1}`, "text/plain", "", 415},
		{"oversized", "POST", "/api/v1/sessions/course-1/module-1/session-1/start", `{"expected_revision":1,"padding":"` + strings.Repeat("x", maxRequestBody) + `"}`, "application/json", "", 413},
		{"invalid route", "GET", "/api/v1/courses/%2e%2e/modules", "", "", "", 400},
		{"method", "DELETE", "/api/v1/health", "", "", "", 405},
		{"cross origin", "GET", "/api/v1/health", "", "", "http://evil.example", 403},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			req.Host = "127.0.0.1:8765"
			if test.contentType != "" {
				req.Header.Set("Content-Type", test.contentType)
			}
			if test.origin != "" {
				req.Header.Set("Origin", test.origin)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != test.want {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body)
			}
			if !strings.Contains(recorder.Header().Get("Content-Type"), "application/json") {
				t.Fatal("error is not JSON")
			}
		})
	}
}

func TestConcurrentSameRevisionHasOneWinnerAndReadsAreSafe(t *testing.T) {
	service := &fakeApplication{revision: 1}
	handler := newTestHandler(t, service)
	start := make(chan struct{})
	codes := make(chan int, 2)
	for range 2 {
		go func() {
			<-start
			codes <- request(t, handler, "POST", "/api/v1/sessions/course-1/module-1/session-1/start", `{"expected_revision":1}`).Code
		}()
	}
	close(start)
	first, second := <-codes, <-codes
	if !((first == 200 && second == 409) || (first == 409 && second == 200)) {
		t.Fatalf("codes=%d,%d", first, second)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := request(t, handler, "GET", "/api/v1/dashboard", "").Code; got != 200 {
				t.Errorf("status=%d", got)
			}
		}()
	}
	wg.Wait()
	if service.dashboardReads.Load() != 20 {
		t.Fatal(service.dashboardReads.Load())
	}
}

func TestCaptureMutationAndTranscriptionReadAreConcurrentSafe(t *testing.T) {
	handler := newTestHandler(t, &fakeApplication{})
	start := make(chan struct{})
	codes := make(chan int, 40)
	for range 20 {
		go func() {
			<-start
			codes <- request(t, handler, http.MethodPost, "/api/v1/sessions/course-1/module-1/session-1/capture/start", `{"expected_revision":1}`).Code
		}()
		go func() {
			<-start
			codes <- request(t, handler, http.MethodGet, "/api/v1/sessions/course-1/module-1/session-1/transcription", "").Code
		}()
	}
	close(start)
	for range 40 {
		if code := <-codes; code != http.StatusOK {
			t.Fatalf("status=%d", code)
		}
	}
}

func TestApplicationErrorNeverLeaksCause(t *testing.T) {
	service := &errorApplication{fakeApplication: &fakeApplication{}}
	recorder := request(t, newTestHandler(t, service), "GET", "/api/v1/dashboard", "")
	if recorder.Code != 500 || strings.Contains(recorder.Body.String(), "/home/ara/private") || strings.Contains(recorder.Body.String(), "secret transcript") {
		t.Fatal(recorder.Body.String())
	}
}

type errorApplication struct{ *fakeApplication }

func (*errorApplication) GetDashboard(context.Context, application.DashboardRequest) (application.DashboardResult, error) {
	return application.DashboardResult{}, errors.New("/home/ara/private secret transcript")
}

func TestValidateAddress(t *testing.T) {
	for _, valid := range []string{"127.0.0.1:8765", "localhost:0"} {
		if err := ValidateAddress(valid); err != nil {
			t.Error(err)
		}
	}
	for _, invalid := range []string{"0.0.0.0:8765", ":8765", "192.168.1.2:80", "[::1]:8765", "127.0.0.1:99999"} {
		if ValidateAddress(invalid) == nil {
			t.Errorf("accepted %s", invalid)
		}
	}
}

func TestNewRejectsUnsafeExecutionConfiguration(t *testing.T) {
	tests := []Config{
		{TranscriptionBackend: "remote"},
		{TranscriptionBackend: "synthetic", TranscriptionModel: "/private/model"},
		{CaptureBackend: "system-command"},
	}
	for _, config := range tests {
		if _, err := New(&fakeApplication{}, config); err == nil {
			t.Fatalf("accepted unsafe configuration: %+v", config)
		}
	}
}

func TestShutdownCancelsActiveRequest(t *testing.T) {
	started := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() })
	serverConn, clientConn := net.Pipe()
	listener := newSingleConnListener(serverConn)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- Serve(ctx, listener, handler) }()
	_, _ = io.WriteString(clientConn, "GET / HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	_ = clientConn.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = bufio.NewReader(clientConn).ReadString('\n')
	_ = clientConn.Close()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
}

type singleConnListener struct {
	conn     net.Conn
	closed   chan struct{}
	once     sync.Once
	accepted atomic.Bool
}

func newSingleConnListener(conn net.Conn) *singleConnListener {
	return &singleConnListener{conn: conn, closed: make(chan struct{})}
}
func (l *singleConnListener) Accept() (net.Conn, error) {
	if !l.accepted.Swap(true) {
		return l.conn, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}
func (l *singleConnListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *singleConnListener) Addr() net.Addr { return fakeAddress("127.0.0.1:0") }

type fakeAddress string

func (fakeAddress) Network() string  { return "tcp" }
func (a fakeAddress) String() string { return string(a) }

func TestResponsesAreJSONDocuments(t *testing.T) {
	rec := request(t, newTestHandler(t, &fakeApplication{}), "GET", "/api/v1/dashboard", "")
	var value map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(value["courses"]) != "1" {
		t.Fatal(value)
	}
}

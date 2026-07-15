package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/application"
	"github.com/Arameair/studypilot/internal/capture"
	capturebackend "github.com/Arameair/studypilot/internal/capture/backend"
	"github.com/Arameair/studypilot/internal/workspace"
)

func TestSyntheticHTTPWorkflowThroughApplicationLayer(t *testing.T) {
	root := filepath.Join(t.TempDir(), "StudyPilot")
	var idMu sync.Mutex
	nextID := 0
	clockTime := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		idMu.Lock()
		defer idMu.Unlock()
		clockTime = clockTime.Add(time.Second)
		return clockTime
	}
	generate := func(kind string) (string, error) {
		idMu.Lock()
		defer idMu.Unlock()
		nextID++
		return fmt.Sprintf("%s-%032x", kind, nextID), nil
	}
	captureFactory := func(paths workspace.Paths, name string, resolve func(string) (string, error)) (application.CaptureService, error) {
		if name != "synthetic" {
			return nil, capture.NewError(capture.ErrorUnavailable, capture.OpStart, false, capture.OutcomeNotStarted, "capture backend is unavailable", nil)
		}
		backend, err := capturebackend.NewSyntheticBackend(capturebackend.SyntheticConfig{Paths: paths, Clock: clock})
		if err != nil {
			return nil, err
		}
		service, serviceErr := capturebackend.NewBackendService(backend, resolve)
		if serviceErr == nil {
			service.SetClock(clock)
		}
		return service, serviceErr
	}
	service, err := application.NewService(application.Dependencies{Now: clock, GenerateID: generate, CaptureServices: captureFactory, TranscriptionExecution: application.TranscriptionExecutionConfig{BackendName: "synthetic", ModelID: "synthetic/deterministic", Timeout: time.Minute}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = service.InitializeWorkspace(ctx, application.WorkspaceRequest{Root: root}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateCourse(ctx, application.CourseCreateRequest{Root: root, Name: "HTTP Course"}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateModule(ctx, application.ModuleCreateRequest{Root: root, CourseRef: "HTTP Course", Number: 1, Name: "HTTP Module"}); err != nil {
		t.Fatal(err)
	}
	handler, err := New(service, Config{Root: root, CaptureBackend: "synthetic", TranscriptionBackend: "synthetic", TranscriptionModel: "synthetic/deterministic"})
	if err != nil {
		t.Fatal(err)
	}
	courses, err := service.ListCourses(ctx, application.ListCoursesRequest{Root: root})
	if err != nil || len(courses) != 1 {
		t.Fatal(err, courses)
	}
	modules, err := service.ListModules(ctx, application.ListModulesRequest{Root: root, CourseRef: courses[0].ID})
	if err != nil || len(modules) != 1 {
		t.Fatal(err, modules)
	}
	createdResponse := request(t, handler, http.MethodPost, "/api/v1/courses/"+courses[0].ID+"/modules/"+modules[0].ID+"/sessions", `{"title":"HTTP Session"}`)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createdResponse.Code, createdResponse.Body)
	}
	var created struct {
		ID       string `json:"id"`
		CourseID string `json:"course_id"`
		ModuleID string `json:"module_id"`
		Revision uint64 `json:"revision"`
	}
	if err = json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil || created.ID == "" || created.Revision != 1 {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	base := "/api/v1/sessions/" + created.CourseID + "/" + created.ModuleID + "/" + created.ID
	steps := []struct {
		path, body string
		want       int
	}{
		{base + "/start", `{"expected_revision":1}`, 200},
	}
	for _, step := range steps {
		response := request(t, handler, http.MethodPost, step.path, step.body)
		if response.Code != step.want {
			t.Fatalf("%s status=%d body=%s", step.path, response.Code, response.Body)
		}
	}
	captureCodes := concurrentResponses(t, handler, http.MethodPost, base+"/capture/start", `{"expected_revision":2}`)
	assertOneWinner(t, captureCodes, http.StatusOK, http.StatusConflict)
	for _, step := range []struct{ path, body string }{{base + "/capture/pause", `{"expected_revision":3}`}, {base + "/capture/resume", `{"expected_revision":4}`}, {base + "/capture/stop", `{"expected_revision":5}`}} {
		response := request(t, handler, http.MethodPost, step.path, step.body)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", step.path, response.Code, response.Body)
		}
	}
	workspaceResponse := request(t, handler, http.MethodGet, base, "")
	var workspaceDocument struct {
		Session struct {
			Revision uint64 `json:"revision"`
			Segments []struct {
				ID string `json:"id"`
			} `json:"segments"`
		} `json:"session"`
	}
	if err = json.Unmarshal(workspaceResponse.Body.Bytes(), &workspaceDocument); err != nil || len(workspaceDocument.Session.Segments) != 2 || workspaceDocument.Session.Revision != 6 {
		t.Fatalf("workspace=%s err=%v", workspaceResponse.Body, err)
	}
	var executedResponses []*httptest.ResponseRecorder
	firstBody := fmt.Sprintf(`{"segment_id":%q,"backend":"synthetic","model":"synthetic/deterministic","language":"en","max_attempts":3,"expected_revision":6}`, workspaceDocument.Session.Segments[0].ID)
	firstExecutions := concurrentRecorders(t, handler, http.MethodPost, base+"/transcription/execute", firstBody)
	assertOneWinner(t, []int{firstExecutions[0].Code, firstExecutions[1].Code}, http.StatusOK, http.StatusConflict)
	executedResponses = append(executedResponses, firstExecutions...)
	revision := uint64(10)
	for _, segment := range workspaceDocument.Session.Segments[1:] {
		executeBody := fmt.Sprintf(`{"segment_id":%q,"backend":"synthetic","model":"synthetic/deterministic","language":"en","max_attempts":3,"expected_revision":%d}`, segment.ID, revision)
		executed := request(t, handler, http.MethodPost, base+"/transcription/execute", executeBody)
		if executed.Code != http.StatusOK || !jsonFieldBool(t, executed.Body.Bytes(), "completed") {
			inspection, inspectErr := service.InspectTranscription(ctx, application.InspectTranscriptionRequest{Root: root, CourseRef: created.CourseID, ModuleRef: created.ModuleID, SessionRef: created.ID})
			t.Fatalf("execute status=%d body=%s inspection=%+v inspectErr=%v", executed.Code, executed.Body, inspection, inspectErr)
		}
		executedResponses = append(executedResponses, executed)
		revision += 4
	}
	artifactBase := "/api/v1/courses/" + created.CourseID + "/modules/" + created.ModuleID
	refreshed := request(t, handler, http.MethodPost, artifactBase+"/artifacts/refresh", `{"expected_artifact_revision":0}`)
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshed.Code, refreshed.Body)
	}
	noteResponses := concurrentRecorders(t, handler, http.MethodPost, base+"/notes/session", `{"title":"Session Notes","expected_artifact_revision":1}`)
	assertOneWinner(t, []int{noteResponses[0].Code, noteResponses[1].Code}, http.StatusCreated, http.StatusConflict)
	refreshResponses := concurrentRecorders(t, handler, http.MethodPost, artifactBase+"/artifacts/refresh", `{"expected_artifact_revision":2}`)
	assertOneWinner(t, []int{refreshResponses[0].Code, refreshResponses[1].Code}, http.StatusOK, http.StatusConflict)
	completed := request(t, handler, http.MethodPost, base+"/complete", `{"expected_revision":14}`)
	if completed.Code != http.StatusOK || !strings.Contains(completed.Body.String(), `"session_status":"completed"`) {
		t.Fatalf("complete status=%d body=%s", completed.Code, completed.Body)
	}
	listed := request(t, handler, http.MethodGet, artifactBase+"/artifacts", "")
	if listed.Code != http.StatusOK || !containsJSONArtifactTypes(t, listed.Body.Bytes(), "transcript", "note") {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body)
	}
	responses := []*httptest.ResponseRecorder{createdResponse, workspaceResponse, refreshed, completed, listed}
	responses = append(responses, noteResponses...)
	responses = append(responses, refreshResponses...)
	responses = append(responses, executedResponses...)
	for _, response := range responses {
		if strings.Contains(response.Body.String(), root) || strings.Contains(response.Body.String(), "Synthetic transcription") {
			t.Fatal("HTTP workflow leaked private path or transcript text")
		}
	}
}

func concurrentResponses(t *testing.T, handler http.Handler, method, path, body string) []int {
	recorders := concurrentRecorders(t, handler, method, path, body)
	return []int{recorders[0].Code, recorders[1].Code}
}

func concurrentRecorders(t *testing.T, handler http.Handler, method, path, body string) []*httptest.ResponseRecorder {
	t.Helper()
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() { <-start; results <- request(t, handler, method, path, body) }()
	}
	close(start)
	return []*httptest.ResponseRecorder{<-results, <-results}
}

func assertOneWinner(t *testing.T, codes []int, winner, conflict int) {
	t.Helper()
	if !((codes[0] == winner && codes[1] == conflict) || (codes[1] == winner && codes[0] == conflict)) {
		t.Fatalf("expected one winner and one conflict, got %v", codes)
	}
}

func jsonFieldBool(t *testing.T, data []byte, name string) bool {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	result, _ := value[name].(bool)
	return result
}

func containsJSONArtifactTypes(t *testing.T, data []byte, expected ...string) bool {
	t.Helper()
	var value struct {
		Artifacts []struct {
			Type string `json:"type"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, artifact := range value.Artifacts {
		found[artifact.Type] = true
	}
	for _, kind := range expected {
		if !found[kind] {
			return false
		}
	}
	return true
}

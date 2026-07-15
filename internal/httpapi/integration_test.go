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
	created, err := service.CreateSession(ctx, application.CreateSessionRequest{Root: root, CourseRef: "HTTP Course", ModuleRef: "HTTP Module", Title: "HTTP Session"})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(service, Config{Root: root, CaptureBackend: "synthetic", TranscriptionBackend: "synthetic", TranscriptionModel: "synthetic/deterministic"})
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/sessions/" + created.CourseID + "/" + created.ModuleID + "/" + created.ID
	steps := []struct {
		path, body string
		want       int
	}{
		{base + "/start", `{"expected_revision":1}`, 200},
		{base + "/capture/start", `{"expected_revision":2}`, 200},
		{base + "/capture/pause", `{"expected_revision":3}`, 200},
		{base + "/capture/stop", `{"expected_revision":4}`, 200},
	}
	for _, step := range steps {
		response := request(t, handler, http.MethodPost, step.path, step.body)
		if response.Code != step.want {
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
	if err = json.Unmarshal(workspaceResponse.Body.Bytes(), &workspaceDocument); err != nil || len(workspaceDocument.Session.Segments) != 1 || workspaceDocument.Session.Revision != 5 {
		t.Fatalf("workspace=%s err=%v", workspaceResponse.Body, err)
	}
	executeBody := fmt.Sprintf(`{"segment_id":%q,"backend":"synthetic","model":"synthetic/deterministic","language":"en","max_attempts":3,"expected_revision":5}`, workspaceDocument.Session.Segments[0].ID)
	executed := request(t, handler, http.MethodPost, base+"/transcription/execute", executeBody)
	if executed.Code != http.StatusOK || !jsonFieldBool(t, executed.Body.Bytes(), "completed") {
		inspection, inspectErr := service.InspectTranscription(ctx, application.InspectTranscriptionRequest{Root: root, CourseRef: created.CourseID, ModuleRef: created.ModuleID, SessionRef: created.ID})
		t.Fatalf("execute status=%d body=%s inspection=%+v inspectErr=%v", executed.Code, executed.Body, inspection, inspectErr)
	}
	artifactBase := "/api/v1/courses/" + created.CourseID + "/modules/" + created.ModuleID
	refreshed := request(t, handler, http.MethodPost, artifactBase+"/artifacts/refresh", `{"expected_artifact_revision":0}`)
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshed.Code, refreshed.Body)
	}
	notes := request(t, handler, http.MethodPost, base+"/notes/session", `{"title":"Session Notes","expected_artifact_revision":1}`)
	if notes.Code != http.StatusCreated {
		t.Fatalf("notes status=%d body=%s", notes.Code, notes.Body)
	}
	listed := request(t, handler, http.MethodGet, artifactBase+"/artifacts", "")
	if listed.Code != http.StatusOK || !containsJSONArtifactTypes(t, listed.Body.Bytes(), "transcript", "note") {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body)
	}
	for _, response := range []*httptest.ResponseRecorder{workspaceResponse, executed, refreshed, notes, listed} {
		if strings.Contains(response.Body.String(), root) || strings.Contains(response.Body.String(), "Synthetic transcription") {
			t.Fatal("HTTP workflow leaked private path or transcript text")
		}
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

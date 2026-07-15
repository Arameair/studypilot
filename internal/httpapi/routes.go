package httpapi

import (
	"net/http"
	"strings"

	"github.com/Arameair/studypilot/internal/application"
)

type revisionRequest struct {
	ExpectedRevision uint64 `json:"expected_revision"`
}
type artifactRevisionRequest struct {
	ExpectedRevision uint64 `json:"expected_artifact_revision"`
}
type noteRequest struct {
	Title            string `json:"title"`
	ExpectedRevision uint64 `json:"expected_artifact_revision"`
}
type createSessionRequest struct {
	Title string `json:"title"`
}
type transcriptionRequest struct {
	SegmentID        string `json:"segment_id"`
	Backend          string `json:"backend"`
	Model            string `json:"model"`
	Language         string `json:"language"`
	MaxAttempts      int    `json:"max_attempts"`
	ExpectedRevision uint64 `json:"expected_revision"`
}

func (a *api) serveAPI(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/"), "/")
	if len(parts) == 1 {
		switch parts[0] {
		case "health":
			if requireMethod(w, r, http.MethodGet) {
				writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "api_version": APIVersion})
			}
			return
		case "dashboard":
			a.dashboard(w, r)
			return
		case "courses":
			a.courses(w, r)
			return
		}
	}
	if len(parts) >= 2 {
		for _, value := range parts[1:] {
			if !safeReference(value) {
				writeError(w, http.StatusBadRequest, "invalid_input", "A route identifier is invalid.", false)
				return
			}
		}
	}
	if len(parts) == 3 && parts[0] == "courses" && parts[2] == "modules" {
		a.modules(w, r, parts[1])
		return
	}
	if len(parts) == 5 && parts[0] == "courses" && parts[2] == "modules" && parts[4] == "sessions" {
		a.moduleSessions(w, r, parts[1], parts[3])
		return
	}
	if len(parts) == 5 && parts[0] == "courses" && parts[2] == "modules" && parts[4] == "workspace" {
		a.moduleWorkspace(w, r, parts[1], parts[3])
		return
	}
	if len(parts) >= 4 && parts[0] == "sessions" {
		a.sessionRoutes(w, r, parts)
		return
	}
	if len(parts) >= 5 && parts[0] == "courses" && parts[2] == "modules" {
		a.moduleRoutes(w, r, parts)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "The requested API endpoint does not exist.", false)
}

func (a *api) dashboard(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	result, err := a.application.GetDashboard(r.Context(), application.DashboardRequest{Root: a.config.Root})
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dashboardDTO(result))
}
func (a *api) courses(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	result, err := a.application.ListCourses(r.Context(), application.ListCoursesRequest{Root: a.config.Root})
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"courses": courseDTOs(result)})
}
func (a *api) modules(w http.ResponseWriter, r *http.Request, course string) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	result, err := a.application.ListModules(r.Context(), application.ListModulesRequest{Root: a.config.Root, CourseRef: course})
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"modules": moduleDTOs(result)})
}
func (a *api) moduleSessions(w http.ResponseWriter, r *http.Request, course, module string) {
	switch r.Method {
	case http.MethodGet:
		result, err := a.application.InspectModuleSessions(r.Context(), application.InspectModuleRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module})
		if err != nil {
			writeApplicationError(w, err)
			return
		}
		sessions := make([]sessionSummaryDTO, 0, len(result.Sessions))
		for _, value := range result.Sessions {
			sessions = append(sessions, newSessionSummaryDTO(value))
		}
		issues := sessionScanIssueDTOs(result.Issues)
		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions, "issues": issues})
	case http.MethodPost:
		var request createSessionRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		result, err := a.application.CreateSession(r.Context(), application.CreateSessionRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module, Title: request.Title})
		if err != nil {
			writeApplicationError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, newSessionDTO(result))
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "The HTTP method is not allowed for this endpoint.", false)
	}
}

func (a *api) moduleWorkspace(w http.ResponseWriter, r *http.Request, course, module string) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	result, err := a.application.GetModuleWorkspace(r.Context(), application.ModuleWorkspaceRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module})
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, moduleWorkspaceDTO(result))
}

func (a *api) sessionRoutes(w http.ResponseWriter, r *http.Request, parts []string) {
	course, module, session := parts[1], parts[2], parts[3]
	if len(parts) == 4 {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		result, err := a.application.GetSessionWorkspace(r.Context(), application.SessionWorkspaceRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module, SessionRef: session})
		if err != nil {
			writeApplicationError(w, err)
			return
		}
		response := workspaceDTO(result)
		available := a.config.TranscriptionBackend != "" && a.config.TranscriptionModel != ""
		status, issue := "ready", ""
		if !available {
			status, issue = "unavailable", "Transcription is not configured for this GUI process."
		}
		response["transcription_execution"] = map[string]any{"available": available, "backend": a.config.TranscriptionBackend, "model": a.config.TranscriptionModel, "status": status, "issue": issue}
		writeJSON(w, http.StatusOK, response)
		return
	}
	if len(parts) == 5 && (parts[4] == "start" || parts[4] == "complete") {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		var request revisionRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		if request.ExpectedRevision == 0 {
			writeError(w, http.StatusBadRequest, "invalid_input", "A positive expected_revision is required.", false)
			return
		}
		base := application.UpdateSessionRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module, SessionRef: session, ExpectedRevision: request.ExpectedRevision}
		var result application.SessionResult
		var err error
		if parts[4] == "start" {
			result, err = a.application.StartSession(r.Context(), base)
		} else {
			result, err = a.application.CompleteSession(r.Context(), application.CompleteSessionRequest{UpdateSessionRequest: base})
		}
		if err != nil {
			writeApplicationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, newSessionDTO(result))
		return
	}
	if len(parts) >= 5 && parts[4] == "capture" {
		a.captureRoutes(w, r, course, module, session, parts)
		return
	}
	if len(parts) >= 5 && parts[4] == "transcription" {
		a.transcriptionRoutes(w, r, course, module, session, parts)
		return
	}
	if len(parts) == 6 && parts[4] == "notes" && parts[5] == "session" {
		a.createSessionNotes(w, r, course, module, session)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "The requested API endpoint does not exist.", false)
}

func (a *api) captureRoutes(w http.ResponseWriter, r *http.Request, course, module, session string, parts []string) {
	if len(parts) == 5 {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		result, err := a.application.InspectCapture(r.Context(), application.InspectCaptureRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module, SessionRef: session, Backend: a.config.CaptureBackend})
		if err != nil {
			writeApplicationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, captureInspectionDTO(result))
		return
	}
	if len(parts) != 6 || (parts[5] != "start" && parts[5] != "pause" && parts[5] != "resume" && parts[5] != "stop") {
		writeError(w, http.StatusNotFound, "not_found", "The requested API endpoint does not exist.", false)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var request revisionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.ExpectedRevision == 0 {
		writeError(w, http.StatusBadRequest, "invalid_input", "A positive expected_revision is required.", false)
		return
	}
	base := application.CaptureRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module, SessionRef: session, ExpectedRevision: request.ExpectedRevision}
	var result application.CaptureResult
	var err error
	switch parts[5] {
	case "start":
		result, err = a.application.StartCapture(r.Context(), application.StartCaptureRequest{CaptureRequest: base, Backend: a.config.CaptureBackend, DeviceID: a.config.CaptureDevice})
	case "pause":
		result, err = a.application.PauseCapture(r.Context(), base)
	case "resume":
		result, err = a.application.ResumeCapture(r.Context(), application.ResumeCaptureRequest{CaptureRequest: base, DeviceID: a.config.CaptureDevice})
	case "stop":
		result, err = a.application.StopCapture(r.Context(), base)
	}
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, captureResultDTO(result))
}

func (a *api) transcriptionRoutes(w http.ResponseWriter, r *http.Request, course, module, session string, parts []string) {
	if len(parts) == 5 {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		result, err := a.application.InspectTranscription(r.Context(), application.InspectTranscriptionRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module, SessionRef: session})
		if err != nil {
			writeApplicationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, transcriptionInspectionDTO(result))
		return
	}
	if len(parts) != 6 || parts[5] != "execute" {
		writeError(w, http.StatusNotFound, "not_found", "The requested API endpoint does not exist.", false)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var request transcriptionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if !safeReference(request.SegmentID) || !safeReference(request.Backend) || !safeModelReference(request.Model) || request.ExpectedRevision == 0 || request.MaxAttempts < 1 || request.MaxAttempts > 10 || (request.Language != "" && !safeReference(request.Language)) || len(request.Language) > 16 {
		writeError(w, http.StatusBadRequest, "invalid_input", "The transcription request is invalid.", false)
		return
	}
	result, err := a.application.ExecuteTranscription(r.Context(), application.ExecuteTranscriptionRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module, SessionRef: session, SegmentID: request.SegmentID, Backend: request.Backend, Model: request.Model, Language: request.Language, MaxAttempts: request.MaxAttempts, ExpectedRevision: request.ExpectedRevision})
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, executeTranscriptionDTO(result))
}

func (a *api) moduleRoutes(w http.ResponseWriter, r *http.Request, parts []string) {
	course, module := parts[1], parts[3]
	if parts[4] == "artifacts" {
		if len(parts) == 5 {
			if !requireMethod(w, r, http.MethodGet) {
				return
			}
			result, err := a.application.ListStudyArtifacts(r.Context(), application.ListStudyArtifactsRequest{StudyArtifactModuleRequest: application.StudyArtifactModuleRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module}})
			if err != nil {
				writeApplicationError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"revision": result.Revision, "artifacts": result.Artifacts})
			return
		}
		if len(parts) == 6 && parts[5] == "inspect" {
			if !requireMethod(w, r, http.MethodGet) {
				return
			}
			result, err := a.application.InspectStudyArtifacts(r.Context(), application.InspectStudyArtifactsRequest{StudyArtifactModuleRequest: application.StudyArtifactModuleRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module}})
			if err != nil {
				writeApplicationError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"revision": result.Revision, "artifacts": result.Artifacts, "issues": result.Issues})
			return
		}
		if len(parts) == 6 && parts[5] == "refresh" {
			if !requireMethod(w, r, http.MethodPost) {
				return
			}
			var request artifactRevisionRequest
			if !decodeJSON(w, r, &request) {
				return
			}
			result, err := a.application.RefreshStudyArtifactIndex(r.Context(), application.RefreshStudyArtifactIndexRequest{StudyArtifactModuleRequest: application.StudyArtifactModuleRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module}, ExpectedArtifactRevision: request.ExpectedRevision})
			if err != nil {
				writeApplicationError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"revision": result.Revision, "artifacts": result.Artifacts, "issues": result.Issues})
			return
		}
	}
	if len(parts) == 6 && parts[4] == "notes" && parts[5] == "module" {
		a.createModuleNotes(w, r, course, module)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "The requested API endpoint does not exist.", false)
}

func (a *api) createModuleNotes(w http.ResponseWriter, r *http.Request, course, module string) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var request noteRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := a.application.CreateModuleNotes(r.Context(), application.CreateModuleNotesRequest{StudyArtifactModuleRequest: application.StudyArtifactModuleRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module}, Title: request.Title, ExpectedArtifactRevision: request.ExpectedRevision})
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, artifactMutationDTO(result))
}
func (a *api) createSessionNotes(w http.ResponseWriter, r *http.Request, course, module, session string) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var request noteRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := a.application.CreateSessionNotes(r.Context(), application.CreateSessionNotesRequest{StudyArtifactModuleRequest: application.StudyArtifactModuleRequest{Root: a.config.Root, CourseRef: course, ModuleRef: module}, SessionRef: session, Title: request.Title, ExpectedArtifactRevision: request.ExpectedRevision})
	if err != nil {
		writeApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, artifactMutationDTO(result))
}

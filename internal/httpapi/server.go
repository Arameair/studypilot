// Package httpapi exposes StudyPilot's application service through a
// loopback-only, versioned, same-origin HTTP boundary.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Arameair/studypilot/internal/application"
	"github.com/Arameair/studypilot/internal/gui"
)

const (
	DefaultAddress = "127.0.0.1:8765"
	APIVersion     = "v1"
	maxRequestBody = 16 << 10
)

type Application interface {
	GetDashboard(context.Context, application.DashboardRequest) (application.DashboardResult, error)
	ListCourses(context.Context, application.ListCoursesRequest) ([]application.CourseSummary, error)
	ListModules(context.Context, application.ListModulesRequest) ([]application.ModuleSummary, error)
	InspectModuleSessions(context.Context, application.InspectModuleRequest) (application.SessionScanResult, error)
	GetSessionWorkspace(context.Context, application.SessionWorkspaceRequest) (application.SessionWorkspaceResult, error)
	StartSession(context.Context, application.UpdateSessionRequest) (application.SessionResult, error)
	CompleteSession(context.Context, application.CompleteSessionRequest) (application.SessionResult, error)
	InspectCapture(context.Context, application.InspectCaptureRequest) (application.CaptureInspectionResult, error)
	StartCapture(context.Context, application.StartCaptureRequest) (application.CaptureResult, error)
	PauseCapture(context.Context, application.CaptureRequest) (application.CaptureResult, error)
	ResumeCapture(context.Context, application.ResumeCaptureRequest) (application.CaptureResult, error)
	StopCapture(context.Context, application.CaptureRequest) (application.CaptureResult, error)
	InspectTranscription(context.Context, application.InspectTranscriptionRequest) (application.TranscriptionInspectionResult, error)
	ExecuteTranscription(context.Context, application.ExecuteTranscriptionRequest) (application.ExecuteTranscriptionResult, error)
	ListStudyArtifacts(context.Context, application.ListStudyArtifactsRequest) (application.StudyArtifactListResult, error)
	InspectStudyArtifacts(context.Context, application.InspectStudyArtifactsRequest) (application.StudyArtifactInspectionResult, error)
	RefreshStudyArtifactIndex(context.Context, application.RefreshStudyArtifactIndexRequest) (application.StudyArtifactRefreshResult, error)
	CreateModuleNotes(context.Context, application.CreateModuleNotesRequest) (application.StudyArtifactMutationResult, error)
	CreateSessionNotes(context.Context, application.CreateSessionNotesRequest) (application.StudyArtifactMutationResult, error)
}

type Config struct {
	Root, CaptureBackend, CaptureDevice, TranscriptionBackend, TranscriptionModel string
}

type api struct {
	application Application
	config      Config
	frontend    http.Handler
}

func New(applicationService Application, config Config) (http.Handler, error) {
	if applicationService == nil {
		return nil, errors.New("httpapi: application service is required")
	}
	if config.CaptureBackend == "" {
		config.CaptureBackend = "synthetic"
	}
	if config.CaptureBackend != "synthetic" {
		return nil, errors.New("httpapi: unsupported capture backend")
	}
	if config.TranscriptionBackend != "" && config.TranscriptionBackend != "synthetic" && config.TranscriptionBackend != "faster-whisper" {
		return nil, errors.New("httpapi: unsupported transcription backend")
	}
	if config.TranscriptionModel != "" && !safeModelReference(config.TranscriptionModel) {
		return nil, errors.New("httpapi: unsafe transcription model identity")
	}
	handler := &api{application: applicationService, config: config, frontend: gui.Handler()}
	return handler.security(handler.dispatch), nil
}

func (a *api) dispatch(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			writeError(w, http.StatusNotFound, "not_found", "The requested API endpoint does not exist.", false)
			return
		}
		a.serveAPI(w, r)
		return
	}
	a.frontend.ServeHTTP(w, r)
}

func (a *api) security(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
			writeError(w, http.StatusForbidden, "unsafe", "Cross-origin requests are not allowed.", false)
			return
		}
		if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
			writeError(w, http.StatusForbidden, "unsafe", "Cross-origin requests are not allowed.", false)
			return
		}
		next(w, r)
	})
}

func sameOrigin(rawOrigin, host string) bool {
	parsed, err := url.Parse(rawOrigin)
	return err == nil && parsed.Scheme == "http" && strings.EqualFold(parsed.Host, host) && parsed.User == nil
}

// ValidateAddress accepts only explicit IPv4 loopback or localhost bindings.
func ValidateAddress(address string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || (host != "127.0.0.1" && host != "localhost") {
		return errors.New("GUI address must use 127.0.0.1 or localhost")
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 0 || value > 65535 {
		return errors.New("GUI address has an invalid port")
	}
	return nil
}

// Listen creates an IPv4 loopback listener after validating the address.
func Listen(address string) (net.Listener, error) {
	if err := ValidateAddress(address); err != nil {
		return nil, err
	}
	host, port, _ := net.SplitHostPort(address)
	if host == "localhost" {
		host = "127.0.0.1"
	}
	return net.Listen("tcp4", net.JoinHostPort(host, port))
}

// Serve runs until cancellation or failure. Cancellation stops acceptance,
// cancels active request contexts, and performs a bounded graceful shutdown.
func Serve(ctx context.Context, listener net.Listener, handler http.Handler) error {
	if ctx == nil || listener == nil || handler == nil {
		return errors.New("httpapi: context, listener, and handler are required")
	}
	requestContext, cancelRequests := context.WithCancel(context.Background())
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		BaseContext:       func(net.Listener) context.Context { return requestContext },
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cancelRequests()
			shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownContext)
		case <-done:
		}
	}()
	err := server.Serve(listener)
	close(done)
	cancelRequests()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("serve GUI: %w", err)
}

// Package application provides UI-neutral use cases for StudyPilot. It is the
// single orchestration path shared by every interface (CLI today; tray or GUI
// later): it resolves workspace paths, builds deterministic filesystem plans
// through the domain packages, executes them safely, and returns typed results
// and errors. It never prints, never reads command-line flags, and never calls
// os.Exit; those concerns belong to the calling adapter.
//
// A Service holds immutable dependencies and a mutex-protected repository cache,
// so calls sharing one workspace also share its in-process mutation locks.
package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Arameair/studypilot/internal/capture"
	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/session"
	"github.com/Arameair/studypilot/internal/transcription"
	"github.com/Arameair/studypilot/internal/workspace"
)

// Dependencies are the injectable collaborators a Service needs. Both fields are
// required; production wiring supplies time.Now and course.DefaultIDGenerator,
// while tests supply fixed clocks and deterministic or failing ID generators.
type Dependencies struct {
	Now                    func() time.Time
	GenerateID             course.IDGenerator
	SessionRepositories    SessionRepositoryFactory
	CaptureServices        CaptureServiceFactory
	TranscriptionQueues    TranscriptionQueueFactory
	TranscriptionServices  TranscriptionServiceFactory
	TranscriptionExecution TranscriptionExecutionConfig
	TranscriptionBackends  TranscriptionBackendFactory
	TranscriptionStores    TranscriptionArtifactStoreFactory
}

// Service exposes StudyPilot's shared application use cases.
type Service struct {
	now                        func() time.Time
	generateID                 course.IDGenerator
	sessionRepositories        SessionRepositoryFactory
	sessionMu                  sync.Mutex
	sessionByRoot              map[string]SessionRepository
	captureServices            CaptureServiceFactory
	captureByRoot              map[string]capture.Service
	captureRoots               map[string]string
	transcriptionQueues        TranscriptionQueueFactory
	transcriptionServices      TranscriptionServiceFactory
	transcriptionCacheMu       sync.Mutex
	transcriptionMutationMu    sync.Mutex
	transcriptionQueueByRoot   map[string]transcription.Queue
	transcriptionServiceByRoot map[string]transcription.Service
	transcriptionExecution     TranscriptionExecutionConfig
	transcriptionBackends      TranscriptionBackendFactory
	transcriptionStores        TranscriptionArtifactStoreFactory
}

// NewService constructs a Service, rejecting missing required dependencies.
func NewService(deps Dependencies) (*Service, error) {
	if deps.Now == nil {
		return nil, errors.New("application: Now dependency is required")
	}
	if deps.GenerateID == nil {
		return nil, errors.New("application: GenerateID dependency is required")
	}
	factory := deps.SessionRepositories
	if factory == nil {
		factory = defaultSessionRepositoryFactory
	}
	captureFactory := deps.CaptureServices
	if captureFactory == nil {
		captureFactory = defaultCaptureServiceFactory
	}
	queueFactory := deps.TranscriptionQueues
	if queueFactory == nil {
		queueFactory = defaultTranscriptionQueueFactory
	}
	transcriptionFactory := deps.TranscriptionServices
	if transcriptionFactory == nil {
		transcriptionFactory = configuredTranscriptionServiceFactory(deps.TranscriptionExecution, deps.Now)
	}
	backendFactory := deps.TranscriptionBackends
	if backendFactory == nil {
		backendFactory = defaultTranscriptionBackendFactory
	}
	storeFactory := deps.TranscriptionStores
	if storeFactory == nil {
		storeFactory = defaultTranscriptionArtifactStoreFactory
	}
	return &Service{now: deps.Now, generateID: deps.GenerateID, sessionRepositories: factory, sessionByRoot: make(map[string]SessionRepository), captureServices: captureFactory, captureByRoot: make(map[string]capture.Service), captureRoots: make(map[string]string), transcriptionQueues: queueFactory, transcriptionServices: transcriptionFactory, transcriptionQueueByRoot: map[string]transcription.Queue{}, transcriptionServiceByRoot: map[string]transcription.Service{}, transcriptionExecution: deps.TranscriptionExecution, transcriptionBackends: backendFactory, transcriptionStores: storeFactory}, nil
}

// NewDefaultService constructs a Service with production defaults: the wall
// clock and StudyPilot's secure course/module ID generator.
func NewDefaultService() *Service {
	service, _ := NewService(Dependencies{Now: time.Now, GenerateID: course.DefaultIDGenerator})
	return service
}

func defaultCaptureServiceFactory(workspace.Paths, string, func(string) (string, error)) (capture.Service, error) {
	return capture.UnavailableService{}, nil
}

func defaultSessionRepositoryFactory(paths workspace.Paths, clock session.Clock, generate session.IDGenerator) (SessionRepository, error) {
	return session.NewRepository(paths, clock, generate)
}

func resolvePaths(root string) (workspace.Paths, error) {
	if strings.TrimSpace(root) == "" {
		return workspace.DefaultPaths()
	}
	return workspace.PathsFromRoot(root)
}

// checkContext returns a classified cancellation error if ctx is already done.
// The context is never retained on the Service.
func checkContext(ctx context.Context, op string) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return newError(op, "operation cancelled", err)
	}
	return nil
}

// execute runs a validated plan and converts the report into a UI-neutral
// result. Content conflicts are reported within the result (no error); unsafe
// symlinks and I/O failures return the partial result plus a classified error.
func (s *Service) execute(ctx context.Context, op string, plan filesystem.Plan) (ExecutionResult, error) {
	if err := checkContext(ctx, op); err != nil {
		return ExecutionResult{}, err
	}
	report, err := filesystem.Execute(plan)
	result := executionResult(report)
	if err != nil {
		return result, newError(op, "execute filesystem plan", err)
	}
	return result, nil
}

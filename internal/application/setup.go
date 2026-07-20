package application

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	localconfig "github.com/Arameair/studypilot/internal/config"
	"github.com/Arameair/studypilot/internal/workspace"
)

var (
	ErrSetupConflict             = errors.New("workspace setup conflicts with existing state")
	ErrSetupCaptureActive        = errors.New("workspace cannot change while capture is active")
	ErrSetupConfirmationRequired = errors.New("workspace setup requires confirmation")
	ErrSetupPersistenceUncertain = errors.New("workspace setup configuration persistence is uncertain")
)

// SetupOptions injects every machine-specific location so tests never touch the
// developer's user configuration directory.
type SetupOptions struct {
	ConfigStore         *localconfig.Store
	UserHome            string
	SourceRoot          string
	ExplicitRoot        string
	Explicit            bool
	InitializeWorkspace func(context.Context, WorkspaceRequest) (ExecutionResult, error)
}

// SetupService owns first-run selection and persistent-root state.
type SetupService struct {
	app          *Service
	store        *localconfig.Store
	home         string
	sourceRoot   string
	explicitRoot bool
	proposedRoot string

	mu             sync.RWMutex
	setupMu        sync.Mutex
	configuredRoot string
	activeRoot     string
	startupIssue   bool
	initialize     func(context.Context, WorkspaceRequest) (ExecutionResult, error)
}

func NewSetupService(app *Service, options SetupOptions) (*SetupService, error) {
	if app == nil || options.ConfigStore == nil {
		return nil, errors.New("application: setup service dependencies are required")
	}
	home := filepath.Clean(options.UserHome)
	if strings.TrimSpace(options.UserHome) == "" || !filepath.IsAbs(home) {
		return nil, errors.New("application: absolute user home is required")
	}
	proposed, err := workspace.PathsFromRoot(filepath.Join(home, "Documents", "vaults"))
	if err != nil {
		return nil, fmt.Errorf("construct setup proposal: %w", err)
	}
	sourceRoot := ""
	if strings.TrimSpace(options.SourceRoot) != "" {
		sourceRoot = filepath.Clean(options.SourceRoot)
		if !filepath.IsAbs(sourceRoot) {
			return nil, errors.New("application: source root must be absolute")
		}
	}
	initialize := options.InitializeWorkspace
	if initialize == nil {
		initialize = app.InitializeWorkspace
	}
	service := &SetupService{app: app, store: options.ConfigStore, home: home, sourceRoot: sourceRoot, proposedRoot: proposed.Root, initialize: initialize}
	if options.Explicit {
		service.explicitRoot = true
		service.configuredRoot = filepath.Clean(options.ExplicitRoot)
		inspection, inspectErr := workspace.InspectSetupRoot(options.ExplicitRoot, home, service.sourceRoot)
		if inspectErr == nil && inspection.Initialized && inspection.Writable {
			service.activeRoot = inspection.Paths.Root
		} else {
			service.startupIssue = inspectErr != nil
		}
		return service, nil
	}
	value, loadErr := options.ConfigStore.Load()
	if errors.Is(loadErr, localconfig.ErrMissing) {
		return service, nil
	}
	if loadErr != nil {
		service.startupIssue = true
		return service, nil
	}
	service.configuredRoot = value.WorkspaceRoot
	inspection, inspectErr := workspace.InspectSetupRoot(value.WorkspaceRoot, home, service.sourceRoot)
	if inspectErr == nil && inspection.Initialized && inspection.Writable {
		service.activeRoot = inspection.Paths.Root
	} else {
		service.startupIssue = true
	}
	return service, nil
}

func (s *SetupService) ActiveWorkspaceRoot() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeRoot
}

func (s *SetupService) GetSetupState(context.Context) (SetupState, error) {
	s.mu.RLock()
	candidate := s.configuredRoot
	active := s.activeRoot
	explicit := s.explicitRoot
	issue := s.startupIssue
	s.mu.RUnlock()
	if candidate == "" {
		candidate = s.proposedRoot
	}
	state, err := s.inspect(candidate)
	state.ProposedRoot = s.proposedRoot
	state.ConfiguredRoot = s.configuredRootValue()
	state.ActiveRoot = active
	state.ExplicitRoot = explicit
	state.SetupRequired = active == ""
	if issue && state.ValidationStatus == "valid" {
		state.ValidationStatus = "repair_required"
	}
	if err != nil {
		state.SetupRequired = true
		return state, nil
	}
	return state, nil
}

func (s *SetupService) ValidateSetup(_ context.Context, req SetupRequest) (SetupState, error) {
	state, err := s.inspect(req.Root)
	state.ProposedRoot = s.proposedRoot
	state.ConfiguredRoot = s.configuredRootValue()
	state.ActiveRoot = s.ActiveWorkspaceRoot()
	state.ExplicitRoot = s.explicitRoot
	state.SetupRequired = state.ActiveRoot == ""
	if err != nil {
		return state, newError("ValidateSetup", "validate workspace selection", err)
	}
	return state, nil
}

func (s *SetupService) InitializeSetup(ctx context.Context, req SetupRequest) (SetupState, error) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if !req.Confirm {
		return SetupState{}, newError("InitializeSetup", "confirm workspace setup", ErrSetupConfirmationRequired)
	}
	inspection, err := workspace.InspectSetupRoot(req.Root, s.home, s.sourceRoot)
	if err != nil {
		return SetupState{}, newError("InitializeSetup", "validate workspace selection", err)
	}
	if inspection.Disposition == workspace.SetupConflicting || !inspection.CanInitialize {
		return SetupState{}, newError("InitializeSetup", "workspace conflicts with existing content", ErrSetupConflict)
	}
	current := s.ActiveWorkspaceRoot()
	if current != "" && !strings.EqualFold(filepath.Clean(current), inspection.Paths.Root) && s.app.HasActiveCapture() {
		return SetupState{}, newError("InitializeSetup", "stop capture before switching workspaces", ErrSetupCaptureActive)
	}
	if !inspection.Initialized {
		result, initErr := s.initialize(ctx, WorkspaceRequest{Root: inspection.Paths.Root})
		if initErr != nil {
			return SetupState{}, newError("InitializeSetup", "initialize workspace", initErr)
		}
		if result.Conflicts != 0 {
			return SetupState{}, newError("InitializeSetup", "workspace initialization was incomplete", ErrSetupConflict)
		}
	}
	verified, err := workspace.InspectSetupRoot(inspection.Paths.Root, s.home, s.sourceRoot)
	if err != nil || !verified.Initialized {
		if err == nil {
			err = ErrSetupConflict
		}
		return SetupState{}, newError("InitializeSetup", "verify initialized workspace", err)
	}
	if !s.explicitRoot {
		value := localconfig.Config{SchemaVersion: localconfig.SchemaVersion, WorkspaceRoot: verified.Paths.Root}
		if err := s.store.Save(value); err != nil {
			return SetupState{}, newError("InitializeSetup", "persist workspace configuration", fmt.Errorf("%w: %v", ErrSetupPersistenceUncertain, err))
		}
	}
	s.mu.Lock()
	s.activeRoot = verified.Paths.Root
	if !s.explicitRoot {
		s.configuredRoot = verified.Paths.Root
	}
	s.startupIssue = false
	s.mu.Unlock()
	return s.GetSetupState(ctx)
}

func (s *SetupService) inspect(root string) (SetupState, error) {
	inspection, err := workspace.InspectSetupRoot(root, s.home, s.sourceRoot)
	state := SetupState{ValidationStatus: "invalid"}
	if inspection.Paths.Root != "" {
		state.ProposedRoot = inspection.Paths.Root
		state.PrivateVault = inspection.Paths.Private
		state.PortfolioVault = inspection.Paths.Portfolio
	}
	state.RootExists = inspection.Exists
	state.RootWritable = inspection.Writable
	state.Initialized = inspection.Initialized
	state.CanInitialize = inspection.CanInitialize
	state.Disposition = string(inspection.Disposition)
	if err == nil {
		state.ValidationStatus = "valid"
		if inspection.Disposition == workspace.SetupConflicting {
			state.ValidationStatus = "conflicting"
		}
	}
	return state, err
}

func (s *SetupService) configuredRootValue() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.configuredRoot
}

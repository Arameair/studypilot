package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	localconfig "github.com/Arameair/studypilot/internal/config"
)

func TestSetupMissingConfigValidatesWithoutMutationThenPersists(t *testing.T) {
	home, source := t.TempDir(), t.TempDir()
	store := setupStore(t)
	setup := newSetupFixture(t, store, home, source, "", false)
	state, err := setup.GetSetupState(context.Background())
	if err != nil || !state.SetupRequired || state.ProposedRoot != filepath.Join(home, "Documents", "vaults") {
		t.Fatalf("initial state = %+v, %v", state, err)
	}
	root := filepath.Join(t.TempDir(), "Example User", "Documents", "vaults Unicode 學習")
	validated, err := setup.ValidateSetup(context.Background(), SetupRequest{Root: root})
	if err != nil || !validated.CanInitialize || validated.RootExists {
		t.Fatalf("validated = %+v, %v", validated, err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("validation mutated root: %v", err)
	}
	initialized, err := setup.InitializeSetup(context.Background(), SetupRequest{Root: root, Confirm: true})
	if err != nil || initialized.SetupRequired || !initialized.Initialized || initialized.ActiveRoot != root {
		t.Fatalf("initialized = %+v, %v", initialized, err)
	}
	saved, err := store.Load()
	if err != nil || saved.WorkspaceRoot != root {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	again, err := setup.InitializeSetup(context.Background(), SetupRequest{Root: root, Confirm: true})
	if err != nil || !again.Initialized || again.ActiveRoot != root {
		t.Fatalf("idempotent setup = %+v, %v", again, err)
	}
	restarted := newSetupFixture(t, store, home, source, "", false)
	restartedState, err := restarted.GetSetupState(context.Background())
	if err != nil || restartedState.SetupRequired || restartedState.ActiveRoot != root {
		t.Fatalf("restart = %+v, %v", restartedState, err)
	}
}

func TestSetupSwitchLeavesPreviousWorkspaceUntouched(t *testing.T) {
	home, source := t.TempDir(), t.TempDir()
	store := setupStore(t)
	setup := newSetupFixture(t, store, home, source, "", false)
	first := filepath.Join(t.TempDir(), "first workspace")
	if _, err := setup.InitializeSetup(context.Background(), SetupRequest{Root: first, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(first, "preserve.txt")
	if err := os.WriteFile(sentinel, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), "second workspace")
	if _, err := setup.InitializeSetup(context.Background(), SetupRequest{Root: second, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "untouched" {
		t.Fatalf("old workspace changed: %q, %v", data, err)
	}
	before, _ := store.Load()
	conflicting := filepath.Join(t.TempDir(), "conflicting")
	if err := os.Mkdir(conflicting, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conflicting, "unrelated"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.InitializeSetup(context.Background(), SetupRequest{Root: conflicting, Confirm: true}); !errors.Is(err, ErrSetupConflict) {
		t.Fatalf("conflicting switch error = %v", err)
	}
	after, _ := store.Load()
	if after != before || setup.ActiveWorkspaceRoot() != second {
		t.Fatalf("failed switch changed state: before=%+v after=%+v active=%q", before, after, setup.ActiveWorkspaceRoot())
	}
}

func TestSetupRootPrecedenceAndInvalidPersistedConfig(t *testing.T) {
	home, source := t.TempDir(), t.TempDir()
	store := setupStore(t)
	persisted := filepath.Join(t.TempDir(), "persisted")
	normal := newSetupFixture(t, store, home, source, "", false)
	if _, err := normal.InitializeSetup(context.Background(), SetupRequest{Root: persisted, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	explicit := filepath.Join(t.TempDir(), "explicit")
	explicitSetup := newSetupFixture(t, store, home, source, explicit, true)
	state, err := explicitSetup.GetSetupState(context.Background())
	if err != nil || state.ConfiguredRoot != explicit || !state.ExplicitRoot {
		t.Fatalf("explicit state = %+v, %v", state, err)
	}
	if _, err := explicitSetup.InitializeSetup(context.Background(), SetupRequest{Root: explicit, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	saved, _ := store.Load()
	if saved.WorkspaceRoot != persisted {
		t.Fatalf("explicit setup overwrote persisted root: %+v", saved)
	}

	badStore := setupStore(t)
	if err := os.MkdirAll(filepath.Dir(badStore.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badStore.Path(), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := newSetupFixture(t, badStore, home, source, "", false)
	badState, err := bad.GetSetupState(context.Background())
	if err != nil || !badState.SetupRequired || badState.ActiveRoot != "" {
		t.Fatalf("invalid persisted state = %+v, %v", badState, err)
	}
}

func TestSetupRequiresConfirmationAndRejectsSwitchDuringCapture(t *testing.T) {
	home, source := t.TempDir(), t.TempDir()
	store := setupStore(t)
	setup := newSetupFixture(t, store, home, source, "", false)
	first := filepath.Join(t.TempDir(), "first")
	if _, err := setup.InitializeSetup(context.Background(), SetupRequest{Root: first}); !errors.Is(err, ErrSetupConfirmationRequired) {
		t.Fatalf("missing confirmation error = %v", err)
	}
	if _, err := setup.InitializeSetup(context.Background(), SetupRequest{Root: first, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	setup.app.setCaptureActive("session-test", true)
	second := filepath.Join(t.TempDir(), "second")
	if _, err := setup.InitializeSetup(context.Background(), SetupRequest{Root: second, Confirm: true}); !errors.Is(err, ErrSetupCaptureActive) {
		t.Fatalf("active capture error = %v", err)
	}
}

func TestSetupPartialInitializationFailureDoesNotPersist(t *testing.T) {
	home, source := t.TempDir(), t.TempDir()
	store := setupStore(t)
	app := NewDefaultService()
	setup, err := NewSetupService(app, SetupOptions{
		ConfigStore: store, UserHome: home, SourceRoot: source,
		InitializeWorkspace: func(context.Context, WorkspaceRequest) (ExecutionResult, error) {
			return ExecutionResult{Created: 1}, errors.New("injected partial failure")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "partial")
	if _, err := setup.InitializeSetup(context.Background(), SetupRequest{Root: root, Confirm: true}); err == nil {
		t.Fatal("partial initialization error = nil")
	}
	if _, err := store.Load(); !errors.Is(err, localconfig.ErrMissing) {
		t.Fatalf("partial failure persisted configuration: %v", err)
	}
	if setup.ActiveWorkspaceRoot() != "" {
		t.Fatalf("partial failure selected root %q", setup.ActiveWorkspaceRoot())
	}
}

func setupStore(t *testing.T) *localconfig.Store {
	t.Helper()
	store, err := localconfig.NewStore(filepath.Join(t.TempDir(), "config", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func newSetupFixture(t *testing.T, store *localconfig.Store, home, source, explicit string, explicitSet bool) *SetupService {
	t.Helper()
	app := NewDefaultService()
	setup, err := NewSetupService(app, SetupOptions{ConfigStore: store, UserHome: home, SourceRoot: source, ExplicitRoot: explicit, Explicit: explicitSet})
	if err != nil {
		t.Fatal(err)
	}
	return setup
}

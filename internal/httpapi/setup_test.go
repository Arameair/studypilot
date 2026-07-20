package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arameair/studypilot/internal/application"
	localconfig "github.com/Arameair/studypilot/internal/config"
)

func TestSetupAPIBlocksDashboardUntilInitialization(t *testing.T) {
	handler, store, root := setupHandler(t)
	state := request(t, handler, http.MethodGet, "/api/v1/setup", "")
	if state.Code != http.StatusOK || !strings.Contains(state.Body.String(), `"setup_required":true`) {
		t.Fatalf("setup state = %d %s", state.Code, state.Body)
	}
	blocked := request(t, handler, http.MethodGet, "/api/v1/dashboard", "")
	if blocked.Code != http.StatusConflict || !strings.Contains(blocked.Body.String(), "setup_required") {
		t.Fatalf("dashboard before setup = %d %s", blocked.Code, blocked.Body)
	}
	body, _ := json.Marshal(map[string]any{"root": root})
	validated := request(t, handler, http.MethodPost, "/api/v1/setup/validate", string(body))
	if validated.Code != http.StatusOK || !strings.Contains(validated.Body.String(), `"can_initialize":true`) {
		t.Fatalf("validate = %d %s", validated.Code, validated.Body)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("validate mutated root: %v", err)
	}
	initializeBody, _ := json.Marshal(map[string]any{"root": root, "confirm": true})
	initialized := request(t, handler, http.MethodPost, "/api/v1/setup/initialize", string(initializeBody))
	if initialized.Code != http.StatusOK || !strings.Contains(initialized.Body.String(), `"setup_required":false`) {
		t.Fatalf("initialize = %d %s", initialized.Code, initialized.Body)
	}
	if _, err := store.Load(); err != nil {
		t.Fatalf("configuration not persisted: %v", err)
	}
	if dashboard := request(t, handler, http.MethodGet, "/api/v1/dashboard", ""); dashboard.Code != http.StatusOK {
		t.Fatalf("dashboard after setup = %d %s", dashboard.Code, dashboard.Body)
	}
}

func TestSetupAPISafetyBoundaries(t *testing.T) {
	handler, _, root := setupHandler(t)
	if got := request(t, handler, http.MethodGet, "/api/v1/setup/validate", "").Code; got != http.StatusMethodNotAllowed {
		t.Fatalf("unsupported method = %d", got)
	}
	oversized := `{"root":"` + strings.Repeat("x", 9<<10) + `"}`
	if got := request(t, handler, http.MethodPost, "/api/v1/setup/validate", oversized).Code; got != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d", got)
	}
	requestBody := `{"root":"` + strings.ReplaceAll(root, `\`, `\\`) + `","unknown":true}`
	if got := request(t, handler, http.MethodPost, "/api/v1/setup/validate", requestBody).Code; got != http.StatusBadRequest {
		t.Fatalf("unknown field = %d", got)
	}
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/setup/initialize", strings.NewReader(`{"root":"x","confirm":true}`))
	req.Host = "127.0.0.1:8765"
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin setup = %d", rec.Code)
	}
}

func TestFirstRunFrontendContainsSetupAndSwitchWorkflow(t *testing.T) {
	handler, _, _ := setupHandler(t)
	index := request(t, handler, http.MethodGet, "/", "").Body.String()
	for _, required := range []string{"Set up StudyPilot", `id="workspace-root"`, `id="validate-workspace"`, `id="create-workspace"`, "Workspace settings", "Switch workspace"} {
		if !strings.Contains(index, required) {
			t.Errorf("setup frontend missing %q", required)
		}
	}
	script := request(t, handler, http.MethodGet, "/app.js", "").Body.String()
	for _, required := range []string{"/setup/validate", "/setup/initialize", "confirm:true", "renderWorkspaceSettings", "Existing workspace data will remain untouched"} {
		if !strings.Contains(script, required) {
			t.Errorf("setup script missing %q", required)
		}
	}
}

func setupHandler(t *testing.T) (http.Handler, *localconfig.Store, string) {
	t.Helper()
	store, err := localconfig.NewStore(filepath.Join(t.TempDir(), "config", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := application.NewDefaultService()
	setup, err := application.NewSetupService(app, application.SetupOptions{ConfigStore: store, UserHome: t.TempDir(), SourceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(app, Config{Setup: setup})
	if err != nil {
		t.Fatal(err)
	}
	return handler, store, filepath.Join(t.TempDir(), "Example User", "Documents", "vaults")
}

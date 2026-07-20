package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	localconfig "github.com/Arameair/studypilot/internal/config"
)

func TestGUIFirstRunSetupRestartAndWorkspaceSwitch(t *testing.T) {
	for _, name := range []string{"STUDYPILOT_CAPTURE_BACKEND", "STUDYPILOT_CAPTURE_EXECUTABLE", "STUDYPILOT_CAPTURE_DRIVER", "STUDYPILOT_CAPTURE_DEVICE", "STUDYPILOT_TRANSCRIPTION_BACKEND"} {
		t.Setenv(name, "")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	store, err := localconfig.NewStore(filepath.Join(t.TempDir(), "isolated config", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	originalStore, originalListen := newLocalConfigStore, guiListen
	newLocalConfigStore = func() (*localconfig.Store, error) { return store, nil }
	t.Cleanup(func() {
		newLocalConfigStore = originalStore
		guiListen = originalListen
	})

	firstRoot := filepath.Join(t.TempDir(), "Example User", "Documents", "vaults Unicode 學習")
	address, stop := launchSetupGUI(t)
	state := setupHTTP(t, address, http.MethodGet, "/api/v1/setup", nil)
	if state["setup_required"] != true || state["proposed_root"] != filepath.Join(home, "Documents", "vaults") {
		t.Fatalf("first launch setup state = %#v", state)
	}
	validated := setupHTTP(t, address, http.MethodPost, "/api/v1/setup/validate", map[string]any{"root": firstRoot})
	if validated["can_initialize"] != true {
		t.Fatalf("validated state = %#v", validated)
	}
	if _, err := os.Stat(firstRoot); !os.IsNotExist(err) {
		t.Fatalf("validation mutated root: %v", err)
	}
	initialized := setupHTTP(t, address, http.MethodPost, "/api/v1/setup/initialize", map[string]any{"root": firstRoot, "confirm": true})
	if initialized["active_root"] != firstRoot || initialized["setup_required"] != false {
		t.Fatalf("initialized state = %#v", initialized)
	}
	if response := setupRawHTTP(t, address, http.MethodGet, "/api/v1/dashboard", nil); response.StatusCode != http.StatusOK {
		t.Fatalf("dashboard after setup = %d", response.StatusCode)
	}
	sentinel := filepath.Join(firstRoot, "leave-this-workspace.txt")
	if err := os.WriteFile(sentinel, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	stop()
	assertPortReleased(t, address)

	address, stop = launchSetupGUI(t)
	restarted := setupHTTP(t, address, http.MethodGet, "/api/v1/setup", nil)
	if restarted["active_root"] != firstRoot || restarted["setup_required"] != false {
		t.Fatalf("restart state = %#v", restarted)
	}
	secondRoot := filepath.Join(t.TempDir(), "second workspace with spaces")
	switched := setupHTTP(t, address, http.MethodPost, "/api/v1/setup/initialize", map[string]any{"root": secondRoot, "confirm": true})
	if switched["active_root"] != secondRoot {
		t.Fatalf("switched state = %#v", switched)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "untouched" {
		t.Fatalf("first workspace changed: %q, %v", data, err)
	}
	response := setupRawHTTP(t, address, http.MethodPost, "/api/v1/setup/initialize", map[string]any{"root": "relative", "confirm": true})
	if response.StatusCode == http.StatusOK {
		t.Fatal("invalid switch succeeded")
	}
	response.Body.Close()
	saved, err := store.Load()
	if err != nil || saved.WorkspaceRoot != secondRoot {
		t.Fatalf("invalid switch replaced configuration: %+v, %v", saved, err)
	}
	stop()
	assertPortReleased(t, address)
}

func launchSetupGUI(t *testing.T) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	guiListen = func(string) (net.Listener, error) { return listener, nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		done <- runContext(ctx, []string{"gui", "--address", "127.0.0.1:0"}, &stdout, &stderr)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := http.Get("http://" + address + "/api/v1/health")
		if err == nil {
			response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("GUI did not start: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return address, func() {
		cancel()
		select {
		case code := <-done:
			if code != 0 {
				t.Errorf("GUI shutdown code = %d", code)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("GUI did not shut down")
		}
	}
}

func setupHTTP(t *testing.T, address, method, path string, body any) map[string]any {
	t.Helper()
	response := setupRawHTTP(t, address, method, path, body)
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("%s %s = %d %s", method, path, response.StatusCode, data)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func setupRawHTTP(t *testing.T, address, method, path string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = strings.NewReader(string(data))
	}
	request, err := http.NewRequest(method, "http://"+address+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func assertPortReleased(t *testing.T, address string) {
	t.Helper()
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatalf("port was not released: %v", err)
	}
	listener.Close()
}

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
)

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	var out, err bytes.Buffer
	if code := run(args, &out, &err); code != 0 {
		t.Fatalf("run %v code=%d stdout=%s stderr=%s", args, code, out.String(), err.String())
	}
	return out.String()
}
func decodeMap(t *testing.T, value string) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCaptureCLIEndToEndAcrossCompositions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "StudyPilot")
	runCLI(t, "init", "--root", root)
	runCLI(t, "course", "create", "--root", root, "--name", "CLI Capture Course")
	runCLI(t, "module", "create", "--root", root, "--course", "CLI Capture Course", "--number", "1", "--name", "CLI Capture Module")
	created := decodeMap(t, runCLI(t, "session", "create", "--root", root, "--course", "CLI Capture Course", "--module", "CLI Capture Module", "--title", "CLI Capture Session", "--idempotency-key", "cli", "--json"))
	sessionID := created["id"].(string)
	courseID := created["course_id"].(string)
	moduleID := created["module_id"].(string)
	revision := int(created["revision"].(float64))
	startedSession := decodeMap(t, runCLI(t, "session", "start", "--root", root, "--course", courseID, "--module", moduleID, "--session", sessionID, "--revision", strconv.Itoa(revision), "--json"))
	revision = int(startedSession["revision"].(float64))
	base := []string{"--root", root, "--course", courseID, "--module", moduleID, "--session", sessionID}
	startArgs := append([]string{"capture", "start"}, base...)
	startArgs = append(startArgs, "--revision", strconv.Itoa(revision), "--backend", "synthetic", "--json")
	captureStart := decodeMap(t, runCLI(t, startArgs...))
	revision = int(captureStart["revision"].(float64))
	if captureStart["capture_status"] != "recording" {
		t.Fatal(captureStart)
	}
	pauseArgs := append([]string{"capture", "pause"}, base...)
	pauseArgs = append(pauseArgs, "--revision", strconv.Itoa(revision), "--json")
	paused := decodeMap(t, runCLI(t, pauseArgs...))
	revision = int(paused["revision"].(float64))
	if paused["capture_status"] != "paused" {
		t.Fatal(paused)
	}
	resumeArgs := append([]string{"capture", "resume"}, base...)
	resumeArgs = append(resumeArgs, "--revision", strconv.Itoa(revision), "--json")
	resumed := decodeMap(t, runCLI(t, resumeArgs...))
	revision = int(resumed["revision"].(float64))
	if resumed["capture_status"] != "recording" {
		t.Fatal(resumed)
	}
	stopArgs := append([]string{"capture", "stop"}, base...)
	stopArgs = append(stopArgs, "--revision", strconv.Itoa(revision), "--json")
	stopped := decodeMap(t, runCLI(t, stopArgs...))
	revision = int(stopped["revision"].(float64))
	if stopped["capture_status"] != "stopped" {
		t.Fatal(stopped)
	}
	inspectArgs := append([]string{"capture", "inspect"}, base...)
	inspectArgs = append(inspectArgs, "--backend", "synthetic", "--json")
	inspection := decodeMap(t, runCLI(t, inspectArgs...))
	if len(inspection["finalized"].([]any)) != 2 || len(inspection["issues"].([]any)) != 0 {
		t.Fatal(inspection)
	}
	textArgs := append([]string{"capture", "inspect"}, base...)
	textArgs = append(textArgs, "--backend", "synthetic")
	if output := runCLI(t, textArgs...); !bytes.Contains([]byte(output), []byte("Finalized: 2")) {
		t.Fatal(output)
	}
	complete := append([]string{"session", "complete"}, base...)
	complete = append(complete, "--revision", strconv.Itoa(revision), "--json")
	completed := decodeMap(t, runCLI(t, complete...))
	if completed["session_status"] != "completed" {
		t.Fatal(completed)
	}
}

func TestCaptureCLIValidationAndSafeJSONError(t *testing.T) {
	var out, err bytes.Buffer
	if code := run([]string{"capture", "start", "--course", "c"}, &out, &err); code != 2 {
		t.Fatalf("code=%d", code)
	}
	out.Reset()
	err.Reset()
	if code := run([]string{"capture", "start", "--course", "c", "--module", "m", "--session", "s", "--revision", "bad", "--backend", "synthetic"}, &out, &err); code != 2 {
		t.Fatalf("code=%d", code)
	}
	out.Reset()
	err.Reset()
	if code := run([]string{"capture", "start", "--course", "c", "--module", "m", "--session", "s", "--revision", "1", "--backend", "unknown"}, &out, &err); code != 2 {
		t.Fatalf("code=%d", code)
	}
	out.Reset()
	err.Reset()
	missingRoot := filepath.Join(t.TempDir(), "missing")
	code := run([]string{"capture", "start", "--root", missingRoot, "--course", "c", "--module", "m", "--session", "s", "--revision", "1", "--backend", "synthetic", "--json"}, &out, &err)
	if code != 1 {
		t.Fatalf("code=%d stderr=%s", code, err.String())
	}
	payload := decodeMap(t, err.String())
	if payload["error"] == nil || bytes.Contains(err.Bytes(), []byte(missingRoot)) {
		t.Fatalf("unsafe error=%s", err.String())
	}
}

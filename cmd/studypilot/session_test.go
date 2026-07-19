package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupModuleForCLI(t *testing.T) string {
	t.Helper()
	setFixedClock(t)
	root := filepath.Join(t.TempDir(), "StudyPilot")
	initializeForCLI(t, root)
	createCourseForCLI(t, root)
	if code, _, stderr := runForTest([]string{"module", "create", "--course", "TCM Practical Help Desk", "--number", "3", "--name", "Windows Services", "--root", root}); code != 0 {
		t.Fatalf("create module code = %d; stderr = %q", code, stderr)
	}
	return root
}

func sessionsDir(root string) string {
	return filepath.Join(root, "Learning-Vault-Private", "01 Courses", "TCM Practical Help Desk", "Modules", "03 - Windows Services", "Sessions")
}

func createSessionForCLI(t *testing.T, root, title string) sessionJSON {
	t.Helper()
	code, stdout, stderr := runForTest([]string{"session", "create", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--title", title, "--root", root, "--json"})
	if code != 0 {
		t.Fatalf("create session code = %d; stderr = %q", code, stderr)
	}
	var payload sessionJSON
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("create session json: %v (%q)", err, stdout)
	}
	return payload
}

func getSessionForCLI(t *testing.T, root, ref string) sessionJSON {
	t.Helper()
	code, stdout, stderr := runForTest([]string{"session", "get", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--session", ref, "--root", root, "--json"})
	if code != 0 {
		t.Fatalf("get session code = %d; stderr = %q", code, stderr)
	}
	var payload sessionJSON
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("get session json: %v (%q)", err, stdout)
	}
	return payload
}

func TestSessionHelp(t *testing.T) {
	for _, args := range [][]string{{"session", "help"}, {"session", "--help"}, {"session", "-h"}} {
		code, stdout, stderr := runForTest(args)
		if code != 0 {
			t.Errorf("%v code = %d, want 0", args, code)
		}
		if !strings.Contains(stdout, "session create") || !strings.Contains(stdout, "session inspect") {
			t.Errorf("%v stdout missing usage: %q", args, stdout)
		}
		if stderr != "" {
			t.Errorf("%v stderr = %q, want empty", args, stderr)
		}
	}
}

func TestSessionMissingRequiredFlags(t *testing.T) {
	root := setupModuleForCLI(t)
	tests := []struct {
		name string
		args []string
	}{
		{"no subcommand", []string{"session"}},
		{"unknown subcommand", []string{"session", "frobnicate"}},
		{"create missing title", []string{"session", "create", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--root", root}},
		{"get missing session", []string{"session", "get", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--root", root}},
		{"start missing revision", []string{"session", "start", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--session", "1", "--root", root}},
		{"start zero revision", []string{"session", "start", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--session", "1", "--revision", "0", "--root", root}},
		{"start noninteger revision", []string{"session", "start", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--session", "1", "--revision", "nope", "--root", root}},
		{"list module without course", []string{"session", "list", "--module", "Windows Services", "--root", root}},
		{"list bad status", []string{"session", "list", "--course", "TCM Practical Help Desk", "--status", "completed", "--root", root}},
		{"inspect neither session nor all", []string{"session", "inspect", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--root", root}},
		{"inspect both session and all", []string{"session", "inspect", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--session", "1", "--all", "--root", root}},
		{"unexpected positional", []string{"session", "get", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--session", "1", "extra", "--root", root}},
		{"unknown flag", []string{"session", "get", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--session", "1", "--nope", "--root", root}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := runForTest(test.args)
			if code != 2 {
				t.Errorf("code = %d, want 2", code)
			}
			if strings.TrimSpace(stderr) == "" {
				t.Error("stderr empty, want usage error")
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty on usage error", stdout)
			}
		})
	}
}

func TestSessionCreateIsPlannedAndGettable(t *testing.T) {
	root := setupModuleForCLI(t)
	code, stdout, stderr := runForTest([]string{"session", "create", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--title", "Service Troubleshooting", "--root", root})
	if code != 0 || stderr != "" {
		t.Fatalf("create code = %d; stderr = %q", code, stderr)
	}
	for _, want := range []string{"SESSION 001 — Service Troubleshooting", "Status: planned", "Capture: unavailable", "Revision: 1", "Directory: 001 - Service Troubleshooting"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q; got %q", want, stdout)
		}
	}
	got := getSessionForCLI(t, root, "Service Troubleshooting")
	if got.SessionStatus != "planned" || got.Revision != 1 || got.CaptureStatus != "unavailable" {
		t.Fatalf("get = %+v", got)
	}
}

func TestSessionCreateIdempotencyKey(t *testing.T) {
	root := setupModuleForCLI(t)
	args := []string{"session", "create", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--title", "Keyed", "--idempotency-key", "req-1", "--root", root, "--json"}
	code, first, _ := runForTest(args)
	if code != 0 {
		t.Fatal("first create failed")
	}
	code, second, _ := runForTest(args)
	if code != 0 {
		t.Fatal("second create failed")
	}
	var a, b sessionJSON
	json.Unmarshal([]byte(first), &a)
	json.Unmarshal([]byte(second), &b)
	if a.ID != b.ID || a.Number != b.Number {
		t.Fatalf("keyed create not idempotent: %+v vs %+v", a, b)
	}
}

func TestSessionLifecycleThroughCLI(t *testing.T) {
	root := setupModuleForCLI(t)
	created := createSessionForCLI(t, root, "Lifecycle")
	ref := created.ID
	run := func(op string, revision uint64) sessionJSON {
		t.Helper()
		code, stdout, stderr := runForTest([]string{"session", op, "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--session", ref, "--revision", itoa(revision), "--root", root, "--json"})
		if code != 0 {
			t.Fatalf("%s code = %d; stderr = %q", op, code, stderr)
		}
		var payload sessionJSON
		if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
			t.Fatalf("%s json: %v (%q)", op, err, stdout)
		}
		return payload
	}

	started := run("start", 1)
	if started.SessionStatus != "active" || started.Revision != 2 || started.CaptureStatus != "ready" {
		t.Fatalf("start = %+v", started)
	}

	interrupted := run("interrupt", 2)
	if interrupted.SessionStatus != "interrupted" || interrupted.CaptureStatus != "ready" {
		t.Fatalf("interrupt = %+v", interrupted)
	}
	recovering := run("recover", 3)
	if recovering.SessionStatus != "recovering" {
		t.Fatalf("recover = %+v", recovering)
	}
	resumed := run("resume", 4)
	if resumed.SessionStatus != "active" {
		t.Fatalf("resume = %+v", resumed)
	}
	completed := run("complete", 5)
	if completed.SessionStatus != "completed" {
		t.Fatalf("complete = %+v", completed)
	}

	// Stale revision must fail closed with a runtime error and change nothing.
	code, _, stderr := runForTest([]string{"session", "start", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--session", ref, "--revision", "2", "--root", root})
	if code != 1 {
		t.Fatalf("stale start code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "conflicted") {
		t.Errorf("stale start missing guidance: %q", stderr)
	}
	if final := getSessionForCLI(t, root, ref); final.SessionStatus != "completed" || final.Revision != completed.Revision {
		t.Fatalf("session changed after failed stale start: %+v", final)
	}
}

func TestSessionAbandonIsTerminal(t *testing.T) {
	root := setupModuleForCLI(t)
	created := createSessionForCLI(t, root, "Abandon Me")
	code, _, stderr := runForTest([]string{"session", "abandon", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--session", created.ID, "--revision", "1", "--reason", "Course section skipped", "--root", root})
	if code != 0 {
		t.Fatalf("abandon code = %d; stderr = %q", code, stderr)
	}
	got := getSessionForCLI(t, root, created.ID)
	if got.SessionStatus != "abandoned" {
		t.Fatalf("status = %s, want abandoned", got.SessionStatus)
	}
	// Record is retained, not deleted.
	if _, err := os.Stat(filepath.Join(sessionsDir(root), got.DirectoryName)); err != nil {
		t.Fatalf("abandoned session directory missing: %v", err)
	}
	// Reason is never persisted in the runtime file.
	runtimeBytes, err := os.ReadFile(filepath.Join(sessionsDir(root), got.DirectoryName, ".studypilot-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(runtimeBytes), "Course section skipped") {
		t.Fatal("abandonment reason leaked into persisted runtime")
	}
}

func TestSessionListFiltersAndStatus(t *testing.T) {
	root := setupModuleForCLI(t)
	planned := createSessionForCLI(t, root, "Planned One")
	active := createSessionForCLI(t, root, "Active One")
	if code, _, stderr := runForTest([]string{"session", "start", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--session", active.ID, "--revision", "1", "--root", root}); code != 0 {
		t.Fatalf("start code = %d; stderr = %q", code, stderr)
	}
	_ = planned

	code, stdout, stderr := runForTest([]string{"session", "list", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--root", root, "--json"})
	if code != 0 || stderr != "" {
		t.Fatalf("list code = %d; stderr = %q", code, stderr)
	}
	var all listJSON
	if err := json.Unmarshal([]byte(stdout), &all); err != nil {
		t.Fatalf("list json: %v (%q)", err, stdout)
	}
	if len(all.Sessions) != 2 {
		t.Fatalf("list = %+v", all.Sessions)
	}
	if all.Sessions[0].Number >= all.Sessions[1].Number {
		t.Fatalf("list not ordered by number: %+v", all.Sessions)
	}

	code, stdout, _ = runForTest([]string{"session", "list", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--status", "active", "--root", root, "--json"})
	if code != 0 {
		t.Fatal("filtered list failed")
	}
	var filtered listJSON
	json.Unmarshal([]byte(stdout), &filtered)
	if len(filtered.Sessions) != 1 || filtered.Sessions[0].SessionStatus != "active" {
		t.Fatalf("status filter = %+v", filtered.Sessions)
	}
}

func TestSessionInspectSingle(t *testing.T) {
	root := setupModuleForCLI(t)
	created := createSessionForCLI(t, root, "Inspect Me")
	code, stdout, stderr := runForTest([]string{"session", "inspect", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--session", created.ID, "--root", root, "--json"})
	if code != 0 || stderr != "" {
		t.Fatalf("inspect code = %d; stderr = %q", code, stderr)
	}
	var payload inspectionJSON
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("inspect json: %v (%q)", err, stdout)
	}
	if payload.Session.ID != created.ID || payload.RecoveryState == "" {
		t.Fatalf("inspection = %+v", payload)
	}
}

func TestSessionInspectAllIsResilient(t *testing.T) {
	root := setupModuleForCLI(t)
	healthy := createSessionForCLI(t, root, "Healthy One")
	withdrawn := createSessionForCLI(t, root, "Missing Runtime")

	sessions := sessionsDir(root)
	// Remove the runtime of one valid session.
	if err := os.Remove(filepath.Join(sessions, withdrawn.DirectoryName, ".studypilot-runtime.json")); err != nil {
		t.Fatal(err)
	}
	// Inject malformed metadata.
	malformed := filepath.Join(sessions, "090 - Malformed")
	if err := os.MkdirAll(malformed, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(malformed, ".studypilot-session.json"), []byte("{ not json"), 0o640); err != nil {
		t.Fatal(err)
	}
	// Inject an unsafe symlink.
	if err := os.Symlink(malformed, filepath.Join(sessions, "091 - Link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	before := snapshotDir(t, sessions)

	code, stdout, stderr := runForTest([]string{"session", "inspect", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--all", "--root", root, "--json"})
	if code != 0 {
		t.Fatalf("inspect --all code = %d, want 0; stderr = %q", code, stderr)
	}
	var scan scanJSON
	if err := json.Unmarshal([]byte(stdout), &scan); err != nil {
		t.Fatalf("scan json: %v (%q)", err, stdout)
	}
	if len(scan.Sessions) != 1 || scan.Sessions[0].ID != healthy.ID {
		t.Fatalf("healthy sessions hidden: %+v", scan.Sessions)
	}
	kinds := map[string]bool{}
	for _, issue := range scan.Issues {
		kinds[issue.Kind] = true
		if strings.ContainsAny(issue.Message, "{}") {
			t.Fatalf("issue message leaks content: %q", issue.Message)
		}
	}
	for _, want := range []string{"missing_runtime", "malformed_metadata", "unsafe_path"} {
		if !kinds[want] {
			t.Fatalf("missing issue kind %q; issues=%+v", want, scan.Issues)
		}
	}

	// The scan modified nothing.
	if after := snapshotDir(t, sessions); !equalDirSnapshots(before, after) {
		t.Fatal("inspect --all modified session directory")
	}

	// Write operations still fail closed on the same module.
	if code, _, _ := runForTest([]string{"session", "create", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--title", "Blocked", "--root", root}); code == 0 {
		t.Fatal("create unexpectedly succeeded on malformed module")
	}
}

func TestSessionJSONHasNoHumanTextAndStableFields(t *testing.T) {
	root := setupModuleForCLI(t)
	created := createSessionForCLI(t, root, "Json Fields")
	code, stdout, stderr := runForTest([]string{"session", "get", "--course", "TCM Practical Help Desk", "--module", "Windows Services", "--session", created.ID, "--root", root, "--json"})
	if code != 0 || stderr != "" {
		t.Fatalf("get json code = %d; stderr = %q", code, stderr)
	}
	trimmed := strings.TrimSpace(stdout)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		t.Fatalf("stdout is not pure JSON: %q", stdout)
	}
	var generic map[string]any
	if err := json.Unmarshal([]byte(stdout), &generic); err != nil {
		t.Fatalf("json invalid: %v", err)
	}
	for _, field := range []string{"id", "number", "title", "revision", "session_status", "capture_status", "course_id", "module_id"} {
		if _, ok := generic[field]; !ok {
			t.Errorf("json missing stable field %q", field)
		}
	}
	if strings.Contains(stdout, "SESSION ") || strings.Contains(stdout, "Status:") {
		t.Fatalf("human text leaked into json: %q", stdout)
	}
}

func itoa(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

type dirEntrySnapshot struct {
	content []byte
	mode    os.FileMode
}

func snapshotDir(t *testing.T, root string) map[string]dirEntrySnapshot {
	t.Helper()
	states := map[string]dirEntrySnapshot{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		states[path] = dirEntrySnapshot{content: content, mode: info.Mode()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return states
}

func equalDirSnapshots(a, b map[string]dirEntrySnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for path, state := range a {
		other, ok := b[path]
		if !ok || other.mode != state.mode || string(other.content) != string(state.content) {
			return false
		}
	}
	return true
}

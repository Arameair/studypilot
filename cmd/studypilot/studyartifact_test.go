package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func artifactCommandJSON(t *testing.T, args ...string) map[string]any {
	t.Helper()
	code, stdout, stderr := runForTest(args)
	if code != 0 || stderr != "" {
		t.Fatalf("command %v code=%d stderr=%q", args, code, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestStudyArtifactCLIWorkflowAndPrivacy(t *testing.T) {
	root, revision, segmentID := transcriptionCLIFixture(t)
	execute := artifactCommandJSON(t, "transcription", "execute", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--session", "CLI Session", "--segment", segmentID, "--backend", "synthetic", "--model", "deterministic", "--revision", integer(revision), "--json")
	if execute["completed"] != true {
		t.Fatal(execute)
	}
	noteArgs := []string{"notes", "create-session", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--session", "CLI Session", "--title", "Session Notes", "--expected-artifact-revision", "0", "--json"}
	note := artifactCommandJSON(t, noteArgs...)
	if note["type"] != "note" || int(note["revision"].(float64)) != 1 || strings.Contains(mustJSONMap(t, note), root) {
		t.Fatal(note)
	}
	source := filepath.Join(t.TempDir(), "private reference.txt")
	if err := os.WriteFile(source, []byte("synthetic private asset content"), 0o640); err != nil {
		t.Fatal(err)
	}
	asset := artifactCommandJSON(t, "assets", "add-session", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--session", "CLI Session", "--file", source, "--title", "Reference", "--category", "document", "--expected-artifact-revision", "1", "--json")
	encoded := mustJSONMap(t, asset)
	if asset["type"] != "asset" || int(asset["revision"].(float64)) != 2 || strings.Contains(encoded, source) || strings.Contains(encoded, root) || strings.Contains(encoded, "synthetic private asset content") {
		t.Fatal(asset)
	}
	refresh := artifactCommandJSON(t, "artifacts", "refresh", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--expected-artifact-revision", "2", "--json")
	if int(refresh["revision"].(float64)) != 3 || len(refresh["artifacts"].([]any)) != 3 || len(refresh["issues"].([]any)) != 0 {
		t.Fatal(refresh)
	}
	moduleNote := artifactCommandJSON(t, "notes", "create-module", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--title", "Module Notes", "--expected-artifact-revision", "3", "--json")
	if int(moduleNote["revision"].(float64)) != 4 {
		t.Fatal(moduleNote)
	}
	list := artifactCommandJSON(t, "artifacts", "list", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--json")
	listEncoded := mustJSONMap(t, list)
	if int(list["revision"].(float64)) != 4 || len(list["artifacts"].([]any)) != 4 || strings.Contains(listEncoded, root) || strings.Contains(listEncoded, source) || strings.Contains(listEncoded, "Synthetic transcription") || strings.Contains(listEncoded, "synthetic private asset content") {
		t.Fatalf("unsafe list %s", listEncoded)
	}
	code, stdout, stderr := runForTest([]string{"artifacts", "list", "--root", root, "--course", "CLI Course", "--module", "CLI Module"})
	if code != 0 || stderr != "" || !strings.Contains(stdout, "title=") || !strings.Contains(stdout, "size=") || !strings.Contains(stdout, "sha256=") || strings.Contains(stdout, root) || strings.Contains(stdout, source) {
		t.Fatalf("human list code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	inspection := artifactCommandJSON(t, "artifacts", "inspect", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--json")
	if len(inspection["issues"].([]any)) != 0 {
		t.Fatal(inspection)
	}
	code, stdout, stderr = runForTest([]string{"artifacts", "inspect", "--root", root, "--course", "CLI Course", "--module", "CLI Module"})
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Study artifact inspection") || strings.Contains(stdout, root) {
		t.Fatalf("human inspect code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runForTest(noteArgs)
	if code != 1 || stdout != "" || strings.Contains(stderr, root) || strings.Contains(stderr, source) {
		t.Fatalf("duplicate code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runForTest([]string{"assets", "add-module", "--root", root, "--course", "CLI Course", "--module", "CLI Module", "--file", source, "--title", "Stale", "--category", "document", "--expected-artifact-revision", "2", "--json"})
	if code != 1 || stdout != "" || strings.Contains(stderr, source) {
		t.Fatalf("stale code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestStudyArtifactHelpAndValidation(t *testing.T) {
	for _, group := range []string{"artifacts", "notes", "assets"} {
		code, stdout, stderr := runForTest([]string{group, "help"})
		if code != 0 || stderr != "" || !strings.Contains(stdout, "expected-artifact-revision") {
			t.Fatalf("%s help code=%d stdout=%q stderr=%q", group, code, stdout, stderr)
		}
	}
	code, stdout, stderr := runForTest([]string{"artifacts", "list", "--course", "only"})
	if code != 2 || stdout != "" || stderr == "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func mustJSONMap(t *testing.T, value map[string]any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

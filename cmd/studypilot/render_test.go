package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arameair/studypilot/internal/application"
)

func TestReportErrorMapsKindsToExitCodes(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  int
		wantUsage bool
	}{
		{name: "invalid input", err: &application.Error{Kind: application.ErrorInvalidInput, Message: "bad name"}, wantCode: 2, wantUsage: true},
		{name: "not found", err: &application.Error{Kind: application.ErrorNotFound, Message: "construct module plan", Cause: errors.New("course is unavailable")}, wantCode: 1},
		{name: "collision", err: &application.Error{Kind: application.ErrorCollision, Message: "construct course plan"}, wantCode: 1},
		{name: "unsafe", err: &application.Error{Kind: application.ErrorUnsafe, Message: "execute filesystem plan"}, wantCode: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			code := reportError(test.err, &stderr)
			if code != test.wantCode {
				t.Errorf("reportError code = %d, want %d", code, test.wantCode)
			}
			if !strings.HasPrefix(stderr.String(), "Error: ") {
				t.Errorf("stderr missing error prefix: %q", stderr.String())
			}
			if gotUsage := strings.Contains(stderr.String(), "Usage:"); gotUsage != test.wantUsage {
				t.Errorf("usage present = %v, want %v", gotUsage, test.wantUsage)
			}
		})
	}
}

func TestRenderPlanRendersOperations(t *testing.T) {
	root := t.TempDir()
	result := application.PlanResult{Operations: []application.PlannedOperation{
		{Kind: application.PlanKindDirectory, Path: filepath.Join(root, "dir")},
		{Kind: application.PlanKindFile, Path: filepath.Join(root, "file.md")},
	}}
	var stdout, stderr bytes.Buffer
	code := renderPlan(result, nil, root, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("renderPlan code = %d, want 0", code)
	}
	for _, want := range []string{"CREATE DIRECTORY  dir", "CREATE FILE       file.md", "Dry run complete: 2 operations planned.", "No files or directories were created."} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q; got %q", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRenderExecutionRoutesConflictsAndCounts(t *testing.T) {
	root := t.TempDir()
	result := application.ExecutionResult{
		Created: 1, Skipped: 1, Conflicts: 1,
		Outcomes: []application.PathOutcome{
			{Path: filepath.Join(root, "a"), Status: application.OutcomeCreated},
			{Path: filepath.Join(root, "b"), Status: application.OutcomeSkipped},
			{Path: filepath.Join(root, "c"), Status: application.OutcomeConflict, Detail: "existing file content differs"},
		},
	}
	var stdout, stderr bytes.Buffer
	code := renderExecution(result, nil, "Test", root, &stdout, &stderr)
	if code != 1 {
		t.Errorf("renderExecution code = %d, want 1 for conflicts", code)
	}
	if !strings.Contains(stdout.String(), "CREATED   a") || !strings.Contains(stdout.String(), "Conflicts: 1") {
		t.Errorf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "CONFLICT  c: managed path conflicts with existing state") {
		t.Errorf("conflict not routed to stderr: %q", stderr.String())
	}
}

func TestRenderExecutionReportsErrorAfterSummary(t *testing.T) {
	result := application.ExecutionResult{
		Conflicts: 1,
		Outcomes: []application.PathOutcome{
			{Path: "/vault/link", Status: application.OutcomeConflict, Detail: "unsafe symlink encountered"},
		},
	}
	err := &application.Error{Kind: application.ErrorUnsafe, Message: "execute filesystem plan", Cause: errors.New("unsafe symlink encountered")}
	var stdout, stderr bytes.Buffer
	code := renderExecution(result, err, "Test", "/vault", &stdout, &stderr)
	if code != 1 {
		t.Errorf("renderExecution code = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "Test complete:") {
		t.Errorf("summary missing: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unsafe symlink") {
		if !strings.Contains(stderr.String(), "unsafe managed filesystem state") {
			t.Errorf("safe error not reported: %q", stderr.String())
		}
	}
}

func TestSafeManagedPathCrossPlatformForms(t *testing.T) {
	tests := []struct{ root, value, want string }{
		{root: "/tmp/example", value: "/tmp/example", want: "."},
		{root: "/home/user/example", value: "/home/user/example/private/file", want: "private/file"},
		{root: `C:\Users\User\example`, value: `C:\Users\User\example\private\file`, want: "private/file"},
		{root: `\\server\share\example`, value: `\\server\share\example\private\file`, want: "private/file"},
		{root: "/safe/root", value: "/home/user/example", want: "<managed-path>"},
		{root: `C:\safe\root`, value: `C:\Users\User\example`, want: "<managed-path>"},
		{root: `\\safe\share`, value: `\\server\share\example`, want: "<managed-path>"},
	}
	for _, test := range tests {
		if got := safeManagedPath(test.value, test.root); got != test.want {
			t.Errorf("safeManagedPath(%q, %q) = %q, want %q", test.value, test.root, got, test.want)
		}
	}
}

func TestReportErrorDoesNotExposeFilesystemCausePaths(t *testing.T) {
	privatePaths := []string{
		"/tmp/example/private/file",
		"/home/user/example/private/file",
		`C:\Users\User\example\private\file`,
		`\\server\share\example\private\file`,
	}
	for _, privatePath := range privatePaths {
		t.Run(privatePath, func(t *testing.T) {
			err := &application.Error{
				Kind:    application.ErrorInternal,
				Message: "execute filesystem plan",
				Cause:   errors.New("open " + privatePath + ": permission denied"),
			}
			var stderr bytes.Buffer
			if code := reportError(err, &stderr); code != 1 {
				t.Fatalf("reportError code = %d, want 1", code)
			}
			if strings.Contains(stderr.String(), privatePath) {
				t.Errorf("stderr exposed private path: %q", stderr.String())
			}
			if !strings.Contains(stderr.String(), "execute filesystem plan: failed") {
				t.Errorf("stderr omitted safe classified message: %q", stderr.String())
			}
		})
	}
}

package course

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	studyfs "github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/workspace"
)

var fixedDate = time.Date(2026, time.July, 11, 18, 30, 0, 0, time.UTC)

func TestNewCoursePlan(t *testing.T) {
	paths := initializedWorkspace(t)
	plan, err := NewCoursePlanWithID(paths, "TCM Practical Help Desk", fixedDate, fixedID("course-test-id"))
	if err != nil {
		t.Fatalf("NewCoursePlan() error = %v", err)
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("plan.Validate() error = %v", err)
	}
	if len(plan.Operations) != 8 {
		t.Fatalf("len(Operations) = %d, want 8", len(plan.Operations))
	}
	wantRoot := filepath.Join(paths.Private, coursesDirectory, "TCM Practical Help Desk")
	if plan.Root != wantRoot || plan.Scope != studyfs.PlanScopeCourse {
		t.Errorf("plan root/scope = %q/%q", plan.Root, plan.Scope)
	}
	assertOperationPaths(t, plan, []string{
		wantRoot,
		filepath.Join(wantRoot, "Course Assets"),
		filepath.Join(wantRoot, "Course Assets", "Documents"),
		filepath.Join(wantRoot, "Course Assets", "Reference"),
		filepath.Join(wantRoot, "Course Assets", "Screenshots"),
		filepath.Join(wantRoot, "Modules"),
		filepath.Join(wantRoot, courseMetadataFile),
		filepath.Join(wantRoot, "Course Overview.md"),
	})
	overview := operationContent(t, plan, filepath.Join(wantRoot, "Course Overview.md"))
	for _, required := range []string{
		"id: course-test-id",
		"course_id: course-test-id",
		"slug: tcm-practical-help-desk",
		"visibility: private",
		"title: TCM Practical Help Desk",
		"created: 2026-07-11T18:30:00Z",
		"updated: 2026-07-11T18:30:00Z",
		"# TCM Practical Help Desk",
		"## Related Knowledge",
	} {
		if !strings.Contains(overview, required) {
			t.Errorf("overview does not contain %q", required)
		}
	}
	for _, operation := range plan.Operations {
		if operation.Path == paths.Portfolio || strings.HasPrefix(operation.Path, paths.Portfolio+string(filepath.Separator)) {
			t.Errorf("course operation enters public portfolio: %q", operation.Path)
		}
	}
	if _, err := os.Lstat(wantRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planning wrote course root: %v", err)
	}

	second, err := NewCoursePlanWithID(paths, "TCM Practical Help Desk", fixedDate, fixedID("course-test-id"))
	if err != nil {
		t.Fatalf("second NewCoursePlan() error = %v", err)
	}
	if !reflect.DeepEqual(plan, second) {
		t.Fatal("course plan is not deterministic")
	}
}

func TestCourseExecutionIsSafeAndIdempotentAcrossDates(t *testing.T) {
	paths := initializedWorkspace(t)
	plan, err := NewCoursePlanWithID(paths, "TCM Practical Help Desk", fixedDate, fixedID("course-stable-id"))
	if err != nil {
		t.Fatalf("NewCoursePlan() error = %v", err)
	}
	first, err := studyfs.Execute(plan)
	if err != nil || first.HasConflicts() || first.CreatedCount() != len(plan.Operations) {
		t.Fatalf("first Execute() report/error = %#v / %v", first, err)
	}
	metadataBefore, err := os.ReadFile(filepath.Join(plan.Root, courseMetadataFile))
	if err != nil {
		t.Fatal(err)
	}
	laterPlan, err := NewCoursePlanWithID(paths, "TCM Practical Help Desk", fixedDate.AddDate(0, 0, 3), failingID)
	if err != nil {
		t.Fatalf("later NewCoursePlan() error = %v", err)
	}
	second, err := studyfs.Execute(laterPlan)
	if err != nil || second.HasConflicts() || second.SkippedCount() != len(plan.Operations) {
		t.Fatalf("second Execute() report/error = %#v / %v", second, err)
	}
	after, err := os.ReadFile(filepath.Join(plan.Root, courseMetadataFile))
	if err != nil {
		t.Fatalf("read overview: %v", err)
	}
	if string(after) != string(metadataBefore) {
		t.Error("idempotent execution changed identity metadata")
	}
}

func TestCourseCollisions(t *testing.T) {
	paths := initializedWorkspace(t)
	plan, err := NewCoursePlanWithID(paths, "Help Desk", fixedDate, fixedID("course-one"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studyfs.Execute(plan); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Help-Desk", "Help_Desk", "help desk"} {
		if _, err := NewCoursePlanWithID(paths, name, fixedDate, fixedID("course-two")); !errors.Is(err, ErrCollision) {
			t.Errorf("name %q error = %v, want collision", name, err)
		}
	}
}

func TestUnicodeNormalizationReusesIdentity(t *testing.T) {
	paths := initializedWorkspace(t)
	plan, err := NewCoursePlanWithID(paths, "Café", fixedDate, fixedID("course-cafe"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studyfs.Execute(plan); err != nil {
		t.Fatal(err)
	}
	later, err := NewCoursePlanWithID(paths, "Cafe\u0301", fixedDate.AddDate(0, 0, 1), failingID)
	if err != nil {
		t.Fatal(err)
	}
	if later.Root != plan.Root || operationContent(t, later, filepath.Join(later.Root, courseMetadataFile)) != operationContent(t, plan, filepath.Join(plan.Root, courseMetadataFile)) {
		t.Fatal("Unicode-equivalent name did not reuse immutable identity")
	}
}

func TestGeneratedCourseIDCollisionRejected(t *testing.T) {
	paths := initializedWorkspace(t)
	plan, _ := NewCoursePlanWithID(paths, "One", fixedDate, fixedID("course-same"))
	if _, err := studyfs.Execute(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCoursePlanWithID(paths, "Two", fixedDate, fixedID("course-same")); !errors.Is(err, ErrCollision) {
		t.Fatalf("error = %v, want generated ID collision", err)
	}
}

func TestCourseExecutionPreservesConflicts(t *testing.T) {
	paths := initializedWorkspace(t)
	plan, err := NewCoursePlan(paths, "TCM Practical Help Desk", fixedDate)
	if err != nil {
		t.Fatalf("NewCoursePlan() error = %v", err)
	}
	if err := os.MkdirAll(plan.Root, 0o750); err != nil {
		t.Fatalf("create root: %v", err)
	}
	overview := filepath.Join(plan.Root, "Course Overview.md")
	if err := os.WriteFile(overview, []byte("preserve"), 0o640); err != nil {
		t.Fatalf("create conflict: %v", err)
	}
	report, err := studyfs.Execute(plan)
	if err != nil || !report.HasConflicts() {
		t.Fatalf("Execute() report/error = %#v / %v", report, err)
	}
	contents, err := os.ReadFile(overview)
	if err != nil || string(contents) != "preserve" {
		t.Fatalf("conflicting overview changed: %q / %v", contents, err)
	}
}

func TestCourseExecutionPreservesTypeConflicts(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, studyfs.Plan) string
	}{
		{
			name: "file where course directory required",
			prepare: func(t *testing.T, plan studyfs.Plan) string {
				t.Helper()
				if err := os.WriteFile(plan.Root, []byte("preserve"), 0o640); err != nil {
					t.Fatalf("create conflicting file: %v", err)
				}
				return plan.Root
			},
		},
		{
			name: "directory where overview required",
			prepare: func(t *testing.T, plan studyfs.Plan) string {
				t.Helper()
				path := filepath.Join(plan.Root, "Course Overview.md")
				if err := os.MkdirAll(path, 0o750); err != nil {
					t.Fatalf("create conflicting directory: %v", err)
				}
				return path
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := initializedWorkspace(t)
			plan, err := NewCoursePlan(paths, "TCM Practical Help Desk", fixedDate)
			if err != nil {
				t.Fatalf("NewCoursePlan() error = %v", err)
			}
			path := test.prepare(t, plan)
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatalf("stat before execution: %v", err)
			}
			report, err := studyfs.Execute(plan)
			if err != nil || !report.HasConflicts() {
				t.Fatalf("Execute() report/error = %#v / %v", report, err)
			}
			after, err := os.Lstat(path)
			if err != nil || before.IsDir() != after.IsDir() {
				t.Fatalf("conflicting path changed: %v", err)
			}
			if !before.IsDir() {
				contents, err := os.ReadFile(path)
				if err != nil || string(contents) != "preserve" {
					t.Fatalf("conflicting file changed: %q / %v", contents, err)
				}
			}
		})
	}
}

func TestCourseExecutionRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	paths := initializedWorkspace(t)
	plan, err := NewCoursePlan(paths, "TCM Practical Help Desk", fixedDate)
	if err != nil {
		t.Fatalf("NewCoursePlan() error = %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, plan.Root); err != nil {
		t.Skipf("symlink creation unsupported: %v", err)
	}
	if _, err := studyfs.Execute(plan); err == nil {
		t.Fatal("Execute() error = nil, want symlink error")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("executor wrote through symlink: %v / %v", entries, err)
	}
}

func initializedWorkspace(t *testing.T) workspace.Paths {
	t.Helper()
	paths, err := workspace.PathsFromRoot(filepath.Join(t.TempDir(), "StudyPilot"))
	if err != nil {
		t.Fatalf("PathsFromRoot() error = %v", err)
	}
	plan, err := studyfs.NewPlan(paths)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	if report, err := studyfs.Execute(plan); err != nil || report.HasConflicts() {
		t.Fatalf("initialize workspace report/error = %#v / %v", report, err)
	}
	return paths
}

func assertOperationPaths(t *testing.T, plan studyfs.Plan, expected []string) {
	t.Helper()
	if len(plan.Operations) != len(expected) {
		t.Fatalf("operation count = %d, want %d", len(plan.Operations), len(expected))
	}
	for index, path := range expected {
		if plan.Operations[index].Path != path {
			t.Errorf("operation %d path = %q, want %q", index, plan.Operations[index].Path, path)
		}
	}
}

func operationContent(t *testing.T, plan studyfs.Plan, path string) string {
	t.Helper()
	for _, operation := range plan.Operations {
		if operation.Path == path {
			return operation.Content
		}
	}
	t.Fatalf("no operation for %q", path)
	return ""
}

func fixedID(id string) IDGenerator    { return func(string) (string, error) { return id, nil } }
func failingID(string) (string, error) { return "", errors.New("ID generator should not be called") }

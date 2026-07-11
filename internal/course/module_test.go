package course

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	studyfs "github.com/Arameair/studypilot/internal/filesystem"
)

func TestNewModulePlan(t *testing.T) {
	paths := initializedWorkspace(t)
	coursePlan, err := NewCoursePlan(paths, "TCM Practical Help Desk", fixedDate)
	if err != nil {
		t.Fatalf("NewCoursePlan() error = %v", err)
	}
	if _, err := studyfs.Execute(coursePlan); err != nil {
		t.Fatalf("execute course plan: %v", err)
	}

	plan, err := NewModulePlanWithID(paths, "TCM Practical Help Desk", 3, "Windows Services", fixedDate, fixedID("module-test-id"))
	if err != nil {
		t.Fatalf("NewModulePlan() error = %v", err)
	}
	root := filepath.Join(coursePlan.Root, "Modules", "03 - Windows Services")
	if plan.Root != root || plan.Scope != studyfs.PlanScopeModule || len(plan.Operations) != 11 {
		t.Fatalf("module plan root/scope/count = %q/%q/%d", plan.Root, plan.Scope, len(plan.Operations))
	}
	assertOperationPaths(t, plan, []string{
		root,
		filepath.Join(root, "Assets"),
		filepath.Join(root, "Assets", "Audio"),
		filepath.Join(root, "Assets", "Documents"),
		filepath.Join(root, "Assets", "Screenshots"),
		filepath.Join(root, "Assets", "Video"),
		filepath.Join(root, "Notes"),
		filepath.Join(root, "Sessions"),
		filepath.Join(root, "Transcripts"),
		filepath.Join(root, moduleMetadataFile),
		filepath.Join(root, "Module Overview.md"),
	})
	overview := operationContent(t, plan, filepath.Join(root, "Module Overview.md"))
	for _, required := range []string{
		"id: module-test-id",
		"module_id: module-test-id",
		"visibility: private",
		"module_number: 3",
		"title: Windows Services",
		"created: 2026-07-11",
		"# 03 - Windows Services",
		"## Assets",
	} {
		if !strings.Contains(overview, required) {
			t.Errorf("overview does not contain %q", required)
		}
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planning wrote module root: %v", err)
	}
	second, err := NewModulePlanWithID(paths, "TCM Practical Help Desk", 3, "Windows Services", fixedDate, fixedID("module-test-id"))
	if err != nil || !reflect.DeepEqual(plan, second) {
		t.Fatalf("module plan is not deterministic: %v", err)
	}
}

func TestNewModulePlanRequiresExistingCourse(t *testing.T) {
	paths := initializedWorkspace(t)
	if _, err := NewModulePlan(paths, "Missing Course", 1, "Introduction", fixedDate); !errors.Is(err, ErrMissingCourse) {
		t.Fatalf("NewModulePlan() error = %v, want ErrMissingCourse", err)
	}
}

func TestModuleExecutionIsIdempotentAcrossDates(t *testing.T) {
	paths := initializedWorkspace(t)
	coursePlan, err := NewCoursePlan(paths, "TCM Practical Help Desk", fixedDate)
	if err != nil {
		t.Fatalf("NewCoursePlan() error = %v", err)
	}
	if _, err := studyfs.Execute(coursePlan); err != nil {
		t.Fatalf("execute course plan: %v", err)
	}
	plan, err := NewModulePlanWithID(paths, "TCM Practical Help Desk", 12, "Windows Services", fixedDate, fixedID("module-stable-id"))
	if err != nil {
		t.Fatalf("NewModulePlan() error = %v", err)
	}
	first, err := studyfs.Execute(plan)
	if err != nil || first.CreatedCount() != 11 || first.HasConflicts() {
		t.Fatalf("first Execute() report/error = %#v / %v", first, err)
	}
	metadataBefore, err := os.ReadFile(filepath.Join(plan.Root, moduleMetadataFile))
	if err != nil {
		t.Fatal(err)
	}
	laterPlan, err := NewModulePlanWithID(paths, "TCM Practical Help Desk", 12, "Windows Services", fixedDate.AddDate(0, 0, 5), failingID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := studyfs.Execute(laterPlan)
	if err != nil || second.SkippedCount() != 11 || second.HasConflicts() {
		t.Fatalf("second Execute() report/error = %#v / %v", second, err)
	}
	metadataAfter, err := os.ReadFile(filepath.Join(plan.Root, moduleMetadataFile))
	if err != nil || string(metadataAfter) != string(metadataBefore) {
		t.Fatal("next-day rerun changed immutable module metadata")
	}
}

func TestModuleCollisions(t *testing.T) {
	paths := initializedWorkspace(t)
	coursePlan, _ := NewCoursePlanWithID(paths, "Course", fixedDate, fixedID("course-parent"))
	if _, err := studyfs.Execute(coursePlan); err != nil {
		t.Fatal(err)
	}
	plan, err := NewModulePlanWithID(paths, "course-parent", 3, "Help Desk", fixedDate, fixedID("module-one"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studyfs.Execute(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := NewModulePlanWithID(paths, "Course", 3, "Different", fixedDate, fixedID("module-two")); !errors.Is(err, ErrCollision) {
		t.Errorf("duplicate number error = %v", err)
	}
	if _, err := NewModulePlanWithID(paths, "Course", 4, "Help_Desk", fixedDate, fixedID("module-three")); !errors.Is(err, ErrCollision) {
		t.Errorf("duplicate slug error = %v", err)
	}
}

func TestNewModulePlanRejectsInvalidNumber(t *testing.T) {
	paths := initializedWorkspace(t)
	if _, err := NewModulePlan(paths, "Course", 0, "Module", fixedDate); !errors.Is(err, ErrInvalidModuleNumber) {
		t.Fatalf("NewModulePlan() error = %v", err)
	}
}

func TestModuleExecutionRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	paths := initializedWorkspace(t)
	coursePlan, _ := NewCoursePlanWithID(paths, "Course", fixedDate, fixedID("course-parent"))
	if _, err := studyfs.Execute(coursePlan); err != nil {
		t.Fatal(err)
	}
	plan, err := NewModulePlanWithID(paths, "course-parent", 1, "Module", fixedDate, fixedID("module-one"))
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, plan.Root); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := studyfs.Execute(plan); err == nil {
		t.Fatal("symlinked module root accepted")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("executor wrote through module symlink")
	}
}

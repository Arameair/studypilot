package application

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func createTestCourse(t *testing.T, service *Service, root, name string) {
	t.Helper()
	if _, err := service.CreateCourse(context.Background(), CourseCreateRequest{Root: root, Name: name}); err != nil {
		t.Fatalf("CreateCourse(%q) error = %v", name, err)
	}
}

func TestCreateModule(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("mod"))
	root := testRoot(t)
	initWorkspace(t, service, root)
	createTestCourse(t, service, root, "TCM Practical Help Desk")

	result, err := service.CreateModule(context.Background(), ModuleCreateRequest{
		Root: root, CourseRef: "TCM Practical Help Desk", Number: 3, Name: "Windows Services",
	})
	if err != nil {
		t.Fatalf("CreateModule() error = %v", err)
	}
	if result.Created != 11 || result.Conflicts != 0 {
		t.Errorf("counts = created %d conflicts %d, want 11/0", result.Created, result.Conflicts)
	}
	moduleRoot := filepath.Join(coursesDir(t, root), "TCM Practical Help Desk", "Modules", "03 - Windows Services")
	for _, relative := range []string{"Sessions", "Notes", "Transcripts", filepath.Join("Assets", "Audio")} {
		assertDir(t, filepath.Join(moduleRoot, relative))
	}
}

func TestPlanModuleCreationWritesNothing(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("mod"))
	root := testRoot(t)
	initWorkspace(t, service, root)
	createTestCourse(t, service, root, "TCM Practical Help Desk")

	result, err := service.PlanModuleCreation(context.Background(), ModuleCreateRequest{
		Root: root, CourseRef: "TCM Practical Help Desk", Number: 3, Name: "Windows Services",
	})
	if err != nil {
		t.Fatalf("PlanModuleCreation() error = %v", err)
	}
	moduleRoot := filepath.Join(coursesDir(t, root), "TCM Practical Help Desk", "Modules", "03 - Windows Services")
	if len(result.Operations) != 11 {
		t.Errorf("plan operation count = %d, want 11", len(result.Operations))
	}
	assertNotExist(t, moduleRoot)
}

func TestCreateModuleMissingCourseIsClassified(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("mod"))
	root := testRoot(t)
	initWorkspace(t, service, root)

	_, err := service.CreateModule(context.Background(), ModuleCreateRequest{
		Root: root, CourseRef: "Missing", Number: 1, Name: "Intro",
	})
	if Classify(err) != ErrorNotFound {
		t.Errorf("missing course Classify = %q (%v), want not found", Classify(err), err)
	}
}

func TestCreateModuleDuplicateNumberIsClassified(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("mod"))
	root := testRoot(t)
	initWorkspace(t, service, root)
	createTestCourse(t, service, root, "TCM Practical Help Desk")
	base := ModuleCreateRequest{Root: root, CourseRef: "TCM Practical Help Desk", Number: 3, Name: "First"}
	if _, err := service.CreateModule(context.Background(), base); err != nil {
		t.Fatalf("first CreateModule() error = %v", err)
	}
	duplicate := ModuleCreateRequest{Root: root, CourseRef: "TCM Practical Help Desk", Number: 3, Name: "Second"}
	if _, err := service.CreateModule(context.Background(), duplicate); Classify(err) != ErrorCollision {
		t.Errorf("duplicate number Classify = %q, want collision", Classify(err))
	}
}

func TestCreateModuleInvalidNumberIsClassified(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("mod"))
	root := testRoot(t)
	initWorkspace(t, service, root)
	createTestCourse(t, service, root, "TCM Practical Help Desk")

	_, err := service.CreateModule(context.Background(), ModuleCreateRequest{
		Root: root, CourseRef: "TCM Practical Help Desk", Number: 0, Name: "Intro",
	})
	if Classify(err) != ErrorInvalidInput {
		t.Errorf("invalid number Classify = %q (%v), want invalid input", Classify(err), err)
	}
}

func TestCreateModuleAmbiguousCourseIsClassified(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("mod"))
	root := testRoot(t)
	initWorkspace(t, service, root)
	base := coursesDir(t, root)
	writeManagedCourse(t, base, "One", "course-duplicate", "One", "one")
	writeManagedCourse(t, base, "Two", "course-duplicate", "Two", "two")

	_, err := service.CreateModule(context.Background(), ModuleCreateRequest{
		Root: root, CourseRef: "course-duplicate", Number: 1, Name: "Intro",
	})
	if Classify(err) != ErrorAmbiguous {
		t.Errorf("ambiguous course Classify = %q (%v), want ambiguous", Classify(err), err)
	}
}

func TestCreateModuleIsIdempotentAcrossDates(t *testing.T) {
	first := newTestService(t, fixedClock(fixedDate), prefixedID("stable"))
	root := testRoot(t)
	initWorkspace(t, first, root)
	createTestCourse(t, first, root, "TCM Practical Help Desk")
	req := ModuleCreateRequest{Root: root, CourseRef: "TCM Practical Help Desk", Number: 12, Name: "Windows Services"}
	if _, err := first.CreateModule(context.Background(), req); err != nil {
		t.Fatalf("first CreateModule() error = %v", err)
	}
	metadataPath := filepath.Join(coursesDir(t, root), "TCM Practical Help Desk", "Modules", "12 - Windows Services", ".studypilot-module.json")
	before := readFile(t, metadataPath)

	later := newTestService(t, fixedClock(fixedDate.AddDate(0, 0, 5)), failingID)
	result, err := later.CreateModule(context.Background(), req)
	if err != nil {
		t.Fatalf("later CreateModule() error = %v", err)
	}
	if result.Created != 0 || result.Skipped != 11 {
		t.Errorf("rerun counts = created %d skipped %d, want 0/11", result.Created, result.Skipped)
	}
	if after := readFile(t, metadataPath); after != before {
		t.Error("cross-date rerun changed immutable module metadata")
	}
}

// TestModulePlanStaysWithinResolvedCourse confirms the sibling-course protection
// holds across the application boundary: a module plan for one course never
// references paths in a sibling course.
func TestModulePlanStaysWithinResolvedCourse(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), sequentialID())
	root := testRoot(t)
	initWorkspace(t, service, root)
	createTestCourse(t, service, root, "Course A")
	createTestCourse(t, service, root, "Course B")

	result, err := service.PlanModuleCreation(context.Background(), ModuleCreateRequest{
		Root: root, CourseRef: "Course A", Number: 1, Name: "Intro",
	})
	if err != nil {
		t.Fatalf("PlanModuleCreation() error = %v", err)
	}
	courseAModules := filepath.Join(coursesDir(t, root), "Course A", "Modules") + string(filepath.Separator)
	courseB := filepath.Join(coursesDir(t, root), "Course B")
	for _, operation := range result.Operations {
		if !strings.HasPrefix(operation.Path, courseAModules) {
			t.Errorf("module operation escapes Course A modules: %q", operation.Path)
		}
		if strings.HasPrefix(operation.Path, courseB) {
			t.Errorf("module operation enters sibling Course B: %q", operation.Path)
		}
	}
}

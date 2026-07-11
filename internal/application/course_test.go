package application

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateCourse(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("help-desk"))
	root := testRoot(t)
	initWorkspace(t, service, root)

	result, err := service.CreateCourse(context.Background(), CourseCreateRequest{Root: root, Name: "TCM Practical Help Desk"})
	if err != nil {
		t.Fatalf("CreateCourse() error = %v", err)
	}
	if result.Created != 8 || result.Conflicts != 0 {
		t.Errorf("counts = created %d conflicts %d, want 8/0", result.Created, result.Conflicts)
	}
	overview := readFile(t, filepath.Join(coursesDir(t, root), "TCM Practical Help Desk", "Course Overview.md"))
	if !strings.Contains(overview, "created: 2026-07-11") {
		t.Errorf("course overview timestamp not controlled by injected clock: %q", overview)
	}
}

func TestPlanCourseCreationWritesNothing(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("help-desk"))
	root := testRoot(t)
	initWorkspace(t, service, root)

	result, err := service.PlanCourseCreation(context.Background(), CourseCreateRequest{Root: root, Name: "TCM Practical Help Desk"})
	if err != nil {
		t.Fatalf("PlanCourseCreation() error = %v", err)
	}
	if len(result.Operations) != 8 {
		t.Errorf("plan operation count = %d, want 8", len(result.Operations))
	}
	assertNotExist(t, filepath.Join(coursesDir(t, root), "TCM Practical Help Desk"))
}

func TestCreateCourseIsIdempotentAcrossDates(t *testing.T) {
	first := newTestService(t, fixedClock(fixedDate), prefixedID("stable"))
	root := testRoot(t)
	initWorkspace(t, first, root)
	req := CourseCreateRequest{Root: root, Name: "TCM Practical Help Desk"}
	if _, err := first.CreateCourse(context.Background(), req); err != nil {
		t.Fatalf("first CreateCourse() error = %v", err)
	}
	metadataPath := filepath.Join(coursesDir(t, root), "TCM Practical Help Desk", courseMetadataFile)
	before := readFile(t, metadataPath)

	later := newTestService(t, fixedClock(fixedDate.AddDate(0, 0, 3)), failingID)
	result, err := later.CreateCourse(context.Background(), req)
	if err != nil {
		t.Fatalf("later CreateCourse() error = %v", err)
	}
	if result.Created != 0 || result.Skipped != 8 {
		t.Errorf("rerun counts = created %d skipped %d, want 0/8", result.Created, result.Skipped)
	}
	if after := readFile(t, metadataPath); after != before {
		t.Error("cross-date rerun changed immutable course metadata")
	}
}

func TestCreateCourseSlugCollisionIsClassified(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("collide"))
	root := testRoot(t)
	initWorkspace(t, service, root)
	if _, err := service.CreateCourse(context.Background(), CourseCreateRequest{Root: root, Name: "Help Desk"}); err != nil {
		t.Fatalf("CreateCourse() error = %v", err)
	}
	_, err := service.CreateCourse(context.Background(), CourseCreateRequest{Root: root, Name: "Help-Desk"})
	if Classify(err) != ErrorCollision {
		t.Errorf("slug collision Classify = %q (%v), want collision", Classify(err), err)
	}
}

func TestCreateCourseUnmanagedDirectoryIsClassified(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("new"))
	root := testRoot(t)
	initWorkspace(t, service, root)
	if err := os.Mkdir(filepath.Join(coursesDir(t, root), "Legacy Course"), 0o750); err != nil {
		t.Fatalf("create unmanaged directory: %v", err)
	}
	_, err := service.CreateCourse(context.Background(), CourseCreateRequest{Root: root, Name: "New Course"})
	if Classify(err) != ErrorConflict {
		t.Errorf("unmanaged directory Classify = %q (%v), want conflict", Classify(err), err)
	}
}

func TestCreateCourseMalformedMetadataIsClassified(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("new"))
	root := testRoot(t)
	initWorkspace(t, service, root)
	managed := filepath.Join(coursesDir(t, root), "Managed")
	if err := os.Mkdir(managed, 0o750); err != nil {
		t.Fatalf("create managed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(managed, courseMetadataFile), []byte("{"), 0o640); err != nil {
		t.Fatalf("write malformed metadata: %v", err)
	}
	_, err := service.CreateCourse(context.Background(), CourseCreateRequest{Root: root, Name: "New"})
	if Classify(err) != ErrorConflict {
		t.Errorf("malformed metadata Classify = %q (%v), want conflict", Classify(err), err)
	}
}

func TestCreateCourseInvalidNameIsClassified(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("new"))
	root := testRoot(t)
	initWorkspace(t, service, root)
	_, err := service.CreateCourse(context.Background(), CourseCreateRequest{Root: root, Name: "bad/name"})
	if Classify(err) != ErrorInvalidInput {
		t.Errorf("invalid name Classify = %q (%v), want invalid input", Classify(err), err)
	}
}

func TestCreateCourseGeneratorFailureIsClassified(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), failingID)
	root := testRoot(t)
	initWorkspace(t, service, root)
	_, err := service.CreateCourse(context.Background(), CourseCreateRequest{Root: root, Name: "New"})
	if err == nil {
		t.Fatal("CreateCourse with failing generator = nil error")
	}
	if Classify(err) != ErrorInternal {
		t.Errorf("generator failure Classify = %q, want internal", Classify(err))
	}
}

func TestCreateCourseConflictSurfacesInResult(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("conflict"))
	root := testRoot(t)
	initWorkspace(t, service, root)
	req := CourseCreateRequest{Root: root, Name: "TCM Practical Help Desk"}
	if _, err := service.CreateCourse(context.Background(), req); err != nil {
		t.Fatalf("CreateCourse() error = %v", err)
	}
	overview := filepath.Join(coursesDir(t, root), "TCM Practical Help Desk", "Course Overview.md")
	if err := os.WriteFile(overview, []byte("keep"), 0o640); err != nil {
		t.Fatalf("overwrite overview: %v", err)
	}

	result, err := service.CreateCourse(context.Background(), req)
	if err != nil {
		t.Fatalf("second CreateCourse() error = %v, want conflict in result", err)
	}
	if result.Conflicts == 0 {
		t.Error("expected conflict in result")
	}
	if content := readFile(t, overview); content != "keep" {
		t.Errorf("conflicting overview overwritten: %q", content)
	}
}

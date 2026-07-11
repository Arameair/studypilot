package application

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlanWorkspaceInitializationDefaultRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	service := newTestService(t, fixedClock(fixedDate), prefixedID("ws"))

	result, err := service.PlanWorkspaceInitialization(context.Background(), WorkspaceRequest{})
	if err != nil {
		t.Fatalf("PlanWorkspaceInitialization() error = %v", err)
	}
	root := filepath.Join(home, "Documents", "StudyPilot")
	if !planCoversPath(result, root) {
		t.Errorf("plan does not include default root %q", root)
	}
	if !planCoversPath(result, filepath.Join(root, "Learning-Vault-Private")) {
		t.Error("plan does not include private vault")
	}
	assertNotExist(t, root)
}

func TestPlanWorkspaceInitializationCustomRootIsDeterministicWithoutWrites(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("ws"))
	root := testRoot(t)

	first, err := service.PlanWorkspaceInitialization(context.Background(), WorkspaceRequest{Root: root})
	if err != nil {
		t.Fatalf("first plan error = %v", err)
	}
	second, err := service.PlanWorkspaceInitialization(context.Background(), WorkspaceRequest{Root: root})
	if err != nil {
		t.Fatalf("second plan error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("workspace plan is not deterministic")
	}
	if len(first.Operations) != 26 {
		t.Errorf("plan operation count = %d, want 26", len(first.Operations))
	}
	assertNotExist(t, root)
}

func TestInitializeWorkspaceCreatesStructure(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("ws"))
	root := testRoot(t)

	result, err := service.InitializeWorkspace(context.Background(), WorkspaceRequest{Root: root})
	if err != nil {
		t.Fatalf("InitializeWorkspace() error = %v", err)
	}
	if result.Created != 26 || result.Skipped != 0 || result.Conflicts != 0 {
		t.Errorf("counts = created %d skipped %d conflicts %d", result.Created, result.Skipped, result.Conflicts)
	}
	assertDir(t, filepath.Join(root, "Learning-Vault-Private"))
	assertDir(t, filepath.Join(root, "IT-Knowledge-Portfolio"))
}

func TestInitializeWorkspaceIsIdempotent(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("ws"))
	root := testRoot(t)
	initWorkspace(t, service, root)

	result, err := service.InitializeWorkspace(context.Background(), WorkspaceRequest{Root: root})
	if err != nil {
		t.Fatalf("second InitializeWorkspace() error = %v", err)
	}
	if result.Created != 0 || result.Skipped != 26 || result.Conflicts != 0 {
		t.Errorf("idempotent counts = created %d skipped %d conflicts %d", result.Created, result.Skipped, result.Conflicts)
	}
}

func TestInitializeWorkspaceConflictSurfacesInResult(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), prefixedID("ws"))
	root := testRoot(t)
	readme := filepath.Join(root, "Learning-Vault-Private", "README.md")
	if err := writeFileTree(t, readme, "user content"); err != nil {
		t.Fatalf("prepare conflict: %v", err)
	}

	result, err := service.InitializeWorkspace(context.Background(), WorkspaceRequest{Root: root})
	if err != nil {
		t.Fatalf("InitializeWorkspace() error = %v, want conflict reported without error", err)
	}
	if result.Conflicts == 0 {
		t.Fatal("expected at least one conflict in result")
	}
	var sawConflict bool
	for _, outcome := range result.Outcomes {
		if outcome.Path == readme && outcome.Status == OutcomeConflict {
			sawConflict = true
		}
	}
	if !sawConflict {
		t.Error("README conflict not present in outcomes")
	}
	if content := readFile(t, readme); content != "user content" {
		t.Errorf("conflicting file overwritten: %q", content)
	}
}

package workspace_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/workspace"
)

func TestProposedPathsUsesDocumentsVaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got, err := workspace.ProposedPaths()
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != filepath.Join(home, "Documents", "vaults") {
		t.Fatalf("Root = %q", got.Root)
	}
}

func TestInspectSetupRootStates(t *testing.T) {
	home, source := t.TempDir(), t.TempDir()
	nonexistent := filepath.Join(t.TempDir(), "new root")
	got, err := workspace.InspectSetupRoot(nonexistent, home, source)
	if err != nil || got.Disposition != workspace.SetupNonexistent || !got.CanInitialize {
		t.Fatalf("nonexistent = %+v, %v", got, err)
	}
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err = workspace.InspectSetupRoot(empty, home, source)
	if err != nil || got.Disposition != workspace.SetupEmpty || !got.CanInitialize {
		t.Fatalf("empty = %+v, %v", got, err)
	}
	conflicting := filepath.Join(t.TempDir(), "conflicting")
	if err := os.Mkdir(conflicting, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conflicting, "unrelated.txt"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = workspace.InspectSetupRoot(conflicting, home, source)
	if err != nil || got.Disposition != workspace.SetupConflicting || got.CanInitialize {
		t.Fatalf("conflicting = %+v, %v", got, err)
	}
}

func TestInspectSetupRootRecognizesInitializedWorkspace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	paths, _ := workspace.PathsFromRoot(root)
	plan, _ := filesystem.NewPlan(paths)
	if report, err := filesystem.Execute(plan); err != nil || report.HasConflicts() {
		t.Fatalf("Execute() = %+v, %v", report, err)
	}
	got, err := workspace.InspectSetupRoot(root, t.TempDir(), t.TempDir())
	if err != nil || got.Disposition != workspace.SetupAdoptable || !got.Initialized {
		t.Fatalf("inspection = %+v, %v", got, err)
	}
}

func TestInspectSetupRootRejectsUnsafeSelections(t *testing.T) {
	home, source := t.TempDir(), t.TempDir()
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []string{
		"relative",
		filepath.VolumeName(home) + string(filepath.Separator),
		home,
		source,
		filepath.Join(source, "child"),
		filepath.Join(t.TempDir(), "candidate") + string(filepath.Separator) + ".." + string(filepath.Separator) + "escape",
		file,
	}
	for _, root := range tests {
		if _, err := workspace.InspectSetupRoot(root, home, source); err == nil {
			t.Errorf("InspectSetupRoot(%q) error = nil", root)
		}
	}
}

func TestInspectSetupRootRejectsSymlink(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := workspace.InspectSetupRoot(link, t.TempDir(), t.TempDir()); !errors.Is(err, workspace.ErrUnsafeSetupRoot) {
		t.Fatalf("error = %v", err)
	}
}

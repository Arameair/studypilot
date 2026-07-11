package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/Arameair/studypilot/internal/workspace"
)

func TestExecuteCreatesCompleteWorkspace(t *testing.T) {
	plan := testExecutionPlan(t)
	report, err := Execute(plan)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if report.HasConflicts() {
		t.Fatalf("Execute() reported %d conflicts", report.ConflictCount())
	}
	if report.CreatedCount() != len(plan.Operations) {
		t.Errorf("CreatedCount() = %d, want %d", report.CreatedCount(), len(plan.Operations))
	}
	if report.SkippedCount() != 0 {
		t.Errorf("SkippedCount() = %d, want 0", report.SkippedCount())
	}
	if len(report.Results) != len(plan.Operations) {
		t.Fatalf("len(Results) = %d, want %d", len(report.Results), len(plan.Operations))
	}

	assertExecutedContract(t, filepath.Join(plan.Root, privateVaultName), workspace.PrivateVaultContract())
	assertExecutedContract(t, filepath.Join(plan.Root, portfolioName), workspace.PublicPortfolioContract())
	assertRestrictivePermissions(t, plan)
}

func TestExecuteIsIdempotent(t *testing.T) {
	plan := testExecutionPlan(t)
	first, err := Execute(plan)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if first.CreatedCount() == 0 || first.HasConflicts() {
		t.Fatalf("first report = %#v", first)
	}
	contentsBefore := readPlannedFiles(t, plan)

	second, err := Execute(plan)
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if second.CreatedCount() != 0 {
		t.Errorf("second CreatedCount() = %d, want 0", second.CreatedCount())
	}
	if second.SkippedCount() != len(plan.Operations) {
		t.Errorf("second SkippedCount() = %d, want %d", second.SkippedCount(), len(plan.Operations))
	}
	if second.HasConflicts() {
		t.Errorf("second ConflictCount() = %d, want 0", second.ConflictCount())
	}
	if contentsAfter := readPlannedFiles(t, plan); !mapsEqual(contentsBefore, contentsAfter) {
		t.Error("second execution changed file contents")
	}
}

func TestExecuteExistingMatchingFile(t *testing.T) {
	plan := testExecutionPlan(t)
	file := firstFileOperation(t, plan)
	if err := os.MkdirAll(filepath.Dir(file.Path), directoryPermissions); err != nil {
		t.Fatalf("create fixture parent: %v", err)
	}
	if err := os.WriteFile(file.Path, []byte(file.Content), filePermissions); err != nil {
		t.Fatalf("create matching file: %v", err)
	}

	report, err := Execute(plan)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	result := resultForPath(t, report, file.Path)
	if result.Status != ResultSkipped {
		t.Errorf("status = %q, want %q", result.Status, ResultSkipped)
	}
}

func TestExecuteExistingConflictingFile(t *testing.T) {
	plan := testExecutionPlan(t)
	file := firstFileOperation(t, plan)
	if err := os.MkdirAll(filepath.Dir(file.Path), directoryPermissions); err != nil {
		t.Fatalf("create fixture parent: %v", err)
	}
	original := []byte("user content that must be preserved")
	if err := os.WriteFile(file.Path, original, filePermissions); err != nil {
		t.Fatalf("create conflicting file: %v", err)
	}

	report, err := Execute(plan)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	result := resultForPath(t, report, file.Path)
	if result.Status != ResultConflict || !report.HasConflicts() {
		t.Errorf("conflicting result = %#v", result)
	}
	contents, err := os.ReadFile(file.Path)
	if err != nil {
		t.Fatalf("read conflicting file: %v", err)
	}
	if !slices.Equal(contents, original) {
		t.Errorf("conflicting file was modified: %q", contents)
	}
}

func TestExecuteTypeConflicts(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, Plan) string
	}{
		{
			name: "file where directory required",
			prepare: func(t *testing.T, plan Plan) string {
				t.Helper()
				if err := os.Mkdir(plan.Root, directoryPermissions); err != nil {
					t.Fatalf("create workspace root: %v", err)
				}
				path := filepath.Join(plan.Root, privateVaultName)
				if err := os.WriteFile(path, []byte("preserve me"), filePermissions); err != nil {
					t.Fatalf("create conflicting file: %v", err)
				}
				return path
			},
		},
		{
			name: "directory where file required",
			prepare: func(t *testing.T, plan Plan) string {
				t.Helper()
				file := firstFileOperation(t, plan)
				if err := os.MkdirAll(file.Path, directoryPermissions); err != nil {
					t.Fatalf("create conflicting directory: %v", err)
				}
				return file.Path
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := testExecutionPlan(t)
			conflictPath := test.prepare(t, plan)
			report, err := Execute(plan)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if result := resultForPath(t, report, conflictPath); result.Status != ResultConflict {
				t.Errorf("status = %q, want %q", result.Status, ResultConflict)
			}
			if _, err := os.Lstat(conflictPath); err != nil {
				t.Errorf("conflicting path was not preserved: %v", err)
			}
			portfolioReadme := filepath.Join(plan.Root, portfolioName, "README.md")
			if _, err := os.Stat(portfolioReadme); err != nil {
				t.Errorf("safe later portfolio operation was not completed: %v", err)
			}
		})
	}
}

func TestExecuteRejectsSymlinkTraversal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	plan := testExecutionPlan(t)
	outside := t.TempDir()
	if err := os.Mkdir(plan.Root, directoryPermissions); err != nil {
		t.Fatalf("create workspace root: %v", err)
	}
	symlink := filepath.Join(plan.Root, privateVaultName)
	if err := os.Symlink(outside, symlink); err != nil {
		t.Skipf("symlink creation unsupported: %v", err)
	}

	report, err := Execute(plan)
	if err == nil {
		t.Fatal("Execute() error = nil, want unsafe symlink error")
	}
	if !report.HasConflicts() {
		t.Fatal("symlink was not reported as a conflict")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("read outside directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("execution wrote outside workspace: %v", entries)
	}
}

func TestExecuteRejectsSymlinkRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	container := t.TempDir()
	outside := t.TempDir()
	root := filepath.Join(container, "StudyPilot")
	if err := os.Symlink(outside, root); err != nil {
		t.Skipf("symlink creation unsupported: %v", err)
	}
	paths := workspace.Paths{
		Root:      root,
		Private:   filepath.Join(root, privateVaultName),
		Portfolio: filepath.Join(root, portfolioName),
	}
	plan, err := NewPlan(paths)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	if _, err := Execute(plan); err == nil {
		t.Fatal("Execute() error = nil, want symlink root error")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("read outside directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("execution wrote through symlink root: %v", entries)
	}
}

func TestAtomicFileCreationCleansTemporaryFileOnConflict(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "existing.md")
	if err := os.WriteFile(destination, []byte("existing"), filePermissions); err != nil {
		t.Fatalf("create destination: %v", err)
	}
	if err := createFileAtomically(destination, "planned"); !errors.Is(err, errDestinationExists) {
		t.Fatalf("createFileAtomically() error = %v, want destination exists", err)
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".studypilot-tmp-*"))
	if err != nil {
		t.Fatalf("glob temporary files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain after failure: %v", matches)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(contents) != "existing" {
		t.Errorf("destination was overwritten: %q", contents)
	}
}

func TestExecuteRejectsInvalidPlanBeforeWrites(t *testing.T) {
	root := filepath.Join(t.TempDir(), "must-not-exist")
	invalid := Plan{Root: root}
	if _, err := Execute(invalid); err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid plan modified filesystem: %v", err)
	}
}

func testExecutionPlan(t *testing.T) Plan {
	t.Helper()
	root := filepath.Join(t.TempDir(), "StudyPilot")
	paths, err := workspace.PathsFromRoot(root)
	if err != nil {
		t.Fatalf("PathsFromRoot() error = %v", err)
	}
	plan, err := NewPlan(paths)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	return plan
}

func assertExecutedContract(t *testing.T, root string, contract workspace.VaultContract) {
	t.Helper()
	for _, directory := range contract.Directories {
		info, err := os.Stat(filepath.Join(root, directory))
		if err != nil {
			t.Errorf("required directory %q: %v", directory, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("required directory %q is not a directory", directory)
		}
	}
	for _, file := range contract.Files {
		contents, err := os.ReadFile(filepath.Join(root, file.Path))
		if err != nil {
			t.Errorf("required file %q: %v", file.Path, err)
			continue
		}
		if string(contents) != file.Content {
			t.Errorf("required file %q content differs", file.Path)
		}
	}
}

func assertRestrictivePermissions(t *testing.T, plan Plan) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	for _, operation := range plan.Operations {
		info, err := os.Stat(operation.Path)
		if err != nil {
			t.Errorf("stat %q: %v", operation.Path, err)
			continue
		}
		want := directoryPermissions
		if operation.Kind == OperationCreateFile {
			want = filePermissions
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("permissions for %q = %04o, want %04o", operation.Path, got, want)
		}
	}
}

func firstFileOperation(t *testing.T, plan Plan) Operation {
	t.Helper()
	for _, operation := range plan.Operations {
		if operation.Kind == OperationCreateFile {
			return operation
		}
	}
	t.Fatal("plan contains no file operation")
	return Operation{}
}

func resultForPath(t *testing.T, report ExecutionReport, path string) Result {
	t.Helper()
	for _, result := range report.Results {
		if result.Operation.Path == path {
			return result
		}
	}
	t.Fatalf("report contains no result for %q", path)
	return Result{}
}

func readPlannedFiles(t *testing.T, plan Plan) map[string]string {
	t.Helper()
	contents := make(map[string]string)
	for _, operation := range plan.Operations {
		if operation.Kind != OperationCreateFile {
			continue
		}
		data, err := os.ReadFile(operation.Path)
		if err != nil {
			t.Fatalf("read %q: %v", operation.Path, err)
		}
		contents[operation.Path] = string(data)
	}
	return contents
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

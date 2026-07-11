package filesystem

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Arameair/studypilot/internal/workspace"
)

func TestNewPlanIncludesCompleteWorkspace(t *testing.T) {
	paths := testWorkspacePaths(t)
	plan, err := NewPlan(paths)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("Plan.Validate() error = %v", err)
	}

	operations := operationsByPath(plan)
	assertDirectoryOperation(t, operations, paths.Root)
	assertDirectoryOperation(t, operations, paths.Private)
	assertDirectoryOperation(t, operations, paths.Portfolio)
	assertContractOperations(t, operations, paths.Private, workspace.PrivateVaultContract())
	assertContractOperations(t, operations, paths.Portfolio, workspace.PublicPortfolioContract())

	for _, operation := range plan.Operations {
		if !filepath.IsAbs(operation.Path) {
			t.Errorf("operation path %q is not absolute", operation.Path)
		}
		if operation.Path != paths.Root && !strictlyWithin(paths.Root, operation.Path) {
			t.Errorf("operation path %q is outside root %q", operation.Path, paths.Root)
		}
	}
}

func TestNewPlanIsDeterministic(t *testing.T) {
	paths := testWorkspacePaths(t)
	first, err := NewPlan(paths)
	if err != nil {
		t.Fatalf("first NewPlan() error = %v", err)
	}
	second, err := NewPlan(paths)
	if err != nil {
		t.Fatalf("second NewPlan() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("plans differ\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func TestNewPlanOrdering(t *testing.T) {
	paths := testWorkspacePaths(t)
	plan, err := NewPlan(paths)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}

	indexes := operationIndexes(plan)
	if indexes[paths.Root] >= indexes[paths.Private] || indexes[paths.Root] >= indexes[paths.Portfolio] {
		t.Error("workspace root must appear before both vault roots")
	}
	for _, operation := range plan.Operations {
		parent := filepath.Dir(operation.Path)
		if parentIndex, exists := indexes[parent]; exists && parentIndex >= indexes[operation.Path] {
			t.Errorf("parent %q does not appear before child %q", parent, operation.Path)
		}
	}

	privateDirectories, privateFiles := contractPaths(paths.Private, workspace.PrivateVaultContract())
	portfolioDirectories, portfolioFiles := contractPaths(paths.Portfolio, workspace.PublicPortfolioContract())
	assertIncreasingIndexes(t, indexes, append([]string{paths.Root, paths.Private}, append(privateDirectories, privateFiles...)...))
	if indexes[privateFiles[len(privateFiles)-1]] >= indexes[paths.Portfolio] {
		t.Error("all private vault files must appear before the public portfolio root")
	}
	assertIncreasingIndexes(t, indexes, append([]string{paths.Portfolio}, append(portfolioDirectories, portfolioFiles...)...))
}

func TestPlanLines(t *testing.T) {
	paths := testWorkspacePaths(t)
	plan, err := NewPlan(paths)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}

	first := plan.Lines()
	second := plan.Lines()
	if !slices.Equal(first, second) {
		t.Fatal("Lines() output is not stable")
	}
	if len(first) != len(plan.Operations) {
		t.Fatalf("len(Lines()) = %d, want %d", len(first), len(plan.Operations))
	}
	for index, line := range first {
		operation := plan.Operations[index]
		prefix := "CREATE DIRECTORY"
		if operation.Kind == OperationCreateFile {
			prefix = "CREATE FILE"
			if operation.Content != "" && strings.Contains(line, operation.Content) {
				t.Errorf("line %d exposes file content", index)
			}
		}
		if !strings.HasPrefix(line, prefix) {
			t.Errorf("line %d = %q, want prefix %q", index, line, prefix)
		}
		if !strings.HasSuffix(line, operation.Path) {
			t.Errorf("line %d = %q, want path %q", index, line, operation.Path)
		}
	}
}

func TestPlanValidateRejectsMalformedPlans(t *testing.T) {
	paths := testWorkspacePaths(t)
	valid, err := NewPlan(paths)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}

	tests := []struct {
		name string
		plan Plan
	}{
		{name: "empty root", plan: mutatePlan(valid, func(plan *Plan) { plan.Root = "" })},
		{name: "relative root", plan: mutatePlan(valid, func(plan *Plan) { plan.Root = "relative" })},
		{name: "empty operations", plan: Plan{Root: paths.Root}},
		{name: "unknown kind", plan: appendOperation(valid, Operation{Kind: "unknown", Path: filepath.Join(paths.Root, "unknown")})},
		{name: "empty operation path", plan: appendOperation(valid, Operation{Kind: OperationCreateFile})},
		{name: "relative operation path", plan: appendOperation(valid, Operation{Kind: OperationCreateFile, Path: "relative/file"})},
		{name: "duplicate path", plan: appendOperation(valid, valid.Operations[0])},
		{name: "file and directory collision", plan: appendOperation(valid, Operation{Kind: OperationCreateFile, Path: paths.Private})},
		{name: "outside root", plan: appendOperation(valid, Operation{Kind: OperationCreateDirectory, Path: filepath.Join(filepath.Dir(paths.Root), "outside")})},
		{name: "directory with content", plan: appendOperation(valid, Operation{Kind: OperationCreateDirectory, Path: filepath.Join(paths.Root, "extra"), Content: "not allowed"})},
		{name: "workspace root as file", plan: mutateOperation(valid, paths.Root, func(operation *Operation) { operation.Kind = OperationCreateFile })},
		{name: "root missing", plan: withoutOperation(valid, paths.Root)},
		{name: "private root missing", plan: withoutOperation(valid, paths.Private)},
		{name: "portfolio root missing", plan: withoutOperation(valid, paths.Portfolio)},
		{name: "unresolved parent traversal", plan: appendOperation(valid, Operation{Kind: OperationCreateDirectory, Path: paths.Root + string(filepath.Separator) + "folder" + string(filepath.Separator) + ".." + string(filepath.Separator) + "other"})},
		{name: "private nested under portfolio", plan: appendOperation(valid, Operation{Kind: OperationCreateDirectory, Path: filepath.Join(paths.Portfolio, privateVaultName)})},
		{name: "portfolio nested under private", plan: appendOperation(valid, Operation{Kind: OperationCreateDirectory, Path: filepath.Join(paths.Private, portfolioName)})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.plan.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
		})
	}
}

func TestNewPlanRejectsInvalidWorkspacePaths(t *testing.T) {
	tests := []struct {
		name  string
		paths workspace.Paths
	}{
		{name: "empty", paths: workspace.Paths{}},
		{name: "relative", paths: workspace.Paths{Root: "root", Private: "private", Portfolio: "portfolio"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPlan(test.paths); err == nil {
				t.Fatal("NewPlan() error = nil, want error")
			}
		})
	}
}

func testWorkspacePaths(t *testing.T) workspace.Paths {
	t.Helper()
	paths, err := workspace.PathsFromRoot(t.TempDir())
	if err != nil {
		t.Fatalf("PathsFromRoot() error = %v", err)
	}
	return paths
}

func operationsByPath(plan Plan) map[string]Operation {
	operations := make(map[string]Operation, len(plan.Operations))
	for _, operation := range plan.Operations {
		operations[operation.Path] = operation
	}
	return operations
}

func operationIndexes(plan Plan) map[string]int {
	indexes := make(map[string]int, len(plan.Operations))
	for index, operation := range plan.Operations {
		indexes[operation.Path] = index
	}
	return indexes
}

func assertDirectoryOperation(t *testing.T, operations map[string]Operation, path string) {
	t.Helper()
	operation, exists := operations[path]
	if !exists {
		t.Errorf("missing directory operation %q", path)
		return
	}
	if operation.Kind != OperationCreateDirectory || operation.Content != "" {
		t.Errorf("operation %q = %#v, want empty directory operation", path, operation)
	}
}

func assertContractOperations(t *testing.T, operations map[string]Operation, root string, contract workspace.VaultContract) {
	t.Helper()
	for _, directory := range contract.Directories {
		assertDirectoryOperation(t, operations, filepath.Join(root, directory))
	}
	for _, file := range contract.Files {
		path := filepath.Join(root, file.Path)
		operation, exists := operations[path]
		if !exists {
			t.Errorf("missing file operation %q", path)
			continue
		}
		if operation.Kind != OperationCreateFile || operation.Content != file.Content {
			t.Errorf("file operation %q does not match contract", path)
		}
	}
}

func contractPaths(root string, contract workspace.VaultContract) ([]string, []string) {
	directories := make([]string, 0, len(contract.Directories))
	for _, directory := range contract.Directories {
		directories = append(directories, filepath.Join(root, directory))
	}
	slices.Sort(directories)
	files := make([]string, 0, len(contract.Files))
	for _, file := range contract.Files {
		files = append(files, filepath.Join(root, file.Path))
	}
	slices.Sort(files)
	return directories, files
}

func assertIncreasingIndexes(t *testing.T, indexes map[string]int, paths []string) {
	t.Helper()
	for index := 1; index < len(paths); index++ {
		if indexes[paths[index-1]] >= indexes[paths[index]] {
			t.Errorf("%q must appear before %q", paths[index-1], paths[index])
		}
	}
}

func mutatePlan(plan Plan, mutate func(*Plan)) Plan {
	plan.Operations = slices.Clone(plan.Operations)
	mutate(&plan)
	return plan
}

func appendOperation(plan Plan, operation Operation) Plan {
	plan.Operations = append(slices.Clone(plan.Operations), operation)
	return plan
}

func mutateOperation(plan Plan, path string, mutate func(*Operation)) Plan {
	plan.Operations = slices.Clone(plan.Operations)
	for index := range plan.Operations {
		if plan.Operations[index].Path == path {
			mutate(&plan.Operations[index])
			break
		}
	}
	return plan
}

func withoutOperation(plan Plan, path string) Plan {
	plan.Operations = slices.DeleteFunc(slices.Clone(plan.Operations), func(operation Operation) bool {
		return operation.Path == path
	})
	return plan
}

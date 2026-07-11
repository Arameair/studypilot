package filesystem

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	directoryPermissions fs.FileMode = 0o750
	filePermissions      fs.FileMode = 0o640
)

var (
	errDestinationExists = errors.New("destination already exists")
	errUnsafeSymlink     = errors.New("unsafe symlink encountered")
)

// ResultStatus describes the outcome of one planned operation.
type ResultStatus string

const (
	ResultCreated  ResultStatus = "created"
	ResultSkipped  ResultStatus = "skipped"
	ResultConflict ResultStatus = "conflict"
)

// Result records the outcome of inspecting or executing one operation.
type Result struct {
	Operation Operation
	Status    ResultStatus
	Message   string
}

// ExecutionReport contains operation results in plan order.
type ExecutionReport struct {
	Results []Result
}

// HasConflicts reports whether any operation encountered a conflict.
func (r ExecutionReport) HasConflicts() bool { return r.ConflictCount() != 0 }

// CreatedCount returns the number of created paths.
func (r ExecutionReport) CreatedCount() int { return r.count(ResultCreated) }

// SkippedCount returns the number of paths that already matched the plan.
func (r ExecutionReport) SkippedCount() int { return r.count(ResultSkipped) }

// ConflictCount returns the number of paths that conflicted with the plan.
func (r ExecutionReport) ConflictCount() int { return r.count(ResultConflict) }

func (r ExecutionReport) count(status ResultStatus) int {
	count := 0
	for _, result := range r.Results {
		if result.Status == status {
			count++
		}
	}
	return count
}

// Execute validates and executes a plan in operation order. Ordinary path and
// content conflicts are reported without overwriting user data. Invalid plans,
// unsafe symlinks, and filesystem I/O failures return an error.
func Execute(plan Plan) (ExecutionReport, error) {
	if err := plan.Validate(); err != nil {
		return ExecutionReport{}, fmt.Errorf("validate plan: %w", err)
	}

	report := ExecutionReport{Results: make([]Result, 0, len(plan.Operations))}
	for _, operation := range plan.Operations {
		if err := checkPathForSymlinks(operation.Path); err != nil {
			if errors.Is(err, errUnsafeSymlink) {
				report.Results = append(report.Results, conflictResult(operation, err.Error()))
			}
			return report, fmt.Errorf("check operation path %q: %w", operation.Path, err)
		}

		var result Result
		var err error
		switch operation.Kind {
		case OperationCreateDirectory:
			workspaceRoot := plan.Scope == PlanScopeWorkspace && filepath.Clean(operation.Path) == filepath.Clean(plan.Root)
			result, err = executeDirectory(operation, workspaceRoot)
		case OperationCreateFile:
			result, err = executeFile(operation)
		}
		if err != nil {
			return report, fmt.Errorf("execute %s %q: %w", operation.Kind, operation.Path, err)
		}
		report.Results = append(report.Results, result)
	}
	return report, nil
}

func executeDirectory(operation Operation, workspaceRoot bool) (Result, error) {
	if workspaceRoot {
		return executeWorkspaceRoot(operation)
	}

	available, message, err := parentDirectoryAvailable(operation.Path)
	if err != nil {
		return Result{}, err
	}
	if !available {
		return conflictResult(operation, message), nil
	}

	info, err := os.Lstat(operation.Path)
	switch {
	case err == nil:
		if info.IsDir() {
			return skippedResult(operation, "directory already exists"), nil
		}
		return conflictResult(operation, "non-directory exists at required directory path"), nil
	case !errors.Is(err, fs.ErrNotExist):
		return Result{}, err
	}

	if err := os.Mkdir(operation.Path, directoryPermissions); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return inspectDirectoryAfterRace(operation)
		}
		return Result{}, err
	}
	return createdResult(operation, "directory created"), nil
}

func executeWorkspaceRoot(operation Operation) (Result, error) {
	info, err := os.Lstat(operation.Path)
	switch {
	case err == nil:
		if info.IsDir() {
			return skippedResult(operation, "directory already exists"), nil
		}
		return conflictResult(operation, "non-directory exists at required directory path"), nil
	case !errors.Is(err, fs.ErrNotExist):
		return Result{}, err
	}

	if err := os.MkdirAll(operation.Path, directoryPermissions); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return inspectDirectoryAfterRace(operation)
		}
		return Result{}, err
	}
	return createdResult(operation, "workspace root directory created"), nil
}

func inspectDirectoryAfterRace(operation Operation) (Result, error) {
	if err := checkPathForSymlinks(operation.Path); err != nil {
		return Result{}, err
	}
	info, err := os.Lstat(operation.Path)
	if err != nil {
		return Result{}, err
	}
	if info.IsDir() {
		return skippedResult(operation, "directory already exists"), nil
	}
	return conflictResult(operation, "path type changed while creating directory"), nil
}

func executeFile(operation Operation) (Result, error) {
	available, message, err := parentDirectoryAvailable(operation.Path)
	if err != nil {
		return Result{}, err
	}
	if !available {
		return conflictResult(operation, message), nil
	}

	result, exists, err := inspectExistingFile(operation)
	if err != nil || exists {
		return result, err
	}

	err = createFileAtomically(operation.Path, operation.Content)
	if errors.Is(err, errDestinationExists) {
		if err := checkPathForSymlinks(operation.Path); err != nil {
			return Result{}, err
		}
		result, exists, inspectErr := inspectExistingFile(operation)
		if inspectErr == nil && !exists {
			return Result{}, errors.New("destination changed while publishing file")
		}
		return result, inspectErr
	}
	if err != nil {
		return Result{}, err
	}
	return createdResult(operation, "file created atomically"), nil
}

func inspectExistingFile(operation Operation) (Result, bool, error) {
	before, err := os.Lstat(operation.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return Result{}, false, errUnsafeSymlink
	}
	if !before.Mode().IsRegular() {
		return conflictResult(operation, "non-file exists at required file path"), true, nil
	}

	contents, err := os.ReadFile(operation.Path)
	if err != nil {
		return Result{}, false, err
	}
	after, err := os.Lstat(operation.Path)
	if err != nil {
		return Result{}, false, err
	}
	if after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, after) {
		return Result{}, false, errors.New("file path changed while being inspected")
	}
	if string(contents) == operation.Content {
		return skippedResult(operation, "file already contains planned content"), true, nil
	}
	return conflictResult(operation, "existing file content differs"), true, nil
}

func createFileAtomically(path, content string) (returnErr error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".studypilot-tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			if err := temporary.Close(); returnErr == nil && err != nil {
				returnErr = err
			}
		}
		if err := os.Remove(temporaryPath); returnErr == nil && err != nil && !errors.Is(err, fs.ErrNotExist) {
			returnErr = err
		}
	}()

	if err := temporary.Chmod(filePermissions); err != nil {
		return err
	}
	if _, err := io.WriteString(temporary, content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	closed = true

	if err := os.Link(temporaryPath, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return errDestinationExists
		}
		return err
	}
	return nil
}

func parentDirectoryAvailable(path string) (bool, string, error) {
	parent := filepath.Clean(filepath.Dir(path))
	for _, current := range pathComponents(parent) {
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return false, "required parent directory does not exist", nil
		}
		if err != nil {
			return false, "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, "", fmt.Errorf("%w at %q", errUnsafeSymlink, current)
		}
		if !info.IsDir() {
			return false, "required parent path is not a directory", nil
		}
	}
	return true, "", nil
}

func checkPathForSymlinks(path string) error {
	path = filepath.Clean(path)
	for _, current := range pathComponents(path) {
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w at %q", errUnsafeSymlink, current)
		}
		if !info.IsDir() && current != path {
			return nil
		}
	}
	return nil
}

func pathComponents(path string) []string {
	path = filepath.Clean(path)
	volume := filepath.VolumeName(path)
	root := volume + string(filepath.Separator)
	relative := strings.TrimPrefix(path, root)
	components := make([]string, 0, strings.Count(relative, string(filepath.Separator))+1)
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		components = append(components, current)
	}
	return components
}

func createdResult(operation Operation, message string) Result {
	return Result{Operation: operation, Status: ResultCreated, Message: message}
}

func skippedResult(operation Operation, message string) Result {
	return Result{Operation: operation, Status: ResultSkipped, Message: message}
}

func conflictResult(operation Operation, message string) Result {
	return Result{Operation: operation, Status: ResultConflict, Message: message}
}

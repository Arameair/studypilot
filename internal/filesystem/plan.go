package filesystem

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Arameair/studypilot/internal/workspace"
)

const (
	privateVaultName = "Learning-Vault-Private"
	portfolioName    = "IT-Knowledge-Portfolio"
)

// OperationKind identifies a planned filesystem action.
type OperationKind string

const (
	OperationCreateDirectory OperationKind = "create-directory"
	OperationCreateFile      OperationKind = "create-file"
)

// Operation describes a filesystem action without executing it.
type Operation struct {
	Kind    OperationKind
	Path    string
	Content string
}

// PlanScope identifies the safety invariants required for a plan. The zero
// value is the full workspace initialization scope for backward compatibility.
type PlanScope string

const (
	PlanScopeWorkspace PlanScope = ""
	PlanScopeCourse    PlanScope = "course"
	PlanScopeModule    PlanScope = "module"
)

// Plan is an ordered collection of filesystem operations for one workspace.
type Plan struct {
	Root       string
	Scope      PlanScope
	Operations []Operation
	authority  *planAuthority
}

type planAuthority struct {
	paths       workspace.Paths
	allowedRoot string
	courseRoot  string
	scope       PlanScope
}

// NewPlan constructs a deterministic workspace plan without accessing the
// filesystem.
func NewPlan(paths workspace.Paths) (Plan, error) {
	if err := paths.Validate(); err != nil {
		return Plan{}, fmt.Errorf("validate workspace paths: %w", err)
	}

	privateContract := workspace.PrivateVaultContract()
	if err := privateContract.Validate(); err != nil {
		return Plan{}, fmt.Errorf("validate private vault contract: %w", err)
	}
	portfolioContract := workspace.PublicPortfolioContract()
	if err := portfolioContract.Validate(); err != nil {
		return Plan{}, fmt.Errorf("validate public portfolio contract: %w", err)
	}

	operations := []Operation{
		directoryOperation(paths.Root),
		directoryOperation(paths.Private),
	}
	operations = append(operations, contractOperations(paths.Private, privateContract)...)
	operations = append(operations, directoryOperation(paths.Portfolio))
	operations = append(operations, contractOperations(paths.Portfolio, portfolioContract)...)

	plan := Plan{
		Root: paths.Root, Scope: PlanScopeWorkspace, Operations: operations,
		authority: &planAuthority{paths: paths, allowedRoot: paths.Root, scope: PlanScopeWorkspace},
	}
	if err := plan.Validate(); err != nil {
		return Plan{}, fmt.Errorf("validate generated plan: %w", err)
	}
	return plan, nil
}

// NewCourseScopedPlan authorizes operations for one course beneath the
// validated private course directory.
func NewCourseScopedPlan(paths workspace.Paths, courseRoot string, operations []Operation) (Plan, error) {
	return newScopedPlan(paths, PlanScopeCourse, "", courseRoot, operations)
}

// NewModuleScopedPlan authorizes operations for one module within a verified
// course root. The course package is responsible for verifying course metadata.
func NewModuleScopedPlan(paths workspace.Paths, courseRoot, moduleRoot string, operations []Operation) (Plan, error) {
	return newScopedPlan(paths, PlanScopeModule, courseRoot, moduleRoot, operations)
}

func newScopedPlan(paths workspace.Paths, scope PlanScope, courseRoot, allowedRoot string, operations []Operation) (Plan, error) {
	if err := paths.Validate(); err != nil {
		return Plan{}, fmt.Errorf("validate workspace paths: %w", err)
	}
	coursesRoot := filepath.Join(paths.Private, "01 Courses")
	if !strictlyWithin(coursesRoot, allowedRoot) {
		return Plan{}, errors.New("scoped plan root must be inside the private course directory")
	}
	if allowedRoot == paths.Portfolio || strictlyWithin(paths.Portfolio, allowedRoot) {
		return Plan{}, errors.New("scoped plan root must not enter the public portfolio")
	}
	if scope == PlanScopeModule {
		if !strictlyWithin(coursesRoot, courseRoot) {
			return Plan{}, errors.New("module course root must be inside the private course directory")
		}
		if !strictlyWithin(filepath.Join(courseRoot, "Modules"), allowedRoot) {
			return Plan{}, errors.New("module root must be inside the verified course Modules directory")
		}
	}
	plan := Plan{
		Root: filepath.Clean(allowedRoot), Scope: scope, Operations: slices.Clone(operations),
		authority: &planAuthority{paths: paths, allowedRoot: filepath.Clean(allowedRoot), courseRoot: filepath.Clean(courseRoot), scope: scope},
	}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// Validate checks the plan lexically without accessing the filesystem.
func (p Plan) Validate() error {
	if p.Scope != PlanScopeWorkspace && p.Scope != PlanScopeCourse && p.Scope != PlanScopeModule {
		return fmt.Errorf("unknown plan scope %q", p.Scope)
	}
	if strings.TrimSpace(p.Root) == "" {
		return errors.New("plan root must not be empty")
	}
	if !filepath.IsAbs(p.Root) {
		return errors.New("plan root must be absolute")
	}
	if containsParentTraversal(p.Root) {
		return errors.New("plan root contains unresolved parent traversal")
	}
	if len(p.Operations) == 0 {
		return errors.New("plan must contain operations")
	}
	if p.authority == nil {
		return errors.New("plan has no trusted authority")
	}
	if err := p.authority.paths.Validate(); err != nil {
		return fmt.Errorf("validate plan authority: %w", err)
	}
	if p.Scope != p.authority.scope || filepath.Clean(p.Root) != p.authority.allowedRoot {
		return errors.New("plan scope or root does not match trusted authority")
	}

	root := filepath.Clean(p.Root)
	privateRoot := p.authority.paths.Private
	portfolioRoot := p.authority.paths.Portfolio
	if p.Scope == PlanScopeWorkspace && root != filepath.Clean(p.authority.paths.Root) {
		return errors.New("workspace plan root does not match workspace authority")
	}
	if p.Scope != PlanScopeWorkspace {
		coursesRoot := filepath.Join(privateRoot, "01 Courses")
		if !strictlyWithin(coursesRoot, root) {
			return errors.New("scoped plan root is outside the private course directory")
		}
	}
	seen := make(map[string]OperationKind, len(p.Operations))
	rootFound := false
	privateRootFound := false
	portfolioRootFound := false

	for index, operation := range p.Operations {
		if operation.Kind != OperationCreateDirectory && operation.Kind != OperationCreateFile {
			return fmt.Errorf("operation %d has unknown kind %q", index, operation.Kind)
		}
		if strings.TrimSpace(operation.Path) == "" {
			return fmt.Errorf("operation %d path must not be empty", index)
		}
		if !filepath.IsAbs(operation.Path) {
			return fmt.Errorf("operation %d path must be absolute", index)
		}
		if containsParentTraversal(operation.Path) {
			return fmt.Errorf("operation %d path contains unresolved parent traversal", index)
		}
		if operation.Kind == OperationCreateDirectory && operation.Content != "" {
			return fmt.Errorf("directory operation %q must not contain content", operation.Path)
		}

		path := filepath.Clean(operation.Path)
		if path != root && !strictlyWithin(root, path) {
			return fmt.Errorf("operation path %q is outside plan root", operation.Path)
		}
		if previousKind, exists := seen[path]; exists {
			if previousKind != operation.Kind {
				return fmt.Errorf("path %q is represented as both a file and directory", operation.Path)
			}
			return fmt.Errorf("duplicate operation path %q", operation.Path)
		}
		seen[path] = operation.Kind
		if p.Scope != PlanScopeWorkspace && (path == portfolioRoot || strictlyWithin(portfolioRoot, path)) {
			return fmt.Errorf("operation path %q enters public portfolio", operation.Path)
		}

		if path == root {
			if operation.Kind != OperationCreateDirectory {
				return errors.New("plan root must be a directory operation")
			}
			rootFound = true
		}

		if p.Scope == PlanScopeWorkspace {
			switch path {
			case privateRoot:
				if operation.Kind != OperationCreateDirectory {
					return errors.New("private vault root must be a directory operation")
				}
				privateRootFound = true
			case portfolioRoot:
				if operation.Kind != OperationCreateDirectory {
					return errors.New("portfolio root must be a directory operation")
				}
				portfolioRootFound = true
			}

			if containsRelativeComponent(portfolioRoot, path, privateVaultName) {
				return fmt.Errorf("private vault path %q is nested under portfolio", operation.Path)
			}
			if containsRelativeComponent(privateRoot, path, portfolioName) {
				return fmt.Errorf("portfolio path %q is nested under private vault", operation.Path)
			}
		}
	}

	if !rootFound {
		return errors.New("plan is missing root directory operation")
	}
	if p.Scope == PlanScopeWorkspace {
		if !privateRootFound {
			return errors.New("plan is missing private vault root directory operation")
		}
		if !portfolioRootFound {
			return errors.New("plan is missing portfolio root directory operation")
		}
	}
	return nil
}

// Lines returns stable human-readable descriptions without file contents.
func (p Plan) Lines() []string {
	lines := make([]string, 0, len(p.Operations))
	for _, operation := range p.Operations {
		label := "CREATE DIRECTORY"
		if operation.Kind == OperationCreateFile {
			label = "CREATE FILE"
		}
		lines = append(lines, fmt.Sprintf("%-18s%s", label, operation.Path))
	}
	return lines
}

func contractOperations(root string, contract workspace.VaultContract) []Operation {
	directories := slices.Clone(contract.Directories)
	slices.Sort(directories)
	files := slices.Clone(contract.Files)
	slices.SortFunc(files, func(a, b workspace.RequiredFile) int {
		return strings.Compare(a.Path, b.Path)
	})

	operations := make([]Operation, 0, len(directories)+len(files))
	for _, directory := range directories {
		operations = append(operations, directoryOperation(filepath.Join(root, directory)))
	}
	for _, file := range files {
		operations = append(operations, Operation{
			Kind:    OperationCreateFile,
			Path:    filepath.Join(root, file.Path),
			Content: file.Content,
		})
	}
	return operations
}

func directoryOperation(path string) Operation {
	return Operation{Kind: OperationCreateDirectory, Path: path}
}

func containsParentTraversal(path string) bool {
	for _, component := range strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if component == ".." {
			return true
		}
	}
	return false
}

func strictlyWithin(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func containsRelativeComponent(parent, child, component string) bool {
	if !strictlyWithin(parent, child) {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil {
		return false
	}
	for _, candidate := range strings.FieldsFunc(relative, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if candidate == component {
			return true
		}
	}
	return false
}

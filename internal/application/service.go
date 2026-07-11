// Package application provides UI-neutral use cases for StudyPilot. It is the
// single orchestration path shared by every interface (CLI today; tray or GUI
// later): it resolves workspace paths, builds deterministic filesystem plans
// through the domain packages, executes them safely, and returns typed results
// and errors. It never prints, never reads command-line flags, and never calls
// os.Exit; those concerns belong to the calling adapter.
//
// A Service holds only immutable dependencies, so independent calls on the same
// Service are safe for concurrent use as long as the injected Now and
// GenerateID functions are themselves safe for concurrent use (the production
// defaults are).
package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/filesystem"
	"github.com/Arameair/studypilot/internal/workspace"
)

// Dependencies are the injectable collaborators a Service needs. Both fields are
// required; production wiring supplies time.Now and course.DefaultIDGenerator,
// while tests supply fixed clocks and deterministic or failing ID generators.
type Dependencies struct {
	Now        func() time.Time
	GenerateID course.IDGenerator
}

// Service exposes StudyPilot's shared application use cases.
type Service struct {
	now        func() time.Time
	generateID course.IDGenerator
}

// NewService constructs a Service, rejecting missing required dependencies.
func NewService(deps Dependencies) (*Service, error) {
	if deps.Now == nil {
		return nil, errors.New("application: Now dependency is required")
	}
	if deps.GenerateID == nil {
		return nil, errors.New("application: GenerateID dependency is required")
	}
	return &Service{now: deps.Now, generateID: deps.GenerateID}, nil
}

// NewDefaultService constructs a Service with production defaults: the wall
// clock and StudyPilot's secure course/module ID generator.
func NewDefaultService() *Service {
	return &Service{now: time.Now, generateID: course.DefaultIDGenerator}
}

func resolvePaths(root string) (workspace.Paths, error) {
	if strings.TrimSpace(root) == "" {
		return workspace.DefaultPaths()
	}
	return workspace.PathsFromRoot(root)
}

// checkContext returns a classified cancellation error if ctx is already done.
// The context is never retained on the Service.
func checkContext(ctx context.Context, op string) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return newError(op, "operation cancelled", err)
	}
	return nil
}

// execute runs a validated plan and converts the report into a UI-neutral
// result. Content conflicts are reported within the result (no error); unsafe
// symlinks and I/O failures return the partial result plus a classified error.
func (s *Service) execute(ctx context.Context, op string, plan filesystem.Plan) (ExecutionResult, error) {
	if err := checkContext(ctx, op); err != nil {
		return ExecutionResult{}, err
	}
	report, err := filesystem.Execute(plan)
	result := executionResult(report)
	if err != nil {
		return result, newError(op, "execute filesystem plan", err)
	}
	return result, nil
}

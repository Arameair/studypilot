package application

import (
	"context"

	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/filesystem"
)

const opModule = "CreateModule"

// PlanModuleCreation returns the deterministic plan for creating (or safely
// re-affirming) a module within an existing course, without writing to disk.
func (s *Service) PlanModuleCreation(ctx context.Context, req ModuleCreateRequest) (PlanResult, error) {
	plan, err := s.buildModulePlan(ctx, req)
	if err != nil {
		return PlanResult{}, err
	}
	return planResult(plan), nil
}

// CreateModule builds and safely executes the module creation plan. Module
// numbers remain unique within a course and repeated requests preserve the
// module's immutable identity and timestamps.
func (s *Service) CreateModule(ctx context.Context, req ModuleCreateRequest) (ExecutionResult, error) {
	plan, err := s.buildModulePlan(ctx, req)
	if err != nil {
		return ExecutionResult{}, err
	}
	return s.execute(ctx, opModule, plan)
}

func (s *Service) buildModulePlan(ctx context.Context, req ModuleCreateRequest) (filesystem.Plan, error) {
	if err := checkContext(ctx, opModule); err != nil {
		return filesystem.Plan{}, err
	}
	paths, err := resolvePaths(req.Root)
	if err != nil {
		return filesystem.Plan{}, newError(opModule, "resolve workspace paths", err)
	}
	plan, err := course.NewModulePlanWithID(paths, req.CourseRef, req.Number, req.Name, s.now(), s.generateID)
	if err != nil {
		return filesystem.Plan{}, newError(opModule, "construct module plan", err)
	}
	return plan, nil
}

package application

import (
	"context"

	"github.com/Arameair/studypilot/internal/filesystem"
)

const opWorkspace = "InitializeWorkspace"

// PlanWorkspaceInitialization returns the deterministic initialization plan for
// the requested workspace without writing to the filesystem.
func (s *Service) PlanWorkspaceInitialization(ctx context.Context, req WorkspaceRequest) (PlanResult, error) {
	plan, err := s.buildWorkspacePlan(ctx, req)
	if err != nil {
		return PlanResult{}, err
	}
	return planResult(plan), nil
}

// InitializeWorkspace builds and safely executes the workspace initialization
// plan, creating the private vault and public portfolio skeletons.
func (s *Service) InitializeWorkspace(ctx context.Context, req WorkspaceRequest) (ExecutionResult, error) {
	plan, err := s.buildWorkspacePlan(ctx, req)
	if err != nil {
		return ExecutionResult{}, err
	}
	return s.execute(ctx, opWorkspace, plan)
}

func (s *Service) buildWorkspacePlan(ctx context.Context, req WorkspaceRequest) (filesystem.Plan, error) {
	if err := checkContext(ctx, opWorkspace); err != nil {
		return filesystem.Plan{}, err
	}
	paths, err := resolvePaths(req.Root)
	if err != nil {
		return filesystem.Plan{}, newError(opWorkspace, "resolve workspace paths", err)
	}
	plan, err := filesystem.NewPlan(paths)
	if err != nil {
		return filesystem.Plan{}, newError(opWorkspace, "construct filesystem plan", err)
	}
	return plan, nil
}

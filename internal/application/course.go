package application

import (
	"context"

	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/filesystem"
)

const opCourse = "CreateCourse"

// PlanCourseCreation returns the deterministic plan for creating (or safely
// re-affirming) a private course, without writing to the filesystem.
func (s *Service) PlanCourseCreation(ctx context.Context, req CourseCreateRequest) (PlanResult, error) {
	plan, err := s.buildCoursePlan(ctx, req)
	if err != nil {
		return PlanResult{}, err
	}
	return planResult(plan), nil
}

// CreateCourse builds and safely executes the course creation plan. Repeating
// the request preserves the course's immutable identity and timestamps.
func (s *Service) CreateCourse(ctx context.Context, req CourseCreateRequest) (ExecutionResult, error) {
	plan, err := s.buildCoursePlan(ctx, req)
	if err != nil {
		return ExecutionResult{}, err
	}
	return s.execute(ctx, opCourse, plan)
}

func (s *Service) buildCoursePlan(ctx context.Context, req CourseCreateRequest) (filesystem.Plan, error) {
	if err := checkContext(ctx, opCourse); err != nil {
		return filesystem.Plan{}, err
	}
	paths, err := resolvePaths(req.Root)
	if err != nil {
		return filesystem.Plan{}, newError(opCourse, "resolve workspace paths", err)
	}
	plan, err := course.NewCoursePlanWithID(paths, req.Name, s.now(), s.generateID)
	if err != nil {
		return filesystem.Plan{}, newError(opCourse, "construct course plan", err)
	}
	return plan, nil
}

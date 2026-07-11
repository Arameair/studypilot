package application

// WorkspaceRequest identifies the StudyPilot workspace to plan or initialize.
// An empty Root selects the default workspace location.
type WorkspaceRequest struct {
	Root string
}

// CourseCreateRequest describes a private course to plan or create. An empty
// Root selects the default workspace location.
type CourseCreateRequest struct {
	Root string
	Name string
}

// ModuleCreateRequest describes a module to plan or create within an existing
// course. CourseRef identifies the parent course by immutable ID, display name,
// or slug. An empty Root selects the default workspace location.
type ModuleCreateRequest struct {
	Root      string
	CourseRef string
	Number    int
	Name      string
}

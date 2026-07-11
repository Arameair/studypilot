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

type CreateSessionRequest struct {
	Root, CourseRef, ModuleRef, Title string
	IdempotencyKey                    string
}

type SessionReferenceRequest struct{ Root, CourseRef, ModuleRef, SessionRef string }
type UpdateSessionRequest struct {
	Root, CourseRef, ModuleRef, SessionRef string
	ExpectedRevision                       uint64
}
type InterruptSessionRequest struct {
	UpdateSessionRequest
	Reason string
}
type CompleteSessionRequest struct{ UpdateSessionRequest }
type AbandonSessionRequest struct {
	UpdateSessionRequest
	Reason string
}
type ListIncompleteSessionsRequest struct{ Root, CourseRef, ModuleRef string }

// InspectModuleRequest identifies a single module for tolerant, read-only
// inspection of all its sessions. Both course and module references are
// required.
type InspectModuleRequest struct{ Root, CourseRef, ModuleRef string }

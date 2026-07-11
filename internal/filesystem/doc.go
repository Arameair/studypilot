// Package filesystem converts StudyPilot workspace contracts into validated,
// deterministic plans and safely executes explicitly supplied plans.
//
// Execution creates user-owned directories and files atomically, refuses
// overwrites, and rejects symlink traversal. Git and other external side effects
// are outside this package.
package filesystem

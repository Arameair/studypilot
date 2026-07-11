package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/course"
	"github.com/Arameair/studypilot/internal/workspace"
)

var fixedDate = time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)

const courseMetadataFile = ".studypilot-course.json"

func fixedClock(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

// prefixedID returns a deterministic generator that honours the entity kind, so
// course IDs start with "course-" and module IDs with "module-", as the domain
// metadata validation requires.
func prefixedID(suffix string) course.IDGenerator {
	return func(kind string) (string, error) { return kind + "-" + suffix, nil }
}

// sequentialID returns kind-aware unique IDs (course-001, course-002, ...) for
// tests that create several distinct entities in one workspace. It is not safe
// for concurrent use, which is fine for sequential test creation.
func sequentialID() course.IDGenerator {
	var n int
	return func(kind string) (string, error) {
		n++
		return fmt.Sprintf("%s-%03d", kind, n), nil
	}
}

// failingID is an ID generator that must never be invoked; reusing an existing
// entity must not mint a new ID.
func failingID(string) (string, error) {
	return "", errors.New("ID generator must not be called")
}

func newTestService(t *testing.T, clock func() time.Time, generate course.IDGenerator) *Service {
	t.Helper()
	service, err := NewService(Dependencies{Now: clock, GenerateID: generate})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func testRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "StudyPilot")
}

func initWorkspace(t *testing.T, service *Service, root string) {
	t.Helper()
	result, err := service.InitializeWorkspace(context.Background(), WorkspaceRequest{Root: root})
	if err != nil {
		t.Fatalf("InitializeWorkspace() error = %v", err)
	}
	if result.Conflicts != 0 {
		t.Fatalf("InitializeWorkspace() conflicts = %d", result.Conflicts)
	}
}

func coursesDir(t *testing.T, root string) string {
	t.Helper()
	paths, err := workspace.PathsFromRoot(root)
	if err != nil {
		t.Fatalf("PathsFromRoot() error = %v", err)
	}
	return filepath.Join(paths.Private, "01 Courses")
}

// writeManagedCourse writes a valid course metadata directory out of band. It is
// used to construct on-disk states (such as duplicate IDs) that the create path
// deliberately refuses to produce.
func writeManagedCourse(t *testing.T, base, dirName, id, display, slug string) {
	t.Helper()
	metadata := course.CourseMetadata{
		SchemaVersion: 1, ID: id, DisplayName: display, Slug: slug,
		DirectoryName: dirName, CreatedAt: fixedDate, UpdatedAt: fixedDate,
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal course metadata: %v", err)
	}
	dir := filepath.Join(base, dirName)
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatalf("create managed course dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, courseMetadataFile), data, 0o640); err != nil {
		t.Fatalf("write course metadata: %v", err)
	}
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("path %q exists or returned unexpected error: %v", path, err)
	}
}

func assertDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("path %q is not a directory", path)
	}
}

func planCoversPath(result PlanResult, path string) bool {
	for _, operation := range result.Operations {
		if operation.Path == path {
			return true
		}
	}
	return false
}

func writeFileTree(t *testing.T, path, content string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o640)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(data)
}

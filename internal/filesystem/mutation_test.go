package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Arameair/studypilot/internal/workspace"
)

type mutationFixture struct {
	paths                   workspace.Paths
	course, module, session string
}

func newMutationFixture(t *testing.T) mutationFixture {
	t.Helper()
	paths, err := workspace.PathsFromRoot(filepath.Join(t.TempDir(), "StudyPilot"))
	if err != nil {
		t.Fatal(err)
	}
	course := filepath.Join(paths.Private, "01 Courses", "synthetic-course")
	module := filepath.Join(course, "Modules", "module-01")
	session := filepath.Join(module, "Sessions", "session-01")
	for _, dir := range []string{paths.Root, paths.Private, paths.Portfolio, course, module, session} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(course, courseMetadataFileName):      `{"schema_version":1,"id":"course-test","directory_name":"synthetic-course"}`,
		filepath.Join(module, moduleMetadataFileName):      `{"schema_version":1,"id":"module-test","course_id":"course-test","directory_name":"module-01"}`,
		filepath.Join(session, sessionMetadataFileName):    `{"schema_version":1,"id":"session-test","course_id":"course-test","module_id":"module-test","directory_name":"session-01"}`,
		filepath.Join(paths.Root, runtimeMetadataFileName): `{"kind":"runtime"}`,
	} {
		if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	return mutationFixture{paths: paths, course: course, module: module, session: session}
}

func TestMutationAuthorities(t *testing.T) {
	fixture := newMutationFixture(t)
	tests := []struct {
		name string
		new  func() error
		want error
	}{
		{"workspace", func() error { _, err := NewWorkspaceMutationAuthority(fixture.paths); return err }, nil},
		{"course", func() error { _, err := NewCourseMutationAuthority(fixture.paths, fixture.course); return err }, nil},
		{"module", func() error {
			_, err := NewModuleMutationAuthority(fixture.paths, fixture.course, fixture.module)
			return err
		}, nil},
		{"session", func() error {
			_, err := NewSessionMutationAuthority(fixture.paths, fixture.module, fixture.session)
			return err
		}, nil},
		{"portfolio", func() error { _, err := NewCourseMutationAuthority(fixture.paths, fixture.paths.Portfolio); return err }, ErrUnauthorized},
		{"arbitrary root", func() error {
			_, err := NewCourseMutationAuthority(fixture.paths, filepath.Join(fixture.paths.Root, "other"))
			return err
		}, ErrUnauthorized},
		{"traversal", func() error {
			_, err := NewCourseMutationAuthority(fixture.paths, fixture.course+string(os.PathSeparator)+"..")
			return err
		}, ErrUnauthorized},
		{"sibling course as module parent", func() error {
			sibling := filepath.Join(fixture.paths.Private, "01 Courses", "sibling")
			_ = os.MkdirAll(sibling, 0o750)
			_ = os.WriteFile(filepath.Join(sibling, courseMetadataFileName), []byte("{}"), 0o640)
			_, err := NewModuleMutationAuthority(fixture.paths, sibling, fixture.module)
			return err
		}, ErrUnauthorized},
		{"sibling module", func() error {
			sibling := filepath.Join(fixture.course, "Modules", "sibling")
			_ = os.MkdirAll(sibling, 0o750)
			_ = os.WriteFile(filepath.Join(sibling, moduleMetadataFileName), []byte("{}"), 0o640)
			_, err := NewSessionMutationAuthority(fixture.paths, sibling, fixture.session)
			return err
		}, ErrUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.new()
			if test.want == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
}

func TestMutationRejectsForgedMismatchedAndUnmanagedTargets(t *testing.T) {
	fixture := newMutationFixture(t)
	executor := NewMutationExecutor()
	if _, err := executor.Read(context.Background(), MutationAuthority{}, filepath.Join(fixture.course, courseMetadataFileName)); !errors.Is(err, ErrInvalidMutation) {
		t.Fatalf("zero authority: %v", err)
	}
	authority, err := NewCourseMutationAuthority(fixture.paths, fixture.course)
	if err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"markdown":       filepath.Join(fixture.course, "Course Overview.md"),
		"arbitrary json": filepath.Join(fixture.course, "other.json"),
		"module target":  filepath.Join(fixture.module, moduleMetadataFileName),
		"portfolio":      filepath.Join(fixture.paths.Portfolio, courseMetadataFileName),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := executor.Read(context.Background(), authority, target)
			if !errors.Is(err, ErrUnmanagedTarget) && !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestReadManagedFileSafetyAndSnapshot(t *testing.T) {
	fixture := newMutationFixture(t)
	authority, err := NewCourseMutationAuthority(fixture.paths, fixture.course)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(fixture.course, courseMetadataFileName)
	executor := NewMutationExecutor()
	managed, err := executor.Read(context.Background(), authority, target)
	if err != nil {
		t.Fatal(err)
	}
	if managed.SHA256 != hashString(hashState(managed.Content).ContentHash) || managed.Mode != 0o640 || managed.Size != int64(len(managed.Content)) {
		t.Fatalf("unexpected snapshot: %+v", managed)
	}
	original := append([]byte(nil), managed.Content...)
	managed.Content[0] ^= 0xff
	again, err := executor.Read(context.Background(), authority, target)
	if err != nil || string(again.Content) != string(original) {
		t.Fatalf("returned content was not defensive: %q, %v", again.Content, err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := executor.Inspect(cancelled, authority, target); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inspect: %v", err)
	}
}

func TestReadManagedFileRejectsUnsafeAndInvalidTargets(t *testing.T) {
	fixture := newMutationFixture(t)
	executor := NewMutationExecutor()
	authority, _ := NewCourseMutationAuthority(fixture.paths, fixture.course)
	target := filepath.Join(fixture.course, courseMetadataFileName)

	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Read(context.Background(), authority, target); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Read(context.Background(), authority, target); !errors.Is(err, ErrTargetNotRegular) {
		t.Fatalf("directory: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(fixture.course, "real")
	if err := os.WriteFile(real, []byte("real"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, target); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Read(context.Background(), authority, target); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("symlink: %v", err)
	}

	if runtime.GOOS == "linux" {
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(real, target); err != nil {
			t.Fatal(err)
		}
		if _, err := executor.Read(context.Background(), authority, target); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("hard link: %v", err)
		}
	}
}

func TestAuthorityRejectsSymlinkedParent(t *testing.T) {
	fixture := newMutationFixture(t)
	real := filepath.Join(fixture.paths.Private, "real-course")
	if err := os.MkdirAll(real, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, courseMetadataFileName), []byte("{}"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(fixture.paths.Private, "01 Courses", "linked-course")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCourseMutationAuthority(fixture.paths, link); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("got %v", err)
	}
}

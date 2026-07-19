package application

import (
	"context"
	"sync"
	"testing"

	"github.com/Arameair/studypilot/internal/course"
)

func TestSessionNotesApplicationPersistenceAndConcurrentRevision(t *testing.T) {
	service, err := NewService(Dependencies{Now: fixedClock(fixedDate), GenerateID: course.DefaultIDGenerator})
	if err != nil {
		t.Fatal(err)
	}
	root := testRoot(t)
	initWorkspace(t, service, root)
	if _, err = service.CreateCourse(context.Background(), CourseCreateRequest{Root: root, Name: "Notes Course"}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateModule(context.Background(), ModuleCreateRequest{Root: root, CourseRef: "Notes Course", Number: 1, Name: "Notes Module"}); err != nil {
		t.Fatal(err)
	}
	sessionValue, err := service.CreateSession(context.Background(), CreateSessionRequest{Root: root, CourseRef: "Notes Course", ModuleRef: "Notes Module", Title: "Notes Session"})
	if err != nil {
		t.Fatal(err)
	}
	base := StudyArtifactModuleRequest{Root: root, CourseRef: "Notes Course", ModuleRef: "Notes Module"}
	created, err := service.CreateSessionNotes(context.Background(), CreateSessionNotesRequest{StudyArtifactModuleRequest: base, SessionRef: sessionValue.ID, Title: "Session Notes", ExpectedArtifactRevision: 0})
	if err != nil || created.Revision != 1 {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	const first = "# Draft\n\nUnicode café 🚀\n<script>inert</script>\n"
	saved, err := service.UpdateSessionNotes(context.Background(), UpdateSessionNotesRequest{StudyArtifactModuleRequest: base, SessionRef: sessionValue.ID, Content: first, ExpectedArtifactRevision: 1})
	if err != nil || saved.Revision != 2 || saved.Content != first {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	restarted, err := NewService(Dependencies{Now: fixedClock(fixedDate), GenerateID: course.DefaultIDGenerator})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := restarted.ReadSessionNotes(context.Background(), ReadSessionNotesRequest{StudyArtifactModuleRequest: base, SessionRef: sessionValue.ID})
	if err != nil || loaded.Content != first || loaded.Revision != 2 || loaded.Artifact.ID != saved.Artifact.ID {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}

	var gate sync.WaitGroup
	gate.Add(1)
	results := make(chan error, 2)
	for _, content := range []string{"winner one", "winner two"} {
		go func(value string) {
			gate.Wait()
			_, updateErr := service.UpdateSessionNotes(context.Background(), UpdateSessionNotesRequest{StudyArtifactModuleRequest: base, SessionRef: sessionValue.ID, Content: value, ExpectedArtifactRevision: 2})
			results <- updateErr
		}(content)
	}
	gate.Done()
	success, conflict := 0, 0
	for range 2 {
		if updateErr := <-results; updateErr == nil {
			success++
		} else if Classify(updateErr) == ErrorConflict {
			conflict++
		} else {
			t.Fatalf("unexpected concurrent error: %v", updateErr)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

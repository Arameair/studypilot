//go:build windows

package studyartifact

import (
	"context"
	"fmt"
	"testing"
)

func TestWindowsSessionNoteAndArtifactIndexReplaceRepeatedly(t *testing.T) {
	fixture := newArtifactFixture(t)
	ctx := context.Background()
	created, index, err := fixture.store.CreateSessionNotes(ctx, "session-1", "Notes", 0)
	if err != nil {
		t.Fatal(err)
	}
	for update := 1; update <= 25; update++ {
		content := fmt.Sprintf("# Notes\n\nWindows replacement %d\n", update)
		record, next, updateErr := fixture.store.UpdateSessionNotes(ctx, "session-1", content, index.Revision)
		if updateErr != nil {
			t.Fatalf("update %d: %v", update, updateErr)
		}
		if record.ID != created.ID || next.Revision != index.Revision+1 {
			t.Fatalf("update %d record=%+v revision=%d", update, record, next.Revision)
		}
		loaded, loadErr := fixture.store.LoadSessionNotes(ctx, "session-1")
		if loadErr != nil || loaded.Content != content || loaded.Revision != next.Revision {
			t.Fatalf("update %d loaded=%+v err=%v", update, loaded, loadErr)
		}
		index = next
	}
}

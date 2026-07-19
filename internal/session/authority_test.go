package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Arameair/studypilot/internal/filesystem"
)

func TestSessionAuthorityBoundaries(t *testing.T) {
	fixture := newRepositoryFixture(t)
	record, err := fixture.repository.Create(context.Background(), fixture.courseID, fixture.moduleID, "Session", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := filesystem.NewSessionMutationAuthority(fixture.paths, fixture.moduleRoot, record.Root); err != nil {
		t.Fatalf("valid: %v", err)
	}
	tests := []struct{ name, module, root string }{
		{"public", fixture.paths.Portfolio, filepath.Join(fixture.paths.Portfolio, "session")},
		{"arbitrary", filepath.Join(fixture.paths.Private, "other"), filepath.Join(fixture.paths.Private, "other", "Sessions", "session")},
		{"traversal", fixture.moduleRoot, record.Root + string(os.PathSeparator) + ".."},
		{"sibling module", filepath.Join(filepath.Dir(fixture.moduleRoot), "sibling"), record.Root},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := filesystem.NewSessionMutationAuthority(fixture.paths, test.module, test.root); err == nil {
				t.Fatal("authority unexpectedly accepted")
			}
		})
	}
	zero := filesystem.MutationAuthority{}
	if _, err := filesystem.NewMutationExecutor().Read(context.Background(), zero, filepath.Join(record.Root, runtimeStateName)); !errors.Is(err, filesystem.ErrInvalidMutation) {
		t.Fatalf("forged: %v", err)
	}
}

func TestSessionAuthorityRejectsSymlinkParent(t *testing.T) {
	fixture := newRepositoryFixture(t)
	real := filepath.Join(fixture.moduleRoot, "real-sessions")
	if err := os.Mkdir(real, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(fixture.moduleRoot, "Sessions", "linked")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := filesystem.NewSessionMutationAuthority(fixture.paths, fixture.moduleRoot, link); !errors.Is(err, filesystem.ErrUnsafePath) && !errors.Is(err, filesystem.ErrTargetNotFound) {
		t.Fatalf("got %v", err)
	}
}

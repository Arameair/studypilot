package backend

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func ownershipAuthority(t *testing.T) (SegmentAuthority, string) {
	t.Helper()
	paths, sessionRoot := newSession(t)
	authority, err := NewSegmentAuthority(paths, sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.EnsureSegmentsDir(); err != nil {
		t.Fatal(err)
	}
	return authority, authority.SegmentsDir()
}

func TestOwnershipExclusiveCreation(t *testing.T) {
	authority, segments := ownershipAuthority(t)
	owner := currentOwnership("capture-test-1", "segment-test-1", 1, time.Now())
	if err := createOwnership(authority, owner); err != nil {
		t.Fatal(err)
	}
	// A second creation conflicts and never overwrites the first.
	if err := createOwnership(authority, owner); CodeOf(err) != ErrorOwnershipConflict {
		t.Fatalf("second create = %v", err)
	}
	loaded, present, err := readOwnership(segments)
	if err != nil || !present || loaded.CaptureID != "capture-test-1" || loaded.Number != 1 {
		t.Fatalf("loaded = %+v present=%v err=%v", loaded, present, err)
	}
	if loaded.ProcessID == 0 || loaded.StartedAt.IsZero() {
		t.Fatalf("ownership missing process metadata: %+v", loaded)
	}
	if err := removeOwnership(segments); err != nil {
		t.Fatal(err)
	}
	if _, present, _ := readOwnership(segments); present {
		t.Fatal("ownership not removed")
	}
}

func TestOwnershipContainsNoSensitiveData(t *testing.T) {
	authority, segments := ownershipAuthority(t)
	if err := createOwnership(authority, currentOwnership("capture-test-1", "segment-test-1", 1, time.Now())); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(segments, ownershipFileName))
	if err != nil {
		t.Fatal(err)
	}
	// Only identity, pid, host, and time; no paths or secrets.
	for _, forbidden := range []string{"password", "/home", "token", "secret"} {
		if containsSub(string(content), forbidden) {
			t.Fatalf("ownership leaked %q: %s", forbidden, content)
		}
	}
}

func TestOwnershipLivenessIsInjectable(t *testing.T) {
	paths, sessionRoot := newSession(t)
	build := func(liveness LivenessChecker) Backend {
		backend, err := NewSyntheticBackend(SyntheticConfig{
			Paths:        paths,
			Source:       SyntheticSource{Frames: 200},
			Clock:        fixedClock(),
			NewSegmentID: sequentialSegmentIDs(),
			Liveness:     liveness,
		})
		if err != nil {
			t.Fatal(err)
		}
		return backend
	}
	// Hold ownership by leaving a recording active, then inspect with each checker.
	backend := build(aliveLiveness)
	startSegment(t, backend, sessionRoot, 1)
	inspection, err := backend.Inspect(nil, sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !hasIssue(inspection, IssueActiveOwnership) {
		t.Fatalf("alive checker did not report active ownership: %+v", inspection.Issues)
	}

	dead := build(deadLiveness)
	deadInspection, err := dead.Inspect(nil, sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !hasIssue(deadInspection, IssueStaleOwnership) {
		t.Fatalf("dead checker did not report stale ownership: %+v", deadInspection.Issues)
	}
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func hasIssue(inspection Inspection, kind RecoveryIssueKind) bool {
	for _, issue := range inspection.Issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}

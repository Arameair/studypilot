package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// finalizeOne records and finalizes segment number n, leaving a healthy segment.
func finalizeOne(t *testing.T, backend Backend, sessionRoot string, n int) {
	t.Helper()
	active := startSegment(t, backend, sessionRoot, n)
	if _, err := backend.FinalizeSegment(context.Background(), active); err != nil {
		t.Fatal(err)
	}
}

func TestInspectReturnsHealthyAndPartialSeparately(t *testing.T) {
	backend, sessionRoot := newSyntheticBackend(t, 400)
	finalizeOne(t, backend, sessionRoot, 1)
	segments := segmentsPath(sessionRoot)
	// Inject an orphan partial for segment 2.
	writeWholeWAV(t, filepath.Join(segments, partialName(2)), DefaultFormat(), 100)

	inspection, err := backend.Inspect(context.Background(), sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Finalized) != 1 || inspection.Finalized[0].Number != 1 {
		t.Fatalf("finalized = %+v", inspection.Finalized)
	}
	if len(inspection.Partial) != 1 || inspection.Partial[0].Number != 2 {
		t.Fatalf("partial = %+v", inspection.Partial)
	}
	if !hasIssue(inspection, IssuePartialAudio) {
		t.Fatalf("partial audio issue missing: %+v", inspection.Issues)
	}
}

func TestInspectClassifiesCrashStates(t *testing.T) {
	format := DefaultFormat()
	tests := []struct {
		name  string
		setup func(t *testing.T, segments string)
		want  RecoveryIssueKind
	}{
		{"missing manifest", func(t *testing.T, s string) {
			writeWholeWAV(t, filepath.Join(s, audioName(1)), format, 100)
		}, IssueMissingManifest},
		{"missing audio", func(t *testing.T, s string) {
			if err := writeManifestAtomic(s, filepath.Join(s, manifestName(1)), validManifest()); err != nil {
				t.Fatal(err)
			}
		}, IssueMissingAudio},
		{"conflicting files", func(t *testing.T, s string) {
			writeWholeWAV(t, filepath.Join(s, audioName(1)), format, 100)
			writeWholeWAV(t, filepath.Join(s, partialName(1)), format, 50)
			if err := writeManifestAtomic(s, filepath.Join(s, manifestName(1)), validManifest()); err != nil {
				t.Fatal(err)
			}
		}, IssueConflictingFiles},
		{"malformed manifest", func(t *testing.T, s string) {
			writeWholeWAV(t, filepath.Join(s, audioName(1)), format, 100)
			if err := os.WriteFile(filepath.Join(s, manifestName(1)), []byte("{ bad"), 0o640); err != nil {
				t.Fatal(err)
			}
		}, IssueMalformedManifest},
		{"unsupported manifest", func(t *testing.T, s string) {
			writeWholeWAV(t, filepath.Join(s, audioName(1)), format, 100)
			m := validManifest()
			m.SchemaVersion = 99
			content := []byte(`{"schema_version":99,"audio_file":"001-audio.wav"}`)
			if err := os.WriteFile(filepath.Join(s, manifestName(1)), content, 0o640); err != nil {
				t.Fatal(err)
			}
		}, IssueUnsupportedManifest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend, sessionRoot := newSyntheticBackend(t, 400)
			segments := segmentsPath(sessionRoot)
			if err := os.MkdirAll(segments, 0o750); err != nil {
				t.Fatal(err)
			}
			before := snapshotDir(t, segments)
			test.setup(t, segments)
			after := snapshotDir(t, segments)
			inspection, err := backend.Inspect(context.Background(), sessionRoot)
			if err != nil {
				t.Fatal(err)
			}
			if !hasIssue(inspection, test.want) {
				t.Fatalf("missing issue %s: %+v", test.want, inspection.Issues)
			}
			// Inspection must not mutate anything.
			if postInspect := snapshotDir(t, segments); !equalDirs(after, postInspect) {
				t.Fatal("inspection mutated the Segments directory")
			}
			_ = before
		})
	}
}

func TestInspectOwnershipLivenessAndStableOrder(t *testing.T) {
	paths, sessionRoot := newSession(t)
	backend, err := NewSyntheticBackend(SyntheticConfig{
		Paths: paths, Source: SyntheticSource{Frames: 200}, Clock: fixedClock(),
		NewSegmentID: sequentialSegmentIDs(), Liveness: deadLiveness,
	})
	if err != nil {
		t.Fatal(err)
	}
	segments := segmentsPath(sessionRoot)
	if err := os.MkdirAll(segments, 0o750); err != nil {
		t.Fatal(err)
	}
	// Two partial segments plus a stale ownership lock.
	writeWholeWAV(t, filepath.Join(segments, partialName(3)), DefaultFormat(), 50)
	writeWholeWAV(t, filepath.Join(segments, partialName(1)), DefaultFormat(), 50)
	owner := Ownership{SchemaVersion: ownershipSchemaVersion, CaptureID: "capture-x", SegmentID: "segment-x", Number: 5, ProcessID: 999999, Host: "otherhost", StartedAt: time.Now()}
	authority, _ := NewSegmentAuthority(paths, sessionRoot)
	if err := createOwnership(authority, owner); err != nil {
		t.Fatal(err)
	}
	inspection, err := backend.Inspect(context.Background(), sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !hasIssue(inspection, IssueStaleOwnership) {
		t.Fatalf("stale ownership not reported: %+v", inspection.Issues)
	}
	// Issues are ordered by number then kind.
	for i := 1; i < len(inspection.Issues); i++ {
		if inspection.Issues[i-1].Number > inspection.Issues[i].Number {
			t.Fatalf("issues not stably ordered: %+v", inspection.Issues)
		}
	}
	if len(inspection.Partial) != 2 || inspection.Partial[0].Number != 1 || inspection.Partial[1].Number != 3 {
		t.Fatalf("partials not ordered: %+v", inspection.Partial)
	}
}

func snapshotDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	states := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return states
		}
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		states[entry.Name()] = content
	}
	return states
}

func equalDirs(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for name, content := range a {
		if other, ok := b[name]; !ok || string(other) != string(content) {
			return false
		}
	}
	return true
}

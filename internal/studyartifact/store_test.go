package studyartifact

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

type artifactFixture struct {
	store             *Store
	root, sessionRoot string
	now               time.Time
	next              int
}

func newArtifactFixture(t *testing.T) *artifactFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "01 Courses", "Course", "Modules", "01 - Module")
	sessionRoot := filepath.Join(root, "Sessions", "001 - Session")
	for _, p := range []string{filepath.Join(root, "Notes"), filepath.Join(root, "Assets"), filepath.Join(sessionRoot, "Notes"), filepath.Join(sessionRoot, "Assets"), filepath.Join(sessionRoot, "Segments"), filepath.Join(sessionRoot, "Transcripts")} {
		if err := os.MkdirAll(p, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	f := &artifactFixture{root: root, sessionRoot: sessionRoot, now: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)}
	store, err := NewStore(Context{CourseID: "course-1", ModuleID: "module-1", ModuleRoot: root, Sessions: []SessionContext{{ID: "session-1", Root: sessionRoot, Snapshot: studyruntime.Snapshot{}}}}, func() time.Time { return f.now }, func() (ID, error) { f.next++; return NewID(fmt.Sprintf("study-artifact-%032x", f.next)) })
	if err != nil {
		t.Fatal(err)
	}
	f.store = store
	return f
}
func digestString(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

func TestSessionNotesReadUpdateValidationAndRevision(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := context.Background()
	created, index, err := f.store.CreateSessionNotes(ctx, "session-1", "Session Notes", 0)
	if err != nil || index.Revision != 1 {
		t.Fatalf("create=%+v index=%+v err=%v", created, index, err)
	}
	original, err := f.store.LoadSessionNotes(ctx, "session-1")
	if err != nil || original.Artifact.ID != created.ID || original.Revision != 1 || !strings.Contains(original.Content, "# Session Notes") {
		t.Fatalf("original=%+v err=%v", original, err)
	}
	audioPath := filepath.Join(f.sessionRoot, "Segments", "001-audio.wav")
	transcriptPath := filepath.Join(f.sessionRoot, "Transcripts", "001-transcript.txt")
	if err = os.WriteFile(audioPath, []byte("immutable audio"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(transcriptPath, []byte("immutable transcript"), 0o640); err != nil {
		t.Fatal(err)
	}
	audioBefore := digestString("immutable audio")
	transcriptBefore := digestString("immutable transcript")
	content := "# Session Notes\n\nUnicode: café 日本語 🚀\n\n<script>alert('inert')</script>\n"
	updated, next, err := f.store.UpdateSessionNotes(ctx, "session-1", content, 1)
	if err != nil || next.Revision != 2 || updated.ID != created.ID || updated.SHA256 != digestString(content) || updated.SizeBytes != int64(len(content)) {
		t.Fatalf("updated=%+v next=%+v err=%v", updated, next, err)
	}
	reloaded, err := f.store.LoadSessionNotes(ctx, "session-1")
	if err != nil || reloaded.Content != content || reloaded.Revision != 2 || reloaded.Artifact.RelatedTranscriptArtifactIDs == nil && created.RelatedTranscriptArtifactIDs != nil {
		t.Fatalf("reloaded=%+v err=%v", reloaded, err)
	}
	for path, want := range map[string]string{audioPath: audioBefore, transcriptPath: transcriptBefore} {
		body, readErr := os.ReadFile(path)
		if readErr != nil || digestString(string(body)) != want {
			t.Fatalf("note update changed %s: err=%v", filepath.Base(path), readErr)
		}
	}
	if _, _, err = f.store.UpdateSessionNotes(ctx, "session-1", "stale", 1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update err=%v", err)
	}
	for name, invalid := range map[string]string{
		"nul":      "invalid\x00content",
		"oversize": strings.Repeat("x", MaxNoteBytes+1),
		"utf8":     string([]byte{0xff, 0xfe}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, invalidErr := f.store.UpdateSessionNotes(ctx, "session-1", invalid, 2); !errors.Is(invalidErr, ErrInvalid) {
				t.Fatalf("invalid update err=%v", invalidErr)
			}
		})
	}
}

func TestIdentityTypeScopeAndRecordValidation(t *testing.T) {
	id, err := NewID("study-artifact-0123456789abcdef0123456789abcdef")
	if err != nil || id.String() == "" {
		t.Fatal(err)
	}
	for _, value := range []string{"artifact-0123456789abcdef0123456789abcdef", "study-artifact-short", "study-artifact-0123456789abcdef0123456789abcdeg"} {
		if _, err = NewID(value); err == nil {
			t.Fatalf("invalid ID %q accepted", value)
		}
	}
	for _, scope := range []Scope{{Kind: ScopeModule, CourseID: "c", ModuleID: "m"}, {Kind: ScopeSession, CourseID: "c", ModuleID: "m", SessionID: "s"}, {Kind: ScopeSegment, CourseID: "c", ModuleID: "m", SessionID: "s", SegmentID: "x"}} {
		if err = scope.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if (Scope{Kind: ScopeSession, CourseID: "c", ModuleID: "m"}).Validate() == nil {
		t.Fatal("invalid scope accepted")
	}
	if Type("unknown").Valid() {
		t.Fatal("unknown artifact type accepted")
	}
	record := Record{ID: id, Type: TypeAsset, Scope: Scope{Kind: ScopeModule, CourseID: "c", ModuleID: "m"}, Title: "Asset", RelativePath: "Assets/file.txt", OriginalFilename: "file.txt", MediaType: "text/plain", Category: "code", Source: "import", CreatedAt: time.Now(), UpdatedAt: time.Now(), SizeBytes: 1, SHA256: digestString("x")}
	if err = record.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Record){
		"absolute path": func(r *Record) { r.RelativePath = "/private/file" },
		"unsafe title":  func(r *Record) { r.Title = "unsafe\nvalue" },
		"invalid hash":  func(r *Record) { r.SHA256 = "ABC" },
		"invalid type":  func(r *Record) { r.Type = Type("unknown") },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := record.Clone()
			mutate(&invalid)
			if invalid.Validate() == nil {
				t.Fatal("invalid record accepted")
			}
		})
	}
	clone := record.Clone()
	clone.Title = "changed"
	if record.Title == clone.Title {
		t.Fatal("clone shares state")
	}
}

func TestIndexValidationRejectsSchemaAndDuplicates(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	id, _ := NewID("study-artifact-0123456789abcdef0123456789abcdef")
	record := Record{ID: id, Type: TypeAsset, Scope: Scope{Kind: ScopeModule, CourseID: "course-1", ModuleID: "module-1"}, Title: "Asset", RelativePath: "Assets/file.txt", OriginalFilename: "file.txt", MediaType: "text/plain", Category: "code", Source: "import", CreatedAt: now, UpdatedAt: now, SizeBytes: 1, SHA256: digestString("x")}
	for name, index := range map[string]Index{
		"unsupported schema": {SchemaVersion: 2},
		"duplicate identity": {SchemaVersion: 1, Revision: 1, UpdatedAt: now, Artifacts: []Record{record, func() Record { r := record.Clone(); r.RelativePath = "Assets/other.txt"; return r }()}},
		"duplicate path": {SchemaVersion: 1, Revision: 1, UpdatedAt: now, Artifacts: []Record{record, func() Record {
			r := record.Clone()
			r.ID, _ = NewID("study-artifact-fedcba9876543210fedcba9876543210")
			return r
		}()}},
	} {
		t.Run(name, func(t *testing.T) {
			if index.Validate() == nil {
				t.Fatal("invalid index accepted")
			}
		})
	}
}

func TestNotesAssetsIndexRevisionRefreshAndInspection(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := context.Background()
	module, index, err := f.store.CreateModuleNotes(ctx, "Module Working Notes", 0)
	if err != nil || index.Revision != 1 || module.RelativePath != "Notes/module-notes.md" {
		t.Fatalf("module=%+v index=%+v err=%v", module, index, err)
	}
	content, _ := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(module.RelativePath)))
	for _, heading := range []string{"## Module Summary", "## Key Concepts", "## Questions", "## Exercises", "## References"} {
		if !strings.Contains(string(content), heading) {
			t.Errorf("missing %s", heading)
		}
	}
	if _, _, err = f.store.CreateModuleNotes(ctx, "Duplicate", 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate err=%v", err)
	}
	session, index, err := f.store.CreateSessionNotes(ctx, "session-1", "Session Working Notes", 1)
	if err != nil || index.Revision != 2 || session.Scope.Kind != ScopeSession {
		t.Fatal(err)
	}
	notePath := filepath.Join(f.root, filepath.FromSlash(session.RelativePath))
	body, _ := os.ReadFile(notePath)
	body = append(body, []byte("\nUser text stays here.\nsession_id: user-authored-example\n")...)
	if err = os.WriteFile(notePath, body, 0o640); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "Reference Diagram.PNG")
	if err = os.WriteFile(source, []byte("synthetic image bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	asset, index, err := f.store.RegisterSessionAsset(ctx, "session-1", source, "Reference Diagram", "image", 2)
	if err != nil || index.Revision != 3 || asset.OriginalFilename != "Reference-Diagram.png" || !strings.HasPrefix(filepath.Base(asset.RelativePath), asset.ID.String()+"-") {
		t.Fatalf("asset=%+v index=%+v err=%v", asset, index, err)
	}
	sourceContent, _ := os.ReadFile(source)
	destinationContent, _ := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(asset.RelativePath)))
	if string(sourceContent) != string(destinationContent) || asset.SHA256 != digestString(string(sourceContent)) {
		t.Fatal("asset copy mismatch")
	}
	if _, _, err = f.store.RegisterModuleAsset(ctx, source, "Stale", "image", 2); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale err=%v", err)
	}
	inspection, err := f.store.Inspect(ctx)
	if err != nil || !hasIssue(inspection.Issues, "artifact_hash_mismatch") {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
	refreshed, issues, err := f.store.Refresh(ctx, 3)
	if err != nil || refreshed.Revision != 4 || len(issues) != 0 || len(refreshed.Artifacts) != 3 {
		t.Fatalf("refresh=%+v issues=%+v err=%v", refreshed, issues, err)
	}
	after, _ := os.ReadFile(notePath)
	if !strings.Contains(string(after), "User text stays here.") {
		t.Fatal("refresh overwrote user body")
	}
	reloaded, err := f.store.Load(ctx)
	if err != nil || reloaded.Revision != 4 || len(reloaded.Artifacts) != 3 {
		t.Fatalf("reload=%+v err=%v", reloaded, err)
	}
	again, _, err := f.store.Refresh(ctx, 4)
	if err != nil || again.Artifacts[0].ID != refreshed.Artifacts[0].ID {
		t.Fatal("refresh did not preserve identities")
	}
}

func TestAssetSafetyAndUnmanagedInspection(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := context.Background()
	dir := t.TempDir()
	if _, _, err := f.store.RegisterModuleAsset(ctx, dir, "Directory", "other", 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("directory err=%v", err)
	}
	target := filepath.Join(dir, "target.txt")
	os.WriteFile(target, []byte("x"), 0o640)
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, _, err := f.store.RegisterModuleAsset(ctx, link, "Link", "other", 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("link err=%v", err)
	}
	oversize := filepath.Join(dir, "large.bin")
	file, err := os.Create(oversize)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(MaxAssetSize + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, _, err = f.store.RegisterModuleAsset(ctx, oversize, "Large", "other", 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("large err=%v", err)
	}
	unmanaged := filepath.Join(f.root, "Assets", "unmanaged.txt")
	os.WriteFile(unmanaged, []byte("private"), 0o640)
	inspection, err := f.store.Inspect(ctx)
	if err != nil || !hasIssue(inspection.Issues, "unmanaged_file") {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
	if strings.Contains(fmt.Sprintf("%+v", inspection), f.root) || strings.Contains(fmt.Sprintf("%+v", inspection), "private") {
		t.Fatal("inspection leaked path or contents")
	}
}

func TestSameNamedAssetsUseDistinctManagedIdentities(t *testing.T) {
	f := newArtifactFixture(t)
	firstSource := filepath.Join(t.TempDir(), "reference.txt")
	secondSource := filepath.Join(t.TempDir(), "reference.txt")
	if err := os.WriteFile(firstSource, []byte("first"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondSource, []byte("second"), 0o640); err != nil {
		t.Fatal(err)
	}
	first, index, err := f.store.RegisterModuleAsset(context.Background(), firstSource, "First reference", "document", 0)
	if err != nil {
		t.Fatal(err)
	}
	second, index, err := f.store.RegisterModuleAsset(context.Background(), secondSource, "Second reference", "document", index.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.RelativePath == second.RelativePath || index.Revision != 2 {
		t.Fatalf("first=%+v second=%+v revision=%d", first, second, index.Revision)
	}
	for _, source := range []struct {
		path, want string
	}{{firstSource, "first"}, {secondSource, "second"}} {
		content, readErr := os.ReadFile(source.path)
		if readErr != nil || string(content) != source.want {
			t.Fatalf("source changed: %q err=%v", content, readErr)
		}
	}
}

func TestInstalledFileSurvivesIndexFailureAndIsInspectable(t *testing.T) {
	f := newArtifactFixture(t)
	f.store.saveHook = func(string, []byte) error { return errors.New("injected index failure") }
	record, index, err := f.store.CreateModuleNotes(context.Background(), "Uncertain Notes", 0)
	if !errors.Is(err, ErrPersistenceUncertain) || index.Revision != 0 || record.RelativePath != "Notes/module-notes.md" {
		t.Fatalf("record=%+v index=%+v err=%v", record, index, err)
	}
	if _, statErr := os.Stat(filepath.Join(f.root, "Notes", "module-notes.md")); statErr != nil {
		t.Fatal("installed note was removed")
	}
	f.store.saveHook = nil
	inspection, inspectErr := f.store.Inspect(context.Background())
	if inspectErr != nil || !hasIssue(inspection.Issues, "unindexed_managed_file") {
		t.Fatalf("inspection=%+v err=%v", inspection, inspectErr)
	}
}

func TestInstalledAssetSurvivesIndexFailureWithoutPersistingSourcePath(t *testing.T) {
	f := newArtifactFixture(t)
	source := filepath.Join(t.TempDir(), "private-source.txt")
	if err := os.WriteFile(source, []byte("synthetic"), 0o640); err != nil {
		t.Fatal(err)
	}
	f.store.saveHook = func(string, []byte) error { return errors.New("injected index failure") }
	record, index, err := f.store.RegisterModuleAsset(context.Background(), source, "Reference", "document", 0)
	if !errors.Is(err, ErrPersistenceUncertain) || index.Revision != 0 {
		t.Fatalf("record=%+v index=%+v err=%v", record, index, err)
	}
	if _, statErr := os.Stat(filepath.Join(f.root, filepath.FromSlash(record.RelativePath))); statErr != nil {
		t.Fatal("installed asset was removed")
	}
	f.store.saveHook = nil
	inspection, inspectErr := f.store.Inspect(context.Background())
	if inspectErr != nil || !hasIssue(inspection.Issues, "unindexed_managed_file") {
		t.Fatalf("inspection=%+v err=%v", inspection, inspectErr)
	}
	encoded, _ := json.Marshal(inspection)
	if strings.Contains(string(encoded), source) {
		t.Fatal("external source path was persisted or returned")
	}
}

func TestMalformedIndexAndNoteMetadataAreReadOnlyIssues(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Dir(f.store.IndexPath()), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.store.IndexPath(), []byte("{bad"), 0o640); err != nil {
		t.Fatal(err)
	}
	inspection, err := f.store.Inspect(ctx)
	if err != nil || !hasIssue(inspection.Issues, "malformed_index") {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
	if err := os.Remove(f.store.IndexPath()); err != nil {
		t.Fatal(err)
	}
	_, _, err = f.store.CreateSessionNotes(ctx, "session-1", "Notes", 0)
	if err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(f.sessionRoot, "Notes", "session-notes.md")
	content, err := os.ReadFile(note)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(content), "session_id: session-1", "session_id: session-other", 1)
	if err = os.WriteFile(note, []byte(changed), 0o640); err != nil {
		t.Fatal(err)
	}
	inspection, err = f.store.Inspect(ctx)
	if err != nil || !hasIssue(inspection.Issues, "note_metadata_mismatch") {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
	after, _ := os.ReadFile(note)
	if string(after) != changed {
		t.Fatal("inspection mutated malformed note")
	}
}

func TestInspectionReportsMissingSizeAndHashDeterministically(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "asset.txt")
	if err := os.WriteFile(source, []byte("original"), 0o640); err != nil {
		t.Fatal(err)
	}
	record, index, err := f.store.RegisterModuleAsset(ctx, source, "Asset", "document", 0)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(f.root, filepath.FromSlash(record.RelativePath))
	if err = os.WriteFile(destination, []byte("changed and longer"), 0o640); err != nil {
		t.Fatal(err)
	}
	first, err := f.store.Inspect(ctx)
	if err != nil || !hasIssue(first.Issues, "artifact_hash_mismatch") || !hasIssue(first.Issues, "artifact_size_mismatch") {
		t.Fatalf("inspection=%+v err=%v", first, err)
	}
	second, err := f.store.Inspect(ctx)
	firstJSON, _ := json.Marshal(first.Issues)
	secondJSON, _ := json.Marshal(second.Issues)
	if err != nil || string(firstJSON) != string(secondJSON) {
		t.Fatalf("issues are not deterministic: %s != %s", firstJSON, secondJSON)
	}
	if err = os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	missing, err := f.store.Inspect(ctx)
	if err != nil || missing.Revision != index.Revision || !hasIssue(missing.Issues, "indexed_file_missing") {
		t.Fatalf("inspection=%+v err=%v", missing, err)
	}
}

func TestTranscriptDiscoveryLinksSessionNotesWithoutBody(t *testing.T) {
	f := newArtifactFixture(t)
	ctx := context.Background()
	sessionNote, index, err := f.store.CreateSessionNotes(ctx, "session-1", "Session Notes", 0)
	if err != nil {
		t.Fatal(err)
	}
	segmentID := "segment-1"
	jobID := "transcription-job-11111111111111111111111111111111"
	audio := []byte("synthetic wav")
	audioPath := filepath.Join(f.sessionRoot, "Segments", "001-audio.wav")
	os.WriteFile(audioPath, audio, 0o640)
	hash := fmt.Sprintf("%x", sha256.Sum256(audio))
	transcript := map[string]any{"schema_version": 1, "job_id": jobID, "session_id": "session-1", "capture_id": "capture-1", "segment_id": segmentID, "segment_number": 1, "transcript": map[string]any{"text": "private transcript body", "language": "en", "duration_millis": 1000}, "provenance_relative_path": "Transcripts/001-provenance.json"}
	provenance := map[string]any{"schema_version": 1, "provenance": map[string]any{"job_id": jobID, "session_id": "session-1", "capture_id": "capture-1", "segment_id": segmentID, "input_relative_path": "Segments/001-audio.wav", "input_sha256": hash}}
	job := map[string]any{"schema_version": 1, "job_id": jobID, "session_id": "session-1", "capture_id": "capture-1", "segment_id": segmentID, "segment_number": 1, "status": "completed", "completed_at": f.now}
	writeJSONTest(t, filepath.Join(f.sessionRoot, "Transcripts", "001-transcript.json"), transcript)
	os.WriteFile(filepath.Join(f.sessionRoot, "Transcripts", "001-transcript.txt"), []byte("private transcript body\n"), 0o640)
	writeJSONTest(t, filepath.Join(f.sessionRoot, "Transcripts", "001-provenance.json"), provenance)
	writeJSONTest(t, filepath.Join(f.sessionRoot, "Transcripts", "001-transcription-job.json"), job)
	queued := f.now
	completed := f.now
	f.store.context.Sessions[0].Snapshot = studyruntime.Snapshot{
		Segments:       []studyruntime.SegmentSummary{{ID: segmentID, Number: 1, AudioPath: "Segments/001-audio.wav"}},
		Transcriptions: []studyruntime.SegmentTranscriptionState{{SegmentID: segmentID, SegmentNumber: 1, JobID: jobID, JobStatus: "completed", QueueStatus: "terminal", InputRelativePath: "Segments/001-audio.wav", TranscriptJSONRelativePath: "Transcripts/001-transcript.json", TranscriptTextRelativePath: "Transcripts/001-transcript.txt", ProvenanceRelativePath: "Transcripts/001-provenance.json", JobMetadataRelativePath: "Transcripts/001-transcription-job.json", QueuedAt: &queued, UpdatedAt: &completed}},
	}
	refreshed, issues, err := f.store.Refresh(ctx, index.Revision)
	if err != nil || len(issues) != 0 || len(refreshed.Artifacts) != 2 {
		t.Fatalf("refresh=%+v issues=%+v err=%v", refreshed, issues, err)
	}
	var transcriptRecord, noteRecord Record
	for _, r := range refreshed.Artifacts {
		if r.Type == TypeTranscript {
			transcriptRecord = r
		}
		if r.ID == sessionNote.ID {
			noteRecord = r
		}
	}
	if transcriptRecord.ID == "" || transcriptRecord.Transcript == nil || len(noteRecord.RelatedTranscriptArtifactIDs) != 1 || noteRecord.RelatedTranscriptArtifactIDs[0] != transcriptRecord.ID {
		t.Fatalf("transcript=%+v note=%+v", transcriptRecord, noteRecord)
	}
	encoded, _ := json.Marshal(refreshed)
	if strings.Contains(string(encoded), "private transcript body") {
		t.Fatal("index contains transcript body")
	}
}

func TestTranscriptDiscoveryReportsMissingAndOrphanEvidence(t *testing.T) {
	t.Run("completed runtime without artifacts", func(t *testing.T) {
		f := newArtifactFixture(t)
		f.store.context.Sessions[0].Snapshot = studyruntime.Snapshot{Transcriptions: []studyruntime.SegmentTranscriptionState{{SegmentID: "segment-1", SegmentNumber: 1, JobID: "transcription-job-11111111111111111111111111111111", JobStatus: "completed", TranscriptJSONRelativePath: "Transcripts/001-transcript.json", TranscriptTextRelativePath: "Transcripts/001-transcript.txt", ProvenanceRelativePath: "Transcripts/001-provenance.json", JobMetadataRelativePath: "Transcripts/001-transcription-job.json"}}}
		inspection, err := f.store.Inspect(context.Background())
		if err != nil || !hasIssue(inspection.Issues, "transcript_artifact_set_incomplete") {
			t.Fatalf("inspection=%+v err=%v", inspection, err)
		}
	})
	t.Run("transcript file without completed runtime", func(t *testing.T) {
		f := newArtifactFixture(t)
		if err := os.WriteFile(filepath.Join(f.sessionRoot, "Transcripts", "001-transcript.json"), []byte("{}"), 0o640); err != nil {
			t.Fatal(err)
		}
		inspection, err := f.store.Inspect(context.Background())
		if err != nil || !hasIssue(inspection.Issues, "transcript_without_completed_job") {
			t.Fatalf("inspection=%+v err=%v", inspection, err)
		}
	})
}

func writeJSONTest(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
}
func hasIssue(issues []Issue, code IssueCode) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

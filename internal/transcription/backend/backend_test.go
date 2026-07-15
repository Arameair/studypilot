package backend

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Arameair/studypilot/internal/transcription"
	"github.com/Arameair/studypilot/internal/workspace"
)

type fixture struct {
	paths              workspace.Paths
	sessionRoot, input string
	job                transcription.Job
	artifacts          transcription.TranscriptArtifacts
	now                time.Time
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	paths, err := workspace.PathsFromRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	sessionRoot := filepath.Join(paths.Private, "01 Courses", "Course", "Modules", "Module", "Sessions", "Session")
	if err = os.MkdirAll(filepath.Join(sessionRoot, "Segments"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(paths.Portfolio, 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(sessionRoot, sessionMarkerName), []byte("{}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(sessionRoot, "Segments", "001-audio.wav")
	if err = os.WriteFile(input, []byte("synthetic wav bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	id, err := transcription.NewJobID("22222222222222222222222222222222")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	job := transcription.Job{ID: id, SessionID: "session-synthetic", CaptureID: "capture-synthetic", SegmentID: "segment-synthetic", SegmentNumber: 1, InputRelativePath: "Segments/001-audio.wav", Backend: "synthetic", Model: "synthetic/deterministic", Language: "en", Status: transcription.JobQueued, QueuedAt: now, UpdatedAt: now}
	artifacts := transcription.TranscriptArtifacts{JSONRelativePath: "Transcripts/001-transcript.json", TextRelativePath: "Transcripts/001-transcript.txt", JobRelativePath: "Transcripts/001-transcription-job.json", ProvenanceRelativePath: "Transcripts/001-provenance.json"}
	return fixture{paths, sessionRoot, input, job, artifacts, now}
}
func request(f fixture) TranscribeRequest {
	return TranscribeRequest{Job: f.job, SessionRoot: f.sessionRoot, Artifacts: f.artifacts, Backend: f.job.Backend, Model: f.job.Model, Language: "en", WordTimestamps: true, Timeout: time.Second}
}

func TestSyntheticBackendDeterministicPartialFailureCancellationAndTimeout(t *testing.T) {
	f := newFixture(t)
	before, _ := os.ReadFile(f.input)
	backend, err := NewSyntheticBackend(SyntheticConfig{Clock: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	first, err := backend.Transcribe(context.Background(), request(f))
	if err != nil {
		t.Fatal(err)
	}
	second, err := backend.Transcribe(context.Background(), request(f))
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("non-deterministic result: %v", err)
	}
	if first.Outcome != OutcomeCompleted || first.Transcript.Text != "Synthetic transcription for segment 001." || first.Provenance.InputSHA256 == "" {
		t.Fatal(first)
	}
	after, _ := os.ReadFile(f.input)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("source WAV changed")
	}
	partial, _ := NewSyntheticBackend(SyntheticConfig{Clock: func() time.Time { return f.now }, Partial: true})
	got, err := partial.Transcribe(context.Background(), request(f))
	if err != nil || got.Outcome != OutcomePartial || !got.Transcript.Partial {
		t.Fatalf("partial=%+v %v", got, err)
	}
	injected := newError(ErrorProcessFailed, "synthetic", true, "synthetic failure", nil)
	failed, _ := NewSyntheticBackend(SyntheticConfig{Clock: func() time.Time { return f.now }, Failure: injected})
	if _, err = failed.Transcribe(context.Background(), request(f)); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = backend.Transcribe(ctx, request(f)); CodeOf(err) != ErrorCancelled {
		t.Fatalf("cancel=%v", err)
	}
	slow, _ := NewSyntheticBackend(SyntheticConfig{Clock: func() time.Time { return f.now }, Delay: 50 * time.Millisecond})
	r := request(f)
	r.Timeout = time.Millisecond
	if _, err = slow.Transcribe(context.Background(), r); CodeOf(err) != ErrorTimeout {
		t.Fatalf("timeout=%v", err)
	}
}

func TestBackendRejectsLinkedSourceAudio(t *testing.T) {
	f := newFixture(t)
	original := f.input + ".original"
	if err := os.Rename(f.input, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(original, f.input); err != nil {
		t.Fatal(err)
	}
	backend, _ := NewSyntheticBackend(SyntheticConfig{Clock: func() time.Time { return f.now }})
	if _, err := backend.Transcribe(context.Background(), request(f)); CodeOf(err) != ErrorUnsafePath {
		t.Fatalf("symlink=%v", err)
	}
	if err := os.Remove(f.input); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, f.input); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Transcribe(context.Background(), request(f)); CodeOf(err) != ErrorUnsafePath {
		t.Fatalf("hardlink=%v", err)
	}
}

func validWorkerResult(f fixture) []byte {
	result := WorkerResult{SchemaVersion: 1, JobID: f.job.ID.String(), Status: "completed", Transcript: transcription.Transcript{Text: "synthetic", Language: "en", DurationMillis: 1, Segments: []transcription.TranscriptSegment{{Index: 0, StartMillis: 0, EndMillis: 1, Text: "synthetic"}}}, Backend: WorkerComponent{Name: "faster-whisper", Version: "1"}, Model: WorkerComponent{Name: "small.en", Version: "1"}}
	data, _ := json.Marshal(result)
	return data
}
func TestWorkerProtocolStrictValidation(t *testing.T) {
	f := newFixture(t)
	valid := validWorkerResult(f)
	if _, err := DecodeWorkerResult(valid, f.job.ID); err != nil {
		t.Fatal(err)
	}
	tests := map[string][]byte{"malformed": []byte("{"), "mixed": append(append([]byte{}, valid...), []byte("\ndiagnostic")...), "unknown": []byte(strings.Replace(string(valid), `"status":`, `"unknown":1,"status":`, 1)), "version": []byte(strings.Replace(string(valid), `"schema_version":1`, `"schema_version":2`, 1)), "job": []byte(strings.Replace(string(valid), f.job.ID.String(), "transcription-job-33333333333333333333333333333333", 1)), "timing": []byte(strings.Replace(string(valid), `"end_millis":1`, `"end_millis":2`, 1))}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeWorkerResult(data, f.job.ID); CodeOf(err) != ErrorProtocolMalformed {
				t.Fatalf("err=%v", err)
			}
		})
	}
	if _, err := DecodeWorkerResult(make([]byte, maxWorkerOutput+1), f.job.ID); CodeOf(err) != ErrorOutputTooLarge {
		t.Fatal(err)
	}
}

type fakeDiscovery struct{ python, worker, pkg, model bool }

func (d fakeDiscovery) Python(context.Context, string) bool  { return d.python }
func (d fakeDiscovery) Worker(context.Context, string) bool  { return d.worker }
func (d fakeDiscovery) Package(context.Context, string) bool { return d.pkg }
func (d fakeDiscovery) Model(context.Context, string) bool   { return d.model }

type fakeRunner struct {
	mu        sync.Mutex
	output    []byte
	err       error
	request   ProcessRequest
	looked    string
	lookupErr error
}

func (r *fakeRunner) Lookup(name string) (string, error) { r.looked = name; return name, r.lookupErr }
func (r *fakeRunner) Run(ctx context.Context, req ProcessRequest) (ProcessResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.request = req
	if err := ctx.Err(); err != nil {
		return ProcessResult{}, contextError(ctx, "fake_process")
	}
	return ProcessResult{Stdout: append([]byte(nil), r.output...)}, r.err
}
func TestLocalBackendDiscoveryAndFixedProcessBoundary(t *testing.T) {
	f := newFixture(t)
	runner := &fakeRunner{output: validWorkerResult(f)}
	local, err := NewLocalBackend(LocalConfig{Runner: runner, Discovery: fakeDiscovery{true, true, true, true}, Python: "python3", Worker: "worker.py", Clock: func() time.Time { return f.now }, ModelVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	f.job.Backend = "faster-whisper"
	f.job.Model = "faster-whisper/small.en"
	r := request(f)
	r.Job = f.job
	r.Backend = f.job.Backend
	r.Model = f.job.Model
	runner.output = validWorkerResult(f)
	runner.output = []byte(strings.Replace(string(runner.output), "synthetic/deterministic", "faster-whisper/small.en", 1))
	got, err := local.Transcribe(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != OutcomeCompleted || !reflect.DeepEqual(runner.request.Args, []string{"worker.py", "--protocol", "json-v1"}) {
		t.Fatalf("result=%+v request=%+v", got, runner.request)
	}
	var workerReq WorkerRequest
	if err = json.Unmarshal(runner.request.Stdin, &workerReq); err != nil || !filepath.IsAbs(workerReq.InputPath) {
		t.Fatalf("worker request=%+v %v", workerReq, err)
	}
	unavailable, _ := NewLocalBackend(LocalConfig{Runner: runner, Discovery: fakeDiscovery{}, Python: "python3", Worker: "worker.py", Clock: func() time.Time { return f.now }})
	capability, err := unavailable.Capabilities(context.Background())
	if err != nil || capability.Status != transcription.CapabilityUnavailable || len(capability.Issues) != 4 {
		t.Fatalf("capability=%+v %v", capability, err)
	}
	for i := 1; i < len(capability.Issues); i++ {
		if capability.Issues[i-1].Code > capability.Issues[i].Code {
			t.Fatal("issues unsorted")
		}
	}
}

func TestLocalBackendPreflightErrorCodes(t *testing.T) {
	f := newFixture(t)
	f.job.Backend = "faster-whisper"
	f.job.Model = "faster-whisper/small.en"
	r := request(f)
	r.Job = f.job
	r.Backend = f.job.Backend
	r.Model = f.job.Model
	runner := &fakeRunner{}
	tests := []struct {
		name      string
		discovery fakeDiscovery
		want      ErrorCode
	}{{"python", fakeDiscovery{}, ErrorPythonMissing}, {"worker", fakeDiscovery{python: true}, ErrorWorkerMissing}, {"package", fakeDiscovery{python: true, worker: true}, ErrorBackendUnavailable}, {"model", fakeDiscovery{python: true, worker: true, pkg: true}, ErrorModelMissing}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local, err := NewLocalBackend(LocalConfig{Runner: runner, Discovery: tt.discovery, Python: "python3", Worker: "worker.py", Clock: func() time.Time { return f.now }})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = local.Transcribe(context.Background(), r); CodeOf(err) != tt.want {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestProcessBoundaryClassifiesFailureAndBoundsDiagnostics(t *testing.T) {
	f := newFixture(t)
	runner := &fakeRunner{err: errors.New("private stderr and command detail")}
	local, err := NewLocalBackend(LocalConfig{Runner: runner, Discovery: fakeDiscovery{true, true, true, true}, Python: "python3", Worker: "worker.py", Clock: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	f.job.Backend = "faster-whisper"
	f.job.Model = "faster-whisper/small.en"
	r := request(f)
	r.Job = f.job
	r.Backend = f.job.Backend
	r.Model = f.job.Model
	if _, err = local.Transcribe(context.Background(), r); CodeOf(err) != ErrorProcessFailed || strings.Contains(err.Error(), "private stderr") {
		t.Fatalf("err=%v", err)
	}
	buffer := &limitedBuffer{limit: 4}
	n, _ := buffer.Write([]byte("123456789"))
	if n != 9 || buffer.buf.String() != "1234" || !buffer.overflow {
		t.Fatalf("buffer=%q total=%d", buffer.buf.String(), buffer.total)
	}
}

func TestArtifactAuthorityRejectsUnsafeBoundaries(t *testing.T) {
	f := newFixture(t)
	authority, err := NewArtifactAuthority(f.paths, f.sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err = authority.EnsureDir(); err != nil {
		t.Fatal(err)
	}
	if _, err = authority.Resolve(f.artifacts.JSONRelativePath); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/tmp/x", "Transcripts/../x", "Other/001.json", "Transcripts/sub/001.json"} {
		if _, err = authority.Resolve(path); CodeOf(err) != ErrorUnsafePath {
			t.Errorf("%q: %v", path, err)
		}
	}
	target, _ := authority.Resolve(f.artifacts.JSONRelativePath)
	if err = os.Symlink(f.input, target); err != nil {
		t.Fatal(err)
	}
	if _, err = authority.Resolve(f.artifacts.JSONRelativePath); CodeOf(err) != ErrorUnsafePath {
		t.Fatal("symlinked target accepted")
	}
	if err = os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err = os.Link(f.input, target); err != nil {
		t.Fatal(err)
	}
	if _, err = authority.Resolve(f.artifacts.JSONRelativePath); CodeOf(err) != ErrorUnsafePath {
		t.Fatal("hard-linked target accepted")
	}
	if err = os.Remove(target); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(filepath.Dir(f.sessionRoot), "Sibling")
	if err = os.MkdirAll(sibling, 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(sibling, sessionMarkerName), []byte("{}"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err = authority.Resolve(filepath.ToSlash(filepath.Join("..", "Sibling", "Transcripts", "001-transcript.json"))); CodeOf(err) != ErrorUnsafePath {
		t.Fatal(err)
	}
	publicRoot := filepath.Join(f.paths.Portfolio, "01 Projects", "Fake", "Modules", "M", "Sessions", "S")
	_ = os.MkdirAll(publicRoot, 0o750)
	_ = os.WriteFile(filepath.Join(publicRoot, sessionMarkerName), []byte("{}"), 0o640)
	if _, err = NewArtifactAuthority(f.paths, publicRoot); CodeOf(err) != ErrorUnsafePath {
		t.Fatal(err)
	}
	_ = os.RemoveAll(authority.TranscriptsDir())
	if err = os.Symlink(t.TempDir(), authority.TranscriptsDir()); err != nil {
		t.Fatal(err)
	}
	if err = authority.EnsureDir(); CodeOf(err) != ErrorUnsafePath {
		t.Fatal(err)
	}
}

func syntheticResult(t *testing.T, f fixture) TranscribeResult {
	t.Helper()
	b, _ := NewSyntheticBackend(SyntheticConfig{Clock: func() time.Time { return f.now }})
	result, err := b.Transcribe(context.Background(), request(f))
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestArtifactPersistenceAndHealthyRecovery(t *testing.T) {
	f := newFixture(t)
	authority, err := NewArtifactAuthority(f.paths, f.sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	store := NewArtifactStore(authority)
	result := syntheticResult(t, f)
	persisted, err := store.Persist(context.Background(), f.job, result, 1)
	if err != nil || persisted.Outcome != OutcomeCompleted {
		t.Fatalf("persist=%+v %v", persisted, err)
	}
	for _, relative := range []string{f.artifacts.JSONRelativePath, f.artifacts.TextRelativePath, f.artifacts.JobRelativePath, f.artifacts.ProvenanceRelativePath} {
		path := filepath.Join(f.sessionRoot, filepath.FromSlash(relative))
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(content), f.sessionRoot) || strings.HasSuffix(path, ".partial") {
			t.Fatal("artifact leaked an absolute path or remained partial")
		}
	}
	entries, _ := os.ReadDir(authority.TranscriptsDir())
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".partial") {
			t.Fatal("successful persistence left partial evidence")
		}
	}
	text, _ := os.ReadFile(filepath.Join(f.sessionRoot, filepath.FromSlash(f.artifacts.TextRelativePath)))
	if string(text) != result.Transcript.Text+"\n" {
		t.Fatalf("text=%q", text)
	}
	inspection, err := store.Inspect(context.Background())
	if err != nil || len(inspection.Completed) != 1 || len(inspection.Issues) != 0 {
		t.Fatalf("inspection=%+v %v", inspection, err)
	}
	if _, err = store.Persist(context.Background(), f.job, result, 1); CodeOf(err) != ErrorArtifactConflict {
		t.Fatal(err)
	}
	inspection, err = store.Inspect(context.Background(), transcription.JobID("transcription-job-33333333333333333333333333333333"))
	if err != nil || !hasRecoveryIssue(inspection, "artifact_without_runtime_job") {
		t.Fatalf("runtime reconciliation=%+v %v", inspection, err)
	}
}

func TestFailedJobMetadataIsSafeAndNotACompletionMarker(t *testing.T) {
	f := newFixture(t)
	authority, _ := NewArtifactAuthority(f.paths, f.sessionRoot)
	store := NewArtifactStore(authority)
	failure, _ := transcription.NewError(transcription.ErrorTimeout, "synthetic", true, "safe timeout", errors.New("private raw cause"), f.job.ID)
	err := store.PersistFailure(context.Background(), f.job, f.artifacts, 1, failure)
	if CodeOf(err) != ErrorPartialOutput {
		t.Fatal(err)
	}
	partial := filepath.Join(f.sessionRoot, filepath.FromSlash(f.artifacts.JobRelativePath)) + ".partial"
	content, readErr := os.ReadFile(partial)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(content), "private raw cause") || !strings.Contains(string(content), `"error_code": "timeout"`) {
		t.Fatalf("unsafe metadata: %s", content)
	}
	if _, statErr := os.Stat(filepath.Join(f.sessionRoot, filepath.FromSlash(f.artifacts.JobRelativePath))); !os.IsNotExist(statErr) {
		t.Fatal("failed job installed a completion marker")
	}
}

type faultOps struct {
	base                  artifactOps
	writeFail, renameFail int
	syncFail              bool
	writes, renames       int
}

func (o *faultOps) WriteExclusive(path string, data []byte) error {
	o.writes++
	if o.writes == o.writeFail {
		return errors.New("synthetic write failure")
	}
	return o.base.WriteExclusive(path, data)
}
func (o *faultOps) Rename(a, b string) error {
	o.renames++
	if o.renames == o.renameFail {
		return errors.New("synthetic rename failure")
	}
	return o.base.Rename(a, b)
}
func (o *faultOps) SyncDir(path string) error {
	if o.syncFail {
		return errors.New("synthetic sync failure")
	}
	return o.base.SyncDir(path)
}
func TestArtifactDurabilityFailuresPreserveEvidence(t *testing.T) {
	for _, tt := range []struct {
		name          string
		write, rename int
		sync          bool
		want          ErrorCode
	}{{"first write", 1, 0, false, ErrorArtifactWriteFailed}, {"text write", 2, 0, false, ErrorPersistenceUncertain}, {"provenance write", 3, 0, false, ErrorPersistenceUncertain}, {"metadata write", 4, 0, false, ErrorPersistenceUncertain}, {"rename", 0, 2, false, ErrorPersistenceUncertain}, {"directory sync", 0, 0, true, ErrorPersistenceUncertain}} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			authority, _ := NewArtifactAuthority(f.paths, f.sessionRoot)
			ops := &faultOps{base: osArtifactOps{}, writeFail: tt.write, renameFail: tt.rename, syncFail: tt.sync}
			store := newArtifactStoreWithOps(authority, ops)
			_, err := store.Persist(context.Background(), f.job, syntheticResult(t, f), 1)
			if CodeOf(err) != tt.want {
				t.Fatalf("err=%v", err)
			}
			if tt.want == ErrorPersistenceUncertain {
				entries, _ := os.ReadDir(authority.TranscriptsDir())
				if len(entries) == 0 {
					t.Fatal("uncertain failure discarded all evidence")
				}
			}
		})
	}
}

func TestPartialPersistenceAndRecoveryIssues(t *testing.T) {
	f := newFixture(t)
	authority, _ := NewArtifactAuthority(f.paths, f.sessionRoot)
	store := NewArtifactStore(authority)
	partialBackend, _ := NewSyntheticBackend(SyntheticConfig{Clock: func() time.Time { return f.now }, Partial: true})
	result, err := partialBackend.Transcribe(context.Background(), request(f))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Persist(context.Background(), f.job, result, 1); CodeOf(err) != ErrorPartialOutput {
		t.Fatal(err)
	}
	inspection, err := store.Inspect(context.Background())
	if err != nil || !hasRecoveryIssue(inspection, "partial_transcript") {
		t.Fatalf("inspection=%+v %v", inspection, err)
	}
	f2 := newFixture(t)
	authority2, _ := NewArtifactAuthority(f2.paths, f2.sessionRoot)
	store2 := NewArtifactStore(authority2)
	final := syntheticResult(t, f2)
	if _, err = store2.Persist(context.Background(), f2.job, final, 1); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(f2.sessionRoot, filepath.FromSlash(f2.artifacts.JobRelativePath))); err != nil {
		t.Fatal(err)
	}
	inspection, _ = store2.Inspect(context.Background())
	if !hasRecoveryIssue(inspection, "missing_job_metadata") || !hasRecoveryIssue(inspection, "uncertain_completion") {
		t.Fatal(inspection)
	}
	f3 := newFixture(t)
	authority3, _ := NewArtifactAuthority(f3.paths, f3.sessionRoot)
	store3 := NewArtifactStore(authority3)
	if _, err = store3.Persist(context.Background(), f3.job, syntheticResult(t, f3), 1); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(f3.input, []byte("changed"), 0o640); err != nil {
		t.Fatal(err)
	}
	inspection, _ = store3.Inspect(context.Background())
	if !hasRecoveryIssue(inspection, "input_hash_mismatch") {
		t.Fatal(inspection)
	}
}

func TestRecoveryMalformedUnsupportedAndStableOrdering(t *testing.T) {
	f := newFixture(t)
	authority, _ := NewArtifactAuthority(f.paths, f.sessionRoot)
	store := NewArtifactStore(authority)
	if _, err := store.Persist(context.Background(), f.job, syntheticResult(t, f), 1); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(f.sessionRoot, filepath.FromSlash(f.artifacts.JSONRelativePath))
	if err := os.WriteFile(transcriptPath, []byte("{malformed"), 0o640); err != nil {
		t.Fatal(err)
	}
	first, err := store.Inspect(context.Background())
	if err != nil || !hasRecoveryIssue(first, "malformed_transcript") {
		t.Fatalf("inspection=%+v %v", first, err)
	}
	second, _ := store.Inspect(context.Background())
	if !reflect.DeepEqual(first, second) {
		t.Fatal("inspection ordering is unstable")
	}
	f2 := newFixture(t)
	authority2, _ := NewArtifactAuthority(f2.paths, f2.sessionRoot)
	store2 := NewArtifactStore(authority2)
	if _, err = store2.Persist(context.Background(), f2.job, syntheticResult(t, f2), 1); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f2.sessionRoot, filepath.FromSlash(f2.artifacts.JSONRelativePath))
	var doc map[string]any
	content, _ := os.ReadFile(path)
	_ = json.Unmarshal(content, &doc)
	doc["schema_version"] = float64(2)
	changed, _ := json.Marshal(doc)
	if err = os.WriteFile(path, changed, 0o640); err != nil {
		t.Fatal(err)
	}
	inspection, _ := store2.Inspect(context.Background())
	if !hasRecoveryIssue(inspection, "unsupported_schema") {
		t.Fatal(inspection)
	}
}

func TestRecoveryIssueMatrixAndPrivacy(t *testing.T) {
	tests := []struct {
		name, code string
		mutate     func(fixture) error
	}{{"missing text", "missing_transcript_text", func(f fixture) error {
		return os.Remove(filepath.Join(f.sessionRoot, filepath.FromSlash(f.artifacts.TextRelativePath)))
	}}, {"missing provenance", "missing_provenance", func(f fixture) error {
		return os.Remove(filepath.Join(f.sessionRoot, filepath.FromSlash(f.artifacts.ProvenanceRelativePath)))
	}}, {"malformed job", "malformed_job_metadata", func(f fixture) error {
		return os.WriteFile(filepath.Join(f.sessionRoot, filepath.FromSlash(f.artifacts.JobRelativePath)), []byte("{"), 0o640)
	}}, {"malformed provenance", "malformed_provenance", func(f fixture) error {
		return os.WriteFile(filepath.Join(f.sessionRoot, filepath.FromSlash(f.artifacts.ProvenanceRelativePath)), []byte("{"), 0o640)
	}}, {"missing input", "input_audio_missing", func(f fixture) error { return os.Remove(f.input) }}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			authority, _ := NewArtifactAuthority(f.paths, f.sessionRoot)
			store := NewArtifactStore(authority)
			if _, err := store.Persist(context.Background(), f.job, syntheticResult(t, f), 1); err != nil {
				t.Fatal(err)
			}
			if err := tt.mutate(f); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadDir(authority.TranscriptsDir())
			inspection, err := store.Inspect(context.Background())
			if err != nil || !hasRecoveryIssue(inspection, tt.code) {
				t.Fatalf("inspection=%+v err=%v", inspection, err)
			}
			after, _ := os.ReadDir(authority.TranscriptsDir())
			if len(before) != len(after) {
				t.Fatal("inspection mutated artifacts")
			}
			encoded, _ := json.Marshal(inspection)
			if strings.Contains(string(encoded), "Synthetic transcription") || strings.Contains(string(encoded), f.sessionRoot) {
				t.Fatal("inspection exposed transcript or absolute path")
			}
		})
	}
}

type partialErrorRunner struct {
	output []byte
	err    error
}

func (r partialErrorRunner) Lookup(name string) (string, error) { return name, nil }
func (r partialErrorRunner) Run(context.Context, ProcessRequest) (ProcessResult, error) {
	return ProcessResult{Stdout: r.output}, r.err
}
func TestLocalPartialOutputSurvivesTimeoutBoundary(t *testing.T) {
	f := newFixture(t)
	f.job.Backend = "faster-whisper"
	f.job.Model = "faster-whisper/small.en"
	worker := WorkerResult{SchemaVersion: 1, JobID: f.job.ID.String(), Status: "partial", Transcript: transcription.Transcript{Text: "synthetic partial", Language: "en", DurationMillis: 1, Partial: true, Segments: []transcription.TranscriptSegment{{Index: 0, StartMillis: 0, EndMillis: 1, Text: "synthetic"}}}, Backend: WorkerComponent{Name: "faster-whisper", Version: "1"}, Model: WorkerComponent{Name: "small.en", Version: "1"}}
	output, _ := json.Marshal(worker)
	timeout := newError(ErrorTimeout, "fake_process", true, "local transcription timed out", context.DeadlineExceeded)
	local, err := NewLocalBackend(LocalConfig{Runner: partialErrorRunner{output, timeout}, Discovery: fakeDiscovery{true, true, true, true}, Python: "python3", Worker: "worker.py", Clock: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	r := request(f)
	r.Job = f.job
	r.Backend = f.job.Backend
	r.Model = f.job.Model
	partial, err := local.Transcribe(context.Background(), r)
	if CodeOf(err) != ErrorTimeout || partial.Outcome != OutcomePartial {
		t.Fatalf("result=%+v err=%v", partial, err)
	}
	authority, _ := NewArtifactAuthority(f.paths, f.sessionRoot)
	store := NewArtifactStore(authority)
	if _, persistErr := store.Persist(context.Background(), f.job, partial, 1); CodeOf(persistErr) != ErrorPartialOutput {
		t.Fatalf("persist=%v", persistErr)
	}
	cancelled := newError(ErrorCancelled, "fake_process", true, "local transcription was cancelled", context.Canceled)
	local, _ = NewLocalBackend(LocalConfig{Runner: partialErrorRunner{output, cancelled}, Discovery: fakeDiscovery{true, true, true, true}, Python: "python3", Worker: "worker.py", Clock: func() time.Time { return f.now }})
	partial, err = local.Transcribe(context.Background(), r)
	if CodeOf(err) != ErrorCancelled || partial.Outcome != OutcomePartial {
		t.Fatalf("cancelled result=%+v err=%v", partial, err)
	}
}
func hasRecoveryIssue(i RecoveryInspection, code string) bool {
	for _, issue := range i.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func TestArtifactJSONDeterministicAndNoSecrets(t *testing.T) {
	f := newFixture(t)
	result := syntheticResult(t, f)
	a, _ := encodeDocument(TranscriptDocument{ArtifactSchemaVersion, f.job.ID.String(), f.job.SessionID, f.job.CaptureID, f.job.SegmentID, 1, result.Transcript, result.Artifacts.ProvenanceRelativePath})
	b, _ := encodeDocument(TranscriptDocument{ArtifactSchemaVersion, f.job.ID.String(), f.job.SessionID, f.job.CaptureID, f.job.SegmentID, 1, result.Transcript, result.Artifacts.ProvenanceRelativePath})
	if !reflect.DeepEqual(a, b) || strings.Contains(string(a), f.sessionRoot) {
		t.Fatal("artifact JSON is unstable or unsafe")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic wav bytes")))
	if result.Provenance.InputSHA256 != digest {
		t.Fatal("wrong source hash")
	}
}

func TestConservativeLocalDiscovery(t *testing.T) {
	root := t.TempDir()
	worker := filepath.Join(root, "worker.py")
	model := filepath.Join(root, "model")
	if err := os.WriteFile(worker, []byte("# synthetic"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(model, 0o750); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	discovery := LocalDiscovery{Runner: runner, PythonExecutable: "python3", ModelPaths: map[string]string{"faster-whisper/small.en": model}, ProbeTimeout: time.Second}
	ctx := context.Background()
	if !discovery.Python(ctx, "python3") || !discovery.Worker(ctx, worker) || !discovery.Package(ctx, "faster-whisper") || !discovery.Model(ctx, "faster-whisper/small.en") {
		t.Fatal("verified local components were not discovered")
	}
	if discovery.Package(ctx, "arbitrary-package") || discovery.Model(ctx, "missing") {
		t.Fatal("discovery fabricated availability")
	}
	linked := filepath.Join(root, "worker-link.py")
	if err := os.Symlink(worker, linked); err != nil {
		t.Fatal(err)
	}
	if discovery.Worker(ctx, linked) {
		t.Fatal("symlinked worker accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if discovery.Python(cancelled, "python3") {
		t.Fatal("cancelled discovery succeeded")
	}
	runner.lookupErr = errors.New("missing")
	if discovery.Python(ctx, "python3") {
		t.Fatal("missing executable reported available")
	}
}

package transcription

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const testSuffix = "0123456789abcdef0123456789abcdef"

func testJobID(t *testing.T) JobID {
	t.Helper()
	id, err := NewJobID(testSuffix)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func ptrFloat(v float64) *float64 { return &v }
func baseTranscript(partial bool) Transcript {
	return Transcript{Text: "synthetic transcript", Language: "en", DurationMillis: 1000, Partial: partial, Segments: []TranscriptSegment{{Index: 0, StartMillis: 0, EndMillis: 1000, Text: "synthetic", Confidence: ptrFloat(.9)}}, Words: []Word{{Index: 0, StartMillis: 0, EndMillis: 500, Text: "synthetic", Confidence: ptrFloat(.8)}}}
}
func baseArtifacts() TranscriptArtifacts {
	return TranscriptArtifacts{JSONRelativePath: "Transcripts/001-transcript.json", TextRelativePath: "Transcripts/001-transcript.txt", JobRelativePath: "Transcripts/001-transcription-job.json"}
}
func baseProvenance(t *testing.T) Provenance {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	return Provenance{JobID: testJobID(t), SessionID: "session-synthetic", CaptureID: "capture-synthetic", SegmentID: "segment-synthetic", InputRelativePath: "Segments/001-audio.wav", InputSHA256: strings.Repeat("a", 64), Backend: "synthetic", BackendVersion: "1", Model: "synthetic/small", ModelVersion: "1", RequestedAt: now, StartedAt: now.Add(time.Second), CompletedAt: now.Add(2 * time.Second), Parameters: map[string]string{"beam_size": "1"}}
}
func baseJob(t *testing.T) Job {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	return Job{ID: testJobID(t), SessionID: "session-synthetic", CaptureID: "capture-synthetic", SegmentID: "segment-synthetic", SegmentNumber: 1, InputRelativePath: "Segments/001-audio.wav", Backend: "synthetic", Model: "synthetic/small", Status: JobQueued, QueuedAt: now, UpdatedAt: now}
}

func TestJobIDIdentity(t *testing.T) {
	id := testJobID(t)
	if id.String() != "transcription-job-"+testSuffix {
		t.Fatal(id)
	}
	for _, value := range []string{"job-" + testSuffix, "transcription-job-short", "transcription-job-0123456789abcdef0123456789abcdeg"} {
		if _, err := ParseJobID(value); err == nil {
			t.Errorf("ParseJobID(%q) succeeded", value)
		}
	}
	sequence := 0
	generator := func() (JobID, error) {
		sequence++
		suffix := strings.Repeat("0", 31) + string("0123456789abcdef"[sequence])
		return NewJobID(suffix)
	}
	a, _ := generator()
	b, _ := generator()
	if a == b {
		t.Fatal("deterministic generator collided")
	}
	seen := map[JobID]bool{}
	for i := 0; i < 64; i++ {
		id, err := DefaultJobIDGenerator()
		if err != nil || seen[id] {
			t.Fatalf("generated ID %q: %v", id, err)
		}
		seen[id] = true
	}
}

func TestJobStatusTransitions(t *testing.T) {
	valid := [][2]JobStatus{{JobQueued, JobPreparing}, {JobPreparing, JobRunning}, {JobRunning, JobPartial}, {JobPartial, JobRunning}, {JobRunning, JobFinalizing}, {JobPartial, JobFinalizing}, {JobFinalizing, JobCompleted}, {JobQueued, JobCancelled}, {JobPreparing, JobCancelled}, {JobRunning, JobCancelled}, {JobPartial, JobCancelled}, {JobPreparing, JobFailed}, {JobRunning, JobFailed}, {JobPartial, JobFailed}, {JobFinalizing, JobFailed}}
	for _, pair := range valid {
		if !CanTransition(pair[0], pair[1]) {
			t.Errorf("transition %s -> %s rejected", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]JobStatus{{JobCompleted, JobRunning}, {JobCancelled, JobCompleted}, {JobFailed, JobCompleted}, {JobQueued, JobCompleted}} {
		if CanTransition(pair[0], pair[1]) {
			t.Errorf("invalid transition %s -> %s accepted", pair[0], pair[1])
		}
	}
	for _, s := range []JobStatus{JobCompleted, JobCancelled, JobFailed} {
		if !s.Terminal() {
			t.Errorf("%s not terminal", s)
		}
	}
}

func TestJobValidation(t *testing.T) {
	valid := baseJob(t)
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Job)
	}{
		{"segment number", func(j *Job) { j.SegmentNumber = 0 }},
		{"absolute input", func(j *Job) { j.InputRelativePath = "/private/audio.wav" }},
		{"partial input", func(j *Job) { j.InputRelativePath = "Segments/001-audio.wav.partial" }},
		{"missing backend", func(j *Job) { j.Backend = "" }},
		{"missing model", func(j *Job) { j.Model = "" }},
		{"contradictory timestamps", func(j *Job) { j.UpdatedAt = j.QueuedAt.Add(-time.Second) }},
		{"completed without transcript", func(j *Job) {
			started := j.QueuedAt
			completed := j.QueuedAt
			j.Status = JobCompleted
			j.StartedAt = &started
			j.CompletedAt = &completed
		}},
		{"failed without error", func(j *Job) { started := j.QueuedAt; j.Status = JobFailed; j.StartedAt = &started }},
		{"cancelled claims completion", func(j *Job) { completed := j.QueuedAt; j.Status = JobCancelled; j.CompletedAt = &completed }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			j := valid.Clone()
			test.mutate(&j)
			if err := j.Validate(); err == nil {
				t.Fatal("Validate succeeded")
			}
		})
	}
	completed := valid.Clone()
	started := completed.QueuedAt.Add(time.Second)
	done := started.Add(time.Second)
	completed.Status = JobCompleted
	completed.StartedAt = &started
	completed.CompletedAt = &done
	completed.UpdatedAt = done
	tr := baseTranscript(false)
	prov := baseProvenance(t)
	completed.Transcript = &tr
	completed.Provenance = &prov
	completed.Artifacts = baseArtifacts()
	if err := completed.Validate(); err != nil {
		t.Fatalf("completed job: %v", err)
	}
	withoutProvenance := completed.Clone()
	withoutProvenance.Provenance = nil
	if err := withoutProvenance.Validate(); err == nil {
		t.Fatal("completed job without provenance validated")
	}
	withoutArtifacts := completed.Clone()
	withoutArtifacts.Artifacts = TranscriptArtifacts{}
	if err := withoutArtifacts.Validate(); err == nil {
		t.Fatal("completed job without artifacts validated")
	}
	clone := completed.Clone()
	*clone.Transcript.Segments[0].Confidence = .1
	clone.Provenance.Parameters["beam_size"] = "9"
	if *completed.Transcript.Segments[0].Confidence == .1 || completed.Provenance.Parameters["beam_size"] == "9" {
		t.Fatal("job clone shares mutable state")
	}
}

func TestCapabilityAndModelValidation(t *testing.T) {
	model := Model{ID: "synthetic/small", Name: "Synthetic Small", Version: "1", Backend: "synthetic", Languages: []string{"en", "es"}, Installed: true, Available: true}
	ready := BackendCapability{Name: "synthetic", Status: CapabilityReady, Models: []Model{model}}
	if err := ready.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := ready.Clone()
	clone.Models[0].Languages[0] = "changed"
	if ready.Models[0].Languages[0] == "changed" {
		t.Fatal("capability clone shares languages")
	}
	for name, capability := range map[string]BackendCapability{
		"unavailable model":      {Name: "x", Status: CapabilityUnavailable, Models: []Model{{ID: "x/m", Name: "m", Backend: "x", Installed: true, Available: true}}},
		"ready without model":    {Name: "x", Status: CapabilityReady},
		"degraded without issue": {Name: "x", Status: CapabilityDegraded},
		"duplicate IDs":          {Name: "synthetic", Status: CapabilityReady, Models: []Model{model, model}},
		"unstable models":        {Name: "synthetic", Status: CapabilityReady, Models: []Model{{ID: "z", Name: "z", Backend: "synthetic", Installed: true, Available: true}, {ID: "a", Name: "a", Backend: "synthetic", Installed: true, Available: true}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := capability.Validate(); err == nil {
				t.Fatal("Validate succeeded")
			}
		})
	}
	notInstalled := model
	notInstalled.Installed = false
	if err := notInstalled.Validate(); err == nil {
		t.Fatal("available uninstalled model accepted")
	}
}

func TestTranscriptAndPartialValidation(t *testing.T) {
	for _, partial := range []bool{false, true} {
		tr := baseTranscript(partial)
		if err := tr.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name   string
		mutate func(*Transcript)
	}{
		{"negative duration", func(x *Transcript) { x.DurationMillis = -1 }},
		{"invalid segment timestamp", func(x *Transcript) { x.Segments[0].StartMillis = 2; x.Segments[0].EndMillis = 1 }},
		{"invalid index", func(x *Transcript) { x.Segments[0].Index = 2 }},
		{"invalid confidence", func(x *Transcript) { x.Words[0].Confidence = ptrFloat(1.1) }},
		{"segment beyond duration", func(x *Transcript) { x.Segments[0].EndMillis = 1001 }},
		{"nonmonotonic words", func(x *Transcript) { x.Words = append(x.Words, Word{Index: 1, StartMillis: 100, EndMillis: 200}) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tr := baseTranscript(false)
			test.mutate(&tr)
			if err := tr.Validate(); err == nil {
				t.Fatal("Validate succeeded")
			}
		})
	}
	tr := baseTranscript(false)
	clone := tr.Clone()
	*clone.Words[0].Confidence = 0
	if *tr.Words[0].Confidence == 0 {
		t.Fatal("transcript clone shares confidence")
	}
	update := PartialUpdate{JobID: testJobID(t), Transcript: baseTranscript(true), Sequence: 1, StableThroughMillis: 500}
	if err := update.Validate(); err != nil {
		t.Fatal(err)
	}
	update.Transcript.Partial = false
	if err := update.Validate(); err == nil {
		t.Fatal("final transcript accepted as partial")
	}
	update.Transcript.Partial = true
	update.Sequence = 0
	if err := update.Validate(); err == nil {
		t.Fatal("zero sequence accepted")
	}
	update.Sequence = 1
	update.StableThroughMillis = 1001
	if err := update.Validate(); err == nil {
		t.Fatal("stable-through beyond duration accepted")
	}
}

func TestProvenanceAndArtifacts(t *testing.T) {
	prov := baseProvenance(t)
	if err := prov.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := prov.Clone()
	clone.Parameters["beam_size"] = "2"
	if prov.Parameters["beam_size"] != "1" {
		t.Fatal("provenance clone shares map")
	}
	left := prov.Clone()
	left.Parameters = map[string]string{"zeta": "2", "alpha": "1"}
	right := prov.Clone()
	right.Parameters = map[string]string{"alpha": "1", "zeta": "2"}
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	if string(leftJSON) != string(rightJSON) {
		t.Fatal("provenance parameter serialization is not stable")
	}
	for name, mutate := range map[string]func(*Provenance){"hash": func(p *Provenance) { p.InputSHA256 = "bad" }, "absolute": func(p *Provenance) { p.InputRelativePath = "/private/audio.wav" }, "timestamps": func(p *Provenance) { p.StartedAt = p.RequestedAt.Add(-time.Second) }, "secret": func(p *Provenance) { p.Parameters["api_token"] = "private" }} {
		t.Run(name, func(t *testing.T) {
			p := baseProvenance(t)
			mutate(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("Validate succeeded")
			}
		})
	}
	artifacts := baseArtifacts()
	if err := artifacts.Validate(1, true); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*TranscriptArtifacts){"traversal": func(a *TranscriptArtifacts) { a.JSONRelativePath = "Transcripts/../private.json" }, "absolute": func(a *TranscriptArtifacts) { a.TextRelativePath = "/private/transcript.txt" }, "wrong root": func(a *TranscriptArtifacts) { a.JobRelativePath = "Other/001-transcription-job.json" }, "wrong number": func(a *TranscriptArtifacts) { a.JSONRelativePath = "Transcripts/002-transcript.json" }, "partial": func(a *TranscriptArtifacts) { a.TextRelativePath = "Transcripts/001-transcript.txt.partial" }} {
		t.Run(name, func(t *testing.T) {
			a := baseArtifacts()
			mutate(&a)
			if err := a.Validate(1, true); err == nil {
				t.Fatal("Validate succeeded")
			}
		})
	}
}

func TestSafeErrorContract(t *testing.T) {
	cause := errors.New("private backend detail")
	err := newError(ErrorUnavailable, "start", true, "transcription backend is unavailable", cause, testJobID(t))
	if !errors.Is(err, &Error{Code: ErrorUnavailable}) || !errors.Is(err, cause) {
		t.Fatal("error matching failed")
	}
	var typed *Error
	if !errors.As(err, &typed) || typed.Operation != "start" || !typed.Recoverable {
		t.Fatal("error fields unavailable")
	}
	if strings.Contains(err.Error(), cause.Error()) {
		t.Fatal("safe Error() exposed cause")
	}
}

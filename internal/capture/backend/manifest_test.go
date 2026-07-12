package backend

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

func validManifest() Manifest {
	started := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	stopped := started.Add(time.Second)
	return Manifest{
		SchemaVersion:  ManifestSchemaVersion,
		SegmentID:      "segment-test-1",
		CaptureID:      "capture-test-1",
		SessionID:      "session-test-1",
		Number:         1,
		Status:         string(studyruntime.SegmentStatusStopped),
		AudioFile:      "001-audio.wav",
		Format:         "pcm_s16le",
		SampleRate:     16000,
		Channels:       1,
		BitDepth:       16,
		StartedAt:      started,
		StoppedAt:      &stopped,
		DurationMillis: 1000,
		BytesWritten:   32000,
		Backend:        "synthetic",
	}
}

func TestManifestValidation(t *testing.T) {
	if err := validManifest().Validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"bad version", func(m *Manifest) { m.SchemaVersion = 2 }},
		{"bad segment id", func(m *Manifest) { m.SegmentID = "x" }},
		{"bad capture id", func(m *Manifest) { m.CaptureID = "x" }},
		{"bad session id", func(m *Manifest) { m.SessionID = "x" }},
		{"absolute audio", func(m *Manifest) { m.AudioFile = "/etc/passwd" }},
		{"nested audio", func(m *Manifest) { m.AudioFile = "sub/001.wav" }},
		{"finalized without stop", func(m *Manifest) { m.StoppedAt = nil }},
		{"finalized but partial", func(m *Manifest) { m.Partial = true }},
		{"negative bytes", func(m *Manifest) { m.BytesWritten = -1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := validManifest()
			test.mutate(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func TestWriteManifestAtomicAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, manifestName(1))
	if err := writeManifestAtomic(dir, path, validManifest()); err != nil {
		t.Fatal(err)
	}
	// No temporary file remains.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != manifestName(1) {
		t.Fatalf("unexpected directory contents: %v", entries)
	}
	loaded, err := readManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SegmentID != validManifest().SegmentID || loaded.Partial {
		t.Fatalf("loaded = %+v", loaded)
	}
}

func TestReadManifestRejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, manifestName(1))
	if err := os.WriteFile(path, []byte("{ not json"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := readManifest(path); err == nil {
		t.Fatal("malformed manifest accepted")
	}
}

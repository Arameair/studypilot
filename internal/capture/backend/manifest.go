package backend

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Arameair/studypilot/internal/capture"
	studyruntime "github.com/Arameair/studypilot/internal/runtime"
)

// ManifestSchemaVersion is the only supported segment manifest version.
const ManifestSchemaVersion = 1

// Manifest is the versioned, on-disk description of one segment. It references
// the audio file by relative name only and never carries an absolute private
// path. It is written atomically and is only marked finalized after the audio
// file has been finalized.
type Manifest struct {
	SchemaVersion int `json:"schema_version"`

	SegmentID string `json:"segment_id"`
	CaptureID string `json:"capture_id"`
	SessionID string `json:"session_id"`
	Number    int    `json:"number"`

	Status string `json:"status"`

	AudioFile string `json:"audio_file"`

	Format     string `json:"format"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	BitDepth   int    `json:"bit_depth"`

	StartedAt time.Time  `json:"started_at"`
	StoppedAt *time.Time `json:"stopped_at,omitempty"`

	DurationMillis int64 `json:"duration_millis"`
	BytesWritten   int64 `json:"bytes_written"`

	Backend string `json:"backend"`

	Partial     bool `json:"partial"`
	Recoverable bool `json:"recoverable"`
}

// Validate checks the manifest against the capture identity contracts and the
// segment status/timestamp rules. AudioFile must be a bare relative filename.
func (m Manifest) Validate() error {
	fail := func(message string) error {
		return newError(ErrorManifestFailed, "manifest", message, nil)
	}
	if m.SchemaVersion != ManifestSchemaVersion {
		return fail("unsupported manifest schema version")
	}
	if err := capture.ValidateSegmentID(m.SegmentID); err != nil {
		return fail("invalid segment id")
	}
	if err := capture.CaptureID(m.CaptureID).Validate(); err != nil {
		return fail("invalid capture id")
	}
	if !strings.HasPrefix(m.SessionID, "session-") || m.SessionID == "session-" {
		return fail("invalid session id")
	}
	if m.Number <= 0 {
		return fail("segment number must be positive")
	}
	status := studyruntime.SegmentStatus(m.Status)
	if !status.Valid() {
		return fail("invalid segment status")
	}
	if m.AudioFile == "" || strings.ContainsAny(m.AudioFile, `/\`) || filepath.IsAbs(m.AudioFile) || containsControlRune(m.AudioFile) {
		return fail("audio file must be a bare relative name")
	}
	if m.Format != (AudioFormat{}).Name() && m.Format != "pcm_s16le" {
		return fail("unsupported audio format")
	}
	if (AudioFormat{SampleRate: m.SampleRate, Channels: m.Channels, BitDepth: m.BitDepth}).Validate() != nil {
		return fail("invalid audio format parameters")
	}
	if m.StartedAt.IsZero() {
		return fail("manifest requires a start time")
	}
	if m.DurationMillis < 0 || m.BytesWritten < 0 {
		return fail("duration and bytes cannot be negative")
	}
	if strings.TrimSpace(m.Backend) == "" || containsControlRune(m.Backend) {
		return fail("manifest requires a safe backend name")
	}
	switch status {
	case studyruntime.SegmentStatusStopped:
		if m.StoppedAt == nil {
			return fail("finalized segment requires a stop time")
		}
		if m.Partial {
			return fail("finalized segment cannot be marked partial")
		}
	case studyruntime.SegmentStatusFailed:
		if !m.Partial {
			return fail("failed segment must be marked partial")
		}
	}
	if m.StoppedAt != nil && m.StoppedAt.Before(m.StartedAt) {
		return fail("stop precedes start")
	}
	return nil
}

// encode marshals the manifest with a trailing newline.
func (m Manifest) encode() ([]byte, error) {
	content, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, newError(ErrorManifestFailed, "manifest", "encode manifest", err)
	}
	return append(content, '\n'), nil
}

// writeManifestAtomic writes the manifest to path via a temporary file in the
// same directory, syncing the file and directory. The manifest must already be
// valid; a finalized manifest is only written after audio finalization.
func writeManifestAtomic(dir, path string, manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	content, err := manifest.encode()
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".studypilot-manifest-*")
	if err != nil {
		return newError(ErrorManifestFailed, "manifest", "create manifest temp", err)
	}
	temporaryPath := temporary.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o640); err != nil {
		_ = temporary.Close()
		return newError(ErrorManifestFailed, "manifest", "chmod manifest temp", err)
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return newError(ErrorManifestFailed, "manifest", "write manifest temp", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return newError(ErrorManifestFailed, "manifest", "sync manifest temp", err)
	}
	if err := temporary.Close(); err != nil {
		return newError(ErrorManifestFailed, "manifest", "close manifest temp", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return newError(ErrorManifestFailed, "manifest", "install manifest", err)
	}
	cleanup = false
	if err := syncDir(dir); err != nil {
		return newError(ErrorDurabilityUncertain, "manifest", "sync manifest directory", err)
	}
	return nil
}

// readManifest reads and decodes a manifest without validating status, so
// recovery can classify malformed and unsupported manifests distinctly.
func readManifest(path string) (Manifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return Manifest{}, newError(ErrorManifestFailed, "manifest", "malformed manifest json", err)
	}
	return manifest, nil
}

// syncDir fsyncs a directory so a rename within it is durable.
func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}

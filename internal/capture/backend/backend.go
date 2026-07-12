package backend

import (
	"context"
	"fmt"
	"time"

	"github.com/Arameair/studypilot/internal/capture"
)

// Backend is the low-level recording contract beneath the capture service. It
// operates on one explicit active-segment handle at a time and never mutates
// session status, writes runtime state, or depends on the application layer.
type Backend interface {
	Capabilities(ctx context.Context) (Capabilities, error)
	StartSegment(ctx context.Context, req StartSegmentRequest) (ActiveSegment, error)
	FinalizeSegment(ctx context.Context, active ActiveSegment) (FinalizedSegment, error)
	AbortSegment(ctx context.Context, active ActiveSegment) (PartialSegment, error)
	Inspect(ctx context.Context, sessionRoot string) (Inspection, error)
}

// Capabilities describes a backend's discovered support in UI-neutral terms
// reusing the capture package's status and device contracts.
type Capabilities struct {
	BackendName     string
	Status          capture.CapabilityStatus
	Devices         []capture.Device
	DefaultDeviceID string
	Format          AudioFormat
	Issues          []capture.CapabilityIssue
}

// Clone returns a deep copy so a shared capabilities value stays immutable.
func (c Capabilities) Clone() Capabilities {
	result := c
	result.Devices = append([]capture.Device(nil), c.Devices...)
	result.Issues = append([]capture.CapabilityIssue(nil), c.Issues...)
	return result
}

// StartSegmentRequest asks to begin recording one segment. SessionRoot is the
// absolute session directory to validate; SegmentID may be empty to generate a
// fresh one. Format defaults to DefaultFormat when zero.
type StartSegmentRequest struct {
	SessionRoot string
	SessionID   string
	CaptureID   capture.CaptureID
	SegmentID   string
	Number      int
	DeviceID    string
	Format      AudioFormat
}

// ActiveSegment is the safe handle for an in-progress recording. It carries no
// file descriptors, process handles, or absolute paths; the backend keeps that
// state in a private active-recording object keyed by segment ID.
type ActiveSegment struct {
	CaptureID    capture.CaptureID
	SegmentID    string
	SessionID    string
	Number       int
	DeviceID     string
	RelativePath string
	StartedAt    time.Time
	Backend      string
}

// FinalizedSegment is the result of finalizing a recording: the capture-level
// segment, its persisted manifest, and a durability warning when a post-rename
// sync was uncertain.
type FinalizedSegment struct {
	Segment           capture.Segment
	Manifest          Manifest
	DurabilityWarning bool
}

// PartialSegment is the result of aborting a recording: a partial audio file
// remains for inspection and is never presented as finalized.
type PartialSegment struct {
	SegmentID    string
	CaptureID    capture.CaptureID
	SessionID    string
	Number       int
	RelativePath string
	BytesWritten int64
	StartedAt    time.Time
	Backend      string
	Recoverable  bool
}

// segment file naming: zero-padded, fixed-width, sort-stable, and derived from
// the segment number. The filename is not identity.
func audioName(number int) string    { return fmt.Sprintf("%03d-audio.wav", number) }
func partialName(number int) string  { return fmt.Sprintf("%03d-audio.wav.partial", number) }
func manifestName(number int) string { return fmt.Sprintf("%03d-segment.json", number) }

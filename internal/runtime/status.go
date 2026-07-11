package runtime

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidStatus     = errors.New("invalid runtime status")
	ErrInvalidTransition = errors.New("invalid runtime transition")
	ErrInvalidSnapshot   = errors.New("invalid runtime snapshot")
)

type SessionStatus string

const (
	SessionStatusNone        SessionStatus = "none"
	SessionStatusPlanned     SessionStatus = "planned"
	SessionStatusActive      SessionStatus = "active"
	SessionStatusInterrupted SessionStatus = "interrupted"
	SessionStatusRecovering  SessionStatus = "recovering"
	SessionStatusCompleted   SessionStatus = "completed"
	SessionStatusAbandoned   SessionStatus = "abandoned"
)

type CaptureStatus string

const (
	CaptureStatusUnavailable CaptureStatus = "unavailable"
	CaptureStatusReady       CaptureStatus = "ready"
	CaptureStatusStarting    CaptureStatus = "starting"
	CaptureStatusRecording   CaptureStatus = "recording"
	CaptureStatusPausing     CaptureStatus = "pausing"
	CaptureStatusPaused      CaptureStatus = "paused"
	CaptureStatusResuming    CaptureStatus = "resuming"
	CaptureStatusStopping    CaptureStatus = "stopping"
	CaptureStatusStopped     CaptureStatus = "stopped"
	CaptureStatusFailed      CaptureStatus = "failed"
)

type TranscriptionStatus string

const (
	TranscriptionStatusNotStarted   TranscriptionStatus = "not_started"
	TranscriptionStatusQueued       TranscriptionStatus = "queued"
	TranscriptionStatusTranscribing TranscriptionStatus = "transcribing"
	TranscriptionStatusComplete     TranscriptionStatus = "complete"
	TranscriptionStatusPartial      TranscriptionStatus = "partial"
	TranscriptionStatusFailed       TranscriptionStatus = "failed"
)

type FilesystemStatus string

const (
	FilesystemStatusUnknown  FilesystemStatus = "unknown"
	FilesystemStatusPlanned  FilesystemStatus = "planned"
	FilesystemStatusApplying FilesystemStatus = "applying"
	FilesystemStatusReady    FilesystemStatus = "ready"
	FilesystemStatusConflict FilesystemStatus = "conflict"
	FilesystemStatusFailed   FilesystemStatus = "failed"
)

type PublicationStatus string

const (
	PublicationStatusPrivate   PublicationStatus = "private"
	PublicationStatusCandidate PublicationStatus = "candidate"
	PublicationStatusReviewing PublicationStatus = "reviewing"
	PublicationStatusApproved  PublicationStatus = "approved"
	PublicationStatusPublished PublicationStatus = "published"
	PublicationStatusRejected  PublicationStatus = "rejected"
)

type SegmentStatus string

const (
	SegmentStatusPending    SegmentStatus = "pending"
	SegmentStatusRecording  SegmentStatus = "recording"
	SegmentStatusFinalizing SegmentStatus = "finalizing"
	SegmentStatusStopped    SegmentStatus = "stopped"
	SegmentStatusFailed     SegmentStatus = "failed"
)

func (s SessionStatus) Valid() bool       { return contains(sessionStatuses, s) }
func (s CaptureStatus) Valid() bool       { return contains(captureStatuses, s) }
func (s TranscriptionStatus) Valid() bool { return contains(transcriptionStatuses, s) }
func (s FilesystemStatus) Valid() bool    { return contains(filesystemStatuses, s) }
func (s PublicationStatus) Valid() bool   { return contains(publicationStatuses, s) }
func (s SegmentStatus) Valid() bool       { return contains(segmentStatuses, s) }

func ParseSessionStatus(v string) (SessionStatus, error) { return parse(v, sessionStatuses) }
func ParseCaptureStatus(v string) (CaptureStatus, error) { return parse(v, captureStatuses) }
func ParseTranscriptionStatus(v string) (TranscriptionStatus, error) {
	return parse(v, transcriptionStatuses)
}
func ParseFilesystemStatus(v string) (FilesystemStatus, error) { return parse(v, filesystemStatuses) }
func ParsePublicationStatus(v string) (PublicationStatus, error) {
	return parse(v, publicationStatuses)
}
func ParseSegmentStatus(v string) (SegmentStatus, error) { return parse(v, segmentStatuses) }

var sessionStatuses = []SessionStatus{SessionStatusNone, SessionStatusPlanned, SessionStatusActive, SessionStatusInterrupted, SessionStatusRecovering, SessionStatusCompleted, SessionStatusAbandoned}
var captureStatuses = []CaptureStatus{CaptureStatusUnavailable, CaptureStatusReady, CaptureStatusStarting, CaptureStatusRecording, CaptureStatusPausing, CaptureStatusPaused, CaptureStatusResuming, CaptureStatusStopping, CaptureStatusStopped, CaptureStatusFailed}
var transcriptionStatuses = []TranscriptionStatus{TranscriptionStatusNotStarted, TranscriptionStatusQueued, TranscriptionStatusTranscribing, TranscriptionStatusComplete, TranscriptionStatusPartial, TranscriptionStatusFailed}
var filesystemStatuses = []FilesystemStatus{FilesystemStatusUnknown, FilesystemStatusPlanned, FilesystemStatusApplying, FilesystemStatusReady, FilesystemStatusConflict, FilesystemStatusFailed}
var publicationStatuses = []PublicationStatus{PublicationStatusPrivate, PublicationStatusCandidate, PublicationStatusReviewing, PublicationStatusApproved, PublicationStatusPublished, PublicationStatusRejected}
var segmentStatuses = []SegmentStatus{SegmentStatusPending, SegmentStatusRecording, SegmentStatusFinalizing, SegmentStatusStopped, SegmentStatusFailed}

func contains[T comparable](values []T, value T) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func parse[T ~string](value string, valid []T) (T, error) {
	parsed := T(value)
	if contains(valid, parsed) {
		return parsed, nil
	}
	var zero T
	return zero, fmt.Errorf("%w: %q", ErrInvalidStatus, value)
}

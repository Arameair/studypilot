package runtime

import "fmt"

func CanTransitionSession(from, to SessionStatus) bool {
	return from.Valid() && to.Valid() && canTransition(from, to, sessionTransitions)
}
func CanTransitionCapture(from, to CaptureStatus) bool {
	return from.Valid() && to.Valid() && canTransition(from, to, captureTransitions)
}
func CanTransitionTranscription(from, to TranscriptionStatus) bool {
	return from.Valid() && to.Valid() && canTransition(from, to, transcriptionTransitions)
}
func CanTransitionFilesystem(from, to FilesystemStatus) bool {
	return from.Valid() && to.Valid() && canTransition(from, to, filesystemTransitions)
}
func CanTransitionPublication(from, to PublicationStatus) bool {
	return from.Valid() && to.Valid() && canTransition(from, to, publicationTransitions)
}

func ValidateSessionTransition(from, to SessionStatus) error {
	return validateTransition(from, to, from.Valid(), to.Valid(), CanTransitionSession(from, to))
}
func ValidateCaptureTransition(from, to CaptureStatus) error {
	return validateTransition(from, to, from.Valid(), to.Valid(), CanTransitionCapture(from, to))
}
func ValidateTranscriptionTransition(from, to TranscriptionStatus) error {
	return validateTransition(from, to, from.Valid(), to.Valid(), CanTransitionTranscription(from, to))
}
func ValidateFilesystemTransition(from, to FilesystemStatus) error {
	return validateTransition(from, to, from.Valid(), to.Valid(), CanTransitionFilesystem(from, to))
}
func ValidatePublicationTransition(from, to PublicationStatus) error {
	return validateTransition(from, to, from.Valid(), to.Valid(), CanTransitionPublication(from, to))
}

func canTransition[T comparable](from, to T, transitions map[T][]T) bool {
	if from == to {
		return true
	}
	return contains(transitions[from], to)
}

func validateTransition[T ~string](from, to T, fromValid, toValid, allowed bool) error {
	if !fromValid || !toValid {
		return fmt.Errorf("%w: %q -> %q", ErrInvalidStatus, from, to)
	}
	if !allowed {
		return fmt.Errorf("%w: %q -> %q", ErrInvalidTransition, from, to)
	}
	return nil
}

var sessionTransitions = map[SessionStatus][]SessionStatus{
	SessionStatusNone:        {SessionStatusPlanned},
	SessionStatusPlanned:     {SessionStatusActive, SessionStatusAbandoned},
	SessionStatusActive:      {SessionStatusInterrupted, SessionStatusCompleted, SessionStatusAbandoned},
	SessionStatusInterrupted: {SessionStatusActive, SessionStatusRecovering, SessionStatusCompleted, SessionStatusAbandoned},
	SessionStatusRecovering:  {SessionStatusActive, SessionStatusInterrupted, SessionStatusAbandoned},
}

var captureTransitions = map[CaptureStatus][]CaptureStatus{
	CaptureStatusUnavailable: {CaptureStatusReady, CaptureStatusFailed},
	CaptureStatusReady:       {CaptureStatusStarting, CaptureStatusUnavailable},
	CaptureStatusStarting:    {CaptureStatusRecording, CaptureStatusFailed},
	CaptureStatusRecording:   {CaptureStatusPausing, CaptureStatusStopping, CaptureStatusFailed},
	CaptureStatusPausing:     {CaptureStatusPaused, CaptureStatusFailed},
	CaptureStatusPaused:      {CaptureStatusResuming, CaptureStatusStopping, CaptureStatusFailed},
	CaptureStatusResuming:    {CaptureStatusRecording, CaptureStatusFailed},
	CaptureStatusStopping:    {CaptureStatusStopped, CaptureStatusFailed},
	CaptureStatusStopped:     {CaptureStatusStarting, CaptureStatusReady, CaptureStatusUnavailable},
	CaptureStatusFailed:      {CaptureStatusReady, CaptureStatusUnavailable},
}

var transcriptionTransitions = map[TranscriptionStatus][]TranscriptionStatus{
	TranscriptionStatusNotStarted:   {TranscriptionStatusQueued},
	TranscriptionStatusQueued:       {TranscriptionStatusTranscribing, TranscriptionStatusFailed},
	TranscriptionStatusTranscribing: {TranscriptionStatusComplete, TranscriptionStatusPartial, TranscriptionStatusFailed},
	TranscriptionStatusPartial:      {TranscriptionStatusQueued, TranscriptionStatusTranscribing, TranscriptionStatusComplete, TranscriptionStatusFailed},
	TranscriptionStatusFailed:       {TranscriptionStatusQueued},
}

var filesystemTransitions = map[FilesystemStatus][]FilesystemStatus{
	FilesystemStatusUnknown:  {FilesystemStatusPlanned, FilesystemStatusReady, FilesystemStatusConflict, FilesystemStatusFailed},
	FilesystemStatusPlanned:  {FilesystemStatusApplying, FilesystemStatusConflict, FilesystemStatusFailed},
	FilesystemStatusApplying: {FilesystemStatusReady, FilesystemStatusConflict, FilesystemStatusFailed},
	FilesystemStatusConflict: {FilesystemStatusPlanned, FilesystemStatusFailed},
	FilesystemStatusFailed:   {FilesystemStatusPlanned},
}

var publicationTransitions = map[PublicationStatus][]PublicationStatus{
	PublicationStatusPrivate:   {PublicationStatusCandidate},
	PublicationStatusCandidate: {PublicationStatusReviewing, PublicationStatusPrivate},
	PublicationStatusReviewing: {PublicationStatusApproved, PublicationStatusRejected, PublicationStatusPrivate},
	PublicationStatusApproved:  {PublicationStatusPublished, PublicationStatusReviewing, PublicationStatusPrivate},
	PublicationStatusRejected:  {PublicationStatusCandidate, PublicationStatusPrivate},
	PublicationStatusPublished: {PublicationStatusReviewing},
}

package application

import (
	"github.com/Arameair/studypilot/internal/transcription"
	"github.com/Arameair/studypilot/internal/workspace"
)

// TranscriptionService is the application-facing transcription domain seam.
// The application coordinates it with the queue and session repository.
type TranscriptionService = transcription.Service

// TranscriptionQueue is the application composition seam for logical
// scheduling. Its default implementation remains in-memory.
type TranscriptionQueue = transcription.Queue

type TranscriptionQueueFactory func(workspace.Paths, transcription.Clock, transcription.JobIDGenerator) (transcription.Queue, error)
type TranscriptionServiceFactory func(workspace.Paths) (transcription.Service, error)

func defaultTranscriptionQueueFactory(_ workspace.Paths, clock transcription.Clock, generate transcription.JobIDGenerator) (transcription.Queue, error) {
	return transcription.NewMemoryQueue(transcription.MemoryQueueConfig{Clock: clock, GenerateJobID: generate})
}
func defaultTranscriptionServiceFactory(workspace.Paths) (transcription.Service, error) {
	return transcription.UnavailableService{BackendName: "unavailable"}, nil
}

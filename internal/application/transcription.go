package application

import "github.com/Arameair/studypilot/internal/transcription"

// TranscriptionService is the application-facing transcription domain seam.
// Orchestration, persistence, runtime mapping, and adapters are intentionally
// deferred; this alias introduces no behavior.
type TranscriptionService = transcription.Service

// TranscriptionQueue is the future application composition seam for logical
// scheduling. No application use cases or persistence are implemented here.
type TranscriptionQueue = transcription.Queue

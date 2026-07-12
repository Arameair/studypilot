package application

import "github.com/Arameair/studypilot/internal/transcription"

// TranscriptionService is the application-facing transcription domain seam.
// Orchestration, persistence, runtime mapping, and adapters are intentionally
// deferred; this alias introduces no behavior.
type TranscriptionService = transcription.Service

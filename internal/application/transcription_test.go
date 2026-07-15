package application

import "github.com/Arameair/studypilot/internal/transcription"

var _ TranscriptionService = transcription.UnavailableService{}
var _ TranscriptionQueue = (*transcription.MemoryQueue)(nil)

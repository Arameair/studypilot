package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectContinuityDocuments(t *testing.T) {
	required := map[string][]string{
		"PROJECT_STATUS.md":                    {"# Project Status", "## Current Milestone", "## Next Approved Milestone", "## Privacy"},
		"ROADMAP.md":                           {"# Roadmap", "## Phase 8", "## Phase 9", "Capture Application and CLI Integration"},
		"DEVELOPMENT_HANDOFF.md":               {"# Development Handoff", "## Architecture Rules", "## Session Stash Warning", "## Next Safe Action"},
		"session-repository.md":                {"# Session Operational Repository", "## Immutable and mutable state", "## Recovery and discovery", "## Current exclusions and next step"},
		"session-lifecycle.md":                 {"# Session Lifecycle Application Services", "## Allowed session transitions", "## Explicit completion", "## Capture independence"},
		"session-cli.md":                       {"# Session CLI Adapter", "## Revision workflow", "## Strict writes versus tolerant reads", "## Exit codes", "## Current exclusions"},
		"capture-contracts.md":                 {"# Capture Service Contracts", "## Scope", "## Runtime snapshot mapping", "## Session independence", "## Current exclusions", "## Next milestone"},
		"recording-backend.md":                 {"# Recording Backend", "## Synthetic backend", "## Pause and resume invariants", "## Durability order", "## Recovery inspection", "## Current exclusions", "## Next milestone"},
		"capture-cli.md":                       {"# Capture Application and CLI Integration", "## Architecture", "## Persistence and uncertain outcomes", "## Current exclusions and next milestone"},
		"transcription-contracts.md":           {"# Core Transcription Domain Contracts", "## Architecture", "## Status lifecycle", "## Provenance", "## Current exclusions and next milestone"},
		"transcription-queue.md":               {"# Transcription Queue, Retry, and Reconciliation Contracts", "## Queue architecture", "## Queue status and job status", "## Retry policy and backoff", "## Inspection and reconciliation", "## Current exclusions and next milestone"},
		"transcription-runtime.md":             {"# Transcription Runtime and Application Integration", "## Runtime schema", "## Per-segment state and aggregate policy", "## Pure mapping behavior", "## Application orchestration", "## Revision control and persistence uncertainty", "## Queue/runtime mismatch and inspection", "## Restart limitation", "## Current exclusions and next milestone"},
		"transcription-backend.md":             {"# Local Transcription Backend and Durable Artifact Persistence", "## Backend architecture", "## Synthetic backend", "## Local process boundary", "## Python/faster-whisper protocol", "## Capability discovery", "## Artifact authority and layout", "## Durability order", "## Recovery inspection", "## Privacy boundary", "## Current limitations and next milestone"},
		"transcription-execution.md":           {"# Transcription Execution Orchestration and CLI", "## Execution architecture", "## Synchronous one-job flow", "## Revision progression", "## Artifact completion boundary", "## Failure and uncertainty semantics", "## In-memory queue process limitation", "## Inspection and recovery", "## Current exclusions and next milestone"},
		"transcription-workflow-validation.md": {"# End-to-End Transcription Workflow Validation", "## Validation scope", "## Synthetic workflow", "## Real workflow", "## Source-audio integrity", "## Artifact assertions", "## Restart behavior", "## Failure scenarios", "## Exact command sequence", "## Final validation status"},
		"study-artifacts.md":                   {"# Study Artifact Organization", "## Artifact concepts", "## Managed layout", "## Transcript authority", "## Note format", "## Note templates", "## Asset registration", "## Artifact index", "## Revisions", "## Discovery and refresh", "## Reconciliation issues", "## Failure and uncertainty behavior", "## CLI commands", "## Privacy boundary", "## Current exclusions", "## Next milestone"},
		"gui-architecture.md":                  {"# Initial Local GUI Architecture", "## Dependency boundary", "## Loopback-only server", "## Embedded frontend", "## UI read models", "## Frontend workflow", "## Capture and transcription lifecycle", "## Shutdown behavior", "## Current exclusions", "## Next milestone"},
		"http-api.md":                          {"# Local HTTP API", "## Version and transport", "## Endpoints", "## Request validation", "## Error contract", "## DTO privacy boundary", "## Revision and conflict handling", "## Security headers and origin policy", "## Synchronous transcription", "## Shutdown and cancellation", "## Current exclusions and next milestone"},
		"gui-workflow.md":                      {"# Minimal Session and Capture GUI Workflow", "## Complete workflow", "## Course, module, and session navigation", "## Control eligibility", "## Capture semantics", "## Transcription experience", "## Notes and artifacts", "## Loading, confirmation, and errors", "## Revision conflict recovery", "## Browser refresh and restart continuity", "## Validation harness", "## Current limitations", "## Next milestone"},
	}
	for name, headings := range required {
		content, err := os.ReadFile(filepath.Join("..", "..", "docs", name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, heading := range headings {
			if !strings.Contains(string(content), heading) {
				t.Errorf("%s missing %q", name, heading)
			}
		}
	}
}

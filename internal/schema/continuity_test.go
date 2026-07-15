package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectContinuityDocuments(t *testing.T) {
	required := map[string][]string{
		"PROJECT_STATUS.md":          {"# Project Status", "## Current Milestone", "## Next Approved Milestone", "## Privacy"},
		"ROADMAP.md":                 {"# Roadmap", "## Phase 8", "## Phase 9", "Capture Application and CLI Integration"},
		"DEVELOPMENT_HANDOFF.md":     {"# Development Handoff", "## Architecture Rules", "## Session Stash Warning", "## Next Safe Action"},
		"session-repository.md":      {"# Session Operational Repository", "## Immutable and mutable state", "## Recovery and discovery", "## Current exclusions and next step"},
		"session-lifecycle.md":       {"# Session Lifecycle Application Services", "## Allowed session transitions", "## Explicit completion", "## Capture independence"},
		"session-cli.md":             {"# Session CLI Adapter", "## Revision workflow", "## Strict writes versus tolerant reads", "## Exit codes", "## Current exclusions"},
		"capture-contracts.md":       {"# Capture Service Contracts", "## Scope", "## Runtime snapshot mapping", "## Session independence", "## Current exclusions", "## Next milestone"},
		"recording-backend.md":       {"# Recording Backend", "## Synthetic backend", "## Pause and resume invariants", "## Durability order", "## Recovery inspection", "## Current exclusions", "## Next milestone"},
		"capture-cli.md":             {"# Capture Application and CLI Integration", "## Architecture", "## Persistence and uncertain outcomes", "## Current exclusions and next milestone"},
		"transcription-contracts.md": {"# Core Transcription Domain Contracts", "## Architecture", "## Status lifecycle", "## Provenance", "## Current exclusions and next milestone"},
		"transcription-queue.md":     {"# Transcription Queue, Retry, and Reconciliation Contracts", "## Queue architecture", "## Queue status and job status", "## Retry policy and backoff", "## Inspection and reconciliation", "## Current exclusions and next milestone"},
		"transcription-runtime.md":   {"# Transcription Runtime and Application Integration", "## Runtime schema", "## Per-segment state and aggregate policy", "## Pure mapping behavior", "## Application orchestration", "## Revision control and persistence uncertainty", "## Queue/runtime mismatch and inspection", "## Restart limitation", "## Current exclusions and next milestone"},
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

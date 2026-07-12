package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectContinuityDocuments(t *testing.T) {
	required := map[string][]string{
		"PROJECT_STATUS.md":      {"# Project Status", "## Current Milestone", "## Next Approved Milestone", "## Privacy"},
		"ROADMAP.md":             {"# Roadmap", "## Phase 8", "## Phase 9", "Recording Segment Lifecycle and Local Audio Backend"},
		"DEVELOPMENT_HANDOFF.md": {"# Development Handoff", "## Architecture Rules", "## Session Stash Warning", "## Next Safe Action"},
		"session-repository.md":  {"# Session Operational Repository", "## Immutable and mutable state", "## Recovery and discovery", "## Current exclusions and next step"},
		"session-lifecycle.md":   {"# Session Lifecycle Application Services", "## Allowed session transitions", "## Explicit completion", "## Capture independence"},
		"session-cli.md":         {"# Session CLI Adapter", "## Revision workflow", "## Strict writes versus tolerant reads", "## Exit codes", "## Current exclusions"},
		"capture-contracts.md":   {"# Capture Service Contracts", "## Scope", "## Runtime snapshot mapping", "## Session independence", "## Current exclusions", "## Next milestone"},
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

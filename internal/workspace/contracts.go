package workspace

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// VaultKind identifies one of StudyPilot's supported vault contracts.
type VaultKind string

const (
	VaultPrivate   VaultKind = "private"
	VaultPortfolio VaultKind = "portfolio"
)

// RequiredFile describes a file required by a vault contract.
type RequiredFile struct {
	Path    string
	Content string
}

// VaultContract defines the required relative paths for a kind of vault.
type VaultContract struct {
	Kind        VaultKind
	Directories []string
	Files       []RequiredFile
}

// PrivateVaultContract returns the machine-readable private vault contract.
func PrivateVaultContract() VaultContract {
	return VaultContract{
		Kind: VaultPrivate,
		Directories: []string{
			"00 Dashboard", "01 Courses", "02 Study", "03 Draft Knowledge",
			"04 Personal", "Templates", ".obsidian", ".studypilot",
		},
		Files: []RequiredFile{
			{Path: "README.md", Content: `# Private Learning Vault

This repository is permanently private. It may contain paid-course transcripts,
paid-course assets, recordings, personal notes, and other sensitive learning
records. It must never be made public.

Public material must be created through a separate review process. It must be
rewritten, verified, and explicitly approved as a new artifact.
`},
			{Path: ".gitignore", Content: ".DS_Store\n*.tmp\n"},
		},
	}
}

// PublicPortfolioContract returns the machine-readable public portfolio contract.
func PublicPortfolioContract() VaultContract {
	return VaultContract{
		Kind: VaultPortfolio,
		Directories: []string{
			"00 Portfolio Index", "01 Projects", "02 Procedures",
			"03 Troubleshooting", "04 Concepts", "05 Labs",
			"06 Professional Development", "Templates", "assets", ".obsidian",
		},
		Files: []RequiredFile{
			{Path: "README.md", Content: `# Public IT Knowledge Portfolio

This public portfolio contains reviewed, employer-facing technical knowledge.
Every artifact must comply with PUBLICATION-POLICY.md.
`},
			{Path: "PUBLICATION-POLICY.md", Content: `# Publication Policy

Only newly written, technically verified, privacy-reviewed material may be
published after explicit human approval. Never copy transcripts, recordings,
paid-course assets, or other raw private files into this portfolio.
`},
			{Path: ".gitignore", Content: ".DS_Store\n*.tmp\n"},
		},
	}
}

// Validate checks a vault contract without accessing the filesystem.
func (c VaultContract) Validate() error {
	if c.Kind != VaultPrivate && c.Kind != VaultPortfolio {
		return fmt.Errorf("unknown vault kind %q", c.Kind)
	}

	directories := make(map[string]struct{}, len(c.Directories))
	for _, path := range c.Directories {
		normalized, err := validateContractPath(path)
		if err != nil {
			return fmt.Errorf("invalid directory path %q: %w", path, err)
		}
		if _, exists := directories[normalized]; exists {
			return fmt.Errorf("duplicate directory path %q", path)
		}
		directories[normalized] = struct{}{}
	}

	files := make(map[string]struct{}, len(c.Files))
	for _, file := range c.Files {
		normalized, err := validateContractPath(file.Path)
		if err != nil {
			return fmt.Errorf("invalid file path %q: %w", file.Path, err)
		}
		if _, exists := files[normalized]; exists {
			return fmt.Errorf("duplicate file path %q", file.Path)
		}
		if _, exists := directories[normalized]; exists {
			return fmt.Errorf("path %q is both a directory and a file", file.Path)
		}
		files[normalized] = struct{}{}
	}

	if _, exists := directories[".obsidian"]; !exists {
		return errors.New("contract must include .obsidian")
	}

	switch c.Kind {
	case VaultPrivate:
		if _, exists := directories[".studypilot"]; !exists {
			return errors.New("private contract must include .studypilot")
		}
		if _, exists := files["README.md"]; !exists {
			return errors.New("private contract must include README.md")
		}
	case VaultPortfolio:
		if _, exists := directories[".studypilot"]; exists {
			return errors.New("public contract must not include .studypilot")
		}
		if _, exists := files["PUBLICATION-POLICY.md"]; !exists {
			return errors.New("public contract must include PUBLICATION-POLICY.md")
		}
		for directory := range directories {
			for _, component := range strings.Split(filepath.ToSlash(directory), "/") {
				if prohibitedPublicDirectory(component) {
					return fmt.Errorf("public contract contains prohibited directory %q", directory)
				}
			}
		}
	}

	return nil
}

func validateContractPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path must not be empty")
	}
	if filepath.IsAbs(path) {
		return "", errors.New("path must be relative")
	}
	if containsParentTraversal(path) {
		return "", errors.New("path must not contain parent traversal")
	}
	return filepath.ToSlash(filepath.Clean(path)), nil
}

func prohibitedPublicDirectory(name string) bool {
	switch strings.ToLower(name) {
	case "transcript", "transcripts", "recording", "recordings", "audio", "course materials":
		return true
	default:
		return false
	}
}

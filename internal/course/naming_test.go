package course

import (
	"errors"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantDisplay string
		wantSlug    string
		wantErr     bool
	}{
		{name: "normal", input: "TCM Practical Help Desk", wantDisplay: "TCM Practical Help Desk", wantSlug: "tcm-practical-help-desk"},
		{name: "spaces", input: "  Windows   Services  ", wantDisplay: "Windows Services", wantSlug: "windows-services"},
		{name: "underscores", input: "Active_Directory", wantDisplay: "Active_Directory", wantSlug: "active-directory"},
		{name: "punctuation", input: "PowerShell: Users & Groups!", wantDisplay: "PowerShell Users & Groups!", wantSlug: "powershell-users-groups"},
		{name: "unicode", input: "Réseau et Sécurité", wantDisplay: "Réseau et Sécurité", wantSlug: "réseau-et-sécurité"},
		{name: "duplicate hyphens", input: "Windows---Help -- Desk", wantDisplay: "Windows---Help -- Desk", wantSlug: "windows-help-desk"},
		{name: "empty", input: " ", wantErr: true},
		{name: "dot", input: ".", wantErr: true},
		{name: "dot dot", input: "..", wantErr: true},
		{name: "parent sequence", input: "Help..Desk", wantErr: true},
		{name: "forward separator", input: "Course/Module", wantErr: true},
		{name: "back separator", input: `Course\Module`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeName(test.input)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidName) {
					t.Fatalf("normalizeName() error = %v, want ErrInvalidName", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeName() error = %v", err)
			}
			if got.Display != test.wantDisplay || got.Slug != test.wantSlug {
				t.Errorf("normalizeName() = %#v, want display %q slug %q", got, test.wantDisplay, test.wantSlug)
			}
		})
	}
}

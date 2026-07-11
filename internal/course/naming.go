// Package course defines StudyPilot's private course and module structures.
package course

import (
	"errors"
	"strings"
	"unicode"
)

var (
	ErrInvalidName         = errors.New("invalid name")
	ErrInvalidModuleNumber = errors.New("invalid module number")
	ErrMissingPrivateVault = errors.New("private course directory is unavailable")
	ErrMissingCourse       = errors.New("course is unavailable")
)

type normalizedName struct {
	Display string
	Slug    string
}

func normalizeName(value string) (normalizedName, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." ||
		strings.Contains(value, "..") || strings.ContainsAny(value, `/\`) {
		return normalizedName{}, ErrInvalidName
	}

	var display strings.Builder
	lastSpace := false
	for _, r := range value {
		if unicode.IsSpace(r) {
			if display.Len() != 0 && !lastSpace {
				display.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"|?*#`, r) {
			continue
		}
		display.WriteRune(r)
		lastSpace = false
	}
	displayName := strings.Trim(strings.TrimSpace(display.String()), ".")
	if displayName == "" || displayName == "." || displayName == ".." {
		return normalizedName{}, ErrInvalidName
	}

	slug := slugify(displayName)
	if slug == "" || slug == "." || slug == ".." {
		return normalizedName{}, ErrInvalidName
	}
	return normalizedName{Display: displayName, Slug: slug}, nil
}

func slugify(value string) string {
	var slug strings.Builder
	hyphenPending := false
	for _, r := range strings.ToLower(value) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if hyphenPending && slug.Len() != 0 {
				slug.WriteByte('-')
			}
			slug.WriteRune(r)
			hyphenPending = false
		case unicode.IsSpace(r) || r == '_' || r == '-':
			hyphenPending = slug.Len() != 0
		}
	}
	return strings.Trim(slug.String(), "-")
}

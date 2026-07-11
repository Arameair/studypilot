// Package course defines StudyPilot's private course and module structures.
package course

import (
	"errors"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	ErrInvalidName         = errors.New("invalid name")
	ErrInvalidModuleNumber = errors.New("invalid module number")
	ErrMissingPrivateVault = errors.New("private course directory is unavailable")
	ErrMissingCourse       = errors.New("course is unavailable")
	ErrCollision           = errors.New("identity collision")
	ErrAmbiguous           = errors.New("ambiguous identity")
	ErrUnmanagedDirectory  = errors.New("unmanaged directory")
	ErrMalformedMetadata   = errors.New("malformed metadata")
)

type normalizedName struct {
	Display string
	Slug    string
	Key     string
}

func normalizeName(value string) (normalizedName, error) {
	value = norm.NFC.String(strings.TrimSpace(value))
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\`) {
		return normalizedName{}, ErrInvalidName
	}
	for _, r := range value {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"|?*#`, r) {
			return normalizedName{}, ErrInvalidName
		}
	}
	display := strings.Join(strings.Fields(value), " ")
	if display == "" {
		return normalizedName{}, ErrInvalidName
	}
	slug := slugify(display)
	if slug == "" {
		return normalizedName{}, ErrInvalidName
	}
	return normalizedName{Display: display, Slug: slug, Key: strings.ToLower(display)}, nil
}

func slugify(value string) string {
	var slug strings.Builder
	hyphenPending := false
	for _, r := range strings.ToLower(norm.NFC.String(value)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if hyphenPending && slug.Len() != 0 {
				slug.WriteByte('-')
			}
			slug.WriteRune(r)
			hyphenPending = false
		case unicode.IsSpace(r) || r == '_' || r == '-' || unicode.IsPunct(r) || unicode.IsSymbol(r):
			hyphenPending = slug.Len() != 0
		}
	}
	return strings.Trim(slug.String(), "-")
}

package capture

import (
	"crypto/rand"
	"encoding/hex"
	"path"
	"strings"
	"unicode"
)

// CaptureID identifies one capture instance: the span from a successful start
// to its final stop or failure. It is independent from session IDs, segment
// IDs, filenames, process IDs, and device IDs, and is never derived from
// session titles, dates, or device names.
type CaptureID string

const (
	captureIDPrefix = "capture-"
	segmentIDPrefix = "segment-"
	sessionIDPrefix = "session-"
	maxIDLength     = 128
)

// CaptureIDGenerator produces new capture IDs; tests inject deterministic
// generators.
type CaptureIDGenerator func() (CaptureID, error)

// SegmentIDGenerator produces new segment IDs; tests inject deterministic
// generators.
type SegmentIDGenerator func() (string, error)

// NewCaptureID returns a collision-resistant random capture ID.
func NewCaptureID() (CaptureID, error) {
	suffix, err := randomSuffix()
	if err != nil {
		return "", err
	}
	return CaptureID(captureIDPrefix + suffix), nil
}

// NewSegmentID returns a collision-resistant random segment ID.
func NewSegmentID() (string, error) {
	suffix, err := randomSuffix()
	if err != nil {
		return "", err
	}
	return segmentIDPrefix + suffix, nil
}

func randomSuffix() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

// Validate rejects capture IDs without the canonical prefix or with unsafe or
// empty suffixes.
func (id CaptureID) Validate() error {
	if err := validatePrefixedID(string(id), captureIDPrefix); err != nil {
		return NewError(ErrorInvalidRequest, "", false, OutcomeNotStarted, "invalid capture id", nil)
	}
	return nil
}

// ValidateSegmentID rejects segment IDs without the canonical prefix or with
// unsafe or empty suffixes.
func ValidateSegmentID(id string) error {
	if err := validatePrefixedID(id, segmentIDPrefix); err != nil {
		return NewError(ErrorInvalidRequest, "", false, OutcomeNotStarted, "invalid segment id", nil)
	}
	return nil
}

func validSessionID(id string) bool {
	return validatePrefixedID(id, sessionIDPrefix) == nil
}

func validatePrefixedID(id, prefix string) error {
	if !strings.HasPrefix(id, prefix) || len(id) == len(prefix) || len(id) > maxIDLength {
		return errInvalidID
	}
	if containsControl(id) || strings.ContainsAny(id, " \t/\\") {
		return errInvalidID
	}
	return nil
}

var errInvalidID = NewError(ErrorInvalidRequest, "", false, OutcomeNotStarted, "invalid identifier", nil)

func containsControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// validateRelativePath accepts only descriptive, slash-separated relative
// paths that stay beneath the session directory: no absolute paths, no parent
// traversal, no backslashes, and no control characters. Paths are descriptive,
// not identity; no file is created from them in this milestone.
func validateRelativePath(value string) error {
	invalid := NewError(ErrorInvalidRequest, "", false, OutcomeNotStarted, "invalid relative segment path", nil)
	if value == "" || len(value) > 512 || containsControl(value) || strings.Contains(value, "\\") {
		return invalid
	}
	if path.IsAbs(value) {
		return invalid
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return invalid
	}
	return nil
}

package transcription

import (
	"path"
	"strconv"
	"strings"
)

func validateRelative(value, requiredRoot string) error {
	if value == "" || isAbsoluteLike(value) || strings.Contains(value, `\`) {
		return newError(ErrorInvalidInput, "validate_path", false, "path must be safe and relative", nil, "")
	}
	clean := path.Clean(value)
	if clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return newError(ErrorInvalidInput, "validate_path", false, "path must be safe and relative", nil, "")
	}
	if requiredRoot != "" && !strings.HasPrefix(clean, requiredRoot+"/") {
		return newError(ErrorInvalidInput, "validate_path", false, "path is outside its managed artifact directory", nil, "")
	}
	return nil
}

func isAbsoluteLike(value string) bool {
	normalized := strings.ReplaceAll(value, `\`, "/")
	return strings.HasPrefix(normalized, "/") || strings.HasPrefix(normalized, "//") || (len(normalized) > 1 && normalized[1] == ':')
}

func containsAbsoluteLike(value string) bool {
	for _, field := range strings.Fields(value) {
		if isAbsoluteLike(strings.Trim(field, `"'(),;`)) {
			return true
		}
	}
	return false
}

func expectedSegmentPrefix(number int) string { return leftPad3(number) + "-" }
func leftPad3(number int) string {
	s := strconv.Itoa(number)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

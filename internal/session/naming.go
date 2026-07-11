package session

import (
	"fmt"
	"strings"
	"unicode"
)

func sessionNames(number int, title string) (string, string, error) {
	title = strings.TrimSpace(title)
	if number <= 0 || title == "" || strings.ContainsAny(title, `/\\`) {
		return "", "", ErrInvalidMetadata
	}
	var slug strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			slug.WriteRune(r)
			dash = false
		case unicode.IsSpace(r) || r == '-' || r == '_':
			if slug.Len() > 0 && !dash {
				slug.WriteByte('-')
				dash = true
			}
		default:
			if unicode.IsControl(r) {
				return "", "", ErrInvalidMetadata
			}
		}
	}
	value := strings.Trim(slug.String(), "-")
	if value == "" {
		return "", "", ErrInvalidMetadata
	}
	return fmt.Sprintf("%03d - %s", number, title), value, nil
}

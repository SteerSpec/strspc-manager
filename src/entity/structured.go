package entity

import (
	"errors"
	"strings"
)

// ParseStructuredNote splits the content of a `structured` note at the first
// colon into its format and its payload (NTE-037). Neither may be blank. The
// payload is returned as written: SteerSpec does not interpret it (NTE-039).
func ParseStructuredNote(content string) (format, payload string, err error) {
	format, payload, ok := strings.Cut(content, ":")
	if !ok || strings.TrimSpace(format) == "" || strings.TrimSpace(payload) == "" {
		return "", "", errors.New("content must be <format>:<payload>, with neither part blank (NTE-037)")
	}
	return format, payload, nil
}

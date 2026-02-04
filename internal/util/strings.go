package util

import (
	"errors"
	"regexp"
	"strings"
)

// ErrUnsafePathSegment is returned when a string contains characters that could enable path traversal.
var ErrUnsafePathSegment = errors.New("invalid character in path segment (possible path traversal)")

// safePathSegmentRe matches allowed characters for URL path segments and IDs.
// Allows alphanumeric, dash, underscore, dot (e.g. CVE-2024-1234).
var safePathSegmentRe = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`)

// SafePathSegment validates that s is safe to use in a URL path segment.
// Rejects strings containing /, \, .., or other characters that could enable path traversal.
func SafePathSegment(s string) error {
	if s == "" {
		return errors.New("empty path segment")
	}
	if strings.Contains(s, "..") {
		return ErrUnsafePathSegment
	}
	if !safePathSegmentRe.MatchString(s) {
		return ErrUnsafePathSegment
	}
	return nil
}

// QuoteYAMLString escapes s for use as a double-quoted YAML string value.
func QuoteYAMLString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return "\"" + s + "\""
}

// TruncateString truncates s to maxLen characters, adding "..." if truncated.
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

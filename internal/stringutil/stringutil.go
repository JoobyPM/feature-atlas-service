// Package stringutil provides common string manipulation utilities.
package stringutil

import "unicode/utf8"

// Truncate shortens a string to maxLen runes with ellipsis.
// Uses rune count for proper UTF-8 handling.
// If maxLen < 4, returns the string unchanged (no room for ellipsis).
func Truncate(s string, maxLen int) string {
	if maxLen < 4 {
		return s
	}
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen-3]) + "..."
}

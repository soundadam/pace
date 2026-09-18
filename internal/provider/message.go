package provider

import (
	"strings"
	"unicode"
)

// MessageLimit bounds a sanitized helper message.  Helper stderr ends up in
// model.Failure.Message and from there in stored run summaries, so every
// provider truncates at the same point unless it has a concrete reason not to.
const MessageLimit = 256

// SanitizeMessage turns untrusted helper output into a single-line message: runs
// of whitespace *and* control characters collapse into one space, leading and
// trailing space is removed, and the result is truncated to roughly limit bytes
// on a rune boundary.  Control characters are treated as whitespace rather than
// preserved because this text is rendered in a terminal and written to JSON.
//
// limit must be positive.
func SanitizeMessage(message string, limit int) string {
	if limit <= 0 {
		panic("provider: message limit must be positive")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(message))
	previousSpace := false
	for _, character := range message {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			if !previousSpace {
				builder.WriteByte(' ')
				previousSpace = true
			}
			continue
		}
		builder.WriteRune(character)
		previousSpace = false
		if builder.Len() >= limit {
			break
		}
	}
	return strings.TrimSpace(builder.String())
}

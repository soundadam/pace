package provider

import (
	"strings"
	"testing"
)

func TestSanitizeMessage(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		message string
		want    string
	}{
		{name: "empty", message: "", want: ""},
		{name: "whitespace only", message: " \n\t ", want: ""},
		{name: "control characters only", message: "\x00\x1b\x07", want: ""},
		{name: "trims", message: "  failed \n", want: "failed"},
		{name: "collapses runs", message: "failed  to \n\t connect", want: "failed to connect"},
		{
			name:    "control characters collapse like spaces",
			message: "failed\x00\x00to\x1b[31mconnect",
			want:    "failed to [31mconnect",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := SanitizeMessage(testCase.message, MessageLimit); got != testCase.want {
				t.Fatalf("SanitizeMessage(%q) = %q, want %q", testCase.message, got, testCase.want)
			}
		})
	}
}

func TestSanitizeMessageTruncationBoundaries(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		input string
		want  int
	}{
		{name: "below limit", input: strings.Repeat("a", 7), want: 7},
		{name: "exactly at limit", input: strings.Repeat("a", 8), want: 8},
		{name: "one past limit", input: strings.Repeat("a", 9), want: 8},
		{name: "well past limit", input: strings.Repeat("a", 10000), want: 8},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := SanitizeMessage(testCase.input, 8); len(got) != testCase.want {
				t.Fatalf("len = %d, want %d (%q)", len(got), testCase.want, got)
			}
		})
	}
}

// Truncation stops on a rune boundary, so the result may slightly exceed the
// limit rather than emit a replacement character mid-rune.
func TestSanitizeMessageTruncatesOnRuneBoundary(t *testing.T) {
	// U+2192 is three bytes in UTF-8, so an 8-byte limit lands mid-rune.
	got := SanitizeMessage(strings.Repeat("→", 100), 8)
	if !strings.HasPrefix(got, "→→→") || strings.ContainsRune(got, '�') {
		t.Fatalf("SanitizeMessage = %q", got)
	}
	if len(got) < 8 || len(got) > 8+2 {
		t.Fatalf("len = %d, want 8..10", len(got))
	}
}

func TestSanitizeMessageRejectsNonPositiveLimit(t *testing.T) {
	for _, limit := range []int{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("SanitizeMessage with limit %d did not panic", limit)
				}
			}()
			SanitizeMessage("x", limit)
		}()
	}
}

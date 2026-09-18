package provider

import (
	"io"
	"strings"
	"testing"
)

func TestCappedBufferWriteBoundaries(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		write string
		want  string
	}{
		{name: "below limit", write: "abc", want: "abc"},
		{name: "exactly at limit", write: "abcdefgh", want: "abcdefgh"},
		{name: "one past limit", write: "abcdefghi", want: "abcdefgh"},
		{name: "well past limit", write: strings.Repeat("x", 4096), want: strings.Repeat("x", 8)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			buffer := NewCappedBuffer(8)
			written, err := buffer.Write([]byte(testCase.write))
			if err != nil {
				t.Fatal(err)
			}
			// A short write would make an exec'd helper abort, so the buffer
			// always claims to have consumed everything.
			if written != len(testCase.write) {
				t.Fatalf("written = %d, want %d", written, len(testCase.write))
			}
			if buffer.String() != testCase.want {
				t.Fatalf("String() = %q, want %q", buffer.String(), testCase.want)
			}
			if string(buffer.Bytes()) != testCase.want {
				t.Fatalf("Bytes() = %q, want %q", buffer.Bytes(), testCase.want)
			}
			if buffer.Len() != len(testCase.want) {
				t.Fatalf("Len() = %d, want %d", buffer.Len(), len(testCase.want))
			}
		})
	}
}

func TestCappedBufferWriteAcrossLimitInChunks(t *testing.T) {
	buffer := NewCappedBuffer(8)
	for _, chunk := range []string{"abcd", "efgh", "ijkl"} {
		written, err := buffer.Write([]byte(chunk))
		if err != nil || written != len(chunk) {
			t.Fatalf("Write(%q) = %d, %v", chunk, written, err)
		}
	}
	if buffer.String() != "abcdefgh" {
		t.Fatalf("String() = %q", buffer.String())
	}
	// Writes after the limit is reached must stay accepted and discarded.
	written, err := buffer.Write([]byte("mnop"))
	if err != nil || written != 4 || buffer.Len() != 8 {
		t.Fatalf("post-limit write = %d, %v, len = %d", written, err, buffer.Len())
	}
}

// io.Copy prefers io.ReaderFrom over Write, and os/exec drains a command's
// stdout/stderr pipe with io.Copy.  If CappedBuffer ever embeds bytes.Buffer
// again, ReadFrom is promoted, that fast path is taken and the limit is
// silently bypassed.
func TestCappedBufferLimitSurvivesIOCopy(t *testing.T) {
	buffer := NewCappedBuffer(64)
	copied, err := io.Copy(buffer, strings.NewReader(strings.Repeat("y", 1<<20)))
	if err != nil {
		t.Fatal(err)
	}
	if copied != 1<<20 {
		t.Fatalf("copied = %d, want %d", copied, 1<<20)
	}
	if buffer.Len() != 64 {
		t.Fatalf("Len() = %d, want 64", buffer.Len())
	}
}

func TestCappedBufferStringDoesNotTrim(t *testing.T) {
	buffer := NewCappedBuffer(64)
	if _, err := buffer.Write([]byte("  failed \n")); err != nil {
		t.Fatal(err)
	}
	if buffer.String() != "  failed \n" {
		t.Fatalf("String() = %q, want the bytes verbatim", buffer.String())
	}
}

func TestNewCappedBufferRejectsNonPositiveLimit(t *testing.T) {
	for _, limit := range []int{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("NewCappedBuffer(%d) did not panic", limit)
				}
			}()
			NewCappedBuffer(limit)
		}()
	}
}

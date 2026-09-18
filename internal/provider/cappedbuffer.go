package provider

import "bytes"

// CappedBuffer collects helper output up to a fixed byte limit and discards
// everything past it.  Writes always report the full input length as written so
// a helper never observes a short write and aborts mid-measurement.
//
// The underlying bytes.Buffer is a named field rather than an embedded type on
// purpose: embedding promotes ReadFrom, and io.Copy — which os/exec uses to
// drain a command's stdout/stderr pipe — prefers ReadFrom over Write, which
// would bypass the limit entirely.
type CappedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

// NewCappedBuffer returns a buffer that retains at most limit bytes.  The limit
// is required and must be positive: an uncapped buffer fed by an untrusted
// helper is never what a caller wants, so there is deliberately no usable zero
// value and no way to ask for one.
func NewCappedBuffer(limit int) *CappedBuffer {
	if limit <= 0 {
		panic("provider: capped buffer limit must be positive")
	}
	return &CappedBuffer{limit: limit}
}

func (buffer *CappedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	if remaining := buffer.limit - buffer.buffer.Len(); remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = buffer.buffer.Write(data)
	}
	return written, nil
}

// Len reports how many bytes have been retained.
func (buffer *CappedBuffer) Len() int { return buffer.buffer.Len() }

// Bytes exposes the retained bytes.  The slice aliases the buffer and is only
// valid until the next Write.
func (buffer *CappedBuffer) Bytes() []byte { return buffer.buffer.Bytes() }

// String returns the retained bytes verbatim.  It deliberately does not trim:
// callers that want a human-readable message run the text through
// SanitizeMessage, which collapses and trims, while callers that compare raw
// helper output need it untouched.
func (buffer *CappedBuffer) String() string { return buffer.buffer.String() }

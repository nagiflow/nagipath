package web

import (
	"strings"
	"sync"
)

// ringBufferCapacity is how many recent log lines the diagnostics bundle can
// show. ringBufferMaxLine caps a single write too, so one runaway line (a
// stack trace, say) cannot blow the whole budget by itself.
const (
	ringBufferCapacity = 500
	ringBufferMaxLine  = 4 * 1024
)

// RingBuffer captures the process's most recent log lines for the diagnostics
// bundle. It implements io.Writer so it can sit beside stderr in an
// io.MultiWriter passed to slog's handler — see logger() in cmd/nagipath/main.go.
type RingBuffer struct {
	mu    sync.Mutex
	lines []string
	next  int // slot the next Write lands in
	full  bool
}

func NewRingBuffer() *RingBuffer {
	return &RingBuffer{lines: make([]string, ringBufferCapacity)}
}

// Write implements io.Writer. log/slog's handlers call Write exactly once per
// log record, with the whole formatted line in one call — so each call is
// captured as one buffer entry, trimmed of its trailing newline, rather than
// re-split on any newlines inside it. That's the simplest behavior that
// matches how slog actually calls Write.
func (b *RingBuffer) Write(p []byte) (int, error) {
	line := strings.TrimSuffix(string(p), "\n")
	if len(line) > ringBufferMaxLine {
		line = line[:ringBufferMaxLine] + " …(truncated)"
	}
	b.mu.Lock()
	b.lines[b.next] = line
	b.next++
	if b.next == ringBufferCapacity {
		b.next = 0
		b.full = true
	}
	b.mu.Unlock()
	return len(p), nil
}

// Lines returns the captured lines oldest-first.
func (b *RingBuffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.full {
		out := make([]string, b.next)
		copy(out, b.lines[:b.next])
		return out
	}
	out := make([]string, ringBufferCapacity)
	n := copy(out, b.lines[b.next:])
	copy(out[n:], b.lines[:b.next])
	return out
}

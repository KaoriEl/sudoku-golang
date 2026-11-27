package docker

import (
	"context"
	"io"
	"strings"
	"sync"
	"unicode/utf8"
)

// TailBuffer is an io.Writer that keeps only the last Limit bytes written to it, so a chatty command
// (a `compose build` easily prints megabytes) costs O(Limit) memory instead of its full output.
type TailBuffer struct {
	Limit     int
	mu        sync.Mutex
	buf       []byte
	truncated bool
	written   int64
}

// Written is the total number of bytes ever written (also the cut ones).
func (t *TailBuffer) Written() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.written
}

func (t *TailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(p)
	t.written += int64(n)
	if t.Limit <= 0 {
		t.buf = append(t.buf, p...)
		return n, nil
	}
	if len(p) >= t.Limit {
		t.buf = append(t.buf[:0], p[len(p)-t.Limit:]...)
		t.truncated = true
		return n, nil
	}
	t.buf = append(t.buf, p...)
	// Compact only when twice over the limit: amortised O(1) per byte, at most 2×Limit held.
	if len(t.buf) > 2*t.Limit {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-t.Limit:]...)
		t.truncated = true
	}
	return n, nil
}

// String returns the kept tail, starting at a rune (and, when cut, a line) boundary.
func (t *TailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.buf
	cut := t.truncated
	if t.Limit > 0 && len(b) > t.Limit {
		b = b[len(b)-t.Limit:]
		cut = true
	}
	if !cut {
		return string(b)
	}
	for len(b) > 0 && !utf8.RuneStart(b[0]) {
		b = b[1:]
	}
	s := string(b)
	if i := strings.IndexByte(s, '\n'); i >= 0 && i < len(s)-1 {
		s = s[i+1:]
	}
	return "…\n" + s
}

// LastLines returns at most max bytes of the trailing lines of s.
func LastLines(s string, maxBytes int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxBytes {
		return s
	}
	s = s[len(s)-maxBytes:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 && i < len(s)-1 {
		s = s[i+1:]
	}
	return "…" + s
}

const errTailBytes = 2048

// ComposeTail runs a compose command with stdout and stderr interleaved (in the order the command
// printed them) into one buffer that keeps only the last limit bytes. A failure's error carries the
// last couple of KB of that output instead of the whole stderr.
func (r *Runner) ComposeTail(ctx context.Context, file string, limit int, args ...string) (string, error) {
	out := &TailBuffer{Limit: limit}
	err := r.ComposeTo(ctx, file, out, args...)
	return strings.TrimSpace(out.String()), err
}

// ComposeTo runs a compose command writing stdout and stderr, interleaved, to out. When out is a
// *TailBuffer, a failure's error carries the last couple of KB of the output.
func (r *Runner) ComposeTo(ctx context.Context, file string, out io.Writer, args ...string) error {
	full := append([]string{flagFile, file}, args...)
	cmd, err := r.Command(ctx, r.compose, full...)
	if err != nil {
		return err
	}
	cmd.Stdout = out // the same comparable writer: os/exec serialises Write calls
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		msg := ""
		if tb, ok := out.(*TailBuffer); ok {
			msg = LastLines(tb.String(), errTailBytes)
		}
		return &CommandError{Args: append(append([]string{}, r.compose...), full...), Stderr: msg, Err: err}
	}
	return nil
}

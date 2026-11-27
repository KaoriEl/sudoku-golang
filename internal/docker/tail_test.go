package docker

import (
	"strings"
	"testing"
)

func TestTailBufferKeepsLastBytes(t *testing.T) {
	tb := &TailBuffer{Limit: 16}
	for i := 0; i < 1000; i++ {
		_, _ = tb.Write([]byte("line-" + strings.Repeat("x", i%7) + "\n"))
	}
	if cap(tb.buf) > 64 {
		t.Fatalf("buffer grew to %d bytes, want it bounded", cap(tb.buf))
	}
	got := tb.String()
	if !strings.HasPrefix(got, "…\n") {
		t.Fatalf("truncated output must be marked, got %q", got)
	}
	if !strings.HasSuffix(got, "\n") || len(got) > 16+len("…\n") {
		t.Fatalf("unexpected tail %q", got)
	}
}

func TestTailBufferSmallOutputUntouched(t *testing.T) {
	tb := &TailBuffer{Limit: 1024}
	_, _ = tb.Write([]byte("hello\n"))
	_, _ = tb.Write([]byte("world"))
	if got := tb.String(); got != "hello\nworld" {
		t.Fatalf("got %q", got)
	}
}

func TestTailBufferHugeWrite(t *testing.T) {
	tb := &TailBuffer{Limit: 8}
	_, _ = tb.Write([]byte(strings.Repeat("a", 100) + "\nabc"))
	if got := tb.String(); got != "…\nabc" {
		t.Fatalf("got %q", got)
	}
}

func TestLastLines(t *testing.T) {
	if got := LastLines("short", 100); got != "short" {
		t.Fatalf("got %q", got)
	}
	got := LastLines(strings.Repeat("noise\n", 100)+"error: boom", 20)
	if !strings.HasSuffix(got, "error: boom") || len(got) > 20+len("…") {
		t.Fatalf("got %q", got)
	}
}

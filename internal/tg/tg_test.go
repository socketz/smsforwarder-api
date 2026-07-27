package tg

import (
	"strings"
	"testing"
)

func TestSplitMessage(t *testing.T) {
	// short text → single chunk, unchanged
	got := splitMessage("hello", 4000)
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("short: got %v", got)
	}

	// long ASCII → multiple chunks, each within limit, lossless rejoin
	s := strings.Repeat("a", 9000)
	parts := splitMessage(s, 4000)
	if len(parts) < 3 {
		t.Fatalf("expected >=3 parts, got %d", len(parts))
	}
	for _, p := range parts {
		if len([]rune(p)) > 4000 {
			t.Fatalf("chunk exceeds limit: %d", len([]rune(p)))
		}
	}
	if strings.Join(parts, "") != s {
		t.Fatal("ascii rejoin lost data")
	}

	// multibyte (Chinese) must never split a rune and must rejoin exactly
	s2 := strings.Repeat("中", 5000)
	p2 := splitMessage(s2, 4000)
	if strings.Join(p2, "") != s2 {
		t.Fatal("multibyte rejoin lost data")
	}

	// prefers to cut at a newline near the limit
	s3 := strings.Repeat("x", 3990) + "\n" + strings.Repeat("y", 1000)
	p3 := splitMessage(s3, 4000)
	if !strings.HasSuffix(p3[0], "\n") {
		t.Fatalf("expected first chunk to end at newline, got tail %q", p3[0][len(p3[0])-5:])
	}
}

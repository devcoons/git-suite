package codes

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMessageStaysOneValidLine(t *testing.T) {
	err := New(InvalidInvocation, "line\n\tnext %s", strings.Repeat("é", 800))
	if strings.Contains(err.Message, "\n") || strings.Contains(err.Message, "\t") {
		t.Fatalf("message = %q", err.Message)
	}
	if !utf8.ValidString(err.Message) || len(err.Message) > 1024 {
		t.Fatalf("message length %d valid %v", len(err.Message), utf8.ValidString(err.Message))
	}
	if !strings.HasSuffix(err.Message, "...") {
		t.Fatalf("message = %q", err.Message)
	}
	if got := New(Internal, "   "); got.Message != "unknown error" {
		t.Fatalf("blank = %q", got.Message)
	}
}

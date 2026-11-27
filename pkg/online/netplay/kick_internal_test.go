package netplay

import (
	"errors"
	"strings"
	"testing"
)

func TestKickError(t *testing.T) {
	err := &KickError{Reason: "cheating"}
	if !errors.Is(err, ErrKicked) {
		t.Fatalf("KickError does not match ErrKicked")
	}
	if errors.Is(err, ErrDisconnected) || errors.Is(err, ErrRejected) {
		t.Fatalf("KickError matches another sentinel")
	}
	if got := err.Error(); got != "net: kicked by server: cheating" {
		t.Fatalf("Error() = %q", got)
	}
	var kick *KickError
	if !errors.As(err, &kick) || kick.Reason != "cheating" {
		t.Fatalf("errors.As failed for %+v", err)
	}
}

func TestTruncateReason(t *testing.T) {
	if got := truncateReason("short", hsMaxReason); got != "short" {
		t.Fatalf("truncateReason kept nothing of a short reason: %q", got)
	}
	long := strings.Repeat("x", hsMaxReason+100)
	if got := truncateReason(long, hsMaxReason); len(got) != hsMaxReason {
		t.Fatalf("truncateReason length = %d, want %d", len(got), hsMaxReason)
	}
	// A cut never lands inside a multi-byte rune (123 = RFC 6455 125-byte control frame limit minus 2-byte close code).
	runes := strings.Repeat("é", 63) // 126 bytes
	got := truncateReason(runes, 123)
	if len(got) != 122 {
		t.Fatalf("truncateReason cut mid-rune: %d bytes, want 122", len(got))
	}
}

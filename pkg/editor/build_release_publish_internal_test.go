package editor

import (
	"math"
	"testing"
)

func TestUploadClampAndSafeGitRef(t *testing.T) {
	if got := clampUploadFraction(math.NaN()); got != 0 {
		t.Fatalf("clampUploadFraction(NaN) = %v, want 0", got)
	}
	if got := clampUploadFraction(math.Inf(-1)); got != 0 {
		t.Fatalf("clampUploadFraction(-Inf) = %v, want 0", got)
	}
	if got := clampUploadFraction(math.Inf(1)); got != 1 {
		t.Fatalf("clampUploadFraction(+Inf) = %v, want 1", got)
	}
	if got := clampUploadFraction(0.42); got != 0.42 {
		t.Fatalf("clampUploadFraction(0.42) = %v, want 0.42", got)
	}

	for _, badRef := range []string{"", "-v1.0", "--option", "v1..v2", "v1\nhead", "v1 head", `v1\x`} {
		if isSafeGitRef(badRef) {
			t.Errorf("isSafeGitRef(%q) = true, want false", badRef)
		}
	}
	for _, okRef := range []string{"v0.3.2", "release/1.0.0", "2026.11.26-rc1"} {
		if !isSafeGitRef(okRef) {
			t.Errorf("isSafeGitRef(%q) = false, want true", okRef)
		}
	}
}

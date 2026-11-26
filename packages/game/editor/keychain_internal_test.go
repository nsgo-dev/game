package editor

import (
	"math"
	"strings"
	"testing"
)

func TestKeychainValidationAndUploadClamp(t *testing.T) {
	badKeys := [][2]string{
		{"", "acc"},
		{"svc", ""},
		{"-svc", "acc"},
		{"svc", "-acc"},
		{"svc\nline", "acc"},
		{"svc", "acc\r"},
		{"svc\"quote", "acc"},
		{"svc", `acc\slash`},
		{"svc", "acc\x00nul"},
		{"svc", "acc\tctrl"},
	}
	for _, tc := range badKeys {
		if _, _, err := validateKeychainKey(tc[0], tc[1]); err == nil {
			t.Errorf("validateKeychainKey(%q, %q) unexpectedly succeeded", tc[0], tc[1])
		}
	}
	if s, a, err := validateKeychainKey(" dev.nsgo.game.publish ", " dev.game:itch.io "); err != nil || s != "dev.nsgo.game.publish" || a != "dev.game:itch.io" {
		t.Fatalf("validateKeychainKey valid = (%q, %q, %v)", s, a, err)
	}

	if err := validateKeychainSecret(""); err == nil {
		t.Error("empty secret should fail")
	}
	if err := validateKeychainSecret("abc\x00def"); err == nil {
		t.Error("secret with NUL should fail")
	}
	if err := validateKeychainSecret(strings.Repeat("x", maxKeychainSecretBytes+1)); err == nil {
		t.Error("oversized secret should fail")
	}

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

package keychain

import (
	"strings"
	"testing"
)

func TestKeychainValidation(t *testing.T) {
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
}

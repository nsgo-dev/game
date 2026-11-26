//go:build darwin && !ios

package editor

import "testing"

func TestDarwinKeychainDecodeHexPayload(t *testing.T) {
	if got := decodeDarwinKeychainPayload("0x68656c6c6f0a776f726c64"); got != "hello\nworld" {
		t.Fatalf("decodeDarwinKeychainPayload(0x...) = %q, want %q", got, "hello\nworld")
	}
	if got := decodeDarwinKeychainPayload("0X616263"); got != "abc" {
		t.Fatalf("decodeDarwinKeychainPayload(0X...) = %q, want %q", got, "abc")
	}
	if got := decodeDarwinKeychainPayload("plain-api-token"); got != "plain-api-token" {
		t.Fatalf("decodeDarwinKeychainPayload(plain) = %q, want plain-api-token", got)
	}
	if got := decodeDarwinKeychainPayload("0xZZZ"); got != "0xZZZ" {
		t.Fatalf("decodeDarwinKeychainPayload(invalid hex) = %q, want 0xZZZ", got)
	}
}

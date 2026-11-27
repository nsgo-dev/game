package token_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts/token"
)

var t0 = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

// clock is a settable test clock.
type clock struct{ t time.Time }

func (c *clock) Now() time.Time { return c.t }

// seedKey returns a deterministic key from one seed byte.
func seedKey(b byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{b}, ed25519.SeedSize))
}

func newPair(t testing.TB, ttl time.Duration) (*token.Signer, *token.Verifier, *clock) {
	t.Helper()
	c := &clock{t0}
	s, err := token.NewSigner(seedKey(1), "https://accounts.example.com", "my-game", ttl)
	if err != nil {
		t.Fatal(err)
	}
	s.Now = c.Now
	// The game server gets only the public key set, through its YAML text.
	data, err := s.KeySet().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	ks, err := token.ParseKeySet(data)
	if err != nil {
		t.Fatal(err)
	}
	v, err := token.NewVerifier(ks)
	if err != nil {
		t.Fatal(err)
	}
	v.Now = c.Now
	return s, v, c
}

func TestIssueAndVerify(t *testing.T) {
	s, v, _ := newPair(t, 15*time.Minute)
	tok, issued, err := s.Issue("acct-1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Verify(tok)
	if err != nil {
		t.Fatalf("Verify = %v", err)
	}
	if got != issued {
		t.Fatalf("claims = %+v, want %+v", got, issued)
	}
	if got.Subject != "acct-1" || got.Issuer != "https://accounts.example.com" || got.Audience != "my-game" || got.KeyID != s.KeyID() {
		t.Fatalf("claims = %+v", got)
	}
	if !got.IssuedAt.Equal(t0) || !got.Expires.Equal(t0.Add(15*time.Minute)) {
		t.Fatalf("issued %v expires %v", got.IssuedAt, got.Expires)
	}
	if tok2, _, _ := s.Issue("acct-1"); tok2 == tok {
		t.Fatal("two tokens share an ID")
	}
}

func TestTokenExpiresAfterTTL(t *testing.T) {
	s, v, c := newPair(t, 90*time.Second)
	tok, _, err := s.Issue("acct-1")
	if err != nil {
		t.Fatal(err)
	}
	c.t = t0.Add(90*time.Second - time.Nanosecond)
	if _, err := v.Verify(tok); err != nil {
		t.Fatalf("just before expiry: %v", err)
	}
	c.t = t0.Add(90 * time.Second)
	if _, err := v.Verify(tok); !errors.Is(err, token.ErrExpired) {
		t.Fatalf("at expiry: %v, want ErrExpired", err)
	}
	v.Leeway = 5 * time.Second
	if _, err := v.Verify(tok); err != nil {
		t.Fatalf("within leeway: %v", err)
	}
	c.t = t0.Add(95 * time.Second)
	if _, err := v.Verify(tok); !errors.Is(err, token.ErrExpired) {
		t.Fatalf("past leeway: %v, want ErrExpired", err)
	}
}

func TestTokenFromTheFutureIsRefused(t *testing.T) {
	s, v, c := newPair(t, time.Minute)
	tok, _, _ := s.Issue("acct-1")
	c.t = t0.Add(-2 * time.Second)
	if _, err := v.Verify(tok); !errors.Is(err, token.ErrNotYetValid) {
		t.Fatalf("Verify = %v, want ErrNotYetValid", err)
	}
	v.Leeway = 2 * time.Second
	if _, err := v.Verify(tok); err != nil {
		t.Fatalf("within leeway: %v", err)
	}
}

func TestVerifyRefusesOtherIssuersAudiencesAndKeys(t *testing.T) {
	_, v, c := newPair(t, time.Minute)
	sign := func(key ed25519.PrivateKey, iss, aud string) string {
		s, err := token.NewSigner(key, iss, aud, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		s.Now = c.Now
		tok, _, err := s.Issue("acct-1")
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	cases := []struct {
		name string
		tok  string
		want error
	}{
		{"issuer", sign(seedKey(1), "https://evil.example.com", "my-game"), token.ErrIssuer},
		{"audience", sign(seedKey(1), "https://accounts.example.com", "other-game"), token.ErrAudience},
		{"key", sign(seedKey(2), "https://accounts.example.com", "my-game"), token.ErrUnknownKey},
	}
	for _, tc := range cases {
		if _, err := v.Verify(tc.tok); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestVerifyRefusesTamperedTokens(t *testing.T) {
	s, v, _ := newPair(t, time.Minute)
	tok, _, _ := s.Issue("acct-1")
	parts := strings.Split(tok, ".")
	enc := base64.RawURLEncoding
	forged := enc.EncodeToString([]byte(`{"iss":"https://accounts.example.com","sub":"acct-2","aud":"my-game","iat":1795435200,"exp":1795435260,"jti":"x"}`))
	none := enc.EncodeToString([]byte(`{"alg":"none","kid":"` + s.KeyID() + `","typ":"JWT"}`))
	extra := enc.EncodeToString([]byte(`{"alg":"EdDSA","kid":"` + s.KeyID() + `","typ":"JWT","jku":"https://evil"}`))
	trailingBrace := enc.EncodeToString([]byte(`{"alg":"EdDSA","kid":"` + s.KeyID() + `","typ":"JWT"}}`))
	sig, _ := enc.DecodeString(parts[2])
	sig[0] ^= 1
	cases := map[string]struct {
		tok  string
		want error
	}{
		"payload swapped":   {parts[0] + "." + forged + "." + parts[2], token.ErrSignature},
		"signature flipped": {parts[0] + "." + parts[1] + "." + enc.EncodeToString(sig), token.ErrSignature},
		"alg none":          {none + "." + parts[1] + ".", token.ErrMalformed},
		"header field":      {extra + "." + parts[1] + "." + parts[2], token.ErrMalformed},
		"two parts":         {parts[0] + "." + parts[1], token.ErrMalformed},
		"four parts":        {tok + ".x", token.ErrMalformed},
		"padded signature":  {parts[0] + "." + parts[1] + "." + parts[2] + "=", token.ErrMalformed},
		"trailing brace":    {trailingBrace + "." + parts[1] + "." + parts[2], token.ErrMalformed},
		"empty":             {"", token.ErrMalformed},
		"too long":          {strings.Repeat("a", token.MaxLength+1), token.ErrMalformed},
	}
	for name, tc := range cases {
		if _, err := v.Verify(tc.tok); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
}

func TestKeyRotation(t *testing.T) {
	c := &clock{t0}
	oldSigner, _ := token.NewSigner(seedKey(1), "iss", "aud", time.Hour)
	newSigner, _ := token.NewSigner(seedKey(2), "iss", "aud", time.Hour)
	oldSigner.Now, newSigner.Now = c.Now, c.Now
	both, err := oldSigner.KeySet().Merge(newSigner.KeySet())
	if err != nil {
		t.Fatal(err)
	}
	if len(both.Keys) != 2 {
		t.Fatalf("merged keys = %d", len(both.Keys))
	}
	v, err := token.NewVerifier(both)
	if err != nil {
		t.Fatal(err)
	}
	v.Now = c.Now
	for _, s := range []*token.Signer{oldSigner, newSigner} {
		tok, _, _ := s.Issue("acct")
		if _, err := v.Verify(tok); err != nil {
			t.Fatalf("key %s: %v", s.KeyID(), err)
		}
	}
	if again, _ := both.Merge(newSigner.KeySet()); len(again.Keys) != 2 {
		t.Fatalf("merging a key twice gave %d keys", len(again.Keys))
	}
	other, _ := token.NewSigner(seedKey(3), "iss", "other", time.Hour)
	if _, err := both.Merge(other.KeySet()); err == nil {
		t.Fatal("merged key sets of different audiences")
	}
}

func TestNewSignerRefusesBadSettings(t *testing.T) {
	key := seedKey(1)
	for name, f := range map[string]func() error{
		"short key":      func() error { _, err := token.NewSigner(key[:10], "i", "a", time.Minute); return err },
		"no issuer":      func() error { _, err := token.NewSigner(key, "", "a", time.Minute); return err },
		"no audience":    func() error { _, err := token.NewSigner(key, "i", "", time.Minute); return err },
		"sub-second ttl": func() error { _, err := token.NewSigner(key, "i", "a", 500*time.Millisecond); return err },
		"fractional ttl": func() error { _, err := token.NewSigner(key, "i", "a", 1500*time.Millisecond); return err },
		"empty subject": func() error {
			s, _ := token.NewSigner(key, "i", "a", time.Minute)
			_, _, err := s.Issue("")
			return err
		},
		"empty key set": func() error {
			_, err := token.NewVerifier(&token.KeySet{Version: 1, Issuer: "i", Audience: "a"})
			return err
		},
		"nil key set":     func() error { _, err := token.NewVerifier(nil); return err },
		"key set version": func() error { ks := mustKeySet(t); ks.Version = 2; _, err := token.NewVerifier(ks); return err },
	} {
		if f() == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func mustKeySet(t *testing.T) *token.KeySet {
	s, err := token.NewSigner(seedKey(1), "https://accounts.example.com", "my-game", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return s.KeySet()
}

const keySetYAML = `version: 1
issuer: https://accounts.example.com
audience: my-game
keys:
  - id: %s
    key: %s
`

func TestKeySetYAMLRoundTrip(t *testing.T) {
	ks := mustKeySet(t)
	data, err := ks.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(strings.Replace(keySetYAML, "%s", ks.Keys[0].ID, 1), "%s", ks.Keys[0].Key, 1)
	if string(data) != want {
		t.Fatalf("Marshal =\n%s\nwant\n%s", data, want)
	}
	back, err := token.ParseKeySet(data)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := back.Marshal()
	if !bytes.Equal(again, data) {
		t.Fatalf("round trip changed the file:\n%s", again)
	}
}

func TestParseKeySetRefuses(t *testing.T) {
	ks := mustKeySet(t)
	good := strings.Replace(strings.Replace(keySetYAML, "%s", ks.Keys[0].ID, 1), "%s", ks.Keys[0].Key, 1)
	cases := map[string]string{
		"unknown field":   good + "extra: 1\n",
		"version":         strings.Replace(good, "version: 1", "version: 2", 1),
		"no version":      strings.Replace(good, "version: 1\n", "", 1),
		"mismatched id":   strings.Replace(good, ks.Keys[0].ID, "0000000000000000", 1),
		"bad key":         strings.Replace(good, ks.Keys[0].Key, "AAAA", 1),
		"duplicate key":   good + "  - id: " + ks.Keys[0].ID + "\n    key: " + ks.Keys[0].Key + "\n",
		"trailing doc":    good + "---\nversion: 1\n",
		"empty":           "",
		"key field typo":  strings.Replace(good, "    key:", "    kye:", 1),
		"missing issuer":  strings.Replace(good, "issuer: https://accounts.example.com\n", "", 1),
		"missing keys":    "version: 1\nissuer: i\naudience: a\n",
		"not a key set":   "- 1\n- 2\n",
		"audience absent": strings.Replace(good, "audience: my-game\n", "", 1),
	}
	for name, data := range cases {
		if _, err := token.ParseKeySet([]byte(data)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestPrivateKeyPEM(t *testing.T) {
	key, err := token.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	pemData, err := token.MarshalPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pemData, []byte("-----BEGIN PRIVATE KEY-----\n")) {
		t.Fatalf("PEM = %q", pemData[:30])
	}
	back, err := token.ParsePrivateKey(pemData)
	if err != nil || !back.Equal(key) {
		t.Fatalf("ParsePrivateKey = %v", err)
	}
	for name, data := range map[string][]byte{
		"empty":      nil,
		"public key": []byte("-----BEGIN PUBLIC KEY-----\nAAAA\n-----END PUBLIC KEY-----\n"),
		"garbage":    []byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"),
		"trailing":   append(append([]byte{}, pemData...), "x"...),
	} {
		if _, err := token.ParsePrivateKey(data); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func FuzzVerify(f *testing.F) {
	s, v, _ := newPair(f, time.Hour)
	// Fuzzing reruns this setup in each worker process, so the token ID must not be random: every process must issue the same valid token.
	s.Rand = zeroReader{}
	valid, _, err := s.Issue("acct-1")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add("")
	f.Add("a.b.c")
	f.Add(valid[:len(valid)-2])
	f.Fuzz(func(t *testing.T, tok string) {
		claims, err := v.Verify(tok)
		if err == nil && tok != valid {
			t.Fatalf("accepted a token the signer did not issue: %q (%+v)", tok, claims)
		}
	})
}

// zeroReader reads endless zero bytes.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func FuzzParseKeySet(f *testing.F) {
	ks := mustKeySetF(f)
	data, _ := ks.Marshal()
	f.Add(data)
	f.Add([]byte("version: 1\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		ks, err := token.ParseKeySet(data)
		if err != nil {
			return
		}
		out, err := ks.Marshal()
		if err != nil {
			t.Fatalf("Marshal of a parsed key set: %v", err)
		}
		back, err := token.ParseKeySet(out)
		if err != nil {
			t.Fatalf("reparse: %v", err)
		}
		again, _ := back.Marshal()
		if !bytes.Equal(out, again) {
			t.Fatalf("round trip not stable:\n%s\n%s", out, again)
		}
	})
}

func mustKeySetF(f *testing.F) *token.KeySet {
	s, err := token.NewSigner(seedKey(1), "https://accounts.example.com", "my-game", time.Minute)
	if err != nil {
		f.Fatal(err)
	}
	return s.KeySet()
}

func BenchmarkIssue(b *testing.B) {
	s, _, _ := newPair(b, time.Hour)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := s.Issue("acct-1"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerify(b *testing.B) {
	s, v, _ := newPair(b, time.Hour)
	tok, _, _ := s.Issue("acct-1")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := v.Verify(tok); err != nil {
			b.Fatal(err)
		}
	}
}

package selfhost

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"
)

func testSigner(t testing.TB, seed byte) (*ProofSigner, map[string]ed25519.PublicKey) {
	t.Helper()
	s, err := NewProofSigner(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC) }
	pub, err := parsePublicKey(s.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	return s, map[string]ed25519.PublicKey{s.PublicKey().ID: pub}
}

func TestProofBindsBoardAccountAndScore(t *testing.T) {
	s, keys := testSigner(t, 3)
	proof, err := s.Sign("high-score", "acct-1", 4200)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := verifyProof(keys, proof, "high-score", "acct-1", 4200)
	if err != nil || !issued.Equal(s.Now()) {
		t.Fatalf("verify = %v, %v", issued, err)
	}
	for name, args := range map[string][3]string{
		"board":   {"other", "acct-1", "4200"},
		"account": {"high-score", "acct-2", "4200"},
		"score":   {"high-score", "acct-1", "4201"},
	} {
		score := int64(4200)
		if args[2] == "4201" {
			score = 4201
		}
		if _, err := verifyProof(keys, proof, args[0], args[1], score); !errors.Is(err, ErrInvalidProof) {
			t.Errorf("other %s: %v, want ErrInvalidProof", name, err)
		}
	}
	_, otherKeys := testSigner(t, 4)
	if _, err := verifyProof(otherKeys, proof, "high-score", "acct-1", 4200); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("unlisted key: %v", err)
	}
	parts := strings.Split(proof, ".")
	for name, bad := range map[string]string{
		"prefix":       "v2." + strings.Join(parts[1:], "."),
		"time changed": strings.Join([]string{parts[0], parts[1], "1795510801", parts[3]}, "."),
		"time padded":  strings.Join([]string{parts[0], parts[1], "0" + parts[2], parts[3]}, "."),
		"signature":    strings.Join([]string{parts[0], parts[1], parts[2], parts[3][:len(parts[3])-2] + "AA"}, "."),
		"padded sig":   proof + "==",
		"extra part":   proof + ".x",
		"empty":        "",
	} {
		if _, err := verifyProof(keys, bad, "high-score", "acct-1", 4200); !errors.Is(err, ErrInvalidProof) {
			t.Errorf("%s: %v, want ErrInvalidProof", name, err)
		}
	}
	if _, err := s.Sign("Bad Board", "acct-1", 1); err == nil {
		t.Fatal("signed for an invalid board name")
	}
	if _, err := s.Sign("b", "", 1); !errors.Is(err, ErrInvalidAccount) {
		t.Fatalf("empty account: %v", err)
	}
	if _, err := NewProofSigner(ed25519.PrivateKey{1, 2}); err == nil {
		t.Fatal("accepted a short key")
	}
}

func FuzzVerifyProof(f *testing.F) {
	s, keys := testSigner(f, 5)
	valid, err := s.Sign("b", "acct", 7)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid, "b", "acct", int64(7))
	f.Add(valid[:40]+"\n"+valid[40:], "b", "acct", int64(7))
	f.Add("v1.x.1.AAAA", "b", "acct", int64(7))
	f.Add("", "", "", int64(0))
	f.Fuzz(func(t *testing.T, proof, board, account string, score int64) {
		_, err := verifyProof(keys, proof, board, account, score)
		if err == nil && (proof != valid || board != "b" || account != "acct" || score != 7) {
			t.Fatalf("accepted a proof that was never signed: %q for %q %q %d", proof, board, account, score)
		}
		if err != nil && !errors.Is(err, ErrInvalidProof) {
			t.Fatalf("error %v is not ErrInvalidProof", err)
		}
	})
}

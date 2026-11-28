package authtoken_test

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts/authtoken"
)

// The account service issues tokens with a Signer; a game server checks them with a Verifier built from the public key set alone, with no network call.
func ExampleSigner() {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	signer, err := authtoken.NewSigner(key, "https://accounts.example.com", "my-game", 15*time.Minute)
	if err != nil {
		fmt.Println(err)
		return
	}
	signer.Now = func() time.Time { return now }
	tok, _, err := signer.Issue("player-42")
	if err != nil {
		fmt.Println(err)
		return
	}

	v, _ := authtoken.NewVerifier(signer.KeySet())
	v.Now = func() time.Time { return now.Add(10 * time.Minute) }
	claims, err := v.Verify(tok)
	fmt.Println(err, claims.Subject, claims.Audience, claims.Expires.Format(time.RFC3339))

	v.Now = func() time.Time { return now.Add(15 * time.Minute) }
	_, err = v.Verify(tok)
	fmt.Println(errors.Is(err, authtoken.ErrExpired))
	// Output:
	// <nil> player-42 my-game 2026-11-23T12:15:00Z
	// true
}

// A key set is the YAML file of public keys a game server loads; unknown fields are refused.
func ExampleParseKeySet() {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	signer, _ := authtoken.NewSigner(key, "https://accounts.example.com", "my-game", time.Minute)
	data, err := signer.KeySet().Marshal()
	if err != nil {
		fmt.Println(err)
		return
	}
	ks, err := authtoken.ParseKeySet(data)
	fmt.Println(err, ks.Issuer, len(ks.Keys), ks.Keys[0].ID == authtoken.KeyID(key.Public().(ed25519.PublicKey)))

	_, err = authtoken.ParseKeySet(append(data, "extra: 1\n"...))
	fmt.Println(err != nil)
	// Output:
	// <nil> https://accounts.example.com 1 true
	// true
}

// The service keeps its signing key as a PKCS #8 PEM block.
func ExampleMarshalPrivateKey() {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	pem, err := authtoken.MarshalPrivateKey(key)
	if err != nil {
		fmt.Println(err)
		return
	}
	back, err := authtoken.ParsePrivateKey(pem)
	fmt.Println(err, bytes.HasPrefix(pem, []byte("-----BEGIN PRIVATE KEY-----")), back.Equal(key))
	// Output:
	// <nil> true true
}

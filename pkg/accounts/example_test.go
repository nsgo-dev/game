package accounts_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/accounts"
	"github.com/nsgo-dev/game/pkg/accounts/token"
)

// exampleConfig keeps the argon2id cost low so the examples run quickly; a real service uses DefaultConfig's cost.
const exampleConfig = `version: 1
issuer: https://accounts.example.com
audience: my-game
tokenTTL: 15m
password:
  minLength: 10
  argon2id:
    time: 1
    memoryKiB: 64
    threads: 1
`

// newExampleService returns a service on an in-memory store with a fixed signing key and clock.
func newExampleService(identities map[string]accounts.IdentityVerifier) *accounts.Service {
	cfg, err := accounts.ParseConfig([]byte(exampleConfig))
	if err != nil {
		panic(err)
	}
	svc, err := accounts.New(cfg, accounts.Options{
		Store:      accounts.NewMemoryStore(),
		Key:        ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)),
		Identities: identities,
		Now:        func() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		panic(err)
	}
	return svc
}

// A guest account upgrades to email sign-in without changing its ID, so a second device that signs in with the address reaches the same account and its data.
func ExampleService() {
	ctx := context.Background()
	svc := newExampleService(nil)

	guest, err := svc.CreateGuest(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	p, _ := svc.Profile(ctx, guest.AccountID)
	fmt.Println("created:", guest.Created, "guest:", p.Guest())

	if err := svc.LinkEmail(ctx, guest.AccountID, " Ada@Example.com ", "correct horse battery"); err != nil {
		fmt.Println(err)
		return
	}
	device2, err := svc.SignInEmail(ctx, "ada@example.com", "correct horse battery")
	if err != nil {
		fmt.Println(err)
		return
	}
	p, _ = svc.Profile(ctx, guest.AccountID)
	fmt.Println("same account:", device2.AccountID == guest.AccountID, "email:", p.Email, "guest:", p.Guest())

	_, err = svc.SignInEmail(ctx, "ada@example.com", "wrong password!")
	fmt.Println(err)
	// Output:
	// created: true guest: true
	// same account: true email: ada@example.com guest: false
	// accounts: invalid credentials
}

// A game server verifies the service's tokens offline with the public key set alone.
func ExampleService_KeySet() {
	ctx := context.Background()
	svc := newExampleService(nil)
	creds, err := svc.CreateGuest(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}

	// The game server loads the key set from the YAML file the service published.
	data, _ := svc.KeySet().Marshal()
	ks, err := token.ParseKeySet(data)
	if err != nil {
		fmt.Println(err)
		return
	}
	v, _ := token.NewVerifier(ks)
	v.Now = func() time.Time { return time.Date(2026, 11, 23, 12, 5, 0, 0, time.UTC) }
	claims, err := v.Verify(creds.Token.Reveal())
	fmt.Println(err, claims.Subject == creds.AccountID, claims.Audience, claims.Expires.Format(time.RFC3339))
	// Output:
	// <nil> true my-game 2026-11-23T12:15:00Z
}

// A platform identity, checked by the IdentityVerifier registered for its provider, signs a player in; a second device with its own credential for the same player reaches the same account.
func ExampleService_SignInIdentity() {
	ctx := context.Background()
	gameCenter := accounts.IdentityVerifierFunc(func(_ context.Context, credential string) (string, error) {
		switch credential {
		case "signature-from-phone", "signature-from-tablet":
			return "G:1234", nil
		}
		return "", fmt.Errorf("bad signature")
	})
	svc := newExampleService(map[string]accounts.IdentityVerifier{"gamecenter": gameCenter})

	phone, err := svc.SignInIdentity(ctx, "gamecenter", "signature-from-phone")
	if err != nil {
		fmt.Println(err)
		return
	}
	tablet, err := svc.SignInIdentity(ctx, "gamecenter", "signature-from-tablet")
	if err != nil {
		fmt.Println(err)
		return
	}
	p, _ := svc.Profile(ctx, phone.AccountID)
	fmt.Println(phone.Created, tablet.Created, tablet.AccountID == phone.AccountID, p.Identities)
	_, err = svc.SignInIdentity(ctx, "steam", "ticket")
	fmt.Println(err)
	// Output:
	// true false true [{gamecenter G:1234}]
	// accounts: unknown identity provider
}

// ParseConfig reads the service's versioned YAML configuration and refuses unknown fields.
func ExampleParseConfig() {
	cfg, err := accounts.ParseConfig([]byte(exampleConfig))
	fmt.Println(err, cfg.Issuer, cfg.Audience, time.Duration(cfg.TokenTTL), cfg.Password.MinLength)

	_, err = accounts.ParseConfig([]byte(exampleConfig + "debug: true\n"))
	fmt.Println(err != nil)
	// Output:
	// <nil> https://accounts.example.com my-game 15m0s 10
	// true
}

// A Secret prints, logs and marshals as "[redacted]"; Reveal returns the value for the one place that needs it.
func ExampleSecret() {
	s := accounts.NewSecret("session-secret")
	fmt.Println(s, s.Reveal())
	fmt.Printf("%+v\n", accounts.Credentials{AccountID: "a1", Session: s})
	// Output:
	// [redacted] session-secret
	// {AccountID:a1 Session:[redacted] Token:[redacted] Expires:0001-01-01 00:00:00 +0000 UTC Created:false}
}

// HashPassword stores a password as an argon2id hash; VerifyPassword checks a password against it and reports the hash's cost.
func ExampleHashPassword() {
	params := accounts.Argon2Params{Time: 1, MemoryKiB: 64, Threads: 1}
	hash, err := accounts.HashPassword("correct horse battery", params, rand.Reader)
	if err != nil {
		fmt.Println(err)
		return
	}
	ok, cost, err := accounts.VerifyPassword("correct horse battery", hash)
	fmt.Println(ok, cost, err)
	ok, _, _ = accounts.VerifyPassword("wrong password!", hash)
	fmt.Println(ok)
	// Output:
	// true {1 64 1} <nil>
	// false
}

// NormalizeEmail trims and lowercases a plain address and refuses anything else.
func ExampleNormalizeEmail() {
	email, err := accounts.NormalizeEmail("  Ada@Example.COM ")
	fmt.Println(email, err)
	_, err = accounts.NormalizeEmail("Ada <ada@example.com>")
	fmt.Println(err)
	// Output:
	// ada@example.com <nil>
	// accounts: invalid email address
}

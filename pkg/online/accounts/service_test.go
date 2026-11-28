package accounts_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts"
	"github.com/nsgo-dev/game/pkg/online/accounts/authtoken"
	"github.com/nsgo-dev/game/pkg/online/accounts/storetest"
)

var t0 = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

// clock is a settable test clock shared by the service and a game server's verifier.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fastArgon2 keeps tests quick; the cost does not change what is tested.
var fastArgon2 = accounts.Argon2Params{Time: 1, MemoryKiB: 64, Threads: 1}

const testConfig = `version: 1
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

func testKey() ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
}

// gameCenter is a fake Game Center verifier: each device gets its own signed credential for the same player, as the real one does.
var gameCenter = accounts.IdentityVerifierFunc(func(_ context.Context, credential string) (string, error) {
	switch credential {
	case "gc-signature-device-1", "gc-signature-device-2":
		return "G:1234", nil
	case "gc-signature-other-player":
		return "G:5678", nil
	}
	return "", fmt.Errorf("bad Game Center signature %q", credential)
})

type fixture struct {
	svc   *accounts.Service
	store accounts.Store
	clock *clock
	logs  *bytes.Buffer
}

func newFixture(t *testing.T, mutate func(*accounts.Config, *accounts.Options)) *fixture {
	t.Helper()
	cfg, err := accounts.ParseConfig([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{store: accounts.NewMemoryStore(), clock: &clock{t: t0}, logs: &bytes.Buffer{}}
	opts := accounts.Options{
		Store:      f.store,
		Key:        testKey(),
		Identities: map[string]accounts.IdentityVerifier{"gamecenter": gameCenter},
		Logger:     slog.New(slog.NewJSONHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Now:        f.clock.Now,
	}
	if mutate != nil {
		mutate(&cfg, &opts)
	}
	f.svc, err = accounts.New(cfg, opts)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// gameServer returns the verifier a game server builds from the key set file alone.
func (f *fixture) gameServer(t *testing.T) *authtoken.Verifier {
	t.Helper()
	data, err := f.svc.KeySet().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	ks, err := authtoken.ParseKeySet(data)
	if err != nil {
		t.Fatal(err)
	}
	v, err := authtoken.NewVerifier(ks)
	if err != nil {
		t.Fatal(err)
	}
	v.Now = f.clock.Now
	return v
}

func TestGuestLinksEmailAndKeepsData(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	server := f.gameServer(t)
	saves := map[string]string{} // what the game keeps per player, keyed by account ID

	guest, err := f.svc.CreateGuest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !guest.Created {
		t.Fatal("CreateGuest did not report a new account")
	}
	claims, err := server.Verify(guest.Token.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	saves[claims.Subject] = "level 7, 1200 coins"
	if p, _ := f.svc.Profile(ctx, guest.AccountID); !p.Guest() {
		t.Fatalf("profile = %+v, want a guest", p)
	}

	if err := f.svc.LinkEmail(ctx, claims.Subject, " Ada@Example.com ", "correct horse battery"); err != nil {
		t.Fatalf("LinkEmail = %v", err)
	}

	// A second device signs in with the email address.
	device2, err := f.svc.SignInEmail(ctx, "ada@example.com", "correct horse battery")
	if err != nil {
		t.Fatalf("SignInEmail = %v", err)
	}
	if device2.Created || device2.AccountID != guest.AccountID {
		t.Fatalf("signed in to %s (created %v), want the guest's %s", device2.AccountID, device2.Created, guest.AccountID)
	}
	claims2, err := server.Verify(device2.Token.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	if saves[claims2.Subject] != "level 7, 1200 coins" {
		t.Fatalf("data after upgrade = %q", saves[claims2.Subject])
	}
	p, err := f.svc.Profile(ctx, guest.AccountID)
	if err != nil || p.Guest() || p.Email != "ada@example.com" {
		t.Fatalf("profile = %+v, %v", p, err)
	}
	// The guest's own device keeps its session.
	if _, err := f.svc.Refresh(ctx, guest.Session); err != nil {
		t.Fatalf("guest session after upgrade: %v", err)
	}
	if _, err := f.svc.SignInEmail(ctx, "ada@example.com", "wrong password!"); !errors.Is(err, accounts.ErrInvalidCredentials) {
		t.Fatalf("wrong password = %v", err)
	}
	if err := f.svc.LinkEmail(ctx, guest.AccountID, "ada2@example.com", "correct horse battery"); !errors.Is(err, accounts.ErrAlreadyLinked) {
		t.Fatalf("second LinkEmail = %v", err)
	}
	other, _ := f.svc.CreateGuest(ctx)
	if err := f.svc.LinkEmail(ctx, other.AccountID, "ADA@example.com", "correct horse battery"); !errors.Is(err, accounts.ErrTaken) {
		t.Fatalf("LinkEmail of a taken address = %v", err)
	}
}

func TestTokenVerifiedOfflineAndExpiresAsConfigured(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, func(c *accounts.Config, _ *accounts.Options) { c.TokenTTL = accounts.Duration(90 * time.Second) })
	server := f.gameServer(t)
	creds, err := f.svc.CreateGuest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !creds.Expires.Equal(t0.Add(90 * time.Second)) {
		t.Fatalf("Expires = %v, want 90s after issue", creds.Expires)
	}
	f.clock.Advance(89 * time.Second)
	if c, err := server.Verify(creds.Token.Reveal()); err != nil || c.Subject != creds.AccountID {
		t.Fatalf("before expiry: %+v, %v", c, err)
	}
	f.clock.Advance(time.Second)
	if _, err := server.Verify(creds.Token.Reveal()); !errors.Is(err, authtoken.ErrExpired) {
		t.Fatalf("at expiry: %v, want ErrExpired", err)
	}
	// The device's session gets a fresh token.
	fresh, err := f.svc.Refresh(ctx, creds.Session)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.AccountID != creds.AccountID || fresh.Session.Reveal() != creds.Session.Reveal() {
		t.Fatalf("Refresh = %s, want the same account and session", fresh.AccountID)
	}
	if _, err := server.Verify(fresh.Token.Reveal()); err != nil {
		t.Fatalf("refreshed token: %v", err)
	}
	// A token of another service's key is refused.
	other := newFixture(t, func(_ *accounts.Config, o *accounts.Options) {
		o.Key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))
	})
	foreign, _ := other.svc.CreateGuest(ctx)
	if _, err := server.Verify(foreign.Token.Reveal()); !errors.Is(err, authtoken.ErrUnknownKey) {
		t.Fatalf("foreign token = %v, want ErrUnknownKey", err)
	}
}

func TestPlatformIdentityLinksOnSecondDevice(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	device1, err := f.svc.CreateGuest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.LinkIdentity(ctx, device1.AccountID, "gamecenter", "gc-signature-device-1"); err != nil {
		t.Fatalf("LinkIdentity = %v", err)
	}
	// Linking again from the same account is a no-op.
	if err := f.svc.LinkIdentity(ctx, device1.AccountID, "gamecenter", "gc-signature-device-1"); err != nil {
		t.Fatalf("LinkIdentity again = %v", err)
	}
	device2, err := f.svc.SignInIdentity(ctx, "gamecenter", "gc-signature-device-2")
	if err != nil {
		t.Fatalf("SignInIdentity = %v", err)
	}
	if device2.Created || device2.AccountID != device1.AccountID {
		t.Fatalf("second device signed in to %s (created %v), want %s", device2.AccountID, device2.Created, device1.AccountID)
	}
	p, _ := f.svc.Profile(ctx, device1.AccountID)
	if p.Guest() || len(p.Identities) != 1 || p.Identities[0] != (accounts.Identity{Provider: "gamecenter", Subject: "G:1234"}) {
		t.Fatalf("profile = %+v", p)
	}

	// A player new to the service starts with the platform identity.
	fresh, err := f.svc.SignInIdentity(ctx, "gamecenter", "gc-signature-other-player")
	if err != nil || !fresh.Created || fresh.AccountID == device1.AccountID {
		t.Fatalf("new player = %+v, %v", fresh, err)
	}
	if err := f.svc.LinkIdentity(ctx, fresh.AccountID, "gamecenter", "gc-signature-device-1"); !errors.Is(err, accounts.ErrTaken) {
		t.Fatalf("linking another account's identity = %v", err)
	}
	if err := f.svc.LinkIdentity(ctx, device1.AccountID, "gamecenter", "gc-signature-other-player"); !errors.Is(err, accounts.ErrTaken) {
		t.Fatalf("linking a second Game Center identity = %v", err)
	}
	if _, err := f.svc.SignInIdentity(ctx, "gamecenter", "forged"); !errors.Is(err, accounts.ErrInvalidCredentials) {
		t.Fatalf("forged credential = %v", err)
	}
	if _, err := f.svc.SignInIdentity(ctx, "steam", "ticket"); !errors.Is(err, accounts.ErrUnknownProvider) {
		t.Fatalf("unknown provider = %v", err)
	}
}

func TestChangePasswordAndUnlinkIdentityKeepACredential(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	creds, err := f.svc.Register(ctx, "ada@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ChangePassword(ctx, creds.AccountID, "wrong password", "new correct password"); !errors.Is(err, accounts.ErrInvalidCredentials) {
		t.Fatalf("wrong old password = %v", err)
	}
	if err := f.svc.ChangePassword(ctx, creds.AccountID, "correct horse battery", "new correct password"); err != nil {
		t.Fatalf("ChangePassword = %v", err)
	}
	if _, err := f.svc.SignInEmail(ctx, "ada@example.com", "correct horse battery"); !errors.Is(err, accounts.ErrInvalidCredentials) {
		t.Fatalf("old password sign-in = %v", err)
	}
	if _, err := f.svc.SignInEmail(ctx, "ada@example.com", "new correct password"); err != nil {
		t.Fatalf("new password sign-in = %v", err)
	}
	if err := f.svc.LinkIdentity(ctx, creds.AccountID, "gamecenter", "gc-signature-device-1"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UnlinkIdentity(ctx, creds.AccountID, "gamecenter"); err != nil {
		t.Fatalf("UnlinkIdentity = %v", err)
	}
	if err := f.svc.UnlinkIdentity(ctx, creds.AccountID, "gamecenter"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatalf("missing identity = %v", err)
	}

	identityOnly, err := f.svc.SignInIdentity(ctx, "gamecenter", "gc-signature-device-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UnlinkIdentity(ctx, identityOnly.AccountID, "gamecenter"); !errors.Is(err, accounts.ErrLastCredential) {
		t.Fatalf("last identity = %v", err)
	}
}

func TestConcurrentFirstSignInsCreateOneAccount(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	const n = 8
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			c, err := f.svc.SignInIdentity(ctx, "gamecenter", "gc-signature-device-1")
			if err != nil {
				t.Errorf("SignInIdentity = %v", err)
				return
			}
			ids[i] = c.AccountID
		})
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("concurrent first sign-ins made several accounts: %v", ids)
		}
	}
}

func TestPasswordsUseArgon2idAndSecretsStayOutOfLogs(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, func(_ *accounts.Config, o *accounts.Options) {
		o.Identities["leaky"] = accounts.IdentityVerifierFunc(func(_ context.Context, credential string) (string, error) {
			return "", fmt.Errorf("refused credential %s", credential)
		})
	})
	const password = "hunter2-is-long-enough"
	const credential = "leaky-credential-0123456789"
	var secrets []string

	reg, err := f.svc.Register(ctx, "lin@example.com", password)
	if err != nil {
		t.Fatal(err)
	}
	secrets = append(secrets, reg.Session.Reveal(), reg.Token.Reveal())
	_, hash, err := f.store.AccountByEmail(ctx, "lin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=64,t=1,p=1$") || strings.Contains(hash, password) {
		t.Fatalf("stored hash = %q, want an argon2id hash", hash)
	}
	if ok, _, err := accounts.VerifyPassword(password, hash); !ok || err != nil {
		t.Fatalf("VerifyPassword = %v, %v", ok, err)
	}
	sess, err := f.store.Session(ctx, strings.SplitN(reg.Session.Reveal(), ".", 2)[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reg.Session.Reveal(), sess.SecretHash) || strings.Contains(sess.SecretHash, strings.SplitN(reg.Session.Reveal(), ".", 2)[1]) {
		t.Fatal("the store holds the session secret itself")
	}
	in, err := f.svc.SignInEmail(ctx, "lin@example.com", password)
	if err != nil {
		t.Fatal(err)
	}
	secrets = append(secrets, in.Session.Reveal(), in.Token.Reveal())
	_, _ = f.svc.SignInEmail(ctx, "lin@example.com", password+"x")
	_, _ = f.svc.SignInEmail(ctx, "nobody@example.com", password)
	_, _ = f.svc.SignInIdentity(ctx, "leaky", credential)
	_ = f.svc.LinkIdentity(ctx, reg.AccountID, "gamecenter", "gc-signature-device-1")
	r, err := f.svc.Refresh(ctx, in.Session)
	if err != nil {
		t.Fatal(err)
	}
	secrets = append(secrets, r.Token.Reveal())
	if err := f.svc.SignOut(ctx, in.Session); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Refresh(ctx, in.Session); !errors.Is(err, accounts.ErrInvalidCredentials) {
		t.Fatalf("Refresh after SignOut = %v", err)
	}

	// Credentials themselves print, log and marshal redacted.
	slog.New(slog.NewTextHandler(f.logs, nil)).Info("credentials", "creds", in, "session", in.Session)
	js, _ := json.Marshal(in)
	printed := fmt.Sprintf("%v %+v %#v %s %q %x", in, in, in, in.Session, in.Token, in.Session)

	everything := f.logs.String() + string(js) + printed
	if !strings.Contains(f.logs.String(), "accounts: signed in") {
		t.Fatalf("expected account events in the log:\n%s", f.logs)
	}
	for _, s := range append(secrets, password, credential, "gc-signature-device-1", hash) {
		if strings.Contains(everything, s) {
			t.Fatalf("a secret reached the output %q:\n%s", s, everything)
		}
	}
	for _, s := range secrets {
		// Session secrets and tokens have parts; none may appear either.
		for part := range strings.SplitSeq(s, ".") {
			if len(part) >= 16 && strings.Contains(everything, part) {
				t.Fatalf("part of a secret reached the output %q", part)
			}
		}
	}
}

func TestOutdatedPasswordHashIsRehashed(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	if _, err := f.svc.Register(ctx, "ada@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	// The operator raises the cost; a new service on the same store rehashes at the next sign-in.
	stronger := accounts.Argon2Params{Time: 2, MemoryKiB: 128, Threads: 1}
	g := newFixture(t, func(c *accounts.Config, o *accounts.Options) {
		c.Password.Argon2id = stronger
		o.Store = f.store
	})
	if _, err := g.svc.SignInEmail(ctx, "ada@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	_, hash, _ := f.store.AccountByEmail(ctx, "ada@example.com")
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=128,t=2,p=1$") {
		t.Fatalf("hash after sign-in = %q, want the new cost", hash)
	}
	if _, err := g.svc.SignInEmail(ctx, "ada@example.com", "correct horse battery"); err != nil {
		t.Fatalf("sign-in after rehash: %v", err)
	}
}

func TestRegisterAndInputRules(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	if _, err := f.svc.Register(ctx, "ada@example.com", "short"); !errors.Is(err, accounts.ErrWeakPassword) {
		t.Fatalf("short password = %v", err)
	}
	if _, err := f.svc.Register(ctx, "ada@example.com", strings.Repeat("x", accounts.MaxPasswordBytes+1)); !errors.Is(err, accounts.ErrWeakPassword) {
		t.Fatalf("long password = %v", err)
	}
	for _, email := range []string{"", "ada", "Ada <ada@example.com>", "<ada@example.com>", "ada@", strings.Repeat("a", 250) + "@example.com"} {
		if _, err := f.svc.Register(ctx, email, "correct horse battery"); !errors.Is(err, accounts.ErrInvalidEmail) {
			t.Errorf("Register(%q) = %v, want ErrInvalidEmail", email, err)
		}
	}
	c, err := f.svc.Register(ctx, "ada@example.com", "correct horse battery")
	if err != nil || !c.Created {
		t.Fatalf("Register = %+v, %v", c, err)
	}
	if _, err := f.svc.Register(ctx, "ADA@example.com", "correct horse battery"); !errors.Is(err, accounts.ErrTaken) {
		t.Fatalf("Register of a taken address = %v", err)
	}
	if _, err := f.svc.SignInEmail(ctx, "not an address", "correct horse battery"); !errors.Is(err, accounts.ErrInvalidCredentials) {
		t.Fatalf("SignInEmail(bad address) = %v", err)
	}
	for _, bad := range []string{"", "x.y", strings.Repeat("a", 32) + ".", c.Session.Reveal() + "x", strings.Repeat("g", 32) + "." + strings.SplitN(c.Session.Reveal(), ".", 2)[1]} {
		if _, err := f.svc.Refresh(ctx, accounts.NewSecret(bad)); !errors.Is(err, accounts.ErrInvalidCredentials) {
			t.Errorf("Refresh(%q) = %v", bad, err)
		}
	}
	if err := f.svc.SignOut(ctx, accounts.NewSecret("nope")); !errors.Is(err, accounts.ErrInvalidCredentials) {
		t.Fatalf("SignOut(bad) = %v", err)
	}
}

func TestGuestsCanBeDisabled(t *testing.T) {
	f := newFixture(t, func(c *accounts.Config, _ *accounts.Options) { c.DisableGuests = true })
	if _, err := f.svc.CreateGuest(context.Background()); !errors.Is(err, accounts.ErrGuestsDisabled) {
		t.Fatalf("CreateGuest = %v", err)
	}
}

func TestNewRefusesBadOptions(t *testing.T) {
	cfg, _ := accounts.ParseConfig([]byte(testConfig))
	store := accounts.NewMemoryStore()
	for name, opts := range map[string]accounts.Options{
		"no store":      {Key: testKey()},
		"no key":        {Store: store},
		"provider name": {Store: store, Key: testKey(), Identities: map[string]accounts.IdentityVerifier{"Game Center": gameCenter}},
		"nil verifier":  {Store: store, Key: testKey(), Identities: map[string]accounts.IdentityVerifier{"steam": nil}},
	} {
		if _, err := accounts.New(cfg, opts); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := accounts.New(accounts.Config{}, accounts.Options{Store: store, Key: testKey()}); err == nil {
		t.Error("empty config: no error")
	}
}

func TestMemoryStoreContract(t *testing.T) {
	storetest.Run(t, func(*testing.T) accounts.Store { return accounts.NewMemoryStore() })
}

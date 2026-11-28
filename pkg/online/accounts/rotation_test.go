package accounts_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/accounts"
	"github.com/nsgo-dev/game/pkg/online/accounts/authtoken"
)

func TestRotatedServiceStillAcceptsOldTokens(t *testing.T) {
	before := newFixture(t, nil)
	guest, err := before.svc.CreateGuest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	oldKeys := before.svc.KeySet()
	newKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))

	// The service restarts on the new key over the same store, with the key set genkey keeps.
	after := newFixture(t, func(_ *accounts.Config, o *accounts.Options) {
		o.Store = before.store
		o.Key = newKey
		o.KeySet = oldKeys
	})
	h := after.svc.Handler()
	var me struct{ Account string }
	if code, _ := call(t, h, "GET", "/v1/me", guest.Token.Reveal(), "", &me); code != http.StatusOK || me.Account != guest.AccountID {
		t.Fatalf("me with an old-key token after rotation = %d %+v", code, me)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/keys", nil))
	ks, err := authtoken.ParseKeySet(rec.Body.Bytes())
	if err != nil || len(ks.Keys) != 2 || ks.Keys[0].ID != authtoken.KeyID(newKey.Public().(ed25519.PublicKey)) || ks.Keys[1] != oldKeys.Keys[0] {
		t.Fatalf("keys after rotation = %+v, %v", ks, err)
	}
	fresh, err := after.svc.Refresh(context.Background(), guest.Session)
	if err != nil {
		t.Fatal(err)
	}
	if c, err := after.svc.Authenticate(fresh.Token.Reveal()); err != nil || c.KeyID != ks.Keys[0].ID {
		t.Fatalf("new token = %+v, %v", c, err)
	}

	// Without the old key the service refuses its tokens.
	dropped := newFixture(t, func(_ *accounts.Config, o *accounts.Options) {
		o.Store = before.store
		o.Key = newKey
	})
	if code, _ := call(t, dropped.svc.Handler(), "GET", "/v1/me", guest.Token.Reveal(), "", nil); code != http.StatusUnauthorized {
		t.Fatalf("me with a dropped key = %d", code)
	}

	// A key set of another issuer is refused.
	foreign := *oldKeys
	foreign.Issuer = "https://elsewhere.example.com"
	cfg, err := accounts.ParseConfig([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.New(cfg, accounts.Options{Store: accounts.NewMemoryStore(), Key: newKey, KeySet: &foreign}); err == nil {
		t.Fatal("New accepted a key set of another issuer")
	}
}

// timeoutStore fails every account creation as a store whose request timed out.
type timeoutStore struct{ accounts.Store }

func (timeoutStore) CreateAccount(ctx context.Context, a accounts.Account, passwordHash string, ids ...accounts.Identity) error {
	return context.DeadlineExceeded
}

func TestHTTPTimeoutIsUnavailable(t *testing.T) {
	f := newFixture(t, func(_ *accounts.Config, o *accounts.Options) { o.Store = timeoutStore{accounts.NewMemoryStore()} })
	var e struct{ Error string }
	if code, _ := call(t, f.svc.Handler(), "POST", "/v1/guest", "", "", &e); code != http.StatusServiceUnavailable || e.Error != "unavailable" {
		t.Fatalf("guest with a timed-out store = %d %+v", code, e)
	}
}

func TestRandNeedNotBeSafeForConcurrentUse(t *testing.T) {
	// ChaCha8 is not safe for concurrent use; the service serializes its reads.
	f := newFixture(t, func(_ *accounts.Config, o *accounts.Options) { o.Rand = rand.NewChaCha8([32]byte{1}) })
	var wg sync.WaitGroup
	ids := make([]string, 8)
	errs := make([]error, len(ids))
	for i := range ids {
		wg.Go(func() {
			c, err := f.svc.CreateGuest(context.Background())
			ids[i], errs[i] = c.AccountID, err
		})
	}
	wg.Wait()
	seen := map[string]bool{}
	for i, id := range ids {
		if errs[i] != nil || seen[id] {
			t.Fatalf("guest %d = %q, %v", i, id, errs[i])
		}
		seen[id] = true
	}
}

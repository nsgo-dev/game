package liveops_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/accounts"
	"github.com/nsgo-dev/game/pkg/accounts/token"
	"github.com/nsgo-dev/game/pkg/cloudsave"
	"github.com/nsgo-dev/game/pkg/liveops"
)

// backend is an account service and a cloud save service served in process, as a game would deploy them behind HTTPS.
type backend struct {
	accounts *accounts.Service
	saves    *cloudsave.Service
	handler  http.Handler
	now      time.Time
	// beforePut and beforeDelete, when set, run once before the next PUT or DELETE reaches the cloud save service, so a test can land another device's upload in between.
	beforePut, beforeDelete func()
}

func newBackend(t *testing.T, mutate func(*cloudsave.Config)) *backend {
	t.Helper()
	key, err := token.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	b := &backend{now: epoch}
	b.accounts, err = accounts.New(accounts.DefaultConfig("https://accounts.example.com", "cloud-test"), accounts.Options{Store: accounts.NewMemoryStore(), Key: key})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := token.NewVerifier(b.accounts.KeySet())
	if err != nil {
		t.Fatal(err)
	}
	cfg := cloudsave.DefaultConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	b.saves, err = cloudsave.New(cfg, cloudsave.Options{Store: cloudsave.NewMemoryStore(), Tokens: verifier, Now: func() time.Time { return b.now }})
	if err != nil {
		t.Fatal(err)
	}
	b.handler = b.saves.Handler()
	return b
}

// RoundTrip serves a client's request with the cloud save handler, without a network.
func (b *backend) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodPut && b.beforePut != nil {
		hook := b.beforePut
		b.beforePut = nil
		hook()
	}
	if r.Method == http.MethodDelete && b.beforeDelete != nil {
		hook := b.beforeDelete
		b.beforeDelete = nil
		hook()
	}
	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, r)
	return rec.Result(), nil
}

// client returns a CloudClient signed in with creds.
func (b *backend) client(t *testing.T, creds accounts.Credentials) *liveops.CloudClient {
	t.Helper()
	c, err := liveops.NewCloudClient(liveops.CloudClientOptions{
		URL:        "https://saves.example.com/",
		Token:      func(context.Context) (string, error) { return creds.Token.Reveal(), nil },
		HTTPClient: &http.Client{Transport: b},
		Now:        func() time.Time { return b.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// signedInPair signs a web browser in as a new guest that links an email address, and an Android phone in to the same account with that address.
func signedInPair(t *testing.T, b *backend) (web, android accounts.Credentials) {
	t.Helper()
	ctx := context.Background()
	web, err := b.accounts.CreateGuest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.accounts.LinkEmail(ctx, web.AccountID, "ada@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	android, err = b.accounts.SignInEmail(ctx, "ada@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if android.AccountID != web.AccountID {
		t.Fatalf("accounts differ: %s and %s", web.AccountID, android.AccountID)
	}
	return web, android
}

func TestCloudClientWebSaveLoadsOnAndroid(t *testing.T) {
	b := newBackend(t, nil)
	webCreds, androidCreds := signedInPair(t, b)
	web := newDevice(t, "web", b.client(t, webCreds), nil)
	android := newDevice(t, "android", b.client(t, androidCreds), nil)

	web.save(t, "main", progress{Level: 12, Note: "castle"}, time.Minute)
	if got := outcomeOf(t, web.sync(t), "main").Outcome; got != liveops.OutcomeUploaded {
		t.Fatalf("web sync = %v", got)
	}
	if got := outcomeOf(t, android.sync(t), "main").Outcome; got != liveops.OutcomeDownloaded {
		t.Fatalf("android sync = %v", got)
	}
	if p := android.load(t, "main"); p.Level != 12 || p.Note != "castle" {
		t.Fatalf("android loaded %+v", p)
	}

	// Another account's device sees none of it.
	other, err := b.accounts.CreateGuest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if slots, err := b.client(t, other).List(context.Background()); err != nil || len(slots) != 0 {
		t.Fatalf("other account lists %v, %v", slots, err)
	}
}

func TestCloudClientConcurrentWritesConflict(t *testing.T) {
	for _, tc := range []struct {
		policy  liveops.ConflictPolicy
		outcome liveops.SyncOutcome
	}{
		{liveops.PolicyKeepBoth, liveops.OutcomeKeptBoth},
		{liveops.PolicyNewest, liveops.OutcomeKeptLocal},
	} {
		t.Run(tc.policy.String(), func(t *testing.T) {
			b := newBackend(t, nil)
			webCreds, androidCreds := signedInPair(t, b)
			policy := func(o *liveops.CloudSyncOptions) { o.Policy = tc.policy }
			web := newDevice(t, "web", b.client(t, webCreds), policy)
			android := newDevice(t, "android", b.client(t, androidCreds), policy)
			web.save(t, "main", progress{Level: 1, Note: "start"}, 0)
			web.sync(t)
			android.sync(t)

			web.save(t, "main", progress{Level: 5, Note: "web"}, time.Hour)
			android.save(t, "main", progress{Level: 7, Note: "android"}, 2*time.Hour)
			// The phone has read the cloud copy and is about to upload when the browser's upload lands.
			b.beforePut = func() {
				if got := outcomeOf(t, web.sync(t), "main").Outcome; got != liveops.OutcomeUploaded {
					t.Fatalf("web upload = %v", got)
				}
			}
			if _, err := android.syncer.SyncSlot(context.Background(), "main"); !errors.Is(err, liveops.ErrCloudChanged) {
				t.Fatalf("racing upload = %v, want ErrCloudChanged", err)
			}
			if v, err := b.saves.Load(context.Background(), webCreds.AccountID, "main"); err != nil || !strings.Contains(string(v.Data), `"device": "web"`) {
				t.Fatalf("the losing upload overwrote the cloud: %s, %v", v.Data, err)
			}

			// The next sync sees both copies and the policy resolves them.
			r := outcomeOf(t, android.sync(t), "main")
			if r.Conflict == nil || r.Outcome != tc.outcome {
				t.Fatalf("resolution = %+v", r)
			}
			web.sync(t)
			switch tc.outcome {
			case liveops.OutcomeKeptBoth:
				if p := web.load(t, r.Fork); p.Level != 7 {
					t.Fatalf("web fork %q = %+v", r.Fork, p)
				}
				if p := android.load(t, "main"); p.Level != 5 {
					t.Fatalf("android main = %+v", p)
				}
			case liveops.OutcomeKeptLocal:
				if r.Discarded == nil || !strings.Contains(r.Discarded.Summary, "Level 5") {
					t.Fatalf("discarded = %+v", r.Discarded)
				}
				if p := web.load(t, "main"); p.Level != 7 {
					t.Fatalf("web main = %+v", p)
				}
			}
			// The service kept every copy of the slot: the base, the web upload and, when the phone's copy won, the phone's.
			want := int64(2)
			if tc.outcome == liveops.OutcomeKeptLocal {
				want = 3
			}
			vs, err := b.client(t, androidCreds).Versions(context.Background(), "main")
			if err != nil || int64(len(vs)) != want || vs[0].Number != want {
				t.Fatalf("versions = %+v, %v", vs, err)
			}
		})
	}
}

// TestCloudClientConcurrentDeleteConflict lands the browser's upload between the phone's re-read and its conditional DELETE, so the phone's deletion must fail rather than delete the browser's copy.
func TestCloudClientConcurrentDeleteConflict(t *testing.T) {
	for _, policy := range []liveops.ConflictPolicy{liveops.PolicyKeepBoth, liveops.PolicyNewest} {
		t.Run(policy.String(), func(t *testing.T) {
			b := newBackend(t, nil)
			webCreds, androidCreds := signedInPair(t, b)
			opts := func(o *liveops.CloudSyncOptions) { o.Policy = policy }
			web := newDevice(t, "web", b.client(t, webCreds), opts)
			android := newDevice(t, "android", b.client(t, androidCreds), opts)
			web.save(t, "main", progress{Level: 1, Note: "start"}, 0)
			web.sync(t)
			android.sync(t)

			if err := android.store.Delete("main"); err != nil {
				t.Fatal(err)
			}
			b.beforeDelete = func() {
				web.save(t, "main", progress{Level: 5, Note: "web"}, time.Hour)
				if got := outcomeOf(t, web.sync(t), "main").Outcome; got != liveops.OutcomeUploaded {
					t.Fatalf("web upload = %v", got)
				}
			}
			if _, err := android.syncer.SyncSlot(context.Background(), "main"); !errors.Is(err, liveops.ErrCloudChanged) {
				t.Fatalf("racing delete = %v, want ErrCloudChanged", err)
			}
			if v, err := b.saves.Load(context.Background(), webCreds.AccountID, "main"); err != nil || !strings.Contains(string(v.Data), `"device": "web"`) {
				t.Fatalf("the losing delete removed the cloud copy: %s, %v", v.Data, err)
			}

			// The next sync sees a deletion against a change, and every policy keeps the changed copy.
			r := outcomeOf(t, android.sync(t), "main")
			if r.Conflict == nil || r.Outcome != liveops.OutcomeKeptCloud {
				t.Fatalf("resolution = %+v", r)
			}
			if p := android.load(t, "main"); p.Level != 5 {
				t.Fatalf("android main = %+v", p)
			}
			if got := outcomeOf(t, web.sync(t), "main").Outcome; got != liveops.OutcomeUpToDate {
				t.Fatalf("web after resolution = %v", got)
			}
		})
	}
}

func TestCloudClientSlotTooLarge(t *testing.T) {
	b := newBackend(t, func(c *cloudsave.Config) { c.MaxSlotBytes = 1024 })
	creds, err := b.accounts.CreateGuest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cloud := b.client(t, creds)
	d := newDevice(t, "web", cloud, nil)
	d.save(t, "main", progress{Level: 1, Note: "small"}, 0)
	d.sync(t)
	d.save(t, "main", progress{Level: 2, Note: strings.Repeat("x", 2000)}, time.Minute)
	_, err = d.syncer.Sync(context.Background())
	if !errors.Is(err, liveops.ErrSlotTooLarge) || !strings.Contains(err.Error(), "at most 1024") {
		t.Fatalf("oversized sync = %v", err)
	}
	if err := cloud.Save(context.Background(), "raw", make([]byte, 1025)); !errors.Is(err, liveops.ErrSlotTooLarge) {
		t.Fatalf("oversized Save = %v", err)
	}
	if vs, err := cloud.Versions(context.Background(), "main"); err != nil || len(vs) != 1 {
		t.Fatalf("versions after a refused write = %+v, %v", vs, err)
	}
	if _, err := cloud.Load(context.Background(), "raw"); !errors.Is(err, liveops.ErrSlotNotFound) {
		t.Fatalf("refused slot exists: %v", err)
	}
}

func TestCloudClientVersionsAndRestore(t *testing.T) {
	b := newBackend(t, nil)
	webCreds, androidCreds := signedInPair(t, b)
	cloud := b.client(t, webCreds)
	web := newDevice(t, "web", cloud, nil)
	android := newDevice(t, "android", b.client(t, androidCreds), nil)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		b.now = epoch.Add(time.Duration(i) * time.Minute)
		web.save(t, "main", progress{Level: i, Note: "web"}, time.Duration(i)*time.Minute)
		web.sync(t)
	}
	android.sync(t)
	vs, err := cloud.Versions(ctx, "main")
	if err != nil || len(vs) != 3 || vs[0].Number != 3 || vs[2].Number != 1 || !vs[2].Created.Equal(epoch.Add(time.Minute)) || vs[0].Deleted {
		t.Fatalf("versions = %+v, %v", vs, err)
	}
	old, err := cloud.LoadVersion(ctx, "main", 1)
	if err != nil || liveops.PayloadHash(old) != vs[2].Hash {
		t.Fatalf("LoadVersion = %v", err)
	}

	b.now = epoch.Add(time.Hour)
	if err := cloud.Restore(ctx, "main", 1); err != nil {
		t.Fatal(err)
	}
	cur, err := cloud.Load(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	v, err := liveops.DecodeCloudSave(cur)
	if err != nil || v.Revision != 4 || !v.Modified.Equal(epoch.Add(time.Hour)) {
		t.Fatalf("restored payload = %+v, %v", v, err)
	}
	// Both devices take the restored copy as the newest one.
	for _, d := range []*device{web, android} {
		if got := outcomeOf(t, d.sync(t), "main").Outcome; got != liveops.OutcomeDownloaded {
			t.Fatalf("sync after restore = %v", got)
		}
		if p := d.load(t, "main"); p.Level != 1 {
			t.Fatalf("restored level = %+v", p)
		}
	}

	// A deleted slot keeps its history and comes back.
	if err := web.store.Delete("main"); err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(t, web.sync(t), "main").Outcome; got != liveops.OutcomeDeletedCloud {
		t.Fatalf("delete sync = %v", got)
	}
	vs, err = cloud.Versions(ctx, "main")
	if err != nil || !vs[0].Deleted {
		t.Fatalf("versions after delete = %+v, %v", vs, err)
	}
	if _, err := cloud.LoadVersion(ctx, "main", vs[0].Number); !errors.Is(err, liveops.ErrSlotNotFound) {
		t.Fatalf("LoadVersion of a deletion = %v", err)
	}
	if err := cloud.Restore(ctx, "main", vs[1].Number); err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(t, web.sync(t), "main").Outcome; got != liveops.OutcomeDownloaded {
		t.Fatalf("sync after undelete = %v", got)
	}
	if p := web.load(t, "main"); p.Level != 1 {
		t.Fatalf("undeleted level = %+v", p)
	}
	// Restoring the deletion deletes the slot again.
	if err := cloud.Restore(ctx, "main", vs[0].Number); err != nil {
		t.Fatal(err)
	}
	if _, err := cloud.Load(ctx, "main"); !errors.Is(err, liveops.ErrSlotNotFound) {
		t.Fatalf("Load after restoring the deletion = %v", err)
	}
	if err := cloud.Purge(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := cloud.Versions(ctx, "main"); !errors.Is(err, liveops.ErrSlotNotFound) {
		t.Fatalf("Versions after purge = %v", err)
	}
}

func TestCloudClientConditionalMethods(t *testing.T) {
	b := newBackend(t, nil)
	creds, err := b.accounts.CreateGuest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := b.client(t, creds)
	ctx := context.Background()
	if err := c.SaveIf(ctx, "s", []byte("one"), ""); err != nil {
		t.Fatal(err)
	}
	if err := c.SaveIf(ctx, "s", []byte("two"), ""); !errors.Is(err, liveops.ErrCloudChanged) {
		t.Fatalf("create over existing = %v", err)
	}
	if err := c.SaveIf(ctx, "s", []byte("two"), liveops.PayloadHash([]byte("one"))); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteIf(ctx, "s", liveops.PayloadHash([]byte("one"))); !errors.Is(err, liveops.ErrCloudChanged) {
		t.Fatalf("stale DeleteIf = %v", err)
	}
	if err := c.DeleteIf(ctx, "s", liveops.PayloadHash([]byte("two"))); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteIf(ctx, "s", ""); err != nil {
		t.Fatalf("DeleteIf of a missing slot = %v", err)
	}
	if err := c.Delete(ctx, "never"); err != nil {
		t.Fatalf("Delete of a missing slot = %v", err)
	}
	if _, err := c.Load(ctx, "s"); !errors.Is(err, liveops.ErrSlotNotFound) {
		t.Fatalf("Load of a deleted slot = %v", err)
	}
}

func TestCloudClientErrors(t *testing.T) {
	b := newBackend(t, func(c *cloudsave.Config) { c.MaxSlots = 1 })
	creds, err := b.accounts.CreateGuest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	c := b.client(t, creds)
	if err := c.Save(ctx, "one", []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(ctx, "two", []byte("2")); !errors.Is(err, liveops.ErrTooManySlots) {
		t.Fatalf("second slot = %v", err)
	}

	// A refused or missing token reads as signed out.
	bad, _ := liveops.NewCloudClient(liveops.CloudClientOptions{URL: "https://saves.example.com", HTTPClient: &http.Client{Transport: b},
		Token: func(context.Context) (string, error) { return "expired", nil }})
	if _, err := bad.List(ctx); !errors.Is(err, liveops.ErrCloudUnavailable) {
		t.Fatalf("bad token = %v", err)
	}
	none, _ := liveops.NewCloudClient(liveops.CloudClientOptions{URL: "https://saves.example.com", HTTPClient: &http.Client{Transport: b},
		Token: func(context.Context) (string, error) { return "", errors.New("signed out") }})
	if _, err := none.Load(ctx, "one"); !errors.Is(err, liveops.ErrCloudUnavailable) {
		t.Fatalf("no token = %v", err)
	}
	// A network failure is unavailable, and a done context is returned as it is.
	down, _ := liveops.NewCloudClient(liveops.CloudClientOptions{URL: "https://saves.example.com", Token: func(context.Context) (string, error) { return "t", nil },
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("connection refused") })}})
	if err := down.Save(ctx, "one", nil); !errors.Is(err, liveops.ErrCloudUnavailable) {
		t.Fatalf("network down = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.List(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled = %v", err)
	}
	// A payload that does not match its ETag is refused.
	tampered, _ := liveops.NewCloudClient(liveops.CloudClientOptions{URL: "https://saves.example.com", Token: func(context.Context) (string, error) { return "t", nil },
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			rec.Header().Set("ETag", `"`+liveops.PayloadHash([]byte("original"))+`"`)
			_, _ = rec.WriteString("tampered")
			return rec.Result(), nil
		})}})
	if _, err := tampered.Load(ctx, "one"); !errors.Is(err, liveops.ErrCloudUnavailable) {
		t.Fatalf("tampered payload = %v", err)
	}

	for _, u := range []string{"", "saves.example.com", "http://saves.example.com", "https://saves.example.com/?a=1", "ftp://saves.example.com"} {
		if _, err := liveops.NewCloudClient(liveops.CloudClientOptions{URL: u, Token: func(context.Context) (string, error) { return "", nil }}); err == nil {
			t.Errorf("NewCloudClient(%q) succeeded", u)
		}
	}
	for _, u := range []string{"http://localhost:8080", "http://127.0.0.1:9000/saves", "http://[::1]:80"} {
		if _, err := liveops.NewCloudClient(liveops.CloudClientOptions{URL: u, Token: func(context.Context) (string, error) { return "", nil }}); err != nil {
			t.Errorf("NewCloudClient(%q) = %v", u, err)
		}
	}
	if _, err := liveops.NewCloudClient(liveops.CloudClientOptions{URL: "https://saves.example.com"}); err == nil {
		t.Error("NewCloudClient without a token source succeeded")
	}

	// "." and ".." are not slots: as path segments they would address the listing.
	for _, slot := range []string{".", ".."} {
		if _, err := c.Load(ctx, slot); err == nil || errors.Is(err, liveops.ErrSlotNotFound) {
			t.Errorf("Load(%q) = %v", slot, err)
		}
		if err := c.Save(ctx, slot, []byte("x")); err == nil {
			t.Errorf("Save(%q) succeeded", slot)
		}
	}

	// A redirect is reported, not followed.
	requests := 0
	redirecting, _ := liveops.NewCloudClient(liveops.CloudClientOptions{URL: "https://saves.example.com", Token: func(context.Context) (string, error) { return "t", nil },
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			rec := httptest.NewRecorder()
			if r.URL.Path == "/v1/saves/one" {
				http.Redirect(rec, r, "/v1/saves", http.StatusMovedPermanently)
			} else {
				_, _ = rec.WriteString(`{"slots":[]}`)
			}
			return rec.Result(), nil
		})}})
	if _, err := redirecting.Load(ctx, "one"); !errors.Is(err, liveops.ErrCloudUnavailable) || requests != 1 {
		t.Fatalf("redirect = %v after %d requests", err, requests)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestCloudClientOverTLS runs one sync over a real HTTPS connection under a base path, as a deployment behind a reverse proxy would serve it.
func TestCloudClientOverTLS(t *testing.T) {
	b := newBackend(t, nil)
	mux := http.NewServeMux()
	mux.Handle("/saves/", http.StripPrefix("/saves", b.handler))
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	creds, err := b.accounts.CreateGuest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c, err := liveops.NewCloudClient(liveops.CloudClientOptions{URL: srv.URL + "/saves", HTTPClient: srv.Client(),
		Token: func(context.Context) (string, error) { return creds.Token.Reveal(), nil }})
	if err != nil {
		t.Fatal(err)
	}
	d := newDevice(t, "desktop", c, nil)
	d.save(t, "slot 1", progress{Level: 3, Note: "tls"}, 0)
	if got := outcomeOf(t, d.sync(t), "slot 1").Outcome; got != liveops.OutcomeUploaded {
		t.Fatalf("sync = %v", got)
	}
	if names, err := c.List(context.Background()); err != nil || len(names) != 1 || names[0] != "slot 1" {
		t.Fatalf("List = %v, %v", names, err)
	}
}

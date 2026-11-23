// Package storetest checks an accounts.Store implementation against the Store contract. A store's tests call Run with a function that opens an empty store.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/accounts"
)

// Run runs the contract's tests as subtests of t, each on a new empty store from open.
func Run(t *testing.T, open func(t *testing.T) accounts.Store) {
	tests := []struct {
		name string
		f    func(t *testing.T, s accounts.Store)
	}{
		{"CreateAndRead", testCreateAndRead},
		{"CreateIsAtomic", testCreateIsAtomic},
		{"SetEmail", testSetEmail},
		{"SetPasswordHash", testSetPasswordHash},
		{"LinkIdentity", testLinkIdentity},
		{"Sessions", testSessions},
		{"ConcurrentClaims", testConcurrentClaims},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.f(t, open(t)) })
	}
}

// at is a fixed time to the millisecond, as the service passes them.
var at = time.Date(2026, 11, 23, 10, 30, 0, 123_000_000, time.UTC)

func account(id, email string) accounts.Account {
	return accounts.Account{ID: id, Email: email, Created: at}
}

func mustCreate(t *testing.T, s accounts.Store, a accounts.Account, hash string, ids ...accounts.Identity) {
	t.Helper()
	if err := s.CreateAccount(context.Background(), a, hash, ids...); err != nil {
		t.Fatalf("CreateAccount(%s) = %v", a.ID, err)
	}
}

func wantErr(t *testing.T, what string, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

func testCreateAndRead(t *testing.T, s accounts.Store) {
	ctx := context.Background()
	mustCreate(t, s, account("a1", ""), "")
	mustCreate(t, s, account("a2", "ada@example.com"), "hash-2")
	got, err := s.Account(ctx, "a1")
	if err != nil || got != account("a1", "") {
		t.Fatalf("Account(a1) = %+v, %v", got, err)
	}
	if !got.Created.Equal(at) {
		t.Fatalf("Created = %v, want %v", got.Created, at)
	}
	got, hash, err := s.AccountByEmail(ctx, "ada@example.com")
	if err != nil || got.ID != "a2" || hash != "hash-2" {
		t.Fatalf("AccountByEmail = %+v, %q, %v", got, hash, err)
	}
	_, err = s.Account(ctx, "missing")
	wantErr(t, "Account(missing)", err, accounts.ErrNotFound)
	_, _, err = s.AccountByEmail(ctx, "nobody@example.com")
	wantErr(t, "AccountByEmail(unknown)", err, accounts.ErrNotFound)
	_, _, err = s.AccountByEmail(ctx, "")
	wantErr(t, "AccountByEmail(empty)", err, accounts.ErrNotFound)
	wantErr(t, "CreateAccount(duplicate id)", s.CreateAccount(ctx, account("a1", ""), ""), accounts.ErrTaken)
	wantErr(t, "CreateAccount(duplicate email)", s.CreateAccount(ctx, account("a3", "ada@example.com"), "h"), accounts.ErrTaken)
	if ids, err := s.Identities(ctx, "missing"); err != nil || len(ids) != 0 {
		t.Fatalf("Identities(missing) = %v, %v", ids, err)
	}
}

func testCreateIsAtomic(t *testing.T, s accounts.Store) {
	ctx := context.Background()
	steam := accounts.Identity{Provider: "steam", Subject: "765"}
	mustCreate(t, s, account("a1", ""), "", steam)
	gc := accounts.Identity{Provider: "gamecenter", Subject: "G:1"}
	wantErr(t, "CreateAccount(taken identity)", s.CreateAccount(ctx, account("a2", "lin@example.com"), "h", gc, steam), accounts.ErrTaken)
	_, err := s.Account(ctx, "a2")
	wantErr(t, "Account after a refused create", err, accounts.ErrNotFound)
	_, err = s.AccountByIdentity(ctx, gc)
	wantErr(t, "identity of a refused create", err, accounts.ErrNotFound)
	_, _, err = s.AccountByEmail(ctx, "lin@example.com")
	wantErr(t, "email of a refused create", err, accounts.ErrNotFound)
	got, err := s.AccountByIdentity(ctx, steam)
	if err != nil || got.ID != "a1" {
		t.Fatalf("AccountByIdentity(steam) = %+v, %v", got, err)
	}
}

func testSetEmail(t *testing.T, s accounts.Store) {
	ctx := context.Background()
	mustCreate(t, s, account("guest", ""), "")
	mustCreate(t, s, account("other", ""), "")
	if err := s.SetEmail(ctx, "guest", "ada@example.com", "hash"); err != nil {
		t.Fatalf("SetEmail = %v", err)
	}
	got, hash, err := s.AccountByEmail(ctx, "ada@example.com")
	if err != nil || got.ID != "guest" || hash != "hash" || !got.Created.Equal(at) {
		t.Fatalf("AccountByEmail after SetEmail = %+v, %q, %v", got, hash, err)
	}
	wantErr(t, "SetEmail(again)", s.SetEmail(ctx, "guest", "ada2@example.com", "hash"), accounts.ErrAlreadyLinked)
	wantErr(t, "SetEmail(taken)", s.SetEmail(ctx, "other", "ada@example.com", "hash"), accounts.ErrTaken)
	wantErr(t, "SetEmail(missing)", s.SetEmail(ctx, "missing", "x@example.com", "hash"), accounts.ErrNotFound)
	if a, _ := s.Account(ctx, "other"); a.Email != "" {
		t.Fatalf("a refused SetEmail changed the account: %+v", a)
	}
}

func testSetPasswordHash(t *testing.T, s accounts.Store) {
	ctx := context.Background()
	mustCreate(t, s, account("a1", "ada@example.com"), "old")
	mustCreate(t, s, account("guest", ""), "")
	if err := s.SetPasswordHash(ctx, "a1", "new"); err != nil {
		t.Fatalf("SetPasswordHash = %v", err)
	}
	if _, hash, _ := s.AccountByEmail(ctx, "ada@example.com"); hash != "new" {
		t.Fatalf("hash = %q, want new", hash)
	}
	wantErr(t, "SetPasswordHash(guest)", s.SetPasswordHash(ctx, "guest", "h"), accounts.ErrNotFound)
	wantErr(t, "SetPasswordHash(missing)", s.SetPasswordHash(ctx, "missing", "h"), accounts.ErrNotFound)
}

func testLinkIdentity(t *testing.T, s accounts.Store) {
	ctx := context.Background()
	mustCreate(t, s, account("a1", ""), "")
	mustCreate(t, s, account("a2", ""), "")
	steam := accounts.Identity{Provider: "steam", Subject: "765"}
	gc := accounts.Identity{Provider: "gamecenter", Subject: "G:1"}
	for _, id := range []accounts.Identity{steam, gc} {
		if err := s.LinkIdentity(ctx, "a1", id, at); err != nil {
			t.Fatalf("LinkIdentity(%v) = %v", id, err)
		}
	}
	if err := s.LinkIdentity(ctx, "a1", steam, at); err != nil {
		t.Fatalf("LinkIdentity(again) = %v, want nil", err)
	}
	wantErr(t, "LinkIdentity(other account)", s.LinkIdentity(ctx, "a2", steam, at), accounts.ErrTaken)
	wantErr(t, "LinkIdentity(second steam)", s.LinkIdentity(ctx, "a1", accounts.Identity{Provider: "steam", Subject: "999"}, at), accounts.ErrAlreadyLinked)
	wantErr(t, "LinkIdentity(missing)", s.LinkIdentity(ctx, "missing", accounts.Identity{Provider: "x", Subject: "y"}, at), accounts.ErrNotFound)
	got, err := s.AccountByIdentity(ctx, steam)
	if err != nil || got.ID != "a1" {
		t.Fatalf("AccountByIdentity = %+v, %v", got, err)
	}
	_, err = s.AccountByIdentity(ctx, accounts.Identity{Provider: "steam", Subject: "999"})
	wantErr(t, "AccountByIdentity(unknown)", err, accounts.ErrNotFound)
	ids, err := s.Identities(ctx, "a1")
	if err != nil || !slices.Equal(ids, []accounts.Identity{gc, steam}) {
		t.Fatalf("Identities = %v, %v; want gamecenter then steam", ids, err)
	}
	if ids, _ := s.Identities(ctx, "a2"); len(ids) != 0 {
		t.Fatalf("Identities(a2) = %v, want none", ids)
	}
}

func testSessions(t *testing.T, s accounts.Store) {
	ctx := context.Background()
	mustCreate(t, s, account("a1", ""), "")
	sess := accounts.Session{ID: "s1", AccountID: "a1", SecretHash: "abc", Created: at}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession = %v", err)
	}
	got, err := s.Session(ctx, "s1")
	if err != nil || got.ID != sess.ID || got.AccountID != sess.AccountID || got.SecretHash != sess.SecretHash || !got.Created.Equal(at) {
		t.Fatalf("Session = %+v, %v", got, err)
	}
	wantErr(t, "CreateSession(duplicate)", s.CreateSession(ctx, sess), accounts.ErrTaken)
	wantErr(t, "CreateSession(missing account)", s.CreateSession(ctx, accounts.Session{ID: "s2", AccountID: "missing", SecretHash: "x", Created: at}), accounts.ErrNotFound)
	if err := s.DeleteSession(ctx, "s1"); err != nil {
		t.Fatalf("DeleteSession = %v", err)
	}
	_, err = s.Session(ctx, "s1")
	wantErr(t, "Session after delete", err, accounts.ErrNotFound)
	wantErr(t, "DeleteSession(again)", s.DeleteSession(ctx, "s1"), accounts.ErrNotFound)
}

func testConcurrentClaims(t *testing.T, s accounts.Store) {
	ctx := context.Background()
	const n = 8
	for i := range n {
		mustCreate(t, s, account(fmt.Sprintf("a%d", i), ""), "")
	}
	steam := accounts.Identity{Provider: "steam", Subject: "765"}
	var wg sync.WaitGroup
	linkErrs := make([]error, n)
	emailErrs := make([]error, n)
	for i := range n {
		wg.Go(func() {
			id := fmt.Sprintf("a%d", i)
			linkErrs[i] = s.LinkIdentity(ctx, id, steam, at)
			emailErrs[i] = s.SetEmail(ctx, id, "ada@example.com", "hash")
		})
	}
	wg.Wait()
	for what, errs := range map[string][]error{"LinkIdentity": linkErrs, "SetEmail": emailErrs} {
		won := 0
		for _, err := range errs {
			switch {
			case err == nil:
				won++
			case !errors.Is(err, accounts.ErrTaken):
				t.Fatalf("%s race: %v, want nil or ErrTaken", what, err)
			}
		}
		if won != 1 {
			t.Fatalf("%s race: %d accounts won the claim, want 1", what, won)
		}
	}
}

//go:build !js && !wasip1

package sqlitestore_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/accounts"
	"github.com/nsgo-dev/game/pkg/accounts/sqlitestore"
	"github.com/nsgo-dev/game/pkg/accounts/storetest"
)

func TestContractInMemory(t *testing.T) {
	storetest.Run(t, func(t *testing.T) accounts.Store {
		s, err := sqlitestore.Open(t.Context(), ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}

func TestContractInFile(t *testing.T) {
	storetest.Run(t, func(t *testing.T) accounts.Store {
		s, err := sqlitestore.Open(t.Context(), filepath.Join(t.TempDir(), "accounts.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}

func TestReopenKeepsAccounts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, err := sqlitestore.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	if err := s.CreateAccount(ctx, accounts.Account{ID: "a1", Email: "ada@example.com", Created: created}, "hash", accounts.Identity{Provider: "steam", Subject: "765"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlitestore.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	a, err := s.AccountByIdentity(ctx, accounts.Identity{Provider: "steam", Subject: "765"})
	if err != nil || a.ID != "a1" || a.Email != "ada@example.com" || !a.Created.Equal(created) {
		t.Fatalf("after reopen: %+v, %v", a, err)
	}
}

func TestNewerSchemaIsRefused(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, err := sqlitestore.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE accounts_schema SET version = 99`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if s, err := sqlitestore.Open(ctx, path); err == nil {
		_ = s.Close()
		t.Fatal("opened a database with a newer schema")
	}
}

func TestOpenRefusesBadPaths(t *testing.T) {
	for _, p := range []string{"", "a.db?mode=ro", "a.db#x"} {
		if _, err := sqlitestore.Open(context.Background(), p); err == nil {
			t.Errorf("Open(%q) succeeded", p)
		}
	}
}

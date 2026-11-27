//go:build !js && !wasip1

package sqlitestore_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/cloudsave"
	"github.com/nsgo-dev/game/pkg/online/cloudsave/sqlitestore"
	"github.com/nsgo-dev/game/pkg/online/cloudsave/sqlstore"
	"github.com/nsgo-dev/game/pkg/online/cloudsave/storetest"
)

func openSQLite(t *testing.T) *sqlstore.Store {
	t.Helper()
	s, err := sqlitestore.Open(context.Background(), filepath.Join(t.TempDir(), "saves.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSQLiteContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) cloudsave.Store { return openSQLite(t) })
}

func TestSQLiteInMemory(t *testing.T) {
	s, err := sqlitestore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, err := s.Append(context.Background(), "a", "main", cloudsave.Write{Data: []byte("x"), Match: cloudsave.MatchAny, Keep: 1, MaxSlots: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteReopenKeepsSlots(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "saves.db")
	s, err := sqlitestore.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, "a", "main", cloudsave.Write{Data: []byte("kept"), Match: cloudsave.MatchAny, Keep: 2, MaxSlots: 4}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlitestore.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if v, err := s.Head(ctx, "a", "main"); err != nil || string(v.Data) != "kept" {
		t.Fatalf("reopened Head = %+v, %v", v, err)
	}
}

func TestSQLiteRefusesNewerSchema(t *testing.T) {
	ctx := context.Background()
	s := openSQLite(t)
	if _, err := s.DB().ExecContext(ctx, `UPDATE cloudsave_schema SET version = 2`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlstore.New(ctx, s.DB(), sqlstore.SQLite); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("New on a newer schema = %v", err)
	}
}

func TestOpenRefusesBadPaths(t *testing.T) {
	for _, p := range []string{"", "saves.db?mode=ro", "saves#1"} {
		if _, err := sqlitestore.Open(context.Background(), p); err == nil {
			t.Errorf("Open(%q) succeeded", p)
		}
	}
	if _, err := sqlstore.New(context.Background(), (*sql.DB)(nil), sqlstore.SQLite); err == nil {
		t.Error("New without a database succeeded")
	}
}

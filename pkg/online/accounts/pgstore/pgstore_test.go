package pgstore_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/accounts"
	"github.com/nsgo-dev/game/pkg/online/accounts/pgstore"
	"github.com/nsgo-dev/game/pkg/online/accounts/storetest"
)

// dsnEnv names the environment variable holding a Postgres URL the tests may create and drop schemas in. Without it the tests skip.
const dsnEnv = "ACCOUNTS_POSTGRES_DSN"

// openSchema opens a store in a new schema of the test database and drops the schema when the test ends.
func openSchema(t *testing.T) accounts.Store {
	t.Helper()
	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		t.Skipf("set %s to a Postgres URL to run the Postgres store tests", dsnEnv)
	}
	ctx := context.Background()
	admin, err := pgstore.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	var b [6]byte
	_, _ = rand.Read(b[:])
	schema := "accounts_test_" + hex.EncodeToString(b[:])
	if _, err := admin.DB().ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.DB().ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`) })
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasPrefix(u.Scheme, "postgres") {
		t.Fatalf("%s must be a postgres:// URL", dsnEnv)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, err := pgstore.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestContract(t *testing.T) {
	if os.Getenv(dsnEnv) == "" {
		t.Skipf("set %s to a Postgres URL to run the Postgres store tests", dsnEnv)
	}
	storetest.Run(t, openSchema)
}

func TestOpenRefusesBadDSNWithoutEchoingIt(t *testing.T) {
	const dsn = "postgres://user:s3cret-pass@[bad"
	_, err := pgstore.Open(context.Background(), dsn)
	if err == nil {
		t.Fatal("opened a malformed DSN")
	}
	if strings.Contains(err.Error(), "s3cret-pass") {
		t.Fatalf("error leaks the password: %v", err)
	}
}

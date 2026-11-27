//go:build !js && !wasip1

package sqlstore_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/cloudsave"
	"github.com/nsgo-dev/game/pkg/online/cloudsave/sqlstore"
	"github.com/nsgo-dev/game/pkg/online/cloudsave/storetest"
)

// dsnEnv names the environment variable holding a Postgres URL the tests may create and drop schemas in. Without it the Postgres tests skip.
const dsnEnv = "CLOUDSAVE_POSTGRES_DSN"

// openSchema opens a store in a new schema of the test database and drops the schema when the test ends.
func openSchema(t *testing.T) cloudsave.Store {
	t.Helper()
	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		t.Skipf("set %s to a Postgres URL to run the Postgres store tests", dsnEnv)
	}
	ctx := context.Background()
	admin, err := sqlstore.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	var b [6]byte
	_, _ = rand.Read(b[:])
	schema := "cloudsave_test_" + hex.EncodeToString(b[:])
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
	s, err := sqlstore.OpenPostgres(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPostgresContract(t *testing.T) {
	if os.Getenv(dsnEnv) == "" {
		t.Skipf("set %s to a Postgres URL to run the Postgres store tests", dsnEnv)
	}
	storetest.Run(t, openSchema)
}

func TestOpenPostgresHidesBadDSN(t *testing.T) {
	_, err := sqlstore.OpenPostgres(context.Background(), "postgres://user:secret@[bad")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("OpenPostgres with a bad DSN = %v", err)
	}
}

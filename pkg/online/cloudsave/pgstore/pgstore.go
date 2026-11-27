// Package pgstore opens a cloudsave.Store in a Postgres database with the github.com/jackc/pgx/v5 driver. Several cloud save service processes may share one database: each write takes an advisory lock on the account, and migrations take one on the schema.
package pgstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/nsgo-dev/game/pkg/online/cloudsave/sqlstore"
)

// Open connects to the Postgres database dsn names (a URL or a key=value string, as pgx parses it) and returns a store on it with the schema created. Several service processes may share the database. Keep the DSN, which may hold a password, out of logs and command lines: read it from the environment or a file. Close the store to close the connections.
func Open(ctx context.Context, dsn string) (*sqlstore.Store, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		// The parse error is not passed on: pgx can only redact the password from a malformed DSN on a best-effort basis.
		return nil, errors.New("pgstore: cannot parse the Postgres DSN")
	}
	db := stdlib.OpenDB(*cfg)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pgstore: connect: %w", err)
	}
	s, err := sqlstore.New(ctx, db, sqlstore.Postgres)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

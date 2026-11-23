package sqlstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// OpenPostgres connects to the Postgres database dsn names (a URL or a key=value string, as pgx parses it) and returns a store on it with the schema created. Several service processes may share the database. Keep the DSN, which may hold a password, out of logs and command lines: read it from the environment or a file. Close the store to close the connections.
func OpenPostgres(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		// The parse error is not passed on: pgx can only redact the password from a malformed DSN on a best-effort basis.
		return nil, errors.New("sqlstore: cannot parse the Postgres DSN")
	}
	db := stdlib.OpenDB(*cfg)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlstore: connect: %w", err)
	}
	s, err := New(ctx, db, Postgres)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

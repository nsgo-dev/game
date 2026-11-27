// Package pgstore opens an accounts.Store in a Postgres database with the github.com/jackc/pgx/v5 driver. Several account service processes may share one database: its constraints keep email addresses and identities unique, and migrations take an advisory lock.
package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/nsgo-dev/game/pkg/online/accounts/sqlstore"
)

// uniqueViolation is Postgres's SQLSTATE for a violated UNIQUE or PRIMARY KEY constraint.
const uniqueViolation = "23505"

// Dialect is Postgres's sqlstore dialect.
var Dialect = sqlstore.Dialect{
	Name:     "postgres",
	Numbered: true,
	IsUnique: func(err error) bool {
		var e *pgconn.PgError
		return errors.As(err, &e) && e.Code == uniqueViolation
	},
	// An arbitrary constant key ("acct" in ASCII) shared by every process migrating the accounts schema.
	MigrateLock: `SELECT pg_advisory_xact_lock(1633903476)`,
}

// Open connects to the Postgres database dsn names (a URL or a key=value string, as pgx parses it) and returns a store on it with the schema created. Keep the DSN, which may hold a password, out of logs and command lines: read it from the environment or a file. Close the store to close the connections.
func Open(ctx context.Context, dsn string) (*sqlstore.Store, error) {
	cfg, err := pgconnConfig(dsn)
	if err != nil {
		return nil, err
	}
	db := stdlib.OpenDB(*cfg)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pgstore: connect: %w", err)
	}
	s, err := New(ctx, db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// New returns a store on an open Postgres database, creating the schema when it is missing.
func New(ctx context.Context, db *sql.DB) (*sqlstore.Store, error) {
	return sqlstore.New(ctx, db, Dialect)
}

// pgconnConfig parses dsn. The parse error is not passed on: pgx can only redact the password from a malformed DSN on a best-effort basis.
func pgconnConfig(dsn string) (*pgx.ConnConfig, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("pgstore: cannot parse the DSN")
	}
	return cfg, nil
}

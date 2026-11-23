//go:build !js && !wasip1

// Package sqlitestore opens an accounts.Store in a SQLite database file, with the pure-Go modernc.org/sqlite driver (no cgo). It suits a single account service process; several processes sharing one database should use Postgres (package pgstore).
//
// The package builds for every target but the web: a browser has no file system to keep the database in, and the account service runs on a server.
package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"modernc.org/sqlite"

	"github.com/nsgo-dev/game/packages/game/accounts/sqlstore"
)

// SQLite's extended result codes for a violated UNIQUE or PRIMARY KEY constraint.
const (
	constraintPrimaryKey = 1555
	constraintUnique     = 2067
)

// Dialect is SQLite's sqlstore dialect.
var Dialect = sqlstore.Dialect{
	Name: "sqlite",
	IsUnique: func(err error) bool {
		var e *sqlite.Error
		return errors.As(err, &e) && (e.Code() == constraintUnique || e.Code() == constraintPrimaryKey)
	},
}

// Open opens (creating when missing) the SQLite database at path, or a private in-memory database for ":memory:", and returns a store on it with the schema created. The database uses write-ahead logging, enforces foreign keys and waits up to five seconds for a lock another process holds.
//
// The store uses one connection, so its statements run one at a time; SQLite serializes writes anyway. Close the store to close the database.
func Open(ctx context.Context, path string) (*sqlstore.Store, error) {
	if path == "" || strings.ContainsAny(path, "?#") {
		return nil, fmt.Errorf("sqlitestore: invalid database path %q", path)
	}
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
	if path != ":memory:" {
		dsn += "&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlitestore: %w", err)
	}
	// One connection: an in-memory database exists per connection, and a file database takes one writer at a time.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlitestore: %w", err)
	}
	s, err := sqlstore.New(ctx, db, Dialect)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

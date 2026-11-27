//go:build !js && !wasip1

// Package sqlitestore opens a cloudsave.Store in a SQLite database file, with the pure-Go modernc.org/sqlite driver (no cgo). It suits a single cloud save service process; several processes sharing one database should use Postgres (package pgstore).
//
// The package builds for every target but the web: a browser has no file system to keep the database in, and the cloud save service runs on a server.
package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	// The pure-Go SQLite driver, registered as "sqlite".
	_ "modernc.org/sqlite"

	"github.com/nsgo-dev/game/pkg/online/cloudsave/sqlstore"
)

// Open opens (creating when missing) the SQLite database at path, or a private in-memory database for ":memory:", and returns a store on it with the schema created. The database uses write-ahead logging and waits up to five seconds for a lock another process holds.
//
// The store uses one connection with immediate transactions, so its writes run one at a time. It suits a single service process; several processes sharing one database should use Postgres.
func Open(ctx context.Context, path string) (*sqlstore.Store, error) {
	if path == "" || strings.ContainsAny(path, "?#") {
		return nil, fmt.Errorf("sqlitestore: invalid SQLite database path %q", path)
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_txlock=immediate"
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
	s, err := sqlstore.New(ctx, db, sqlstore.SQLite)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

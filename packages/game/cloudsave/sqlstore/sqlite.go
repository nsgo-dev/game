//go:build !js && !wasip1

package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	// The pure-Go SQLite driver, registered as "sqlite".
	_ "modernc.org/sqlite"
)

// OpenSQLite opens (creating when missing) the SQLite database at path, or a private in-memory database for ":memory:", and returns a store on it with the schema created. The database uses write-ahead logging and waits up to five seconds for a lock another process holds.
//
// The store uses one connection with immediate transactions, so its writes run one at a time. It suits a single service process; several processes sharing one database should use Postgres. OpenSQLite is not available on the web, which has no file system to keep the database in.
func OpenSQLite(ctx context.Context, path string) (*Store, error) {
	if path == "" || strings.ContainsAny(path, "?#") {
		return nil, fmt.Errorf("sqlstore: invalid SQLite database path %q", path)
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_txlock=immediate"
	if path != ":memory:" {
		dsn += "&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: %w", err)
	}
	// One connection: an in-memory database exists per connection, and a file database takes one writer at a time.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlstore: %w", err)
	}
	s, err := New(ctx, db, SQLite)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

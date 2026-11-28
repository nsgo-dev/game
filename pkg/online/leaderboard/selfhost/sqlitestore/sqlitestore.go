//go:build !js && !wasip1

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost/sqlstore"
	_ "modernc.org/sqlite"
	"strings"
)

func Open(ctx context.Context, path string) (*sqlstore.Store, error) {
	if path == "" || strings.ContainsAny(path, "?#") {
		return nil, fmt.Errorf("leaderboard sqlitestore: invalid path %q", path)
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_txlock=immediate"
	if path != ":memory:" {
		dsn += "&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	s, err := sqlstore.New(ctx, db, sqlstore.SQLite)
	if err != nil {
		db.Close()
	}
	return s, err
}

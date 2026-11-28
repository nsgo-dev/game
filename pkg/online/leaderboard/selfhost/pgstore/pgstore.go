//go:build !js && !wasip1

package pgstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost/sqlstore"
)

func Open(ctx context.Context, dsn string) (*sqlstore.Store, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("leaderboard pgstore: cannot parse Postgres DSN")
	}
	db := stdlib.OpenDB(*cfg)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("leaderboard pgstore: connect: %w", err)
	}
	s, err := sqlstore.New(ctx, db, sqlstore.Postgres)
	if err != nil {
		db.Close()
	}
	return s, err
}

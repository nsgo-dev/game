// Package sqlstore persists self-hosted leaderboard records in SQLite or Postgres.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost"
)

const SchemaVersion = 1

type Dialect struct {
	Name, Blob              string
	Numbered                bool
	MigrateLock, LockRecord string
}

var SQLite = Dialect{Name: "sqlite", Blob: "BLOB"}
var Postgres = Dialect{Name: "postgres", Blob: "BIGINT", Numbered: true, MigrateLock: `SELECT pg_advisory_xact_lock(16801680)`, LockRecord: `SELECT pg_advisory_xact_lock(16801680, hashtext(?))`}

type Store struct {
	db     *sql.DB
	d      Dialect
	orders map[string]selfhost.Order
}

var _ selfhost.Store = (*Store)(nil)

func New(ctx context.Context, db *sql.DB, d Dialect) (*Store, error) {
	if db == nil || d.Name == "" || d.Blob == "" {
		return nil, errors.New("leaderboard sqlstore: need database and dialect")
	}
	s := &Store{db: db, d: d, orders: map[string]selfhost.Order{}}
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) DB() *sql.DB  { return s.db }
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Configure(boards []selfhost.Board) {
	for _, b := range boards {
		s.orders[b.Name] = b.Order
	}
}
func (s *Store) q(q string) string {
	if !s.d.Numbered {
		return q
	}
	n := 0
	out := make([]byte, 0, len(q))
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			out = append(out, '$')
			out = append(out, fmt.Sprint(n)...)
		} else {
			out = append(out, q[i])
		}
	}
	return string(out)
}
func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("leaderboard sqlstore: migrate: %w", err)
	}
	defer tx.Rollback()
	if s.d.MigrateLock != "" {
		if _, err = tx.ExecContext(ctx, s.d.MigrateLock); err != nil {
			return fmt.Errorf("leaderboard sqlstore: migrate: %w", err)
		}
	}
	stmts := []string{`CREATE TABLE IF NOT EXISTS leaderboard_schema (version INTEGER NOT NULL)`, `CREATE TABLE IF NOT EXISTS leaderboard_records (board TEXT NOT NULL, period TEXT NOT NULL, account TEXT NOT NULL, score BIGINT NOT NULL, achieved BIGINT NOT NULL, PRIMARY KEY(board,period,account))`, `CREATE INDEX IF NOT EXISTS leaderboard_order ON leaderboard_records(board,period,score,achieved,account)`}
	for _, st := range stmts {
		if _, err = tx.ExecContext(ctx, st); err != nil {
			return fmt.Errorf("leaderboard sqlstore: migrate: %w", err)
		}
	}
	var v int
	err = tx.QueryRowContext(ctx, `SELECT version FROM leaderboard_schema`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, s.q(`INSERT INTO leaderboard_schema(version) VALUES (?)`), SchemaVersion)
	} else if err == nil && v > SchemaVersion {
		return fmt.Errorf("leaderboard sqlstore: schema version %d is newer than %d", v, SchemaVersion)
	}
	if err != nil {
		return fmt.Errorf("leaderboard sqlstore: migrate: %w", err)
	}
	return tx.Commit()
}
func (s *Store) Load(ctx context.Context, fn func(selfhost.Record) error) error {
	rows, err := s.db.QueryContext(ctx, `SELECT board,period,account,score,achieved FROM leaderboard_records ORDER BY board,period,account`)
	if err != nil {
		return fmt.Errorf("leaderboard sqlstore: load: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var b, p, a string
		var score, ns int64
		if err := rows.Scan(&b, &p, &a, &score, &ns); err != nil {
			return fmt.Errorf("leaderboard sqlstore: load: %w", err)
		}
		if err := fn(selfhost.Record{Board: b, Period: p, Account: a, Score: score, Achieved: time.Unix(0, ns).UTC()}); err != nil {
			return err
		}
	}
	return rows.Err()
}
func (s *Store) Save(ctx context.Context, rec selfhost.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("leaderboard sqlstore: save: %w", err)
	}
	defer tx.Rollback()
	if s.d.LockRecord != "" {
		key := rec.Board + "/" + rec.Period + "/" + rec.Account
		if _, err = tx.ExecContext(ctx, s.q(s.d.LockRecord), key); err != nil {
			return fmt.Errorf("leaderboard sqlstore: save: %w", err)
		}
	}
	var oldScore, oldAch int64
	err = tx.QueryRowContext(ctx, s.q(`SELECT score,achieved FROM leaderboard_records WHERE board=? AND period=? AND account=?`), rec.Board, rec.Period, rec.Account).Scan(&oldScore, &oldAch)
	if err == nil {
		order := s.orders[rec.Board]
		better := rec.Score > oldScore
		if order == selfhost.Ascending {
			better = rec.Score < oldScore
		}
		if !better {
			return tx.Commit()
		}
		_, err = tx.ExecContext(ctx, s.q(`UPDATE leaderboard_records SET score=?,achieved=? WHERE board=? AND period=? AND account=?`), rec.Score, rec.Achieved.UnixNano(), rec.Board, rec.Period, rec.Account)
	} else if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, s.q(`INSERT INTO leaderboard_records(board,period,account,score,achieved) VALUES (?,?,?,?,?)`), rec.Board, rec.Period, rec.Account, rec.Score, rec.Achieved.UnixNano())
	}
	if err != nil {
		return fmt.Errorf("leaderboard sqlstore: save: %w", err)
	}
	return tx.Commit()
}
func (s *Store) Drop(ctx context.Context, board, period string) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM leaderboard_records WHERE board=? AND period=?`), board, period)
	if err != nil {
		return fmt.Errorf("leaderboard sqlstore: drop: %w", err)
	}
	return nil
}

// Records returns records in deterministic order and is intended for diagnostics and tests.
func (s *Store) Records(ctx context.Context, board, period string) ([]selfhost.Record, error) {
	var out []selfhost.Record
	err := s.Load(ctx, func(r selfhost.Record) error {
		if r.Board == board && r.Period == period {
			out = append(out, r)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Account < out[j].Account })
	return out, err
}

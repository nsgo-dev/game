// Package sqlstore is a cloudsave.Store over database/sql, for SQLite (OpenSQLite, with the pure-Go modernc.org/sqlite driver) and Postgres (OpenPostgres, with github.com/jackc/pgx/v5).
//
// The schema is two tables, created by New when missing and versioned in cloudsave_schema:
//   - cloudsave_slots: account_id and slot (the key), and the newest version's number, deleted flag, hash, size and created_at
//   - cloudsave_versions: account_id, slot and version (the key), the same metadata and the payload in data
//
// Times are Unix milliseconds. Each write runs in one transaction that compares and sets the slot's newest version number, so of two concurrent writes based on the same version at most one commits; on Postgres it also takes a transaction-level advisory lock on the account, so the slot count and pruning see every concurrent write of that account.
package sqlstore

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/cloudsave"
)

// SchemaVersion is the schema version New creates and accepts.
const SchemaVersion = 1

// Dialect is what differs between SQL databases.
type Dialect struct {
	// Name names the database in errors.
	Name string
	// Numbered is true for "$1" placeholders, false for "?".
	Numbered bool
	// Blob is the column type of a byte string, such as BLOB or BYTEA.
	Blob string
	// MigrateLock, when set, runs first in the migration's transaction to serialize migrations of several processes.
	MigrateLock string
	// LockAccount, when set, runs first in each write's transaction with the account ID as its one parameter, to serialize the writes of one account across connections and processes.
	LockAccount string
}

// SQLite is SQLite's dialect. A SQLite store uses one connection with immediate transactions (OpenSQLite), which serializes writes without an account lock.
var SQLite = Dialect{Name: "sqlite", Blob: "BLOB"}

// Postgres is Postgres's dialect. Its locks use the arbitrary class key 1668051830 ("clsv" in ASCII).
var Postgres = Dialect{
	Name:        "postgres",
	Numbered:    true,
	Blob:        "BYTEA",
	MigrateLock: `SELECT pg_advisory_xact_lock(1668051830)`,
	LockAccount: `SELECT pg_advisory_xact_lock(1668051830, hashtext(?))`,
}

// Store is a cloudsave.Store in a SQL database.
type Store struct {
	db *sql.DB
	d  Dialect
}

var _ cloudsave.Store = (*Store)(nil)

func schema(d Dialect) []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS cloudsave_schema (version INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS cloudsave_slots (
			account_id TEXT NOT NULL,
			slot TEXT NOT NULL,
			version BIGINT NOT NULL,
			deleted INTEGER NOT NULL,
			hash TEXT NOT NULL,
			size BIGINT NOT NULL,
			created_at BIGINT NOT NULL,
			PRIMARY KEY (account_id, slot)
		)`,
		`CREATE TABLE IF NOT EXISTS cloudsave_versions (
			account_id TEXT NOT NULL,
			slot TEXT NOT NULL,
			version BIGINT NOT NULL,
			deleted INTEGER NOT NULL,
			hash TEXT NOT NULL,
			size BIGINT NOT NULL,
			created_at BIGINT NOT NULL,
			data ` + d.Blob + ` NOT NULL,
			PRIMARY KEY (account_id, slot, version)
		)`,
	}
}

// New returns a store on db, creating the schema when it is missing. It refuses a database whose schema is newer than SchemaVersion.
func New(ctx context.Context, db *sql.DB, d Dialect) (*Store, error) {
	if db == nil || d.Name == "" || d.Blob == "" {
		return nil, errors.New("sqlstore: need a database and a dialect with a name and a blob type")
	}
	s := &Store{db: db, d: d}
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return s.wrap("migrate", err)
	}
	defer func() { _ = tx.Rollback() }()
	if s.d.MigrateLock != "" {
		if _, err := tx.ExecContext(ctx, s.d.MigrateLock); err != nil {
			return s.wrap("migrate", err)
		}
	}
	for _, stmt := range schema(s.d) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return s.wrap("migrate", err)
		}
	}
	var version int
	err = tx.QueryRowContext(ctx, `SELECT version FROM cloudsave_schema`).Scan(&version)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO cloudsave_schema (version) VALUES (?)`), SchemaVersion); err != nil {
			return s.wrap("migrate", err)
		}
	case err != nil:
		return s.wrap("migrate", err)
	case version > SchemaVersion:
		return fmt.Errorf("sqlstore: %s schema version %d is newer than %d", s.d.Name, version, SchemaVersion)
	}
	return s.wrap("migrate", tx.Commit())
}

// DB returns the store's database.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the store's database.
func (s *Store) Close() error { return s.db.Close() }

// Head implements cloudsave.Store. It reads the slot's head and its data in one statement, so an Append that commits in between and prunes that version cannot make a live slot look missing.
func (s *Store) Head(ctx context.Context, account, slot string) (cloudsave.Version, error) {
	row := s.db.QueryRowContext(ctx, s.q(`SELECT v.version, v.deleted, v.hash, v.size, v.created_at, v.data FROM cloudsave_slots s JOIN cloudsave_versions v ON v.account_id = s.account_id AND v.slot = s.slot AND v.version = s.version WHERE s.account_id = ? AND s.slot = ?`), account, slot)
	return s.scanVersion(row, slot, "head")
}

// Version implements cloudsave.Store.
func (s *Store) Version(ctx context.Context, account, slot string, number int64) (cloudsave.Version, error) {
	row := s.db.QueryRowContext(ctx, s.q(`SELECT version, deleted, hash, size, created_at, data FROM cloudsave_versions WHERE account_id = ? AND slot = ? AND version = ?`), account, slot, number)
	return s.scanVersion(row, slot, "version")
}

// scanVersion scans one version row with its data, returning ErrNotFound when there is none.
func (s *Store) scanVersion(row *sql.Row, slot, op string) (cloudsave.Version, error) {
	v := cloudsave.Version{Slot: slot}
	var deleted, created int64
	if err := row.Scan(&v.Number, &deleted, &v.Hash, &v.Size, &created, &v.Data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cloudsave.Version{}, cloudsave.ErrNotFound
		}
		return cloudsave.Version{}, s.wrap(op, err)
	}
	v.Deleted, v.Created = deleted != 0, time.UnixMilli(created).UTC()
	if v.Deleted {
		v.Data = nil
	} else if v.Data == nil {
		v.Data = []byte{}
	}
	return v, nil
}

// Versions implements cloudsave.Store.
func (s *Store) Versions(ctx context.Context, account, slot string) ([]cloudsave.Version, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT slot, version, deleted, hash, size, created_at FROM cloudsave_versions WHERE account_id = ? AND slot = ? ORDER BY version DESC`), account, slot)
	if err != nil {
		return nil, s.wrap("versions", err)
	}
	out, err := scanVersions(rows)
	if err != nil {
		return nil, s.wrap("versions", err)
	}
	if len(out) == 0 {
		return nil, cloudsave.ErrNotFound
	}
	return out, nil
}

// Slots implements cloudsave.Store.
func (s *Store) Slots(ctx context.Context, account string) ([]cloudsave.Version, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT slot, version, deleted, hash, size, created_at FROM cloudsave_slots WHERE account_id = ?`), account)
	if err != nil {
		return nil, s.wrap("slots", err)
	}
	out, err := scanVersions(rows)
	if err != nil {
		return nil, s.wrap("slots", err)
	}
	// Sorted here rather than by the database, whose text collation may not be byte order.
	slices.SortFunc(out, func(a, b cloudsave.Version) int { return cmp.Compare(a.Slot, b.Slot) })
	return out, nil
}

// Append implements cloudsave.Store.
func (s *Store) Append(ctx context.Context, account, slot string, w cloudsave.Write) (cloudsave.Version, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cloudsave.Version{}, s.wrap("append", err)
	}
	defer func() { _ = tx.Rollback() }()
	v, err := s.append(ctx, tx, account, slot, w)
	if err != nil {
		if errors.Is(err, cloudsave.ErrChanged) || errors.Is(err, cloudsave.ErrNotFound) || errors.Is(err, cloudsave.ErrTooManySlots) {
			return cloudsave.Version{}, err
		}
		return cloudsave.Version{}, s.wrap("append", err)
	}
	if err := tx.Commit(); err != nil {
		return cloudsave.Version{}, s.wrap("append", err)
	}
	return v, nil
}

func (s *Store) append(ctx context.Context, tx *sql.Tx, account, slot string, w cloudsave.Write) (cloudsave.Version, error) {
	if s.d.LockAccount != "" {
		if _, err := tx.ExecContext(ctx, s.q(s.d.LockAccount), account); err != nil {
			return cloudsave.Version{}, err
		}
	}
	head, ok, err := s.head(ctx, tx, account, slot)
	if err != nil {
		return cloudsave.Version{}, err
	}
	if !w.Match.Holds(head, ok) {
		return cloudsave.Version{}, cloudsave.ErrChanged
	}
	live := ok && !head.Deleted
	if w.Delete && !live {
		return cloudsave.Version{}, cloudsave.ErrNotFound
	}
	if !w.Delete && !live {
		var n int
		if err := tx.QueryRowContext(ctx, s.q(`SELECT COUNT(*) FROM cloudsave_slots WHERE account_id = ? AND deleted = 0`), account).Scan(&n); err != nil {
			return cloudsave.Version{}, err
		}
		if n >= w.MaxSlots {
			return cloudsave.Version{}, cloudsave.ErrTooManySlots
		}
	}
	v := cloudsave.Version{Slot: slot, Number: head.Number + 1, Deleted: w.Delete, Created: w.Created.UTC().Truncate(time.Millisecond)}
	data := []byte{}
	if !w.Delete {
		if w.Data != nil {
			data = w.Data
		}
		v.Hash, v.Size = cloudsave.Hash(data), int64(len(data))
	}
	deleted, created := boolInt(v.Deleted), v.Created.UnixMilli()
	var res sql.Result
	if ok {
		res, err = tx.ExecContext(ctx, s.q(`UPDATE cloudsave_slots SET version = ?, deleted = ?, hash = ?, size = ?, created_at = ? WHERE account_id = ? AND slot = ? AND version = ?`),
			v.Number, deleted, v.Hash, v.Size, created, account, slot, head.Number)
	} else {
		res, err = tx.ExecContext(ctx, s.q(`INSERT INTO cloudsave_slots (account_id, slot, version, deleted, hash, size, created_at) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`),
			account, slot, v.Number, deleted, v.Hash, v.Size, created)
	}
	if err != nil {
		return cloudsave.Version{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return cloudsave.Version{}, err
	} else if n != 1 {
		// Another transaction moved the slot on between the read and this write.
		return cloudsave.Version{}, cloudsave.ErrChanged
	}
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO cloudsave_versions (account_id, slot, version, deleted, hash, size, created_at, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
		account, slot, v.Number, deleted, v.Hash, v.Size, created, data); err != nil {
		return cloudsave.Version{}, err
	}
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM cloudsave_versions WHERE account_id = ? AND slot = ? AND version <= ?`), account, slot, v.Number-int64(max(w.Keep, 1))); err != nil {
		return cloudsave.Version{}, err
	}
	if w.Delete {
		if err := s.purgeDeleted(ctx, tx, account, w.MaxSlots); err != nil {
			return cloudsave.Version{}, err
		}
	}
	return v, nil
}

// purgeDeleted removes the account's oldest deleted slots beyond limit.
func (s *Store) purgeDeleted(ctx context.Context, tx *sql.Tx, account string, limit int) error {
	rows, err := tx.QueryContext(ctx, s.q(`SELECT slot, version, deleted, hash, size, created_at FROM cloudsave_slots WHERE account_id = ? AND deleted = 1`), account)
	if err != nil {
		return err
	}
	deleted, err := scanVersions(rows)
	if err != nil || len(deleted) <= limit {
		return err
	}
	slices.SortFunc(deleted, func(a, b cloudsave.Version) int {
		if c := a.Created.Compare(b.Created); c != 0 {
			return c
		}
		return cmp.Compare(a.Slot, b.Slot)
	})
	for _, h := range deleted[:len(deleted)-limit] {
		if err := s.purge(ctx, tx, account, h.Slot); err != nil {
			return err
		}
	}
	return nil
}

// Purge implements cloudsave.Store.
func (s *Store) Purge(ctx context.Context, account, slot string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return s.wrap("purge", err)
	}
	defer func() { _ = tx.Rollback() }()
	if s.d.LockAccount != "" {
		if _, err := tx.ExecContext(ctx, s.q(s.d.LockAccount), account); err != nil {
			return s.wrap("purge", err)
		}
	}
	if err := s.purge(ctx, tx, account, slot); err != nil {
		return s.wrap("purge", err)
	}
	return s.wrap("purge", tx.Commit())
}

func (s *Store) purge(ctx context.Context, tx *sql.Tx, account, slot string) error {
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM cloudsave_versions WHERE account_id = ? AND slot = ?`), account, slot); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, s.q(`DELETE FROM cloudsave_slots WHERE account_id = ? AND slot = ?`), account, slot)
	return err
}

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// head reads the newest version's metadata of slot; ok is false when it has none.
func (s *Store) head(ctx context.Context, q querier, account, slot string) (cloudsave.Version, bool, error) {
	row := q.QueryRowContext(ctx, s.q(`SELECT version, deleted, hash, size, created_at FROM cloudsave_slots WHERE account_id = ? AND slot = ?`), account, slot)
	v := cloudsave.Version{Slot: slot}
	var deleted, created int64
	if err := row.Scan(&v.Number, &deleted, &v.Hash, &v.Size, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cloudsave.Version{}, false, nil
		}
		return cloudsave.Version{}, false, err
	}
	v.Deleted, v.Created = deleted != 0, time.UnixMilli(created).UTC()
	return v, true, nil
}

func scanVersions(rows *sql.Rows) ([]cloudsave.Version, error) {
	defer func() { _ = rows.Close() }()
	var out []cloudsave.Version
	for rows.Next() {
		var v cloudsave.Version
		var deleted, created int64
		if err := rows.Scan(&v.Slot, &v.Number, &deleted, &v.Hash, &v.Size, &created); err != nil {
			return nil, err
		}
		v.Deleted, v.Created = deleted != 0, time.UnixMilli(created).UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// q rewrites "?" placeholders for the dialect.
func (s *Store) q(query string) string {
	if !s.d.Numbered {
		return query
	}
	var b strings.Builder
	n := 0
	for _, c := range query {
		if c == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

func (s *Store) wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("sqlstore: %s %s: %w", s.d.Name, op, err)
}

// Package sqlstore is an accounts.Store over database/sql, shared by the SQLite (package sqlitestore) and Postgres (package pgstore) stores. Open a store with one of those; use this package directly only to add another SQL database through a Dialect.
//
// The schema is three tables, created by New when missing and versioned in accounts_schema:
//   - accounts: id, email (unique, null for none), password_hash, created_at
//   - account_identities: provider and subject (the key), account_id (unique per provider), created_at
//   - account_sessions: id, account_id, secret_hash, created_at
//
// Times are Unix milliseconds. Uniqueness is enforced by the database's constraints, so concurrent services sharing one database cannot give one email address or identity to two accounts.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts"
)

// SchemaVersion is the schema version New creates and accepts.
const SchemaVersion = 1

// Dialect is what differs between SQL databases.
type Dialect struct {
	// Name names the database in errors.
	Name string
	// Numbered is true for "$1" placeholders, false for "?".
	Numbered bool
	// IsUnique reports whether err is a unique or primary key constraint violation.
	IsUnique func(err error) bool
	// MigrateLock, when set, runs first in the migration's transaction to serialize migrations of several processes, such as Postgres's pg_advisory_xact_lock.
	MigrateLock string
}

// Store is an accounts.Store in a SQL database.
type Store struct {
	db *sql.DB
	d  Dialect
}

var _ accounts.Store = (*Store)(nil)

var schema = []string{
	`CREATE TABLE IF NOT EXISTS accounts_schema (version INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS accounts (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE,
		password_hash TEXT NOT NULL DEFAULT '',
		created_at BIGINT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS account_identities (
		provider TEXT NOT NULL,
		subject TEXT NOT NULL,
		account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		created_at BIGINT NOT NULL,
		PRIMARY KEY (provider, subject),
		UNIQUE (account_id, provider)
	)`,
	`CREATE TABLE IF NOT EXISTS account_sessions (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		secret_hash TEXT NOT NULL,
		created_at BIGINT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS account_sessions_account ON account_sessions (account_id)`,
}

// New returns a store on db, creating the schema when it is missing. It refuses a database whose schema is newer than SchemaVersion.
func New(ctx context.Context, db *sql.DB, d Dialect) (*Store, error) {
	if db == nil || d.IsUnique == nil {
		return nil, errors.New("sqlstore: need a database and a dialect with IsUnique")
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
	for _, stmt := range schema {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return s.wrap("migrate", err)
		}
	}
	var version int
	err = tx.QueryRowContext(ctx, `SELECT version FROM accounts_schema`).Scan(&version)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO accounts_schema (version) VALUES (?)`), SchemaVersion); err != nil {
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

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func millis(t time.Time) int64 { return t.UnixMilli() }

func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// CreateAccount implements accounts.Store.
func (s *Store) CreateAccount(ctx context.Context, a accounts.Account, passwordHash string, identities ...accounts.Identity) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return s.wrap("create account", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO accounts (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)`),
		a.ID, nullable(a.Email), passwordHash, millis(a.Created)); err != nil {
		if s.d.IsUnique(err) {
			return accounts.ErrTaken
		}
		return s.wrap("create account", err)
	}
	for _, id := range identities {
		if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO account_identities (provider, subject, account_id, created_at) VALUES (?, ?, ?, ?)`),
			id.Provider, id.Subject, a.ID, millis(a.Created)); err != nil {
			if s.d.IsUnique(err) {
				return accounts.ErrTaken
			}
			return s.wrap("create account", err)
		}
	}
	return s.wrap("create account", tx.Commit())
}

func (s *Store) scanAccount(row *sql.Row) (accounts.Account, string, error) {
	var a accounts.Account
	var email sql.NullString
	var hash string
	var created int64
	if err := row.Scan(&a.ID, &email, &hash, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return accounts.Account{}, "", accounts.ErrNotFound
		}
		return accounts.Account{}, "", s.wrap("read account", err)
	}
	a.Email = email.String
	a.Created = fromMillis(created)
	return a, hash, nil
}

// Account implements accounts.Store.
func (s *Store) Account(ctx context.Context, id string) (accounts.Account, error) {
	a, _, err := s.scanAccount(s.db.QueryRowContext(ctx, s.q(`SELECT id, email, password_hash, created_at FROM accounts WHERE id = ?`), id))
	return a, err
}

// AccountByEmail implements accounts.Store.
func (s *Store) AccountByEmail(ctx context.Context, email string) (accounts.Account, string, error) {
	if email == "" {
		return accounts.Account{}, "", accounts.ErrNotFound
	}
	return s.scanAccount(s.db.QueryRowContext(ctx, s.q(`SELECT id, email, password_hash, created_at FROM accounts WHERE email = ?`), email))
}

// SetEmail implements accounts.Store.
func (s *Store) SetEmail(ctx context.Context, id, email, passwordHash string) error {
	if email == "" {
		return accounts.ErrInvalidEmail
	}
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE accounts SET email = ?, password_hash = ? WHERE id = ? AND email IS NULL`), email, passwordHash, id)
	if err != nil {
		if s.d.IsUnique(err) {
			return accounts.ErrTaken
		}
		return s.wrap("set email", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return s.wrap("set email", err)
	} else if n == 1 {
		return nil
	}
	if _, err := s.Account(ctx, id); err != nil {
		return err
	}
	return accounts.ErrAlreadyLinked
}

// SetPasswordHash implements accounts.Store.
func (s *Store) SetPasswordHash(ctx context.Context, id, passwordHash string) error {
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE accounts SET password_hash = ? WHERE id = ? AND email IS NOT NULL`), passwordHash, id)
	if err != nil {
		return s.wrap("set password hash", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return s.wrap("set password hash", err)
	}
	if n == 0 {
		return accounts.ErrNotFound
	}
	return nil
}

// LinkIdentity implements accounts.Store.
func (s *Store) LinkIdentity(ctx context.Context, id string, identity accounts.Identity, at time.Time) error {
	if _, err := s.Account(ctx, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, s.q(`INSERT INTO account_identities (provider, subject, account_id, created_at) VALUES (?, ?, ?, ?)`),
		identity.Provider, identity.Subject, id, millis(at))
	if err == nil {
		return nil
	}
	if !s.d.IsUnique(err) {
		return s.wrap("link identity", err)
	}
	owner, err := s.AccountByIdentity(ctx, identity)
	switch {
	case err == nil && owner.ID == id:
		return nil
	case err == nil:
		return accounts.ErrTaken
	case errors.Is(err, accounts.ErrNotFound):
		// The (account, provider) constraint refused it: the account has another identity of the provider.
		return accounts.ErrAlreadyLinked
	default:
		return err
	}
}

// UnlinkIdentity implements accounts.Store.
func (s *Store) UnlinkIdentity(ctx context.Context, id, provider string) error {
	if _, err := s.Account(ctx, id); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, s.q(`DELETE FROM account_identities WHERE account_id = ? AND provider = ?`), id, provider)
	if err != nil {
		return s.wrap("unlink identity", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return s.wrap("unlink identity", err)
	}
	if n == 0 {
		return accounts.ErrNotFound
	}
	return nil
}

// AccountByIdentity implements accounts.Store.
func (s *Store) AccountByIdentity(ctx context.Context, identity accounts.Identity) (accounts.Account, error) {
	a, _, err := s.scanAccount(s.db.QueryRowContext(ctx, s.q(`SELECT a.id, a.email, a.password_hash, a.created_at FROM accounts a
		JOIN account_identities i ON i.account_id = a.id WHERE i.provider = ? AND i.subject = ?`), identity.Provider, identity.Subject))
	return a, err
}

// Identities implements accounts.Store.
func (s *Store) Identities(ctx context.Context, id string) ([]accounts.Identity, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT provider, subject FROM account_identities WHERE account_id = ? ORDER BY provider`), id)
	if err != nil {
		return nil, s.wrap("identities", err)
	}
	defer func() { _ = rows.Close() }()
	var out []accounts.Identity
	for rows.Next() {
		var i accounts.Identity
		if err := rows.Scan(&i.Provider, &i.Subject); err != nil {
			return nil, s.wrap("identities", err)
		}
		out = append(out, i)
	}
	return out, s.wrap("identities", rows.Err())
}

// CreateSession implements accounts.Store.
func (s *Store) CreateSession(ctx context.Context, sess accounts.Session) error {
	if _, err := s.Account(ctx, sess.AccountID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, s.q(`INSERT INTO account_sessions (id, account_id, secret_hash, created_at) VALUES (?, ?, ?, ?)`),
		sess.ID, sess.AccountID, sess.SecretHash, millis(sess.Created))
	if err != nil {
		if s.d.IsUnique(err) {
			return accounts.ErrTaken
		}
		return s.wrap("create session", err)
	}
	return nil
}

// Session implements accounts.Store.
func (s *Store) Session(ctx context.Context, id string) (accounts.Session, error) {
	var sess accounts.Session
	var created int64
	err := s.db.QueryRowContext(ctx, s.q(`SELECT id, account_id, secret_hash, created_at FROM account_sessions WHERE id = ?`), id).
		Scan(&sess.ID, &sess.AccountID, &sess.SecretHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.Session{}, accounts.ErrNotFound
	}
	if err != nil {
		return accounts.Session{}, s.wrap("session", err)
	}
	sess.Created = fromMillis(created)
	return sess, nil
}

// DeleteSession implements accounts.Store.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.q(`DELETE FROM account_sessions WHERE id = ?`), id)
	if err != nil {
		return s.wrap("delete session", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return s.wrap("delete session", err)
	}
	if n == 0 {
		return accounts.ErrNotFound
	}
	return nil
}

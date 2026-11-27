//go:build !js && !wasip1

// Command accountsd runs the account service as a standalone HTTP server, with guest and email accounts in SQLite or Postgres.
//
//	accountsd init   -config accounts.yaml -issuer https://accounts.example.com -audience my-game
//	accountsd genkey -config accounts.yaml -key key.pem -keyset keys.yaml
//	accountsd serve  -config accounts.yaml -key key.pem -keyset keys.yaml -sqlite accounts.db -addr :8080
//	ACCOUNTS_POSTGRES_DSN=postgres://... accountsd serve -config accounts.yaml -key key.pem -keyset keys.yaml -postgres
//
// init writes a configuration with the default settings. genkey writes a new signing key and adds its public half to the key set file a game server verifies tokens with, keeping the keys already there; it replaces the key set file atomically, so a reader never sees half of it. Rotating a key is genkey with a new -key file, a restart on it once game servers have the new key set, and removing the old key from the key set once its tokens have expired. serve runs the service until SIGINT or SIGTERM; with -keyset it also accepts the tokens of every key in the key set and serves them at GET /v1/keys, so tokens signed before a rotation keep working on the service too.
//
// Identity providers (Game Center, Play Games, Steam, OAuth) need their verifiers and platform credentials, so a game that links identities builds its own command around accounts.New with Options.Identities; this one serves guests and email accounts.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts"
	"github.com/nsgo-dev/game/pkg/online/accounts/pgstore"
	"github.com/nsgo-dev/game/pkg/online/accounts/sqlitestore"
	"github.com/nsgo-dev/game/pkg/online/accounts/token"
)

// dsnEnv is the environment variable serve -postgres reads the database URL from, so the password stays off the command line.
const dsnEnv = "ACCOUNTS_POSTGRES_DSN"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "accountsd:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: accountsd init|genkey|serve [flags]")
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], stderr)
	case "genkey":
		return runGenkey(args[1:], stderr)
	case "serve":
		return runServe(ctx, args[1:], stderr)
	}
	return fmt.Errorf("unknown command %q: want init, genkey or serve", args[0])
}

func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fl := flag.NewFlagSet("accountsd "+name, flag.ContinueOnError)
	fl.SetOutput(stderr)
	return fl
}

func runInit(args []string, stderr io.Writer) error {
	fl := newFlags("init", stderr)
	configPath := fl.String("config", "accounts.yaml", "configuration file to write")
	issuer := fl.String("issuer", "", "issuer of the tokens, usually the service's URL")
	audience := fl.String("audience", "", "the game the tokens are for")
	if err := fl.Parse(args); err != nil {
		return err
	}
	data, err := accounts.DefaultConfig(*issuer, *audience).Marshal()
	if err != nil {
		return err
	}
	return writeNew(*configPath, data, 0o644)
}

func runGenkey(args []string, stderr io.Writer) error {
	fl := newFlags("genkey", stderr)
	configPath := fl.String("config", "accounts.yaml", "configuration file")
	keyPath := fl.String("key", "key.pem", "private key file to write; it must not exist")
	keySetPath := fl.String("keyset", "keys.yaml", "key set file to write or add the public key to")
	if err := fl.Parse(args); err != nil {
		return err
	}
	cfg, err := readConfig(*configPath)
	if err != nil {
		return err
	}
	key, err := token.GenerateKey(nil)
	if err != nil {
		return err
	}
	signer, err := token.NewSigner(key, cfg.Issuer, cfg.Audience, time.Duration(cfg.TokenTTL))
	if err != nil {
		return err
	}
	ks := signer.KeySet()
	if old, err := os.ReadFile(*keySetPath); err == nil {
		existing, err := token.ParseKeySet(old)
		if err != nil {
			return fmt.Errorf("%s: %w", *keySetPath, err)
		}
		if ks, err = existing.Merge(ks); err != nil {
			return fmt.Errorf("%s: %w", *keySetPath, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	pemData, err := token.MarshalPrivateKey(key)
	if err != nil {
		return err
	}
	ksData, err := ks.Marshal()
	if err != nil {
		return err
	}
	if err := writeNew(*keyPath, pemData, 0o600); err != nil {
		return err
	}
	if err := writeAtomic(*keySetPath, ksData, 0o644); err != nil {
		// Without its public half published the key is useless; remove it so genkey can run again with the same -key.
		_ = os.Remove(*keyPath)
		return err
	}
	return nil
}

func runServe(ctx context.Context, args []string, stderr io.Writer) error {
	fl := newFlags("serve", stderr)
	configPath := fl.String("config", "accounts.yaml", "configuration file")
	keyPath := fl.String("key", "key.pem", "private signing key file")
	keySetPath := fl.String("keyset", "", "key set file whose keys the service also accepts and serves, such as the one genkey keeps")
	sqlitePath := fl.String("sqlite", "", "SQLite database file")
	usePostgres := fl.Bool("postgres", false, "use the Postgres database in $"+dsnEnv)
	addr := fl.String("addr", ":8080", "address to listen on")
	if err := fl.Parse(args); err != nil {
		return err
	}
	cfg, err := readConfig(*configPath)
	if err != nil {
		return err
	}
	pemData, err := os.ReadFile(*keyPath)
	if err != nil {
		return err
	}
	key, err := token.ParsePrivateKey(pemData)
	if err != nil {
		return fmt.Errorf("%s: %w", *keyPath, err)
	}
	var keySet *token.KeySet
	if *keySetPath != "" {
		data, err := os.ReadFile(*keySetPath)
		if err != nil {
			return err
		}
		if keySet, err = token.ParseKeySet(data); err != nil {
			return fmt.Errorf("%s: %w", *keySetPath, err)
		}
	}
	var store interface {
		accounts.Store
		Close() error
	}
	switch {
	case *sqlitePath != "" && *usePostgres:
		return errors.New("choose -sqlite or -postgres, not both")
	case *sqlitePath != "":
		store, err = sqlitestore.Open(ctx, *sqlitePath)
	case *usePostgres:
		dsn := os.Getenv(dsnEnv)
		if dsn == "" {
			return fmt.Errorf("-postgres needs $%s", dsnEnv)
		}
		store, err = pgstore.Open(ctx, dsn)
	default:
		return errors.New("choose a database with -sqlite or -postgres")
	}
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	log := slog.New(slog.NewJSONHandler(stderr, nil))
	svc, err := accounts.New(cfg, accounts.Options{Store: store, Key: key, KeySet: keySet, Logger: log})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           svc.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("accountsd: serving", "addr", *addr, "key", svc.KeySet().Keys[0].ID)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

func readConfig(path string) (accounts.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return accounts.Config{}, err
	}
	cfg, err := accounts.ParseConfig(data)
	if err != nil {
		return accounts.Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// writeAtomic replaces path with data through a temporary file in the same directory and a rename, so a reader sees the old file or the new one, never a partial one.
func writeAtomic(path string, data []byte, perm fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// writeNew writes a file that must not exist yet, so a key or configuration is never overwritten.
func writeNew(path string, data []byte, perm fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

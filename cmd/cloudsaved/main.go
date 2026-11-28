//go:build !js && !wasip1

// Command cloudsaved runs the cloud save service as a standalone HTTP server, with slots in SQLite or Postgres.
//
//	cloudsaved init  -config cloudsave.yaml
//	cloudsaved serve -config cloudsave.yaml -keyset keys.yaml -sqlite saves.db -addr :8081
//	CLOUDSAVE_POSTGRES_DSN=postgres://... cloudsaved serve -config cloudsave.yaml -keyset keys.yaml -postgres
//
// init writes a configuration with the default settings. serve runs the service until SIGINT or SIGTERM. It verifies players' tokens with the key set file of the account service (the one accountsd genkey keeps), which it reads once at start, so restart it after adding a key to that file.
//
// Put the server behind TLS and a rate limiter, as for accountsd.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts/authtoken"
	"github.com/nsgo-dev/game/pkg/online/cloudsave"
	"github.com/nsgo-dev/game/pkg/online/cloudsave/sqlstore"
)

// dsnEnv is the environment variable serve -postgres reads the database URL from, so the password stays off the command line.
const dsnEnv = "CLOUDSAVE_POSTGRES_DSN"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stderr, nil); err != nil {
		fmt.Fprintln(os.Stderr, "cloudsaved:", err)
		os.Exit(1)
	}
}

// run runs a command. ready, when set, receives the address serve listens on once it accepts connections.
func run(ctx context.Context, args []string, stderr io.Writer, ready func(addr string)) error {
	if len(args) == 0 {
		return errors.New("usage: cloudsaved init|serve [flags]")
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], stderr)
	case "serve":
		return runServe(ctx, args[1:], stderr, ready)
	}
	return fmt.Errorf("unknown command %q: want init or serve", args[0])
}

func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fl := flag.NewFlagSet("cloudsaved "+name, flag.ContinueOnError)
	fl.SetOutput(stderr)
	return fl
}

func runInit(args []string, stderr io.Writer) error {
	fl := newFlags("init", stderr)
	configPath := fl.String("config", "cloudsave.yaml", "configuration file to write")
	if err := fl.Parse(args); err != nil {
		return err
	}
	data, err := cloudsave.DefaultConfig().Marshal()
	if err != nil {
		return err
	}
	return writeNew(*configPath, data, 0o644)
}

func runServe(ctx context.Context, args []string, stderr io.Writer, ready func(string)) error {
	fl := newFlags("serve", stderr)
	configPath := fl.String("config", "cloudsave.yaml", "configuration file")
	keySetPath := fl.String("keyset", "keys.yaml", "the account service's key set file, to verify players' tokens with")
	sqlitePath := fl.String("sqlite", "", "SQLite database file")
	usePostgres := fl.Bool("postgres", false, "use the Postgres database in $"+dsnEnv)
	addr := fl.String("addr", ":8081", "address to listen on")
	if err := fl.Parse(args); err != nil {
		return err
	}
	data, err := os.ReadFile(*configPath)
	if err != nil {
		return err
	}
	cfg, err := cloudsave.ParseConfig(data)
	if err != nil {
		return fmt.Errorf("%s: %w", *configPath, err)
	}
	ksData, err := os.ReadFile(*keySetPath)
	if err != nil {
		return err
	}
	ks, err := authtoken.ParseKeySet(ksData)
	if err != nil {
		return fmt.Errorf("%s: %w", *keySetPath, err)
	}
	verifier, err := authtoken.NewVerifier(ks)
	if err != nil {
		return fmt.Errorf("%s: %w", *keySetPath, err)
	}
	var store *sqlstore.Store
	switch {
	case *sqlitePath != "" && *usePostgres:
		return errors.New("choose -sqlite or -postgres, not both")
	case *sqlitePath != "":
		store, err = sqlstore.OpenSQLite(ctx, *sqlitePath)
	case *usePostgres:
		dsn := os.Getenv(dsnEnv)
		if dsn == "" {
			return fmt.Errorf("-postgres needs $%s", dsnEnv)
		}
		store, err = sqlstore.OpenPostgres(ctx, dsn)
	default:
		return errors.New("choose a database with -sqlite or -postgres")
	}
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	log := slog.New(slog.NewJSONHandler(stderr, nil))
	svc, err := cloudsave.New(cfg, cloudsave.Options{Store: store, Tokens: verifier, Logger: log})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           svc.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		// A slot of up to 64 MiB has to arrive in time on a slow mobile link.
		ReadTimeout:    2 * time.Minute,
		WriteTimeout:   2 * time.Minute,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 16 << 10,
		ErrorLog:       slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	ln, err := new(net.ListenConfig).Listen(ctx, "tcp", *addr)
	if err != nil {
		return err
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Info("cloudsaved: serving", "addr", ln.Addr().String(), "maxSlotBytes", cfg.MaxSlotBytes, "maxSlots", cfg.MaxSlots, "keepVersions", cfg.KeepVersions)
	if ready != nil {
		ready(ln.Addr().String())
	}
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

// writeNew writes a file that must not exist yet, so a configuration is never overwritten.
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

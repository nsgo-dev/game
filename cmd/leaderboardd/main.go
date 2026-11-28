//go:build !js && !wasip1

// Command leaderboardd runs the self-hosted leaderboard service as a standalone HTTP server, with its scores in a directory of JSON Lines files.
//
//	leaderboardd init   -config leaderboards.yaml
//	leaderboardd genkey -key server.pem
//	leaderboardd serve  -config leaderboards.yaml -keyset keys.yaml -data scores -addr :8081
//
// init writes a configuration with one all-time high-score board to edit. genkey writes a new game server signing key for score proofs and prints the serverKeys entry to add to the configuration; the game server reads the key with authtoken.ParsePrivateKey and signs with selfhost.NewProofSigner. serve runs the service until SIGINT or SIGTERM, verifying player tokens with the account service's key set file (accountsd genkey writes it). It accepts only tokens for the configuration's audience ("leaderboard" by default), which devices get from the account service's refresh, and refuses to start when that audience is the key set's, which names the game's servers.
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
	"syscall"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts/authtoken"
	"github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "leaderboardd:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: leaderboardd init|genkey|serve [flags]")
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], stderr)
	case "genkey":
		return runGenkey(args[1:], stdout, stderr)
	case "serve":
		return runServe(ctx, args[1:], stderr)
	}
	return fmt.Errorf("unknown command %q: want init, genkey or serve", args[0])
}

func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fl := flag.NewFlagSet("leaderboardd "+name, flag.ContinueOnError)
	fl.SetOutput(stderr)
	return fl
}

// starterConfig is the configuration init writes.
func starterConfig() selfhost.Config {
	return selfhost.Config{
		Version:  selfhost.ConfigVersion,
		Audience: selfhost.DefaultAudience,
		Boards:   []selfhost.Board{{Name: "high-score", Order: selfhost.Descending, Reset: selfhost.AllTime}},
	}
}

func runInit(args []string, stderr io.Writer) error {
	fl := newFlags("init", stderr)
	configPath := fl.String("config", "leaderboards.yaml", "configuration file to write")
	if err := fl.Parse(args); err != nil {
		return err
	}
	data, err := starterConfig().Marshal()
	if err != nil {
		return err
	}
	return writeNew(*configPath, data, 0o644)
}

func runGenkey(args []string, stdout, stderr io.Writer) error {
	fl := newFlags("genkey", stderr)
	keyPath := fl.String("key", "server.pem", "private key file to write; it must not exist")
	if err := fl.Parse(args); err != nil {
		return err
	}
	key, err := authtoken.GenerateKey(nil)
	if err != nil {
		return err
	}
	signer, err := selfhost.NewProofSigner(key)
	if err != nil {
		return err
	}
	pemData, err := authtoken.MarshalPrivateKey(key)
	if err != nil {
		return err
	}
	if err := writeNew(*keyPath, pemData, 0o600); err != nil {
		return err
	}
	pub := signer.PublicKey()
	_, err = fmt.Fprintf(stdout, "# Add to serverKeys in the leaderboard configuration:\n  - id: %s\n    key: %s\n", pub.ID, pub.Key)
	return err
}

// service builds the service serve runs from its files.
func service(ctx context.Context, configPath, keySetPath, dataDir string, syncWrites bool, log *slog.Logger) (*selfhost.Service, *selfhost.FileStore, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := selfhost.ParseConfig(data)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", configPath, err)
	}
	if keySetPath == "" {
		return nil, nil, errors.New("-keyset is required: the account service's key set file")
	}
	ksData, err := os.ReadFile(keySetPath)
	if err != nil {
		return nil, nil, err
	}
	ks, err := authtoken.ParseKeySet(ksData)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", keySetPath, err)
	}
	audience := cfg.TokenAudience()
	if audience == ks.Audience {
		return nil, nil, fmt.Errorf("%s: audience %q is the game servers' audience in %s; set audience to one the account service lists in its audiences, such as %q", configPath, audience, keySetPath, selfhost.DefaultAudience)
	}
	verifier, err := authtoken.NewVerifierFor(ks, audience)
	if err != nil {
		return nil, nil, err
	}
	verifier.Leeway = 5 * time.Second
	if dataDir == "" {
		return nil, nil, errors.New("-data is required: the directory scores are kept in")
	}
	store, err := selfhost.OpenFileStore(dataDir, selfhost.FileStoreOptions{Sync: syncWrites})
	if err != nil {
		return nil, nil, err
	}
	svc, err := selfhost.New(ctx, cfg, selfhost.Options{Store: store, Tokens: verifier, Logger: log, Leeway: 5 * time.Second})
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	return svc, store, nil
}

func runServe(ctx context.Context, args []string, stderr io.Writer) error {
	fl := newFlags("serve", stderr)
	configPath := fl.String("config", "leaderboards.yaml", "configuration file")
	keySetPath := fl.String("keyset", "", "the account service's key set file, to verify player tokens with")
	dataDir := fl.String("data", "", "directory to keep scores in")
	addr := fl.String("addr", ":8081", "address to listen on")
	syncWrites := fl.Bool("sync", false, "flush every score to the disk before acknowledging it, for a machine that may lose power")
	if err := fl.Parse(args); err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(stderr, nil))
	svc, store, err := service(ctx, *configPath, *keySetPath, *dataDir, *syncWrites, log)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
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
	log.Info("leaderboardd: serving", "addr", *addr, "boards", len(svc.Config().Boards))
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
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

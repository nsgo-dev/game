//go:build !js && !wasip1

package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts/authtoken"
	"github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost"
)

func TestInitGenkeyAndServe(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "leaderboards.yaml")
	keyPath := filepath.Join(dir, "server.pem")
	ctx := context.Background()
	if err := run(ctx, []string{"init", "-config", cfgPath}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{"init", "-config", cfgPath}, io.Discard, io.Discard); err == nil {
		t.Fatal("init overwrote the configuration")
	}
	var out bytes.Buffer
	if err := run(ctx, []string{"genkey", "-key", keyPath}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{"genkey", "-key", keyPath}, io.Discard, io.Discard); err == nil {
		t.Fatal("genkey overwrote a key")
	}
	info, err := os.Stat(keyPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %v, %v", info, err)
	}

	// Turn the starter board into a signed one and paste the printed key entry, as the output says.
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(cfgData), "reset: allTime\n", "reset: allTime\n    validation: signed\n", 1) + "serverKeys:\n" + strings.SplitN(out.String(), "\n", 2)[1]
	if err := os.WriteFile(cfgPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	// The account service's signing key and the key set file it publishes.
	accountKey, err := authtoken.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authtoken.NewSigner(accountKey, "https://accounts.example.com", "my-game", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ksData, err := signer.KeySet().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	ksPath := filepath.Join(dir, "keys.yaml")
	if err := os.WriteFile(ksPath, ksData, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, bad := range [][3]string{{cfgPath, "", "data"}, {cfgPath, ksPath, ""}, {filepath.Join(dir, "missing.yaml"), ksPath, "data"}} {
		if _, _, err := service(ctx, bad[0], bad[1], bad[2], false, nil); err == nil {
			t.Fatalf("service(%q) succeeded", bad)
		}
	}
	svc, store, err := service(ctx, cfgPath, ksPath, filepath.Join(dir, "scores"), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	pemData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := authtoken.ParsePrivateKey(pemData)
	if err != nil {
		t.Fatal(err)
	}
	prover, err := selfhost.NewProofSigner(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	tok, claims, err := signer.IssueFor("acct-1", selfhost.DefaultAudience)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := prover.Sign("high-score", claims.Subject, 4200)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/boards/high-score/scores", strings.NewReader(`{"score":4200,"proof":"`+proof+`"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("submit = %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "scores", "high-score", "all.jsonl")); err != nil {
		t.Fatalf("score not written: %v", err)
	}
}

func TestRunRefusesUnknownCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"launch"}} {
		if err := run(context.Background(), args, io.Discard, io.Discard); err == nil {
			t.Errorf("run(%q) succeeded", args)
		}
	}
}

// TestServeRefusesGameServerToken checks that a token a game server receives from a joining player cannot submit scores for that player: leaderboardd accepts only tokens issued for its own audience.
func TestServeRefusesGameServerToken(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "leaderboards.yaml")
	ctx := context.Background()
	if err := run(ctx, []string{"init", "-config", cfgPath}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	accountKey, err := authtoken.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authtoken.NewSigner(accountKey, "https://accounts.example.com", "my-game", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ksData, err := signer.KeySet().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	ksPath := filepath.Join(dir, "keys.yaml")
	if err := os.WriteFile(ksPath, ksData, 0o644); err != nil {
		t.Fatal(err)
	}
	svc, store, err := service(ctx, cfgPath, ksPath, filepath.Join(dir, "scores"), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	gameTok, _, err := signer.Issue("acct-1")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/boards/high-score/scores", strings.NewReader(`{"score":4200}`))
	req.Header.Set("Authorization", "Bearer "+gameTok)
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("submit with a game server token = %d %s, want 401", rec.Code, rec.Body)
	}

	// A configuration naming the game servers' audience would accept their tokens again, so serve refuses it.
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(strings.Replace(string(cfgData), "audience: leaderboard\n", "audience: my-game\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service(ctx, cfgPath, ksPath, filepath.Join(dir, "scores"), false, nil); err == nil || !strings.Contains(err.Error(), "game servers' audience") {
		t.Fatalf("service with the game servers' audience = %v", err)
	}
}

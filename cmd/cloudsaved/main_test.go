//go:build !js && !wasip1

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/accounts/authtoken"
	"github.com/nsgo-dev/game/pkg/online/cloudsave"
)

func TestInitWritesDefaultConfigOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloudsave.yaml")
	if err := run(context.Background(), []string{"init", "-config", path}, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := cloudsave.DefaultConfig().Marshal()
	if !bytes.Equal(data, want) {
		t.Fatalf("config = %q", data)
	}
	if err := run(context.Background(), []string{"init", "-config", path}, io.Discard, nil); err == nil {
		t.Fatal("init overwrote an existing config")
	}
}

func TestServeAnswersWithSQLite(t *testing.T) {
	dir := t.TempDir()
	cfgPath, ksPath := filepath.Join(dir, "cloudsave.yaml"), filepath.Join(dir, "keys.yaml")
	if err := run(context.Background(), []string{"init", "-config", cfgPath}, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	key, err := authtoken.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authtoken.NewSigner(key, "https://accounts.example.com", "game", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ks, err := signer.KeySet().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ksPath, ks, 0o644); err != nil {
		t.Fatal(err)
	}
	tok, _, err := signer.IssueFor("acct", cloudsave.DefaultAudience)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	addrc := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"serve", "-config", cfgPath, "-keyset", ksPath, "-sqlite", filepath.Join(dir, "saves.db"), "-addr", "127.0.0.1:0"}, io.Discard, func(a string) { addrc <- a })
	}()
	var addr string
	select {
	case addr = <-addrc:
	case err := <-done:
		t.Fatalf("serve stopped: %v", err)
	}
	req, _ := http.NewRequest(http.MethodPut, "http://"+addr+"/v1/saves/main", strings.NewReader("hello"))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("If-None-Match", "*")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT = %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, "http://"+addr+"/v1/saves/main", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "hello" {
		t.Fatalf("GET = %d %q", resp.StatusCode, body)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve after shutdown = %v", err)
	}
}

func TestServeRefusesBadSetups(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cloudsave.yaml")
	if err := run(context.Background(), []string{"init", "-config", cfgPath}, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	key, _ := authtoken.GenerateKey(nil)
	signer, _ := authtoken.NewSigner(key, "https://accounts.example.com", "game", time.Minute)
	ks, _ := signer.KeySet().Marshal()
	ksPath := filepath.Join(dir, "keys.yaml")
	if err := os.WriteFile(ksPath, ks, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(dsnEnv, "")
	cases := map[string][]string{
		"no database":   {"serve", "-config", cfgPath, "-keyset", ksPath},
		"two databases": {"serve", "-config", cfgPath, "-keyset", ksPath, "-sqlite", "x.db", "-postgres"},
		"no dsn":        {"serve", "-config", cfgPath, "-keyset", ksPath, "-postgres"},
		"no key set":    {"serve", "-config", cfgPath, "-keyset", filepath.Join(dir, "missing.yaml"), "-sqlite", "x.db"},
		"no config":     {"serve", "-config", filepath.Join(dir, "missing.yaml"), "-keyset", ksPath, "-sqlite", "x.db"},
		"no command":    {},
		"bad command":   {"start"},
	}
	for name, args := range cases {
		if err := run(context.Background(), args, io.Discard, nil); err == nil || errors.Is(err, context.Canceled) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestServeRefusesGameServerToken checks that a token a game server receives from a joining player cannot reach that player's saves: cloudsaved accepts only tokens issued for its own audience.
func TestServeRefusesGameServerToken(t *testing.T) {
	dir := t.TempDir()
	cfgPath, ksPath := filepath.Join(dir, "cloudsave.yaml"), filepath.Join(dir, "keys.yaml")
	if err := run(context.Background(), []string{"init", "-config", cfgPath}, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	key, err := authtoken.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authtoken.NewSigner(key, "https://accounts.example.com", "game", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ks, err := signer.KeySet().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ksPath, ks, 0o644); err != nil {
		t.Fatal(err)
	}
	gameTok, _, err := signer.Issue("acct")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	addrc := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"serve", "-config", cfgPath, "-keyset", ksPath, "-sqlite", filepath.Join(dir, "saves.db"), "-addr", "127.0.0.1:0"}, io.Discard, func(a string) { addrc <- a })
	}()
	var addr string
	select {
	case addr = <-addrc:
	case err := <-done:
		t.Fatalf("serve stopped: %v", err)
	}
	for _, r := range []struct{ method, path string }{
		{http.MethodPut, "/v1/saves/main"},
		{http.MethodGet, "/v1/saves"},
		{http.MethodDelete, "/v1/saves/main/versions"},
	} {
		req, _ := http.NewRequest(r.method, "http://"+addr+r.path, strings.NewReader("overwritten"))
		req.Header.Set("Authorization", "Bearer "+gameTok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s with a game server token = %d, want 401", r.method, r.path, resp.StatusCode)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve after shutdown = %v", err)
	}

	// A configuration naming the game servers' audience would accept their tokens again, so serve refuses it.
	if err := os.WriteFile(cfgPath, []byte("version: 1\naudience: game\nmaxSlotBytes: 1048576\nmaxSlots: 32\nkeepVersions: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = run(context.Background(), []string{"serve", "-config", cfgPath, "-keyset", ksPath, "-sqlite", filepath.Join(dir, "saves.db"), "-addr", "127.0.0.1:0"}, io.Discard, func(string) { t.Error("serve started with the game servers' audience") })
	if err == nil || !strings.Contains(err.Error(), "game servers' audience") {
		t.Fatalf("serve with the game servers' audience = %v", err)
	}
}

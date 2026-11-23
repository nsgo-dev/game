//go:build !js && !wasip1

package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/accounts/token"
)

func TestInitAndKeyRotation(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "accounts.yaml")
	keys := filepath.Join(dir, "keys.yaml")
	ctx := context.Background()
	if err := run(ctx, []string{"init", "-config", cfg, "-issuer", "https://accounts.example.com", "-audience", "my-game"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{"init", "-config", cfg, "-issuer", "x", "-audience", "y"}, io.Discard); err == nil {
		t.Fatal("init overwrote the configuration")
	}
	for i, key := range []string{"key-1.pem", "key-2.pem"} {
		if err := run(ctx, []string{"genkey", "-config", cfg, "-key", filepath.Join(dir, key), "-keyset", keys}, io.Discard); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(keys)
		if err != nil {
			t.Fatal(err)
		}
		ks, err := token.ParseKeySet(data)
		if err != nil || len(ks.Keys) != i+1 || ks.Issuer != "https://accounts.example.com" {
			t.Fatalf("key set after %s: %+v, %v", key, ks, err)
		}
		info, err := os.Stat(filepath.Join(dir, key))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, %v", key, info.Mode(), err)
		}
	}
	if err := run(ctx, []string{"genkey", "-config", cfg, "-key", filepath.Join(dir, "key-1.pem"), "-keyset", keys}, io.Discard); err == nil {
		t.Fatal("genkey overwrote a key")
	}
}

func TestServeNeedsADatabase(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "accounts.yaml")
	key := filepath.Join(dir, "key.pem")
	ctx := context.Background()
	if err := run(ctx, []string{"init", "-config", cfg, "-issuer", "i", "-audience", "a"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{"genkey", "-config", cfg, "-key", key, "-keyset", filepath.Join(dir, "keys.yaml")}, io.Discard); err != nil {
		t.Fatal(err)
	}
	t.Setenv(dsnEnv, "")
	for _, args := range [][]string{
		{"serve", "-config", cfg, "-key", key},
		{"serve", "-config", cfg, "-key", key, "-postgres"},
		{"serve", "-config", cfg, "-key", key, "-postgres", "-sqlite", filepath.Join(dir, "a.db")},
		{"serve", "-config", cfg, "-key", filepath.Join(dir, "missing.pem"), "-sqlite", filepath.Join(dir, "a.db")},
		{"bogus"},
		{},
	} {
		if err := run(ctx, args, io.Discard); err == nil {
			t.Errorf("run(%v) succeeded", args)
		}
	}
}

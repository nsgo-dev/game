package selfhost_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/leaderboard/selfhost"
)

func openFileStore(t *testing.T, dir string) *selfhost.FileStore {
	t.Helper()
	fs, err := selfhost.OpenFileStore(dir, selfhost.FileStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	return fs
}

func TestFileStoreRestoresBoardsAfterARestart(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	fs, err := selfhost.OpenFileStore(dir, selfhost.FileStoreOptions{Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	svc := newService(t, fs, c)
	submit(t, svc, "high-score", "ada", 300)
	c.advance(time.Millisecond)
	submit(t, svc, "high-score", "bob", 300)
	submit(t, svc, "high-score", "ada", 200) // not better, not written
	submit(t, svc, "weekly", "ada", 7)
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "high-score", "all.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"account":"ada","score":300,"achieved":"2026-11-25T15:30:00Z"}` + "\n" + `{"account":"bob","score":300,"achieved":"2026-11-25T15:30:00.001Z"}` + "\n"
	if string(data) != want {
		t.Fatalf("file =\n%s\nwant\n%s", data, want)
	}

	again := newService(t, openFileStore(t, dir), c)
	if got := top(t, again, "high-score", ""); !slices.Equal(got, []string{"ada:300", "bob:300"}) {
		t.Fatalf("after restart = %v", got)
	}
	if got := top(t, again, "weekly", ""); !slices.Equal(got, []string{"ada:7"}) {
		t.Fatalf("weekly after restart = %v", got)
	}

	// Two weeks on, the old week's file is gone.
	c.t = t0.Add(14 * 24 * time.Hour)
	newService(t, openFileStore(t, dir), c)
	if _, err := os.Stat(filepath.Join(dir, "weekly", "2026-11-23.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("expired week kept: %v", err)
	}
}

func TestFileStoreCutsOffATornLastLine(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	fs := openFileStore(t, dir)
	submit(t, newService(t, fs, c), "high-score", "ada", 300)
	_ = fs.Close()
	path := filepath.Join(dir, "high-score", "all.jsonl")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"account":"bob","sco`)
	_ = f.Close()

	fs2 := openFileStore(t, dir)
	svc := newService(t, fs2, c)
	if got := top(t, svc, "high-score", ""); !slices.Equal(got, []string{"ada:300"}) {
		t.Fatalf("after a torn write = %v", got)
	}
	submit(t, svc, "high-score", "cyd", 100)
	_ = fs2.Close()
	if got := top(t, newService(t, openFileStore(t, dir), c), "high-score", ""); !slices.Equal(got, []string{"ada:300", "cyd:100"}) {
		t.Fatalf("appending after the cut = %v", got)
	}
}

func TestFileStoreRefusesACorruptLine(t *testing.T) {
	for name, line := range map[string]string{
		"not json":      "garbage\n",
		"unknown field": `{"account":"ada","score":1,"achieved":"2026-11-25T15:30:00Z","x":1}` + "\n",
		"no account":    `{"score":1,"achieved":"2026-11-25T15:30:00Z"}` + "\n",
		"trailing":      `{"account":"ada","score":1,"achieved":"2026-11-25T15:30:00Z"} {}` + "\n",
	} {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "high-score"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "high-score", "all.jsonl"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, _ := selfhost.ParseConfig([]byte(testConfig))
		if _, err := selfhost.New(context.Background(), cfg, selfhost.Options{Store: openFileStore(t, dir), Now: (&clock{t0}).Now}); err == nil {
			t.Errorf("%s: loaded a corrupt line", name)
		}
	}
}

func TestFileStoreCompactsSupersededScores(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t0}
	fs := openFileStore(t, dir)
	svc := newService(t, fs, c)
	for i := int64(1); i <= 1500; i++ {
		submit(t, svc, "high-score", "ada", i)
	}
	submit(t, svc, "high-score", "bob", 5)
	_ = fs.Close()
	path := filepath.Join(dir, "high-score", "all.jsonl")
	if data, _ := os.ReadFile(path); bytes.Count(data, []byte{'\n'}) != 1501 {
		t.Fatalf("before compaction: %d lines", bytes.Count(data, []byte{'\n'}))
	}
	fs2 := openFileStore(t, dir)
	again := newService(t, fs2, c)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"); len(lines) != 2 || !strings.Contains(lines[0], `"score":1500`) {
		t.Fatalf("after compaction = %q", lines)
	}
	submit(t, again, "high-score", "cyd", 9000)
	_ = fs2.Close()
	if got := top(t, newService(t, openFileStore(t, dir), c), "high-score", ""); !slices.Equal(got, []string{"cyd:9000", "ada:1500", "bob:5"}) {
		t.Fatalf("after compaction and a new score = %v", got)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "high-score"))
	if len(entries) != 1 {
		t.Fatalf("temporary files left: %v", entries)
	}
}

func TestFileStoreIgnoresForeignFiles(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"notes.txt", "Bad Name/all.jsonl", "high-score/readme.md", "high-score/2026-13-01.jsonl"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte("junk"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := top(t, newService(t, openFileStore(t, dir), &clock{t0}), "high-score", ""); len(got) != 0 {
		t.Fatalf("foreign files loaded: %v", got)
	}
	fs := openFileStore(t, dir)
	if err := fs.Save(context.Background(), selfhost.Record{Board: "../x", Period: "all", Account: "a", Achieved: t0}); err == nil {
		t.Fatal("saved under a path outside the store")
	}
	if err := fs.Drop(context.Background(), "high-score", "../../etc"); err == nil {
		t.Fatal("dropped a path outside the store")
	}
}

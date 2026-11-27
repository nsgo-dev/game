package audit_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/audit"
)

// readDay returns the one file of dir's day, failing when it is not there.
func readDay(t *testing.T, dir, day string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, day+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLogWritesOneLinePerRecord(t *testing.T) {
	dir := t.TempDir()
	w, err := audit.Open(audit.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	w.Log(audit.Record{
		Time:    time.Date(2026, 11, 28, 9, 15, 2, 500_000_000, time.UTC),
		Actor:   "admin:ada",
		Action:  "peer.kick",
		Subject: "peer:42",
		Data:    map[string]any{"reason": "cheating"},
	})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"time":"2026-11-28T09:15:02.500Z","actor":"admin:ada","action":"peer.kick","subject":"peer:42","data":{"reason":"cheating"}}` + "\n"
	if got := readDay(t, dir, "2026-11-28"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if w.Dropped() != 0 {
		t.Errorf("Dropped() = %d, want 0", w.Dropped())
	}
}

func TestLogFillsVersionTimeAndActor(t *testing.T) {
	dir := t.TempDir()
	w, err := audit.Open(audit.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC()
	w.Log(audit.Record{Action: "admin.run", Subject: "restart"})
	after := time.Now().UTC()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("the log holds %v entries, err %v; want the one file of today", len(entries), err)
	}
	data := readDay(t, dir, strings.TrimSuffix(entries[0].Name(), ".jsonl"))
	line := strings.TrimSuffix(data, "\n")
	for _, part := range []string{`"v":1`, `"actor":"system"`, `"action":"admin.run"`, `"subject":"restart"`} {
		if !strings.Contains(line, part) {
			t.Errorf("%s has no %s", line, part)
		}
	}
	if strings.Contains(line, `"data"`) {
		t.Errorf("%s writes an empty data", line)
	}
	var r struct {
		Time time.Time `json:"time"`
	}
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		t.Fatal(err)
	}
	if r.Time.Before(before.Add(-time.Second)) || r.Time.After(after.Add(time.Second)) {
		t.Errorf("time %v is outside [%v, %v]", r.Time, before, after)
	}
}

func TestLogIsByteStable(t *testing.T) {
	records := []audit.Record{
		{Time: time.Date(2026, 11, 27, 23, 59, 59, 999_000_000, time.UTC), Actor: "system", Action: "session.replaced", Subject: "acct:7"},
		{Time: time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC), Actor: "admin:ada", Action: "peer.kick", Subject: "peer:42", Data: map[string]any{"b": 2, "a": "x"}},
	}
	write := func() string {
		dir := t.TempDir()
		w, err := audit.Open(audit.Options{Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range records {
			w.Log(r)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return readDay(t, dir, "2026-11-27") + readDay(t, dir, "2026-11-28")
	}
	if a, b := write(), write(); a != b {
		t.Errorf("the same records wrote different bytes:\n%s\n%s", a, b)
	}
}

func TestLogRollsAtUTCMidnight(t *testing.T) {
	dir := t.TempDir()
	w, err := audit.Open(audit.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	w.Log(audit.Record{Time: time.Date(2026, 11, 28, 23, 59, 59, 999_000_000, time.UTC), Action: "peer.kick", Subject: "peer:1"})
	w.Log(audit.Record{Time: time.Date(2026, 11, 29, 0, 0, 0, 0, time.UTC), Action: "peer.kick", Subject: "peer:2"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readDay(t, dir, "2026-11-28"); !strings.Contains(got, `"subject":"peer:1"`) || strings.Count(got, "\n") != 1 {
		t.Errorf("2026-11-28.jsonl = %q, want the one record before midnight", got)
	}
	if got := readDay(t, dir, "2026-11-29"); !strings.Contains(got, `"subject":"peer:2"`) || strings.Count(got, "\n") != 1 {
		t.Errorf("2026-11-29.jsonl = %q, want the one record at midnight", got)
	}
}

func TestRetentionDeletesOnlyOldFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"2026-11-20.jsonl", "2026-11-26.jsonl", "2026-11-27.jsonl", "notes.txt", "not-a-date.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w, err := audit.Open(audit.Options{Dir: dir, Retain: 2})
	if err != nil {
		t.Fatal(err)
	}
	w.Log(audit.Record{Time: time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC), Action: "admin.run"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"2026-11-20.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s survived retention", gone)
		}
	}
	for _, kept := range []string{"2026-11-26.jsonl", "2026-11-27.jsonl", "2026-11-28.jsonl", "notes.txt", "not-a-date.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s was deleted: %v", kept, err)
		}
	}
}

func TestCloseFlushesTheQueue(t *testing.T) {
	dir := t.TempDir()
	w, err := audit.Open(audit.Options{Dir: dir, Queue: 16})
	if err != nil {
		t.Fatal(err)
	}
	for range 16 {
		w.Log(audit.Record{Time: time.Date(2026, 11, 28, 1, 0, 0, 0, time.UTC), Action: "peer.kick"})
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(readDay(t, dir, "2026-11-28"), "\n"); got != 16 {
		t.Errorf("the file holds %d lines after Close, want 16", got)
	}
}

func TestLogAfterCloseIsDropped(t *testing.T) {
	w, err := audit.Open(audit.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil { // safe to call twice
		t.Fatal(err)
	}
	w.Log(audit.Record{Action: "peer.kick"})
	if w.Dropped() != 1 {
		t.Errorf("Dropped() = %d, want 1", w.Dropped())
	}
}

func TestCloseReportsTheAppendFailure(t *testing.T) {
	dir := t.TempDir()
	// A directory where the day's file should go makes the open fail.
	if err := os.Mkdir(filepath.Join(dir, "2026-11-28.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := audit.Open(audit.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	w.Log(audit.Record{Time: time.Date(2026, 11, 28, 1, 0, 0, 0, time.UTC), Action: "peer.kick"})
	err = w.Close()
	if err == nil || !strings.Contains(err.Error(), "audit:") {
		t.Errorf("Close() = %v, want the open failure", err)
	}
	if w.Dropped() != 1 {
		t.Errorf("Dropped() = %d, want 1", w.Dropped())
	}
}

func TestOpenValidatesOptions(t *testing.T) {
	if _, err := audit.Open(audit.Options{}); err == nil {
		t.Error("an empty Dir opened")
	}
	if _, err := audit.Open(audit.Options{Dir: t.TempDir(), Queue: -1}); err == nil {
		t.Error("a negative Queue opened")
	}
	if _, err := audit.Open(audit.Options{Dir: t.TempDir(), Retain: -1}); err == nil {
		t.Error("a negative Retain opened")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := audit.Open(audit.Options{Dir: file}); err == nil {
		t.Error("a Dir that is a file opened")
	}
}

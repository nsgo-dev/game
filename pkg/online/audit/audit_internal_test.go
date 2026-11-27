package audit

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogRollsAtUTCMidnightWithAFakeClock(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 28, 23, 59, 59, 500_000_000, time.UTC)
	w, err := open(Options{Dir: dir}, func() time.Time { return now }, nil)
	if err != nil {
		t.Fatal(err)
	}
	w.Log(Record{Action: "peer.kick", Subject: "peer:1"})
	now = now.Add(time.Second) // past UTC midnight
	w.Log(Record{Action: "peer.kick", Subject: "peer:2"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	for day, subject := range map[string]string{"2026-11-28": "peer:1", "2026-11-29": "peer:2"} {
		data, err := os.ReadFile(filepath.Join(dir, day+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"subject":"`+subject+`"`) {
			t.Errorf("%s.jsonl = %q, want the record of %s", day, data, subject)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("the log holds %d files, want one per day", len(entries))
	}
}

func TestFullQueueDropsWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	syncFile := func(f *os.File) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}
	w, err := open(Options{Dir: dir, Queue: 1}, time.Now, syncFile)
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	w.Log(Record{Time: day, Action: "peer.kick", Subject: "peer:1"})
	<-entered                                                        // the writer is appending the first record and holds the sync
	w.Log(Record{Time: day, Action: "peer.kick", Subject: "peer:2"}) // fills the queue
	w.Log(Record{Time: day, Action: "peer.kick", Subject: "peer:3"}) // dropped
	close(release)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if w.Dropped() != 1 {
		t.Errorf("Dropped() = %d, want 1", w.Dropped())
	}
	data, err := os.ReadFile(filepath.Join(dir, "2026-11-28.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "\n"); got != 2 {
		t.Errorf("the file holds %d lines, want the two records that fit", got)
	}
	if strings.Contains(string(data), "peer:3") {
		t.Error("the dropped record was written")
	}
}

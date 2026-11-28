package liveops

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBatchProviderDurableFlushAndPurge(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	var got []byte
	b, _ := NewBatchProvider(p, 1<<20, func(data []byte) error { got = append([]byte(nil), data...); return nil })
	if err := b.Send(Event{Name: "start", Time: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	if b.Pending() != 1 {
		t.Fatal("pending")
	}
	if err := b.Flush(); err != nil || len(got) == 0 {
		t.Fatalf("flush %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	if err := b.Purge(); err != nil {
		t.Fatal(err)
	}
}

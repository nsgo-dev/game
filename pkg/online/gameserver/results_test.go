package gameserver

import (
	"context"
	"testing"
)

func TestMemoryResultCommitterIdempotent(t *testing.T) {
	c := NewMemoryResultCommitter()
	r := MatchResults{MatchID: "m", Records: []byte("r")}
	if err := c.Commit(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if err := c.Commit(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	r.Records = []byte("changed")
	if err := c.Commit(context.Background(), r); err != ErrResultConflict {
		t.Fatalf("err=%v", err)
	}
}

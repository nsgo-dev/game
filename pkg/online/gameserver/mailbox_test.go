package gameserver

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMatchMailboxOrderAndCapacity(t *testing.T) {
	m := &Match{Mailbox: 2}
	if err := m.Post(func() {}); err != nil {
		t.Fatal(err)
	}
	if err := m.Post(func() {}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(m.Post(func() {}), ErrMailboxFull) {
		t.Fatal("expected mailbox full")
	}
	if m.MailboxLen() != 2 {
		t.Fatalf("len = %d", m.MailboxLen())
	}
}

func TestMatchCallRunsOnRunnerAndStops(t *testing.T) {
	done := make(chan struct{})
	m := &Match{Update: func(time.Duration) { close(done) }}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{Matches: []*Match{m}}
	runDone := make(chan struct{})
	go func() { _ = r.Run(ctx); close(runDone) }()
	<-done
	cancel()
	<-runDone
	if err := m.Call(context.Background(), func() error { return nil }); !errors.Is(err, ErrMatchStopped) {
		t.Fatalf("Call after stop = %v", err)
	}
}

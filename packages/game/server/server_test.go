package server_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/server"
)

func TestRunnerStepsMatchesAtTheirOwnRates(t *testing.T) {
	var fast, slow atomic.Int64
	var fastDT, slowDT atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	r := &server.Runner{Matches: []*server.Match{
		{Rate: 40, Update: func(dt time.Duration) { fastDT.Store(int64(dt)); fast.Add(1) }},
		{Rate: 20, Update: func(dt time.Duration) { slowDT.Store(int64(dt)); slow.Add(1) }},
	}}
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if fast.Load() < 3 || fast.Load() > 8 {
		t.Fatalf("fast updates = %d, want about 6 in 150ms at 40Hz", fast.Load())
	}
	if slow.Load() < 1 || slow.Load() > 4 {
		t.Fatalf("slow updates = %d, want about 3 in 150ms at 20Hz", slow.Load())
	}
	if fastDT.Load() != int64(25*time.Millisecond) {
		t.Fatalf("40Hz dt = %v, want 25ms", time.Duration(fastDT.Load()))
	}
	if slowDT.Load() != int64(50*time.Millisecond) {
		t.Fatalf("20Hz dt = %v, want 50ms", time.Duration(slowDT.Load()))
	}
}

func TestRunnerReportsOverrunsWithoutCatchUp(t *testing.T) {
	var calls, overruns atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	r := &server.Runner{
		OnOverrun: func(*server.Match, time.Duration, int) { overruns.Add(1) },
		Matches: []*server.Match{{Rate: 20, Update: func(time.Duration) {
			calls.Add(1)
			time.Sleep(80 * time.Millisecond)
		}}},
	}
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if overruns.Load() == 0 {
		t.Fatal("slow update was not reported as an overrun")
	}
	// 250ms with a 50ms step is at most 5 started ticks; catch-up would run many more.
	if calls.Load() > 5 {
		t.Fatalf("updates = %d, want no burst catch-up", calls.Load())
	}
}

func TestRunnerFinishesCurrentTickThenStops(t *testing.T) {
	var updates, stops atomic.Int64
	tickDone := make(chan struct{})
	var tickRan atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	r := &server.Runner{Matches: []*server.Match{{Rate: 20, Update: func(time.Duration) {
		tickRan.Store(true)
		updates.Add(1)
		close(tickDone)
		time.Sleep(30 * time.Millisecond)
	}, OnStop: func() { stops.Add(1) }}}}
	go func() {
		select {
		case <-tickDone:
		case <-time.After(time.Second):
			t.Error("no tick started")
		}
		cancel()
	}()
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !tickRan.Load() || updates.Load() != 1 {
		t.Fatalf("updates = %d, want the one in-flight tick completed", updates.Load())
	}
	if stops.Load() != 1 {
		t.Fatalf("OnStop calls = %d, want 1", stops.Load())
	}
}

func TestRunnerRejectsAMatchWithoutUpdate(t *testing.T) {
	if err := (&server.Runner{Matches: []*server.Match{{Rate: 60}}}).Run(context.Background()); !errors.Is(err, errors.New("server: a match has no Update function")) {
		// Compare only the failure, not identity: a nil Update must not start.
		if err == nil {
			t.Fatal("nil Update was accepted")
		}
	}
}

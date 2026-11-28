package gameserver_test

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nsgo-dev/game/pkg/online/gameserver"
)

func TestRunnerStepsMatchesAtTheirOwnRates(t *testing.T) {
	var fast, slow atomic.Int64
	var fastDT, slowDT atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	r := &gameserver.Runner{Matches: []*gameserver.Match{
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
	r := &gameserver.Runner{
		OnOverrun: func(*gameserver.Match, time.Duration, int) { overruns.Add(1) },
		Matches: []*gameserver.Match{{Rate: 20, Update: func(time.Duration) {
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
	r := &gameserver.Runner{Matches: []*gameserver.Match{{Rate: 20, Update: func(time.Duration) {
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
	if err := (&gameserver.Runner{Matches: []*gameserver.Match{{Rate: 60}}}).Run(context.Background()); !errors.Is(err, errors.New("gameserver: a match has no Update function")) {
		// Compare only the failure, not identity: a nil Update must not start.
		if err == nil {
			t.Fatal("nil Update was accepted")
		}
	}
}

func TestRunnerHoldsRateUnderHalfStepUpdate(t *testing.T) {
	cases := []struct {
		name       string
		rate       int
		updateCost time.Duration
	}{
		{name: "100Hz_5ms", rate: 100, updateCost: 5 * time.Millisecond},
		{name: "60Hz_8ms", rate: 60, updateCost: 8 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				step := time.Duration(int64(time.Second) / int64(tc.rate))
				start := time.Now()
				var ticks, overruns int
				var lastTickStart time.Duration
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				r := &gameserver.Runner{
					OnOverrun: func(*gameserver.Match, time.Duration, int) { overruns++ },
					Matches: []*gameserver.Match{{
						Rate: tc.rate,
						Update: func(dt time.Duration) {
							if dt != step {
								t.Errorf("dt = %v, want %v", dt, step)
							}
							ticks++
							lastTickStart = time.Since(start)
							time.Sleep(tc.updateCost)
							if ticks == tc.rate {
								cancel()
							}
						},
					}},
				}
				if err := r.Run(ctx); err != nil {
					t.Fatal(err)
				}
				if ticks != tc.rate {
					t.Fatalf("ticks = %d, want %d", ticks, tc.rate)
				}
				if overruns != 0 {
					t.Fatalf("overruns = %d, want 0", overruns)
				}
				wantLast := time.Duration(tc.rate) * step
				if lastTickStart != wantLast {
					t.Fatalf("last tick started at %v, want %v", lastTickStart, wantLast)
				}
			})
		})
	}
}

func TestRunnerOverrunResetsScheduleWithoutBurst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		type overrunEvent struct {
			lag    time.Duration
			missed int
		}
		const step = 50 * time.Millisecond
		updateCosts := []time.Duration{
			80 * time.Millisecond,  // overruns by 30ms -> missed 1
			20 * time.Millisecond,  // normal after overrun reset
			180 * time.Millisecond, // overruns by 130ms -> missed 2
			20 * time.Millisecond,  // normal after multi-step overrun reset
		}
		start := time.Now()
		var starts []time.Duration
		var overruns []overrunEvent
		var stops int

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		r := &gameserver.Runner{
			OnOverrun: func(_ *gameserver.Match, lag time.Duration, missed int) {
				overruns = append(overruns, overrunEvent{lag: lag, missed: missed})
			},
			Matches: []*gameserver.Match{{
				Rate: 20,
				Update: func(dt time.Duration) {
					if dt != step {
						t.Errorf("dt = %v, want %v", dt, step)
					}
					idx := len(starts)
					starts = append(starts, time.Since(start))
					time.Sleep(updateCosts[idx])
					if idx == len(updateCosts)-1 {
						cancel()
					}
				},
				OnStop: func() { stops++ },
			}},
		}
		if err := r.Run(ctx); err != nil {
			t.Fatal(err)
		}

		// Tick 1: starts at 50ms, finishes at 130ms (overrun -> reset to 130+50=180ms).
		// Tick 2: starts at 180ms, finishes at 200ms (deadline -> 180+50=230ms).
		// Tick 3: starts at 230ms, finishes at 410ms (overrun -> reset to 410+50=460ms).
		// Tick 4: starts at 460ms, finishes at 480ms, cancels.
		wantStarts := []time.Duration{
			50 * time.Millisecond,
			180 * time.Millisecond,
			230 * time.Millisecond,
			460 * time.Millisecond,
		}
		if !slices.Equal(starts, wantStarts) {
			t.Fatalf("tick starts = %v, want %v", starts, wantStarts)
		}
		wantOverruns := []overrunEvent{
			{lag: 30 * time.Millisecond, missed: 1},
			{lag: 130 * time.Millisecond, missed: 2},
		}
		if !slices.Equal(overruns, wantOverruns) {
			t.Fatalf("overruns = %+v, want %+v", overruns, wantOverruns)
		}
		if stops != 1 {
			t.Fatalf("OnStop calls = %d, want 1", stops)
		}
	})
}

func TestRunnerExactStepUpdateDoesNotOverrunOrSkip(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const step = 10 * time.Millisecond
		start := time.Now()
		var starts []time.Duration
		var overruns, stops int
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		r := &gameserver.Runner{
			OnOverrun: func(*gameserver.Match, time.Duration, int) { overruns++ },
			Matches: []*gameserver.Match{{
				Rate: 100,
				Update: func(time.Duration) {
					starts = append(starts, time.Since(start))
					time.Sleep(step)
					if len(starts) == 5 {
						cancel()
					}
				},
				OnStop: func() { stops++ },
			}},
		}
		if err := r.Run(ctx); err != nil {
			t.Fatal(err)
		}
		wantStarts := []time.Duration{
			10 * time.Millisecond,
			20 * time.Millisecond,
			30 * time.Millisecond,
			40 * time.Millisecond,
			50 * time.Millisecond,
		}
		if !slices.Equal(starts, wantStarts) {
			t.Fatalf("tick starts = %v, want %v", starts, wantStarts)
		}
		if overruns != 0 {
			t.Fatalf("overruns = %d, want 0", overruns)
		}
		if stops != 1 {
			t.Fatalf("OnStop calls = %d, want 1", stops)
		}
	})
}

func TestRunnerCancelInsideOverrunStopsImmediately(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var ticks, overruns, stops int
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		r := &gameserver.Runner{
			OnOverrun: func(*gameserver.Match, time.Duration, int) {
				overruns++
				cancel()
			},
			Matches: []*gameserver.Match{{
				Rate: 20,
				Update: func(time.Duration) {
					ticks++
					time.Sleep(80 * time.Millisecond)
				},
				OnStop: func() { stops++ },
			}},
		}
		if err := r.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if ticks != 1 || overruns != 1 || stops != 1 {
			t.Fatalf("ticks=%d overruns=%d stops=%d, want 1, 1, 1", ticks, overruns, stops)
		}
	})
}

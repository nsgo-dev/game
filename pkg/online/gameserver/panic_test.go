package gameserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/gameserver"
	"github.com/nsgo-dev/game/pkg/online/liveops/crashreport"
)

type panicRecorder struct {
	mu     sync.Mutex
	panics []gameserver.Panic
}

func (p *panicRecorder) record() func(gameserver.Panic) gameserver.PanicAction {
	return func(got gameserver.Panic) gameserver.PanicAction {
		p.mu.Lock()
		p.panics = append(p.panics, got)
		p.mu.Unlock()
		return gameserver.StopMatch
	}
}

func (p *panicRecorder) one(t *testing.T) gameserver.Panic {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.panics) != 1 {
		t.Fatalf("OnPanic calls = %d, want 1", len(p.panics))
	}
	return p.panics[0]
}

func TestRunnerRecoversPanickingMatch(t *testing.T) {
	var rec panicRecorder
	var aTicks, bTicks, aStops, bStops atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	r := &gameserver.Runner{
		OnPanic: rec.record(),
		Matches: []*gameserver.Match{
			{Name: "panic", Rate: 100, Update: func(time.Duration) {
				if aTicks.Add(1) == 2 {
					panic("boom")
				}
			}, OnStop: func() { aStops.Add(1) }},
			{Name: "steady", Rate: 100, Update: func(time.Duration) { bTicks.Add(1) }, OnStop: func() { bStops.Add(1) }},
		},
	}
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if aTicks.Load() != 2 {
		t.Errorf("panicking match ticks = %d, want 2 (never ticks again after the panic)", aTicks.Load())
	}
	if aStops.Load() != 1 {
		t.Errorf("panicking match OnStop = %d, want 1", aStops.Load())
	}
	if bStops.Load() != 1 {
		t.Errorf("sibling match OnStop = %d, want 1", bStops.Load())
	}
	if bTicks.Load() < 10 {
		t.Errorf("sibling match ticks = %d, want it to keep ticking until the context ends", bTicks.Load())
	}
	p := rec.one(t)
	if p.Match == nil || p.Match.Name != "panic" {
		t.Errorf("Panic.Match = %v, want the panicking match", p.Match)
	}
	if fmt.Sprint(p.Value) != "boom" {
		t.Errorf("Panic.Value = %v, want boom", p.Value)
	}
	if p.Tick != 2 {
		t.Errorf("Panic.Tick = %d, want 2", p.Tick)
	}
	if len(p.Stack) == 0 {
		t.Error("Panic.Stack is empty")
	}
	if p.Time.IsZero() {
		t.Error("Panic.Time is zero")
	}
}

func TestRunnerRecoversPanickingMatchWithoutOnPanic(t *testing.T) {
	var aTicks, bTicks atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	r := &gameserver.Runner{Matches: []*gameserver.Match{
		{Rate: 100, Update: func(time.Duration) {
			if aTicks.Add(1) == 1 {
				panic("boom")
			}
		}},
		{Rate: 100, Update: func(time.Duration) { bTicks.Add(1) }},
	}}
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if aTicks.Load() != 1 {
		t.Errorf("panicking match ticks = %d, want 1", aTicks.Load())
	}
	if bTicks.Load() < 5 {
		t.Errorf("sibling match ticks = %d, want it to keep ticking with OnPanic nil (default StopMatch)", bTicks.Load())
	}
}

func TestRunnerCrashActionStopsAllAndRepanics(t *testing.T) {
	var aTicks, stops atomic.Int64
	r := &gameserver.Runner{
		OnPanic: func(gameserver.Panic) gameserver.PanicAction { return gameserver.Crash },
		Matches: []*gameserver.Match{
			{Rate: 100, Update: func(time.Duration) {
				if aTicks.Add(1) == 2 {
					panic("boom")
				}
			}, OnStop: func() { stops.Add(1) }},
			{Rate: 100, Update: func(time.Duration) {}, OnStop: func() { stops.Add(1) }},
		},
	}
	defer func() {
		v := recover()
		if v == nil || fmt.Sprint(v) != "boom" {
			t.Fatalf("Run re-panicked with %v, want boom", v)
		}
		if stops.Load() != 2 {
			t.Errorf("OnStop ran %d times, want 2 (every match's OnStop runs before the re-panic)", stops.Load())
		}
	}()
	if err := r.Run(context.Background()); err != nil {
		t.Error(err)
	}
}

func TestRunnerRecoversOnStopPanic(t *testing.T) {
	var rec panicRecorder
	var otherStop atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	r := &gameserver.Runner{
		OnPanic: rec.record(),
		Matches: []*gameserver.Match{
			{Name: "bad-stop", Rate: 100, Update: func(time.Duration) {}, OnStop: func() { panic("stop boom") }},
			{Name: "ok", Rate: 100, Update: func(time.Duration) {}, OnStop: func() { otherStop.Add(1) }},
		},
	}
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if otherStop.Load() != 1 {
		t.Errorf("sibling match OnStop = %d, want 1", otherStop.Load())
	}
	p := rec.one(t)
	if p.Match == nil || p.Match.Name != "bad-stop" {
		t.Errorf("Panic.Match = %v, want the match whose OnStop panicked", p.Match)
	}
	if fmt.Sprint(p.Value) != "stop boom" {
		t.Errorf("Panic.Value = %v, want stop boom", p.Value)
	}
	if p.Tick != 0 {
		t.Errorf("Panic.Tick = %d, want 0 for an OnStop panic", p.Tick)
	}
}

func TestPanicReportMatchesCrashSchema(t *testing.T) {
	ts := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	p := gameserver.Panic{
		Match: &gameserver.Match{Name: "lobby"},
		Value: errors.New("boom"),
		Stack: []byte("goroutine 7 [running]:..."),
		Tick:  7,
		Time:  ts,
	}
	rep := p.Report()
	if rep.Timestamp != ts || rep.Message != "boom" || rep.Stack != "goroutine 7 [running]:..." || rep.Step != 7 {
		t.Errorf("Report() = %+v, want the panic's time, message, stack and tick", rep)
	}
	if rep.Metadata["match"] != "lobby" {
		t.Errorf("Report().Metadata = %v, want the match name under \"match\"", rep.Metadata)
	}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("Report() does not marshal: %v", err)
	}
	var decoded crashreport.CrashReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Report() does not round-trip as a crashreport.CrashReport: %v", err)
	}
	if decoded.Message != "boom" || decoded.Step != 7 || decoded.Metadata["match"] != "lobby" {
		t.Errorf("round-tripped report = %+v, want the same message, step and metadata", decoded)
	}
}

//go:build !release

package gamedebug_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
)

func TestPanicRecordingAndRecovery(t *testing.T) {
	gamedebug.ClearPanic()
	t.Cleanup(gamedebug.ClearPanic)

	if _, ok := gamedebug.LastPanic(); ok {
		t.Fatal("LastPanic reported ok after ClearPanic")
	}
	if rec := gamedebug.RecordPanic(nil, nil, "ignored"); rec.Value != "" {
		t.Fatalf("RecordPanic(nil) = %+v, want zero", rec)
	}
	if _, ok := gamedebug.LastPanic(); ok {
		t.Fatal("RecordPanic(nil) stored a panic record")
	}

	// Direct deferred gamedebug.Recover catches panics from the enclosing function.
	func() {
		defer gamedebug.Recover("loop.Update", "physics")
		panic("bad transform")
	}()

	got, ok := gamedebug.LastPanic()
	if !ok {
		t.Fatal("LastPanic returned false after defer gamedebug.Recover")
	}
	if got.Value != "bad transform" {
		t.Fatalf("Value = %q, want %q", got.Value, "bad transform")
	}
	if got.Context != "loop.Update: physics" {
		t.Fatalf("Context = %q, want %q", got.Context, "loop.Update: physics")
	}
	if !strings.Contains(got.Stack, "TestPanicRecordingAndRecovery") {
		t.Fatalf("Stack missing test frame:\n%s", got.Stack)
	}
	if got.Time.IsZero() {
		t.Fatal("Time is zero")
	}

	// RecoverFunc reports panicked=false when fn succeeds, and true when fn panics.
	if rec, panicked := gamedebug.RecoverFunc("ok", func() {}); panicked || rec.Value != "" {
		t.Fatalf("RecoverFunc(ok) = (%+v, %v), want zero/false", rec, panicked)
	}
	rec, panicked := gamedebug.RecoverFunc("scene.Draw", func() {
		panic("nil sprite atlas")
	})
	if !panicked || rec.Value != "nil sprite atlas" || rec.Context != "scene.Draw" {
		t.Fatalf("RecoverFunc(panic) = (%+v, %v)", rec, panicked)
	}
	latest, ok := gamedebug.LastPanic()
	if !ok || latest.Value != "nil sprite atlas" {
		t.Fatalf("LastPanic = (%+v, %v), want nil sprite atlas", latest, ok)
	}

	// Oversized stacks are truncated safely.
	huge := strings.Repeat("frame\n", 20000)
	trunc := gamedebug.RecordPanic("overflow", []byte(huge), "stack")
	if !strings.HasSuffix(trunc.Stack, "... (truncated)") {
		t.Fatalf("expected truncated stack suffix, len=%d", len(trunc.Stack))
	}

	// Concurrent RecordPanic / LastPanic / ClearPanic are race-free.
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range 50 {
				gamedebug.RecordPanic(id, []byte("stack"), "worker")
				_, _ = gamedebug.LastPanic()
			}
		}(i)
	}
	wg.Wait()
	gamedebug.ClearPanic()
	if _, ok := gamedebug.LastPanic(); ok {
		t.Fatal("LastPanic still present after ClearPanic")
	}
}

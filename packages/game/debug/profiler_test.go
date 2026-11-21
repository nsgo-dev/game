//go:build !release

package debug_test

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestProfilerNestedScopesAndTopScopes(t *testing.T) {
	now := time.Unix(1000, 0)
	p := debug.NewProfiler(60)
	p.SetNow(func() time.Time { return now })

	runFrame := func(physA, physB, render time.Duration) {
		p.BeginFrame()
		func() {
			defer p.Scope("frame").End()
			tok := p.Begin("physics")
			now = now.Add(physA)
			p.End(tok)

			tok2 := p.Begin("physics")
			now = now.Add(physB)
			p.End(tok2)

			func() {
				defer p.Scope("render").End()
				now = now.Add(render)
			}()
		}()
		p.EndFrame()
	}

	// Frame 1: physics=4ms (1+3), render=2ms, frame=6ms
	runFrame(1*time.Millisecond, 3*time.Millisecond, 2*time.Millisecond)
	// Frame 2: physics=6ms (2+4), render=4ms, frame=10ms
	runFrame(2*time.Millisecond, 4*time.Millisecond, 4*time.Millisecond)

	top := p.TopScopes(2)
	if len(top) != 2 {
		t.Fatalf("TopScopes(2) len = %d, want 2", len(top))
	}
	if top[0].Name != "frame" || top[0].Total != 10*time.Millisecond || top[0].Avg != 8*time.Millisecond || top[0].Max != 10*time.Millisecond || top[0].Calls != 1 {
		t.Fatalf("top[0] = %+v, want frame Total=10ms Avg=8ms Max=10ms Calls=1", top[0])
	}
	if top[1].Name != "physics" || top[1].Total != 6*time.Millisecond || top[1].Avg != 5*time.Millisecond || top[1].Max != 6*time.Millisecond || top[1].Calls != 2 {
		t.Fatalf("top[1] = %+v, want physics Total=6ms Avg=5ms Max=6ms Calls=2", top[1])
	}

	all := p.TopScopes(0)
	if len(all) != 3 || all[2].Name != "render" || all[2].Total != 4*time.Millisecond || all[2].Avg != 3*time.Millisecond {
		t.Fatalf("TopScopes(0) = %+v, want 3 scopes with render last", all)
	}
}

func TestProfilerChromeTrace300Frames(t *testing.T) {
	now := time.Unix(2000, 0)
	p := debug.NewProfiler(300)
	p.SetNow(func() time.Time { return now })

	if p.Recording() || p.RecordedFrames() != 0 {
		t.Fatalf("initial recording=(%v, %d), want (false, 0)", p.Recording(), p.RecordedFrames())
	}

	p.StartRecording(300)
	if !p.Recording() {
		t.Fatal("Recording() = false after StartRecording(300)")
	}

	// Run 305 frames; recording should stop automatically at 300.
	for range 305 {
		p.BeginFrame()
		t1 := p.Begin("physics")
		now = now.Add(2 * time.Millisecond)
		p.End(t1)

		t2 := p.Begin("ai")
		now = now.Add(1 * time.Millisecond)
		p.End(t2)

		t3 := p.Begin("render")
		now = now.Add(3 * time.Millisecond)
		p.End(t3)
		p.EndFrame()
	}

	if p.Recording() {
		t.Fatal("Recording() still true after 300 frames")
	}
	if got := p.RecordedFrames(); got != 300 {
		t.Fatalf("RecordedFrames() = %d, want 300", got)
	}

	var buf bytes.Buffer
	if err := p.WriteChromeTrace(&buf); err != nil {
		t.Fatalf("WriteChromeTrace error = %v", err)
	}

	type traceEvent struct {
		Name string         `json:"name"`
		Cat  string         `json:"cat"`
		Ph   string         `json:"ph"`
		Ts   *float64       `json:"ts"`
		Dur  *float64       `json:"dur"`
		PID  int            `json:"pid"`
		TID  int            `json:"tid"`
		Args map[string]any `json:"args"`
	}
	var doc struct {
		TraceEvents []traceEvent `json:"traceEvents"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("invalid Chrome trace JSON: %v", err)
	}

	metaTIDs := make(map[string]int)
	scopeTIDs := make(map[string]int)
	completeCount := 0
	for _, ev := range doc.TraceEvents {
		if ev.PID != 1 {
			t.Fatalf("event %+v has pid=%d, want 1", ev, ev.PID)
		}
		if ev.TID <= 0 {
			t.Fatalf("event %+v has invalid tid=%d", ev, ev.TID)
		}
		switch ev.Ph {
		case "M":
			if ev.Name == "thread_name" {
				if label, ok := ev.Args["name"].(string); ok {
					metaTIDs[label] = ev.TID
				}
			}
		case "X":
			completeCount++
			if ev.Ts == nil || *ev.Ts < 0 {
				t.Fatalf("complete event %+v missing valid ts", ev)
			}
			if ev.Dur == nil || *ev.Dur <= 0 {
				t.Fatalf("complete event %+v missing positive dur", ev)
			}
			if prev, exists := scopeTIDs[ev.Name]; exists && prev != ev.TID {
				t.Fatalf("scope %q changed tid from %d to %d", ev.Name, prev, ev.TID)
			}
			scopeTIDs[ev.Name] = ev.TID
		default:
			t.Fatalf("unexpected ph %q in event %+v", ev.Ph, ev)
		}
	}

	if completeCount != 300*3 {
		t.Fatalf("got %d complete events, want %d", completeCount, 300*3)
	}
	if len(scopeTIDs) != 3 {
		t.Fatalf("got %d distinct scope tracks, want 3: %v", len(scopeTIDs), scopeTIDs)
	}
	// Verify every scope got a unique tid matching its thread_name metadata event.
	seenTID := make(map[int]string)
	for name, tid := range scopeTIDs {
		if other, dup := seenTID[tid]; dup {
			t.Fatalf("scopes %q and %q share tid %d, want unique tid per scope", name, other, tid)
		}
		seenTID[tid] = name
		if metaTIDs[name] != tid {
			t.Fatalf("scope %q metadata tid = %d, want %d", name, metaTIDs[name], tid)
		}
	}
}

func TestProfilerZeroAllocsSteadyState(t *testing.T) {
	p := debug.NewProfiler(300)

	step := func() {
		p.BeginFrame()
		func() {
			defer p.Scope("frame").End()
			t1 := p.Begin("physics")
			p.End(t1)
			t2 := p.Begin("render")
			p.End(t2)
		}()
		p.EndFrame()
	}

	// Warm up one frame to register scope names.
	step()

	// Steady state without recording: 0 allocs/frame.
	debug.AssertZeroAllocs(t, 100, step)

	// Steady state while recording (within pre-allocated capacity): 0 allocs/frame.
	p.StartRecording(200)
	debug.AssertZeroAllocs(t, 100, step)
}

func TestProfilerOverlayDisplay(t *testing.T) {
	now := time.Unix(3000, 0)
	p := debug.NewProfiler(60)
	p.SetNow(func() time.Time { return now })

	dbg := debug.New(debug.Options{
		Visible:  true,
		Profiler: p,
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{
			Style:    core.Style{Width: core.Pt(400), Height: core.Pt(300)},
			Children: []core.Node{dbg.View(cx)},
		}
	})

	if !app.Exists("debug-profiler") {
		t.Fatal("debug-profiler section not mounted")
	}

	app.Post(func() {
		p.BeginFrame()
		tok := p.Begin("physics")
		now = now.Add(4 * time.Millisecond)
		p.End(tok)
		p.EndFrame()
		dbg.RecordFrame(16*time.Millisecond, 4*time.Millisecond, 2*time.Millisecond)
	})
	app.Settle()

	texts := app.Texts()
	if !slices.Contains(texts, "physics") {
		t.Fatalf("overlay texts %v missing scope 'physics'", texts)
	}
	foundTiming := false
	for _, txt := range texts {
		if strings.Contains(txt, "4.00ms") {
			foundTiming = true
			break
		}
	}
	if !foundTiming {
		t.Fatalf("overlay texts %v missing 4.00ms timing readout", texts)
	}
}

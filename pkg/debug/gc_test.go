//go:build !release

package debug_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/debug"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

var gcTestAllocSink []byte

func TestGCMonitorSamplesAndDetectsHitch(t *testing.T) {
	m := debug.NewGCMonitor(8)
	beforeCycles := m.TotalCycles()

	runtime.GC()
	sample := m.SampleFrame(18 * time.Millisecond)

	if sample.CyclesDelta == 0 {
		t.Fatalf("CyclesDelta = 0 after runtime.GC(), want >= 1")
	}
	if !sample.Hitched {
		t.Fatal("Hitched = false after runtime.GC(), want true")
	}
	if sample.HeapObjectsBytes == 0 {
		t.Fatal("HeapObjectsBytes = 0, want > 0")
	}
	if m.TotalCycles() <= beforeCycles {
		t.Fatalf("TotalCycles() = %d, want > %d", m.TotalCycles(), beforeCycles)
	}
	if m.RecentHitches() < 1 {
		t.Fatalf("RecentHitches() = %d, want >= 1", m.RecentHitches())
	}
	if m.Len() != 1 || m.Latest() != sample || m.At(0) != sample {
		t.Fatalf("history accessors mismatch: len=%d latest=%+v at0=%+v", m.Len(), m.Latest(), m.At(0))
	}

	// Fill past ring buffer capacity to verify bounded retention.
	for range 12 {
		_ = m.SampleFrame(16 * time.Millisecond)
	}
	if m.Len() != 8 {
		t.Fatalf("Len() = %d after 13 samples with historyLen=8, want 8", m.Len())
	}

	// Deliberate heap allocations are reflected in per-frame alloc averages and cleared by Reset.
	gcTestAllocSink = make([]byte, 64<<10)
	gcTestAllocSink[0] = 42
	allocSample := m.SampleFrame(16 * time.Millisecond)
	gcTestAllocSink = nil
	if allocSample.AllocsDelta == 0 || allocSample.AllocBytesDelta == 0 {
		t.Fatalf("expected non-zero AllocsDelta/AllocBytesDelta after heap allocation, got %+v", allocSample)
	}
	if m.AllocsPerFrame() <= 0 || m.AllocBytesPerFrame() <= 0 {
		t.Fatalf("AllocsPerFrame=%v AllocBytesPerFrame=%v, want > 0", m.AllocsPerFrame(), m.AllocBytesPerFrame())
	}

	m.Reset()
	if m.Len() != 0 || m.RecentHitches() != 0 || m.LastPause() != 0 || m.MaxPause() != 0 || m.AllocsPerFrame() != 0 || m.AllocBytesPerFrame() != 0 {
		t.Fatalf("GCMonitor not cleared after Reset: len=%d hitches=%d allocs=%v", m.Len(), m.RecentHitches(), m.AllocsPerFrame())
	}
}

func TestGCMonitorZeroAllocsSteadyState(t *testing.T) {
	m := debug.NewGCMonitor(debug.FrameHistory)
	// Warm up once.
	_ = m.SampleFrame(16 * time.Millisecond)

	debug.AssertZeroAllocs(t, 100, func() {
		_ = m.SampleFrame(16 * time.Millisecond)
	})
}

func TestMeasureAllocsHelper(t *testing.T) {
	var sink int
	if got := debug.MeasureAllocs(50, func() { sink++ }); got != 0 {
		t.Fatalf("MeasureAllocs(non-alloc) = %v, want 0", got)
	}
	var escape []byte
	if got := debug.MeasureAllocs(50, func() { escape = make([]byte, 256) }); got < 1 {
		t.Fatalf("MeasureAllocs(alloc) = %v, want >= 1 (len=%d)", got, len(escape))
	}
}

func TestGCOverlayDisplaysHitchesAndMarksGraph(t *testing.T) {
	gcMon := debug.NewGCMonitor(debug.FrameHistory)
	dbg := debug.New(debug.Options{
		Visible: true,
		GC:      gcMon,
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{
			Style:    core.Style{Width: core.Pt(400), Height: core.Pt(300)},
			Children: []core.Node{dbg.View(cx)},
		}
	})

	if !app.Exists("debug-gc") {
		t.Fatal("debug-gc section not mounted")
	}

	app.Post(func() {
		runtime.GC()
		dbg.RecordFrame(24*time.Millisecond, 4*time.Millisecond, 3*time.Millisecond)
	})
	app.Settle()

	if !dbg.Stats().Latest().GCHitch {
		t.Fatal("FrameStats.Latest().GCHitch = false after runtime.GC(), want true")
	}

	foundGC := false
	foundHitch := false
	for _, txt := range app.Texts() {
		if strings.Contains(txt, "GC (") {
			foundGC = true
		}
		if strings.Contains(txt, "hitches 1") {
			foundHitch = true
		}
	}
	if !foundGC || !foundHitch {
		t.Fatalf("GC overlay texts = %v, want GC header and hitches 1", app.Texts())
	}

	// Graph should include the top-edge GC hitch marker rect at y=0, h=3.
	ops := app.Find("debug-frame-graph").Ops
	foundMarker := false
	for _, op := range ops {
		if op.Kind == core.DrawRect && op.Y == 0 && op.H == 3 {
			foundMarker = true
			break
		}
	}
	if !foundMarker {
		t.Fatal("debug-frame-graph missing top-edge GC hitch marker rect")
	}
}

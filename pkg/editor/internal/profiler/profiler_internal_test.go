package profiler

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/mcp"
)

func TestSetOpenBumpsSeqAndNotifiesOnChange(t *testing.T) {
	notified := 0
	s := New(Host{Notify: func() { notified++ }})
	seq0 := s.Seq()

	s.SetOpen(true)
	if s.Seq() <= seq0 || notified != 1 {
		t.Fatalf("SetOpen(true): Seq %d -> %d, notified %d times, want a larger Seq and 1 notification", seq0, s.Seq(), notified)
	}
	seq1 := s.Seq()
	s.SetOpen(true)
	if s.Seq() != seq1 || notified != 1 {
		t.Fatalf("SetOpen(true) again: Seq %d -> %d, notified %d times, want no change", seq1, s.Seq(), notified)
	}
}

func TestNilStateIsDisconnected(t *testing.T) {
	var s *State
	s.SetOpen(true)
	s.SetScope(nil)
	if s.Open() || s.Connected() || s.PlayActive() || s.Seq() != 0 || s.Metric() != MetricFrame {
		t.Fatal("nil State reports state")
	}
	if err := s.Poll(context.Background()); err != ErrNotConnected {
		t.Fatalf("nil State Poll = %v, want ErrNotConnected", err)
	}
}

func TestProfilerOverflowAndSanitizeHelpers(t *testing.T) {
	if got := msToDuration(math.NaN()); got != 0 {
		t.Fatalf("msToDuration(NaN) = %v, want 0", got)
	}
	if got := msToDuration(math.Inf(1)); got != 0 {
		t.Fatalf("msToDuration(+Inf) = %v, want 0", got)
	}
	if got := msToDuration(-5); got != 0 {
		t.Fatalf("msToDuration(-5) = %v, want 0", got)
	}
	if got := msToDuration(1e18); got <= 0 || got > time.Duration(math.MaxInt64) {
		t.Fatalf("msToDuration(1e18) overflowed: %v", got)
	}

	hugeHeap := uint64(1) << 63
	got := formatUintBytes(hugeHeap)
	if !strings.HasSuffix(got, " GiB") || strings.HasPrefix(got, "0") || strings.HasPrefix(got, "-") {
		t.Fatalf("formatUintBytes(1<<63) = %q, want positive GiB", got)
	}

	zones := sortZones([]mcp.PerfZoneInfo{
		{Name: "nan_zone", LastMs: math.NaN(), AvgMs: math.Inf(1)},
		{Name: "real_zone", LastMs: 2.5, AvgMs: 1.5},
	})
	if len(zones) != 2 || zones[0].Name != "real_zone" || zones[1].LastMs != 0 || zones[1].AvgMs != 0 {
		t.Fatalf("sortZones with NaN/Inf = %+v", zones)
	}
}

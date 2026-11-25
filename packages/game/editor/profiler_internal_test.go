package editor

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/mcp"
)

func TestProfilerSeqUnchangedByUnrelatedEditorMutations(t *testing.T) {
	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	seq0 := ed.profilerSeq()
	rev0 := ed.Revision()

	ed.SetTool(ToolMove)
	ed.SetTool(ToolSelect)
	if ed.Revision() <= rev0 {
		t.Fatal("expected Revision to increase on tool changes")
	}
	if ed.profilerSeq() != seq0 {
		t.Fatalf("profilerSeq changed from %d to %d on unrelated tool mutations", seq0, ed.profilerSeq())
	}

	ed.SetProfilerOpen(true)
	if ed.profilerSeq() <= seq0 {
		t.Fatal("expected profilerSeq to increase on SetProfilerOpen")
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
	got := formatProfilerUintBytes(hugeHeap)
	if !strings.HasSuffix(got, " GiB") || strings.HasPrefix(got, "0") || strings.HasPrefix(got, "-") {
		t.Fatalf("formatProfilerUintBytes(1<<63) = %q, want positive GiB", got)
	}

	zones := sortProfilerZones([]mcp.PerfZoneInfo{
		{Name: "nan_zone", LastMs: math.NaN(), AvgMs: math.Inf(1)},
		{Name: "real_zone", LastMs: 2.5, AvgMs: 1.5},
	})
	if len(zones) != 2 || zones[0].Name != "real_zone" || zones[1].LastMs != 0 || zones[1].AvgMs != 0 {
		t.Fatalf("sortProfilerZones with NaN/Inf = %+v", zones)
	}
}

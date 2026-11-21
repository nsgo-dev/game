//go:build !release

package debug_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMemoryEstimates(t *testing.T) {
	if got := debug.EstimateImageBytes(256, 128); got != 256*128*4 {
		t.Fatalf("EstimateImageBytes(256, 128) = %d, want %d", got, 256*128*4)
	}
	if got := debug.EstimateImageBytes(0, 128); got != 0 {
		t.Fatalf("EstimateImageBytes(0, 128) = %d, want 0", got)
	}
	if got := debug.EstimateImageBytes(-4, 10); got != 0 {
		t.Fatalf("EstimateImageBytes(-4, 10) = %d, want 0", got)
	}

	if got := debug.EstimatePCMBytes(44100, 2, 2); got != 176400 {
		t.Fatalf("EstimatePCMBytes(44100, 2, 2) = %d, want 176400", got)
	}
	if got := debug.EstimatePCMBytes(44100, 0, 2); got != 0 {
		t.Fatalf("EstimatePCMBytes(44100, 0, 2) = %d, want 0", got)
	}
}

func TestMemoryTrackerGroupsReleaseAndBudgets(t *testing.T) {
	mem := debug.NewMemoryTracker()
	mem.SetBudget("level1", 200_000)
	mem.SetBudget("ui", 100_000)
	mem.SetTotalBudget(250_000)

	bgBytes := debug.EstimateImageBytes(128, 128) // 65,536
	atlasBytes := debug.EstimateImageBytes(64, 64)
	bgmBytes := debug.EstimatePCMBytes(44100, 1, 2) // 88,200
	uiBytes := debug.EstimateImageBytes(64, 64)     // 16,384

	mem.Track("level1", "bg.png", debug.AssetImage, bgBytes)
	mem.Track("level1", "sprites.atlas", debug.AssetAtlas, atlasBytes)
	mem.Track("level1", "theme.pcm", debug.AssetAudio, bgmBytes)
	mem.Track("ui", "icons.png", debug.AssetImage, uiBytes)

	wantL1 := bgBytes + atlasBytes + bgmBytes
	if got := mem.GroupBytes("level1"); got != wantL1 {
		t.Fatalf("GroupBytes(level1) = %d, want %d", got, wantL1)
	}
	if got := mem.TotalBytes(); got != wantL1+uiBytes {
		t.Fatalf("TotalBytes() = %d, want %d", got, wantL1+uiBytes)
	}

	// Under all budgets initially.
	if err := mem.CheckBudget("level1"); err != nil {
		t.Fatalf("CheckBudget(level1) unexpected error = %v", err)
	}
	if err := mem.CheckAllBudgets(); err != nil {
		t.Fatalf("CheckAllBudgets() unexpected error = %v", err)
	}

	// Re-tracking an existing asset replaces its size instead of double counting.
	mem.Track("ui", "icons.png", debug.AssetImage, 2*uiBytes)
	if got := mem.GroupBytes("ui"); got != 2*uiBytes {
		t.Fatalf("GroupBytes(ui) after update = %d, want %d", got, 2*uiBytes)
	}

	// Inspect sorted groups and per-kind breakdown.
	groups := mem.Groups()
	if len(groups) != 2 || groups[0].Group != "level1" || groups[1].Group != "ui" {
		t.Fatalf("Groups() = %+v, want sorted [level1, ui]", groups)
	}
	if groups[0].ByKind[debug.AssetImage] != bgBytes || groups[0].ByKind[debug.AssetAtlas] != atlasBytes || groups[0].ByKind[debug.AssetAudio] != bgmBytes {
		t.Fatalf("level1 ByKind = %+v", groups[0].ByKind)
	}

	// Exceed level1 budget and total budget.
	mem.Track("level1", "boss.png", debug.AssetImage, 150_000)
	if err := mem.CheckBudget("level1"); err == nil || !strings.Contains(err.Error(), "level1") {
		t.Fatalf("CheckBudget(level1) = %v, want budget error", err)
	}
	if err := mem.CheckAllBudgets(); err == nil || !strings.Contains(err.Error(), "total asset memory") {
		t.Fatalf("CheckAllBudgets() = %v, want total + group budget error", err)
	}

	// Untrack single asset brings level1 back under budget.
	mem.Untrack("level1", "boss.png")
	if err := mem.CheckBudget("level1"); err != nil {
		t.Fatalf("CheckBudget(level1) after Untrack = %v, want nil", err)
	}

	// ReleaseGroup drops all tracked bytes for level1 while keeping ui intact.
	mem.ReleaseGroup("level1")
	if got := mem.GroupBytes("level1"); got != 0 {
		t.Fatalf("GroupBytes(level1) after ReleaseGroup = %d, want 0", got)
	}
	if got := mem.TotalBytes(); got != 2*uiBytes {
		t.Fatalf("TotalBytes() after ReleaseGroup = %d, want %d", got, 2*uiBytes)
	}
	if err := mem.CheckAllBudgets(); err != nil {
		t.Fatalf("CheckAllBudgets() after ReleaseGroup = %v, want nil", err)
	}
}

func TestMemoryOverlayDisplay(t *testing.T) {
	mem := debug.NewMemoryTracker()
	mem.SetTotalBudget(1 << 20)
	mem.Track("level1", "map.png", debug.AssetImage, 512<<10)

	dbg := debug.New(debug.Options{
		Visible: true,
		Memory:  mem,
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{
			Style:    core.Style{Width: core.Pt(400), Height: core.Pt(300)},
			Children: []core.Node{dbg.View(cx)},
		}
	})

	if !app.Exists("debug-memory") {
		t.Fatal("debug-memory section not mounted")
	}

	app.Post(func() {
		dbg.RecordFrame(16*time.Millisecond, 2*time.Millisecond, 2*time.Millisecond)
	})
	app.Settle()

	texts := app.Texts()
	if !slices.Contains(texts, "level1") {
		t.Fatalf("overlay texts %v missing group 'level1'", texts)
	}
	foundMem := false
	for _, txt := range texts {
		if strings.Contains(txt, "Memory:") && strings.Contains(txt, "KB") {
			foundMem = true
			break
		}
	}
	if !foundMem {
		t.Fatalf("overlay texts %v missing Memory summary", texts)
	}
}

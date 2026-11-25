//go:build release

package debug

import (
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/framedata"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// Enabled reports whether debug overlays are compiled into this build. It is false in release builds.
func Enabled() bool { return false }

var (
	releaseOverlay Overlay
	releaseStats   FrameStats
)

// Overlay is a zero-cost stub in release builds.
type Overlay struct{}

// New returns a no-op Overlay in release builds.
func New(Options) *Overlay { return &releaseOverlay }

// Attach is a no-op in release builds.
func (*Overlay) Attach(*core.Scope) {}

// Visible is always false in release builds.
func (*Overlay) Visible() bool { return false }

// SetVisible is a no-op in release builds.
func (*Overlay) SetVisible(bool) {}

// Toggle is a no-op in release builds.
func (*Overlay) Toggle() {}

// RecordFrame is a no-op in release builds.
func (*Overlay) RecordFrame(time.Duration, time.Duration, time.Duration) {}

// NoteFrameCause is a no-op in release builds.
func (*Overlay) NoteFrameCause(string) {}

// WrapLoop is a no-op in release builds.
func (*Overlay) WrapLoop(*game.Loop) {}

// Stats returns a no-op FrameStats in release builds.
func (*Overlay) Stats() *FrameStats { return &releaseStats }

// Console returns a no-op Console in release builds.
func (*Overlay) Console() *Console { return &releaseConsole }

// Profiler returns a no-op Profiler in release builds.
func (*Overlay) Profiler() *Profiler { return &releaseProfiler }

// Memory returns a no-op MemoryTracker in release builds.
func (*Overlay) Memory() *MemoryTracker { return &releaseMemoryTracker }

// GC returns a no-op GCMonitor in release builds.
func (*Overlay) GC() *GCMonitor { return &releaseGCMonitor }

// SetDrawStats is a no-op in release builds.
func (*Overlay) SetDrawStats(DrawStats) {}

// DrawStats always returns the zero DrawStats in release builds.
func (*Overlay) DrawStats() DrawStats { return DrawStats{} }

// HasDrawStats always returns false in release builds.
func (*Overlay) HasDrawStats() bool { return false }

// ResetStats is a no-op in release builds.
func (*Overlay) ResetStats() {}

// SetScenes is a no-op in release builds.
func (*Overlay) SetScenes([]SceneSnapshot) {}

// RefreshScenes is a no-op in release builds.
func (*Overlay) RefreshScenes() {}

// DrawShapes is a no-op in release builds.
func (*Overlay) DrawShapes(*core.Drawing, func(*ShapeOverlay)) {}

// DrawBox is a no-op in release builds.
func (*Overlay) DrawBox(*core.Drawing, core.Bounds, core.Color) {}

// DrawCircle is a no-op in release builds.
func (*Overlay) DrawCircle(*core.Drawing, float32, float32, float32, core.Color) {}

// DrawSpriteBounds is a no-op in release builds.
func (*Overlay) DrawSpriteBounds(*core.Drawing, *sprite.Sprite, core.Color) {}

// DrawBatchBounds is a no-op in release builds.
func (*Overlay) DrawBatchBounds(*core.Drawing, *sprite.Batch, core.Color) {}

// DrawFighterBoxes is a no-op in release builds.
func (*Overlay) DrawFighterBoxes(*core.Drawing, *framedata.Fighter, core.Color, core.Color) {}

// View returns nil in release builds so no overlay views are built or linked.
func (*Overlay) View(*core.Scope) core.Node { return nil }

// FrameStats is a zero-cost stub in release builds.
type FrameStats struct{}

// Record is a no-op in release builds.
func (*FrameStats) Record(time.Duration, time.Duration, time.Duration) {}

// Reset is a no-op in release builds.
func (*FrameStats) Reset() {}

// Len always returns 0 in release builds.
func (*FrameStats) Len() int { return 0 }

// At always returns the zero FrameSample in release builds.
func (*FrameStats) At(int) FrameSample { return FrameSample{} }

// Latest always returns the zero FrameSample in release builds.
func (*FrameStats) Latest() FrameSample { return FrameSample{} }

// Samples returns dst unchanged in release builds.
func (*FrameStats) Samples(dst []FrameSample) []FrameSample { return dst }

// Average always returns the zero FrameSample in release builds.
func (*FrameStats) Average() FrameSample { return FrameSample{} }

// FPS always returns 0 in release builds.
func (*FrameStats) FPS() float64 { return 0 }

// MaxTotal always returns 0 in release builds.
func (*FrameStats) MaxTotal() time.Duration { return 0 }

// DrawGraph is a no-op in release builds.
func (*FrameStats) DrawGraph(*core.Drawing, core.Size, float64) {}

// ShapeOverlay is a zero-cost stub in release builds.
type ShapeOverlay struct {
	// LineWidth is ignored in release builds.
	LineWidth float32
}

// DrawBox is a no-op in release builds.
func (*ShapeOverlay) DrawBox(*core.Drawing, core.Bounds, core.Color) {}

// DrawCircle is a no-op in release builds.
func (*ShapeOverlay) DrawCircle(*core.Drawing, float32, float32, float32, core.Color) {}

// DrawSpriteBounds is a no-op in release builds.
func (*ShapeOverlay) DrawSpriteBounds(*core.Drawing, *sprite.Sprite, core.Color) {}

// DrawBatchBounds is a no-op in release builds.
func (*ShapeOverlay) DrawBatchBounds(*core.Drawing, *sprite.Batch, core.Color) {}

// DrawFighterBoxes is a no-op in release builds.
func (*ShapeOverlay) DrawFighterBoxes(*core.Drawing, *framedata.Fighter, core.Color, core.Color) {}

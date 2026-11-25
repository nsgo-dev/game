//go:build release

package ai

import (
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// InfluenceHeatmapEnabled reports whether DrawHeatmap is compiled into this build.
// It is false when built with -tags release.
func InfluenceHeatmapEnabled() bool { return false }

// DrawHeatmap is a no-op stub in release builds so debug heatmap rendering is compiled out.
func (*InfluenceLayer) DrawHeatmap(*core.Drawing, *tilemap.Camera, HeatmapOptions) {}

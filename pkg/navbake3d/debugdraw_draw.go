//go:build !release

package navbake3d

import (
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/nsgo/core"
)

// DrawDebug strokes m's polygon outlines and, unless style.HideDetail is set, its detail triangles onto d, projected by viewProj, a [0, 1] depth projection such as math3d.Perspective or a render3d camera's, into viewport, clipped at the near plane. Zero style fields take DefaultDebugStyle's. In a release build it draws nothing.
func DrawDebug(d *core.Drawing, viewProj math3d.Mat4, viewport core.Bounds, m *Mesh, style DebugStyle) {
	if d == nil || m == nil {
		return
	}
	def := DefaultDebugStyle()
	if style.PolygonColor == 0 {
		style.PolygonColor = def.PolygonColor
	}
	if style.DetailColor == 0 {
		style.DetailColor = def.DetailColor
	}
	if style.LineWidth <= 0 {
		style.LineWidth = def.LineWidth
	}
	vp := physics3d.Mat4(viewProj)
	lift := math3d.Vec3{Y: style.Lift}
	lines := m.DebugLines(style.HideDetail)
	outlines := len(m.DebugLines(true))
	for i, l := range lines {
		col := style.PolygonColor
		if i >= outlines {
			col = style.DetailColor
		}
		a, b := l[0].Add(lift), l[1].Add(lift)
		physics3d.DrawLine3DZO(d, vp, viewport, physics3d.Vec3{X: a.X, Y: a.Y, Z: a.Z}, physics3d.Vec3{X: b.X, Y: b.Y, Z: b.Z}, col, style.LineWidth)
	}
}

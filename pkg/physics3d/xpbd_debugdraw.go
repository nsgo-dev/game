//go:build !release

package physics3d

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// DrawSoftWorld projects and draws XPBD structural constraint wireframes, rigid
// attachments, and pinned particle markers from sw onto d using viewProj, viewport,
// and style (respecting style.DepthZO). Release builds compile this to a no-op.
func DrawSoftWorld(d *core.Drawing, viewProj Mat4, viewport core.Bounds, sw *SoftWorld, style DebugStyle) {
	if d == nil || sw == nil || len(sw.Particles) == 0 {
		return
	}
	st := resolveDebugStyle(style)

	// 1. Structural distance constraint edges.
	edges := sw.Edges()
	if len(edges) > 0 {
		drawEdges(d, viewProj, viewport, edges, st.SphereColor, st.LineWidth, st.DepthZO)
	}

	// 2. Rigid-body attachments.
	var attEdges [][2]Vec3
	for _, att := range sw.Attachments {
		if att.Body == nil || !sw.validParticle(att.Particle) {
			continue
		}
		anchor := att.Body.Pos.Add(normalizedQuat(att.Body.Rot).RotateVec(att.LocalAnchor))
		attEdges = append(attEdges, [2]Vec3{sw.Particles[att.Particle].Pos, anchor})
	}
	if len(attEdges) > 0 {
		drawEdges(d, viewProj, viewport, attEdges, st.RayColor, st.LineWidth, st.DepthZO)
	}

	// 3. Pinned particle markers.
	pinR := st.LineWidth * 2
	if pinR < 3 {
		pinR = 3
	}
	pinStarted := false
	for i := range sw.Particles {
		p := &sw.Particles[i]
		if !p.IsPinned() {
			continue
		}
		pt, ndcZ, ok := Project(viewProj, viewport, p.Pos)
		if !ok || (st.DepthZO && ndcZ < 0) || !pointInBounds(viewport, pt) {
			continue
		}
		if !pinStarted {
			d.Save()
			d.SetFillColor(st.ContactColor)
			d.BeginPath()
			pinStarted = true
		}
		d.MoveTo(float32(pt.X)+pinR, float32(pt.Y))
		d.Arc(float32(pt.X), float32(pt.Y), pinR, 0, float32(2*math.Pi), false)
	}
	if pinStarted {
		d.Fill()
		d.Restore()
	}
}

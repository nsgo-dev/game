//go:build !release

package physics

import "github.com/nsgo-dev/nsgo/core"

// DrawSoftWorld draws XPBD particles, structural distance constraints, area polygons,
// and rigid-body attachments from sw onto d using style. Pinned particles are highlighted
// in ContactColor and free particles in CircleColor. It preserves d's drawing state.
func DrawSoftWorld(d *core.Drawing, sw *SoftWorld, style DebugStyle) {
	if d == nil || sw == nil || len(sw.Particles) == 0 {
		return
	}
	st := resolveDebugStyle(d, style)
	d.Save()

	// 1. Structural distance constraints (skip internal unilateral MaxOnly tethers).
	edgeStarted := false
	for _, c := range sw.Distances {
		if c.MaxOnly || !sw.validParticle(c.A) || !sw.validParticle(c.B) {
			continue
		}
		if !edgeStarted {
			d.SetStrokeColor(st.boxColor)
			d.SetLineWidth(st.lineWidth)
			d.BeginPath()
			edgeStarted = true
		}
		pa, pb := sw.Particles[c.A], sw.Particles[c.B]
		d.MoveTo(pa.X, pa.Y)
		d.LineTo(pb.X, pb.Y)
	}
	if edgeStarted {
		d.Stroke()
	}

	// 2. Rigid-body attachments.
	attStarted := false
	for _, att := range sw.Attachments {
		if att.Body == nil || !sw.validParticle(att.Particle) {
			continue
		}
		if !attStarted {
			d.SetStrokeColor(st.rayColor)
			d.SetLineWidth(st.lineWidth)
			d.BeginPath()
			attStarted = true
		}
		p := sw.Particles[att.Particle]
		anchor := att.Body.LocalToWorld(att.LocalAnchor)
		d.MoveTo(p.X, p.Y)
		d.LineTo(float32(anchor.X), float32(anchor.Y))
	}
	if attStarted {
		d.Stroke()
	}

	// 3. Particles (free particles stroked in CircleColor; pinned particles filled in ContactColor).
	freeStarted := false
	for i := range sw.Particles {
		p := &sw.Particles[i]
		r := p.Radius
		if r <= 0 {
			r = st.contactRadius
		}
		if p.IsPinned() {
			continue
		}
		if !freeStarted {
			d.SetStrokeColor(st.circleColor)
			d.SetLineWidth(st.lineWidth)
			d.BeginPath()
			freeStarted = true
		}
		d.MoveTo(p.X+r, p.Y)
		d.Arc(p.X, p.Y, r, 0, twoPi, false)
	}
	if freeStarted {
		d.Stroke()
	}

	pinStarted := false
	for i := range sw.Particles {
		p := &sw.Particles[i]
		if !p.IsPinned() {
			continue
		}
		r := p.Radius
		if r <= 0 {
			r = st.contactRadius
		}
		if !pinStarted {
			d.SetFillColor(st.contactColor)
			d.BeginPath()
			pinStarted = true
		}
		d.MoveTo(p.X+r, p.Y)
		d.Arc(p.X, p.Y, r, 0, twoPi, false)
	}
	if pinStarted {
		d.Fill()
	}

	d.Restore()
}

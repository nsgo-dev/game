package text

import (
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
)

// Facing is how text placed in a 3D world turns.
type Facing uint8

const (
	// FacingBillboard turns the text to face the camera, upright on the screen, as name labels and damage numbers do. This is the zero value.
	FacingBillboard Facing = iota
	// FacingFixed lays the text in a plane of the world that WorldText.Rot sets, as a sign on a wall is: seen from the side it is foreshortened, seen from behind it is mirrored unless CullBackFace is set.
	FacingFixed
)

// WorldText is text placed in a 3D world, drawn by SDFFont.DrawWorld.
type WorldText struct {
	// Text is the text, with "\n" between lines.
	Text string
	// Pos is the anchor's position in the world.
	Pos math3d.Vec3
	// Facing is how the text turns.
	Facing Facing
	// Rot orients FacingFixed text: its lines run along Rot's +X, its glyphs stand along +Y, and it reads from the +Z side. The zero Quat means the identity, text in the XY plane read from +Z.
	Rot math3d.Quat
	// Size is the em size in world units, so the text shrinks with distance under a perspective camera.
	Size float32
	// ScreenSize, when > 0, makes FacingBillboard text keep an em of ScreenSize points at any distance, in place of Size; Options.MaxWidth is then in points.
	ScreenSize float32
	// AnchorX and AnchorY are the point of the text's box that sits at Pos, as fractions of its width and height from its top left: (0, 0) is the top left, (0.5, 0.5) the center and (0.5, 1) the bottom middle. The box is [0, MaxWidth] wide when Options.MaxWidth > 0, else the widest line's, placed as Options.Align places a line at x.
	AnchorX, AnchorY float32
	// CullBackFace draws nothing of FacingFixed text seen from behind.
	CullBackFace bool
	// Options sets the color, outline, alignment, wrapping, opacity and pixel density; its Size is ignored and its MaxWidth is in world units.
	Options SDFOptions
}

// WorldGlyph is one glyph of world-space text projected onto the screen.
type WorldGlyph struct {
	// Glyph is the font's glyph.
	Glyph Glyph
	// Transform maps the glyph's atlas rectangle, in atlas pixels from its top left, into the drawing's points. For FacingFixed text it is the affine transform closest to the perspective at the glyph's center, which, for glyphs small on the screen, the perspective across one glyph cannot tell apart.
	Transform math2d.Affine
}

// WorldLayout is world-space text projected onto the screen by SDFFont.LayoutWorld, ready for SDFFont.DrawLayout.
type WorldLayout struct {
	// Depth is the anchor's depth from the camera's near plane (0) to its far plane (1), for drawing labels back to front: sort by Depth, highest first. For FacingFixed text whose anchor is behind the camera or past the far plane, it is the mean depth of the visible glyphs' centers.
	Depth float32
	// Glyphs are the glyphs that reach the viewport, in the text's order.
	Glyphs []WorldGlyph
	// Options are the WorldText's options the glyphs draw with.
	Options SDFOptions
}

// minClipW is the smallest homogeneous w a projected point may have: nearer to the camera's plane than this, it is behind the camera.
const minClipW = 1e-5

// LayoutWorld projects t through cam onto viewport, the camera's view in the drawing's points (as render3d.ProjectSegment's), and reports whether any of it is visible.
//
// FacingBillboard text is hidden as a whole when its anchor is outside the camera's near and far planes. FacingFixed text is hidden glyph by glyph: a glyph reaching behind the near plane or past the far plane is left out. Glyphs wholly outside the viewport are left out of both.
func (f *SDFFont) LayoutWorld(cam *scene3d.Camera, viewport core.Bounds, t WorldText) (WorldLayout, bool) {
	out := WorldLayout{Options: t.Options}
	if f == nil || cam == nil || t.Text == "" || t.Options.Alpha < 0 || viewport.Size.Width <= 0 || viewport.Size.Height <= 0 {
		return out, false
	}
	screenSized := t.Facing == FacingBillboard && t.ScreenSize > 0
	em := t.Size
	if screenSized {
		em = t.ScreenSize
	}
	if !(em > 0) || !finite32(em) {
		return out, false
	}
	// unit is the world units (or, screen sized, points) an atlas pixel spans.
	unit := em / f.Size
	opts := t.Options
	opts.MaxWidth = t.Options.MaxWidth / unit
	lo := f.layoutOptions(opts, 1)
	lf := f.layoutFont()
	size := lf.Measure(t.Text, lo)
	boxW := float32(size.Width)
	boxLeft := alignedX(0, boxW, 0, opts.Align)
	if opts.MaxWidth > 0 {
		boxW, boxLeft = opts.MaxWidth, 0
	}
	ox := -(boxLeft + t.AnchorX*boxW)
	oy := -t.AnchorY * float32(size.Height)
	placements := lf.Layout(t.Text, ox, oy, lo)
	if len(placements) == 0 {
		return out, false
	}

	vp := cam.ViewProj()
	view := viewRect{x: float32(viewport.Origin.X), y: float32(viewport.Origin.Y), w: float32(viewport.Size.Width), h: float32(viewport.Size.Height)}
	anchor := vp.MulVec4(t.Pos.Vec4(1))
	anchorIn := anchor.W > minClipW && anchor.Z >= 0 && anchor.Z <= anchor.W
	if anchorIn {
		out.Depth = anchor.Z / anchor.W
	}

	switch t.Facing {
	case FacingFixed:
		rot := t.Rot
		if rot == (math3d.Quat{}) {
			rot = math3d.QuatIdentity()
		}
		rot = rot.Norm()
		var depthSum float32
		right := rot.RotateVec3(math3d.Vec3Right())
		up := rot.RotateVec3(math3d.Vec3Up())
		if t.CullBackFace {
			normal := rot.RotateVec3(math3d.Vec3UnitZ())
			toCam := cam.Pos.Sub(t.Pos)
			if cam.Projection.Kind == scene3d.ProjectionOrthographic {
				toCam = cam.Forward().Scale(-1)
			}
			if normal.Dot(toCam) <= 0 {
				return out, false
			}
		}
		// world returns the world position of the text's atlas point (x, y), y down its lines.
		world := func(x, y float32) math3d.Vec3 {
			return t.Pos.Add(right.Scale(x * unit)).Sub(up.Scale(y * unit))
		}
		for _, p := range placements {
			w, h := p.W, p.H
			c := world(p.X+w/2, p.Y+h/2)
			cs, depth, ok := view.project(vp, c, true)
			if !ok {
				continue
			}
			rx := right.Scale(w / 2 * unit)
			uy := up.Scale(h / 2 * unit)
			r0, _, ok0 := view.project(vp, c.Sub(rx), false)
			r1, _, ok1 := view.project(vp, c.Add(rx), false)
			d0, _, ok2 := view.project(vp, c.Add(uy), false)
			d1, _, ok3 := view.project(vp, c.Sub(uy), false)
			if !ok0 || !ok1 || !ok2 || !ok3 {
				continue
			}
			jx := r1.Sub(r0).Scale(1 / w)
			jy := d1.Sub(d0).Scale(1 / h)
			m := math2d.Affine{
				A: jx.X, B: jx.Y, C: jy.X, D: jy.Y,
				Tx: cs.X - jx.X*w/2 - jy.X*h/2,
				Ty: cs.Y - jx.Y*w/2 - jy.Y*h/2,
			}
			if view.overlaps(m, p.Glyph) {
				out.Glyphs = append(out.Glyphs, WorldGlyph{Glyph: p.Glyph, Transform: m})
				depthSum += depth
			}
		}
		if !anchorIn && len(out.Glyphs) > 0 {
			out.Depth = depthSum / float32(len(out.Glyphs))
		}
	default:
		if !anchorIn {
			return out, false
		}
		s, _, _ := view.project(vp, t.Pos, false)
		k := unit
		if !screenSized {
			// An offset along the camera's up keeps the anchor's w, so it moves proj[5]/w in NDC per world unit.
			k = unit * cam.ProjMatrix()[5] * 0.5 * view.h / anchor.W
		}
		for _, p := range placements {
			m := math2d.Affine{A: k, D: k, Tx: s.X + k*p.X, Ty: s.Y + k*p.Y}
			if view.overlaps(m, p.Glyph) {
				out.Glyphs = append(out.Glyphs, WorldGlyph{Glyph: p.Glyph, Transform: m})
			}
		}
	}
	return out, len(out.Glyphs) > 0
}

// DrawLayout draws world-space text that LayoutWorld projected into d.
func (f *SDFFont) DrawLayout(d *core.Drawing, l WorldLayout) {
	if f == nil || d == nil || len(l.Glyphs) == 0 || l.Options.Alpha < 0 {
		return
	}
	st, ps := l.Options.style(), l.Options.pixelScale()
	f.beginAlpha(d, l.Options.Alpha)
	for _, g := range l.Glyphs {
		f.drawGlyph(d, g.Glyph, g.Transform, ps, st)
	}
	f.endAlpha(d, l.Options.Alpha)
}

// DrawWorld projects t through cam onto viewport, as LayoutWorld does, and draws it into d. To draw overlapping labels back to front, lay them out with LayoutWorld, sort them by Depth and draw them with DrawLayout instead.
func (f *SDFFont) DrawWorld(d *core.Drawing, cam *scene3d.Camera, viewport core.Bounds, t WorldText) {
	if l, ok := f.LayoutWorld(cam, viewport, t); ok {
		f.DrawLayout(d, l)
	}
}

type viewRect struct{ x, y, w, h float32 }

// project returns where p falls in the viewport and its depth from the near plane (0) to the far plane (1), and false when it is behind the near plane or, if far is set, past the far plane.
func (v viewRect) project(vp math3d.Mat4, p math3d.Vec3, far bool) (math2d.Vec2, float32, bool) {
	c := vp.MulVec4(p.Vec4(1))
	if c.W <= minClipW || c.Z < 0 || far && c.Z > c.W {
		return math2d.Vec2{}, 0, false
	}
	inv := 1 / c.W
	return math2d.Vec2{
		X: v.x + (c.X*inv*0.5+0.5)*v.w,
		Y: v.y + (0.5-c.Y*inv*0.5)*v.h,
	}, c.Z * inv, true
}

// overlaps reports whether g's rectangle under m reaches the viewport.
func (v viewRect) overlaps(m math2d.Affine, g Glyph) bool {
	b := m.TransformRect(math2d.Rect{W: g.Frame.W, H: g.Frame.H})
	return b.X < v.x+v.w && b.X+b.W > v.x && b.Y < v.y+v.h && b.Y+b.H > v.y
}

package render3d

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/nsgo/core"
)

// degenerateLenSq is the squared length below which a direction is treated as zero when building camera-facing geometry.
const degenerateLenSq = 1e-12

// CameraBasis is a camera's world-space position and unit axes, the input that billboards and trails turn toward (#281).
//
// Build one from a view and projection matrix with [NewCameraBasis], or fill it from a scene3d camera as CameraBasis{Pos: cam.Pos, Right: cam.Right(), Up: cam.Up(), Forward: cam.Forward(), Orthographic: cam.Projection.Kind == scene3d.ProjectionOrthographic}.
type CameraBasis struct {
	// Pos is the camera's world-space position.
	Pos math3d.Vec3
	// Right is the camera's unit world-space right axis (+X in view space).
	Right math3d.Vec3
	// Up is the camera's unit world-space up axis (+Y in view space).
	Up math3d.Vec3
	// Forward is the camera's unit world-space viewing direction (-Z in view space).
	Forward math3d.Vec3
	// Orthographic makes every point see the camera along -Forward instead of along the line to Pos.
	Orthographic bool
}

// NewCameraBasis derives a CameraBasis from a right-handed view matrix and its projection matrix. It reports false when view is singular or yields a non-finite or degenerate basis. A projection whose bottom row is (0, 0, 0, w != 0) marks the basis orthographic.
func NewCameraBasis(view, proj math3d.Mat4) (CameraBasis, bool) {
	inv, ok := view.Inverse()
	if !ok {
		return CameraBasis{}, false
	}
	right, okR := unitVec(inv.TransformDir(math3d.Vec3Right()))
	up, okU := unitVec(inv.TransformDir(math3d.Vec3Up()))
	fwd, okF := unitVec(inv.TransformDir(math3d.Vec3Forward()))
	pos := inv.TransformPoint(math3d.Vec3Zero())
	if !okR || !okU || !okF || !pos.IsFinite() {
		return CameraBasis{}, false
	}
	return CameraBasis{
		Pos:          pos,
		Right:        right,
		Up:           up,
		Forward:      fwd,
		Orthographic: proj.At(3, 0) == 0 && proj.At(3, 1) == 0 && proj.At(3, 2) == 0 && proj.At(3, 3) != 0,
	}, true
}

// ToCamera returns the unit direction from p toward the viewer: -Forward for an orthographic basis, or when p coincides with Pos, and the normalized line from p to Pos otherwise.
func (c CameraBasis) ToCamera(p math3d.Vec3) math3d.Vec3 {
	back, ok := unitVec(c.Forward.Neg())
	if !ok {
		back = math3d.Vec3UnitZ() // same fallback as cameraAxes
	}
	if c.Orthographic {
		return back
	}
	if d, ok := unitVec(c.Pos.Sub(p)); ok {
		return d
	}
	return back
}

// BillboardMode selects how a [Billboard] turns toward the camera.
type BillboardMode uint8

const (
	// BillboardViewPlane lies in the camera's view plane, using the camera's right and up axes. Every quad is parallel to the screen and never shears, which suits particles, sprites and impostors.
	BillboardViewPlane BillboardMode = iota
	// BillboardViewPoint turns its normal straight at the camera's position, keeping the camera's up as the roll reference. Large quads near the screen edge stay undistorted, at the cost of neighbouring quads no longer being parallel.
	BillboardViewPoint
	// BillboardAxial rotates only about [Billboard.Axis] (cylindrical billboarding), for trees, grass, flames and beams that must stay upright. Its normal is the direction to the camera with the axis component removed, so a camera looking straight along the axis sees the quad edge-on.
	BillboardAxial
)

// String returns a human-readable billboard mode name.
func (m BillboardMode) String() string {
	switch m {
	case BillboardViewPlane:
		return "view_plane"
	case BillboardViewPoint:
		return "view_point"
	case BillboardAxial:
		return "axial"
	default:
		return "unknown"
	}
}

// Billboard is a camera-facing quad in world space (#281).
type Billboard struct {
	// Center is the world-space point the quad's Pivot is placed at.
	Center math3d.Vec3
	// Width and Height are the quad's size in world units. A quad with a non-positive or non-finite size is not built.
	Width, Height float32
	// Pivot is the point of the quad placed at Center, in quad units where (0, 0) is the middle and ±0.5 are the edges; (0, -0.5) stands the quad on its bottom edge.
	Pivot math3d.Vec2
	// Rotation rolls the quad counter-clockwise as seen by the viewer, in radians, about its normal. Axial billboards ignore it so they stay aligned with their axis.
	Rotation float32
	// Mode selects how the quad turns toward the camera; the zero value is [BillboardViewPlane].
	Mode BillboardMode
	// Axis is the world-space axis an axial billboard rotates about and points its up edge along; a zero or non-finite axis means +Y.
	Axis math3d.Vec3
	// Color fills the quad on the [*core.Drawing] path, and its alpha fades a textured quad; zero means opaque white.
	Color core.Color
	// Texture is an optional image mapped across the quad on the [*core.Drawing] path, with its top-left at the quad's top-left corner.
	Texture core.ImageSource
}

// BillboardQuad is the world-space geometry of a [Billboard] facing a camera.
//
// Corners run counter-clockwise as seen by the viewer: bottom-left, bottom-right, top-right, top-left, with texture coordinates (0, 1), (1, 1), (1, 0) and (0, 0). Right × Up == Normal, so a back-face-culling pipeline keeps the quad.
type BillboardQuad struct {
	// Corners are the quad's world-space corners in the order above.
	Corners [4]math3d.Vec3
	// Center is the quad's geometric middle, which differs from Billboard.Center when Pivot is not zero.
	Center math3d.Vec3
	// Right, Up and Normal are the quad's unit axes; Normal points toward the camera.
	Right, Up, Normal math3d.Vec3
	// Width and Height are the quad's size in world units.
	Width, Height float32
}

// Quad builds the billboard's geometry facing cam. It reports false when the billboard's size or center is non-finite, its size is not positive, or a corner overflows to a non-finite position.
func (b Billboard) Quad(cam CameraBasis) (BillboardQuad, bool) {
	if !(b.Width > 0) || !(b.Height > 0) || isInf32(b.Width) || isInf32(b.Height) || !b.Center.IsFinite() {
		return BillboardQuad{}, false
	}
	right, up, normal := b.axes(cam)
	px, py := finiteOr0(b.Pivot.X), finiteOr0(b.Pivot.Y)
	q := BillboardQuad{Right: right, Up: up, Normal: normal, Width: b.Width, Height: b.Height}
	rx := right.Scale(b.Width)
	uy := up.Scale(b.Height)
	q.Center = b.Center.Sub(rx.Scale(px)).Sub(uy.Scale(py))
	hr, hu := rx.Scale(0.5), uy.Scale(0.5)
	q.Corners = [4]math3d.Vec3{
		q.Center.Sub(hr).Sub(hu),
		q.Center.Add(hr).Sub(hu),
		q.Center.Add(hr).Add(hu),
		q.Center.Sub(hr).Add(hu),
	}
	if !q.Center.IsFinite() {
		return BillboardQuad{}, false
	}
	// A finite center with a huge size can still overflow at the corners.
	for _, c := range q.Corners {
		if !c.IsFinite() {
			return BillboardQuad{}, false
		}
	}
	return q, true
}

func (b Billboard) axes(cam CameraBasis) (right, up, normal math3d.Vec3) {
	camRight, camUp, back := cameraAxes(cam)
	switch b.Mode {
	case BillboardAxial:
		axis, ok := unitVec(b.Axis)
		if !ok {
			axis = math3d.Vec3Up()
		}
		// Fall back through directions that cannot all be parallel to axis.
		for _, cand := range [...]math3d.Vec3{cam.ToCamera(b.Center), back, camUp, camRight} {
			if n, ok := unitVec(cand.Sub(axis.Scale(cand.Dot(axis)))); ok {
				return axis.Cross(n), axis, n
			}
		}
		return camRight, camUp, back
	case BillboardViewPoint:
		n := cam.ToCamera(b.Center)
		r, ok := unitVec(camUp.Cross(n))
		if !ok {
			r, ok = unitVec(camRight.Sub(n.Scale(camRight.Dot(n))))
			if !ok {
				r = camRight
			}
		}
		right, up, normal = r, n.Cross(r), n
	default:
		right, up, normal = camRight, camUp, back
	}
	if b.Rotation != 0 && !isNaN32(b.Rotation) && !isInf32(b.Rotation) {
		s, c := math.Sincos(float64(b.Rotation))
		sf, cf := float32(s), float32(c)
		right, up = right.Scale(cf).Add(up.Scale(sf)), up.Scale(cf).Sub(right.Scale(sf))
	}
	return right, up, normal
}

// cameraAxes returns an orthonormal right-handed (right, up, back) frame from cam, repairing a basis whose axes are not unit or not perpendicular.
func cameraAxes(cam CameraBasis) (right, up, back math3d.Vec3) {
	back, ok := unitVec(cam.Forward.Neg())
	if !ok {
		back = math3d.Vec3UnitZ()
	}
	right, ok = unitVec(cam.Up.Cross(back))
	if !ok {
		right, ok = unitVec(cam.Right.Sub(back.Scale(cam.Right.Dot(back))))
		if !ok {
			right = anyPerpendicular(back)
		}
	}
	return right, back.Cross(right), back
}

// Transform returns the matrix that maps the unit quad [-0.5, 0.5]² in its XY plane onto q, with +Z along q.Normal, for instancing a shared quad mesh.
func (q BillboardQuad) Transform() math3d.Mat4 {
	r := q.Right.Scale(q.Width)
	u := q.Up.Scale(q.Height)
	return math3d.Mat4FromCols(r.Vec4(0), u.Vec4(0), q.Normal.Vec4(0), q.Center.Vec4(1))
}

// Bounds returns the world-space AABB of q's corners.
func (q BillboardQuad) Bounds() math3d.AABB {
	b := math3d.AABBEmpty()
	for _, c := range q.Corners {
		b = b.ExpandPoint(c)
	}
	return b
}

// DrawItem returns template with Transform, WorldBounds, Triangles and Vertices taken from q, so the quad can be sorted by [Queue.Build]. A zero template Blend is replaced by [BlendAlphaBlend], since billboards are usually translucent.
func (q BillboardQuad) DrawItem(template DrawItem) DrawItem {
	template.Transform = q.Transform()
	template.WorldBounds = q.Bounds()
	template.Triangles = 2
	template.Vertices = 4
	if template.Blend == BlendOpaque {
		template.Blend = BlendAlphaBlend
	}
	return template
}

type billboardEntry struct {
	quad  BillboardQuad
	depth float32
	index int
}

// BillboardRenderer draws billboards into a [*core.Drawing] back to front, reusing its scratch storage across frames. Its zero value is ready to use.
type BillboardRenderer struct {
	entries []billboardEntry
	screen  [maxClipVerts]screenPoint
}

// Draw builds every billboard facing cam, sorts them back to front by view depth (keeping slice order on ties), clips each against the near plane and fills it into d through viewProj and viewport. It returns the number of quads drawn.
//
// A textured quad is mapped with an affine transform, which is exact under an orthographic projection and a close approximation for quads that are small on screen under perspective; a textured quad that crosses the near plane is skipped.
func (r *BillboardRenderer) Draw(d *core.Drawing, items []Billboard, cam CameraBasis, viewProj math3d.Mat4, viewport core.Bounds) int {
	if d == nil || len(items) == 0 || viewport.Size.Width <= 0 || viewport.Size.Height <= 0 {
		return 0
	}
	_, _, back := cameraAxes(cam)
	r.entries = r.entries[:0]
	for i, b := range items {
		q, ok := b.Quad(cam)
		if !ok {
			continue
		}
		r.entries = append(r.entries, billboardEntry{quad: q, depth: -q.Center.Sub(cam.Pos).Dot(back), index: i})
	}
	slices.SortStableFunc(r.entries, func(a, b billboardEntry) int {
		return cmp.Compare(b.depth, a.depth)
	})
	drawn := 0
	d.Save()
	for _, e := range r.entries {
		if r.drawQuad(d, &items[e.index], &e.quad, viewProj, viewport) {
			drawn++
		}
	}
	d.Restore()
	return drawn
}

func (r *BillboardRenderer) drawQuad(d *core.Drawing, b *Billboard, q *BillboardQuad, viewProj math3d.Mat4, viewport core.Bounds) bool {
	color := b.Color
	if color == 0 {
		color = core.RGB(0xffffff)
	}
	if color&0xff == 0 {
		return false
	}
	pts, clipped := projectPolygon(q.Corners[:], viewProj, viewport, r.screen[:0])
	if len(pts) < 3 {
		return false
	}
	textured := !b.Texture.Empty()
	if textured && (clipped || len(pts) != 4) {
		return false
	}
	d.BeginPath()
	d.MoveTo(pts[0].x, pts[0].y)
	for _, p := range pts[1:] {
		d.LineTo(p.x, p.y)
	}
	d.ClosePath()
	if !textured {
		d.SetFillColor(color)
		d.Fill()
		return true
	}
	// Corners are BL, BR, TR, TL; the image's (0, 0), (1, 0) and (0, 1) map to TL, TR and BL.
	bl, tr, tl := pts[0], pts[2], pts[3]
	d.Save()
	d.Clip()
	if a := float32(color&0xff) / 255; a < 1 {
		d.SetAlpha(a)
	}
	d.Transform(tr.x-tl.x, tr.y-tl.y, bl.x-tl.x, bl.y-tl.y, tl.x, tl.y)
	d.DrawImageIn(b.Texture, 0, 0, 1, 1)
	d.Restore()
	return true
}

// maxClipVerts bounds a quad clipped against two planes (4 + 2 vertices), with room to spare.
const maxClipVerts = 8

type screenPoint struct{ x, y float32 }

// projectPolygon clips the convex world-space polygon poly (at most 4 vertices) against the near plane (clip z >= 0) and the positive-w plane, then maps it into viewport. It appends to out and reports whether any vertex was clipped away.
func projectPolygon(poly []math3d.Vec3, viewProj math3d.Mat4, viewport core.Bounds, out []screenPoint) ([]screenPoint, bool) {
	var a, b [maxClipVerts]math3d.Vec4
	const minW = 1e-5
	n := 0
	// Decide from the input vertices, not the output count: cutting two corners off a quad adds two intersections and leaves four vertices.
	clipped := false
	for _, p := range poly {
		c := viewProj.MulVec4(p.Vec4(1))
		if !(c.Z >= 0) || !(c.W >= minW) {
			clipped = true
		}
		a[n] = c
		n++
	}
	n = clipPlane(a[:n], b[:0], func(v math3d.Vec4) float32 { return v.Z })
	n = clipPlane(b[:n], a[:0], func(v math3d.Vec4) float32 { return v.W - minW })
	vx, vy := float32(viewport.Origin.X), float32(viewport.Origin.Y)
	vw, vh := float32(viewport.Size.Width), float32(viewport.Size.Height)
	for _, c := range a[:n] {
		if c.W < minW {
			clipped = true
			continue
		}
		x := vx + (c.X/c.W*0.5+0.5)*vw
		y := vy + (0.5-c.Y/c.W*0.5)*vh
		if isNaN32(x) || isNaN32(y) || isInf32(x) || isInf32(y) {
			return out[:0], true
		}
		out = append(out, screenPoint{x, y})
	}
	return out, clipped
}

// clipPlane runs one Sutherland–Hodgman pass, keeping the part of in where dist >= 0, and returns the vertex count written into out's backing array.
func clipPlane(in, out []math3d.Vec4, dist func(math3d.Vec4) float32) int {
	if len(in) == 0 {
		return 0
	}
	prev := in[len(in)-1]
	dPrev := dist(prev)
	for _, cur := range in {
		dCur := dist(cur)
		if (dPrev >= 0) != (dCur >= 0) && len(out) < maxClipVerts {
			out = append(out, prev.Lerp(cur, dPrev/(dPrev-dCur)))
		}
		if dCur >= 0 && len(out) < maxClipVerts {
			out = append(out, cur)
		}
		prev, dPrev = cur, dCur
	}
	return len(out)
}

func unitVec(v math3d.Vec3) (math3d.Vec3, bool) {
	l := v.LenSq()
	if !(l > degenerateLenSq) || isInf32(l) {
		return math3d.Vec3{}, false
	}
	return v.Scale(1 / float32(math.Sqrt(float64(l)))), true
}

// anyPerpendicular returns a unit vector perpendicular to the unit vector n.
func anyPerpendicular(n math3d.Vec3) math3d.Vec3 {
	ref := math3d.Vec3UnitX()
	if math.Abs(float64(n.X)) > 0.9 {
		ref = math3d.Vec3UnitY()
	}
	p, _ := unitVec(ref.Sub(n.Scale(ref.Dot(n))))
	return p
}

func isNaN32(v float32) bool { return v != v }

func isInf32(v float32) bool { return math.IsInf(float64(v), 0) }

func finiteOr0(v float32) float32 {
	if isNaN32(v) || isInf32(v) {
		return 0
	}
	return v
}

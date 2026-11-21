package scene

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// HitFunc reports whether p (in the layer's coordinate space: world space for SpaceWorld, view space for SpaceScreen) hits a target.
type HitFunc func(p core.Point) bool

// HitOptions configures custom hit testing and press/touch callbacks for a sprite in a Scene.
type HitOptions struct {
	// Hit overrides the sprite's default transformed untrimmed bounds when non-nil.
	Hit HitFunc
	// OnPress runs when Scene.DispatchPress hits this sprite as the topmost target.
	OnPress func(hit HitResult, ev core.PressEvent)
	// OnTouch runs when Scene.DispatchTouch hits this sprite as the topmost target.
	OnTouch func(hit HitResult, ev core.TouchEvent)
	// Ignore excludes the sprite from hit testing when true.
	Ignore bool
}

// HitResult describes one target hit by a view-space point.
type HitResult struct {
	// Layer is the layer containing the hit target.
	Layer *Layer
	// Space is the layer's coordinate space (SpaceWorld or SpaceScreen).
	Space Space
	// Point is the tested point in the layer's coordinate space: world coordinates for SpaceWorld, view coordinates for SpaceScreen.
	Point core.Point
	// ViewPoint is the original tested point in view coordinates.
	ViewPoint core.Point
	// Sprite is non-nil when the hit target is a sprite inside a Batch.
	Sprite *sprite.Sprite
	// Batch is the sprite's batch when Sprite is non-nil.
	Batch *sprite.Batch
	// Drawable is non-nil when the hit target is a custom Drawable.
	Drawable *Drawable
}

// SetSpriteHit binds custom hit-testing options and input callbacks to sp in s.
func (s *Scene) SetSpriteHit(sp *sprite.Sprite, opts HitOptions) {
	if s == nil || sp == nil {
		return
	}
	if s.spriteHits == nil {
		s.spriteHits = make(map[*sprite.Sprite]HitOptions)
	}
	s.spriteHits[sp] = opts
}

// ClearSpriteHit removes any custom hit-testing options previously bound to sp.
func (s *Scene) ClearSpriteHit(sp *sprite.Sprite) {
	if s == nil || sp == nil || s.spriteHits == nil {
		return
	}
	delete(s.spriteHits, sp)
}

// CircleHit returns a HitFunc that hits points within radius of center (inclusive).
func CircleHit(center core.Point, radius float32) HitFunc {
	if radius <= 0 {
		return func(core.Point) bool { return false }
	}
	r2 := float64(radius) * float64(radius)
	cx, cy := float64(center.X), float64(center.Y)
	return func(p core.Point) bool {
		dx := float64(p.X) - cx
		dy := float64(p.Y) - cy
		return dx*dx+dy*dy <= r2
	}
}

// RectHit returns a HitFunc that hits points inside bounds (inclusive of edges).
func RectHit(bounds core.Bounds) HitFunc {
	if bounds.Size.Width <= 0 || bounds.Size.Height <= 0 {
		return func(core.Point) bool { return false }
	}
	minX := float64(bounds.Origin.X)
	minY := float64(bounds.Origin.Y)
	maxX := minX + float64(bounds.Size.Width)
	maxY := minY + float64(bounds.Size.Height)
	return func(p core.Point) bool {
		x, y := float64(p.X), float64(p.Y)
		return x >= minX && x <= maxX && y >= minY && y <= maxY
	}
}

// PolygonHit returns a HitFunc that tests whether a point lies inside or on the boundary of the polygon formed by vertices.
func PolygonHit(vertices ...core.Point) HitFunc {
	pts := slices.Clone(vertices)
	if len(pts) < 3 {
		return func(core.Point) bool { return false }
	}
	return func(p core.Point) bool {
		px, py := float64(p.X), float64(p.Y)
		inside := false
		n := len(pts)
		j := n - 1
		for i := 0; i < n; i++ {
			xi, yi := float64(pts[i].X), float64(pts[i].Y)
			xj, yj := float64(pts[j].X), float64(pts[j].Y)
			if pointOnSegment(px, py, xi, yi, xj, yj) {
				return true
			}
			if (yi > py) != (yj > py) {
				xIntersect := (xj-xi)*(py-yi)/(yj-yi) + xi
				if px < xIntersect {
					inside = !inside
				}
			}
			j = i
		}
		return inside
	}
}

func pointOnSegment(px, py, ax, ay, bx, by float64) bool {
	const eps = 1e-5
	dx, dy := bx-ax, by-ay
	cross := (px-ax)*dy - (py-ay)*dx
	if math.Abs(cross) > eps*math.Hypot(dx, dy)+eps {
		return false
	}
	minX, maxX := min(ax, bx)-eps, max(ax, bx)+eps
	minY, maxY := min(ay, by)-eps, max(ay, by)+eps
	return px >= minX && px <= maxX && py >= minY && py <= maxY
}

// SpriteToLocal maps a layer-space point p into s's untrimmed frame coordinates (0..Width, 0..Height), inverting position, origin, scale, rotation, and flip. It returns false when s has zero scale or an empty frame.
func SpriteToLocal(s *sprite.Sprite, p core.Point) (core.Point, bool) {
	if s == nil {
		return core.Point{}, false
	}
	f := s.Frame
	if f.W <= 0 || f.H <= 0 || s.ScaleX == 0 || s.ScaleY == 0 {
		return core.Point{}, false
	}
	fw, fh := float64(f.Width), float64(f.Height)
	if fw <= 0 {
		fw = float64(f.W)
	}
	if fh <= 0 {
		fh = float64(f.H)
	}

	sx, sy := float64(s.ScaleX), float64(s.ScaleY)
	var tx, ty float64
	if s.FlipX {
		sx, tx = -sx, fw
	}
	if s.FlipY {
		sy, ty = -sy, fh
	}
	u0 := float64(s.ScaleX) * (tx - float64(s.OriginX))
	v0 := float64(s.ScaleY) * (ty - float64(s.OriginY))
	sin, cos := 0.0, 1.0
	if s.Rotation != 0 {
		sin, cos = math.Sincos(float64(s.Rotation))
	}
	a, b := cos*sx, sin*sx
	c, dd := -sin*sy, cos*sy
	e := float64(s.X) + cos*u0 - sin*v0
	ff := float64(s.Y) + sin*u0 + cos*v0

	det := a*dd - b*c
	if det == 0 {
		return core.Point{}, false
	}
	dx := float64(p.X) - e
	dy := float64(p.Y) - ff
	u := (dd*dx - c*dy) / det
	v := (-b*dx + a*dy) / det
	return core.Point{X: core.Points(u), Y: core.Points(v)}, true
}

// SpriteContains reports whether layer-space point p lies inside s's transformed untrimmed frame bounds (position, origin, scale, rotation, and flip). Invisible sprites (Alpha <= 0) never hit.
func SpriteContains(s *sprite.Sprite, p core.Point) bool {
	if s == nil || s.Alpha <= 0 {
		return false
	}
	local, ok := SpriteToLocal(s, p)
	if !ok {
		return false
	}
	fw, fh := float64(s.Frame.Width), float64(s.Frame.Height)
	if fw <= 0 {
		fw = float64(s.Frame.W)
	}
	if fh <= 0 {
		fh = float64(s.Frame.H)
	}
	u, v := float64(local.X), float64(local.Y)
	return u >= 0 && u <= fw && v >= 0 && v <= fh
}

// SpriteLocalHit returns a HitFunc that transforms each tested point into s's untrimmed local frame space before evaluating local.
func SpriteLocalHit(s *sprite.Sprite, local HitFunc) HitFunc {
	if s == nil || local == nil {
		return func(core.Point) bool { return false }
	}
	return func(p core.Point) bool {
		if s.Alpha <= 0 {
			return false
		}
		lp, ok := SpriteToLocal(s, p)
		if !ok {
			return false
		}
		return local(lp)
	}
}

// HitTest returns the topmost visible target hit by view-space point p, or (nil, false) if nothing was hit.
//
// Layers are tested in reverse draw order (highest Order first), and items and sprites within each layer are tested in reverse Z/slice order. SpaceWorld layers convert p via Camera.ViewToWorld; SpaceScreen layers test p directly in view coordinates.
func (s *Scene) HitTest(p core.Point) (*HitResult, bool) {
	var hit HitResult
	found := false
	s.walkHits(p, func(r HitResult) bool {
		hit = r
		found = true
		return false
	})
	if !found {
		return nil, false
	}
	return &hit, true
}

// HitTestAll returns every visible target hit by view-space point p, ordered from topmost to bottommost.
func (s *Scene) HitTestAll(p core.Point) []HitResult {
	var hits []HitResult
	s.walkHits(p, func(r HitResult) bool {
		hits = append(hits, r)
		return true
	})
	return hits
}

func (s *Scene) walkHits(p core.Point, yield func(HitResult) bool) {
	if s == nil || len(s.Layers) == 0 {
		return
	}
	layers := slices.Clone(s.Layers)
	slices.SortStableFunc(layers, func(a, b *Layer) int {
		var ao, bo int
		if a != nil {
			ao = a.Order
		}
		if b != nil {
			bo = b.Order
		}
		return cmp.Compare(ao, bo)
	})

	for li := len(layers) - 1; li >= 0; li-- {
		l := layers[li]
		if l == nil || !l.Visible || len(l.Items) == 0 {
			continue
		}
		entries := l.buildDrawEntries(nil)
		if len(entries) == 0 {
			continue
		}

		if l.Space == SpaceWorld && s.Camera != nil {
			pcam := l.parallaxCamera(s.Camera)
			pt := pcam.ViewToWorld(p)
			minCol, maxCol, minRow, maxRow, rx, ry := l.repeatRanges(pcam.VisibleBounds())
			if !rx && !ry {
				if !s.walkLayerEntries(l, entries, p, pt, yield) {
					return
				}
				continue
			}
			if rx {
				c := int(math.Floor(float64(pt.X) / float64(l.RepeatWidth)))
				if c < minCol || c > maxCol {
					minCol, maxCol = c, c
				}
			}
			if ry {
				r := int(math.Floor(float64(pt.Y) / float64(l.RepeatHeight)))
				if r < minRow || r > maxRow {
					minRow, maxRow = r, r
				}
			}
			for row := maxRow; row >= minRow; row-- {
				var ty float32
				if ry {
					ty = float32(row) * l.RepeatHeight
				}
				for col := maxCol; col >= minCol; col-- {
					var tx float32
					if rx {
						tx = float32(col) * l.RepeatWidth
					}
					localPt := core.Point{
						X: pt.X - core.Points(tx),
						Y: pt.Y - core.Points(ty),
					}
					if !s.walkLayerEntries(l, entries, p, localPt, yield) {
						return
					}
				}
			}
			continue
		}

		pt := core.Point{
			X: p.X - core.Points(l.OffsetX),
			Y: p.Y - core.Points(l.OffsetY),
		}
		if !s.walkLayerEntries(l, entries, p, pt, yield) {
			return
		}
	}
}

func (s *Scene) walkLayerEntries(l *Layer, entries []drawEntry, viewP, localP core.Point, yield func(HitResult) bool) bool {
	for ei := len(entries) - 1; ei >= 0; ei-- {
		e := entries[ei]
		switch e.kind {
		case entryDrawable:
			if e.drawable != nil && e.drawable.Visible && e.drawable.Hit != nil {
				if e.drawable.Hit(localP) {
					r := HitResult{
						Layer:     l,
						Space:     l.Space,
						Point:     localP,
						ViewPoint: viewP,
						Drawable:  e.drawable,
					}
					if !yield(r) {
						return false
					}
				}
			}
		case entrySprite:
			if !s.testSpriteHit(l, e.batch, e.sprite, viewP, localP, yield) {
				return false
			}
		case entryBatch:
			if e.batch != nil && len(e.batch.Sprites) > 0 {
				sprites := slices.Clone(e.batch.Sprites)
				slices.SortStableFunc(sprites, func(a, b *sprite.Sprite) int {
					var az, bz int
					if a != nil {
						az = a.Z
					}
					if b != nil {
						bz = b.Z
					}
					return cmp.Compare(az, bz)
				})
				for si := len(sprites) - 1; si >= 0; si-- {
					if !s.testSpriteHit(l, e.batch, sprites[si], viewP, localP, yield) {
						return false
					}
				}
			}
		}
	}
	return true
}

func (s *Scene) testSpriteHit(l *Layer, batch *sprite.Batch, sp *sprite.Sprite, viewP, localP core.Point, yield func(HitResult) bool) bool {
	if sp == nil {
		return true
	}
	var opts HitOptions
	if s.spriteHits != nil {
		opts = s.spriteHits[sp]
	}
	if opts.Ignore {
		return true
	}
	var matched bool
	if opts.Hit != nil {
		matched = opts.Hit(localP)
	} else {
		matched = SpriteContains(sp, localP)
	}
	if !matched {
		return true
	}
	r := HitResult{
		Layer:     l,
		Space:     l.Space,
		Point:     localP,
		ViewPoint: viewP,
		Sprite:    sp,
		Batch:     batch,
	}
	return yield(r)
}

// DispatchPress hit-tests ev.Position and invokes the topmost hit target's OnPress callback when set, returning whether a callback ran.
func (s *Scene) DispatchPress(ev core.PressEvent) bool {
	hit, ok := s.HitTest(ev.Position)
	if !ok {
		return false
	}
	if hit.Drawable != nil {
		if hit.Drawable.OnPress != nil {
			hit.Drawable.OnPress(*hit, ev)
			return true
		}
		return false
	}
	if hit.Sprite != nil && s.spriteHits != nil {
		if cb := s.spriteHits[hit.Sprite].OnPress; cb != nil {
			cb(*hit, ev)
			return true
		}
	}
	return false
}

// DispatchTouch hit-tests each changed finger of ev (or all fingers when none are marked Changed) and routes it to the topmost hit target's OnTouch callback, or to OnPress on TouchStart when OnTouch is nil. It returns whether any callback ran.
func (s *Scene) DispatchTouch(ev core.TouchEvent) bool {
	if s == nil || len(ev.Touches) == 0 {
		return false
	}
	hasChanged := false
	for _, t := range ev.Touches {
		if t.Changed {
			hasChanged = true
			break
		}
	}
	handled := false
	for _, t := range ev.Touches {
		if hasChanged && !t.Changed {
			continue
		}
		hit, ok := s.HitTest(t.Position)
		if !ok {
			continue
		}
		if hit.Drawable != nil {
			if hit.Drawable.OnTouch != nil {
				hit.Drawable.OnTouch(*hit, ev)
				handled = true
			} else if ev.Phase == core.TouchStart && hit.Drawable.OnPress != nil {
				hit.Drawable.OnPress(*hit, core.PressEvent{
					Position:     t.Position,
					PagePosition: t.PagePosition,
					Clicks:       1,
				})
				handled = true
			}
			continue
		}
		if hit.Sprite != nil && s.spriteHits != nil {
			opts := s.spriteHits[hit.Sprite]
			if opts.OnTouch != nil {
				opts.OnTouch(*hit, ev)
				handled = true
			} else if ev.Phase == core.TouchStart && opts.OnPress != nil {
				opts.OnPress(*hit, core.PressEvent{
					Position:     t.Position,
					PagePosition: t.PagePosition,
					Clicks:       1,
				})
				handled = true
			}
		}
	}
	return handled
}

package scene

import (
	"cmp"
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// DrawScreenLayers draws only the visible SpaceScreen layers of s in stable
// ascending Order into d, skipping SpaceWorld layers and leaving d's transform
// and paint state as it found it.
func (s *Scene) DrawScreenLayers(d *core.Drawing) {
	if s == nil || d == nil {
		return
	}
	layers := s.sortedLayers()
	defer clear(s.order)
	for _, l := range layers {
		if l != nil && l.Space == SpaceScreen {
			l.draw(d, nil)
		}
	}
}

// ProjectToOverlay projects the 3D world point (worldX, worldY, worldZ) through
// the column-major view-projection matrix viewProj (using [0, 1] clip depth, as
// math3d.Mat4 and scene3d.Camera.ViewProj produce) into 2D overlay coordinates
// spanning (0..viewWidth, 0..viewHeight) with (0, 0) at the top-left corner.
//
// visible is true when the projected point lies inside the view frustum (NDC X
// and Y in [-1, 1] and NDC Z in [0, 1]). Points in front of the camera but
// outside the frustum return their projected off-screen coordinates with
// visible == false; points behind the camera, degenerate matrices, non-finite
// inputs, or non-positive view dimensions return (0, 0, false).
func ProjectToOverlay(viewProj [16]float32, worldX, worldY, worldZ, viewWidth, viewHeight float32) (x, y float32, visible bool) {
	if !finite32(viewWidth) || !finite32(viewHeight) || viewWidth <= 0 || viewHeight <= 0 {
		return 0, 0, false
	}
	if !finite32(worldX) || !finite32(worldY) || !finite32(worldZ) {
		return 0, 0, false
	}
	clipX := viewProj[0]*worldX + viewProj[4]*worldY + viewProj[8]*worldZ + viewProj[12]
	clipY := viewProj[1]*worldX + viewProj[5]*worldY + viewProj[9]*worldZ + viewProj[13]
	clipZ := viewProj[2]*worldX + viewProj[6]*worldY + viewProj[10]*worldZ + viewProj[14]
	clipW := viewProj[3]*worldX + viewProj[7]*worldY + viewProj[11]*worldZ + viewProj[15]
	if !finite32(clipW) || clipW <= 1e-6 {
		return 0, 0, false
	}
	invW := 1 / clipW
	ndcX := clipX * invW
	ndcY := clipY * invW
	ndcZ := clipZ * invW
	sx := (ndcX + 1) * 0.5 * viewWidth
	sy := (1 - ndcY) * 0.5 * viewHeight
	if !finite32(sx) || !finite32(sy) || !finite32(ndcZ) {
		return 0, 0, false
	}
	inView := ndcX >= -1 && ndcX <= 1 && ndcY >= -1 && ndcY <= 1 && ndcZ >= 0 && ndcZ <= 1
	return sx, sy, inView
}

func finite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

// Overlay3D draws 2D screen-space layers, sprite batches, and custom draw
// callbacks in a transparent core.Canvas stacked over a 3D gpu.View.
//
// Items draw in deterministic order on every redraw:
//  1. Visible SpaceScreen layers from Scene (SpaceWorld layers in Scene are
//     skipped because the 3D view supplies the world) merged with visible
//     Layers, sorted in stable ascending Layer.Order.
//  2. Batches in slice order.
//  3. OnDraw (when non-nil), receiving d and the active canvas or virtual size.
//
// When Viewport is non-nil, drawing scales and clips through screen.Viewport.
// A zero Viewport.BarColor is treated as core.Transparent so letterbox and
// pillarbox margins reveal the 3D scene underneath; set Viewport.BarColor
// explicitly (for example core.RGB(0x000000)) when opaque bars are desired.
//
// By default the overlay canvas sets core.PointerEventsNone so pointer events
// pass through unobstructed to the 3D gpu.View underneath. Set Interactive to
// true (or provide OnPress) to receive pointer events directly on the overlay
// canvas instead.
type Overlay3D struct {
	// Scene is an optional Scene whose visible SpaceScreen layers draw over the
	// 3D view. SpaceWorld layers in Scene are skipped.
	Scene *Scene
	// Layers are additional 2D layers drawn over the 3D view alongside Scene's
	// SpaceScreen layers, ordered together by stable ascending Layer.Order.
	Layers []*Layer
	// Batches are standalone sprite batches drawn over the layers in slice order.
	Batches []*sprite.Batch
	// Viewport optionally scales and clips the 2D overlay to a virtual
	// resolution using screen.Viewport. A zero BarColor defaults to transparent
	// so the 3D scene shows through letterbox and pillarbox margins.
	Viewport *screen.Viewport
	// OnDraw optionally records additional 2D drawing operations after all
	// layers and batches, inside the active Viewport transform when set.
	OnDraw func(d *core.Drawing, size core.Size)
	// Hidden suppresses drawing and hit testing of the entire 2D overlay when true.
	Hidden bool
	// Interactive, when true (or when OnPress is non-nil), enables pointer
	// events (core.PointerEventsAuto) on the overlay canvas. When false and
	// OnPress is nil, the canvas uses core.PointerEventsNone so pointer input
	// passes through to the 3D gpu.View beneath it.
	Interactive bool
	// OnPress handles presses on the overlay canvas when Interactive is true or
	// OnPress is non-nil. When Interactive is true and OnPress is nil, presses
	// are routed to DispatchPress automatically.
	OnPress func(ev core.PressEvent)
	// TestID optionally sets core.Canvas.TestID on the overlay canvas.
	TestID string
	// Label optionally sets core.Canvas.Label on the overlay canvas.
	Label string

	cx       *core.Scope
	rev      *core.Signal[uint64]
	lastSize core.Size
	order    []*Layer
}

// NewOverlay3D returns a visible Overlay3D bound to cx so Invalidate triggers
// redraws of its overlay canvas. A nil cx defers binding until Canvas or View
// mounts the overlay in a scope.
func NewOverlay3D(cx *core.Scope) *Overlay3D {
	o := &Overlay3D{}
	o.bindScope(cx)
	return o
}

func (o *Overlay3D) bindScope(cx *core.Scope) {
	if o == nil || cx == nil || cx.Disposed() {
		return
	}
	if o.cx == cx && o.rev != nil {
		return
	}
	cx.Run(func() {
		o.cx = cx
		o.rev = core.NewSignal[uint64](cx, 0)
	})
}

// AddLayer appends l to o.Layers and returns l.
func (o *Overlay3D) AddLayer(l *Layer) *Layer {
	if o == nil || l == nil {
		return nil
	}
	o.Layers = append(o.Layers, l)
	return l
}

// AddBatch appends b to o.Batches.
func (o *Overlay3D) AddBatch(b *sprite.Batch) {
	if o == nil || b == nil {
		return
	}
	o.Batches = append(o.Batches, b)
}

// Invalidate marks the 2D overlay dirty so any mounted overlay canvas redraws
// on the next frame.
func (o *Overlay3D) Invalidate() {
	if o == nil || o.rev == nil || o.cx == nil || o.cx.Disposed() {
		return
	}
	o.rev.Set(o.rev.GetUntracked() + 1)
}

// SetHidden updates o.Hidden and invalidates the overlay when the state changes.
func (o *Overlay3D) SetHidden(hidden bool) {
	if o == nil || o.Hidden == hidden {
		return
	}
	o.Hidden = hidden
	o.Invalidate()
}

func (o *Overlay3D) mergedLayers(dst []*Layer) []*Layer {
	dst = dst[:0]
	if o.Scene != nil {
		for _, l := range o.Scene.Layers {
			if l != nil && l.Space == SpaceScreen {
				dst = append(dst, l)
			}
		}
	}
	for _, l := range o.Layers {
		if l != nil {
			dst = append(dst, l)
		}
	}
	slices.SortStableFunc(dst, func(a, b *Layer) int {
		return cmp.Compare(a.Order, b.Order)
	})
	return dst
}

// Update advances tilemaps and custom drawables in Scene's SpaceScreen layers
// and o.Layers by dt in ascending Layer.Order.
func (o *Overlay3D) Update(dt time.Duration) {
	if o == nil {
		return
	}
	layers := o.mergedLayers(o.order)
	o.order = layers
	defer clear(o.order)
	for _, l := range layers {
		l.update(dt)
	}
}

// Draw records the 2D overlay into d for a canvas of size, leaving d's
// transform and paint state as it found it.
//
// Calling Draw inside a core.Canvas subscribes the canvas to Invalidate calls
// automatically.
func (o *Overlay3D) Draw(d *core.Drawing, size core.Size) {
	if o == nil || d == nil {
		return
	}
	if o.rev != nil {
		_ = o.rev.Get()
	}
	if o.Hidden {
		return
	}
	if o.Viewport != nil {
		vp := *o.Viewport
		if vp.BarColor == 0 {
			vp.BarColor = core.Transparent
		}
		vp.Draw(d, size, func(dst *core.Drawing, vsize core.Size) {
			o.drawContent(dst, vsize)
		})
		return
	}
	o.drawContent(d, size)
}

func (o *Overlay3D) drawContent(d *core.Drawing, size core.Size) {
	layers := o.mergedLayers(o.order)
	o.order = layers
	defer clear(o.order)
	for _, l := range layers {
		l.draw(d, nil)
	}
	for _, b := range o.Batches {
		if b != nil {
			b.Draw(d)
		}
	}
	if o.OnDraw != nil {
		d.Save()
		o.OnDraw(d, size)
		d.Restore()
	}
}

// Project maps the 3D world point (worldX, worldY, worldZ) through the
// column-major view-projection matrix viewProj (as scene3d.Camera.ViewProj
// returns) into o's active 2D drawing coordinates for a 3D view of size window.
// When o.Viewport is set, projected view coordinates are converted into virtual
// coordinates via Viewport.ViewToGame, returning visible == false if the
// projected point falls outside the frustum or inside clipped letterbox/pillarbox
// margins.
func (o *Overlay3D) Project(viewProj [16]float32, worldX, worldY, worldZ float32, window core.Size) (x, y float32, visible bool) {
	sx, sy, inView := ProjectToOverlay(viewProj, worldX, worldY, worldZ, float32(window.Width), float32(window.Height))
	if o == nil || o.Viewport == nil {
		return sx, sy, inView
	}
	if !inView {
		return 0, 0, false
	}
	gp, ok := o.Viewport.ViewToGame(window, core.Point{X: core.Points(sx), Y: core.Points(sy)})
	if !ok {
		return 0, 0, false
	}
	return float32(gp.X), float32(gp.Y), true
}

func (o *Overlay3D) toOverlayPoint(window core.Size, p core.Point) (core.Point, bool) {
	if o.Viewport != nil {
		return o.Viewport.ViewToGame(window, p)
	}
	return p, true
}

// HitTest returns the topmost visible 2D overlay target hit by view-space point
// p inside a view of size window, testing standalone Batches first (in reverse
// slice and Z order) and then merged screen layers (in reverse Layer.Order).
func (o *Overlay3D) HitTest(window core.Size, p core.Point) (*HitResult, bool) {
	var hit HitResult
	found := false
	o.walkHits(window, p, func(r HitResult) bool {
		hit = r
		found = true
		return false
	})
	if !found {
		return nil, false
	}
	return &hit, true
}

// HitTestAll returns every visible 2D overlay target hit by view-space point p
// inside a view of size window, ordered from topmost to bottommost.
func (o *Overlay3D) HitTestAll(window core.Size, p core.Point) []HitResult {
	var hits []HitResult
	o.walkHits(window, p, func(r HitResult) bool {
		hits = append(hits, r)
		return true
	})
	return hits
}

func (o *Overlay3D) walkHits(window core.Size, p core.Point, yield func(HitResult) bool) {
	if o == nil || o.Hidden {
		return
	}
	overlayP, ok := o.toOverlayPoint(window, p)
	if !ok {
		return
	}

	var sc Scene
	if o.Scene != nil {
		sc.spriteHits = o.Scene.spriteHits
	}

	for bi := len(o.Batches) - 1; bi >= 0; bi-- {
		b := o.Batches[bi]
		if b == nil || len(b.Sprites) == 0 {
			continue
		}
		sprites := slices.Clone(b.Sprites)
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
			if !testOverlayBatchSprite(sc.spriteHits, b, sprites[si], p, overlayP, yield) {
				return
			}
		}
	}

	layers := o.mergedLayers(nil)
	for li := len(layers) - 1; li >= 0; li-- {
		l := layers[li]
		if l == nil || !l.Visible || len(l.Items) == 0 {
			continue
		}
		entries := l.buildDrawEntries(nil, nil, false)
		if len(entries) == 0 {
			continue
		}
		localP := core.Point{
			X: overlayP.X - core.Points(l.OffsetX),
			Y: overlayP.Y - core.Points(l.OffsetY),
		}
		if !sc.walkLayerEntries(l, entries, p, localP, yield) {
			return
		}
	}
}

func testOverlayBatchSprite(spriteHits map[*sprite.Sprite]HitOptions, batch *sprite.Batch, sp *sprite.Sprite, viewP, localP core.Point, yield func(HitResult) bool) bool {
	if sp == nil {
		return true
	}
	var opts HitOptions
	if spriteHits != nil {
		opts = spriteHits[sp]
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
	return yield(HitResult{
		Space:     SpaceScreen,
		Point:     localP,
		ViewPoint: viewP,
		Sprite:    sp,
		Batch:     batch,
	})
}

// DispatchPress hit-tests ev.Position in o for a view of size window and
// invokes the topmost hit target's OnPress callback when set, returning whether
// a callback ran.
func (o *Overlay3D) DispatchPress(window core.Size, ev core.PressEvent) bool {
	hit, ok := o.HitTest(window, ev.Position)
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
	if hit.Sprite != nil && o.Scene != nil && o.Scene.spriteHits != nil {
		if cb := o.Scene.spriteHits[hit.Sprite].OnPress; cb != nil {
			cb(*hit, ev)
			return true
		}
	}
	return false
}

// DispatchTouch hit-tests each changed finger of ev (or all fingers when none
// are marked Changed) against o for a view of size window, invoking OnTouch or
// falling back to OnPress on TouchStart when OnTouch is nil.
func (o *Overlay3D) DispatchTouch(window core.Size, ev core.TouchEvent) bool {
	if o == nil || o.Hidden || len(ev.Touches) == 0 {
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
		hit, ok := o.HitTest(window, t.Position)
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
		if hit.Sprite != nil && o.Scene != nil && o.Scene.spriteHits != nil {
			opts := o.Scene.spriteHits[hit.Sprite]
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

// Canvas returns a transparent, absolutely positioned core.Canvas sized to fill
// its parent view (such as a gpu.View). Mount it directly in gpu.ViewProps.Children
// or compose it automatically with Overlay3D.View.
func (o *Overlay3D) Canvas(cx *core.Scope) core.Canvas {
	if o == nil {
		return core.Canvas{
			Style: core.Style{
				Position:      core.Absolute,
				Top:           core.Pt(0),
				Left:          core.Pt(0),
				Right:         core.Pt(0),
				Bottom:        core.Pt(0),
				PointerEvents: core.PointerEventsNone,
			},
		}
	}
	o.bindScope(cx)

	style := core.Style{
		Position: core.Absolute,
		Top:      core.Pt(0),
		Left:     core.Pt(0),
		Right:    core.Pt(0),
		Bottom:   core.Pt(0),
	}
	var onPress func(core.PressEvent)
	if o.Interactive || o.OnPress != nil {
		style.PointerEvents = core.PointerEventsAuto
		onPress = func(ev core.PressEvent) {
			if o.OnPress != nil {
				o.OnPress(ev)
				return
			}
			o.DispatchPress(o.lastSize, ev)
		}
	} else {
		style.PointerEvents = core.PointerEventsNone
	}

	return core.Canvas{
		TestID: o.TestID,
		Label:  o.Label,
		Style:  style,
		OnLayout: func(b core.Bounds) {
			o.lastSize = b.Size
		},
		OnPress: onPress,
		Draw: func(d *core.Drawing, size core.Size) {
			o.lastSize = size
			o.Draw(d, size)
		},
	}
}

// View returns a gpu.View configured by props with o's transparent 2D overlay
// canvas mounted directly above the 3D GPU surface and beneath any native views
// listed in props.Children.
func (o *Overlay3D) View(cx *core.Scope, props gpu.ViewProps) core.Node {
	if o == nil {
		return gpu.View(cx, props)
	}
	merged := props
	children := make([]core.Node, 0, len(props.Children)+1)
	children = append(children, o.Canvas(cx))
	children = append(children, props.Children...)
	merged.Children = children
	return gpu.View(cx, merged)
}

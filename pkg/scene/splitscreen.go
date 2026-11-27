package scene

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// SplitViewport defines one clipped viewport region within a SplitScreen layout.
type SplitViewport struct {
	// Camera is the world camera used to draw and hit-test the scene in this viewport.
	Camera *tilemap.Camera
	// Region is the viewport rectangle in parent view coordinates.
	Region core.Bounds
	// DrawHUD optionally draws per-viewport HUD elements in viewport-local coordinates
	// (0..Region.Size.Width, 0..Region.Size.Height) after the scene draws.
	DrawHUD func(d *core.Drawing, size core.Size)
}

// SplitScreen renders and hit-tests a Scene across multiple clipped viewports with independent cameras.
type SplitScreen struct {
	// Viewports are the active screen regions drawn in slice order.
	Viewports []SplitViewport
	// DividerWidth is the thickness in points of divider lines drawn along shared edges between viewports when positive.
	DividerWidth float32
	// DividerColor is the fill color of divider lines when DividerWidth > 0; a zero color defaults to opaque black.
	DividerColor core.Color
}

// NewSplitScreen returns a SplitScreen configured with the given viewports.
func NewSplitScreen(views ...SplitViewport) *SplitScreen {
	ss := &SplitScreen{
		Viewports: slices.Clone(views),
	}
	ss.syncCameraSizes()
	return ss
}

// HorizontalSplit returns a two-viewport top/bottom SplitScreen covering size.
func HorizontalSplit(size core.Size, topCam, bottomCam *tilemap.Camera) *SplitScreen {
	halfH := size.Height / 2
	return NewSplitScreen(
		SplitViewport{
			Camera: topCam,
			Region: core.Bounds{
				Origin: core.Point{X: 0, Y: 0},
				Size:   core.Size{Width: size.Width, Height: halfH},
			},
		},
		SplitViewport{
			Camera: bottomCam,
			Region: core.Bounds{
				Origin: core.Point{X: 0, Y: halfH},
				Size:   core.Size{Width: size.Width, Height: size.Height - halfH},
			},
		},
	)
}

// VerticalSplit returns a two-viewport left/right SplitScreen covering size.
func VerticalSplit(size core.Size, leftCam, rightCam *tilemap.Camera) *SplitScreen {
	halfW := size.Width / 2
	return NewSplitScreen(
		SplitViewport{
			Camera: leftCam,
			Region: core.Bounds{
				Origin: core.Point{X: 0, Y: 0},
				Size:   core.Size{Width: halfW, Height: size.Height},
			},
		},
		SplitViewport{
			Camera: rightCam,
			Region: core.Bounds{
				Origin: core.Point{X: halfW, Y: 0},
				Size:   core.Size{Width: size.Width - halfW, Height: size.Height},
			},
		},
	)
}

// QuadSplit returns a four-viewport 2x2 SplitScreen covering size in row-major order:
// top-left (0), top-right (1), bottom-left (2), bottom-right (3).
func QuadSplit(size core.Size, cams [4]*tilemap.Camera) *SplitScreen {
	halfW := size.Width / 2
	halfH := size.Height / 2
	remW := size.Width - halfW
	remH := size.Height - halfH
	return NewSplitScreen(
		SplitViewport{
			Camera: cams[0],
			Region: core.Bounds{
				Origin: core.Point{X: 0, Y: 0},
				Size:   core.Size{Width: halfW, Height: halfH},
			},
		},
		SplitViewport{
			Camera: cams[1],
			Region: core.Bounds{
				Origin: core.Point{X: halfW, Y: 0},
				Size:   core.Size{Width: remW, Height: halfH},
			},
		},
		SplitViewport{
			Camera: cams[2],
			Region: core.Bounds{
				Origin: core.Point{X: 0, Y: halfH},
				Size:   core.Size{Width: halfW, Height: remH},
			},
		},
		SplitViewport{
			Camera: cams[3],
			Region: core.Bounds{
				Origin: core.Point{X: halfW, Y: halfH},
				Size:   core.Size{Width: remW, Height: remH},
			},
		},
	)
}

func (ss *SplitScreen) syncCameraSizes() {
	for i := range ss.Viewports {
		v := ss.Viewports[i]
		if v.Camera != nil {
			v.Camera.Width = float32(v.Region.Size.Width)
			v.Camera.Height = float32(v.Region.Size.Height)
		}
	}
}

// Draw renders s into each viewport clipped strictly to its Region under its Camera, draws any per-viewport HUD, draws divider lines when DividerWidth > 0, and restores both d and s.Camera cleanly.
func (ss *SplitScreen) Draw(d *core.Drawing, s *Scene) {
	if ss == nil || d == nil {
		return
	}
	var savedCam *tilemap.Camera
	if s != nil {
		savedCam = s.Camera
		defer func() {
			s.Camera = savedCam
		}()
	}

	for i := range ss.Viewports {
		v := ss.Viewports[i]
		w := float32(v.Region.Size.Width)
		h := float32(v.Region.Size.Height)
		if w <= 0 || h <= 0 {
			continue
		}
		ox := float32(v.Region.Origin.X)
		oy := float32(v.Region.Origin.Y)

		d.Save()
		d.BeginPath().Rect(ox, oy, w, h).Clip().BeginPath()
		if ox != 0 || oy != 0 {
			d.Translate(ox, oy)
		}
		if v.Camera != nil {
			v.Camera.Width = w
			v.Camera.Height = h
		}
		if s != nil {
			s.Camera = v.Camera
			s.Draw(d)
		}
		if v.DrawHUD != nil {
			d.Save()
			v.DrawHUD(d, v.Region.Size)
			d.Restore()
		}
		d.Restore()
	}

	ss.drawDividers(d)
}

type dividerSeg struct {
	pos   float32
	start float32
	end   float32
}

func mergeDividerSegs(segs []dividerSeg) []dividerSeg {
	if len(segs) <= 1 {
		return segs
	}
	const eps = 1e-3
	slices.SortFunc(segs, func(a, b dividerSeg) int {
		if math.Abs(float64(a.pos-b.pos)) > eps {
			return cmp.Compare(a.pos, b.pos)
		}
		if c := cmp.Compare(a.start, b.start); c != 0 {
			return c
		}
		return cmp.Compare(a.end, b.end)
	})
	out := segs[:1]
	for _, s := range segs[1:] {
		last := &out[len(out)-1]
		if math.Abs(float64(s.pos-last.pos)) <= eps && s.start <= last.end+eps {
			if s.end > last.end {
				last.end = s.end
			}
		} else {
			out = append(out, s)
		}
	}
	return out
}

func (ss *SplitScreen) drawDividers(d *core.Drawing) {
	if ss.DividerWidth <= 0 || len(ss.Viewports) < 2 {
		return
	}
	const eps = 1e-3
	var vSegs, hSegs []dividerSeg

	for i := range ss.Viewports {
		a := ss.Viewports[i].Region
		if a.Size.Width <= 0 || a.Size.Height <= 0 {
			continue
		}
		ax0, ay0 := float32(a.Origin.X), float32(a.Origin.Y)
		ax1, ay1 := ax0+float32(a.Size.Width), ay0+float32(a.Size.Height)
		for j := range ss.Viewports {
			if i == j {
				continue
			}
			b := ss.Viewports[j].Region
			if b.Size.Width <= 0 || b.Size.Height <= 0 {
				continue
			}
			bx0, by0 := float32(b.Origin.X), float32(b.Origin.Y)
			bx1, by1 := bx0+float32(b.Size.Width), by0+float32(b.Size.Height)

			// Vertical seam: a's right edge touches b's left edge with overlapping Y span.
			if math.Abs(float64(ax1-bx0)) <= eps {
				y0 := max(ay0, by0)
				y1 := min(ay1, by1)
				if y1 > y0+eps {
					vSegs = append(vSegs, dividerSeg{pos: (ax1 + bx0) / 2, start: y0, end: y1})
				}
			}
			// Horizontal seam: a's bottom edge touches b's top edge with overlapping X span.
			if math.Abs(float64(ay1-by0)) <= eps {
				x0 := max(ax0, bx0)
				x1 := min(ax1, bx1)
				if x1 > x0+eps {
					hSegs = append(hSegs, dividerSeg{pos: (ay1 + by0) / 2, start: x0, end: x1})
				}
			}
		}
	}

	if len(vSegs) == 0 && len(hSegs) == 0 {
		return
	}
	vSegs = mergeDividerSegs(vSegs)
	hSegs = mergeDividerSegs(hSegs)

	color := ss.DividerColor
	if color == 0 {
		color = core.RGB(0x000000)
	}
	w := ss.DividerWidth
	half := w / 2

	d.Save()
	d.SetFillColor(color)
	for _, seg := range vSegs {
		d.FillRect(seg.pos-half, seg.start, w, seg.end-seg.start)
	}
	for _, seg := range hSegs {
		d.FillRect(seg.start, seg.pos-half, seg.end-seg.start, w)
	}
	d.Restore()
}

// ViewportAt returns the index of the topmost viewport containing parent view point p, or -1 if p lies outside all viewports.
func (ss *SplitScreen) ViewportAt(p core.Point) int {
	if ss == nil || len(ss.Viewports) == 0 {
		return -1
	}
	for i := len(ss.Viewports) - 1; i >= 0; i-- {
		r := ss.Viewports[i].Region
		if r.Size.Width <= 0 || r.Size.Height <= 0 {
			continue
		}
		if p.X >= r.Origin.X && p.X < r.Origin.X+r.Size.Width &&
			p.Y >= r.Origin.Y && p.Y < r.Origin.Y+r.Size.Height {
			return i
		}
	}
	for i := len(ss.Viewports) - 1; i >= 0; i-- {
		r := ss.Viewports[i].Region
		if r.Size.Width <= 0 || r.Size.Height <= 0 {
			continue
		}
		if p.X >= r.Origin.X && p.X <= r.Origin.X+r.Size.Width &&
			p.Y >= r.Origin.Y && p.Y <= r.Origin.Y+r.Size.Height {
			return i
		}
	}
	return -1
}

// HitTest routes parent view-space point p to the viewport containing p, hit-tests s under that viewport's Camera, and restores s.Camera afterwards.
//
// When p lies outside all viewports, it returns (-1, nil, false). When p falls inside viewport i but hits nothing, it returns (i, nil, false).
func (ss *SplitScreen) HitTest(s *Scene, p core.Point) (viewportIndex int, hit *HitResult, ok bool) {
	if ss == nil || s == nil {
		return -1, nil, false
	}
	idx := ss.ViewportAt(p)
	if idx < 0 {
		return -1, nil, false
	}
	v := ss.Viewports[idx]
	localP := core.Point{
		X: p.X - v.Region.Origin.X,
		Y: p.Y - v.Region.Origin.Y,
	}

	savedCam := s.Camera
	defer func() {
		s.Camera = savedCam
	}()
	if v.Camera != nil {
		v.Camera.Width = float32(v.Region.Size.Width)
		v.Camera.Height = float32(v.Region.Size.Height)
	}
	s.Camera = v.Camera

	hit, ok = s.HitTest(localP)
	if !ok {
		return idx, nil, false
	}
	hit.ViewPoint = p
	return idx, hit, true
}

// HitTestAll routes parent view-space point p to the viewport containing p and returns all hits in topmost-to-bottommost order.
func (ss *SplitScreen) HitTestAll(s *Scene, p core.Point) (viewportIndex int, hits []HitResult) {
	if ss == nil || s == nil {
		return -1, nil
	}
	idx := ss.ViewportAt(p)
	if idx < 0 {
		return -1, nil
	}
	v := ss.Viewports[idx]
	localP := core.Point{
		X: p.X - v.Region.Origin.X,
		Y: p.Y - v.Region.Origin.Y,
	}

	savedCam := s.Camera
	defer func() {
		s.Camera = savedCam
	}()
	if v.Camera != nil {
		v.Camera.Width = float32(v.Region.Size.Width)
		v.Camera.Height = float32(v.Region.Size.Height)
	}
	s.Camera = v.Camera

	hits = s.HitTestAll(localP)
	for i := range hits {
		hits[i].ViewPoint = p
	}
	return idx, hits
}

// DispatchPress hit-tests ev.Position in the matching viewport and invokes the topmost target's OnPress callback when set.
func (ss *SplitScreen) DispatchPress(s *Scene, ev core.PressEvent) (viewportIndex int, handled bool) {
	idx, hit, ok := ss.HitTest(s, ev.Position)
	if !ok {
		return idx, false
	}
	if hit.Drawable != nil {
		if hit.Drawable.OnPress != nil {
			hit.Drawable.OnPress(*hit, ev)
			return idx, true
		}
		return idx, false
	}
	if hit.Sprite != nil && s.spriteHits != nil {
		if cb := s.spriteHits[hit.Sprite].OnPress; cb != nil {
			cb(*hit, ev)
			return idx, true
		}
	}
	return idx, false
}

// DispatchTouch hit-tests each changed finger of ev (or all fingers when none are marked Changed) against its containing viewport and routes it to OnTouch (or OnPress on TouchStart when OnTouch is nil).
func (ss *SplitScreen) DispatchTouch(s *Scene, ev core.TouchEvent) bool {
	if ss == nil || s == nil || len(ev.Touches) == 0 {
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
		_, hit, ok := ss.HitTest(s, t.Position)
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

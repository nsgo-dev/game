//go:build !release

package debug

import (
	"github.com/nsgo-dev/game/pkg/framedata"
	"github.com/nsgo-dev/nsgo/core"
)

// DrawFighterBoxes strokes the boxes f's current move has active on its current frame under d's active transform: hurtboxes in hurt, then hitboxes in hit on top, mirrored as the fighter faces.
func (s *ShapeOverlay) DrawFighterBoxes(d *core.Drawing, f *framedata.Fighter, hurt, hit core.Color) {
	if d == nil || f == nil {
		return
	}
	var buf [16]framedata.WorldBox
	boxes := f.Boxes(buf[:0])
	for _, kind := range [...]framedata.Kind{framedata.KindHurt, framedata.KindHit} {
		col := hurt
		if kind == framedata.KindHit {
			col = hit
		}
		for _, b := range boxes {
			if b.Kind == kind {
				s.DrawBox(d, b.Rect.Bounds(), col)
			}
		}
	}
}

// DrawFighterBoxes draws f's active hurtboxes and hitboxes as wireframes under d's active transform while the overlay is visible.
func (o *Overlay) DrawFighterBoxes(d *core.Drawing, f *framedata.Fighter, hurt, hit core.Color) {
	if o == nil || !o.Visible() {
		return
	}
	o.shapes.DrawFighterBoxes(d, f, hurt, hit)
}

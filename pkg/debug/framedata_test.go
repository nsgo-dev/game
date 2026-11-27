//go:build !release

package debug_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/debug"
	"github.com/nsgo-dev/game/pkg/framedata"
	"github.com/nsgo-dev/nsgo/core"
)

const punchYAML = `version: 1
moves:
  - name: idle
    length: 1
    loop: true
    boxes:
      - {kind: hurt, start: 1, end: 1, x: -10, "y": -40, width: 20, height: 40}
  - name: punch
    length: 6
    boxes:
      - {kind: hit, start: 3, end: 4, x: 8, "y": -30, width: 16, height: 6, damage: 10, hitstun: 8}
      - {kind: hurt, start: 1, end: 6, x: -10, "y": -40, width: 20, height: 40}
`

func TestDrawFighterBoxesFollowsFrameData(t *testing.T) {
	doc, err := framedata.Parse([]byte(punchYAML))
	if err != nil {
		t.Fatal(err)
	}
	f := &framedata.Fighter{Moves: doc, Neutral: "idle", X: 100, Y: 50, FacingLeft: true}
	w := &framedata.World{Fighters: []*framedata.Fighter{f}}
	w.Step()
	if !f.Start("punch") {
		t.Fatal("Start(punch) refused")
	}

	const zoom = 2
	dbg := debug.New(debug.Options{Visible: true, LineWidth: 1})
	rects := func() []core.DrawOp {
		d := &core.Drawing{}
		d.Scale(zoom, zoom)
		dbg.DrawFighterBoxes(d, f, core.RGB(0x38BDF8), core.RGB(0xEF4444))
		var out []core.DrawOp
		for _, op := range d.Ops {
			switch op.Kind {
			case core.DrawRect:
				out = append(out, op)
			case core.DrawLineWidth:
				if math.Abs(float64(op.W-0.5)) > 1e-4 {
					t.Fatalf("line width %v at zoom %v, want 0.5", op.W, zoom)
				}
			}
		}
		return out
	}

	// Frame 1 of the punch: only the hurtbox, mirrored about x=100.
	w.Step()
	got := rects()
	if len(got) != 1 {
		t.Fatalf("frame 1 drew %d boxes, want 1", len(got))
	}
	assertRect(t, "hurtbox", got[0], 90, 10, 20, 40)

	// Frame 3: the hitbox is active and drawn after the hurtbox even though the document lists it first.
	w.Step()
	w.Step()
	got = rects()
	if len(got) != 2 {
		t.Fatalf("frame 3 drew %d boxes, want 2", len(got))
	}
	assertRect(t, "hurtbox", got[0], 90, 10, 20, 40)
	assertRect(t, "hitbox", got[1], 76, 20, 16, 6)

	dbg.SetVisible(false)
	if got := rects(); len(got) != 0 {
		t.Fatalf("hidden overlay drew %d boxes", len(got))
	}
}

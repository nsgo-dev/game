//go:build !release

package physics_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func recordCanvas(t *testing.T, draw func(d *core.Drawing)) []core.DrawOp {
	t.Helper()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "canvas",
			Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
			Draw: func(d *core.Drawing, _ core.Size) {
				draw(d)
			},
		}
	})
	return app.Find("canvas").Ops
}

func assertBalancedSaveRestore(t *testing.T, ops []core.DrawOp) {
	t.Helper()
	depth := 0
	for i, op := range ops {
		switch op.Kind {
		case core.DrawSave:
			depth++
		case core.DrawRestore:
			depth--
			if depth < 0 {
				t.Fatalf("op %d restored below initial save depth", i)
			}
		}
	}
	if depth != 0 {
		t.Fatalf("unbalanced save/restore depth: ended at %d", depth)
	}
}

func TestDebugDrawShapes(t *testing.T) {
	style := physics.DebugStyle{
		BoxColor:    core.RGB(0x11aa22),
		CircleColor: core.RGB(0x2288ee),
		LineWidth:   2,
	}

	t.Run("box", func(t *testing.T) {
		box := physics.Box{X: 12, Y: 24, W: 32, H: 16}
		ops := recordCanvas(t, func(d *core.Drawing) {
			physics.DrawShape(d, physics.BoxShape(box), style)
		})
		assertBalancedSaveRestore(t, ops)

		wantKinds := []core.DrawKind{
			core.DrawSave,
			core.DrawStrokeColor,
			core.DrawLineWidth,
			core.DrawBeginPath,
			core.DrawRect,
			core.DrawStroke,
			core.DrawRestore,
		}
		if len(ops) != len(wantKinds) {
			t.Fatalf("got %d ops (%+v), want %d", len(ops), ops, len(wantKinds))
		}
		for i, wk := range wantKinds {
			if ops[i].Kind != wk {
				t.Fatalf("op %d kind = %v, want %v", i, ops[i].Kind, wk)
			}
		}
		if ops[1].Color != style.BoxColor {
			t.Fatalf("stroke color = %#x, want %#x", ops[1].Color, style.BoxColor)
		}
		if !near(ops[2].W, 2, 1e-4) {
			t.Fatalf("line width = %v, want 2", ops[2].W)
		}
		r := ops[4]
		if !near(r.X, 12, 1e-4) || !near(r.Y, 24, 1e-4) || !near(r.W, 32, 1e-4) || !near(r.H, 16, 1e-4) {
			t.Fatalf("rect = (%v,%v,%v,%v), want (12,24,32,16)", r.X, r.Y, r.W, r.H)
		}
	})

	t.Run("circle", func(t *testing.T) {
		circ := physics.Circle{X: 50, Y: 60, R: 10}
		ops := recordCanvas(t, func(d *core.Drawing) {
			physics.DrawShape(d, physics.CircleShape(circ), style)
		})
		assertBalancedSaveRestore(t, ops)

		wantKinds := []core.DrawKind{
			core.DrawSave,
			core.DrawStrokeColor,
			core.DrawLineWidth,
			core.DrawBeginPath,
			core.DrawArc,
			core.DrawMoveTo,
			core.DrawLineTo,
			core.DrawStroke,
			core.DrawRestore,
		}
		if len(ops) != len(wantKinds) {
			t.Fatalf("got %d ops (%+v), want %d", len(ops), ops, len(wantKinds))
		}
		for i, wk := range wantKinds {
			if ops[i].Kind != wk {
				t.Fatalf("op %d kind = %v, want %v", i, ops[i].Kind, wk)
			}
		}
		if ops[1].Color != style.CircleColor {
			t.Fatalf("stroke color = %#x, want %#x", ops[1].Color, style.CircleColor)
		}
		arc := ops[4]
		if !near(arc.X, 50, 1e-4) || !near(arc.Y, 60, 1e-4) || !near(arc.R, 10, 1e-4) || !near(arc.A0, 0, 1e-4) || !near(arc.A1, float32(2*math.Pi), 1e-4) {
			t.Fatalf("arc = %+v, want center (50,60) r=10 full circle", arc)
		}
		// Orientation tick from center to (cx+r, cy).
		if !near(ops[5].X, 50, 1e-4) || !near(ops[5].Y, 60, 1e-4) || !near(ops[6].X, 60, 1e-4) || !near(ops[6].Y, 60, 1e-4) {
			t.Fatalf("orientation tick = (%v,%v)->(%v,%v), want (50,60)->(60,60)", ops[5].X, ops[5].Y, ops[6].X, ops[6].Y)
		}
	})

	t.Run("empty and nil emit nothing", func(t *testing.T) {
		physics.DrawShape(nil, physics.BoxShape(physics.Box{W: 10, H: 10}), style)
		ops := recordCanvas(t, func(d *core.Drawing) {
			physics.DrawShape(d, physics.Shape{}, style)
			physics.DrawShape(d, physics.BoxShape(physics.Box{X: 1, Y: 2, W: 0, H: 10}), style)
			physics.DrawShape(d, physics.CircleShape(physics.Circle{X: 1, Y: 2, R: -5}), style)
		})
		if len(ops) != 0 {
			t.Fatalf("expected 0 ops for empty shapes, got %d: %+v", len(ops), ops)
		}
	})
}

func TestDebugDrawBodyAndVelocity(t *testing.T) {
	body := physics.NewBoxBody(10, 20, 20, 10)
	body.VX, body.VY = 100, -50

	opsNoVel := recordCanvas(t, func(d *core.Drawing) {
		physics.DrawBody(d, body, physics.DefaultDebugStyle())
	})
	assertBalancedSaveRestore(t, opsNoVel)
	for _, op := range opsNoVel {
		if op.Kind == core.DrawLineTo {
			t.Fatalf("unexpected velocity arrow op when VelocityScale == 0: %+v", op)
		}
	}

	style := physics.DefaultDebugStyle()
	style.VelocityScale = 0.2
	style.VelocityColor = core.RGB(0x3366ff)
	opsVel := recordCanvas(t, func(d *core.Drawing) {
		physics.DrawBody(d, body, style)
	})
	assertBalancedSaveRestore(t, opsVel)

	// Center of box (10,20,20,10) is (20,25); end is (20 + 100*0.2, 25 - 50*0.2) = (40, 15).
	foundShaft := false
	foundColor := false
	for i := 0; i+1 < len(opsVel); i++ {
		if opsVel[i].Kind == core.DrawStrokeColor && opsVel[i].Color == style.VelocityColor {
			foundColor = true
		}
		if opsVel[i].Kind == core.DrawMoveTo && opsVel[i+1].Kind == core.DrawLineTo {
			if near(opsVel[i].X, 20, 1e-4) && near(opsVel[i].Y, 25, 1e-4) &&
				near(opsVel[i+1].X, 40, 1e-4) && near(opsVel[i+1].Y, 15, 1e-4) {
				foundShaft = true
			}
		}
	}
	if !foundColor || !foundShaft {
		t.Fatalf("expected velocity arrow (20,25)->(40,15) in %#x; ops: %+v", style.VelocityColor, opsVel)
	}
}

func TestDebugDrawContactAndNormal(t *testing.T) {
	a := physics.Box{X: 10, Y: 5, W: 16, H: 10}
	b := physics.Box{X: 10, Y: 10, W: 16, H: 16}
	contact := physics.TestBoxBox(a, b)
	if !contact.Hit {
		t.Fatal("expected boxes to overlap")
	}

	style := physics.DebugStyle{
		ContactColor:  core.RGB(0xff0000),
		NormalColor:   core.RGB(0xffff00),
		LineWidth:     1.5,
		NormalLength:  10,
		ContactRadius: 3,
	}

	ops := recordCanvas(t, func(d *core.Drawing) {
		physics.DrawContact(d, physics.Contact{}, style) // no-op
		physics.DrawContact(d, contact, style)
	})
	assertBalancedSaveRestore(t, ops)

	var dotOp core.DrawOp
	foundDot := false
	foundNormal := false
	for i, op := range ops {
		if op.Kind == core.DrawArc {
			dotOp = op
			foundDot = true
		}
		if op.Kind == core.DrawMoveTo && i+1 < len(ops) && ops[i+1].Kind == core.DrawLineTo {
			wantEndX := contact.PointX + contact.NormalX*style.NormalLength
			wantEndY := contact.PointY + contact.NormalY*style.NormalLength
			if near(op.X, contact.PointX, 1e-4) && near(op.Y, contact.PointY, 1e-4) &&
				near(ops[i+1].X, wantEndX, 1e-4) && near(ops[i+1].Y, wantEndY, 1e-4) {
				foundNormal = true
			}
		}
	}
	if !foundDot {
		t.Fatalf("missing contact point marker arc in ops: %+v", ops)
	}
	if !near(dotOp.X, contact.PointX, 1e-4) || !near(dotOp.Y, contact.PointY, 1e-4) || !near(dotOp.R, 3, 1e-4) {
		t.Fatalf("contact arc = (%v,%v,r=%v), want (%v,%v,r=3)", dotOp.X, dotOp.Y, dotOp.R, contact.PointX, contact.PointY)
	}
	if !foundNormal {
		t.Fatalf("missing normal arrow from (%v,%v) along (%v,%v)*%v in ops: %+v",
			contact.PointX, contact.PointY, contact.NormalX, contact.NormalY, style.NormalLength, ops)
	}
}

func TestDebugDrawTileContacts(t *testing.T) {
	grid := physics.NewGrid(4, 4, 16, 16)
	grid.FillRect(0, 3, 4, 1, physics.TileSolid) // floor row 3 (y=48)
	grid.Set(3, 2, physics.TileSolid)            // right wall col 3, row 2 (x=48)

	body := physics.NewBoxBody(32, 32, 16, 16)
	body.VX = 320
	body.VY = 320
	res := physics.MoveAndSlide(body, grid, 100*time.Millisecond)
	if len(res.Contacts) < 2 {
		t.Fatalf("expected wall + floor contacts, got %+v", res.Contacts)
	}

	style := physics.DefaultDebugStyle()
	ops := recordCanvas(t, func(d *core.Drawing) {
		physics.DrawTileContacts(d, grid, res.Contacts, style)
	})
	assertBalancedSaveRestore(t, ops)

	// Collect all contact dots and shafts.
	var dots []core.DrawOp
	for _, op := range ops {
		if op.Kind == core.DrawArc {
			dots = append(dots, op)
		}
	}
	if len(dots) != len(res.Contacts) {
		t.Fatalf("got %d contact dots, want %d", len(dots), len(res.Contacts))
	}

	// Verify wall contact (3,2, NormalX=-1) sits on left edge (48, 40) and floor contact (2,3, NormalY=-1) sits on top edge (40, 48).
	for i, tc := range res.Contacts {
		wantX := (float32(tc.Col)+0.5)*16 + tc.NormalX*8
		wantY := (float32(tc.Row)+0.5)*16 + tc.NormalY*8
		if !near(dots[i].X, wantX, 1e-4) || !near(dots[i].Y, wantY, 1e-4) {
			t.Fatalf("tile contact %d (%+v): dot at (%v,%v), want (%v,%v)", i, tc, dots[i].X, dots[i].Y, wantX, wantY)
		}
	}
}

func TestDebugDrawTileGridCullingAndKinds(t *testing.T) {
	grid := physics.NewGrid(6, 4, 16, 16)
	grid.Set(0, 0, physics.TileSolid)  // outside visible
	grid.Set(1, 1, physics.TileSolid)  // inside visible: rect (16,16,16,16)
	grid.Set(2, 1, physics.TileOneWay) // inside visible: top edge (32,16)->(48,16)
	grid.Set(5, 3, physics.TileSolid)  // outside visible

	visible := core.Bounds{
		Origin: core.Point{X: 16, Y: 16},
		Size:   core.Size{Width: 32, Height: 16},
	}
	style := physics.DebugStyle{
		SolidTileColor:  core.RGB(0xff8800),
		OneWayTileColor: core.RGB(0xaa44ff),
		LineWidth:       1,
	}

	ops := recordCanvas(t, func(d *core.Drawing) {
		physics.DrawTileGrid(d, grid, visible, style)
	})
	assertBalancedSaveRestore(t, ops)

	var rects []core.DrawOp
	var strokeColors []core.Color
	hasOneWayTopEdge := false
	for i, op := range ops {
		switch op.Kind {
		case core.DrawStrokeColor:
			strokeColors = append(strokeColors, op.Color)
		case core.DrawRect:
			rects = append(rects, op)
		case core.DrawLineTo:
			// Top edge segment (32, 16) -> (48, 16) in the one-way polyline.
			if i > 0 && ops[i-1].Kind == core.DrawLineTo &&
				near(ops[i-1].X, 32, 1e-4) && near(ops[i-1].Y, 16, 1e-4) &&
				near(op.X, 48, 1e-4) && near(op.Y, 16, 1e-4) {
				hasOneWayTopEdge = true
			}
		}
	}

	// Exactly 1 solid rect (col 1, row 1); out-of-bounds solid tiles (0,0) and (5,3) are culled; one-way tile is not a rect.
	if len(rects) != 1 {
		t.Fatalf("got %d solid tile rects (%+v), want 1", len(rects), rects)
	}
	if !near(rects[0].X, 16, 1e-4) || !near(rects[0].Y, 16, 1e-4) || !near(rects[0].W, 16, 1e-4) || !near(rects[0].H, 16, 1e-4) {
		t.Fatalf("solid tile rect = (%v,%v,%v,%v), want (16,16,16,16)", rects[0].X, rects[0].Y, rects[0].W, rects[0].H)
	}
	if !hasOneWayTopEdge {
		t.Fatalf("expected one-way top edge (32,16)->(48,16) in ops: %+v", ops)
	}
	if len(strokeColors) != 2 || strokeColors[0] != style.SolidTileColor || strokeColors[1] != style.OneWayTileColor {
		t.Fatalf("stroke colors = %#x, want [%#x, %#x]", strokeColors, style.SolidTileColor, style.OneWayTileColor)
	}

	// Empty visible bounds draws nothing.
	emptyOps := recordCanvas(t, func(d *core.Drawing) {
		physics.DrawTileGrid(d, grid, core.Bounds{}, style)
	})
	if len(emptyOps) != 0 {
		t.Fatalf("expected 0 ops for empty visible bounds, got %d", len(emptyOps))
	}
}

func TestDebugDrawRaycast(t *testing.T) {
	w := &physics.World{
		Bodies: []*physics.Body{physics.NewBoxBody(40, 10, 20, 20)},
	}
	from := core.Point{X: 0, Y: 20}
	to := core.Point{X: 100, Y: 20}
	hits := w.Raycast(from, to, physics.QueryAll)
	if len(hits) != 1 {
		t.Fatalf("expected 1 ray hit, got %d", len(hits))
	}

	style := physics.DebugStyle{
		RayColor:      core.RGB(0xff44aa),
		NormalColor:   core.RGB(0xffff00),
		NormalLength:  8,
		ContactRadius: 2,
		LineWidth:     1,
	}
	ops := recordCanvas(t, func(d *core.Drawing) {
		physics.DrawRaycast(d, from, to, hits, style)
	})
	assertBalancedSaveRestore(t, ops)

	foundRay := false
	foundHitDot := false
	foundHitNormal := false
	for i, op := range ops {
		if op.Kind == core.DrawMoveTo && i+1 < len(ops) && ops[i+1].Kind == core.DrawLineTo {
			if near(op.X, 0, 1e-4) && near(op.Y, 20, 1e-4) && near(ops[i+1].X, 100, 1e-4) && near(ops[i+1].Y, 20, 1e-4) {
				foundRay = true
			}
			// Hit at (40,20) with normal (-1,0) -> arrow to (32,20).
			if near(op.X, 40, 1e-4) && near(op.Y, 20, 1e-4) && near(ops[i+1].X, 32, 1e-4) && near(ops[i+1].Y, 20, 1e-4) {
				foundHitNormal = true
			}
		}
		if op.Kind == core.DrawArc && near(op.X, 40, 1e-4) && near(op.Y, 20, 1e-4) && near(op.R, 2, 1e-4) {
			foundHitDot = true
		}
	}
	if !foundRay || !foundHitDot || !foundHitNormal {
		t.Fatalf("raycast draw missing expected ops (ray=%v dot=%v normal=%v): %+v", foundRay, foundHitDot, foundHitNormal, ops)
	}
}

// affine2D replays core.Drawing transform ops to compare rendered screen-space geometry.
type affine2D struct{ a, b, c, d, e, f float64 }

func (m affine2D) then(n affine2D) affine2D {
	return affine2D{
		a: m.a*n.a + m.c*n.b,
		b: m.b*n.a + m.d*n.b,
		c: m.a*n.c + m.c*n.d,
		d: m.b*n.c + m.d*n.d,
		e: m.a*n.e + m.c*n.f + m.e,
		f: m.b*n.e + m.d*n.f + m.f,
	}
}

func (m affine2D) apply(x, y float32) (float32, float32) {
	fx, fy := float64(x), float64(y)
	return float32(m.a*fx + m.c*fy + m.e), float32(m.b*fx + m.d*fy + m.f)
}

func (m affine2D) scale() float32 {
	sx := math.Hypot(m.a, m.b)
	sy := math.Hypot(m.c, m.d)
	return float32(math.Sqrt(sx * sy))
}

func TestDebugDrawAlignsWithSpritesAtEveryZoom(t *testing.T) {
	atlas := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:sprites.png"},
		Frames: map[string]sprite.Frame{
			"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 24, Width: 16, Height: 24},
			"ball": {Name: "ball", X: 16, Y: 0, W: 12, H: 12, Width: 12, Height: 12},
			"tile": {Name: "tile", X: 32, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		},
	}

	grid := physics.NewGrid(4, 4, 16, 16)
	grid.Set(1, 2, physics.TileSolid) // (16, 32, 16, 16)
	grid.Set(2, 2, physics.TileOneWay)

	heroBox := physics.Box{X: 20, Y: 8, W: 16, H: 24}
	ballCircle := physics.Circle{X: 46, Y: 20, R: 6} // bounds (40, 14, 12, 12)
	world := &physics.World{
		Tiles: grid,
		Bodies: []*physics.Body{
			physics.NewBoxBody(heroBox.X, heroBox.Y, heroBox.W, heroBox.H),
			physics.NewCircleBody(ballCircle.X, ballCircle.Y, ballCircle.R),
		},
	}
	contact := physics.Contact{
		Hit:     true,
		PointX:  36,
		PointY:  20,
		NormalX: -1,
		NormalY: 0,
		Depth:   2,
	}

	heroSprite := sprite.NewSprite(atlas.Frames["hero"])
	heroSprite.X, heroSprite.Y = heroBox.X, heroBox.Y
	ballSprite := sprite.NewSprite(atlas.Frames["ball"])
	ballSprite.X, ballSprite.Y = ballCircle.X-ballCircle.R, ballCircle.Y-ballCircle.R
	tileSprite := sprite.NewSprite(atlas.Frames["tile"])
	tileSprite.X, tileSprite.Y = 16, 32

	batch := &sprite.Batch{
		Atlas:   atlas,
		Sprites: []*sprite.Sprite{&tileSprite, &heroSprite, &ballSprite},
	}
	visible := core.Bounds{
		Origin: core.Point{X: 0, Y: 0},
		Size:   core.Size{Width: 64, Height: 64},
	}
	style := physics.DebugStyle{
		LineWidth:     1.5,
		NormalLength:  12,
		ContactRadius: 2.5,
	}

	cases := []struct {
		zoom float32
		rot  float32
	}{
		{0.5, 0},
		{1, 0},
		{2, 0},
		{4, 0},
		{2, 0.35},
	}

	for _, tc := range cases {
		name := fmt.Sprintf("zoom=%gx_rot=%g", tc.zoom, tc.rot)
		t.Run(name, func(t *testing.T) {
			ops := recordCanvas(t, func(d *core.Drawing) {
				d.Save()
				d.Translate(200, 150)
				d.Scale(tc.zoom, tc.zoom)
				if tc.rot != 0 {
					d.Rotate(tc.rot)
				}
				d.Translate(-32, -24)
				batch.Draw(d)
				physics.DrawWorld(d, world, visible, []physics.Contact{contact}, style)
				d.Restore()
			})
			assertBalancedSaveRestore(t, ops)

			// Replay transforms and collect transformed sprite boxes, physics rects, circles, contact dot, normal arrow, and line widths.
			type screenRect struct{ x0, y0, x1, y1, x2, y2, x3, y3 float32 }
			rectCorners := func(m affine2D, x, y, w, h float32) screenRect {
				x0, y0 := m.apply(x, y)
				x1, y1 := m.apply(x+w, y)
				x2, y2 := m.apply(x+w, y+h)
				x3, y3 := m.apply(x, y+h)
				return screenRect{x0, y0, x1, y1, x2, y2, x3, y3}
			}
			sameRect := func(a, b screenRect) bool {
				return near(a.x0, b.x0, 1e-3) && near(a.y0, b.y0, 1e-3) &&
					near(a.x1, b.x1, 1e-3) && near(a.y1, b.y1, 1e-3) &&
					near(a.x2, b.x2, 1e-3) && near(a.y2, b.y2, 1e-3) &&
					near(a.x3, b.x3, 1e-3) && near(a.y3, b.y3, 1e-3)
			}

			cur := affine2D{a: 1, d: 1}
			var stack []affine2D
			var spriteRects []screenRect
			var physRects []screenRect
			var circleCenterX, circleCenterY, circleScreenR float32
			var contactScreenR float32
			var normalScreenLen float32
			lineWidthCount := 0

			for i, op := range ops {
				switch op.Kind {
				case core.DrawSave:
					stack = append(stack, cur)
				case core.DrawRestore:
					cur = stack[len(stack)-1]
					stack = stack[:len(stack)-1]
				case core.DrawTranslate:
					cur = cur.then(affine2D{a: 1, d: 1, e: float64(op.X), f: float64(op.Y)})
				case core.DrawScale:
					cur = cur.then(affine2D{a: float64(op.X), d: float64(op.Y)})
				case core.DrawRotate:
					sin, cos := math.Sincos(float64(op.A))
					cur = cur.then(affine2D{a: cos, b: sin, c: -sin, d: cos})
				case core.DrawTransform:
					cur = cur.then(affine2D{float64(op.X1), float64(op.Y1), float64(op.X2), float64(op.Y2), float64(op.X), float64(op.Y)})
				case core.DrawSetTransform:
					cur = affine2D{float64(op.X1), float64(op.Y1), float64(op.X2), float64(op.Y2), float64(op.X), float64(op.Y)}
				case core.DrawImage:
					spriteRects = append(spriteRects, rectCorners(cur, op.X, op.Y, op.W, op.H))
				case core.DrawRect:
					physRects = append(physRects, rectCorners(cur, op.X, op.Y, op.W, op.H))
				case core.DrawLineWidth:
					lineWidthCount++
					screenW := op.W * cur.scale()
					if !near(screenW, style.LineWidth, 1e-3) {
						t.Fatalf("op %d: world lineWidth=%v at zoom=%v gives screen width %v, want %v",
							i, op.W, tc.zoom, screenW, style.LineWidth)
					}
				case core.DrawArc:
					if near(op.X, ballCircle.X, 1e-4) && near(op.Y, ballCircle.Y, 1e-4) {
						circleCenterX, circleCenterY = cur.apply(op.X, op.Y)
						circleScreenR = op.R * cur.scale()
					}
					if near(op.X, contact.PointX, 1e-4) && near(op.Y, contact.PointY, 1e-4) {
						contactScreenR = op.R * cur.scale()
					}
				case core.DrawMoveTo:
					if i+1 < len(ops) && ops[i+1].Kind == core.DrawLineTo &&
						near(op.X, contact.PointX, 1e-4) && near(op.Y, contact.PointY, 1e-4) {
						sx0, sy0 := cur.apply(op.X, op.Y)
						sx1, sy1 := cur.apply(ops[i+1].X, ops[i+1].Y)
						normalScreenLen = float32(math.Hypot(float64(sx1-sx0), float64(sy1-sy0)))
					}
				}
			}

			// spriteRects: [0]=tile, [1]=hero, [2]=ball.
			// physRects:   [0]=solid tile, [1]=hero box.
			if len(spriteRects) != 3 || len(physRects) != 2 {
				t.Fatalf("got %d sprite rects and %d physics rects, want 3 and 2", len(spriteRects), len(physRects))
			}
			if !sameRect(physRects[0], spriteRects[0]) {
				t.Fatalf("solid tile screen rect %+v != tile sprite %+v", physRects[0], spriteRects[0])
			}
			if !sameRect(physRects[1], spriteRects[1]) {
				t.Fatalf("hero box screen rect %+v != hero sprite %+v", physRects[1], spriteRects[1])
			}

			// Ball circle center and radius match ball sprite's transformed bounding box.
			bs := spriteRects[2]
			ballSpriteCX := (bs.x0 + bs.x2) * 0.5
			ballSpriteCY := (bs.y0 + bs.y2) * 0.5
			ballSpriteHalfW := float32(math.Hypot(float64(bs.x1-bs.x0), float64(bs.y1-bs.y0))) * 0.5
			if !near(circleCenterX, ballSpriteCX, 1e-3) || !near(circleCenterY, ballSpriteCY, 1e-3) || !near(circleScreenR, ballSpriteHalfW, 1e-3) {
				t.Fatalf("circle screen (%v,%v,r=%v) != ball sprite (%v,%v,halfW=%v)",
					circleCenterX, circleCenterY, circleScreenR, ballSpriteCX, ballSpriteCY, ballSpriteHalfW)
			}

			if lineWidthCount == 0 {
				t.Fatal("expected DrawLineWidth ops from DrawWorld")
			}
			if !near(contactScreenR, style.ContactRadius, 1e-3) {
				t.Fatalf("contact screen radius = %v, want %v", contactScreenR, style.ContactRadius)
			}
			if !near(normalScreenLen, style.NormalLength, 1e-3) {
				t.Fatalf("normal arrow screen length = %v, want %v", normalScreenLen, style.NormalLength)
			}
		})
	}
}

func TestDebugDrawExplicitZoomOverride(t *testing.T) {
	style := physics.DebugStyle{
		LineWidth: 2,
		Zoom:      4,
	}
	ops := recordCanvas(t, func(d *core.Drawing) {
		physics.DrawShape(d, physics.BoxShape(physics.Box{X: 0, Y: 0, W: 10, H: 10}), style)
	})
	for _, op := range ops {
		if op.Kind == core.DrawLineWidth {
			if !near(op.W, 0.5, 1e-4) {
				t.Fatalf("line width with explicit Zoom=4: got %v, want 0.5", op.W)
			}
			return
		}
	}
	t.Fatal("missing DrawLineWidth op")
}

func TestDebugDrawWorldPreservesDrawingState(t *testing.T) {
	grid := physics.NewGrid(2, 2, 16, 16)
	grid.Set(0, 0, physics.TileSolid)
	world := &physics.World{
		Tiles:  grid,
		Bodies: []*physics.Body{physics.NewBoxBody(4, 4, 8, 8)},
	}
	contact := physics.Contact{Hit: true, PointX: 8, PointY: 8, NormalX: 0, NormalY: -1, Depth: 1}
	visible := core.Bounds{Size: core.Size{Width: 32, Height: 32}}

	var beforeOK, afterOK bool
	var beforeStroke, afterStroke bool

	ops := recordCanvas(t, func(d *core.Drawing) {
		d.Translate(50, 40).Scale(2, 2)
		d.SetStrokeColor(core.RGB(0x123456)).SetFillColor(core.RGB(0x654321)).SetLineWidth(4)

		// Probe transform & line width before DrawWorld using a Path.
		probe := new(core.Path).MoveTo(0, 0).LineTo(10, 0)
		// Under Translate(50,40) + Scale(2,2), (5, 0) maps to (60, 40) and half line width 2*2=4 reaches y=43.
		beforeOK = d.StrokeContains(probe, 60, 43)
		beforeStroke = d.StrokeContains(probe, 60, 45)

		physics.DrawWorld(d, world, visible, []physics.Contact{contact}, physics.DefaultDebugStyle())

		// After DrawWorld, transform and line width must be identical.
		afterOK = d.StrokeContains(probe, 60, 43)
		afterStroke = d.StrokeContains(probe, 60, 45)
	})

	assertBalancedSaveRestore(t, ops)
	if !beforeOK || beforeStroke {
		t.Fatalf("unexpected baseline probe: beforeOK=%v beforeStroke=%v", beforeOK, beforeStroke)
	}
	if afterOK != beforeOK || afterStroke != beforeStroke {
		t.Fatalf("DrawWorld mutated drawing state: afterOK=%v afterStroke=%v, want %v %v",
			afterOK, afterStroke, beforeOK, beforeStroke)
	}
}

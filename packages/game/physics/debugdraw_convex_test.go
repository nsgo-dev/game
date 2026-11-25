//go:build !release

package physics_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/nsgo/core"
)

func opKinds(ops []core.DrawOp) []core.DrawKind {
	out := make([]core.DrawKind, len(ops))
	for i, op := range ops {
		out[i] = op.Kind
	}
	return out
}

func expectKinds(t *testing.T, ops []core.DrawOp, want []core.DrawKind) {
	t.Helper()
	got := opKinds(ops)
	if len(got) != len(want) {
		t.Fatalf("got ops %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("op %d = %v, want %v (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestDebugDrawConvexShapes(t *testing.T) {
	style := physics.DebugStyle{
		PolygonColor: core.RGB(0x112233),
		CapsuleColor: core.RGB(0x445566),
		SegmentColor: core.RGB(0x778899),
		LineWidth:    2,
	}
	head := []core.DrawKind{core.DrawSave, core.DrawStrokeColor, core.DrawLineWidth, core.DrawBeginPath}
	tail := []core.DrawKind{core.DrawStroke, core.DrawRestore}
	with := func(mid ...core.DrawKind) []core.DrawKind {
		out := append([]core.DrawKind{}, head...)
		out = append(out, mid...)
		return append(out, tail...)
	}

	t.Run("polygon", func(t *testing.T) {
		p := mustPolygon(t, 0, 0, 0, 20, 0, 10, 15)
		ops := recordCanvas(t, func(d *core.Drawing) { physics.DrawShape(d, physics.PolygonShape(p), style) })
		assertBalancedSaveRestore(t, ops)
		expectKinds(t, ops, with(core.DrawMoveTo, core.DrawLineTo, core.DrawLineTo, core.DrawClosePath))
		if ops[1].Color != style.PolygonColor {
			t.Fatalf("stroke color = %#x", ops[1].Color)
		}
		for i := range 3 {
			op := ops[4+i]
			v := p.Vertices[i]
			if op.X != float32(v.X) || op.Y != float32(v.Y) {
				t.Fatalf("vertex %d drawn at (%v, %v), want %v", i, op.X, op.Y, v)
			}
		}
	})

	t.Run("rounded polygon", func(t *testing.T) {
		p := mustPolygon(t, 2, 0, 0, 10, 0, 10, 10, 0, 10)
		ops := recordCanvas(t, func(d *core.Drawing) { physics.DrawShape(d, physics.PolygonShape(p), style) })
		assertBalancedSaveRestore(t, ops)
		mid := []core.DrawKind{core.DrawMoveTo}
		for range 4 {
			mid = append(mid, core.DrawArc, core.DrawLineTo)
		}
		expectKinds(t, ops, with(append(mid, core.DrawClosePath)...))
		// Every corner arc turns a quarter circle clockwise of radius 2.
		for i := 0; i < 4; i++ {
			arc := ops[5+2*i]
			if arc.R != 2 || arc.CCW || !near(arc.A1-arc.A0, math.Pi/2, 1e-5) {
				t.Fatalf("arc %d = %+v", i, arc)
			}
		}
		// The outline runs radius 2 outside the core: the top edge at y = -2.
		if ops[6].Y != -2 {
			t.Fatalf("top edge ends at y=%v, want -2", ops[6].Y)
		}
	})

	t.Run("capsule", func(t *testing.T) {
		c := physics.Capsule{X1: 10, Y1: 20, X2: 50, Y2: 20, R: 5}
		ops := recordCanvas(t, func(d *core.Drawing) { physics.DrawShape(d, physics.CapsuleShape(c), style) })
		assertBalancedSaveRestore(t, ops)
		expectKinds(t, ops, with(core.DrawMoveTo, core.DrawArc, core.DrawLineTo, core.DrawArc, core.DrawLineTo, core.DrawClosePath, core.DrawMoveTo, core.DrawLineTo))
		if ops[1].Color != style.CapsuleColor {
			t.Fatalf("stroke color = %#x", ops[1].Color)
		}
		for _, i := range []int{5, 7} {
			if arc := ops[i]; arc.R != 5 || !near(arc.A1-arc.A0, math.Pi, 1e-5) {
				t.Fatalf("end cap %+v", arc)
			}
		}
		if ops[10].X != 10 || ops[11].X != 50 {
			t.Fatalf("core segment (%v)->(%v)", ops[10].X, ops[11].X)
		}
	})

	t.Run("zero-length capsule", func(t *testing.T) {
		c := physics.Capsule{X1: 10, Y1: 20, X2: 10, Y2: 20, R: 5}
		ops := recordCanvas(t, func(d *core.Drawing) { physics.DrawShape(d, physics.CapsuleShape(c), style) })
		expectKinds(t, ops, with(core.DrawArc))
	})

	t.Run("segment", func(t *testing.T) {
		s := physics.Segment{X1: 1, Y1: 2, X2: 30, Y2: 40}
		ops := recordCanvas(t, func(d *core.Drawing) { physics.DrawShape(d, physics.SegmentShape(s), style) })
		expectKinds(t, ops, with(core.DrawMoveTo, core.DrawLineTo))
		if ops[1].Color != style.SegmentColor || ops[4].X != 1 || ops[5].Y != 40 {
			t.Fatalf("segment ops %+v", ops)
		}
	})

	t.Run("chain", func(t *testing.T) {
		open := physics.ChainShape(pts(0, 0, 10, 5, 20, 0), false)
		ops := recordCanvas(t, func(d *core.Drawing) { physics.DrawShape(d, open, style) })
		expectKinds(t, ops, with(core.DrawMoveTo, core.DrawLineTo, core.DrawLineTo))
		loop := physics.ChainShape(pts(0, 0, 10, 5, 20, 0), true)
		ops = recordCanvas(t, func(d *core.Drawing) { physics.DrawShape(d, loop, style) })
		expectKinds(t, ops, with(core.DrawMoveTo, core.DrawLineTo, core.DrawLineTo, core.DrawClosePath))
	})

	t.Run("defaults and empties", func(t *testing.T) {
		ops := recordCanvas(t, func(d *core.Drawing) {
			physics.DrawShape(d, physics.PolygonShape(physics.Polygon{}), physics.DebugStyle{})
			physics.DrawShape(d, physics.SegmentShape(physics.Segment{}), physics.DebugStyle{})
			physics.DrawShape(d, physics.ChainShape(nil, false), physics.DebugStyle{})
		})
		if len(ops) != 0 {
			t.Fatalf("empty shapes drew %v", opKinds(ops))
		}
		def := physics.DefaultDebugStyle()
		ops = recordCanvas(t, func(d *core.Drawing) {
			physics.DrawShape(d, physics.SegmentShape(physics.Segment{X2: 10}), physics.DebugStyle{})
		})
		if ops[1].Color != def.SegmentColor {
			t.Fatalf("default segment color = %#x, want %#x", ops[1].Color, def.SegmentColor)
		}
	})

	t.Run("body velocity from polygon center", func(t *testing.T) {
		b := &physics.Body{Shape: physics.PolygonShape(mustPolygon(t, 0, 0, 0, 20, 0, 20, 10, 0, 10)), VX: 10}
		st := style
		st.VelocityScale = 1
		ops := recordCanvas(t, func(d *core.Drawing) { physics.DrawBody(d, b, st) })
		var arrowStart *core.DrawOp
		for i := range ops {
			if ops[i].Kind == core.DrawStrokeColor && ops[i].Color == physics.DefaultDebugStyle().VelocityColor {
				for j := i; j < len(ops); j++ {
					if ops[j].Kind == core.DrawMoveTo {
						arrowStart = &ops[j]
						break
					}
				}
				break
			}
		}
		if arrowStart == nil || arrowStart.X != 10 || arrowStart.Y != 5 {
			t.Fatalf("velocity arrow start %+v, want (10, 5)", arrowStart)
		}
	})
}

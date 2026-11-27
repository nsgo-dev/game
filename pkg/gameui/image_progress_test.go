package gameui_test

import (
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func renderImageProgress(t *testing.T, ip *gameui.ImageProgress, w, h float32) []core.DrawOp {
	t.Helper()
	theme := gameui.DefaultTheme()
	root := &gameui.Root{Theme: &theme, Child: ip}
	root.Layout(core.Bounds{Size: core.Size{Width: core.Points(w), Height: core.Points(h)}})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "ip",
			Style:  core.Style{Width: core.Pt(core.Points(w)), Height: core.Pt(core.Points(h))},
			Draw: func(d *core.Drawing, size core.Size) {
				root.Draw(d, core.Bounds{Size: size})
			},
		}
	})
	return app.Find("ip").Ops
}

// clipRectOp returns the DrawRect op that feeds the single DrawClip in ops.
func clipRectOp(t *testing.T, ops []core.DrawOp) core.DrawOp {
	t.Helper()
	clips := 0
	var rect *core.DrawOp
	for i, op := range ops {
		if op.Kind != core.DrawClip {
			continue
		}
		clips++
		for j := i - 1; j >= 0; j-- {
			if ops[j].Kind == core.DrawRect {
				rect = &ops[j]
				break
			}
			if ops[j].Kind != core.DrawBeginPath {
				break
			}
		}
	}
	if clips != 1 {
		t.Fatalf("expected exactly 1 DrawClip op, got %d", clips)
	}
	if rect == nil {
		t.Fatal("no DrawRect op precedes the DrawClip")
	}
	return *rect
}

func TestImageProgressLinearClipRect(t *testing.T) {
	const w, h = 200, 100
	tests := []struct {
		mode         gameui.FillMode
		x, y, cw, ch float32
	}{
		{gameui.FillLeftToRight, 0, 0, 100, 100},
		{gameui.FillRightToLeft, 100, 0, 100, 100},
		{gameui.FillTopToBottom, 0, 0, 200, 50},
		{gameui.FillBottomToTop, 0, 50, 200, 50},
		{gameui.FillCenterHorizontal, 50, 0, 100, 100},
		{gameui.FillCenterVertical, 0, 25, 200, 50},
	}
	for _, tt := range tests {
		t.Run(tt.mode.String(), func(t *testing.T) {
			ip := &gameui.ImageProgress{Fill: tt.mode, Value: 0.5}
			ops := renderImageProgress(t, ip, w, h)
			rect := clipRectOp(t, ops)
			if rect.X != tt.x || rect.Y != tt.y || rect.W != tt.cw || rect.H != tt.ch {
				t.Fatalf("clip rect = (%v,%v,%v,%v), want (%v,%v,%v,%v)", rect.X, rect.Y, rect.W, rect.H, tt.x, tt.y, tt.cw, tt.ch)
			}
		})
	}
}

func TestImageProgressRadialPieClip(t *testing.T) {
	const w, h = 200, 100
	r := float32(math.Hypot(w, h)) / 2

	tests := []struct {
		name string
		fill gameui.FillMode
		a1   float64
		ccw  bool
	}{
		{"clockwise", gameui.FillClockwise, -math.Pi/2 + math.Pi/2, false},
		{"counter_clockwise", gameui.FillCounterClockwise, -math.Pi/2 - math.Pi/2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := &gameui.ImageProgress{Fill: tt.fill, StartAngle: -90, Span: 360, Value: 0.25}
			ops := renderImageProgress(t, ip, w, h)
			var move, line, arc *core.DrawOp
			for i := range ops {
				switch ops[i].Kind {
				case core.DrawMoveTo:
					move = &ops[i]
				case core.DrawLineTo:
					line = &ops[i]
				case core.DrawArc:
					arc = &ops[i]
				}
			}
			if move == nil || line == nil || arc == nil {
				t.Fatal("radial fill did not record a pie clip path (MoveTo/LineTo/Arc)")
			}
			cx, cy := float32(w/2), float32(h/2)
			if move.X != cx || move.Y != cy {
				t.Fatalf("pie MoveTo = (%v,%v), want center (%v,%v)", move.X, move.Y, cx, cy)
			}
			a0 := -math.Pi / 2
			wantLX := cx + r*float32(math.Cos(a0))
			wantLY := cy + r*float32(math.Sin(a0))
			if math.Abs(float64(line.X-wantLX)) > 1e-4 || math.Abs(float64(line.Y-wantLY)) > 1e-4 {
				t.Fatalf("pie LineTo = (%v,%v), want ray endpoint (%v,%v)", line.X, line.Y, wantLX, wantLY)
			}
			if arc.X != cx || arc.Y != cy || math.Abs(float64(arc.R-r)) > 1e-4 {
				t.Fatalf("arc center/radius = (%v,%v,%v), want (%v,%v,%v)", arc.X, arc.Y, arc.R, cx, cy, r)
			}
			if math.Abs(float64(arc.A0)-a0) > 1e-6 || math.Abs(float64(arc.A1)-tt.a1) > 1e-6 {
				t.Fatalf("arc angles = (%v,%v), want (%v,%v)", arc.A0, arc.A1, a0, tt.a1)
			}
			if arc.CCW != tt.ccw {
				t.Fatalf("arc CCW = %v, want %v", arc.CCW, tt.ccw)
			}
		})
	}
}

func TestImageProgressFractionGoldenOps(t *testing.T) {
	const w, h = 200, 100
	fractions := []struct {
		value float32
		clipW float32 // expected left_to_right clip width; -1 means no progress ops at all
	}{
		{0, -1},
		{0.25, 50},
		{0.5, 100},
		{1, 200},
	}
	digests := make(map[string]float32, len(fractions))
	for _, f := range fractions {
		ip := &gameui.ImageProgress{Fill: gameui.FillLeftToRight, Value: f.value}
		ops1 := renderImageProgress(t, ip, w, h)
		ops2 := renderImageProgress(t, &gameui.ImageProgress{Fill: gameui.FillLeftToRight, Value: f.value}, w, h)
		d1, d2 := canonicalOpsDigest(ops1), canonicalOpsDigest(ops2)
		if d1 != d2 {
			t.Fatalf("fraction %v render is non-deterministic: %s != %s", f.value, d1, d2)
		}
		if prev, dup := digests[d1]; dup {
			t.Fatalf("fractions %v and %v produced identical DrawOps", prev, f.value)
		}
		digests[d1] = f.value

		clips := 0
		for _, op := range ops1 {
			if op.Kind == core.DrawClip {
				clips++
			}
		}
		if f.clipW < 0 {
			if clips != 0 {
				t.Fatalf("fraction %v drew progress ops, want none", f.value)
			}
			continue
		}
		rect := clipRectOp(t, ops1)
		if rect.X != 0 || rect.Y != 0 || rect.W != f.clipW || rect.H != h {
			t.Fatalf("fraction %v clip rect = (%v,%v,%v,%v), want (0,0,%v,%v)", f.value, rect.X, rect.Y, rect.W, rect.H, f.clipW, h)
		}
	}
}

func TestImageProgressNineSliceCapsPreserved(t *testing.T) {
	const w, h = 200, 100
	layer := func(originX float32) *gameui.NineSlice {
		return &gameui.NineSlice{
			Image:  core.Src("assets/ui_frame.png"),
			Src:    core.Bounds{Origin: core.Point{X: core.Points(originX)}, Size: core.Size{Width: 48, Height: 48}},
			Left:   12,
			Top:    12,
			Right:  12,
			Bottom: 12,
		}
	}
	ip := &gameui.ImageProgress{
		Under:    layer(0),
		Progress: layer(48),
		Over:     layer(96),
		Fill:     gameui.FillLeftToRight,
		Value:    0.5,
	}
	ops := renderImageProgress(t, ip, w, h)

	// dst rect (X,Y,W,H) tuples of each layer's DrawImage ops, in op order.
	var under, progress, over [][4]float32
	seenClip := false
	seenRestoreAfterClip := false
	for _, op := range ops {
		switch op.Kind {
		case core.DrawClip:
			seenClip = true
		case core.DrawRestore:
			if seenClip && len(progress) > 0 {
				seenRestoreAfterClip = true
			}
		case core.DrawImage:
			tuple := [4]float32{op.X, op.Y, op.W, op.H}
			switch {
			case seenClip && !seenRestoreAfterClip:
				progress = append(progress, tuple)
			case len(under) < 9 && len(progress) == 0:
				under = append(under, tuple)
			default:
				over = append(over, tuple)
			}
		}
	}
	if len(under) != 9 || len(progress) != 9 || len(over) != 9 {
		t.Fatalf("DrawImage ops per layer = under:%d progress:%d over:%d, want 9 each", len(under), len(progress), len(over))
	}
	if !seenRestoreAfterClip {
		t.Fatal("progress layer is not bracketed by Clip...Restore")
	}
	// The clip never changes the nine-slice destination geometry: at any fraction
	// the progress caps sit exactly where the under caps do.
	for i := range under {
		if under[i] != progress[i] {
			t.Fatalf("patch %d dst = under %v, progress %v; caps moved under the clip", i, under[i], progress[i])
		}
	}
	// Corners keep their 12pt cap size; the middle column stretches.
	if under[0][2] != 12 || under[2][2] != 12 {
		t.Fatalf("horizontal caps = %v and %v, want 12 each", under[0][2], under[2][2])
	}
}

func TestParseFillModeRoundTrip(t *testing.T) {
	for mode := gameui.FillLeftToRight; mode <= gameui.FillCounterClockwise; mode++ {
		got, ok := gameui.ParseFillMode(mode.String())
		if !ok || got != mode {
			t.Fatalf("ParseFillMode(%q) = %v, %v; want %v, true", mode.String(), got, ok, mode)
		}
	}
	if _, ok := gameui.ParseFillMode("diagonal"); ok {
		t.Fatal("ParseFillMode accepted unknown mode")
	}
	if s := gameui.FillMode(99).String(); s != "left_to_right" {
		t.Fatalf("unknown FillMode String() = %q, want left_to_right", s)
	}
}

func TestImageProgressScreenNode(t *testing.T) {
	yamlDoc := `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/screen.v1.json
version: 1
name: boss
designWidth: 640
designHeight: 360
nodes:
  - id: boss_bar
    type: image_progress
    anchor: top_center
    offset:
      "y": 20
    width: 300
    height: 24
    under: art/ui/bar_back.png
    progress: art/ui/bar_fill.png
    over: art/ui/bar_frame.png
    fill: clockwise
    startAngle: -90
    min: 10
    max: 100
    value: 25
`
	doc, err := gameui.ParseScreen([]byte(yamlDoc))
	if err != nil {
		t.Fatalf("ParseScreen: %v", err)
	}
	encoded, err := gameui.EncodeScreen(doc)
	if err != nil {
		t.Fatalf("EncodeScreen: %v", err)
	}
	if string(encoded) != yamlDoc {
		t.Fatalf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", encoded, yamlDoc)
	}

	node := doc.Nodes[0]
	resolveCalls := 0
	built, err := gameui.BuildScreen(doc, gameui.ScreenBuildOptions{
		Sprites: func(path, frame string) (core.ImageSource, core.Bounds, bool) {
			resolveCalls++
			return core.Src("asset:" + path), core.Bounds{Size: core.Size{Width: 48, Height: 48}}, true
		},
	})
	if err != nil {
		t.Fatalf("BuildScreen: %v", err)
	}
	if resolveCalls != 3 {
		t.Fatalf("Sprites resolver called %d times, want 3 (under, progress, over)", resolveCalls)
	}
	ip, ok := built.Nodes[node.ID].(*gameui.ImageProgress)
	if !ok {
		t.Fatalf("node %q built as %T, want *gameui.ImageProgress", node.ID, built.Nodes[node.ID])
	}
	if ip.Under == nil || ip.Progress == nil || ip.Over == nil {
		t.Fatal("image layers were not resolved onto the widget")
	}
	if ip.Fill != gameui.FillClockwise || ip.StartAngle != -90 || ip.Min != 10 || ip.Max != 100 || ip.Value != 25 {
		t.Fatalf("widget fields = fill:%v startAngle:%v min:%v max:%v value:%v", ip.Fill, ip.StartAngle, ip.Min, ip.Max, ip.Value)
	}

	invalid := []struct {
		name string
		yaml string
		want string
	}{
		{"bad fill", strings.Replace(yamlDoc, "fill: clockwise", "fill: diagonal", 1), "unknown fill"},
		{"span over 360", strings.Replace(yamlDoc, "startAngle: -90", "startAngle: -90\n    span: 400", 1), "span"},
		{"bad under path", strings.Replace(yamlDoc, "under: art/ui/bar_back.png", "under: '  art/ui/bar_back.png'", 1), "invalid under path"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := gameui.ParseScreen([]byte(tt.yaml)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseScreen error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

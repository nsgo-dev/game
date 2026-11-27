package worldmap_test

import (
	"image/color"
	"math"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/worldmap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// testStyle uses opaque primary colors so rendered pixels identify what was drawn; borders and links are off because the software renderer only fills rectangles.
func testStyle() *worldmap.Style {
	return &worldmap.Style{
		Visited:     core.RGB(0x0000ff),
		Revealed:    core.RGB(0x00ff00),
		Current:     core.RGB(0x00ffff),
		Marker:      core.RGB(0xffff00),
		MarkerKinds: map[string]core.Color{"boss": core.RGB(0xff00ff)},
		MarkerSize:  4,
		Player:      core.RGB(0xff0000),
		PlayerSize:  4,
		Padding:     0,
	}
}

func rgba(c uint32) color.RGBA {
	return color.RGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 0xff}
}

func pixel(t *testing.T, ops []core.DrawOp, w, h, x, y int) color.RGBA {
	t.Helper()
	img := gametest.RenderOps(w, h, ops)
	return img.RGBAAt(x, y)
}

type fill struct {
	color core.Color
	rects []math2d.Rect
}

// fills returns every Fill in ops with its color and the rectangles of its path, whether recorded by Rect + Fill or FillRect.
func fills(ops []core.DrawOp) []fill {
	var out []fill
	var path []math2d.Rect
	var c core.Color
	for _, op := range ops {
		switch op.Kind {
		case core.DrawBeginPath:
			path = nil
		case core.DrawRect:
			path = append(path, math2d.Rect{X: op.X, Y: op.Y, W: op.W, H: op.H})
		case core.DrawFillColor:
			c = op.Color
		case core.DrawFill:
			out = append(out, fill{color: c, rects: append([]math2d.Rect(nil), path...)})
		}
	}
	return out
}

func filledWith(ops []core.DrawOp, c core.Color) []math2d.Rect {
	var out []math2d.Rect
	for _, f := range fills(ops) {
		if f.color == c {
			out = append(out, f.rects...)
		}
	}
	return out
}

func TestWorldMapDrawsOnlyDiscoveredRooms(t *testing.T) {
	m := loadCastle(t)
	// The default area spans x 0..800 and y -180..180, so a 400x180 map draws it at scale 0.5 with y offset 90.
	dst := core.Bounds{Size: core.Size{Width: 400, Height: 180}}
	draw := func() []core.DrawOp {
		var d core.Drawing
		m.DrawWorldMap(&d, dst, "", nil, testStyle())
		return d.Ops
	}

	ops := draw()
	if got := fills(ops); len(got) != 0 {
		t.Fatalf("an undiscovered map filled %v", got)
	}
	if c := pixel(t, ops, 400, 180, 80, 90); c.A != 0 {
		t.Fatalf("undiscovered entrance painted %v", c)
	}

	if _, err := m.Enter("entrance"); err != nil {
		t.Fatal(err)
	}
	m.Visit(math2d.Vec2{X: 400, Y: 90}) // hall becomes current
	if err := m.Reveal("tower"); err != nil {
		t.Fatal(err)
	}
	ops = draw()
	img := gametest.RenderOps(400, 180, ops)
	for _, tc := range []struct {
		name string
		x, y int
		want color.RGBA
	}{
		{"visited entrance", 80, 120, rgba(0x0000ff)},
		{"current hall", 240, 120, rgba(0x00ffff)},
		{"revealed tower", 360, 40, rgba(0x00ff00)},
		{"fixed save marker", 20, 135, rgba(0xffff00)},
		{"boss marker", 360, 90, rgba(0xff00ff)},
	} {
		if got := img.RGBAAt(tc.x, tc.y); got != tc.want {
			t.Errorf("%s at (%d, %d) = %v, want %v", tc.name, tc.x, tc.y, got, tc.want)
		}
	}
	if c := img.RGBAAt(80, 20); c.A != 0 {
		t.Errorf("outside every room painted %v", c)
	}

	if !reflect.DeepEqual(ops, draw()) {
		t.Fatal("drawing the same map twice recorded different ops")
	}
}

func TestWorldMapLeavesOutOtherAreasAndUnknownMarkers(t *testing.T) {
	m := loadCastle(t)
	if err := m.Reveal("cellar"); err != nil {
		t.Fatal(err)
	}
	if err := m.AddMarker(worldmap.Marker{ID: "pin", Kind: "note", Room: "hall", X: 400, Y: 90}); err != nil {
		t.Fatal(err)
	}
	var d core.Drawing
	dst := core.Bounds{Size: core.Size{Width: 400, Height: 180}}
	m.DrawWorldMap(&d, dst, "", nil, testStyle())
	// Nothing: the cellar is in another area and the hall (with the pin) is unknown.
	if got := fills(d.Ops); len(got) != 0 {
		t.Fatalf("filled %v", got)
	}

	d.Ops = nil
	m.DrawWorldMap(&d, dst, "crypt", nil, testStyle())
	if got := filledWith(d.Ops, core.RGB(0x00ff00)); len(got) != 1 || got[0] != (math2d.Rect{X: 40, W: 320, H: 180}) {
		t.Fatalf("crypt map revealed rooms = %v, want the cellar fitted at scale 1", got)
	}

	d.Ops = nil
	m.DrawWorldMap(&d, dst, "attic", nil, testStyle())
	if len(d.Ops) != 0 {
		t.Fatalf("a missing area drew %d ops", len(d.Ops))
	}
	m.DrawWorldMap(nil, dst, "", nil, nil)
}

func TestWorldMapPlayerOnlyInTheCurrentArea(t *testing.T) {
	m := loadCastle(t)
	player := math2d.Vec2{X: 100, Y: 90}
	m.Visit(player)
	dst := core.Bounds{Size: core.Size{Width: 400, Height: 180}}

	var d core.Drawing
	m.DrawWorldMap(&d, dst, "", &player, testStyle())
	if c := pixel(t, d.Ops, 400, 180, 50, 135); c != rgba(0xff0000) {
		t.Fatalf("player pixel = %v", c)
	}
	if err := m.Reveal("cellar"); err != nil {
		t.Fatal(err)
	}
	d.Ops = nil
	m.DrawWorldMap(&d, dst, "crypt", &player, testStyle())
	for _, op := range d.Ops {
		if op.Kind == core.DrawFillColor && op.Color == core.RGB(0xff0000) {
			t.Fatal("drew the player on another area's map")
		}
	}
}

func TestMinimapCentersOnThePlayerAndClips(t *testing.T) {
	m := loadCastle(t)
	m.Visit(math2d.Vec2{X: 10, Y: 10})
	player := math2d.Vec2{X: 400, Y: 90}
	m.Visit(player)
	if err := m.Reveal("tower"); err != nil {
		t.Fatal(err)
	}
	if err := m.Reveal("cellar"); err != nil {
		t.Fatal(err)
	}
	// 100x100 minimap at scale 0.25 shows world x 200..600, y -110..290: the hall and part of the entrance, not the tower (x >= 640) or the cellar (another area).
	dst := core.Bounds{Origin: core.Point{X: 10, Y: 10}, Size: core.Size{Width: 100, Height: 100}}
	var d core.Drawing
	m.DrawMinimap(&d, dst, player, 0.25, testStyle())

	if got := filledWith(d.Ops, core.RGB(0x0000ff)); len(got) != 1 || got[0] != (math2d.Rect{X: -40, Y: 37.5, W: 80, H: 45}) {
		t.Fatalf("visited rooms on the minimap = %v, want only the entrance", got)
	}
	if got := filledWith(d.Ops, core.RGB(0x00ffff)); len(got) != 1 || got[0] != (math2d.Rect{X: 40, Y: 37.5, W: 80, H: 45}) {
		t.Fatalf("current room on the minimap = %v, want the hall", got)
	}
	if got := filledWith(d.Ops, core.RGB(0x00ff00)); len(got) != 0 {
		t.Fatalf("revealed rooms on the minimap = %v, want none in view", got)
	}
	if d.Ops[0].Kind != core.DrawSave || d.Ops[2] != (core.DrawOp{Kind: core.DrawRect, X: 10, Y: 10, W: 100, H: 100}) || d.Ops[3].Kind != core.DrawClip {
		t.Fatalf("minimap does not start by clipping to dst: %+v", d.Ops[:4])
	}
	img := gametest.RenderOps(120, 120, d.Ops)
	if c := img.RGBAAt(60, 60); c != rgba(0xff0000) {
		t.Fatalf("center pixel = %v, want the player", c)
	}
	if c := img.RGBAAt(100, 60); c != rgba(0x00ffff) {
		t.Fatalf("hall pixel = %v, want the current room color", c)
	}

	var again core.Drawing
	m.DrawMinimap(&again, dst, player, 0.25, testStyle())
	if !reflect.DeepEqual(d.Ops, again.Ops) {
		t.Fatal("minimap ops differ between identical draws")
	}

	// A scale that is not positive and finite draws as scale 1.
	var unit core.Drawing
	m.DrawMinimap(&unit, dst, player, 1, testStyle())
	for _, scale := range []float32{0, -2, float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
		if p := worldmap.MinimapProjection(dst, player, scale); p.Scale != 1 {
			t.Fatalf("MinimapProjection scale %v = %+v, want scale 1", scale, p)
		}
		var got core.Drawing
		m.DrawMinimap(&got, dst, player, scale, testStyle())
		if !reflect.DeepEqual(got.Ops, unit.Ops) {
			t.Fatalf("minimap at scale %v differs from scale 1", scale)
		}
	}
	// A position that is not finite, or that overflows at this scale, draws nothing.
	nan, inf := float32(math.NaN()), float32(math.Inf(1))
	for _, p := range []math2d.Vec2{{X: nan, Y: 90}, {X: 400, Y: nan}, {X: inf, Y: 90}, {X: 400, Y: -inf}, {X: 3e38, Y: 0}} {
		var got core.Drawing
		m.DrawMinimap(&got, dst, p, 10, testStyle())
		if len(got.Ops) != 0 {
			t.Fatalf("minimap at player %v drew %d ops, want none", p, len(got.Ops))
		}
	}
}

func TestWorldMapSkipsANonFinitePlayer(t *testing.T) {
	m := loadCastle(t)
	m.Visit(math2d.Vec2{X: 100, Y: 90})
	dst := core.Bounds{Size: core.Size{Width: 400, Height: 180}}
	var want core.Drawing
	m.DrawWorldMap(&want, dst, "", nil, testStyle())
	for _, p := range []math2d.Vec2{{X: float32(math.NaN()), Y: 90}, {X: 100, Y: float32(math.Inf(1))}} {
		var got core.Drawing
		m.DrawWorldMap(&got, dst, "", &p, testStyle())
		if !reflect.DeepEqual(got.Ops, want.Ops) {
			t.Fatalf("world map with player %v differs from one without a player", p)
		}
	}
}

func TestTransparentCurrentFillsWithVisited(t *testing.T) {
	m := loadCastle(t)
	m.Visit(math2d.Vec2{X: 10, Y: 10})
	m.Visit(math2d.Vec2{X: 400, Y: 10})
	st := testStyle()
	st.Current = 0
	var d core.Drawing
	m.DrawWorldMap(&d, core.Bounds{Size: core.Size{Width: 800, Height: 360}}, "", nil, st)
	if got := filledWith(d.Ops, st.Visited); len(got) != 2 {
		t.Fatalf("visited fills with a transparent Current = %v, want the entrance and the hall", got)
	}
}

func TestProjectionRoundTrip(t *testing.T) {
	m := loadCastle(t)
	dst := core.Bounds{Origin: core.Point{X: 20, Y: 30}, Size: core.Size{Width: 300, Height: 300}}
	proj, ok := m.WorldMapProjection(dst, "", 10)
	if !ok {
		t.Fatal("WorldMapProjection failed")
	}
	if proj.Scale != 280.0/800 {
		t.Fatalf("scale = %v, want the width-limited fit", proj.Scale)
	}
	world := math2d.Vec2{X: 640, Y: -180}
	if back := proj.ToWorld(proj.ToMap(world)); !back.ApproxEqual(world, 1e-3) {
		t.Fatalf("ToWorld(ToMap(%v)) = %v", world, back)
	}
	center := proj.ToMap(math2d.Vec2{X: 400, Y: 0})
	if !center.ApproxEqual(math2d.Vec2{X: 170, Y: 180}, 1e-3) {
		t.Fatalf("area center on the map = %v, want the center of dst", center)
	}
	if _, ok := m.WorldMapProjection(dst, "", 200); ok {
		t.Fatal("padding larger than dst produced a projection")
	}
	if p := (worldmap.Projection{}).ToWorld(math2d.Vec2{X: 1, Y: 1}); p != (math2d.Vec2{}) {
		t.Fatalf("zero projection ToWorld = %v", p)
	}
	if p := worldmap.MinimapProjection(dst, math2d.Vec2{}, -1); p.Scale != 1 {
		t.Fatalf("non-positive minimap scale = %v, want 1", p.Scale)
	}
}

func TestDrawInACanvas(t *testing.T) {
	m := loadCastle(t)
	player := math2d.Vec2{X: 100, Y: 90}
	m.Visit(player)
	style := worldmap.DefaultStyle()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "minimap",
				Style:  core.Style{Width: core.Pt(120), Height: core.Pt(120)},
				Draw: func(d *core.Drawing, size core.Size) {
					m.DrawMinimap(d, core.Bounds{Size: size}, player, 0.2, &style)
				},
			},
			core.Canvas{
				TestID: "world",
				Style:  core.Style{Width: core.Pt(320), Height: core.Pt(180)},
				Draw: func(d *core.Drawing, size core.Size) {
					m.DrawWorldMap(d, core.Bounds{Size: size}, "", &player, &style)
				},
			},
		}}
	})
	for _, id := range []string{"minimap", "world"} {
		ops := app.Find(id).Ops
		var strokes, fills int
		for _, op := range ops {
			switch op.Kind {
			case core.DrawStroke:
				strokes++
			case core.DrawFill:
				fills++
			}
		}
		if fills == 0 || strokes == 0 {
			t.Errorf("%s canvas: %d fills and %d strokes, want rooms filled and outlined", id, fills, strokes)
		}
	}
}

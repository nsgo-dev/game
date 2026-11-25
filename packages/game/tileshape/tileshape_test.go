package tileshape_test

import (
	"cmp"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/lights2d"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/platformer"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/game/packages/game/tileshape"
	"github.com/nsgo-dev/nsgo/core"
)

const step = time.Second / 60

func loadSlopes(t testing.TB) *tilemap.Tilemap {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "slopes.tmj"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{})
	if err != nil {
		t.Fatalf("LoadTiled: %v", err)
	}
	return m
}

// surface is the height of the ground of testdata/slopes.tmj at world x: flat, a 45 degree rise, flat, a 2:1 rise broken by a half tile, a flat top, a mirrored 2:1 descent and a mirrored 45 degree descent.
func surface(x float64) float64 {
	switch {
	case x < 32:
		return 96
	case x < 48:
		return 96 - (x - 32)
	case x < 64:
		return 80
	case x < 80:
		return 80 - (x-64)/2
	case x < 96:
		return 72
	case x < 112:
		return 72 - (x-96)/2
	case x < 128:
		return 64
	case x < 144:
		return 64 + (x-128)/2
	case x < 160:
		return 72 + (x-144)/2
	case x < 176:
		return 80 + (x - 160)
	default:
		return 96
	}
}

// supportY is the highest ground of surface under a body spanning [x, x+w].
func supportY(surface func(float64) float64, x, w float64) float64 {
	y := surface(x + w - 1e-9)
	for s := x; s < x+w; s += 0.25 {
		y = min(y, surface(s))
	}
	return y
}

func TestPlatformerWalksSlopeAndHalfTilesWithoutHandPlacedSlopes(t *testing.T) {
	m := loadSlopes(t)
	walkAcross(t, tileshape.New(m, tileshape.Options{}), surface, 4, 80, 196)
}

// TestPlatformerWalksTrue22Point5DegreeSlopes walks a ramp of tiles rising at exactly 22.5 degrees (tan 22.5 of a cell per cell), up two cells, across a flat top and down a mirrored ramp, since slopes.tmj's 2:1 tiles are about 26.6 degrees.
func TestPlatformerWalksTrue22Point5DegreeSlopes(t *testing.T) {
	rise := 16 * math.Tan(22.5*math.Pi/180)
	low, high := float32(16-rise), float32(16-2*rise)
	poly := func(pts ...core.Point) []tilemap.TileShape {
		return []tilemap.TileShape{{Shape: tilemap.ObjectPolygon, Points: pts}}
	}
	ts := tilemap.NewGridTileset(1, core.ImageSource{}, 16, 16, 4, 1)
	ts.Shapes = map[uint32][]tilemap.TileShape{
		1: poly(pt(0, 16), pt(16, float64(low)), pt(16, 16)),
		2: poly(pt(0, float64(low)), pt(16, float64(high)), pt(16, 16), pt(0, 16)),
		3: poly(pt(0, float64(high)), pt(16, float64(high)), pt(16, 16), pt(0, 16)),
	}
	m := tilemap.New(12, 4, 16, 16, ts)
	h := tilemap.FlipHorizontal
	ground := make([]uint32, 12*4)
	copy(ground[2*12:], []uint32{0, 0, 0, 2, 3, 4, 4, 3 | h, 2 | h, 0, 0, 0})
	for c := range 12 {
		ground[3*12+c] = 1
	}
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 12, 4, ground))
	m.SetFlags(1, tilemap.TileSolid)
	top := 48 - 2*rise
	surface := func(x float64) float64 {
		switch {
		case x < 48:
			return 48
		case x < 80:
			return 48 - (x-48)*rise/16
		case x < 112:
			return top
		case x < 144:
			return top + (x-112)*rise/16
		default:
			return 48
		}
	}
	walkAcross(t, tileshape.New(m, tileshape.Options{}), surface, 4, 32, 172)
}

// walkAcross drives a platformer controller over v from x0 to past x1 and back, failing the moment the body leaves the ground, meets a wall or strays more than 0.6 from surface.
func walkAcross(t *testing.T, v *tileshape.View, surface func(float64) float64, x0, y0, x1 float32) {
	t.Helper()
	body := physics.NewCharacterBody(physics.NewBoxBody(x0, y0, 12, 16))
	c, err := platformer.NewController(body, platformer.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	walk := func(dir float32, until func(x float64) bool) {
		t.Helper()
		for i := 0; ; i++ {
			if i > 600 {
				t.Fatalf("walking %v: stuck at %v", dir, body.Body.Bounds())
			}
			res := c.Step(platformer.Input{Move: dir}, v, v.Slopes(), nil, step)
			b := body.Body.Bounds()
			x := float64(b.X)
			if res.Move.OnLeftWall || res.Move.OnRightWall {
				t.Fatalf("walking %v: blocked by a wall at x=%.2f", dir, x)
			}
			if !body.Grounded {
				t.Fatalf("walking %v: left the ground at x=%.2f, bottom %.2f", dir, x, b.Bottom())
			}
			if want := supportY(surface, x, float64(b.W)); math.Abs(float64(b.Bottom())-want) > 0.6 {
				t.Fatalf("walking %v: bottom %.3f at x=%.2f, want the ground at %.3f", dir, b.Bottom(), x, want)
			}
			if until(x) {
				return
			}
		}
	}
	walk(1, func(x float64) bool { return x > float64(x1) })
	walk(-1, func(x float64) bool { return x < float64(x0) })
}

func TestViewIsATileGridForMoveAndSlideAndWorld(t *testing.T) {
	m := loadSlopes(t)
	var grid physics.TileGrid = tileshape.New(m, tileshape.Options{})

	if w, h := grid.TileSize(); w != 16 || h != 16 {
		t.Fatalf("TileSize = %v, %v", w, h)
	}
	if c, r := grid.GridSize(); c != 14 || r != 7 {
		t.Fatalf("GridSize = %v, %v", c, r)
	}
	for _, tc := range []struct {
		col, row int
		want     physics.TileFlags
	}{
		{0, 6, physics.TileSolid}, // a shape that fills the cell
		{3, 5, physics.TileSolid}, // the solid property
		{2, 5, 0},                 // a slope collides through Slopes
		{5, 4, 0},                 // a half tile too
		{0, 0, 0},
		{-1, 0, 0},
		{14, 6, 0},
	} {
		if got := grid.TileFlagsAt(tc.col, tc.row); got != tc.want {
			t.Errorf("TileFlagsAt(%d, %d) = %v, want %v", tc.col, tc.row, got, tc.want)
		}
	}

	b := physics.NewBoxBody(1, 10, 12, 16)
	for range 30 {
		b.VY += 900 * float32(step.Seconds())
		physics.MoveAndSlide(b, grid, step)
	}
	if !b.Grounded || b.Bounds().Bottom() != 96 {
		t.Fatalf("MoveAndSlide: grounded %v, bottom %v, want on the floor at 96", b.Grounded, b.Bounds().Bottom())
	}

	w := physics.World{Tiles: grid}
	hit, ok := w.RaycastFirst(core.Point{X: 8, Y: 0}, core.Point{X: 8, Y: 200}, physics.QueryAll)
	if !ok || hit.Point.Y != 96 {
		t.Fatalf("World.RaycastFirst = %+v, %v, want a hit at y 96", hit, ok)
	}

	solid := tileshape.New(m, tileshape.Options{ShapedSolid: true})
	if got := solid.TileFlagsAt(2, 5); got != physics.TileSolid {
		t.Fatalf("ShapedSolid: TileFlagsAt(2, 5) = %v, want TileSolid", got)
	}
}

func TestSlopesFromShapes(t *testing.T) {
	m := loadSlopes(t)
	v := tileshape.New(m, tileshape.Options{})
	got := v.Slopes()
	// Every slope is a shape's upward-facing or vertical edge; edges against solid cells and other shapes are gone.
	want := []physics.Slope{
		{A: pt(32, 96), B: pt(48, 80)},   // 45 degree rise
		{A: pt(64, 80), B: pt(80, 72)},   // 2:1 lower
		{A: pt(80, 72), B: pt(96, 72)},   // half tile
		{A: pt(96, 72), B: pt(112, 64)},  // 2:1 upper
		{A: pt(128, 64), B: pt(160, 80)}, // mirrored 2:1 upper and lower, merged
		{A: pt(160, 80), B: pt(176, 96)}, // mirrored 45 degree
	}
	if !sameSlopes(got, want) {
		t.Fatalf("Slopes = %v\nwant %v", got, want)
	}
}

func TestOutlineMergesCellsAndShapes(t *testing.T) {
	m := tilemap.New(5, 4, 10, 10)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 5, 4, nil))
	m.SetFlags(1, tilemap.TileSolid)
	m.FillRect(0, 1, 1, 3, 2, 1)
	v := tileshape.New(m, tileshape.Options{})

	got := v.Outline()
	want := []tileshape.Segment{
		{A: pt(10, 10), B: pt(40, 10)},
		{A: pt(40, 10), B: pt(40, 30)},
		{A: pt(40, 30), B: pt(10, 30)},
		{A: pt(10, 30), B: pt(10, 10)},
	}
	if !sameSegmentSet(got, want) {
		t.Fatalf("Outline = %v\nwant %v", got, want)
	}
	for _, s := range got {
		n := s.Normal()
		mid := core.Point{X: (s.A.X+s.B.X)/2 + n.X, Y: (s.A.Y+s.B.Y)/2 + n.Y}
		if mid.X > 10 && mid.X < 40 && mid.Y > 10 && mid.Y < 30 {
			t.Errorf("Normal of %v points into the solid", s)
		}
	}
	if boxes := v.Boxes(); !slices.Equal(boxes, []physics.Box{{X: 10, Y: 10, W: 30, H: 20}}) {
		t.Fatalf("Boxes = %v", boxes)
	}
	if v.Slopes() != nil {
		t.Fatalf("whole cells gave slopes %v", v.Slopes())
	}
}

func TestOccluderCastsTheShadowsOfRectangles(t *testing.T) {
	const cols, rows, size = 24, 18, 16
	m := tilemap.New(cols, rows, size, size)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("walls", cols, rows, nil))
	m.SetFlags(1, tilemap.TileSolid)
	rng := rand.New(rand.NewPCG(7, 11))
	for range 90 {
		m.SetTile(0, rng.IntN(cols), rng.IntN(rows), 1)
	}
	v := tileshape.New(m, tileshape.Options{})

	check := func(label string) {
		t.Helper()
		var rects []lights2d.Occluder
		for row := range rows {
			for col := range cols {
				if m.FlagsAt(col, row)&tilemap.TileSolid != 0 {
					rects = append(rects, lights2d.RectOccluder(core.Bounds{
						Origin: core.Point{X: core.Points(col * size), Y: core.Points(row * size)},
						Size:   core.Size{Width: size, Height: size},
					}))
				}
			}
		}
		derived := v.Occluders()
		segments := 0
		for _, o := range derived {
			segments += len(o.Segments)
		}
		if segments >= 4*len(rects) {
			t.Fatalf("%s: %d derived segments for %d cells: nothing merged", label, segments, len(rects))
		}
		lights := []core.Point{{X: 100.3, Y: 77.7}, {X: 251.1, Y: 203.9}, {X: 13.37, Y: 270.2}}
		differ := 0
		for _, l := range lights {
			if m.FlagsAt(int(l.X)/size, int(l.Y)/size) != 0 {
				continue
			}
			for y := 3.1; y < rows*size; y += 7.3 {
				for x := 2.9; x < cols*size; x += 7.7 {
					p := core.Point{X: core.Points(x), Y: core.Points(y)}
					if lights2d.Visible(l, p, rects) != lights2d.Visible(l, p, derived) {
						differ++
					}
				}
			}
		}
		if differ != 0 {
			t.Fatalf("%s: %d sample points lit differently from per-cell rectangles", label, differ)
		}
	}
	check("loaded")
	for range 40 {
		gid := uint32(0)
		if rng.IntN(2) == 0 {
			gid = 1
		}
		m.SetTile(0, rng.IntN(cols), rng.IntN(rows), gid)
	}
	check("after SetTile")
}

func TestShapesFollowFlipBits(t *testing.T) {
	// The tile's shape is the triangle filling its bottom-right half.
	tri := []core.Point{pt(0, 16), pt(16, 0), pt(16, 16)}
	for _, tc := range []struct {
		name  string
		flags uint32
		want  []core.Point // the shape's corners after the flip
	}{
		{"none", 0, []core.Point{pt(0, 16), pt(16, 0), pt(16, 16)}},
		{"horizontal", tilemap.FlipHorizontal, []core.Point{pt(16, 16), pt(0, 0), pt(0, 16)}},
		{"vertical", tilemap.FlipVertical, []core.Point{pt(0, 0), pt(16, 16), pt(16, 0)}},
		{"both", tilemap.FlipHorizontal | tilemap.FlipVertical, []core.Point{pt(16, 0), pt(0, 16), pt(0, 0)}},
		{"rotate 90", tilemap.FlipDiagonal | tilemap.FlipHorizontal, []core.Point{pt(0, 0), pt(16, 16), pt(0, 16)}},
		{"rotate 270", tilemap.FlipDiagonal | tilemap.FlipVertical, []core.Point{pt(16, 16), pt(0, 0), pt(16, 0)}},
		{"diagonal", tilemap.FlipDiagonal, []core.Point{pt(16, 0), pt(0, 16), pt(16, 16)}},
		{"anti-diagonal", tilemap.FlipDiagonal | tilemap.FlipHorizontal | tilemap.FlipVertical, []core.Point{pt(0, 16), pt(16, 0), pt(0, 0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := tilemap.NewGridTileset(1, core.ImageSource{}, 16, 16, 1, 1)
			ts.Shapes = map[uint32][]tilemap.TileShape{0: {{Shape: tilemap.ObjectPolygon, Points: tri}}}
			m := tilemap.New(3, 3, 16, 16, ts)
			m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 3, 3, nil))
			m.SetTile(0, 1, 1, 1|tc.flags)

			if got := m.TileShapes(1 | tc.flags)[0].Points; !slices.Equal(got, tc.want) {
				t.Fatalf("TileShapes = %v, want %v", got, tc.want)
			}
			v := tileshape.New(m, tileshape.Options{})
			// The outline winds the triangle clockwise on screen.
			corners := slices.Clone(tc.want)
			if area := (corners[1].X-corners[0].X)*(corners[2].Y-corners[0].Y) - (corners[2].X-corners[0].X)*(corners[1].Y-corners[0].Y); area < 0 {
				slices.Reverse(corners)
			}
			var want []tileshape.Segment
			for i, p := range corners {
				q := corners[(i+1)%len(corners)]
				want = append(want, tileshape.Segment{A: pt(float64(p.X)+16, float64(p.Y)+16), B: pt(float64(q.X)+16, float64(q.Y)+16)})
			}
			if got := v.Outline(); !sameSegmentSet(got, want) {
				t.Fatalf("Outline = %v\nwant the triangle %v", got, want)
			}
		})
	}
}

func TestOnlyChosenLayersCollide(t *testing.T) {
	m := tilemap.New(3, 1, 8, 8)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("decor", 3, 1, []uint32{1, 0, 0}), tilemap.NewTileLayer("collision", 3, 1, []uint32{0, 1, 0}))
	m.SetFlags(1, tilemap.TileSolid)
	for _, tc := range []struct {
		opts tileshape.Options
		want []physics.TileFlags
	}{
		{tileshape.Options{}, []physics.TileFlags{physics.TileSolid, physics.TileSolid, 0}},
		{tileshape.Options{Layers: []int{1}}, []physics.TileFlags{0, physics.TileSolid, 0}},
		{tileshape.Options{LayerNames: []string{"decor"}}, []physics.TileFlags{physics.TileSolid, 0, 0}},
	} {
		v := tileshape.New(m, tc.opts)
		var got []physics.TileFlags
		for col := range 3 {
			got = append(got, v.TileFlagsAt(col, 0))
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%+v: flags %v, want %v", tc.opts, got, tc.want)
		}
		m.SetTile(0, 2, 0, 1) // decor
		after := v.TileFlagsAt(2, 0)
		m.SetTile(0, 2, 0, 0)
		decor := len(tc.opts.Layers) == 0 && len(tc.opts.LayerNames) == 0 || slices.Contains(tc.opts.LayerNames, "decor")
		if (after == physics.TileSolid) != decor {
			t.Errorf("%+v: TileFlagsAt(2, 0) = %v after a decor change", tc.opts, after)
		}
		if got := v.TileFlagsAt(2, 0); got != 0 {
			t.Errorf("%+v: TileFlagsAt(2, 0) = %v after clearing it", tc.opts, got)
		}
	}
}

func TestCellOverrideAndOneWayShapes(t *testing.T) {
	ts := tilemap.NewGridTileset(1, core.ImageSource{}, 16, 16, 2, 2)
	half := []core.Point{pt(0, 8), pt(16, 8), pt(16, 16), pt(0, 16)}
	ts.Shapes = map[uint32][]tilemap.TileShape{0: {{Shape: tilemap.ObjectRect, Points: half}}, 1: {{Shape: tilemap.ObjectRect, Points: half}}}
	m := tilemap.New(3, 1, 16, 16, ts)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 3, 1, []uint32{1, 2, 1}))
	m.SetFlags(2, tilemap.TileOneWay)
	v := tileshape.New(m, tileshape.Options{})

	// The solid half tiles give their top and sides, the one-way half tile only its top.
	want := []physics.Slope{
		{A: pt(0, 16), B: pt(0, 8)}, {A: pt(0, 8), B: pt(16, 8)}, {A: pt(16, 8), B: pt(16, 16)},
		{A: pt(16, 8), B: pt(32, 8)},
		{A: pt(32, 16), B: pt(32, 8)}, {A: pt(32, 8), B: pt(48, 8)}, {A: pt(48, 8), B: pt(48, 16)},
	}
	if got := v.Slopes(); !sameSlopes(got, want) {
		t.Fatalf("Slopes = %v\nwant %v", got, want)
	}
	if got := v.Boxes(); !slices.Equal(got, []physics.Box{{X: 0, Y: 8, W: 16, H: 8}, {X: 32, Y: 8, W: 16, H: 8}}) {
		t.Fatalf("Boxes = %v", got)
	}

	m.SetCellFlags(0, 0, tilemap.TileSolid)
	if got := v.TileFlagsAt(0, 0); got != physics.TileSolid {
		t.Fatalf("override: TileFlagsAt = %v", got)
	}
	if got := v.Slopes(); len(got) != 4 {
		t.Fatalf("override: Slopes = %v, want the first half tile gone", got)
	}
	m.SetCellFlags(2, 0, 0)
	if got := v.Slopes(); len(got) != 1 {
		t.Fatalf("empty override: Slopes = %v, want only the one-way top", got)
	}
	m.ClearCellFlags(0, 0)
	m.ClearCellFlags(2, 0)
	if got := v.Slopes(); !sameSlopes(got, want) {
		t.Fatalf("cleared: Slopes = %v", got)
	}
}

// TestIncrementalRebuildMatchesFreshView edits a map tile by tile, across chunk borders, and checks that the View that followed the edits agrees with one built from scratch.
func TestIncrementalRebuildMatchesFreshView(t *testing.T) {
	m := loadSlopes(t)
	big := tilemap.New(40, 36, 16, 16, m.Tilesets...)
	big.Layers = append(big.Layers, tilemap.NewTileLayer("ground", 40, 36, nil))
	big.SetFlags(1, tilemap.TileSolid)
	v := tileshape.New(big, tileshape.Options{})
	_ = v.Outline()

	gids := []uint32{0, 1, 2, 3, 4, 5, 6, 2 | tilemap.FlipHorizontal, 3 | tilemap.FlipVertical, 4 | tilemap.FlipDiagonal | tilemap.FlipHorizontal}
	rng := rand.New(rand.NewPCG(3, 5))
	for i := range 300 {
		col, row := 14+rng.IntN(6), 14+rng.IntN(6) // around the chunk corner at (16, 16)
		gid := gids[rng.IntN(len(gids))]
		if i%7 == 0 {
			big.FillRect(0, col, row, 2, 2, gid)
		} else {
			big.SetTile(0, col, row, gid)
		}
		if i%25 != 0 {
			continue
		}
		fresh := tileshape.New(big, tileshape.Options{})
		if !sameSegments(v.Outline(), fresh.Outline()) || !sameSlopes(v.Slopes(), fresh.Slopes()) || !slices.Equal(v.Boxes(), fresh.Boxes()) {
			t.Fatalf("edit %d: incremental geometry differs from a fresh View", i)
		}
		for row := range 36 {
			for col := range 40 {
				if v.TileFlagsAt(col, row) != fresh.TileFlagsAt(col, row) {
					t.Fatalf("edit %d: TileFlagsAt(%d, %d) differs", i, col, row)
				}
			}
		}
	}
}

func TestInvalidateAfterDirectWrites(t *testing.T) {
	m := tilemap.New(2, 1, 8, 8)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 2, 1, nil))
	m.SetFlags(1, tilemap.TileSolid)
	v := tileshape.New(m, tileshape.Options{})
	if v.TileFlagsAt(1, 0) != 0 {
		t.Fatal("empty cell is solid")
	}
	m.Layers[0].Data[1] = 1
	if v.TileFlagsAt(1, 0) != 0 {
		t.Fatal("a direct write was seen before Invalidate")
	}
	v.Invalidate(1, 0, 1, 1)
	if v.TileFlagsAt(1, 0) != physics.TileSolid {
		t.Fatal("Invalidate did not rebuild the cell")
	}
	m.SetTile(0, 0, 0, 1)
	if v.TileFlagsAt(0, 0) != physics.TileSolid {
		t.Fatal("SetTile was not followed")
	}
	m.Layers[0].Data[0] = 0
	v.InvalidateAll()
	if v.TileFlagsAt(0, 0) != 0 {
		t.Fatal("InvalidateAll did not rebuild")
	}

	// A layer appended after the View was built is seen on the next call.
	m.Layers = append(m.Layers, tilemap.NewTileLayer("late", 2, 1, []uint32{1, 0}))
	if v.TileFlagsAt(0, 0) != physics.TileSolid {
		t.Fatal("an added layer was not seen")
	}
	m.Layers = m.Layers[:1]

	// A map that grows is rebuilt at its new size.
	m.Width = 20
	m.Layers[0] = tilemap.NewTileLayer("ground", 20, 1, nil)
	m.Layers[0].Data[19] = 1
	if v.TileFlagsAt(19, 0) != physics.TileSolid {
		t.Fatal("a resized map was not rebuilt")
	}
	empty := tileshape.New(tilemap.New(0, 0, 0, 0), tileshape.Options{})
	if empty.TileFlagsAt(0, 0) != 0 || empty.Outline() != nil || empty.Occluders() != nil || empty.AppendSlopesIn(nil, core.Bounds{}) != nil {
		t.Fatal("an empty map has geometry")
	}
	if nilMap := tileshape.New(nil, tileshape.Options{Layers: []int{0}}); nilMap.TileFlagsAt(0, 0) != 0 || nilMap.Slopes() != nil || nilMap.Map() != nil {
		t.Fatal("a nil map has geometry")
	}
}

func TestAppendSlopesInCoversTheRegion(t *testing.T) {
	m := loadSlopes(t)
	v := tileshape.New(m, tileshape.Options{})
	all := v.Slopes()
	in := v.AppendSlopesIn(nil, core.Bounds{Origin: core.Point{X: 40, Y: 60}, Size: core.Size{Width: 12, Height: 16}})
	if !sameSlopes(in, all) {
		// The sample fits in one chunk, so the region's chunk holds every slope.
		t.Fatalf("AppendSlopesIn = %v, want %v", in, all)
	}
	if got := v.AppendSlopesIn(nil, core.Bounds{Origin: core.Point{X: -500, Y: -500}, Size: core.Size{Width: 10, Height: 10}}); got != nil {
		t.Fatalf("a region off the map gave %v", got)
	}
}

func pt(x, y float64) core.Point { return core.Point{X: core.Points(x), Y: core.Points(y)} }

func sameSlopes(a, b []physics.Slope) bool {
	key := func(s physics.Slope) string { return fmt.Sprint(s.A, s.B) }
	return sameKeys(a, b, key)
}

func sameSegmentSet(a, b []tileshape.Segment) bool {
	key := func(s tileshape.Segment) string { return fmt.Sprint(s.A, s.B) }
	return sameKeys(a, b, key)
}

// sameSegments reports whether a and b hold the same segments in the same order.
func sameSegments(a, b []tileshape.Segment) bool { return slices.Equal(a, b) }

func sameKeys[T any](a, b []T, key func(T) string) bool {
	ka, kb := make([]string, len(a)), make([]string, len(b))
	for i, s := range a {
		ka[i] = key(s)
	}
	for i, s := range b {
		kb[i] = key(s)
	}
	slices.SortFunc(ka, cmp.Compare)
	slices.SortFunc(kb, cmp.Compare)
	return slices.Equal(ka, kb)
}

// TestShapeEditsNeedInvalidateAll checks the contract the docs give: Invalidate rebuilds cells but keeps the geometry derived from each tile's shapes, so an edit to Tileset.Shapes needs InvalidateAll.
func TestShapeEditsNeedInvalidateAll(t *testing.T) {
	ts := tilemap.NewGridTileset(1, core.ImageSource{}, 16, 16, 1, 1)
	ts.Shapes = map[uint32][]tilemap.TileShape{0: {{Shape: tilemap.ObjectRect, Points: []core.Point{pt(0, 8), pt(16, 8), pt(16, 16), pt(0, 16)}}}}
	m := tilemap.New(2, 1, 16, 16, ts)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 2, 1, []uint32{1, 0}))
	v := tileshape.New(m, tileshape.Options{})
	if got := v.Boxes(); !slices.Equal(got, []physics.Box{{X: 0, Y: 8, W: 16, H: 8}}) {
		t.Fatalf("Boxes = %v", got)
	}
	ts.Shapes[0] = []tilemap.TileShape{{Shape: tilemap.ObjectRect, Points: []core.Point{pt(0, 12), pt(16, 12), pt(16, 16), pt(0, 16)}}}
	v.InvalidateAll()
	if got := v.Boxes(); !slices.Equal(got, []physics.Box{{X: 0, Y: 12, W: 16, H: 4}}) {
		t.Fatalf("after InvalidateAll: Boxes = %v", got)
	}
}

func TestUnboundedMapHasNoCells(t *testing.T) {
	m := tilemap.NewInfinite(16, 16)
	m.Layers = []tilemap.TileLayer{tilemap.NewChunkedTileLayer("world", tilemap.DefaultChunkSize)}
	m.SetFlags(1, tilemap.TileSolid)
	m.SetTile(0, 2, 3, 1)
	m.Width, m.Height = 8, 8
	v := tileshape.New(m, tileshape.Options{})
	if v.TileFlagsAt(2, 3) != 0 || v.Outline() != nil || v.Occluders() != nil || v.Slopes() != nil || v.Boxes() != nil {
		t.Fatal("a View of an unbounded map has cells")
	}

	// A chunked layer in a map without Infinite set is unbounded too.
	m.Infinite = false
	if v.TileFlagsAt(2, 3) != 0 || v.Outline() != nil {
		t.Fatal("a View of a map with a chunked layer has cells")
	}
}

func TestPolylineIsAnOpenSlope(t *testing.T) {
	ts := tilemap.NewGridTileset(1, core.ImageSource{}, 16, 16, 1, 1)
	ts.Shapes = map[uint32][]tilemap.TileShape{0: {{Shape: tilemap.ObjectPolyline, Points: []core.Point{pt(0, 2), pt(16, 2)}}}}
	m := tilemap.New(1, 1, 16, 16, ts)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 1, 1, []uint32{1}))
	m.SetFlags(1, tilemap.TileSolid)
	for _, shapedSolid := range []bool{false, true} {
		v := tileshape.New(m, tileshape.Options{ShapedSolid: shapedSolid})
		if got := v.TileFlagsAt(0, 0); got != 0 {
			t.Errorf("ShapedSolid %v: TileFlagsAt = %v, want 0", shapedSolid, got)
		}
		if got := v.Outline(); got != nil {
			t.Errorf("ShapedSolid %v: Outline = %v, want none", shapedSolid, got)
		}
		if got := v.Occluders(); got != nil {
			t.Errorf("ShapedSolid %v: Occluders = %v, want none", shapedSolid, got)
		}
		if got := v.Slopes(); !sameSlopes(got, []physics.Slope{{A: pt(0, 2), B: pt(16, 2)}}) {
			t.Errorf("ShapedSolid %v: Slopes = %v", shapedSolid, got)
		}
	}
}

// TestTallTileMeetsCellsAcrossAChunk places a tile two cells tall, whose shape reaches into the row above its cell, under a solid cell in the chunk above, and checks that the edge they share is removed and comes back when the cell is cleared.
func TestTallTileMeetsCellsAcrossAChunk(t *testing.T) {
	ts := tilemap.NewGridTileset(1, core.ImageSource{}, 16, 32, 1, 1)
	ts.Shapes = map[uint32][]tilemap.TileShape{0: {{Shape: tilemap.ObjectRect, Points: []core.Point{pt(2, 0), pt(14, 0), pt(14, 32), pt(2, 32)}}}}
	m := tilemap.New(8, 20, 16, 16, ts)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", 8, 20, nil))
	m.SetFlags(2, tilemap.TileSolid)
	m.SetTile(0, 5, 17, 1) // spans rows 16 and 17, in the second chunk row
	m.SetTile(0, 5, 15, 2) // a solid cell in the first chunk row, on top of it
	v := tileshape.New(m, tileshape.Options{})

	// The tall tile's top, y = 256 from x = 82 to 94, lies inside the solid cell's bottom.
	onSeam := func(segs []tileshape.Segment) bool {
		for _, s := range segs {
			if s.A.Y == 256 && s.B.Y == 256 && min(s.A.X, s.B.X) < 94 && max(s.A.X, s.B.X) > 82 {
				return true
			}
		}
		return false
	}
	seamCount := func(segs []tileshape.Segment) int {
		n := 0
		for _, s := range segs {
			if s.A.Y == 256 && s.B.Y == 256 {
				n++
			}
		}
		return n
	}
	out := v.Outline()
	// The cell's bottom is cut around the tile's top into two pieces, and the tile's top is gone.
	if seamCount(out) != 2 || onSeam(out) {
		t.Fatalf("Outline has an internal edge at the seam: %v", out)
	}
	m.SetTile(0, 5, 15, 0)
	out = v.Outline()
	if !onSeam(out) {
		t.Fatalf("Outline lost the tall tile's top after the cell above it was cleared: %v", out)
	}
	if fresh := tileshape.New(m, tileshape.Options{}); !sameSegments(out, fresh.Outline()) {
		t.Fatal("incremental outline differs from a fresh View")
	}
	m.SetTile(0, 5, 15, 2)
	if fresh := tileshape.New(m, tileshape.Options{}); !sameSegments(v.Outline(), fresh.Outline()) || onSeam(v.Outline()) {
		t.Fatal("incremental outline differs from a fresh View after restoring the cell")
	}
}

// TestOccluderCastsTheShadowsOfShapes fills a map with the slope, half and block tiles of slopes.tmj, flipped every way, and checks that the derived occluders light every sample point exactly as a polygon occluder per placed shape and a rectangle per solid cell do, on load and after SetTile edits.
func TestOccluderCastsTheShadowsOfShapes(t *testing.T) {
	const cols, rows, size = 24, 18, 16
	src := loadSlopes(t)
	m := tilemap.New(cols, rows, size, size, src.Tilesets...)
	m.Layers = append(m.Layers, tilemap.NewTileLayer("ground", cols, rows, nil))
	m.SetFlags(1, tilemap.TileSolid)
	flips := []uint32{0, tilemap.FlipHorizontal, tilemap.FlipVertical, tilemap.FlipDiagonal, tilemap.FlipHorizontal | tilemap.FlipVertical, tilemap.FlipDiagonal | tilemap.FlipHorizontal, tilemap.FlipDiagonal | tilemap.FlipVertical, tilemap.FlipMask}
	rng := rand.New(rand.NewPCG(13, 17))
	randomGID := func() uint32 {
		if rng.IntN(3) == 0 {
			return 0
		}
		return uint32(1+rng.IntN(6)) | flips[rng.IntN(len(flips))]
	}
	for row := range rows {
		for col := range cols {
			m.SetTile(0, col, row, randomGID())
		}
	}
	lights := []core.Point{{X: 100.3, Y: 77.7}, {X: 251.1, Y: 203.9}, {X: 13.37, Y: 270.2}}
	clearLights := func() {
		for _, l := range lights {
			m.SetTile(0, int(l.X)/size, int(l.Y)/size, 0)
		}
	}
	clearLights()
	v := tileshape.New(m, tileshape.Options{})

	check := func(label string) {
		t.Helper()
		var ref []lights2d.Occluder
		shaped := 0
		for row := range rows {
			for col := range cols {
				gid := m.TileAt(0, col, row)
				ox, oy := float32(col*size), float32(row*size)
				if shapes := m.TileShapes(gid); len(shapes) > 0 {
					shaped++
					for _, s := range shapes {
						pts := make([]core.Point, len(s.Points))
						for i, p := range s.Points {
							pts[i] = core.Point{X: p.X + core.Points(ox), Y: p.Y + core.Points(oy)}
						}
						ref = append(ref, lights2d.PolygonOccluder(pts...))
					}
				} else if m.FlagsAt(col, row)&tilemap.TileSolid != 0 {
					ref = append(ref, lights2d.RectOccluder(core.Bounds{Origin: core.Point{X: core.Points(ox), Y: core.Points(oy)}, Size: core.Size{Width: size, Height: size}}))
				}
			}
		}
		if shaped < 100 {
			t.Fatalf("%s: only %d shaped cells", label, shaped)
		}
		derived := v.Occluders()
		differ, samples := 0, 0
		for _, l := range lights {
			for y := 3.1; y < rows*size; y += 7.3 {
				for x := 2.9; x < cols*size; x += 7.7 {
					p := core.Point{X: core.Points(x), Y: core.Points(y)}
					samples++
					if lights2d.Visible(l, p, ref) != lights2d.Visible(l, p, derived) {
						differ++
					}
				}
			}
		}
		if differ != 0 {
			t.Fatalf("%s: %d of %d sample points lit differently from per-shape polygons", label, differ, samples)
		}
	}
	check("loaded")
	for range 40 {
		m.SetTile(0, rng.IntN(cols), rng.IntN(rows), randomGID())
	}
	clearLights()
	check("after SetTile")
}

// TestViewHonoursTheMapOrigin moves slopes.tmj to a world origin and checks that every derived segment moves with it and that physics finds the floor where the map draws it.
func TestViewHonoursTheMapOrigin(t *testing.T) {
	at := loadSlopes(t)
	base := tileshape.New(loadSlopes(t), tileshape.Options{})
	const dx, dy = 100, -40
	at.Origin = core.Point{X: dx, Y: dy}
	v := tileshape.New(at, tileshape.Options{})
	var og physics.TileGridOrigin = v
	if x, y := og.GridOrigin(); x != dx || y != dy {
		t.Fatalf("GridOrigin = %v, %v", x, y)
	}
	move := func(p core.Point) core.Point { return core.Point{X: p.X + dx, Y: p.Y + dy} }
	var want []tileshape.Segment
	for _, s := range base.Outline() {
		want = append(want, tileshape.Segment{A: move(s.A), B: move(s.B)})
	}
	if !sameSegments(v.Outline(), want) {
		t.Fatalf("Outline = %v\nwant the base outline moved by the origin", v.Outline())
	}
	var slopes []physics.Slope
	for _, s := range base.Slopes() {
		slopes = append(slopes, physics.Slope{A: move(s.A), B: move(s.B)})
	}
	if !sameSlopes(v.Slopes(), slopes) {
		t.Fatalf("Slopes = %v\nwant %v", v.Slopes(), slopes)
	}
	for i, b := range base.Boxes() {
		if got := v.Boxes()[i]; got != (physics.Box{X: b.X + dx, Y: b.Y + dy, W: b.W, H: b.H}) {
			t.Fatalf("Boxes[%d] = %v, base %v", i, got, b)
		}
	}
	if in := v.AppendSlopesIn(nil, core.Bounds{Origin: core.Point{X: 40 + dx, Y: 60 + dy}, Size: core.Size{Width: 12, Height: 16}}); len(in) == 0 {
		t.Fatal("AppendSlopesIn found nothing around a moved slope")
	}

	b := physics.NewBoxBody(1+dx, 10+dy, 12, 16)
	for range 30 {
		b.VY += 900 * float32(step.Seconds())
		physics.MoveAndSlide(b, v, step)
	}
	if !b.Grounded || b.Bounds().Bottom() != 96+dy {
		t.Fatalf("MoveAndSlide: grounded %v, bottom %v, want on the floor at %v", b.Grounded, b.Bounds().Bottom(), 96+dy)
	}
}

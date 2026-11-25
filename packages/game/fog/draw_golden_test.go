package fog_test

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/fog"
	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// makeDiamondTileImage builds a 2-tile sprite sheet (floor + wall) of crisp diamond sprites.
func makeDiamondTileImage(tw, th int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, tw*2, th))
	drawDiamond := func(xOff int, fill, border color.RGBA) {
		hw, hh := float64(tw)/2, float64(th)/2
		for y := range th {
			dy := math.Abs(float64(y)+0.5-hh) / hh
			for x := range tw {
				dx := math.Abs(float64(x)+0.5-hw) / hw
				d := dx + dy
				if d <= 1.0 {
					c := fill
					if d > 0.86 {
						c = border
					}
					img.SetRGBA(xOff+x, y, c)
				}
			}
		}
	}
	drawDiamond(0, color.RGBA{R: 92, G: 184, B: 112, A: 255}, color.RGBA{R: 52, G: 120, B: 68, A: 255})
	drawDiamond(tw, color.RGBA{R: 180, G: 132, B: 88, A: 255}, color.RGBA{R: 112, G: 78, B: 48, A: 255})
	return img
}

// makeHexTileImage builds a 2-tile sprite sheet (floor + wall) of crisp pointy-top or flat-top hexagons matching tilemap.Tilemap's projection.
func makeHexTileImage(tw, th, side int, flatTop bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, tw*2, th))
	insideHex := func(px, py float64) (inside, edge bool) {
		hw, hh := float64(tw)/2, float64(th)/2
		dx := math.Abs(px - hw)
		dy := math.Abs(py - hh)
		if !flatTop {
			// Pointy-top: |x| <= hw and shoulder from y = hh - side/2 to y = hh.
			if dx > hw || dy > hh {
				return false, false
			}
			hs := float64(side) / 2
			slopeH := hh - hs
			if dy <= hs {
				return true, dx >= hw-1.5
			}
			maxX := hw * (1 - (dy-hs)/slopeH)
			return dx <= maxX, dx >= maxX-1.5
		}
		// Flat-top: |y| <= hh and shoulder from x = hw - side/2 to x = hw.
		if dx > hw || dy > hh {
			return false, false
		}
		hs := float64(side) / 2
		slopeW := hw - hs
		if dx <= hs {
			return true, dy >= hh-1.5
		}
		maxY := hh * (1 - (dx-hs)/slopeW)
		return dy <= maxY, dy >= maxY-1.5
	}
	drawHex := func(xOff int, fill, border color.RGBA) {
		for y := range th {
			for x := range tw {
				in, edge := insideHex(float64(x)+0.5, float64(y)+0.5)
				if in {
					c := fill
					if edge {
						c = border
					}
					img.SetRGBA(xOff+x, y, c)
				}
			}
		}
	}
	drawHex(0, color.RGBA{R: 84, G: 176, B: 196, A: 255}, color.RGBA{R: 46, G: 112, B: 132, A: 255})
	drawHex(tw, color.RGBA{R: 196, G: 124, B: 84, A: 255}, color.RGBA{R: 128, G: 72, B: 46, A: 255})
	return img
}

func assertFogAlignmentAndGolden(t *testing.T, name string, m *tilemap.Tilemap, f *fog.Fog, padX, padY float32, floorRGB, wallRGB color.RGBA) {
	t.Helper()
	wb := m.WorldBounds()
	w := int(math.Ceil(float64(wb.Size.Width))) + int(padX*2)
	h := int(math.Ceil(float64(wb.Size.Height))) + int(padY*2)
	tx := padX - float32(wb.Origin.X)
	ty := padY - float32(wb.Origin.Y)

	bg := color.RGBA{R: 28, G: 36, B: 52, A: 255}
	var d core.Drawing
	d.Save()
	d.Translate(tx, ty)
	m.Draw(&d, nil)
	f.Draw(&d, blue, nil, fog.DefaultStyle())
	d.Restore()

	img := gametest.RenderOps(w, h, d.Ops, gametest.RenderOptions{Background: bg})
	gametest.AssertGolden(t, name, img)

	// Verify the rendered pixel at every tile center matches its exact fog state (Visible, Explored, Unseen).
	for row := range m.Height {
		for col := range m.Width {
			center := m.TileToWorld(col, row)
			px := int(math.Floor(float64(center.X) + float64(tx)))
			py := int(math.Floor(float64(center.Y) + float64(ty)))
			got := img.RGBAAt(px, py)
			base := floorRGB
			if f.Opaque(col, row) {
				base = wallRGB
			}
			switch f.State(blue, col, row) {
			case fog.Visible:
				if got != base {
					t.Fatalf("%s visible cell (%d,%d) pixel (%d,%d) = %+v, want %+v", name, col, row, px, py, got, base)
				}
			case fog.Explored:
				// DefaultStyle Explored alpha is 0.55 black over base.
				alpha := math.Round(0.55*255) / 255
				inv := 1 - alpha
				want := color.RGBA{
					R: uint8(math.Round(float64(base.R) * inv)),
					G: uint8(math.Round(float64(base.G) * inv)),
					B: uint8(math.Round(float64(base.B) * inv)),
					A: 255,
				}
				if absDiffU8(got.R, want.R) > 1 || absDiffU8(got.G, want.G) > 1 || absDiffU8(got.B, want.B) > 1 || got.A != 255 {
					t.Fatalf("%s explored cell (%d,%d) pixel (%d,%d) = %+v, want ~%+v", name, col, row, px, py, got, want)
				}
			case fog.Unseen:
				want := color.RGBA{R: 0, G: 0, B: 0, A: 255}
				if got != want {
					t.Fatalf("%s unseen cell (%d,%d) pixel (%d,%d) = %+v, want %+v", name, col, row, px, py, got, want)
				}
			}
		}
	}

	// Corner pixel outside the map silhouette must stay untouched backdrop (proves fog polygons do not spill as bounding boxes).
	if got := img.RGBAAt(1, 1); got != bg {
		t.Fatalf("%s exterior corner pixel (1,1) = %+v, want backdrop %+v", name, got, bg)
	}
}

func absDiffU8(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func TestFogDrawGoldenHexPointy(t *testing.T) {
	sheet := makeHexTileImage(32, 36, 18, false)
	ts := tilemap.NewGridTileset(1, core.ImageSource{Pixels: sheet, Version: 1}, 32, 36, 2, 2)
	m := tilemap.New(6, 5, 32, 36, ts)
	m.Orientation = tilemap.OrientationHexagonal
	m.StaggerAxis = tilemap.StaggerY
	m.StaggerIndex = tilemap.StaggerOdd
	m.HexSideLength = 18
	m.SetFlags(2, tilemap.TileSolid)

	data := make([]uint32, 30)
	for i := range data {
		data[i] = 1
	}
	data[2*6+3] = 2 // wall at (3, 2)
	data[1*6+3] = 2 // wall at (3, 1)
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("ground", 6, 5, data)}

	f := fog.NewFromTilemap(m, tilemap.TileSolid)
	scout := f.AddSource(fog.Source{Team: blue, Col: 1, Row: 1, Radius: 1})
	f.Update()
	f.Move(scout, 2, 2)
	f.Update()

	assertFogAlignmentAndGolden(t, "fog_hex_pointy", m, f, 8, 8,
		color.RGBA{R: 84, G: 176, B: 196, A: 255},
		color.RGBA{R: 196, G: 124, B: 84, A: 255},
	)
}

func TestFogDrawGoldenHexFlat(t *testing.T) {
	sheet := makeHexTileImage(36, 32, 18, true)
	ts := tilemap.NewGridTileset(1, core.ImageSource{Pixels: sheet, Version: 1}, 36, 32, 2, 2)
	m := tilemap.New(6, 5, 36, 32, ts)
	m.Orientation = tilemap.OrientationHexagonal
	m.StaggerAxis = tilemap.StaggerX
	m.StaggerIndex = tilemap.StaggerOdd
	m.HexSideLength = 18
	m.SetFlags(2, tilemap.TileSolid)

	data := make([]uint32, 30)
	for i := range data {
		data[i] = 1
	}
	data[2*6+3] = 2
	data[3*6+3] = 2
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("ground", 6, 5, data)}

	f := fog.NewFromTilemap(m, tilemap.TileSolid)
	scout := f.AddSource(fog.Source{Team: blue, Col: 1, Row: 1, Radius: 1})
	f.Update()
	f.Move(scout, 2, 2)
	f.Update()

	assertFogAlignmentAndGolden(t, "fog_hex_flat", m, f, 8, 8,
		color.RGBA{R: 84, G: 176, B: 196, A: 255},
		color.RGBA{R: 196, G: 124, B: 84, A: 255},
	)
}

func TestFogDrawGoldenIsometric(t *testing.T) {
	sheet := makeDiamondTileImage(48, 24)
	ts := tilemap.NewGridTileset(1, core.ImageSource{Pixels: sheet, Version: 1}, 48, 24, 2, 2)
	m := tilemap.New(5, 5, 48, 24, ts)
	m.Orientation = tilemap.OrientationIsometric
	m.SetFlags(2, tilemap.TileSolid)

	data := make([]uint32, 25)
	for i := range data {
		data[i] = 1
	}
	data[2*5+3] = 2
	data[1*5+3] = 2
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("ground", 5, 5, data)}

	f := fog.NewFromTilemap(m, tilemap.TileSolid)
	scout := f.AddSource(fog.Source{Team: blue, Col: 1, Row: 1, Radius: 1})
	f.Update()
	f.Move(scout, 2, 2)
	f.Update()

	assertFogAlignmentAndGolden(t, "fog_isometric", m, f, 8, 8,
		color.RGBA{R: 92, G: 184, B: 112, A: 255},
		color.RGBA{R: 180, G: 132, B: 88, A: 255},
	)
}

func TestFogDrawGoldenStaggered(t *testing.T) {
	sheet := makeDiamondTileImage(48, 24)
	ts := tilemap.NewGridTileset(1, core.ImageSource{Pixels: sheet, Version: 1}, 48, 24, 2, 2)
	m := tilemap.New(5, 6, 48, 24, ts)
	m.Orientation = tilemap.OrientationStaggered
	m.StaggerAxis = tilemap.StaggerY
	m.StaggerIndex = tilemap.StaggerOdd
	m.SetFlags(2, tilemap.TileSolid)

	data := make([]uint32, 30)
	for i := range data {
		data[i] = 1
	}
	data[3*5+3] = 2
	data[2*5+3] = 2
	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("ground", 5, 6, data)}

	f := fog.NewFromTilemap(m, tilemap.TileSolid)
	scout := f.AddSource(fog.Source{Team: blue, Col: 1, Row: 1, Radius: 1})
	f.Update()
	f.Move(scout, 2, 3)
	f.Update()

	assertFogAlignmentAndGolden(t, "fog_staggered", m, f, 8, 8,
		color.RGBA{R: 92, G: 184, B: 112, A: 255},
		color.RGBA{R: 180, G: 132, B: 88, A: 255},
	)
}

func TestFogDrawProjectedCameraCulling(t *testing.T) {
	for _, tc := range []struct {
		name        string
		orientation tilemap.Orientation
		axis        tilemap.StaggerAxis
		tw, th      float32
	}{
		{"hex_pointy", tilemap.OrientationHexagonal, tilemap.StaggerY, 32, 36},
		{"hex_flat", tilemap.OrientationHexagonal, tilemap.StaggerX, 36, 32},
		{"isometric", tilemap.OrientationIsometric, tilemap.StaggerY, 48, 24},
		{"staggered", tilemap.OrientationStaggered, tilemap.StaggerY, 48, 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tilemap.New(20, 20, tc.tw, tc.th)
			m.Orientation = tc.orientation
			m.StaggerAxis = tc.axis
			f := fog.NewFromTilemap(m, 0)

			var full, culled core.Drawing
			f.Draw(&full, blue, nil, fog.DefaultStyle())

			center := m.TileToWorld(10, 10)
			cam := &tilemap.Camera{X: float32(center.X), Y: float32(center.Y), Zoom: 1, Width: tc.tw * 2, Height: tc.th * 2}
			f.Draw(&culled, blue, cam, fog.DefaultStyle())

			countSubpaths := func(ops []core.DrawOp) int {
				n := 0
				for _, op := range ops {
					if op.Kind == core.DrawClosePath {
						n++
					}
				}
				return n
			}
			fullN := countSubpaths(full.Ops)
			culledN := countSubpaths(culled.Ops)
			if fullN != 400 {
				t.Fatalf("full drew %d tile polygons, want 400", fullN)
			}
			if culledN == 0 || culledN >= 40 {
				t.Fatalf("culled drew %d tile polygons, want >0 and <40 (from 400)", culledN)
			}
		})
	}
}

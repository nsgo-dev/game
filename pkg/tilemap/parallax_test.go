package tilemap_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"math"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func TestTiledParallaxZeroLoadDrawAndGroupMultiply(t *testing.T) {
	tmj := []byte(`{
		"width": 2, "height": 1, "tilewidth": 16, "tileheight": 16,
		"tilesets": [
			{"firstgid": 1, "name": "t", "image": "tiles.png", "tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 4, "objectalignment": "topleft"}
		],
		"layers": [
			{"name": "tile_fixed", "type": "tilelayer", "width": 2, "height": 1, "data": [1, 0], "parallaxx": 0, "parallaxy": 0},
			{"name": "tile_x_fixed", "type": "tilelayer", "width": 2, "height": 1, "data": [2, 0], "parallaxx": 0},
			{"name": "tile_half", "type": "tilelayer", "width": 2, "height": 1, "data": [1, 0], "parallaxx": 0.5, "parallaxy": 0.25},
			{"name": "tile_default", "type": "tilelayer", "width": 2, "height": 1, "data": [1, 0]},
			{"name": "sky", "type": "imagelayer", "image": "sky.png", "imagewidth": 64, "imageheight": 32, "parallaxx": 0, "parallaxy": 0},
			{"name": "hud_objs", "type": "objectgroup", "parallaxx": 0, "parallaxy": 0, "objects": [
				{"id": 1, "gid": 1, "x": 10, "y": 5, "width": 16, "height": 16, "visible": true}
			]},
			{
				"name": "grp_zero", "type": "group", "parallaxx": 0, "parallaxy": 1,
				"layers": [
					{"name": "grp_child_scaled", "type": "imagelayer", "image": "clouds.png", "imagewidth": 32, "imageheight": 16, "parallaxx": 2, "parallaxy": 0}
				]
			},
			{
				"name": "grp_neg", "type": "group", "parallaxx": -1, "parallaxy": 0.5,
				"layers": [
					{"name": "grp_child_zero", "type": "tilelayer", "width": 2, "height": 1, "data": [1, 0], "parallaxx": 0, "parallaxy": 0}
				]
			}
		]
	}`)

	tmx := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<map version="1.10" orientation="orthogonal" width="2" height="1" tilewidth="16" tileheight="16">
 <tileset firstgid="1" name="t" tilewidth="16" tileheight="16" tilecount="4" columns="2" objectalignment="topleft">
  <image source="tiles.png" width="32" height="32"/>
 </tileset>
 <layer name="tile_fixed" width="2" height="1" parallaxx="0" parallaxy="0">
  <data encoding="csv">1,0</data>
 </layer>
 <layer name="tile_x_fixed" width="2" height="1" parallaxx="0">
  <data encoding="csv">2,0</data>
 </layer>
 <layer name="tile_half" width="2" height="1" parallaxx="0.5" parallaxy="0.25">
  <data encoding="csv">1,0</data>
 </layer>
 <layer name="tile_default" width="2" height="1">
  <data encoding="csv">1,0</data>
 </layer>
 <imagelayer name="sky" parallaxx="0" parallaxy="0">
  <image source="sky.png" width="64" height="32"/>
 </imagelayer>
 <objectgroup name="hud_objs" parallaxx="0" parallaxy="0">
  <object id="1" gid="1" x="10" y="5" width="16" height="16" visible="1"/>
 </objectgroup>
 <group name="grp_zero" parallaxx="0" parallaxy="1">
  <imagelayer name="grp_child_scaled" parallaxx="2" parallaxy="0">
   <image source="clouds.png" width="32" height="16"/>
  </imagelayer>
 </group>
 <group name="grp_neg" parallaxx="-1" parallaxy="0.5">
  <layer name="grp_child_zero" width="2" height="1" parallaxx="0" parallaxy="0">
   <data encoding="csv">1,0</data>
  </layer>
 </group>
</map>`)

	loader := tilemap.TilesetLoader{
		LoadImage: func(p string) (core.ImageSource, error) {
			return core.ImageSource{URI: "asset:" + p}, nil
		},
	}

	mTMJ, err := tilemap.LoadTiled(tmj, loader)
	if err != nil {
		t.Fatalf("LoadTiled TMJ: %v", err)
	}
	mTMX, err := tilemap.LoadTiled(tmx, loader)
	if err != nil {
		t.Fatalf("LoadTiled TMX: %v", err)
	}
	if !reflect.DeepEqual(mTMX, mTMJ) {
		t.Fatalf("TMX and TMJ loaded differently:\nTMX=%+v\nTMJ=%+v", mTMX, mTMJ)
	}

	// Verify loaded parallax fields and HasParallax flags (and no negative zero from -1 * 0).
	checkTileParallax := func(name string, wantPX, wantPY float32) {
		t.Helper()
		l := mTMJ.Layer(name)
		if l == nil {
			t.Fatalf("missing tile layer %q", name)
		}
		if !l.HasParallax || l.ParallaxX != wantPX || l.ParallaxY != wantPY || math.Signbit(float64(l.ParallaxX)) || math.Signbit(float64(l.ParallaxY)) {
			t.Fatalf("tile layer %q parallax = (%g, %g, has=%v), want (%g, %g, true)", name, l.ParallaxX, l.ParallaxY, l.HasParallax, wantPX, wantPY)
		}
	}
	checkTileParallax("tile_fixed", 0, 0)
	checkTileParallax("tile_x_fixed", 0, 1)
	checkTileParallax("tile_half", 0.5, 0.25)
	checkTileParallax("tile_default", 1, 1)
	checkTileParallax("grp_child_zero", 0, 0)

	sky := mTMJ.ImageLayer("sky")
	if sky == nil || !sky.HasParallax || sky.ParallaxX != 0 || sky.ParallaxY != 0 {
		t.Fatalf("sky parallax = %+v, want (0, 0, has=true)", sky)
	}
	clouds := mTMJ.ImageLayer("grp_child_scaled")
	if clouds == nil || !clouds.HasParallax || clouds.ParallaxX != 0 || clouds.ParallaxY != 0 {
		t.Fatalf("grp_child_scaled parallax = %+v, want (0, 0, has=true)", clouds)
	}
	hud := mTMJ.ObjectLayer("hud_objs")
	if hud == nil || !hud.HasParallax || hud.ParallaxX != 0 || hud.ParallaxY != 0 {
		t.Fatalf("hud_objs parallax = %+v, want (0, 0, has=true)", hud)
	}

	// Move camera far away to (100, 40) with a viewport covering [0..200, -10..90].
	cam := &tilemap.Camera{X: 100, Y: 40, Zoom: 1, Width: 200, Height: 100}

	drawOneTileLayer := func(name string) core.DrawOp {
		t.Helper()
		var d core.Drawing
		mTMJ.DrawLayer(&d, mTMJ.Layer(name), cam)
		if len(d.Ops) != 1 || d.Ops[0].Kind != core.DrawImage {
			t.Fatalf("DrawLayer(%q) ops = %+v, want 1 DrawImage", name, d.Ops)
		}
		return d.Ops[0]
	}

	// Parallax 0 shifts world draw position by +cam so after cam.Apply (-cam) it stays fixed on screen.
	if op := drawOneTileLayer("tile_fixed"); op.X != 100 || op.Y != 40 {
		t.Fatalf("tile_fixed drew at (%g, %g), want (100, 40)", op.X, op.Y)
	}
	if op := drawOneTileLayer("tile_x_fixed"); op.X != 100 || op.Y != 0 {
		t.Fatalf("tile_x_fixed drew at (%g, %g), want (100, 0)", op.X, op.Y)
	}
	if op := drawOneTileLayer("tile_half"); op.X != 50 || op.Y != 30 {
		t.Fatalf("tile_half drew at (%g, %g), want (50, 30)", op.X, op.Y)
	}
	if op := drawOneTileLayer("tile_default"); op.X != 0 || op.Y != 0 {
		t.Fatalf("tile_default drew at (%g, %g), want (0, 0)", op.X, op.Y)
	}
	if op := drawOneTileLayer("grp_child_zero"); op.X != 100 || op.Y != 40 {
		t.Fatalf("grp_child_zero drew at (%g, %g), want (100, 40)", op.X, op.Y)
	}

	// Image layers with direct 0 and group-multiplied 0.
	var d core.Drawing
	mTMJ.DrawImageLayer(&d, sky, cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 100 || d.Ops[0].Y != 40 {
		t.Fatalf("sky DrawImageLayer ops = %+v, want (100, 40)", d.Ops)
	}
	d.Ops = d.Ops[:0]
	mTMJ.DrawImageLayer(&d, clouds, cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 100 || d.Ops[0].Y != 40 {
		t.Fatalf("clouds (group 0 * 2) DrawImageLayer ops = %+v, want (100, 40)", d.Ops)
	}

	// Object layer with parallax 0: obj at (10, 5) + cam (100, 40) = (110, 45).
	d.Ops = d.Ops[:0]
	mTMJ.DrawObjectLayer(&d, hud, cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 110 || d.Ops[0].Y != 45 {
		t.Fatalf("hud_objs DrawObjectLayer ops = %+v, want (110, 45)", d.Ops)
	}

	// Y-sorted tile layer with parallax 0 also honors 0.
	mTMJ.Layer("tile_fixed").YSort = true
	d.Ops = d.Ops[:0]
	mTMJ.DrawLayer(&d, mTMJ.Layer("tile_fixed"), cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 100 || d.Ops[0].Y != 40 {
		t.Fatalf("YSort tile_fixed drew at %+v, want (100, 40)", d.Ops)
	}
}

func TestParallaxZeroChunkedAndCachedLayers(t *testing.T) {
	sheet := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			sheet.SetRGBA(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	imgSrc := core.ImageSource{URI: "asset:sheet.png"}
	images := map[string]image.Image{"asset:sheet.png": sheet}
	ts := tilemap.NewGridTileset(1, imgSrc, 16, 16, 1, 1)

	// Dense cached layer with parallax 0.
	denseMap := tilemap.New(1, 1, 16, 16, ts)
	dl := tilemap.NewTileLayer("bg", 1, 1, []uint32{1})
	dl.ParallaxX = 0
	dl.ParallaxY = 0
	denseMap.Layers = []tilemap.TileLayer{dl}
	if _, _, err := denseMap.CacheStaticLayer(0, images); err != nil {
		t.Fatalf("CacheStaticLayer: %v", err)
	}

	cam := &tilemap.Camera{X: 120, Y: 60, Zoom: 1, Width: 200, Height: 120}
	var d core.Drawing
	denseMap.DrawLayer(&d, &denseMap.Layers[0], cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 120 || d.Ops[0].Y != 60 {
		t.Fatalf("cached dense layer with parallax 0 drew at %+v, want (120, 60)", d.Ops)
	}

	// Sparse chunked layer with parallax 0.
	infMap := tilemap.NewInfinite(16, 16, ts)
	cl := tilemap.NewChunkedTileLayer("inf_bg", 16)
	cl.ParallaxX = 0
	cl.ParallaxY = 0
	infMap.Layers = []tilemap.TileLayer{cl}
	infMap.SetTile(0, 0, 0, 1)

	d.Ops = d.Ops[:0]
	infMap.DrawLayer(&d, &infMap.Layers[0], cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 120 || d.Ops[0].Y != 60 {
		t.Fatalf("chunked layer with parallax 0 drew at %+v, want (120, 60)", d.Ops)
	}
}

func TestParallaxConstructorsAndStructLiterals(t *testing.T) {
	img := core.ImageSource{URI: "asset:t.png"}
	ts := tilemap.NewGridTileset(1, img, 16, 16, 1, 1)
	ts.ObjectAlignment = tilemap.ObjectAlignmentTopLeft
	cam := &tilemap.Camera{X: 100, Y: 50, Zoom: 1, Width: 300, Height: 200}

	// Constructors default to (1, 1) with HasParallax: true, so setting one axis to 0 locks only that axis.
	tl := tilemap.NewTileLayer("t", 1, 1, []uint32{1})
	if tl.ParallaxX != 1 || tl.ParallaxY != 1 || !tl.HasParallax {
		t.Fatalf("NewTileLayer parallax = (%g, %g, %v), want (1, 1, true)", tl.ParallaxX, tl.ParallaxY, tl.HasParallax)
	}
	tl.ParallaxX = 0

	il := tilemap.NewImageLayer("i", img)
	if il.ParallaxX != 1 || il.ParallaxY != 1 || !il.HasParallax {
		t.Fatalf("NewImageLayer parallax = (%g, %g, %v), want (1, 1, true)", il.ParallaxX, il.ParallaxY, il.HasParallax)
	}
	il.ParallaxX = 0
	il.ParallaxY = 0

	ol := tilemap.NewObjectLayer("o", tilemap.Object{ID: 1, Shape: tilemap.ObjectTile, X: 4, Y: 6, Width: 16, Height: 16, GID: 1, Visible: true})
	if ol.ParallaxX != 1 || ol.ParallaxY != 1 || !ol.HasParallax {
		t.Fatalf("NewObjectLayer parallax = (%g, %g, %v), want (1, 1, true)", ol.ParallaxX, ol.ParallaxY, ol.HasParallax)
	}
	ol.ParallaxY = 0

	m := tilemap.New(1, 1, 16, 16, ts)
	m.Layers = []tilemap.TileLayer{tl}
	m.ImageLayers = []tilemap.ImageLayer{il}
	m.Objects = []tilemap.ObjectLayer{ol}

	var d core.Drawing
	m.DrawLayer(&d, &m.Layers[0], cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 100 || d.Ops[0].Y != 0 {
		t.Fatalf("NewTileLayer with ParallaxX=0 drew at %+v, want (100, 0)", d.Ops)
	}
	d.Ops = d.Ops[:0]
	m.DrawImageLayer(&d, &m.ImageLayers[0], cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 100 || d.Ops[0].Y != 50 {
		t.Fatalf("NewImageLayer with Parallax=(0,0) drew at %+v, want (100, 50)", d.Ops)
	}
	d.Ops = d.Ops[:0]
	m.DrawObjectLayer(&d, &m.Objects[0], cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 4 || d.Ops[0].Y != 56 {
		t.Fatalf("NewObjectLayer with ParallaxY=0 drew at %+v, want (4, 56)", d.Ops)
	}

	// Zero-value struct literals without HasParallax default to (1, 1) for backward compatibility.
	litTile := tilemap.TileLayer{Name: "lit_t", Width: 1, Height: 1, Data: []uint32{1}, Opacity: 1, Visible: true}
	litImg := tilemap.ImageLayer{Name: "lit_i", Image: img, Opacity: 1, Visible: true}
	litObj := tilemap.ObjectLayer{
		Name: "lit_o", Opacity: 1, Visible: true,
		Objects: []tilemap.Object{{ID: 1, Shape: tilemap.ObjectTile, X: 4, Y: 6, Width: 16, Height: 16, GID: 1, Visible: true}},
	}
	// Struct literal with one non-zero axis and HasParallax: false defaults only the unset zero axis to 1.
	litPartial := tilemap.ImageLayer{Name: "lit_half", Image: img, ParallaxX: 0.5, Opacity: 1, Visible: true}
	// Struct literal with explicit HasParallax: true honors 0 on both axes.
	litFixed := tilemap.ImageLayer{Name: "lit_fixed", Image: img, HasParallax: true, Opacity: 1, Visible: true}

	d.Ops = d.Ops[:0]
	m.DrawLayer(&d, &litTile, cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 0 || d.Ops[0].Y != 0 {
		t.Fatalf("literal TileLayer drew at %+v, want (0, 0)", d.Ops)
	}
	d.Ops = d.Ops[:0]
	m.DrawImageLayer(&d, &litImg, cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 0 || d.Ops[0].Y != 0 {
		t.Fatalf("literal ImageLayer drew at %+v, want (0, 0)", d.Ops)
	}
	d.Ops = d.Ops[:0]
	m.DrawObjectLayer(&d, &litObj, cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 4 || d.Ops[0].Y != 6 {
		t.Fatalf("literal ObjectLayer drew at %+v, want (4, 6)", d.Ops)
	}
	d.Ops = d.Ops[:0]
	m.DrawImageLayer(&d, &litPartial, cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 50 || d.Ops[0].Y != 0 {
		t.Fatalf("literal partial ImageLayer drew at %+v, want (50, 0)", d.Ops)
	}
	d.Ops = d.Ops[:0]
	m.DrawImageLayer(&d, &litFixed, cam)
	if len(d.Ops) != 1 || d.Ops[0].X != 100 || d.Ops[0].Y != 50 {
		t.Fatalf("literal HasParallax:true ImageLayer drew at %+v, want (100, 50)", d.Ops)
	}
}

func TestEncodeTiledPreservesParallaxZeroRoundTrip(t *testing.T) {
	raw := []byte(`{
		"width": 2, "height": 1, "tilewidth": 16, "tileheight": 16,
		"tilesets": [
			{"firstgid": 1, "name": "t", "image": "tiles.png", "tilewidth": 16, "tileheight": 16, "columns": 2, "tilecount": 4}
		],
		"layers": [
			{"name": "tile_zero", "type": "tilelayer", "width": 2, "height": 1, "data": [1, 0], "parallaxx": 0, "parallaxy": 0},
			{"name": "tile_mixed", "type": "tilelayer", "width": 2, "height": 1, "data": [1, 0], "parallaxx": 0, "parallaxy": 1},
			{"name": "tile_half", "type": "tilelayer", "width": 2, "height": 1, "data": [1, 0], "parallaxx": 0.5, "parallaxy": 0.25},
			{"name": "tile_one", "type": "tilelayer", "width": 2, "height": 1, "data": [1, 0]},
			{"name": "sky", "type": "imagelayer", "image": "sky.png", "imagewidth": 64, "imageheight": 32, "parallaxx": 0, "parallaxy": 0},
			{"name": "hud", "type": "objectgroup", "parallaxx": 0, "parallaxy": 0, "objects": [
				{"id": 1, "gid": 1, "x": 8, "y": 16, "width": 16, "height": 16, "visible": true}
			]},
			{
				"name": "grp", "type": "group", "parallaxx": 0, "parallaxy": 1,
				"layers": [
					{"name": "child_zero", "type": "imagelayer", "image": "bg.png", "imagewidth": 32, "imageheight": 32, "parallaxx": 2}
				]
			}
		]
	}`)

	m1, err := tilemap.LoadTiled(raw, tilemap.TilesetLoader{})
	if err != nil {
		t.Fatalf("LoadTiled: %v", err)
	}

	enc1, err := tilemap.EncodeTiled(m1)
	if err != nil {
		t.Fatalf("EncodeTiled #1: %v", err)
	}

	// Verify encoded JSON explicitly writes 0 and omits 1.
	var doc struct {
		Layers []struct {
			Name      string   `json:"name"`
			ParallaxX *float32 `json:"parallaxx"`
			ParallaxY *float32 `json:"parallaxy"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(enc1, &doc); err != nil {
		t.Fatalf("unmarshal encoded JSON: %v", err)
	}
	byName := make(map[string]struct{ px, py *float32 }, len(doc.Layers))
	for _, l := range doc.Layers {
		byName[l.Name] = struct{ px, py *float32 }{l.ParallaxX, l.ParallaxY}
	}
	checkJSON := func(name string, wantPX, wantPY *float32) {
		t.Helper()
		got, ok := byName[name]
		if !ok {
			t.Fatalf("encoded JSON missing layer %q", name)
		}
		if (got.px == nil) != (wantPX == nil) || (got.px != nil && *got.px != *wantPX) {
			t.Fatalf("layer %q JSON parallaxx = %v, want %v", name, got.px, wantPX)
		}
		if (got.py == nil) != (wantPY == nil) || (got.py != nil && *got.py != *wantPY) {
			t.Fatalf("layer %q JSON parallaxy = %v, want %v", name, got.py, wantPY)
		}
	}
	z := float32(0)
	half := float32(0.5)
	quarter := float32(0.25)
	checkJSON("tile_zero", &z, &z)
	checkJSON("tile_mixed", &z, nil)
	checkJSON("tile_half", &half, &quarter)
	checkJSON("tile_one", nil, nil)
	checkJSON("sky", &z, &z)
	checkJSON("hud", &z, &z)
	checkJSON("child_zero", &z, nil)

	// Reload and re-encode: round-trip preserves 0 and is byte-identical.
	m2, err := tilemap.LoadTiled(enc1, tilemap.TilesetLoader{})
	if err != nil {
		t.Fatalf("LoadTiled #2: %v", err)
	}
	if !reflect.DeepEqual(m2, m1) {
		t.Fatalf("round-tripped Tilemap differs:\ngot=%+v\nwant=%+v", m2, m1)
	}
	enc2, err := tilemap.EncodeTiled(m2)
	if err != nil {
		t.Fatalf("EncodeTiled #2: %v", err)
	}
	if !bytes.Equal(enc1, enc2) {
		t.Fatalf("EncodeTiled is not deterministic across round-trip:\n#1:\n%s\n#2:\n%s", enc1, enc2)
	}

	// Unset struct literal layers encode without parallaxx/parallaxy (default 1) instead of writing 0.
	ts := tilemap.NewGridTileset(1, core.ImageSource{URI: "tiles.png"}, 16, 16, 1, 1)
	prog := tilemap.New(1, 1, 16, 16, ts)
	prog.Layers = []tilemap.TileLayer{{Name: "lit_t", Width: 1, Height: 1, Data: []uint32{1}, Opacity: 1, Visible: true}}
	prog.ImageLayers = []tilemap.ImageLayer{{Name: "lit_i", Image: core.ImageSource{URI: "bg.png"}, Opacity: 1, Visible: true}}
	prog.Objects = []tilemap.ObjectLayer{{Name: "lit_o", Opacity: 1, Visible: true}}
	progEnc, err := tilemap.EncodeTiled(prog)
	if err != nil {
		t.Fatalf("EncodeTiled literal map: %v", err)
	}
	if bytes.Contains(progEnc, []byte("parallaxx")) || bytes.Contains(progEnc, []byte("parallaxy")) {
		t.Fatalf("EncodeTiled wrote parallax on unset struct literal layers:\n%s", progEnc)
	}
}

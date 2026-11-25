package world3d_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

func makeFlatHeightmap(cols, rows int, cellSize float32, origin math3d.Vec3) *world3d.Heightmap {
	return &world3d.Heightmap{
		Cols:        cols,
		Rows:        rows,
		CellSize:    cellSize,
		HeightScale: 10,
		Origin:      origin,
		Heights:     make([]float32, cols*rows),
	}
}

func makeFourCornerSplat() *image.NRGBA {
	// 2x2 splat map with pure distinct layers in each texel:
	// (0,0) = R (grass), (1,0) = G (dirt), (0,1) = B (rock), (1,1) = A (snow)
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 0, B: 0, A: 0})
	img.SetNRGBA(1, 0, color.NRGBA{R: 0, G: 255, B: 0, A: 0})
	img.SetNRGBA(0, 1, color.NRGBA{R: 0, G: 0, B: 255, A: 0})
	img.SetNRGBA(1, 1, color.NRGBA{R: 0, G: 0, B: 0, A: 255})
	return img
}

func TestHeightmapLayerAtCornersBilinearAndClamping(t *testing.T) {
	hm := makeFlatHeightmap(5, 5, 2.0, math3d.Vec3{X: -4, Y: 0, Z: -4})
	// Footprint spans X in [-4, 4], Z in [-4, 4].
	layers := []world3d.TerrainLayerInfo{
		{Name: "grass"},
		{Name: "dirt"},
		{Name: "rock"},
		{Name: "snow"},
	}
	splat, err := world3d.LoadTerrainSplatMap(layers, makeFourCornerSplat())
	if err != nil {
		t.Fatalf("LoadTerrainSplatMap: %v", err)
	}
	hm.Splat = splat

	// Corners match each pure texel exactly.
	cornerCases := []struct {
		x, z     float32
		wantDom  int
		wantName string
	}{
		{-4, -4, 0, "grass"},
		{4, -4, 1, "dirt"},
		{-4, 4, 2, "rock"},
		{4, 4, 3, "snow"},
	}
	for _, tc := range cornerCases {
		got, ok := hm.LayerAt(tc.x, tc.z)
		if !ok {
			t.Fatalf("LayerAt(%v, %v): expected ok=true", tc.x, tc.z)
		}
		if got.Dominant != tc.wantDom || got.Name != tc.wantName || got.Count != 4 {
			t.Fatalf("LayerAt(%v, %v) = (dom=%d name=%q count=%d), want (%d %q 4)",
				tc.x, tc.z, got.Dominant, got.Name, got.Count, tc.wantDom, tc.wantName)
		}
		if !math3d.ApproxEqual(got.Weight(tc.wantDom), 1.0, 1e-5) {
			t.Fatalf("LayerAt(%v, %v) dominant weight = %v, want 1.0", tc.x, tc.z, got.Weight(tc.wantDom))
		}
		var sum float32
		for i := range got.Count {
			sum += got.Weights[i]
		}
		if !math3d.ApproxEqual(sum, 1.0, 1e-5) {
			t.Fatalf("LayerAt(%v, %v) weight sum = %v, want 1.0", tc.x, tc.z, sum)
		}
	}

	// Exact center (0, 0) -> UV (0.5, 0.5) sits equidistant from all four 2x2 texel centers: 0.25 each.
	mid, ok := hm.LayerAt(0, 0)
	if !ok {
		t.Fatalf("LayerAt(0, 0): expected ok=true")
	}
	for i := range 4 {
		if !math3d.ApproxEqual(mid.Weights[i], 0.25, 1e-5) {
			t.Fatalf("center weight[%d] = %v, want 0.25", i, mid.Weights[i])
		}
	}
	// Ties break deterministically to lowest index (0 = grass).
	if mid.Dominant != 0 || mid.Name != "grass" {
		t.Fatalf("center dominant = (%d, %q), want (0, grass)", mid.Dominant, mid.Name)
	}

	// Halfway along top edge (x=0, z=-4) -> UV (0.5, 0) blends grass (0.5) and dirt (0.5).
	topMid, ok := hm.LayerAt(0, -4)
	if !ok {
		t.Fatalf("LayerAt(0, -4): expected ok=true")
	}
	if !math3d.ApproxEqual(topMid.Weights[0], 0.5, 1e-5) || !math3d.ApproxEqual(topMid.Weights[1], 0.5, 1e-5) ||
		!math3d.ApproxEqual(topMid.Weights[2], 0, 1e-5) || !math3d.ApproxEqual(topMid.Weights[3], 0, 1e-5) {
		t.Fatalf("topMid weights = %v, want [0.5, 0.5, 0, 0]", topMid.Weights[:4])
	}

	// Out-of-bounds returns ok=false while still returning the clamped edge sample.
	oobNW, ok := hm.LayerAt(-100, -100)
	if ok {
		t.Fatalf("LayerAt(-100, -100): expected ok=false")
	}
	if oobNW.Dominant != 0 || oobNW.Name != "grass" || !math3d.ApproxEqual(oobNW.Weights[0], 1, 1e-5) {
		t.Fatalf("oobNW = %+v, want clamped grass", oobNW)
	}
	oobSE, ok := hm.LayerAt(100, 100)
	if ok {
		t.Fatalf("LayerAt(100, 100): expected ok=false")
	}
	if oobSE.Dominant != 3 || oobSE.Name != "snow" || !math3d.ApproxEqual(oobSE.Weights[3], 1, 1e-5) {
		t.Fatalf("oobSE = %+v, want clamped snow", oobSE)
	}
}

func TestTerrainSplatEightLayers(t *testing.T) {
	// 8 texels wide x 1 texel tall: texel i has 100% weight in layer i (0..7).
	s0 := image.NewNRGBA(image.Rect(0, 0, 8, 1))
	s1 := image.NewNRGBA(image.Rect(0, 0, 8, 1))
	names := []string{"grass", "dirt", "rock", "snow", "sand", "mud", "gravel", "ice"}
	layers := make([]world3d.TerrainLayerInfo, 8)
	for i, name := range names {
		layers[i] = world3d.TerrainLayerInfo{Name: name}
		switch i {
		case 0:
			s0.SetNRGBA(i, 0, color.NRGBA{R: 255})
		case 1:
			s0.SetNRGBA(i, 0, color.NRGBA{G: 255})
		case 2:
			s0.SetNRGBA(i, 0, color.NRGBA{B: 255})
		case 3:
			s0.SetNRGBA(i, 0, color.NRGBA{A: 255})
		case 4:
			s1.SetNRGBA(i, 0, color.NRGBA{R: 255})
		case 5:
			s1.SetNRGBA(i, 0, color.NRGBA{G: 255})
		case 6:
			s1.SetNRGBA(i, 0, color.NRGBA{B: 255})
		case 7:
			s1.SetNRGBA(i, 0, color.NRGBA{A: 255})
		}
	}

	splat, err := world3d.LoadTerrainSplatMap(layers, s0, s1)
	if err != nil {
		t.Fatalf("LoadTerrainSplatMap(8 layers): %v", err)
	}
	splat.Origin = math3d.Vec2{X: 10, Y: 20}
	splat.Size = math3d.Vec2{X: 80, Y: 10}

	if splat.LayerCount() != 8 {
		t.Fatalf("LayerCount = %d, want 8", splat.LayerCount())
	}
	for i, wantName := range names {
		if idx := splat.LayerIndex(wantName); idx != i {
			t.Fatalf("LayerIndex(%q) = %d, want %d", wantName, idx, i)
		}
		if gotName := splat.LayerName(i); gotName != wantName {
			t.Fatalf("LayerName(%d) = %q, want %q", i, gotName, wantName)
		}
		// Sample exact texel center u = (i + 0.5) / 8 in world coordinates.
		u := (float32(i) + 0.5) / 8.0
		x := splat.Origin.X + u*splat.Size.X
		z := splat.Origin.Y + 0.5*splat.Size.Y
		got := splat.LayerAt(x, z)
		if got.Dominant != i || got.Name != wantName {
			t.Fatalf("LayerAt(layer %d) = (%d, %q), want (%d, %q)", i, got.Dominant, got.Name, i, wantName)
		}
		if !math3d.ApproxEqual(got.Weight(i), 1.0, 1e-5) {
			t.Fatalf("layer %d weight = %v, want 1.0", i, got.Weight(i))
		}
	}

	// Midpoint between layer 3 (splat0.A) and layer 4 (splat1.R) should be 50/50.
	boundary := splat.SampleUV(0.5, 0.5)
	if !math3d.ApproxEqual(boundary.Weights[3], 0.5, 1e-5) || !math3d.ApproxEqual(boundary.Weights[4], 0.5, 1e-5) {
		t.Fatalf("splat0->splat1 boundary weights = %v, want 0.5 at indices 3 and 4", boundary.Weights)
	}
}

func TestTerrainSplatHeightWeightedBlending(t *testing.T) {
	// 1x1 splat with 50% grass, 50% rock.
	s0 := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	s0.SetNRGBA(0, 0, color.NRGBA{R: 128, G: 128, B: 0, A: 0})

	// High height map for rock (0.9), low height map for grass (0.1).
	lowH := image.NewGray(image.Rect(0, 0, 2, 2))
	highH := image.NewGray(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			lowH.SetGray(x, y, color.Gray{Y: 26})   // ~0.10
			highH.SetGray(x, y, color.Gray{Y: 230}) // ~0.90
		}
	}

	layers := []world3d.TerrainLayerInfo{
		{Name: "grass", HeightMap: world3d.LoadTerrainHeightLayer(lowH)},
		{Name: "rock", HeightMap: world3d.LoadTerrainHeightLayer(highH)},
	}
	splat, err := world3d.LoadTerrainSplatMap(layers, s0)
	if err != nil {
		t.Fatalf("LoadTerrainSplatMap: %v", err)
	}

	// Linear blend (contrast 0): 50/50.
	lin := splat.SampleUV(0.5, 0.5)
	if !math3d.ApproxEqual(lin.Weights[0], 0.5, 1e-3) || !math3d.ApproxEqual(lin.Weights[1], 0.5, 1e-3) {
		t.Fatalf("linear weights = %v, want [0.5, 0.5]", lin.Weights[:2])
	}

	// With HeightBlendContrast = 0.7, rock's higher elevation dominates crisply.
	splat.HeightBlendContrast = 0.7
	hb := splat.SampleUV(0.5, 0.5)
	if hb.Dominant != 1 || hb.Name != "rock" {
		t.Fatalf("height-blended dominant = (%d, %q), want (1, rock)", hb.Dominant, hb.Name)
	}
	if hb.Weights[1] < 0.95 {
		t.Fatalf("height-blended rock weight = %v, expected > 0.95 (got %v)", hb.Weights[1], hb.Weights[:2])
	}

	// Zero-weight layer must never bleed in even with max height=1.0 and high contrast.
	var raw, heights [world3d.MaxTerrainLayers]float32
	raw[0], raw[1] = 1.0, 0.0
	heights[0], heights[1] = 0.0, 1.0
	out := world3d.BlendTerrainWeights(raw, heights, 2, 0.9)
	if out[1] != 0 || !math3d.ApproxEqual(out[0], 1, 1e-6) {
		t.Fatalf("zero-weight bleed: got %v, want [1, 0]", out[:2])
	}

	// All-zero raw splat falls back safely to layer 0 = 1.0.
	fallback := world3d.BlendTerrainWeights([world3d.MaxTerrainLayers]float32{}, heights, 4, 0.5)
	if !math3d.ApproxEqual(fallback[0], 1, 1e-6) {
		t.Fatalf("all-zero fallback = %v, want layer 0 = 1", fallback[:4])
	}
}

func TestTerrainSplatValidationAndNilSafety(t *testing.T) {
	if _, err := world3d.LoadTerrainSplatMap(nil, makeFourCornerSplat()); !errors.Is(err, world3d.ErrInvalidTerrainSplat) {
		t.Fatalf("expected ErrInvalidTerrainSplat for 0 layers, got %v", err)
	}
	nine := make([]world3d.TerrainLayerInfo, 9)
	if _, err := world3d.LoadTerrainSplatMap(nine, makeFourCornerSplat(), makeFourCornerSplat()); !errors.Is(err, world3d.ErrInvalidTerrainSplat) {
		t.Fatalf("expected ErrInvalidTerrainSplat for 9 layers, got %v", err)
	}
	five := make([]world3d.TerrainLayerInfo, 5)
	if _, err := world3d.LoadTerrainSplatMap(five, makeFourCornerSplat()); !errors.Is(err, world3d.ErrInvalidTerrainSplat) {
		t.Fatalf("expected ErrInvalidTerrainSplat for 5 layers with 1 splat map, got %v", err)
	}
	if _, err := world3d.NewTerrainSplatMapFromRGBA(2, 2, five[:2], []byte{1, 2, 3}, nil); !errors.Is(err, world3d.ErrInvalidTerrainSplat) {
		t.Fatalf("expected ErrInvalidTerrainSplat for short RGBA buffer, got %v", err)
	}

	hm := makeFlatHeightmap(4, 4, 1, math3d.Vec3Zero())
	if _, ok := hm.LayerAt(1, 1); ok {
		t.Fatalf("expected ok=false when hm.Splat is nil")
	}
	var nilSplat *world3d.TerrainSplatMap
	if nilSplat.LayerCount() != 0 || nilSplat.LayerName(0) != "" || nilSplat.LayerIndex("grass") != -1 {
		t.Fatalf("nil TerrainSplatMap accessors should return zero values")
	}
}

func TestTerrainYAMLParseEncodeAndValidation(t *testing.T) {
	r0 := float32(0.85)
	r1 := float32(0.4)
	ns := float32(1.2)
	uv0 := [2]float32{8, 8}
	col := [3]float32{0.8, 0.9, 0.7}
	doc := &world3d.TerrainDoc{
		Version:             world3d.TerrainFormatVersion,
		Name:                "Highland",
		Heightmap:           "height.png",
		CellSize:            2,
		HeightScale:         30,
		SplatMaps:           []string{"splat0.png"},
		HeightBlendContrast: 0.5,
		Triplanar: &world3d.TerrainTriplanarDoc{
			Enabled:        true,
			SlopeThreshold: 0.72,
			SlopeBlend:     0.2,
			Sharpness:      4,
			Scale:          0.25,
		},
		Layers: []world3d.TerrainLayerDoc{
			{
				Name:        "grass",
				Albedo:      "grass.png",
				Color:       &col,
				Roughness:   &r0,
				NormalScale: &ns,
				UVScale:     &uv0,
			},
			{
				Name:      "snow",
				Roughness: &r1,
			},
		},
	}

	raw, err := world3d.EncodeTerrain(doc)
	if err != nil {
		t.Fatalf("EncodeTerrain: %v", err)
	}
	if !strings.HasPrefix(string(raw), world3d.TerrainSchemaComment+"\n") {
		t.Fatalf("encoded YAML missing schema comment:\n%s", raw)
	}
	parsed, err := world3d.ParseTerrain(raw)
	if err != nil {
		t.Fatalf("ParseTerrain: %v", err)
	}
	raw2, err := world3d.EncodeTerrain(parsed)
	if err != nil {
		t.Fatalf("re-EncodeTerrain: %v", err)
	}
	if !bytes.Equal(raw, raw2) {
		t.Fatalf("round-trip mismatch:\n--- got ---\n%s\n--- want ---\n%s", raw2, raw)
	}
	if got := parsed.EffectiveCellSize(); got != 2 {
		t.Fatalf("EffectiveCellSize = %v, want 2", got)
	}
	infos := parsed.LayerInfos()
	if len(infos) != 2 || infos[0].Name != "grass" || infos[0].UVScale.X != 8 || infos[1].UVScale.X != 1 {
		t.Fatalf("LayerInfos = %+v", infos)
	}

	// Reject unknown fields, duplicate layer names, >4 layers with 1 splat map, trailing document, NaN.
	badInputs := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "unknown_field",
			yaml: "version: 1\nbogusKey: 1\nlayers:\n  - name: grass\n",
			want: "bogusKey",
		},
		{
			name: "duplicate_layer",
			yaml: "version: 1\nlayers:\n  - name: grass\n  - name: grass\n",
			want: "duplicate",
		},
		{
			name: "five_layers_one_splat",
			yaml: "version: 1\nsplatMaps:\n  - s0.png\nlayers:\n  - name: a\n  - name: b\n  - name: c\n  - name: d\n  - name: e\n",
			want: "splatMaps",
		},
		{
			name: "trailing_doc",
			yaml: "version: 1\nlayers:\n  - name: grass\n---\nversion: 1\n",
			want: "trailing",
		},
		{
			name: "bad_contrast",
			yaml: "version: 1\nheightBlendContrast: 1.5\nlayers:\n  - name: grass\n",
			want: "heightBlendContrast",
		},
	}
	for _, tc := range badInputs {
		_, err := world3d.ParseTerrain([]byte(tc.yaml))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got err=%v, want error mentioning %q", tc.name, err, tc.want)
		}
	}

	// NaN cellSize rejected by ValidateTerrain.
	nanDoc := *doc
	nanDoc.CellSize = float32(math.NaN())
	if err := world3d.ValidateTerrain(&nanDoc); err == nil {
		t.Fatalf("expected error for NaN CellSize")
	}
}

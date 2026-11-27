package world3d_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/world3d"
)

func TestHeightmap16BitPNGPrecisionNoBanding(t *testing.T) {
	// Construct a gentle slope spanning 512 columns across a small elevation band
	// (uint16 values 1000..1511 out of 65535). An 8-bit grayscale image only has
	// 256 levels (~256 uint16 units per step), so this entire 512-sample slope
	// would collapse into 2 flat staircase bands in 8-bit, whereas 16-bit preserves
	// every single step smoothly.
	const cols, rows = 512, 4
	hm := world3d.NewHeightmap(cols, rows, 1.0, 100.0)
	for r := range rows {
		for c := range cols {
			u16 := uint16(1000 + c)
			hm.Heights[r*cols+c] = float32(u16) / 65535.0
		}
	}

	rawPNG, err := world3d.EncodeHeightmapPNG(hm)
	if err != nil {
		t.Fatalf("EncodeHeightmapPNG: %v", err)
	}

	// Verify the encoded PNG decodes specifically as *image.Gray16 (true 16-bit grayscale).
	decodedImg, err := png.Decode(bytes.NewReader(rawPNG))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	g16, ok := decodedImg.(*image.Gray16)
	if !ok {
		t.Fatalf("decoded PNG type = %T, want *image.Gray16", decodedImg)
	}
	for c := range cols {
		want := uint16(1000 + c)
		got := g16.Gray16At(c, 0).Y
		if got != want {
			t.Fatalf("Gray16At(%d, 0) = %d, want %d", c, got, want)
		}
	}

	// Decode back via DecodeHeightmapPNG and verify smooth monotonic slope with zero 8-bit banding.
	loaded, err := world3d.DecodeHeightmapPNG(bytes.NewReader(rawPNG), 1.0, 100.0)
	if err != nil {
		t.Fatalf("DecodeHeightmapPNG: %v", err)
	}
	wantStep := float32(100.0 / 65535.0)
	for c := 1; c < cols; c++ {
		prev := loaded.SampleHeight(c-1, 0)
		curr := loaded.SampleHeight(c, 0)
		diff := curr - prev
		if diff <= 0 || math.Abs(float64(diff-wantStep)) > 1e-4 {
			t.Fatalf("slope step at col %d = %v, want %v (8-bit banding detected)", c, diff, wantStep)
		}
	}
}

func TestTerrainYAMLSizeWeightMapsAndEmbeddedFoliage(t *testing.T) {
	hmImg := makeSampleHeightImage() // 9x9 Gray16

	// Per-layer grayscale weight maps: left half grass, right half rock.
	grassW := image.NewGray(image.Rect(0, 0, 4, 4))
	rockW := image.NewGray(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			if x < 2 {
				grassW.SetGray(x, y, color.Gray{Y: 255})
				rockW.SetGray(x, y, color.Gray{Y: 0})
			} else {
				grassW.SetGray(x, y, color.Gray{Y: 0})
				rockW.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}

	// Foliage density map: top half dense (255), bottom half empty (0).
	foliageDM := image.NewGray(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			if y < 2 {
				foliageDM.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}

	terrainYAML := []byte(`# yaml-language-server: $schema=https://game.nsgo.dev/schemas/terrain.v1.json
version: 1
name: Meadow Ridge
size:
  - 9
  - 9
heightmap: terrain/meadow_height.png
cellSize: 2
heightScale: 30
layers:
  - name: grass
    material: materials/grass.material.yaml
    weightMap: terrain/meadow_weight_grass.png
    uvScale:
      - 8
      - 8
  - name: rock
    material: materials/rock.material.yaml
    weightMap: terrain/meadow_weight_rock.png
    uvScale:
      - 4
      - 4
foliage:
  - name: fern
    mesh: models/fern.glb
    densityMap: terrain/meadow_foliage_fern.png
    density: 2
    seed: 7
`)

	// Strict round-trip determinism.
	doc, err := world3d.ParseTerrain(terrainYAML)
	if err != nil {
		t.Fatalf("ParseTerrain: %v", err)
	}
	encoded, err := world3d.EncodeTerrain(doc)
	if err != nil {
		t.Fatalf("EncodeTerrain: %v", err)
	}
	if !bytes.Equal(encoded, terrainYAML) {
		t.Fatalf("round-trip changed bytes:\n--- got ---\n%s\n--- want ---\n%s", encoded, terrainYAML)
	}
	if !doc.HasLayerWeightMaps() {
		t.Fatalf("HasLayerWeightMaps() = false, want true")
	}

	fsys := fstest.MapFS{
		"terrain/meadow.terrain.yaml":     {Data: terrainYAML},
		"terrain/meadow_height.png":       {Data: encodePNG(t, hmImg)},
		"terrain/meadow_weight_grass.png": {Data: encodePNG(t, grassW)},
		"terrain/meadow_weight_rock.png":  {Data: encodePNG(t, rockW)},
		"terrain/meadow_foliage_fern.png": {Data: encodePNG(t, foliageDM)},
	}
	hm, loadedDoc, err := world3d.LoadTerrainFS(fsys, "terrain/meadow.terrain.yaml")
	if err != nil {
		t.Fatalf("LoadTerrainFS: %v", err)
	}
	if hm.Cols != 9 || hm.Rows != 9 {
		t.Fatalf("hm dims = %dx%d, want 9x9", hm.Cols, hm.Rows)
	}

	// Left side (x=1, z=8) is pure grass; right side (x=15, z=8) is pure rock.
	left, ok := hm.LayerAt(1, 8)
	if !ok || left.Name != "grass" || left.Weight(0) < 0.99 {
		t.Fatalf("left LayerAt = (%+v, %v), want grass ~1.0", left, ok)
	}
	right, ok := hm.LayerAt(15, 8)
	if !ok || right.Name != "rock" || right.Weight(1) < 0.99 {
		t.Fatalf("right LayerAt = (%+v, %v), want rock ~1.0", right, ok)
	}

	// Foliage density map is attached and queryable via FoliageDensityAt.
	topDens, ok := hm.FoliageDensityAt("fern", 8, 1)
	if !ok || topDens < 0.99 {
		t.Fatalf("top FoliageDensityAt = (%v, %v), want ~1.0", topDens, ok)
	}
	botDens, ok := hm.FoliageDensityAt("fern", 8, 15)
	if !ok || botDens > 0.01 {
		t.Fatalf("bottom FoliageDensityAt = (%v, %v), want ~0.0", botDens, ok)
	}

	// ScatterFoliage reuses attached density maps and places instances only in the dense half.
	fields, err := loadedDoc.ScatterFoliage(hm, hm.Bounds(), world3d.FoliageBuildOptions{
		ResolveMesh: func(string) (uint32, error) { return 42, nil },
	})
	if err != nil {
		t.Fatalf("ScatterFoliage: %v", err)
	}
	if len(fields) != 1 || fields[0].TotalCount() == 0 {
		t.Fatalf("ScatterFoliage produced %d fields (count=%d), want 1 non-empty field", len(fields), fields[0].TotalCount())
	}
	if fields[0].Layer.MeshID != 42 {
		t.Fatalf("MeshID = %d, want 42", fields[0].Layer.MeshID)
	}
}

func TestTerrainYAMLNewFieldValidation(t *testing.T) {
	badCases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "size too small",
			yaml: "version: 1\nsize: [1, 8]\nlayers: [{name: grass}]\n",
			want: "size",
		},
		{
			name: "both splatMaps and layer weightMap",
			yaml: "version: 1\nsplatMaps: [s0.png]\nlayers: [{name: grass, weightMap: w0.png}]\n",
			want: "splatMaps",
		},
		{
			name: "embedded foliage missing mesh",
			yaml: "version: 1\nlayers: [{name: grass}]\nfoliage: [{name: fern, density: 1}]\n",
			want: "foliage[0].mesh",
		},
		{
			name: "embedded foliage duplicate name",
			yaml: "version: 1\nlayers: [{name: grass}]\nfoliage: [{name: fern, mesh: a.glb, density: 1}, {name: fern, mesh: b.glb, spacing: 2}]\n",
			want: "duplicate foliage layer name",
		},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := world3d.ParseTerrain([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if _, ok := loaderr.As(err); !ok {
				t.Fatalf("error %T is not *loaderr.Error: %v", err, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}

	// Size mismatch against heightmap image is refused by BuildHeightmap.
	size := [2]int{16, 16}
	doc := &world3d.TerrainDoc{
		Version: world3d.TerrainFormatVersion,
		Size:    &size,
		Layers:  []world3d.TerrainLayerDoc{{Name: "grass"}},
	}
	if _, err := doc.BuildHeightmap(world3d.TerrainImages{Heightmap: makeSampleHeightImage()}); err == nil || !strings.Contains(err.Error(), "does not match size") {
		t.Fatalf("expected size mismatch error, got %v", err)
	}

	// Omitting Heightmap when Size is set builds a flat heightmap of the declared size.
	flat, err := doc.BuildHeightmap(world3d.TerrainImages{})
	if err != nil {
		t.Fatalf("BuildHeightmap(size-only): %v", err)
	}
	if flat.Cols != 16 || flat.Rows != 16 {
		t.Fatalf("flat dims = %dx%d, want 16x16", flat.Cols, flat.Rows)
	}
}

func TestPackLayerWeightMapsAndDensityMapPNG(t *testing.T) {
	// 6 layers pack into 2 RGBA splat maps and unpack losslessly per channel.
	weights := make([]image.Image, 6)
	for i := range 6 {
		g := image.NewGray(image.Rect(0, 0, 6, 1))
		g.SetGray(i, 0, color.Gray{Y: 255})
		weights[i] = g
	}
	s0, s1, err := world3d.PackLayerWeightMaps(weights...)
	if err != nil {
		t.Fatalf("PackLayerWeightMaps: %v", err)
	}
	if s0 == nil || s1 == nil {
		t.Fatalf("expected both splat0 and splat1 for 6 layers")
	}
	layers := make([]world3d.TerrainLayerInfo, 6)
	for i := range 6 {
		layers[i] = world3d.TerrainLayerInfo{Name: string(rune('a' + i))}
	}
	splat, err := world3d.LoadTerrainSplatMapFromWeights(layers, weights)
	if err != nil {
		t.Fatalf("LoadTerrainSplatMapFromWeights: %v", err)
	}
	for i := range 6 {
		extracted := splat.LayerWeightImage(i)
		if extracted == nil || extracted.GrayAt(i, 0).Y != 255 {
			t.Fatalf("LayerWeightImage(%d) mismatch", i)
		}
		u := (float32(i) + 0.5) / 6.0
		sample := splat.SampleUV(u, 0.5)
		if sample.Dominant != i {
			t.Fatalf("SampleUV(%v) dominant = %d, want %d", u, sample.Dominant, i)
		}
	}

	// DensityMap PNG round-trip.
	dm := world3d.NewDensityMap(4, 4)
	dm.Set(1, 2, 0.75)
	dmBytes, err := world3d.EncodeDensityMapPNG(dm)
	if err != nil {
		t.Fatalf("EncodeDensityMapPNG: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(dmBytes))
	if err != nil {
		t.Fatalf("png.Decode density: %v", err)
	}
	dm2 := world3d.LoadDensityMapGray(decoded)
	if !math3d.ApproxEqual(dm2.At(1, 2), 0.75, 1.0/255.0) {
		t.Fatalf("round-tripped density At(1,2) = %v, want ~0.75", dm2.At(1, 2))
	}
}

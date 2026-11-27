package render3d_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/game/pkg/world3d"
)

func mustPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestTerrainMaterialFromDocWeightMapsAndMaterialResources(t *testing.T) {
	// Per-layer grayscale weight maps: layer 0 (grass) on left, layer 1 (rock) on right.
	wGrass := image.NewGray(image.Rect(0, 0, 4, 4))
	wRock := image.NewGray(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			if x < 2 {
				wGrass.SetGray(x, y, color.Gray{Y: 255})
			} else {
				wRock.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}

	// Solid green albedo texture referenced via material resource; solid gray albedo for rock.
	greenTex := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	grayTex := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			greenTex.SetNRGBA(x, y, color.NRGBA{R: 40, G: 200, B: 60, A: 255})
			grayTex.SetNRGBA(x, y, color.NRGBA{R: 180, G: 180, B: 180, A: 255})
		}
	}

	hm := world3d.NewHeightmap(5, 5, 2, 10)
	hmPNG, err := world3d.EncodeHeightmapPNG(hm)
	if err != nil {
		t.Fatalf("EncodeHeightmapPNG: %v", err)
	}

	terrainYAML := []byte(`version: 1
name: Blended
size: [5, 5]
heightmap: terrain/height.png
cellSize: 2
heightScale: 10
layers:
  - name: grass
    material: materials/grass.material.yaml
    weightMap: terrain/weight_grass.png
  - name: rock
    albedo: textures/rock.png
    weightMap: terrain/weight_rock.png
    roughness: 0.9
`)
	grassMatYAML := []byte(`type: material
version: 1
name: Grass
texture: textures/grass.png
roughness: 0.8
`)

	fsys := fstest.MapFS{
		"terrain/blended.terrain.yaml":  {Data: terrainYAML},
		"terrain/height.png":            {Data: hmPNG},
		"terrain/weight_grass.png":      {Data: mustPNG(t, wGrass)},
		"terrain/weight_rock.png":       {Data: mustPNG(t, wRock)},
		"materials/grass.material.yaml": {Data: grassMatYAML},
		"textures/grass.png":            {Data: mustPNG(t, greenTex)},
		"textures/rock.png":             {Data: mustPNG(t, grayTex)},
	}

	loadedHM, doc, err := world3d.LoadTerrainFS(fsys, "terrain/blended.terrain.yaml")
	if err != nil {
		t.Fatalf("LoadTerrainFS: %v", err)
	}
	mat, err := render3d.TerrainMaterialFromDoc(fsys, doc)
	if err != nil {
		t.Fatalf("TerrainMaterialFromDoc: %v", err)
	}
	if len(mat.Layers) != 2 {
		t.Fatalf("len(Layers) = %d, want 2", len(mat.Layers))
	}
	if mat.Layers[0].AlbedoImage == nil || mat.Layers[1].AlbedoImage == nil {
		t.Fatalf("expected both layers to have decoded albedo images")
	}
	if !math3d.ApproxEqual(mat.Layers[0].RoughnessFactor, 0.8, 1e-5) {
		t.Fatalf("grass roughness = %v, want 0.8 from material resource", mat.Layers[0].RoughnessFactor)
	}

	// Render the terrain mesh via GPUTerrainMaterial and confirm left side is green (grass)
	// and right side is neutral gray (rock).
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{X: 4, Y: 12, Z: 4}, 8, 1.0, 0.1, 40)
	cam.LookAt(math3d.Vec3{X: 4, Y: 0, Z: 4}, math3d.Vec3{Z: -1})
	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.3, Y: 0.3, Z: 0.3}, Intensity: 1},
		Directional: []render3d.DirectionalLight{
			{Direction: math3d.Vec3{X: 0, Y: -1, Z: 0}, Color: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: 2.0},
		},
	}
	mesh := render3d.MeshFromTerrainMesh(loadedHM.BuildMesh(0, 0, 0, 0))
	img := renderTerrainSoftware(t, 64, 64, mesh, mat, rig, cam)

	// Left side (x=16, y=32) should be distinctly greener than red/blue (grass).
	leftPx := img.RGBAAt(16, 32)
	if leftPx.G <= leftPx.R+30 || leftPx.G <= leftPx.B+30 {
		t.Fatalf("left pixel = %+v, expected green-dominant grass layer", leftPx)
	}
	// Right side (x=48, y=32) should be neutral gray (rock, R ~= G ~= B).
	rightPx := img.RGBAAt(48, 32)
	if rightPx.R == 0 || absDiffU8(rightPx.R, rightPx.G) > 5 || absDiffU8(rightPx.G, rightPx.B) > 5 {
		t.Fatalf("right pixel = %+v, expected neutral gray rock layer", rightPx)
	}
}

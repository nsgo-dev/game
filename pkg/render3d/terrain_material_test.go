package render3d_test

import (
	"errors"
	"image"
	"image/color"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/game/pkg/world3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func terrainAssertGolden(t *testing.T, name string, got *image.RGBA) {
	t.Helper()
	path := gametest.GoldenPath("", name)
	if gametest.ShouldUpdateGolden() {
		if err := gametest.WritePNG(path, got); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := gametest.ReadPNG(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (UPDATE_GOLDEN=1 writes it)", path, err)
	}
	if !want.Bounds().Eq(got.Bounds()) {
		t.Fatalf("%s: size %v, golden %v", name, got.Bounds(), want.Bounds())
	}
	const channelTolerance = 3
	far := 0
	b := got.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g := got.RGBAAt(x, y)
			wr, wg, wb, wa := want.At(x, y).RGBA()
			w := [4]int{int(wr >> 8), int(wg >> 8), int(wb >> 8), int(wa >> 8)}
			for i, c := range [4]int{int(g.R), int(g.G), int(g.B), int(g.A)} {
				if d := c - w[i]; d > channelTolerance || d < -channelTolerance {
					far++
					break
				}
			}
		}
	}
	if limit := b.Dx() * b.Dy() / 200; far > limit {
		dir := t.TempDir()
		diff, _ := gametest.DiffImage(got, want)
		_ = gametest.WritePNG(dir+"/"+name+".got.png", got)
		_ = gametest.WritePNG(dir+"/"+name+".diff.png", diff)
		t.Fatalf("%s: %d pixels differ from golden (limit %d); got and diff written to %s", name, far, limit, dir)
	}
}

// TestTerrainMaterialWGSLCompilesAndHasNoForwardCalls verifies that every
// terrain WGSL shader variant compiles cleanly and declares every helper
// function before any caller (required for WGSL -> MSL compilation).
func TestTerrainMaterialWGSLCompilesAndHasNoForwardCalls(t *testing.T) {
	shaders := map[string]string{
		"TerrainMaterialWGSL":       render3d.TerrainMaterialWGSL,
		"TerrainMaterialShadowWGSL": render3d.TerrainMaterialShadowWGSL,
		"TerrainMaterialCSMWGSL":    render3d.TerrainMaterialCSMWGSL,
		"TerrainMaterialFogWGSL":    render3d.TerrainMaterialFogWGSL,
	}

	fnDeclRe := regexp.MustCompile(`(?m)^\s*fn\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	for name, src := range shaders {
		mod, err := gpu.CompileWGSL(src)
		if err != nil {
			t.Fatalf("CompileWGSL(%s): %v", name, err)
		}
		if !mod.HasVertexEntry("vs_main") || !mod.HasFragmentEntry("fs_main") {
			t.Fatalf("%s missing vs_main or fs_main entry points", name)
		}

		// Check function declaration ordering: no function body may call a user-defined function declared later.
		matches := fnDeclRe.FindAllStringSubmatchIndex(src, -1)
		fnNames := make([]string, len(matches))
		fnStarts := make([]int, len(matches))
		for i, m := range matches {
			fnNames[i] = src[m[2]:m[3]]
			fnStarts[i] = m[0]
		}
		for i := range matches {
			end := len(src)
			if i+1 < len(matches) {
				end = fnStarts[i+1]
			}
			body := src[fnStarts[i]:end]
			for j := i + 1; j < len(matches); j++ {
				callRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(fnNames[j]) + `\s*\(`)
				if callRe.FindStringIndex(body) != nil {
					t.Errorf("%s: function %q calls forward-declared function %q (breaks WGSL -> MSL)", name, fnNames[i], fnNames[j])
				}
			}
		}
	}
}

func TestTerrainMaterialValidationAndUniformLayout(t *testing.T) {
	if render3d.TerrainMaterialUniformSize != 544 {
		t.Fatalf("TerrainMaterialUniformSize = %d, want 544", render3d.TerrainMaterialUniformSize)
	}
	if render3d.TerrainMaterialGroup != render3d.PBRDrawGroup {
		t.Fatalf("TerrainMaterialGroup = %d, want PBRDrawGroup %d", render3d.TerrainMaterialGroup, render3d.PBRDrawGroup)
	}

	valid := render3d.TerrainMaterial{
		Layers: []render3d.TerrainLayer{
			render3d.DefaultTerrainLayer("grass"),
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid material rejected: %v", err)
	}

	// Zero-allocation AppendUniformData when capacity suffices.
	buf := make([]byte, 0, render3d.TerrainMaterialUniformSize)
	buf = valid.AppendUniformData(buf)
	if len(buf) != render3d.TerrainMaterialUniformSize {
		t.Fatalf("AppendUniformData len = %d, want %d", len(buf), render3d.TerrainMaterialUniformSize)
	}

	bad := []render3d.TerrainMaterial{
		{}, // 0 layers
		{Layers: make([]render3d.TerrainLayer, 9)},
		{Layers: []render3d.TerrainLayer{render3d.DefaultTerrainLayer("a")}, HeightBlendContrast: 1.5},
		{Layers: []render3d.TerrainLayer{render3d.DefaultTerrainLayer("a")}, HeightBlendContrast: float32(math.NaN())},
		{Layers: []render3d.TerrainLayer{{Name: "a", RoughnessFactor: -0.1}}},
		{Layers: []render3d.TerrainLayer{{Name: "a", MetallicFactor: 2.0}}},
		{Layers: []render3d.TerrainLayer{{Name: "a", UVScale: math3d.Vec2{X: -1, Y: 1}}}},
		{Layers: []render3d.TerrainLayer{{Name: "a", HeightBias: 1.5}}},
		{Layers: []render3d.TerrainLayer{render3d.DefaultTerrainLayer("a")}, Triplanar: render3d.TerrainTriplanar{Enabled: true, SlopeThreshold: 1.5}},
		{Layers: []render3d.TerrainLayer{render3d.DefaultTerrainLayer("a")}, Triplanar: render3d.TerrainTriplanar{Enabled: true, Sharpness: -1}},
		{Layers: []render3d.TerrainLayer{render3d.DefaultTerrainLayer("a")}, SplatMap0: image.NewNRGBA(image.Rect(0, 0, 0, 0))},
	}
	for i, m := range bad {
		if err := m.Validate(); !errors.Is(err, render3d.ErrInvalidTerrainMaterial) {
			t.Fatalf("bad[%d]: got err=%v, want ErrInvalidTerrainMaterial", i, err)
		}
	}
}

func terrainSolidImage(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func terrainCheckerImage(size, cells int, c0, c1 color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	step := max(size/cells, 1)
	for y := range size {
		for x := range size {
			if ((x/step)+(y/step))%2 == 0 {
				img.SetNRGBA(x, y, c0)
			} else {
				img.SetNRGBA(x, y, c1)
			}
		}
	}
	return img
}

func terrainPlaneMesh(size float32) render3d.Mesh {
	h := size * 0.5
	up := math3d.Vec3Up()
	return render3d.Mesh{
		Positions: []math3d.Vec3{
			{X: -h, Y: 0, Z: h},
			{X: h, Y: 0, Z: h},
			{X: h, Y: 0, Z: -h},
			{X: -h, Y: 0, Z: -h},
		},
		Normals: []math3d.Vec3{up, up, up, up},
		UVs: []math3d.Vec2{
			{X: 0, Y: 1},
			{X: 1, Y: 1},
			{X: 1, Y: 0},
			{X: 0, Y: 0},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

func renderTerrainSoftware(t *testing.T, w, h int, mesh render3d.Mesh, mat render3d.TerrainMaterial, rig render3d.LightRig, cam *scene3d.Camera) *image.RGBA {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	camBuf.WriteCamera(cam)

	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{Label: "terrain-lights"})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	gpuMat, err := render3d.NewGPUTerrainMaterial(nil, dev, mat, render3d.TerrainMaterialUpload{Label: "terrain-mat"})
	if err != nil {
		t.Fatalf("NewGPUTerrainMaterial: %v", err)
	}
	t.Cleanup(func() { _ = gpuMat.Close() })

	pipe, err := render3d.NewTerrainMaterialPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), gpuMat.Layout(), render3d.TerrainPipelineOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewTerrainMaterialPipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipe.Close() })

	gpuMesh, err := render3d.UploadMesh(nil, dev, mesh, render3d.MeshUpload{Label: "terrain-mesh"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = gpuMesh.Close() })

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: core.RGB(0x101824),
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1.0,
		},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	pass.SetBindGroup(render3d.LightGroup, lightBuf.BindGroup())
	gpuMat.Bind(pass)
	gpuMesh.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer error: %v", err)
	}

	return sr.Image()
}

// TestTerrainMaterialGPUParityEightLayers verifies 8-layer splat blending on
// the software GPU against EvaluateTerrainMaterial across both splat maps.
func TestTerrainMaterialGPUParityEightLayers(t *testing.T) {
	const w, h = 160, 40
	// 8 vertical strips across U in [0, 1], each selecting one of 8 distinct solid layers.
	s0 := image.NewNRGBA(image.Rect(0, 0, 8, 1))
	s1 := image.NewNRGBA(image.Rect(0, 0, 8, 1))
	layerColors := [8][3]float32{
		{0.18, 0.55, 0.16}, // 0 grass
		{0.48, 0.32, 0.18}, // 1 dirt
		{0.42, 0.44, 0.48}, // 2 rock
		{0.90, 0.93, 0.96}, // 3 snow
		{0.82, 0.72, 0.42}, // 4 sand
		{0.28, 0.19, 0.12}, // 5 mud
		{0.58, 0.54, 0.50}, // 6 gravel
		{0.45, 0.76, 0.90}, // 7 ice
	}
	layers := make([]render3d.TerrainLayer, 8)
	for i := range 8 {
		l := render3d.DefaultTerrainLayer("layer")
		l.BaseColorFactor = layerColors[i]
		l.RoughnessFactor = 0.8
		layers[i] = l
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

	mat := render3d.TerrainMaterial{
		Layers:    layers,
		SplatMap0: s0,
		SplatMap1: s1,
	}
	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.2, Y: 0.2, Z: 0.22}, Intensity: 1},
		Directional: []render3d.DirectionalLight{
			{Direction: math3d.Vec3{X: -0.3, Y: -1.0, Z: -0.2}.Norm(), Color: math3d.Vec3{X: 1, Y: 0.98, Z: 0.95}, Intensity: 2.2},
		},
	}

	cam := scene3d.NewOrthographicCamera(math3d.Vec3{X: 0, Y: 5, Z: 0}, 4.0, 1.0, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3{Z: -1})

	// Plane covering exact ortho frustum [-2, 2] x [-0.5, 0.5] so U runs linearly 0..1 left->right.
	up := math3d.Vec3Up()
	mesh := render3d.Mesh{
		Positions: []math3d.Vec3{
			{X: -2, Y: 0, Z: 0.5},
			{X: 2, Y: 0, Z: 0.5},
			{X: 2, Y: 0, Z: -0.5},
			{X: -2, Y: 0, Z: -0.5},
		},
		Normals: []math3d.Vec3{up, up, up, up},
		UVs: []math3d.Vec2{
			{X: 0, Y: 1},
			{X: 1, Y: 1},
			{X: 1, Y: 0},
			{X: 0, Y: 0},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}

	img := renderTerrainSoftware(t, w, h, mesh, mat, rig, cam)

	// Sample the exact center of each of the 8 layer strips and compare against CPU EvaluateTerrainMaterial.
	for i := range 8 {
		px := int(math.Round((float64(i) + 0.5) / 8.0 * float64(w)))
		got := img.RGBAAt(px, h/2)

		var sp0, sp1 [4]float32
		if i < 4 {
			sp0[i] = 1
		} else {
			sp1[i-4] = 1
		}
		worldX := -2 + (float32(px)+0.5)/float32(w)*4
		ev := render3d.EvaluateTerrainMaterial(render3d.TerrainEvalInput{
			Material: mat,
			Splat0:   sp0,
			Splat1:   sp1,
			WorldPos: math3d.Vec3{X: worldX, Y: 0, Z: 0},
			Normal:   up,
			ViewDir:  math3d.Vec3Up(),
			Rig:      rig,
		})
		wantR := uint8(math.Round(float64(ev.SRGB.X) * 255))
		wantG := uint8(math.Round(float64(ev.SRGB.Y) * 255))
		wantB := uint8(math.Round(float64(ev.SRGB.Z) * 255))
		if absDiffU8(got.R, wantR) > 3 || absDiffU8(got.G, wantG) > 3 || absDiffU8(got.B, wantB) > 3 {
			t.Fatalf("layer %d (px=%d): got RGBA(%d,%d,%d), want (%d,%d,%d)",
				i, px, got.R, got.G, got.B, wantR, wantG, wantB)
		}
	}
}

// TestTerrainMaterialHeightBlendAndTriplanar verifies both height-based layer
// contrast sharpening and slope-driven triplanar projection on the GPU.
func TestTerrainMaterialHeightBlendAndTriplanar(t *testing.T) {
	// 1. Height-based blending: 50/50 splat of dark dirt (height 0.1) vs bright rock (height 0.9).
	s0 := terrainSolidImage(2, 2, color.NRGBA{R: 128, G: 128, B: 0, A: 0})
	dirt := render3d.DefaultTerrainLayer("dirt")
	dirt.BaseColorFactor = [3]float32{0.15, 0.10, 0.08}
	dirt.HeightImage = terrainSolidImage(2, 2, color.NRGBA{R: 25, G: 25, B: 25, A: 255})
	rock := render3d.DefaultTerrainLayer("rock")
	rock.BaseColorFactor = [3]float32{0.85, 0.85, 0.85}
	rock.HeightImage = terrainSolidImage(2, 2, color.NRGBA{R: 235, G: 235, B: 235, A: 255})

	rig := render3d.LightRig{
		Ambient:     render3d.AmbientLight{Color: math3d.Vec3{X: 0.25, Y: 0.25, Z: 0.25}, Intensity: 1},
		Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{Y: -1}, Color: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: 2.0}},
	}
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Y: 5}, 2, 2, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3{Z: -1})
	plane := terrainPlaneMesh(2)

	linMat := render3d.TerrainMaterial{Layers: []render3d.TerrainLayer{dirt, rock}, SplatMap0: s0, HeightBlendContrast: 0}
	hbMat := render3d.TerrainMaterial{Layers: []render3d.TerrainLayer{dirt, rock}, SplatMap0: s0, HeightBlendContrast: 0.8}

	linImg := renderTerrainSoftware(t, 32, 32, plane, linMat, rig, cam)
	hbImg := renderTerrainSoftware(t, 32, 32, plane, hbMat, rig, cam)
	linC := linImg.RGBAAt(16, 16)
	hbC := hbImg.RGBAAt(16, 16)
	if hbC.R <= linC.R+35 {
		t.Fatalf("expected height-blended rock (R=%d) to be significantly brighter than linear 50/50 mix (R=%d)", hbC.R, linC.R)
	}

	// 2. Triplanar on steep vertical cliff (normal = +Z) with constant planar UV (0.5, 0.5).
	// Without triplanar, constant UV samples a single uniform color across the entire cliff;
	// with triplanar enabled, world-space XY sampling reveals the checkered pattern across the cliff!
	cliffN := math3d.Vec3{Z: 1}
	cliff := render3d.Mesh{
		Positions: []math3d.Vec3{
			{X: -2, Y: -2, Z: 0},
			{X: 2, Y: -2, Z: 0},
			{X: 2, Y: 2, Z: 0},
			{X: -2, Y: 2, Z: 0},
		},
		Normals: []math3d.Vec3{cliffN, cliffN, cliffN, cliffN},
		UVs: []math3d.Vec2{
			{X: 0.1, Y: 0.1},
			{X: 0.1, Y: 0.1},
			{X: 0.1, Y: 0.1},
			{X: 0.1, Y: 0.1},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
	cliffLayer := render3d.DefaultTerrainLayer("cliff")
	cliffLayer.AlbedoImage = terrainCheckerImage(16, 2, color.NRGBA{R: 230, G: 60, B: 60, A: 255}, color.NRGBA{R: 50, G: 210, B: 70, A: 255})
	cliffCam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 4, 1, 0.1, 20)
	cliffCam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	cliffRig := render3d.LightRig{
		Ambient:     render3d.AmbientLight{Color: math3d.Vec3{X: 0.3, Y: 0.3, Z: 0.3}, Intensity: 1},
		Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{Z: -1}, Color: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: 2.0}},
	}

	noTriMat := render3d.TerrainMaterial{Layers: []render3d.TerrainLayer{cliffLayer}}
	triMat := render3d.TerrainMaterial{
		Layers: []render3d.TerrainLayer{cliffLayer},
		Triplanar: render3d.TerrainTriplanar{
			Enabled:        true,
			SlopeThreshold: 0.7,
			SlopeBlend:     0.15,
			Sharpness:      4,
			Scale:          0.5,
		},
	}
	noTriImg := renderTerrainSoftware(t, 48, 48, cliff, noTriMat, cliffRig, cliffCam)
	triImg := renderTerrainSoftware(t, 48, 48, cliff, triMat, cliffRig, cliffCam)

	// Measure color variation across the cliff face.
	variation := func(img *image.RGBA) int {
		minR, maxR := 255, 0
		for y := 8; y < 40; y += 4 {
			for x := 8; x < 40; x += 4 {
				r := int(img.RGBAAt(x, y).R)
				if r < minR {
					minR = r
				}
				if r > maxR {
					maxR = r
				}
			}
		}
		return maxR - minR
	}
	if vNoTri := variation(noTriImg); vNoTri > 5 {
		t.Fatalf("expected uniform stretched cliff without triplanar, got R variation %d", vNoTri)
	}
	if vTri := variation(triImg); vTri < 60 {
		t.Fatalf("expected checkered triplanar projection across steep cliff, got R variation %d", vTri)
	}
}

// buildFourLayerTerrainSample creates a realistic rolling alpine valley with
// 4 splat-blended layers (grass valley, dirt paths, steep rock cliffs, snowy
// peaks) and returns the Heightmap, TerrainSplatMap, render3d.Mesh, and TerrainMaterial.
func buildFourLayerTerrainSample() (*world3d.Heightmap, render3d.Mesh, render3d.TerrainMaterial) {
	const cols, rows = 49, 49
	const cellSize = float32(0.5)
	const heightScale = float32(6.5)
	half := float32(cols-1) * cellSize * 0.5

	heights := make([]float32, cols*rows)
	for r := range rows {
		for c := range cols {
			u := float64(c) / float64(cols-1)
			v := float64(r) / float64(rows-1)
			x := (u - 0.5) * 2.0
			z := (v - 0.5) * 2.0

			// Central valley with rising twin ridges and a steep rock escarpment in back.
			peak1 := 0.85 * math.Exp(-((x+0.52)*(x+0.52)+(z+0.42)*(z+0.42))/0.22)
			peak2 := 0.92 * math.Exp(-((x-0.55)*(x-0.55)+(z+0.35)*(z+0.35))/0.25)
			hill := 0.35 * math.Exp(-((x+0.15)*(x+0.15)+(z-0.38)*(z-0.38))/0.30)
			ridge := 0.14 * (0.5 + 0.5*math.Sin(x*3.6)*math.Cos(z*2.8))
			h := peak1 + peak2 + hill + ridge
			if h > 1.0 {
				h = 1.0
			}
			heights[r*cols+c] = float32(h)
		}
	}

	hm := &world3d.Heightmap{
		Cols:        cols,
		Rows:        rows,
		CellSize:    cellSize,
		HeightScale: heightScale,
		Origin:      math3d.Vec3{X: -half, Y: 0, Z: -half},
		Heights:     heights,
	}

	// Build a 64x64 4-channel splat map driven by height, slope, and a winding dirt trail.
	const sw, sh = 64, 64
	splat0 := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	span := float32(cols-1) * cellSize
	for py := range sh {
		v := (float32(py) + 0.5) / float32(sh)
		wz := hm.Origin.Z + v*span
		for px := range sw {
			u := (float32(px) + 0.5) / float32(sw)
			wx := hm.Origin.X + u*span
			wy, _ := hm.HeightAt(wx, wz)
			normY := wy / heightScale
			n := hm.NormalAt(wx, wz)

			// Snow on high peaks, rock on steep slopes, dirt along a winding valley path, grass elsewhere.
			snowW := smoothRange(normY, 0.56, 0.76)
			rockW := smoothRange(1-n.Y, 0.12, 0.34) * (1 - snowW*0.7)
			trailCenter := float32(math.Sin(float64(v)*math.Pi*1.8) * 0.14)
			trailDist := float32(math.Abs(float64((u - 0.5) - trailCenter)))
			dirtW := (1 - smoothRange(trailDist, 0.05, 0.14)) * (1 - snowW) * (1 - rockW)
			grassW := max(float32(0), 1-snowW-rockW-dirtW)

			total := grassW + dirtW + rockW + snowW
			if total <= 1e-6 {
				grassW, total = 1, 1
			}
			splat0.SetNRGBA(px, py, color.NRGBA{
				R: uint8(math.Round(float64(grassW/total) * 255)),
				G: uint8(math.Round(float64(dirtW/total) * 255)),
				B: uint8(math.Round(float64(rockW/total) * 255)),
				A: uint8(math.Round(float64(snowW/total) * 255)),
			})
		}
	}

	// Procedural layer textures (32x32) with micro-detail, height maps, and subtle normal maps.
	const ts = 32
	makeNoiseTex := func(base [3]float64, amp float64, seed int) (*image.NRGBA, *image.Gray) {
		alb := image.NewNRGBA(image.Rect(0, 0, ts, ts))
		hImg := image.NewGray(image.Rect(0, 0, ts, ts))
		for y := range ts {
			for x := range ts {
				fx := float64(x) / float64(ts) * 2 * math.Pi
				fy := float64(y) / float64(ts) * 2 * math.Pi
				p := 0.5 + 0.25*math.Sin(fx*2+float64(seed)) + 0.25*math.Cos(fy*3-float64(seed)*1.3)
				p += 0.15 * math.Sin((fx+fy)*4+float64(seed)*0.7)
				if p < 0 {
					p = 0
				} else if p > 1 {
					p = 1
				}
				delta := (p - 0.5) * amp
				r := uint8(math.Round(math.Max(0, math.Min(1, base[0]+delta)) * 255))
				g := uint8(math.Round(math.Max(0, math.Min(1, base[1]+delta)) * 255))
				b := uint8(math.Round(math.Max(0, math.Min(1, base[2]+delta)) * 255))
				alb.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: 255})
				hImg.SetGray(x, y, color.Gray{Y: uint8(math.Round(p * 255))})
			}
		}
		return alb, hImg
	}

	grassAlb, grassH := makeNoiseTex([3]float64{0.26, 0.52, 0.20}, 0.14, 1)
	dirtAlb, dirtH := makeNoiseTex([3]float64{0.52, 0.36, 0.22}, 0.16, 2)
	rockAlb, rockH := makeNoiseTex([3]float64{0.44, 0.45, 0.48}, 0.20, 3)
	snowAlb, snowH := makeNoiseTex([3]float64{0.92, 0.95, 0.99}, 0.08, 4)

	grass := render3d.DefaultTerrainLayer("grass")
	grass.AlbedoImage = grassAlb
	grass.NormalImage = bumpedNormalMap(ts, 4, 0.35)
	grass.HeightImage = grassH
	grass.RoughnessFactor = 0.88
	grass.UVScale = math3d.Vec2{X: 10, Y: 10}

	dirt := render3d.DefaultTerrainLayer("dirt")
	dirt.AlbedoImage = dirtAlb
	dirt.NormalImage = bumpedNormalMap(ts, 3, 0.45)
	dirt.HeightImage = dirtH
	dirt.RoughnessFactor = 0.92
	dirt.UVScale = math3d.Vec2{X: 8, Y: 8}
	dirt.HeightBias = -0.05

	rock := render3d.DefaultTerrainLayer("rock")
	rock.AlbedoImage = rockAlb
	rock.NormalImage = bumpedNormalMap(ts, 2, 0.70)
	rock.HeightImage = rockH
	rock.RoughnessFactor = 0.80
	rock.UVScale = math3d.Vec2{X: 6, Y: 6}
	rock.HeightBias = 0.10

	snow := render3d.DefaultTerrainLayer("snow")
	snow.AlbedoImage = snowAlb
	snow.NormalImage = bumpedNormalMap(ts, 4, 0.20)
	snow.HeightImage = snowH
	snow.RoughnessFactor = 0.48
	snow.UVScale = math3d.Vec2{X: 7, Y: 7}
	snow.HeightBias = 0.05

	splatMap, err := world3d.LoadTerrainSplatMap([]world3d.TerrainLayerInfo{
		{Name: "grass", UVScale: grass.UVScale, HeightBias: grass.HeightBias, HeightMap: world3d.LoadTerrainHeightLayer(grassH)},
		{Name: "dirt", UVScale: dirt.UVScale, HeightBias: dirt.HeightBias, HeightMap: world3d.LoadTerrainHeightLayer(dirtH)},
		{Name: "rock", UVScale: rock.UVScale, HeightBias: rock.HeightBias, HeightMap: world3d.LoadTerrainHeightLayer(rockH)},
		{Name: "snow", UVScale: snow.UVScale, HeightBias: snow.HeightBias, HeightMap: world3d.LoadTerrainHeightLayer(snowH)},
	}, splat0)
	if err != nil {
		panic(err)
	}
	splatMap.HeightBlendContrast = 0.55
	hm.Splat = splatMap

	tm := hm.BuildMesh(0, 0, 0, 0)
	mesh := render3d.Mesh{
		Positions: tm.Positions,
		Normals:   tm.Normals,
		UVs:       tm.UVs,
		Indices:   tm.Indices,
	}
	mat := render3d.TerrainMaterial{
		Layers:              []render3d.TerrainLayer{grass, dirt, rock, snow},
		SplatMap0:           splat0,
		HeightBlendContrast: 0.55,
		Triplanar:           render3d.DefaultTerrainTriplanar(),
	}
	return hm, mesh, mat
}

func smoothRange(v, edge0, edge1 float32) float32 {
	t := math3d.Clamp((v-edge0)/max(edge1-edge0, 1e-5), 0, 1)
	return t * t * (3 - 2*t)
}

// TestTerrainMaterialFourLayerShadowedGolden renders a 4-layer alpine terrain
// (grass, dirt, triplanar rock cliffs, snow peaks) with directional shadows
// from an occluding stone pillar, verifies LayerAt matches the sampled regions,
// verifies shadows darken the terrain beneath the pillar, and asserts golden
// image parity.
func TestTerrainMaterialFourLayerShadowedGolden(t *testing.T) {
	const w, h = 192, 128
	hm, terrainMesh, terrainMat := buildFourLayerTerrainSample()

	// Verify LayerAt matches the expected biomes on this sample terrain.
	if s, ok := hm.LayerAt(0, 0); !ok || s.Name != "dirt" {
		t.Fatalf("valley center LayerAt(0, 0) = (%+v, ok=%v), want dirt", s, ok)
	}
	if s, ok := hm.LayerAt(-6.2, -5.0); !ok || s.Name != "snow" {
		t.Fatalf("left peak LayerAt(-6.2, -5.0) = (%+v, ok=%v), want snow", s, ok)
	}
	if s, ok := hm.LayerAt(6.5, 6.0); !ok || s.Name != "grass" {
		t.Fatalf("grass meadow LayerAt(6.5, 6.0) = (%+v, ok=%v), want grass", s, ok)
	}

	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	cam := scene3d.NewPerspectiveCamera(
		math3d.Vec3{X: 0.5, Y: 10.5, Z: 16.5},
		math3d.DegToRad(44),
		float32(w)/float32(h),
		0.2,
		80,
	)
	cam.LookAt(math3d.Vec3{X: 0, Y: 2.0, Z: -1.5}, math3d.Vec3Up())

	sunDir := math3d.Vec3{X: -0.60, Y: -0.82, Z: -0.35}.Norm()
	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.22, Y: 0.25, Z: 0.30}, Intensity: 1},
		Directional: []render3d.DirectionalLight{
			{Direction: sunDir, Color: math3d.Vec3{X: 1.0, Y: 0.96, Z: 0.88}, Intensity: 2.8},
			{Direction: math3d.Vec3{X: 0.5, Y: -0.7, Z: 0.5}.Norm(), Color: math3d.Vec3{X: 0.55, Y: 0.65, Z: 0.80}, Intensity: 0.45},
		},
	}

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	camBuf.WriteCamera(cam)

	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{Label: "alpine-lights"})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	gpuTerrainMat, err := render3d.NewGPUTerrainMaterial(nil, dev, terrainMat, render3d.TerrainMaterialUpload{
		Label:   "alpine-terrain",
		Mipmaps: true,
	})
	if err != nil {
		t.Fatalf("NewGPUTerrainMaterial: %v", err)
	}
	t.Cleanup(func() { _ = gpuTerrainMat.Close() })
	if gpuTerrainMat.LayerCount() != 4 || gpuTerrainMat.ByteSize() == 0 {
		t.Fatalf("unexpected terrain material stats: layers=%d bytes=%d", gpuTerrainMat.LayerCount(), gpuTerrainMat.ByteSize())
	}

	smOpts := render3d.DefaultShadowMapOptions()
	smOpts.Resolution = 256
	smOpts.PCFKernel = render3d.ShadowFilterPCF3x3
	sm, err := render3d.NewDirectionalShadowMap(nil, dev, smOpts)
	if err != nil {
		t.Fatalf("NewDirectionalShadowMap: %v", err)
	}
	t.Cleanup(func() { _ = sm.Close() })

	bounds := hm.Bounds()
	bounds.Max.Y += 5
	smCfg := render3d.DefaultDirectionalShadowConfig()
	smCfg.Direction = sunDir
	smCfg.Resolution = 256
	smCfg.PCFKernel = render3d.ShadowFilterPCF3x3
	smCfg.DepthBias = 0.002
	smCfg.NormalBias = 0.04
	if _, err := sm.UpdateBounds(dev, smCfg, bounds); err != nil {
		t.Fatalf("shadow.UpdateBounds: %v", err)
	}

	terrainPipe, err := render3d.NewTerrainMaterialPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), gpuTerrainMat.Layout(), render3d.TerrainPipelineOptions{
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		ShadowLayout: sm.Layout(),
	})
	if err != nil {
		t.Fatalf("NewTerrainMaterialPipeline(ShadowLayout): %v", err)
	}
	t.Cleanup(func() { _ = terrainPipe.Close() })

	gpuTerrainMesh, err := render3d.UploadMesh(nil, dev, terrainMesh, render3d.MeshUpload{Label: "alpine-mesh"})
	if err != nil {
		t.Fatalf("UploadMesh terrain: %v", err)
	}
	t.Cleanup(func() { _ = gpuTerrainMesh.Close() })

	// Tall monolith pillar standing on the terrain to cast a distinct shadow across the valley.
	pillarY, _ := hm.HeightAt(2.2, 2.0)
	pillarModel := math3d.Translate(math3d.Vec3{X: 2.2, Y: pillarY + 2.2, Z: 2.0}).Mul(math3d.Scale(math3d.Vec3{X: 1.2, Y: 4.8, Z: 1.2}))
	pillarMesh, err := render3d.UploadMesh(nil, dev, shadowBoxMesh(), render3d.MeshUpload{Label: "monolith"})
	if err != nil {
		t.Fatalf("UploadMesh monolith: %v", err)
	}
	t.Cleanup(func() { _ = pillarMesh.Close() })

	pillarDraw, err := render3d.NewPBRDrawBuffer(nil, dev, nil, render3d.NewPBRDrawUniform(pillarModel, render3d.PBRMaterial{
		BaseColor: math3d.Vec3{X: 0.72, Y: 0.64, Z: 0.56},
		Roughness: 0.75,
	}), false)
	if err != nil {
		t.Fatalf("NewPBRDrawBuffer monolith: %v", err)
	}
	t.Cleanup(func() { _ = pillarDraw.Close() })

	pillarPipe, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), pillarDraw.Layout(), render3d.LitPBRPipelineOptions{
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		ShadowLayout: sm.Layout(),
	})
	if err != nil {
		t.Fatalf("NewLitPBRPipelineWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = pillarPipe.Close() })

	renderScene := func(castShadow bool) *image.RGBA {
		enc := dev.BeginEncoder()
		sp := sm.BeginShadowPass(dev, enc)
		if castShadow {
			sm.DrawCaster(sp, gpuTerrainMesh, math3d.Mat4Identity())
			sm.DrawCaster(sp, pillarMesh, pillarModel)
		}
		sp.End()

		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: core.RGB(0x6ea0d4),
			}},
			DepthStencil: &gpu.DepthStencilAttachment{
				DepthLoadOp:  gpu.LoadClear,
				DepthStoreOp: gpu.StoreDiscard,
				ClearDepth:   1.0,
			},
		})
		pass.SetPipeline(terrainPipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		pass.SetBindGroup(render3d.LightGroup, lightBuf.BindGroup())
		gpuTerrainMat.Bind(pass)
		pass.SetBindGroup(render3d.ShadowGroup, sm.BindGroup())
		gpuTerrainMesh.Draw(pass)

		pass.SetPipeline(pillarPipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		pass.SetBindGroup(render3d.LightGroup, lightBuf.BindGroup())
		pass.SetBindGroup(render3d.PBRDrawGroup, pillarDraw.BindGroup())
		pass.SetBindGroup(render3d.ShadowGroup, sm.BindGroup())
		pillarMesh.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer error: %v", err)
		}
		return sr.Image()
	}

	unshadowed := renderScene(false)
	shadowed := renderScene(true)

	// Confirm the shadow pass visibly darkened terrain pixels under the monolith shadow.
	darkened := 0
	for y := range h {
		for x := range w {
			u := unshadowed.RGBAAt(x, y)
			s := shadowed.RGBAAt(x, y)
			if int(u.R)+int(u.G)+int(u.B)-(int(s.R)+int(s.G)+int(s.B)) > 45 {
				darkened++
			}
		}
	}
	if darkened < 40 {
		t.Fatalf("expected directional shadow to visibly darken terrain pixels, only %d pixels darkened", darkened)
	}

	terrainAssertGolden(t, "terrain_four_layer", shadowed)
}

// TestTerrainMaterialLifecycleCSMAndFog exercises UpdateUniforms, UpdateSplatMaps,
// CSM/Fog/LinearOutput pipeline variants, and nsgotest gpu.View rendering.
func TestTerrainMaterialLifecycleCSMAndFog(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	rig := render3d.LightRig{
		Ambient:     render3d.AmbientLight{Color: math3d.Vec3{X: 0.2, Y: 0.2, Z: 0.2}, Intensity: 1},
		Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{Y: -1}, Color: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: 2}},
	}
	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	mat := render3d.TerrainMaterial{
		Layers: []render3d.TerrainLayer{
			render3d.DefaultTerrainLayer("grass"),
			render3d.DefaultTerrainLayer("rock"),
		},
		SplatMap0: terrainSolidImage(4, 4, color.NRGBA{R: 255, G: 0, B: 0, A: 0}),
	}
	gpuMat, err := render3d.NewGPUTerrainMaterial(nil, dev, mat, render3d.TerrainMaterialUpload{Retained: true})
	if err != nil {
		t.Fatalf("NewGPUTerrainMaterial: %v", err)
	}
	t.Cleanup(func() { _ = gpuMat.Close() })

	// UpdateUniforms & UpdateSplatMaps succeed cleanly in place.
	mat.HeightBlendContrast = 0.5
	if err := gpuMat.UpdateUniforms(dev, mat); err != nil {
		t.Fatalf("UpdateUniforms: %v", err)
	}
	if err := gpuMat.UpdateSplatMaps(dev, terrainSolidImage(4, 4, color.NRGBA{R: 0, G: 255, B: 0, A: 0}), nil); err != nil {
		t.Fatalf("UpdateSplatMaps: %v", err)
	}
	// Mismatched layer count rejected.
	badMat := mat
	badMat.Layers = mat.Layers[:1]
	if err := gpuMat.UpdateUniforms(dev, badMat); !errors.Is(err, render3d.ErrInvalidTerrainMaterial) {
		t.Fatalf("expected ErrInvalidTerrainMaterial on layer count mismatch, got %v", err)
	}

	// Verify CSM, Fog, LinearOutput pipelines compile and construct cleanly, and conflicting group-3 layouts fail.
	csm, err := render3d.NewCascadedShadowMap(nil, dev, render3d.CSMConfig{CascadeCount: 2, Resolution: 64})
	if err != nil {
		t.Fatalf("NewCascadedShadowMap: %v", err)
	}
	t.Cleanup(func() { _ = csm.Close() })

	fogBuf, err := render3d.NewGPUFogBuffer(nil, dev, render3d.Fog{Mode: render3d.FogLinear, Start: 5, End: 50}, render3d.FogBufferOptions{})
	if err != nil {
		t.Fatalf("NewGPUFogBuffer: %v", err)
	}
	t.Cleanup(func() { _ = fogBuf.Close() })

	for _, tc := range []struct {
		name string
		opts render3d.TerrainPipelineOptions
	}{
		{"csm", render3d.TerrainPipelineOptions{ColorFormat: gpu.FormatRGBA8Unorm, DepthFormat: gpu.FormatDepth24Plus, CSMLayout: csm.Layout()}},
		{"csm_linear", render3d.TerrainPipelineOptions{ColorFormat: gpu.FormatRGBA16Float, DepthFormat: gpu.FormatDepth24Plus, CSMLayout: csm.Layout(), LinearOutput: true}},
		{"fog", render3d.TerrainPipelineOptions{ColorFormat: gpu.FormatRGBA8Unorm, DepthFormat: gpu.FormatDepth24Plus, FogLayout: fogBuf.Layout()}},
		{"fog_linear", render3d.TerrainPipelineOptions{ColorFormat: gpu.FormatRGBA16Float, DepthFormat: gpu.FormatDepth24Plus, FogLayout: fogBuf.Layout(), LinearOutput: true}},
		{"base_linear", render3d.TerrainPipelineOptions{ColorFormat: gpu.FormatRGBA16Float, DepthFormat: gpu.FormatDepth24Plus, LinearOutput: true}},
	} {
		p, err := render3d.NewTerrainMaterialPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), gpuMat.Layout(), tc.opts)
		if err != nil {
			t.Fatalf("NewTerrainMaterialPipeline(%s): %v", tc.name, err)
		}
		_ = p.Close()
	}
	if _, err := render3d.NewTerrainMaterialPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), gpuMat.Layout(), render3d.TerrainPipelineOptions{
		CSMLayout: csm.Layout(),
		FogLayout: fogBuf.Layout(),
	}); err == nil || !strings.Contains(err.Error(), "group 3") {
		t.Fatalf("expected conflict error for CSMLayout+FogLayout, got %v", err)
	}

	// Verify live gpu.View rendering via nsgotest (runs under both full and NoGPUCompute profiles).
	drew := false
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var (
			cBuf *render3d.CameraBuffer
			lBuf *render3d.GPULightBuffer
			tMat *render3d.GPUTerrainMaterial
			pipe *gpu.Pipeline
			mesh *render3d.GPUMesh
		)
		return gpu.View(cx, gpu.ViewProps{
			Style:       core.Style{Width: core.Pt(64), Height: core.Pt(64)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(d *gpu.Device) {
				var initErr error
				cBuf, initErr = render3d.NewCameraBuffer(cx, d)
				if initErr != nil {
					t.Errorf("OnInit CameraBuffer: %v", initErr)
					return
				}
				lBuf, initErr = render3d.NewLightBuffer(cx, d, rig, render3d.LightBufferOptions{Retained: true})
				if initErr != nil {
					t.Errorf("OnInit LightBuffer: %v", initErr)
					return
				}
				tMat, initErr = render3d.NewGPUTerrainMaterial(cx, d, mat, render3d.TerrainMaterialUpload{Retained: true})
				if initErr != nil {
					t.Errorf("OnInit GPUTerrainMaterial: %v", initErr)
					return
				}
				pipe, initErr = render3d.NewTerrainMaterialPipeline(cx, d, cBuf.Layout(), lBuf.Layout(), tMat.Layout(), render3d.TerrainPipelineOptions{
					DepthFormat: gpu.FormatDepth24Plus,
				})
				if initErr != nil {
					t.Errorf("OnInit pipeline: %v", initErr)
					return
				}
				mesh, initErr = render3d.UploadMesh(cx, d, terrainPlaneMesh(4), render3d.MeshUpload{Retained: true})
				if initErr != nil {
					t.Errorf("OnInit mesh: %v", initErr)
					return
				}
			},
			OnFrame: func(f *gpu.Frame) {
				if pipe == nil || !tMat.Valid() {
					return
				}
				cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Y: 4, Z: 4}, math3d.DegToRad(50), 1, 0.1, 30)
				cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
				cBuf.WriteCamera(cam)
				pass := f.BeginRenderPass(gpu.RenderPassDesc{
					ColorAttachments: []gpu.ColorAttachment{{
						LoadOp:     gpu.LoadClear,
						StoreOp:    gpu.StoreStore,
						ClearColor: core.RGB(0x101824),
					}},
					DepthStencil: &gpu.DepthStencilAttachment{
						DepthLoadOp:  gpu.LoadClear,
						DepthStoreOp: gpu.StoreDiscard,
						ClearDepth:   1.0,
					},
				})
				pass.SetPipeline(pipe)
				pass.SetBindGroup(render3d.CameraGroup, cBuf.BindGroup())
				pass.SetBindGroup(render3d.LightGroup, lBuf.BindGroup())
				tMat.Bind(pass)
				mesh.Draw(pass)
				pass.End()
				drew = true
			},
		})
	})
	app.Advance(100 * time.Millisecond)
	if !drew {
		t.Fatal("gpu.View did not draw a terrain frame")
	}
}

package render3d_test

import (
	"encoding/binary"
	"errors"
	"image"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const (
	localShadowTestWidth  = 128
	localShadowTestHeight = 96
)

var localShadowTestClear = core.RGB(0x0c1018)

// localShadowTestSubdividedQuad builds a grid of subdivisions x subdivisions triangles spanning center +/- uAxis*halfU +/- vAxis*halfV with outward normal. Subdividing large floors and walls ensures triangles never straddle w <= 0 of a point light's cube-face frustum in the software rasterizer.
func localShadowTestSubdividedQuad(center, uAxis, vAxis, normal math3d.Vec3, halfU, halfV float32, subdiv int) render3d.Mesh {
	var m render3d.Mesh
	n := normal.Norm()
	for iy := range subdiv {
		v0 := -halfV + 2*halfV*float32(iy)/float32(subdiv)
		v1 := -halfV + 2*halfV*float32(iy+1)/float32(subdiv)
		for ix := range subdiv {
			u0 := -halfU + 2*halfU*float32(ix)/float32(subdiv)
			u1 := -halfU + 2*halfU*float32(ix+1)/float32(subdiv)
			p00 := center.Add(uAxis.Scale(u0)).Add(vAxis.Scale(v0))
			p10 := center.Add(uAxis.Scale(u1)).Add(vAxis.Scale(v0))
			p11 := center.Add(uAxis.Scale(u1)).Add(vAxis.Scale(v1))
			p01 := center.Add(uAxis.Scale(u0)).Add(vAxis.Scale(v1))
			base := uint32(len(m.Positions))
			m.Positions = append(m.Positions, p00, p10, p11, p01)
			m.Normals = append(m.Normals, n, n, n, n)
			m.UVs = append(m.UVs,
				math3d.Vec2{X: 0, Y: 0},
				math3d.Vec2{X: 1, Y: 0},
				math3d.Vec2{X: 1, Y: 1},
				math3d.Vec2{X: 0, Y: 1},
			)
			m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
		}
	}
	return m
}

// localShadowTestBoxMesh builds a CCW outward-facing box mesh centered at center with half extents half.
func localShadowTestBoxMesh(center, half math3d.Vec3) render3d.Mesh {
	type face struct{ n, u, v math3d.Vec3 }
	faces := [...]face{
		{math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, math3d.Vec3{Y: 1}},
		{math3d.Vec3{X: -1}, math3d.Vec3{Z: 1}, math3d.Vec3{Y: 1}},
		{math3d.Vec3{Y: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}},
		{math3d.Vec3{Y: -1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: 1}},
		{math3d.Vec3{Z: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Y: 1}},
		{math3d.Vec3{Z: -1}, math3d.Vec3{X: -1}, math3d.Vec3{Y: 1}},
	}
	var m render3d.Mesh
	for _, f := range faces {
		base := uint32(len(m.Positions))
		c := center.Add(math3d.Vec3{X: f.n.X * half.X, Y: f.n.Y * half.Y, Z: f.n.Z * half.Z})
		u := math3d.Vec3{X: f.u.X * half.X, Y: f.u.Y * half.Y, Z: f.u.Z * half.Z}
		v := math3d.Vec3{X: f.v.X * half.X, Y: f.v.Y * half.Y, Z: f.v.Z * half.Z}
		m.Positions = append(m.Positions,
			c.Sub(u).Sub(v),
			c.Add(u).Sub(v),
			c.Add(u).Add(v),
			c.Sub(u).Add(v),
		)
		m.Normals = append(m.Normals, f.n, f.n, f.n, f.n)
		m.UVs = append(m.UVs,
			math3d.Vec2{X: 0, Y: 1},
			math3d.Vec2{X: 1, Y: 1},
			math3d.Vec2{X: 1, Y: 0},
			math3d.Vec2{X: 0, Y: 0},
		)
		m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	return m
}

func localShadowTestMergeMeshes(meshes ...render3d.Mesh) render3d.Mesh {
	var out render3d.Mesh
	for _, m := range meshes {
		base := uint32(len(out.Positions))
		out.Positions = append(out.Positions, m.Positions...)
		out.Normals = append(out.Normals, m.Normals...)
		out.UVs = append(out.UVs, m.UVs...)
		for _, idx := range m.Indices {
			out.Indices = append(out.Indices, base+idx)
		}
	}
	return out
}

func localShadowTestReadDepth(t *testing.T, dev *gpu.Device, tex *gpu.Texture, w, h uint32) []float32 {
	t.Helper()
	var raw []byte
	dev.ReadTexture(tex, gpu.TextureRegion{Width: w, Height: h}, func(b []byte, err error) {
		if err != nil {
			t.Fatalf("ReadTexture depth: %v", err)
		}
		raw = append([]byte(nil), b...)
	})
	if len(raw) != int(w*h*4) {
		t.Fatalf("depth readback len = %d, want %d", len(raw), w*h*4)
	}
	return render3d.DecodeDepth32Float(raw)
}

func localShadowTestColorToRGB8(c math3d.Vec3) [3]uint8 {
	conv := func(v float32) uint8 {
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint8(v*255.0 + 0.5)
	}
	return [3]uint8{conv(c.X), conv(c.Y), conv(c.Z)}
}

func localShadowTestAssertGolden(t *testing.T, name string, got *image.RGBA) {
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
		t.Fatalf("%s: bounds %v, want %v", name, got.Bounds(), want.Bounds())
	}
	const channelTol = 3
	far := 0
	b := got.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g := got.RGBAAt(x, y)
			wr, wg, wb, wa := want.At(x, y).RGBA()
			w := [4]int{int(wr >> 8), int(wg >> 8), int(wb >> 8), int(wa >> 8)}
			for i, c := range [4]int{int(g.R), int(g.G), int(g.B), int(g.A)} {
				if d := c - w[i]; d > channelTol || d < -channelTol {
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
		t.Fatalf("%s: %d pixels differ from golden (limit %d); wrote got & diff to %s", name, far, limit, dir)
	}
}

func localShadowTestSamplePixel(img *image.RGBA, cam *scene3d.Camera, world math3d.Vec3) ([3]uint8, int, int, bool) {
	b := img.Bounds()
	s, _, ok := cam.WorldToScreen(world, float32(b.Dx()), float32(b.Dy()))
	if !ok {
		return [3]uint8{}, 0, 0, false
	}
	x, y := int(s.X), int(s.Y)
	if x < 0 || x >= b.Dx() || y < 0 || y >= b.Dy() {
		return [3]uint8{}, x, y, false
	}
	c := img.RGBAAt(x, y)
	return [3]uint8{c.R, c.G, c.B}, x, y, true
}

func localShadowTestRGBDiff(a, b [3]uint8) int {
	maxD := 0
	for i := range 3 {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		if d > maxD {
			maxD = d
		}
	}
	return maxD
}

var (
	localShadowFloorMat  = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.85, Y: 0.85, Z: 0.85}, Roughness: 0.9}
	localShadowPillarMat = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.88, Y: 0.42, Z: 0.30}, Roughness: 0.6}
	localShadowAmbient   = render3d.AmbientLight{Color: math3d.Vec3{X: 0.08, Y: 0.09, Z: 0.12}, Intensity: 1}

	localShadowTestSpot = render3d.SpotLight{
		Position:       math3d.Vec3{X: -2.4, Y: 3.6, Z: 2.3},
		Direction:      math3d.Vec3{X: 2.5, Y: -3.5, Z: -2.5},
		Color:          math3d.Vec3{X: 1.0, Y: 0.92, Z: 0.78},
		Intensity:      45,
		Range:          14,
		InnerConeAngle: math3d.DegToRad(26),
		OuterConeAngle: math3d.DegToRad(44),
	}
	localShadowTestPoint = render3d.PointLight{
		Position:  math3d.Vec3{X: 0.0, Y: 1.8, Z: 0.0},
		Color:     math3d.Vec3{X: 1.0, Y: 0.94, Z: 0.85},
		Intensity: 25,
		Range:     12,
	}
)

// localShadowExpected returns the sRGB 8-bit color the lit PBR local shadow pipelines draw at wp for material mat under rig with visibility vis, from the CPU references ShadePointVisibility and LinearToSRGB.
func localShadowExpected(cam *scene3d.Camera, wp, n math3d.Vec3, mat render3d.PBRMaterial, rig render3d.LightRig, vis render3d.LightVisibility) [3]uint8 {
	e := cam.Eye()
	eye := math3d.Vec3{X: e.X, Y: e.Y, Z: e.Z}
	return localShadowTestColorToRGB8(render3d.LinearToSRGB(render3d.ShadePointVisibility(wp, n, eye.Sub(wp), mat, rig, vis)))
}

// localShadowDraw is one receiver drawn in the lit pass: a mesh with its own per-draw model matrix and material.
type localShadowDraw struct {
	mesh *render3d.GPUMesh
	buf  *render3d.PBRDrawBuffer
}

func localShadowDrawBuffers(t *testing.T, cx *core.Scope, dev *gpu.Device, retained bool, meshes []*render3d.GPUMesh, uniforms []render3d.PBRDrawUniform) []localShadowDraw {
	t.Helper()
	var layout *gpu.BindGroupLayout
	draws := make([]localShadowDraw, len(meshes))
	for i, u := range uniforms {
		db, err := render3d.NewPBRDrawBuffer(cx, dev, layout, u, retained)
		if err != nil {
			t.Fatalf("NewPBRDrawBuffer[%d]: %v", i, err)
		}
		if layout == nil {
			layout = db.Layout()
		}
		draws[i] = localShadowDraw{mesh: meshes[i], buf: db}
	}
	if cx == nil {
		t.Cleanup(func() {
			for _, d := range draws {
				_ = d.buf.Close()
			}
		})
	}
	return draws
}

func localShadowUpload(t *testing.T, cx *core.Scope, dev *gpu.Device, label string, m render3d.Mesh, retained bool) *render3d.GPUMesh {
	t.Helper()
	gm, err := render3d.UploadMesh(cx, dev, m, render3d.MeshUpload{Label: label, Retained: retained})
	if err != nil {
		t.Fatalf("UploadMesh %s: %v", label, err)
	}
	if cx == nil {
		t.Cleanup(func() { _ = gm.Close() })
	}
	return gm
}

func localShadowMainPass(target render3d.ShadowPassTarget) *gpu.RenderPass {
	return target.BeginRenderPass(gpu.RenderPassDesc{
		Label: "local-shadow-main",
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: localShadowTestClear,
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1.0,
		},
	})
}

func localShadowDrawReceivers(pass *gpu.RenderPass, pipe *gpu.Pipeline, camBuf *render3d.CameraBuffer, lightBuf *render3d.GPULightBuffer, shadow *gpu.BindGroup, draws []localShadowDraw) {
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	pass.SetBindGroup(render3d.LightGroup, lightBuf.BindGroup())
	pass.SetBindGroup(render3d.LocalShadowGroup, shadow)
	for _, d := range draws {
		pass.SetBindGroup(render3d.PBRDrawGroup, d.buf.BindGroup())
		d.mesh.Draw(pass)
	}
}

func TestLocalShadowConfigValidation(t *testing.T) {
	spotDef := render3d.DefaultSpotShadowConfig()
	if err := spotDef.Validate(); err != nil {
		t.Fatalf("DefaultSpotShadowConfig.Validate: %v", err)
	}
	if spotDef.Resolution != 1024 || spotDef.Filter != render3d.LocalShadowFilterPCF3x3 || spotDef.Near != render3d.DefaultLocalShadowNear || spotDef.Far != render3d.DefaultLocalShadowFar {
		t.Fatalf("unexpected default spot config: %+v", spotDef)
	}
	if render3d.LocalShadowFilterHard.String() != "hard" || render3d.LocalShadowFilterPCF3x3.String() != "pcf3x3" {
		t.Fatal("unexpected filter string names")
	}
	if render3d.LocalShadowGroup != render3d.ShadowGroup {
		t.Fatalf("LocalShadowGroup = %d, want ShadowGroup %d", render3d.LocalShadowGroup, render3d.ShadowGroup)
	}

	badSpot := []render3d.SpotShadowConfig{
		{DepthBias: -0.01},
		{DepthBias: float32(math.NaN())},
		{NormalBias: -0.1},
		{NormalBias: float32(math.Inf(1))},
		{Filter: render3d.LocalShadowFilter(99)},
		{CullMode: gpu.CullMode(99)},
		{Near: -1},
		{Near: 5, Far: 4},
		{Far: float32(math.Inf(1))},
		{MaxCasters: render3d.MaxLocalShadowCasters + 1},
	}
	for i, cfg := range badSpot {
		if err := cfg.Validate(); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
			t.Errorf("badSpot[%d] = %v, want ErrInvalidLocalShadow", i, err)
		}
	}

	pointDef := render3d.DefaultPointShadowConfig()
	if err := pointDef.Validate(); err != nil {
		t.Fatalf("DefaultPointShadowConfig.Validate: %v", err)
	}
	if aw, ah := pointDef.AtlasSize(); aw != 3*512 || ah != 2*512 {
		t.Fatalf("default atlas size = %dx%d, want %dx%d", aw, ah, 3*512, 2*512)
	}
	badPoint := []render3d.PointShadowConfig{
		{DepthBias: -0.01},
		{NormalBias: float32(math.NaN())},
		{Filter: render3d.LocalShadowFilter(42)},
		{CullMode: gpu.CullMode(42)},
		{Near: 2, Far: 1},
		{MaxCasters: render3d.MaxLocalShadowCasters + 1},
	}
	for i, cfg := range badPoint {
		if err := cfg.Validate(); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
			t.Errorf("badPoint[%d] = %v, want ErrInvalidLocalShadow", i, err)
		}
	}

	if _, err := render3d.NewSpotShadowMap(nil, nil, spotDef); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("spot nil device error = %v, want gpu.ErrDeviceLost", err)
	}
	if _, err := render3d.NewPointShadowMap(nil, nil, pointDef); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("point nil device error = %v, want gpu.ErrDeviceLost", err)
	}
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	if _, err := render3d.NewSpotShadowMap(nil, dev, render3d.SpotShadowConfig{Resolution: dev.Limits().MaxTextureDimension2D + 1}); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
		t.Fatalf("oversized spot resolution error = %v, want ErrInvalidLocalShadow", err)
	}
	if _, err := render3d.NewPointShadowMap(nil, dev, render3d.PointShadowConfig{FaceResolution: dev.Limits().MaxTextureDimension2D/3 + 1}); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
		t.Fatalf("oversized point atlas error = %v, want ErrInvalidLocalShadow", err)
	}
	if _, err := render3d.NewSpotShadowMap(nil, dev, render3d.SpotShadowConfig{Resolution: 16, MaxCasters: render3d.MaxLocalShadowCasters + 1}); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
		t.Fatalf("spot MaxCasters above cap error = %v, want ErrInvalidLocalShadow", err)
	}
}

func TestLocalShadowUniformsFromLightRigLights(t *testing.T) {
	cfg := render3d.SpotShadowConfig{Resolution: 256, Near: 0.2, Far: 30, DepthBias: 0.002, NormalBias: 0.03, Filter: render3d.LocalShadowFilterPCF3x3}

	// A spot light with a Range projects to that range; one with Range 0 (unbounded) projects to cfg.Far.
	u, err := render3d.NewSpotShadowUniform(localShadowTestSpot, cfg)
	if err != nil {
		t.Fatalf("NewSpotShadowUniform: %v", err)
	}
	if want := render3d.SpotShadowViewProj(localShadowTestSpot, 0.2, localShadowTestSpot.Range); u.ViewProj != want {
		t.Fatalf("ViewProj with Range = %v, want %v", u.ViewProj, want)
	}
	unbounded := localShadowTestSpot
	unbounded.Range = 0
	u0, err := render3d.NewSpotShadowUniform(unbounded, cfg)
	if err != nil {
		t.Fatalf("NewSpotShadowUniform(Range 0): %v", err)
	}
	if want := render3d.SpotShadowViewProj(unbounded, 0.2, 30); u0.ViewProj != want {
		t.Fatalf("ViewProj with Range 0 = %v, want far plane %v", u0.ViewProj, cfg.Far)
	}
	// The cone apex maps to the shadow map center and the cone edge to its border.
	dir := localShadowTestSpot.Direction.Norm()
	center := u.ViewProj.MulVec4(localShadowTestSpot.Position.Add(dir.Scale(5)).Vec4(1))
	if math.Abs(float64(center.X/center.W)) > 1e-4 || math.Abs(float64(center.Y/center.W)) > 1e-4 {
		t.Fatalf("cone axis projects to (%v, %v), want (0, 0)", center.X/center.W, center.Y/center.W)
	}

	wide := localShadowTestSpot
	wide.OuterConeAngle = math.Pi / 2 // valid for a LightRig, too wide for one perspective map
	if _, err := render3d.NewSpotShadowUniform(wide, cfg); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
		t.Fatalf("wide cone error = %v, want ErrInvalidLocalShadow", err)
	}
	short := localShadowTestSpot
	short.Range = 0.1
	if _, err := render3d.NewSpotShadowUniform(short, cfg); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
		t.Fatalf("Range below Near error = %v, want ErrInvalidLocalShadow", err)
	}
	broken := localShadowTestSpot
	broken.Direction = math3d.Vec3{}
	if _, err := render3d.NewSpotShadowUniform(broken, cfg); !errors.Is(err, render3d.ErrInvalidLight) {
		t.Fatalf("zero direction error = %v, want ErrInvalidLight", err)
	}

	pcfg := render3d.PointShadowConfig{FaceResolution: 128, Near: 0.15, Far: 40, DepthBias: 0.001, NormalBias: 0.02}
	pu, err := render3d.NewPointShadowUniform(localShadowTestPoint, pcfg)
	if err != nil {
		t.Fatalf("NewPointShadowUniform: %v", err)
	}
	if pu.Far != localShadowTestPoint.Range || pu.Near != 0.15 || pu.Position != localShadowTestPoint.Position {
		t.Fatalf("point uniform = %+v, want Far = Range", pu)
	}
	pl := localShadowTestPoint
	pl.Range = 0
	if pu0, err := render3d.NewPointShadowUniform(pl, pcfg); err != nil || pu0.Far != 40 {
		t.Fatalf("NewPointShadowUniform(Range 0) = %+v, %v; want Far 40", pu0, err)
	}
	pl.Range = 0.1
	if _, err := render3d.NewPointShadowUniform(pl, pcfg); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
		t.Fatalf("point Range below Near error = %v, want ErrInvalidLocalShadow", err)
	}
	pl.Intensity = -1
	if _, err := render3d.NewPointShadowUniform(pl, pcfg); !errors.Is(err, render3d.ErrInvalidLight) {
		t.Fatalf("invalid point light error = %v, want ErrInvalidLight", err)
	}
}

func TestLocalShadowUniformEncodeZeroAlloc(t *testing.T) {
	u, err := render3d.NewSpotShadowUniform(localShadowTestSpot, render3d.SpotShadowConfig{Resolution: 256, DepthBias: 0.002, NormalBias: 0.03, Filter: render3d.LocalShadowFilterPCF3x3})
	if err != nil {
		t.Fatal(err)
	}
	var buf [render3d.SpotShadowUniformSize + 8]byte
	out := u.Encode(buf[:])
	if len(out) != render3d.SpotShadowUniformSize || render3d.SpotShadowUniformSize != 80 {
		t.Fatalf("spot encoded %d bytes, want 80", len(out))
	}
	f := func(b []byte, i int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:])) }
	for i := range 16 {
		if f(out, i) != u.ViewProj[i] {
			t.Fatalf("ViewProj[%d] = %v, want %v", i, f(out, i), u.ViewProj[i])
		}
	}
	if f(out, 16) != 0.002 || f(out, 17) != 0.03 || f(out, 18) != 1.0/256.0 || f(out, 19) != 1 {
		t.Fatalf("spot params = %v %v %v %v", f(out, 16), f(out, 17), f(out, 18), f(out, 19))
	}
	if allocs := testing.AllocsPerRun(100, func() { u.Encode(buf[:]) }); allocs != 0 {
		t.Fatalf("SpotShadowUniform.Encode allocated %v times", allocs)
	}

	pu, err := render3d.NewPointShadowUniform(render3d.PointLight{Position: math3d.Vec3{X: 1, Y: 2, Z: -3}, Color: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: 3, Range: 14}, render3d.PointShadowConfig{FaceResolution: 128, Near: 0.15, DepthBias: 0.001, NormalBias: 0.02, Filter: render3d.LocalShadowFilterHard})
	if err != nil {
		t.Fatal(err)
	}
	var pbuf [render3d.PointShadowUniformSize + 8]byte
	pout := pu.Encode(pbuf[:])
	if len(pout) != render3d.PointShadowUniformSize || render3d.PointShadowUniformSize != 48 {
		t.Fatalf("point encoded %d bytes, want 48", len(pout))
	}
	want := [12]float32{1, 2, -3, 14, 0.001, 0.02, 1.0 / 128.0, 0, 0.15, 0, 0, 0}
	for i, w := range want {
		if f(pout, i) != w {
			t.Fatalf("point float %d = %v, want %v", i, f(pout, i), w)
		}
	}
	if allocs := testing.AllocsPerRun(100, func() { pu.Encode(pbuf[:]) }); allocs != 0 {
		t.Fatalf("PointShadowUniform.Encode allocated %v times", allocs)
	}

	for name, enc := range map[string]func(){
		"spot":  func() { u.Encode(make([]byte, render3d.SpotShadowUniformSize-1)) },
		"point": func() { pu.Encode(make([]byte, render3d.PointShadowUniformSize-1)) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s Encode with short slice did not panic", name)
				}
			}()
			enc()
		}()
	}
}

func TestPointShadowFaceProjectionConsistency(t *testing.T) {
	u, err := render3d.NewPointShadowUniform(render3d.PointLight{Position: math3d.Vec3{X: 1.5, Y: -0.75, Z: 2.25}, Color: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: 1, Range: 16}, render3d.PointShadowConfig{Near: 0.2})
	if err != nil {
		t.Fatal(err)
	}
	probes := []struct {
		offset   math3d.Vec3
		wantFace gpu.CubeFace
	}{
		{math3d.Vec3{X: 4.0, Y: 1.2, Z: -1.7}, gpu.CubeFacePosX},
		{math3d.Vec3{X: -3.5, Y: -0.8, Z: 1.4}, gpu.CubeFaceNegX},
		{math3d.Vec3{X: 0.9, Y: 5.0, Z: -1.3}, gpu.CubeFacePosY},
		{math3d.Vec3{X: -1.1, Y: -4.2, Z: 1.8}, gpu.CubeFaceNegY},
		{math3d.Vec3{X: 1.2, Y: -0.6, Z: 3.8}, gpu.CubeFacePosZ},
		{math3d.Vec3{X: -1.3, Y: 1.5, Z: -4.6}, gpu.CubeFaceNegZ},
	}
	for _, tc := range probes {
		face, fu, fv, axisDist := render3d.PointShadowSelectFace(tc.offset)
		if face != tc.wantFace {
			t.Fatalf("offset %v selected face %d, want %d", tc.offset, face, tc.wantFace)
		}
		clip := u.FaceViewProj(face).MulVec4(u.Position.Add(tc.offset).Vec4(1))
		if clip.W <= 0 {
			t.Fatalf("face %d clip.W = %v, want > 0", face, clip.W)
		}
		gotU := clip.X/clip.W*0.5 + 0.5
		gotV := 0.5 - clip.Y/clip.W*0.5
		wantZ := (u.Far / (u.Far - u.Near)) * (1.0 - u.Near/axisDist)
		if math.Abs(float64(fu-gotU)) > 1e-5 || math.Abs(float64(fv-gotV)) > 1e-5 {
			t.Errorf("face %d UV mismatch: select=(%f,%f), proj=(%f,%f)", face, fu, fv, gotU, gotV)
		}
		if math.Abs(float64(clip.Z/clip.W-wantZ)) > 1e-5 {
			t.Errorf("face %d depth mismatch: analytic=%f, proj=%f", face, wantZ, clip.Z/clip.W)
		}
	}
}

type localShadowSpotResult struct {
	img     *image.RGBA
	depth   []float32
	uniform render3d.SpotShadowUniform
	cam     *scene3d.Camera
	rig     render3d.LightRig
}

// localShadowTestRenderSpotScene draws a floor and two pillars, all with their own model matrices, lit by the first spot light of a LightRig through LitPBRSpotShadowWGSL.
func localShadowTestRenderSpotScene(t *testing.T, filter render3d.LocalShadowFilter) localShadowSpotResult {
	t.Helper()
	const shadowRes = 128

	sr := gpu.NewSoftwareRenderer(localShadowTestWidth, localShadowTestHeight, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	rig := render3d.LightRig{Ambient: localShadowAmbient, Spot: []render3d.SpotLight{localShadowTestSpot}}
	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{Label: "spot-lights"})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	spotShadow, err := render3d.NewSpotShadowMap(nil, dev, render3d.SpotShadowConfig{
		Resolution: shadowRes,
		Near:       0.2,
		DepthBias:  0.002,
		NormalBias: 0.025,
		Filter:     filter,
		Label:      "test.spot",
	})
	if err != nil {
		t.Fatalf("NewSpotShadowMap: %v", err)
	}
	t.Cleanup(func() { _ = spotShadow.Close() })

	floor := localShadowUpload(t, nil, dev, "floor", localShadowTestSubdividedQuad(math3d.Vec3Zero(), math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, math3d.Vec3Up(), 4.5, 4.5, 6), false)
	unitBox := localShadowUpload(t, nil, dev, "unit-box", localShadowTestBoxMesh(math3d.Vec3Zero(), math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}), false)
	pillar1Model := math3d.Translate(math3d.Vec3{X: -0.2, Y: 0.9, Z: 0.1}).Mul(math3d.Scale(math3d.Vec3{X: 0.9, Y: 1.8, Z: 0.9}))
	pillar2Model := math3d.Translate(math3d.Vec3{X: 1.15, Y: 0.65, Z: -0.95}).Mul(math3d.Scale(math3d.Vec3{X: 0.7, Y: 1.3, Z: 0.7}))
	draws := localShadowDrawBuffers(t, nil, dev, false,
		[]*render3d.GPUMesh{floor, unitBox, unitBox},
		[]render3d.PBRDrawUniform{
			render3d.NewPBRDrawUniform(math3d.Mat4Identity(), localShadowFloorMat),
			render3d.NewPBRDrawUniform(pillar1Model, localShadowPillarMat),
			render3d.NewPBRDrawUniform(pillar2Model, localShadowPillarMat),
		})

	litPipe, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), draws[0].buf.Layout(), render3d.LitPBRPipelineOptions{
		ColorFormat:      gpu.FormatRGBA8Unorm,
		DepthFormat:      gpu.FormatDepth24Plus,
		SpotShadowLayout: spotShadow.Layout(),
	})
	if err != nil {
		t.Fatalf("NewLitPBRPipelineWithOptions(SpotShadowLayout): %v", err)
	}
	t.Cleanup(func() { _ = litPipe.Close() })

	if err := spotShadow.Update(dev, localShadowTestSpot); err != nil {
		t.Fatalf("spotShadow.Update: %v", err)
	}
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0.4, Y: 4.8, Z: 5.0}, math3d.DegToRad(48), float32(localShadowTestWidth)/float32(localShadowTestHeight), 0.1, 30)
	cam.LookAt(math3d.Vec3{X: 0.35, Y: 0.3, Z: -0.35}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	sp := spotShadow.BeginShadowPass(dev, enc)
	if sp == nil {
		t.Fatal("BeginShadowPass returned nil")
	}
	for i, m := range []math3d.Mat4{pillar1Model, pillar2Model} {
		if !spotShadow.DrawCaster(sp, unitBox, m) {
			t.Fatalf("DrawCaster %d returned false", i)
		}
	}
	sp.End()
	mp := localShadowMainPass(enc)
	localShadowDrawReceivers(mp, litPipe, camBuf, lightBuf, spotShadow.BindGroup(), draws)
	mp.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer error: %v", err)
	}

	depth := localShadowTestReadDepth(t, dev, spotShadow.DepthTexture(), shadowRes, shadowRes)
	return localShadowSpotResult{img: sr.Image(), depth: depth, uniform: spotShadow.Uniforms(), cam: cam, rig: rig}
}

func TestSpotShadowMatchesReferenceAndGolden(t *testing.T) {
	pcf := localShadowTestRenderSpotScene(t, render3d.LocalShadowFilterPCF3x3)
	hard := localShadowTestRenderSpotScene(t, render3d.LocalShadowFilterHard)

	localShadowTestAssertGolden(t, "local_shadow_spot", pcf.img)

	probes := []struct {
		name      string
		world     math3d.Vec3
		wantVisLo float32
		wantVisHi float32
	}{
		{"lit-floor-in-front", math3d.Vec3{X: -1.1, Y: 0, Z: 1.0}, 0.95, 1.0},
		{"shadowed-floor-behind-pillar1", math3d.Vec3{X: 0.7, Y: 0, Z: -0.7}, 0.0, 0.05},
		{"shadowed-floor-behind-pillar2", math3d.Vec3{X: 1.7, Y: 0, Z: -1.5}, 0.0, 0.05},
		{"lit-floor-side", math3d.Vec3{X: -1.4, Y: 0, Z: -0.9}, 0.95, 1.0},
	}
	up := math3d.Vec3Up()
	for _, r := range []struct {
		name string
		res  localShadowSpotResult
	}{{"pcf", pcf}, {"hard", hard}} {
		for _, tc := range probes {
			vis := render3d.EvaluateSpotShadow(r.res.uniform, r.res.depth, 128, tc.world, up)
			if vis < tc.wantVisLo || vis > tc.wantVisHi {
				t.Errorf("%s %s: CPU visibility = %.3f, want in [%.2f, %.2f]", r.name, tc.name, vis, tc.wantVisLo, tc.wantVisHi)
			}
			want := localShadowExpected(r.res.cam, tc.world, up, localShadowFloorMat, r.res.rig, render3d.LightVisibility{Directional0: 1, Point0: 1, Spot0: vis})
			got, px, py, ok := localShadowTestSamplePixel(r.res.img, r.res.cam, tc.world)
			if !ok {
				t.Fatalf("%s: probe %v outside view", tc.name, tc.world)
			}
			if diff := localShadowTestRGBDiff(got, want); diff > 10 {
				t.Errorf("%s %s at (%d,%d): GPU=%v vs CPU=%v (max diff %d)", r.name, tc.name, px, py, got, want, diff)
			}
		}
	}

	// The shadow removes the spot light's contribution: lit floor is markedly brighter than floor behind either pillar.
	litPx, _, _, _ := localShadowTestSamplePixel(pcf.img, pcf.cam, math3d.Vec3{X: -1.1, Y: 0, Z: 1.0})
	shad1Px, _, _, _ := localShadowTestSamplePixel(pcf.img, pcf.cam, math3d.Vec3{X: 0.7, Y: 0, Z: -0.7})
	shad2Px, _, _, _ := localShadowTestSamplePixel(pcf.img, pcf.cam, math3d.Vec3{X: 1.7, Y: 0, Z: -1.5})
	if int(litPx[0]) < int(shad1Px[0])+80 || int(litPx[0]) < int(shad2Px[0])+80 {
		t.Fatalf("expected strong shadow contrast: lit=%v shad1=%v shad2=%v", litPx, shad1Px, shad2Px)
	}

	// Hard shadows are binary; PCF 3x3 produces a penumbra along the shadow edge.
	pcfPenumbra := 0
	for i := range 60 {
		pt := math3d.Vec3{X: float32(-0.5) + float32(i)*0.04, Y: 0, Z: -0.7}
		if hv := render3d.EvaluateSpotShadow(hard.uniform, hard.depth, 128, pt, up); hv != 0 && hv != 1 {
			t.Fatalf("hard shadow returned non-binary visibility %v at %v", hv, pt)
		}
		if pv := render3d.EvaluateSpotShadow(pcf.uniform, pcf.depth, 128, pt, up); pv > 0.05 && pv < 0.95 {
			pcfPenumbra++
		}
	}
	if pcfPenumbra == 0 {
		t.Error("expected PCF3x3 to produce intermediate penumbra taps along the shadow boundary")
	}
}

type localShadowPointResult struct {
	img     *image.RGBA
	depth   []float32
	uniform render3d.PointShadowUniform
	cam     *scene3d.Camera
	rig     render3d.LightRig
}

// localShadowTestRenderPointScene draws an open room corner with three blockers lit by the first point light of a LightRig through LitPBRPointShadowWGSL.
func localShadowTestRenderPointScene(t *testing.T, filter render3d.LocalShadowFilter) localShadowPointResult {
	t.Helper()
	const faceRes = 64

	sr := gpu.NewSoftwareRenderer(localShadowTestWidth, localShadowTestHeight, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	rig := render3d.LightRig{Ambient: localShadowAmbient, Point: []render3d.PointLight{localShadowTestPoint}}
	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{Label: "point-lights"})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	pointShadow, err := render3d.NewPointShadowMap(nil, dev, render3d.PointShadowConfig{
		FaceResolution: faceRes,
		Near:           0.15,
		DepthBias:      0.0025,
		NormalBias:     0.03,
		Filter:         filter,
		Label:          "test.point",
	})
	if err != nil {
		t.Fatalf("NewPointShadowMap: %v", err)
	}
	t.Cleanup(func() { _ = pointShadow.Close() })

	floor := localShadowTestSubdividedQuad(math3d.Vec3Zero(), math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, math3d.Vec3Up(), 3.2, 3.2, 8)
	backWall := localShadowTestSubdividedQuad(math3d.Vec3{Y: 2.0, Z: -3.0}, math3d.Vec3{X: 1}, math3d.Vec3{Y: 1}, math3d.Vec3{Z: 1}, 3.2, 2.0, 6)
	rightWall := localShadowTestSubdividedQuad(math3d.Vec3{X: 3.0, Y: 2.0}, math3d.Vec3{Z: 1}, math3d.Vec3{Y: 1}, math3d.Vec3{X: -1}, 3.2, 2.0, 6)
	room := localShadowUpload(t, nil, dev, "room", localShadowTestMergeMeshes(floor, backWall, rightWall), false)
	unitBox := localShadowUpload(t, nil, dev, "unit-box", localShadowTestBoxMesh(math3d.Vec3Zero(), math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}), false)

	// Three blockers along distinct cube faces (+X, -Z and -Y relative to the light at (0, 1.8, 0)).
	blockers := []math3d.Mat4{
		math3d.Translate(math3d.Vec3{X: 1.45, Y: 1.45, Z: 0.1}).Mul(math3d.Scale(math3d.Vec3{X: 0.7, Y: 1.1, Z: 0.9})),
		math3d.Translate(math3d.Vec3{X: -0.1, Y: 1.45, Z: -1.45}).Mul(math3d.Scale(math3d.Vec3{X: 0.9, Y: 1.1, Z: 0.7})),
		math3d.Translate(math3d.Vec3{X: -0.85, Y: 0.75, Z: 0.75}).Mul(math3d.Scale(math3d.Vec3{X: 0.9, Y: 0.7, Z: 0.9})),
	}
	meshes := []*render3d.GPUMesh{room}
	uniforms := []render3d.PBRDrawUniform{render3d.NewPBRDrawUniform(math3d.Mat4Identity(), localShadowFloorMat)}
	for _, m := range blockers {
		meshes = append(meshes, unitBox)
		uniforms = append(uniforms, render3d.NewPBRDrawUniform(m, localShadowPillarMat))
	}
	draws := localShadowDrawBuffers(t, nil, dev, false, meshes, uniforms)

	litPipe, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), draws[0].buf.Layout(), render3d.LitPBRPipelineOptions{
		ColorFormat:       gpu.FormatRGBA8Unorm,
		DepthFormat:       gpu.FormatDepth24Plus,
		PointShadowLayout: pointShadow.Layout(),
	})
	if err != nil {
		t.Fatalf("NewLitPBRPipelineWithOptions(PointShadowLayout): %v", err)
	}
	t.Cleanup(func() { _ = litPipe.Close() })

	if err := pointShadow.Update(dev, localShadowTestPoint); err != nil {
		t.Fatalf("pointShadow.Update: %v", err)
	}
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: -3.8, Y: 4.3, Z: 4.4}, math3d.DegToRad(50), float32(localShadowTestWidth)/float32(localShadowTestHeight), 0.1, 30)
	cam.LookAt(math3d.Vec3{X: 0.5, Y: 1.0, Z: -0.5}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	for face := range gpu.CubeFace(render3d.PointShadowFaceCount) {
		fp := pointShadow.BeginFacePass(dev, enc, face)
		if fp == nil {
			t.Fatalf("BeginFacePass(%d) returned nil", face)
		}
		for i, m := range blockers {
			if !pointShadow.DrawCaster(fp, unitBox, m) {
				t.Fatalf("face %d DrawCaster %d returned false", face, i)
			}
		}
		fp.End()
	}
	mp := localShadowMainPass(enc)
	localShadowDrawReceivers(mp, litPipe, camBuf, lightBuf, pointShadow.BindGroup(), draws)
	mp.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer error: %v", err)
	}

	depth := localShadowTestReadDepth(t, dev, pointShadow.DepthTexture(), pointShadow.AtlasWidth(), pointShadow.AtlasHeight())
	return localShadowPointResult{img: sr.Image(), depth: depth, uniform: pointShadow.Uniforms(), cam: cam, rig: rig}
}

func TestPointShadowMatchesReferenceAndGolden(t *testing.T) {
	pcf := localShadowTestRenderPointScene(t, render3d.LocalShadowFilterPCF3x3)
	hard := localShadowTestRenderPointScene(t, render3d.LocalShadowFilterHard)

	localShadowTestAssertGolden(t, "local_shadow_point", pcf.img)

	probes := []struct {
		name      string
		world     math3d.Vec3
		normal    math3d.Vec3
		wantFace  gpu.CubeFace
		wantVisLo float32
		wantVisHi float32
	}{
		{"right-wall-shadow-posX", math3d.Vec3{X: 3.0, Y: 1.1, Z: 0.2}, math3d.Vec3{X: -1}, gpu.CubeFacePosX, 0.0, 0.05},
		{"right-wall-lit-posX", math3d.Vec3{X: 3.0, Y: 2.8, Z: -1.8}, math3d.Vec3{X: -1}, gpu.CubeFacePosX, 0.95, 1.0},
		{"back-wall-shadow-negZ", math3d.Vec3{X: -0.2, Y: 1.1, Z: -3.0}, math3d.Vec3{Z: 1}, gpu.CubeFaceNegZ, 0.0, 0.05},
		{"back-wall-lit-negZ", math3d.Vec3{X: -2.0, Y: 2.6, Z: -3.0}, math3d.Vec3{Z: 1}, gpu.CubeFaceNegZ, 0.95, 1.0},
		{"floor-shadow-negY", math3d.Vec3{X: -1.45, Y: 0.0, Z: 1.3}, math3d.Vec3Up(), gpu.CubeFaceNegY, 0.0, 0.05},
		{"floor-lit-front", math3d.Vec3{X: 0.8, Y: 0.0, Z: 0.8}, math3d.Vec3Up(), gpu.CubeFaceNegY, 0.95, 1.0},
	}
	for _, r := range []struct {
		name string
		res  localShadowPointResult
	}{{"pcf", pcf}, {"hard", hard}} {
		for _, tc := range probes {
			if gotFace, _, _, _ := render3d.PointShadowSelectFace(tc.world.Sub(r.res.uniform.Position)); gotFace != tc.wantFace {
				t.Errorf("%s: cube face = %d, want %d", tc.name, gotFace, tc.wantFace)
			}
			vis := render3d.EvaluatePointShadow(r.res.uniform, r.res.depth, 64, tc.world, tc.normal)
			if vis < tc.wantVisLo || vis > tc.wantVisHi {
				t.Errorf("%s %s: CPU visibility = %.3f, want in [%.2f, %.2f]", r.name, tc.name, vis, tc.wantVisLo, tc.wantVisHi)
			}
			want := localShadowExpected(r.res.cam, tc.world, tc.normal, localShadowFloorMat, r.res.rig, render3d.LightVisibility{Directional0: 1, Point0: vis, Spot0: 1})
			got, px, py, ok := localShadowTestSamplePixel(r.res.img, r.res.cam, tc.world)
			if !ok {
				t.Fatalf("%s: probe %v outside view", tc.name, tc.world)
			}
			if diff := localShadowTestRGBDiff(got, want); diff > 12 {
				t.Errorf("%s %s at (%d,%d): GPU=%v vs CPU=%v (max diff %d)", r.name, tc.name, px, py, got, want, diff)
			}
		}
	}

	pairs := []struct {
		name      string
		lit, shad math3d.Vec3
	}{
		{"+X wall", math3d.Vec3{X: 3.0, Y: 2.8, Z: -1.8}, math3d.Vec3{X: 3.0, Y: 1.1, Z: 0.2}},
		{"-Z wall", math3d.Vec3{X: -2.0, Y: 2.6, Z: -3.0}, math3d.Vec3{X: -0.2, Y: 1.1, Z: -3.0}},
		{"-Y floor", math3d.Vec3{X: 0.8, Y: 0.0, Z: 0.8}, math3d.Vec3{X: -1.45, Y: 0.0, Z: 1.3}},
	}
	for _, p := range pairs {
		litPx, _, _, _ := localShadowTestSamplePixel(pcf.img, pcf.cam, p.lit)
		shadPx, _, _, _ := localShadowTestSamplePixel(pcf.img, pcf.cam, p.shad)
		if int(litPx[0]) < int(shadPx[0])+40 {
			t.Errorf("%s shadow contrast too low: lit=%v shad=%v", p.name, litPx, shadPx)
		}
	}
}

// TestLocalShadowWGSLComposesWithLightRig compiles the shadow helpers next to CameraWGSL and LightWGSL, and draws with a spot and a point shadow bound at once through the ForGroup variants, proving they share no binding with the camera, light or per-draw groups.
func TestLocalShadowWGSLComposesWithLightRig(t *testing.T) {
	if render3d.SpotShadowWGSLForGroup(render3d.LocalShadowGroup) != render3d.SpotShadowWGSL {
		t.Fatal("SpotShadowWGSLForGroup(LocalShadowGroup) differs from SpotShadowWGSL")
	}
	if render3d.PointShadowWGSLForGroup(render3d.LocalShadowGroup) != render3d.PointShadowWGSL {
		t.Fatal("PointShadowWGSLForGroup(LocalShadowGroup) differs from PointShadowWGSL")
	}
	for name, src := range map[string]string{
		"LitPBRSpotShadowWGSL":           render3d.LitPBRSpotShadowWGSL,
		"LitPBRPointShadowWGSL":          render3d.LitPBRPointShadowWGSL,
		"Camera+Light+SpotShadowWGSL":    render3d.CameraWGSL + render3d.LightWGSL + render3d.SpotShadowWGSL + "\n@fragment fn fs() -> @location(0) vec4<f32> { return vec4<f32>(sample_spot_shadow(vec3<f32>(0.0), vec3<f32>(0.0, 1.0, 0.0))); }\n",
		"Camera+Light+PointShadowWGSL":   render3d.CameraWGSL + render3d.LightWGSL + render3d.PointShadowWGSL + "\n@fragment fn fs() -> @location(0) vec4<f32> { return vec4<f32>(sample_point_shadow(vec3<f32>(0.0), vec3<f32>(0.0, 1.0, 0.0))); }\n",
		"Spot(3)+Point(2) in one shader": localShadowBothWGSL,
	} {
		if _, err := gpu.CompileWGSL(src); err != nil {
			t.Fatalf("CompileWGSL(%s): %v", name, err)
		}
	}

	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	rig := render3d.LightRig{
		Ambient: localShadowAmbient,
		Point:   []render3d.PointLight{localShadowTestPoint},
		Spot:    []render3d.SpotLight{localShadowTestSpot},
	}
	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })
	spot, err := render3d.NewSpotShadowMap(nil, dev, render3d.SpotShadowConfig{Resolution: 16})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spot.Close() })
	point, err := render3d.NewPointShadowMap(nil, dev, render3d.PointShadowConfig{FaceResolution: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = point.Close() })
	if err := spot.Update(dev, localShadowTestSpot); err != nil {
		t.Fatal(err)
	}
	if err := point.Update(dev, localShadowTestPoint); err != nil {
		t.Fatal(err)
	}
	mod, err := gpu.CompileWGSL(localShadowBothWGSL)
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs"},
		Layouts:       []*gpu.BindGroupLayout{camBuf.Layout(), lightBuf.Layout(), point.Layout(), spot.Layout()},
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList},
		Targets:       []gpu.ColorTargetState{{Format: gpu.FormatRGBA8Unorm, WriteMask: gpu.ColorWriteAll}},
		DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
	})
	if err != nil {
		t.Fatalf("CreatePipeline(spot at 3, point at 2): %v", err)
	}
	t.Cleanup(func() { _ = pipe.Close() })
	floor := localShadowUpload(t, nil, dev, "floor", localShadowTestSubdividedQuad(math3d.Vec3Zero(), math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, math3d.Vec3Up(), 3, 3, 2), false)
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Y: 5, Z: 5}, math3d.DegToRad(50), 1, 0.1, 30)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	mp := localShadowMainPass(enc)
	mp.SetPipeline(pipe)
	mp.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	mp.SetBindGroup(render3d.LightGroup, lightBuf.BindGroup())
	mp.SetBindGroup(2, point.BindGroup())
	mp.SetBindGroup(render3d.LocalShadowGroup, spot.BindGroup())
	floor.Draw(mp)
	mp.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer error: %v", err)
	}
}

// localShadowBothWGSL shades a world-space mesh with the spot shadow at group 3 and the point shadow at group 2 alongside the camera and light rig.
var localShadowBothWGSL = render3d.CameraWGSL + render3d.LightWGSL + render3d.SpotShadowWGSL + render3d.PointShadowWGSLForGroup(2) + `
struct VsOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world_pos: vec3<f32>,
	@location(1) world_normal: vec3<f32>,
}

@vertex
fn vs(@location(0) pos: vec3<f32>, @location(1) normal: vec3<f32>, @location(2) uv: vec2<f32>) -> VsOut {
	var out: VsOut;
	out.clip = camera.view_proj * vec4<f32>(pos, 1.0);
	out.world_pos = pos;
	out.world_normal = normal;
	return out;
}

@fragment
fn fs(in: VsOut) -> @location(0) vec4<f32> {
	let view_dir = normalize(camera.eye.xyz - in.world_pos * camera.eye.w);
	let spot_vis = sample_spot_shadow(in.world_pos, in.world_normal);
	let point_vis = sample_point_shadow(in.world_pos, in.world_normal);
	let lit = shade_pbr_visibility(in.world_pos, in.world_normal, view_dir, vec3<f32>(0.8, 0.8, 0.8), 0.0, 0.9, 1.0, point_vis, spot_vis);
	return vec4<f32>(linear_to_srgb(lit), 1.0);
}
`

func TestLitPBRLocalShadowPipelineOptions(t *testing.T) {
	if _, err := render3d.NewLitPBRSpotShadowPipeline(nil, nil, nil, nil, nil, nil, gpu.FormatUndefined); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewLitPBRSpotShadowPipeline(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewLitPBRPointShadowPipeline(nil, nil, nil, nil, nil, nil, gpu.FormatUndefined); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewLitPBRPointShadowPipeline(nil dev) = %v, want ErrDeviceLost", err)
	}
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	lightBuf, err := render3d.NewLightBuffer(nil, dev, render3d.LightRig{}, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })
	draw, err := render3d.NewPBRDrawBuffer(nil, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), localShadowFloorMat), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = draw.Close() })
	spot, err := render3d.NewSpotShadowMap(nil, dev, render3d.SpotShadowConfig{Resolution: 16})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spot.Close() })
	point, err := render3d.NewPointShadowMap(nil, dev, render3d.PointShadowConfig{FaceResolution: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = point.Close() })

	if _, err := render3d.NewLitPBRSpotShadowPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), draw.Layout(), nil, gpu.FormatUndefined); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("NewLitPBRSpotShadowPipeline(nil layout) = %v, want ErrInvalidDescriptor", err)
	}
	if _, err := render3d.NewLitPBRPointShadowPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), draw.Layout(), nil, gpu.FormatUndefined); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("NewLitPBRPointShadowPipeline(nil layout) = %v, want ErrInvalidDescriptor", err)
	}
	for name, opts := range map[string]render3d.LitPBRPipelineOptions{
		"spot+point":       {SpotShadowLayout: spot.Layout(), PointShadowLayout: point.Layout()},
		"spot+directional": {SpotShadowLayout: spot.Layout(), ShadowLayout: point.Layout()},
		"point+fog":        {PointShadowLayout: point.Layout(), FogLayout: spot.Layout()},
	} {
		if _, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), draw.Layout(), opts); !errors.Is(err, gpu.ErrInvalidDescriptor) {
			t.Errorf("%s: err = %v, want ErrInvalidDescriptor (bind group 3 conflict)", name, err)
		}
	}
	for name, mk := range map[string]func() (*gpu.Pipeline, error){
		"spot": func() (*gpu.Pipeline, error) {
			return render3d.NewLitPBRSpotShadowPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), draw.Layout(), spot.Layout(), gpu.FormatDepth24Plus)
		},
		"point": func() (*gpu.Pipeline, error) {
			return render3d.NewLitPBRPointShadowPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), draw.Layout(), point.Layout(), gpu.FormatDepth24Plus)
		},
		"spot-linear": func() (*gpu.Pipeline, error) {
			return render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), draw.Layout(), render3d.LitPBRPipelineOptions{SpotShadowLayout: spot.Layout(), LinearOutput: true})
		},
		"point-linear": func() (*gpu.Pipeline, error) {
			return render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), draw.Layout(), render3d.LitPBRPipelineOptions{PointShadowLayout: point.Layout(), LinearOutput: true})
		},
	} {
		p, err := mk()
		if err != nil {
			t.Fatalf("%s pipeline: %v", name, err)
		}
		_ = p.Close()
	}
}

func TestLocalShadowCasterSlots(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	box := localShadowUpload(t, nil, dev, "box", localShadowTestBoxMesh(math3d.Vec3{Y: 0.5}, math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}), false)

	spot, err := render3d.NewSpotShadowMap(nil, dev, render3d.SpotShadowConfig{Resolution: 16, MaxCasters: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spot.Close() })
	if spot.MaxCasters() != 2 {
		t.Fatalf("MaxCasters = %d, want 2", spot.MaxCasters())
	}

	// Drawing before Update has no light projection, so it is refused.
	enc := dev.BeginEncoder()
	sp := spot.BeginShadowPass(dev, enc)
	if spot.DrawCaster(sp, box, math3d.Mat4Identity()) {
		t.Fatal("DrawCaster before Update returned true")
	}
	sp.End()
	dev.Submit(enc.Finish())

	if err := spot.Update(dev, localShadowTestSpot); err != nil {
		t.Fatal(err)
	}
	for frame := range 3 {
		enc := dev.BeginEncoder()
		sp := spot.BeginShadowPass(dev, enc)
		for i := range 2 {
			if !spot.DrawCaster(sp, box, math3d.Translate(math3d.Vec3{X: float32(i)})) {
				t.Fatalf("frame %d caster %d refused", frame, i)
			}
		}
		if spot.DrawCaster(sp, box, math3d.Mat4Identity()) {
			t.Fatalf("frame %d: caster beyond MaxCasters accepted", frame)
		}
		sp.End()
		dev.Submit(enc.Finish())
	}

	point, err := render3d.NewPointShadowMap(nil, dev, render3d.PointShadowConfig{FaceResolution: 8, MaxCasters: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = point.Close() })
	if err := point.Update(dev, localShadowTestPoint); err != nil {
		t.Fatal(err)
	}
	// Refreshing only some faces, in any order, frame after frame never runs out of slots: each face owns its own.
	for frame := range 8 {
		enc := dev.BeginEncoder()
		for _, face := range []gpu.CubeFace{gpu.CubeFacePosZ, gpu.CubeFaceNegX} {
			fp := point.BeginFacePass(dev, enc, face)
			if fp == nil {
				t.Fatalf("BeginFacePass(%d) returned nil", face)
			}
			if !point.DrawCaster(fp, box, math3d.Mat4Identity()) {
				t.Fatalf("frame %d face %d: caster refused", frame, face)
			}
			if point.DrawCaster(fp, box, math3d.Mat4Identity()) {
				t.Fatalf("frame %d face %d: caster beyond MaxCasters accepted", frame, face)
			}
			fp.End()
		}
		dev.Submit(enc.Finish())
	}
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer error: %v", err)
	}
	if point.BeginFacePass(dev, dev.BeginEncoder(), render3d.PointShadowFaceCount) != nil {
		t.Fatal("BeginFacePass accepted an out-of-range face")
	}
}

func localShadowTestStartGPUView(t *testing.T, retained bool, usePoint bool, extraOpts ...nsgotest.Option) (
	*nsgotest.App,
	func() bool,
	*int,
	*scene3d.Camera,
) {
	t.Helper()
	var (
		dev         *gpu.Device
		camBuf      *render3d.CameraBuffer
		lightBuf    *render3d.GPULightBuffer
		draws       []localShadowDraw
		spotShadow  *render3d.SpotShadowMap
		pointShadow *render3d.PointShadowMap
		litPipe     *gpu.Pipeline
		gpuCaster   *render3d.GPUMesh
		gpuFloor    *render3d.GPUMesh
		frames      int
	)

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 4.5, Z: 4.5}, math3d.DegToRad(50), 1.0, 0.1, 30)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())

	floorMesh := localShadowTestSubdividedQuad(math3d.Vec3Zero(), math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, math3d.Vec3Up(), 4.0, 4.0, 6)
	pillarMesh := localShadowTestBoxMesh(math3d.Vec3Zero(), math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5})
	pillarModel := math3d.Translate(math3d.Vec3{Y: 0.9}).Mul(math3d.Scale(math3d.Vec3{X: 0.9, Y: 1.8, Z: 0.9}))

	spotLight := render3d.SpotLight{
		Position:       math3d.Vec3{X: -2.0, Y: 3.2, Z: 0.0},
		Direction:      math3d.Vec3{X: 2.0, Y: -3.2, Z: 0.0},
		Color:          math3d.Vec3{X: 1, Y: 1, Z: 1},
		Intensity:      30,
		Range:          12,
		InnerConeAngle: math3d.DegToRad(30),
		OuterConeAngle: math3d.DegToRad(45),
	}
	pointLight := render3d.PointLight{
		Position:  math3d.Vec3{X: -2.0, Y: 2.0, Z: 0.0},
		Color:     math3d.Vec3{X: 1, Y: 1, Z: 1},
		Intensity: 12,
		Range:     12,
	}
	rig := render3d.LightRig{Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.08, Y: 0.08, Z: 0.08}, Intensity: 1}}
	if usePoint {
		rig.Point = []render3d.PointLight{pointLight}
	} else {
		rig.Spot = []render3d.SpotLight{spotLight}
	}

	opts := append([]nsgotest.Option{nsgotest.Size(64, 64), nsgotest.ManualFrames()}, extraOpts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "viewport",
			Style:       core.Style{Width: core.Pt(64), Height: core.Pt(64)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(d *gpu.Device) {
				dev = d
				var err error
				if camBuf, err = render3d.NewCameraBuffer(cx, d); err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				if lightBuf, err = render3d.NewLightBuffer(cx, d, rig, render3d.LightBufferOptions{}); err != nil {
					t.Fatalf("NewLightBuffer: %v", err)
				}
				if !retained || gpuCaster == nil {
					gpuCaster = localShadowUpload(t, cx, d, "caster", pillarMesh, retained)
					gpuFloor = localShadowUpload(t, cx, d, "floor", floorMesh, retained)
				}
				draws = localShadowDrawBuffers(t, cx, d, false,
					[]*render3d.GPUMesh{gpuFloor, gpuCaster},
					[]render3d.PBRDrawUniform{
						render3d.NewPBRDrawUniform(math3d.Mat4Identity(), localShadowFloorMat),
						render3d.NewPBRDrawUniform(pillarModel, localShadowPillarMat),
					})
				if usePoint {
					if !retained || pointShadow == nil {
						if pointShadow, err = render3d.NewPointShadowMap(cx, d, render3d.PointShadowConfig{FaceResolution: 32, DepthBias: 0.002, NormalBias: 0.02, Filter: render3d.LocalShadowFilterPCF3x3, Retained: retained}); err != nil {
							t.Fatalf("NewPointShadowMap: %v", err)
						}
					}
					if litPipe, err = render3d.NewLitPBRPointShadowPipeline(cx, d, camBuf.Layout(), lightBuf.Layout(), draws[0].buf.Layout(), pointShadow.Layout(), gpu.FormatDepth24Plus); err != nil {
						t.Fatalf("NewLitPBRPointShadowPipeline: %v", err)
					}
				} else {
					if !retained || spotShadow == nil {
						if spotShadow, err = render3d.NewSpotShadowMap(cx, d, render3d.SpotShadowConfig{Resolution: 64, DepthBias: 0.002, NormalBias: 0.02, Filter: render3d.LocalShadowFilterPCF3x3, Retained: retained}); err != nil {
							t.Fatalf("NewSpotShadowMap: %v", err)
						}
					}
					if litPipe, err = render3d.NewLitPBRSpotShadowPipeline(cx, d, camBuf.Layout(), lightBuf.Layout(), draws[0].buf.Layout(), spotShadow.Layout(), gpu.FormatDepth24Plus); err != nil {
						t.Fatalf("NewLitPBRSpotShadowPipeline: %v", err)
					}
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				camBuf.WriteCamera(cam)
				var shadow *gpu.BindGroup
				if usePoint {
					if err := pointShadow.Update(dev, pointLight); err != nil {
						t.Fatalf("pointShadow.Update: %v", err)
					}
					for face := range gpu.CubeFace(render3d.PointShadowFaceCount) {
						fp := pointShadow.BeginFacePass(dev, frame, face)
						pointShadow.DrawCaster(fp, gpuCaster, pillarModel)
						fp.End()
					}
					shadow = pointShadow.BindGroup()
				} else {
					if err := spotShadow.Update(dev, spotLight); err != nil {
						t.Fatalf("spotShadow.Update: %v", err)
					}
					sp := spotShadow.BeginShadowPass(dev, frame)
					spotShadow.DrawCaster(sp, gpuCaster, pillarModel)
					sp.End()
					shadow = spotShadow.BindGroup()
				}
				mp := localShadowMainPass(frame)
				localShadowDrawReceivers(mp, litPipe, camBuf, lightBuf, shadow, draws)
				mp.End()
				frame.Submit()
				frames++
			},
		})
	}, opts...)

	isValid := func() bool {
		if usePoint {
			return pointShadow != nil && pointShadow.Valid()
		}
		return spotShadow != nil && spotShadow.Valid()
	}
	return app, isValid, &frames, cam
}

func localShadowTestAssertViewShadowContrast(t *testing.T, app *nsgotest.App, frames *int, cam *scene3d.Camera) {
	t.Helper()
	before := *frames
	app.Advance(16 * time.Millisecond)
	if *frames <= before {
		t.Fatalf("expected rendered frame, got frames=%d", *frames)
	}
	img, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels: %v", err)
	}
	litPx, _, _, okLit := localShadowTestSamplePixel(img, cam, math3d.Vec3{X: -1.2, Y: 0, Z: 0})
	shadPx, _, _, okShad := localShadowTestSamplePixel(img, cam, math3d.Vec3{X: 1.0, Y: 0, Z: 0})
	if !okLit || !okShad {
		t.Fatalf("probes outside view: lit=%v shad=%v", okLit, okShad)
	}
	if int(litPx[0]) < int(shadPx[0])+60 {
		t.Fatalf("expected strong shadow contrast in gpu.View: lit=%v shad=%v", litPx, shadPx)
	}
}

func TestLocalShadowsInGPUViewProfiles(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	for _, p := range profiles {
		for _, kind := range []struct {
			name     string
			usePoint bool
		}{
			{"spot", false},
			{"point", true},
		} {
			t.Run(p.name+"/"+kind.name, func(t *testing.T) {
				app, valid, frames, cam := localShadowTestStartGPUView(t, false, kind.usePoint, p.opts...)
				if !valid() {
					t.Fatal("shadow map not valid after OnReady")
				}
				localShadowTestAssertViewShadowContrast(t, app, frames, cam)
			})
		}
	}
}

func TestLocalShadowsRetainedDeviceLossRestore(t *testing.T) {
	for _, kind := range []struct {
		name     string
		usePoint bool
	}{
		{"spot", false},
		{"point", true},
	} {
		t.Run(kind.name, func(t *testing.T) {
			app, valid, frames, cam := localShadowTestStartGPUView(t, true, kind.usePoint)
			localShadowTestAssertViewShadowContrast(t, app, frames, cam)

			app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
			if valid() {
				t.Fatalf("retained %s shadow map still valid while device is lost", kind.name)
			}

			app.RestoreGPUDevice("id:viewport")
			if !valid() {
				t.Fatalf("retained %s shadow map not restored after RestoreGPUDevice", kind.name)
			}
			localShadowTestAssertViewShadowContrast(t, app, frames, cam)
		})
	}
}

func TestLocalShadowsCloseAndScopeDisposal(t *testing.T) {
	var nilSpot *render3d.SpotShadowMap
	var nilPoint *render3d.PointShadowMap
	if nilSpot.Valid() || nilPoint.Valid() || nilSpot.ByteSize() != 0 || nilPoint.ByteSize() != 0 || nilSpot.MaxCasters() != 0 || nilPoint.MaxCasters() != 0 {
		t.Fatal("nil shadow map should report invalid and zero sizes")
	}
	if err := nilSpot.Close(); err != nil {
		t.Fatalf("nil spot Close: %v", err)
	}
	if err := nilPoint.Close(); err != nil {
		t.Fatalf("nil point Close: %v", err)
	}
	if nilSpot.BeginShadowPass(nil, nil) != nil || nilPoint.BeginFacePass(nil, nil, 0) != nil || nilSpot.DrawCaster(nil, nil, math3d.Mat4Identity()) || nilPoint.DrawCaster(nil, nil, math3d.Mat4Identity()) {
		t.Fatal("nil shadow map recorded a pass or caster")
	}

	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	spot, err := render3d.NewSpotShadowMap(nil, dev, render3d.SpotShadowConfig{Resolution: 64, MaxCasters: 16})
	if err != nil {
		t.Fatalf("NewSpotShadowMap: %v", err)
	}
	if want := uint64(64*64*4) + render3d.SpotShadowUniformSize + 16*256; spot.ByteSize() != want {
		t.Fatalf("spot ByteSize = %d, want %d", spot.ByteSize(), want)
	}
	if err := spot.Close(); err != nil {
		t.Fatalf("spot Close: %v", err)
	}
	if err := spot.Close(); err != nil {
		t.Fatalf("spot second Close: %v", err)
	}
	if spot.Valid() {
		t.Fatal("spot still valid after Close")
	}
	if err := spot.Update(dev, localShadowTestSpot); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
		t.Fatalf("Update after Close = %v, want ErrInvalidLocalShadow", err)
	}

	point, err := render3d.NewPointShadowMap(nil, dev, render3d.PointShadowConfig{FaceResolution: 32, MaxCasters: 24})
	if err != nil {
		t.Fatalf("NewPointShadowMap: %v", err)
	}
	if want := uint64(96*64*4) + render3d.PointShadowUniformSize + 6*24*256; point.ByteSize() != want {
		t.Fatalf("point ByteSize = %d, want %d", point.ByteSize(), want)
	}
	if err := point.Close(); err != nil {
		t.Fatalf("point Close: %v", err)
	}
	if err := point.Close(); err != nil {
		t.Fatalf("point second Close: %v", err)
	}
	if point.Valid() {
		t.Fatal("point still valid after Close")
	}

	var (
		scopedSpot  *render3d.SpotShadowMap
		scopedPoint *render3d.PointShadowMap
		show        *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
				OnReady: func(d *gpu.Device) {
					var err error
					if scopedSpot, err = render3d.NewSpotShadowMap(cx, d, render3d.SpotShadowConfig{Resolution: 32}); err != nil {
						t.Fatalf("NewSpotShadowMap: %v", err)
					}
					if scopedPoint, err = render3d.NewPointShadowMap(cx, d, render3d.PointShadowConfig{FaceResolution: 16}); err != nil {
						t.Fatalf("NewPointShadowMap: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())

	if !scopedSpot.Valid() || !scopedPoint.Valid() {
		t.Fatal("scoped shadow maps must be valid while mounted")
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if scopedSpot.Valid() || scopedPoint.Valid() {
		t.Fatalf("scoped shadow maps still valid after disposal: spot=%v point=%v", scopedSpot.Valid(), scopedPoint.Valid())
	}
}

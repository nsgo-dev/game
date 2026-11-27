package render3d_test

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// testSpotLight is a valid spot light hanging 3 units above the origin and pointing straight down.
func testSpotLight() render3d.SpotLight {
	return render3d.SpotLight{
		Position:       math3d.Vec3{Y: 3},
		Direction:      math3d.Vec3{Y: -1},
		Color:          math3d.Vec3{X: 1, Y: 0.9, Z: 0.8},
		Intensity:      12,
		Range:          8,
		InnerConeAngle: 0.2,
		OuterConeAngle: 0.4,
	}
}

func TestSpotLightValidation(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	cases := []struct {
		name   string
		modify func(*render3d.SpotLight)
		ok     bool
	}{
		{"valid", func(*render3d.SpotLight) {}, true},
		{"zero inner cone", func(l *render3d.SpotLight) { l.InnerConeAngle = 0 }, true},
		{"outer cone at pi/2", func(l *render3d.SpotLight) { l.OuterConeAngle = math.Pi / 2 }, true},
		{"unbounded range", func(l *render3d.SpotLight) { l.Range = 0 }, true},
		{"unnormalized direction", func(l *render3d.SpotLight) { l.Direction = math3d.Vec3{X: 2, Y: -6} }, true},
		{"nan position", func(l *render3d.SpotLight) { l.Position.X = nan }, false},
		{"zero direction", func(l *render3d.SpotLight) { l.Direction = math3d.Vec3{} }, false},
		{"inf direction", func(l *render3d.SpotLight) { l.Direction.Y = inf }, false},
		{"negative color", func(l *render3d.SpotLight) { l.Color.Z = -0.1 }, false},
		{"negative intensity", func(l *render3d.SpotLight) { l.Intensity = -1 }, false},
		{"negative range", func(l *render3d.SpotLight) { l.Range = -1 }, false},
		{"zero outer cone", func(l *render3d.SpotLight) { l.InnerConeAngle, l.OuterConeAngle = 0, 0 }, false},
		{"negative outer cone", func(l *render3d.SpotLight) { l.OuterConeAngle = -0.1 }, false},
		{"outer cone past pi/2", func(l *render3d.SpotLight) { l.OuterConeAngle = math.Pi/2 + 0.01 }, false},
		{"nan outer cone", func(l *render3d.SpotLight) { l.OuterConeAngle = nan }, false},
		{"negative inner cone", func(l *render3d.SpotLight) { l.InnerConeAngle = -0.1 }, false},
		{"equal inner and outer cone", func(l *render3d.SpotLight) { l.InnerConeAngle = l.OuterConeAngle }, false},
		{"inner cone past outer", func(l *render3d.SpotLight) { l.InnerConeAngle = 0.5 }, false},
		{"nan inner cone", func(l *render3d.SpotLight) { l.InnerConeAngle = nan }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := testSpotLight()
			tc.modify(&l)
			err := l.Validate()
			if tc.ok {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, render3d.ErrInvalidLight) {
				t.Fatalf("Validate() = %v, want an error wrapping ErrInvalidLight", err)
			}
			rigErr := render3d.LightRig{Spot: []render3d.SpotLight{l}}.Validate()
			if !errors.Is(rigErr, render3d.ErrInvalidLight) {
				t.Fatalf("LightRig.Validate() = %v, want an error wrapping ErrInvalidLight", rigErr)
			}
		})
	}

	tooMany := make([]render3d.SpotLight, render3d.MaxSpotLights+1)
	for i := range tooMany {
		tooMany[i] = testSpotLight()
	}
	if err := (render3d.LightRig{Spot: tooMany}).Validate(); !errors.Is(err, render3d.ErrInvalidLight) {
		t.Fatalf("rig with %d spot lights: Validate() = %v, want ErrInvalidLight", len(tooMany), err)
	}
	if err := (render3d.LightRig{Spot: tooMany[:render3d.MaxSpotLights]}).Validate(); err != nil {
		t.Fatalf("rig with MaxSpotLights spot lights: Validate() = %v, want nil", err)
	}
}

func TestSpotConeAttenuation(t *testing.T) {
	l := testSpotLight()
	scale, offset := l.ConeScaleOffset()
	cosInner, cosOuter := math.Cos(float64(l.InnerConeAngle)), math.Cos(float64(l.OuterConeAngle))
	if want := float32(1 / (cosInner - cosOuter)); !nearF32(scale, want, want*1e-6) {
		t.Errorf("angleScale = %v, want %v", scale, want)
	}
	if want := float32(-cosOuter / (cosInner - cosOuter)); !nearF32(offset, want, float32(math.Abs(float64(want)))*1e-6) {
		t.Errorf("angleOffset = %v, want %v", offset, want)
	}

	// A ground point at angle a off the cone axis.
	groundAt := func(a float64) math3d.Vec3 { return math3d.Vec3{X: float32(3 * math.Tan(a))} }
	cases := []struct {
		name  string
		angle float64
		want  float64
	}{
		{"on axis", 0, 1},
		{"inside inner cone", 0.15, 1},
		{"between cones", 0.3, math.Pow((math.Cos(0.3)-cosOuter)/(cosInner-cosOuter), 2)},
		{"outside outer cone", 0.45, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := groundAt(tc.angle)
			toLight := l.Position.Sub(p).Norm()
			cone := render3d.SpotConeAttenuation(l.Direction, toLight, scale, offset)
			if !nearF32(cone, float32(tc.want), 2e-4) {
				t.Fatalf("SpotConeAttenuation = %v, want %v", cone, tc.want)
			}
			wantAtten := render3d.PointLightAttenuation(l.Position.Sub(p).LenSq(), l.Range) * cone
			if got := l.Attenuation(p); !nearF32(got, wantAtten, 1e-6) {
				t.Fatalf("Attenuation = %v, want distance * cone = %v", got, wantAtten)
			}
		})
	}

	if got := l.Attenuation(math3d.Vec3{Y: 5}); got != 0 {
		t.Errorf("Attenuation behind the light = %v, want 0", got)
	}
	if got := l.Attenuation(l.Position); got != 0 {
		t.Errorf("Attenuation at the light position = %v, want 0", got)
	}
	if got := l.Attenuation(math3d.Vec3{Y: -6}); got != 0 {
		t.Errorf("Attenuation beyond Range = %v, want 0", got)
	}
}

func TestSpotLightUniformLayout(t *testing.T) {
	if render3d.LightUniformSize != 1056 {
		t.Fatalf("LightUniformSize = %d, want 1056", render3d.LightUniformSize)
	}
	spot := render3d.SpotLight{
		Position:       math3d.Vec3{X: 1, Y: 2, Z: 3},
		Direction:      math3d.Vec3{Y: -3, Z: -4},
		Color:          math3d.Vec3{X: 0.5, Y: 0.25, Z: 1},
		Intensity:      4,
		Range:          8,
		InnerConeAngle: 0.25,
		OuterConeAngle: 0.5,
	}
	rig := render3d.LightRig{
		Point: []render3d.PointLight{{Position: math3d.Vec3{X: 1}, Color: math3d.Vec3One(), Intensity: 1}},
		Spot:  []render3d.SpotLight{spot},
	}
	u, err := render3d.NewLightUniforms(rig)
	if err != nil {
		t.Fatalf("NewLightUniforms: %v", err)
	}
	dirty := make([]byte, render3d.LightUniformSize)
	for i := range dirty {
		dirty[i] = 0xFF
	}
	enc := u.Encode(dirty)
	f32 := func(off int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(enc[off:])) }
	u32 := func(off int) uint32 { return binary.LittleEndian.Uint32(enc[off:]) }

	if u32(16) != 0 || u32(20) != 1 || u32(24) != 1 || u32(28) != 0 {
		t.Fatalf("counts = (%d, %d, %d, %d), want (0, 1, 1, 0)", u32(16), u32(20), u32(24), u32(28))
	}
	scale, offset := spot.ConeScaleOffset()
	const base = 16 + 16 + render3d.MaxDirectionalLights*32 + render3d.MaxPointLights*32
	want := [12]float32{
		1, 2, 3, 8, // position_range
		0, -0.6, -0.8, scale, // direction_scale
		2, 1, 4, offset, // color_offset
	}
	for i, w := range want {
		if got := f32(base + 4*i); !nearF32(got, w, 1e-6) {
			t.Errorf("spot[0] float %d at offset %d = %v, want %v", i, base+4*i, got, w)
		}
	}
	for off := base + 48; off < render3d.LightUniformSize; off += 4 {
		if u32(off) != 0 {
			t.Fatalf("unused spot slot at offset %d = %x, want 0", off, u32(off))
		}
	}

	appended := rig.AppendUniformData(nil)
	for i := range enc {
		if enc[i] != appended[i] {
			t.Fatalf("byte %d: Encode=%02x AppendUniformData=%02x", i, enc[i], appended[i])
		}
	}
	buf := make([]byte, 0, render3d.LightUniformSize)
	if allocs := testing.AllocsPerRun(50, func() { buf = rig.AppendUniformData(buf[:0]) }); allocs != 0 {
		t.Errorf("AppendUniformData with a spot light allocs = %v, want 0", allocs)
	}

	// Lights beyond MaxSpotLights are left out of an unvalidated rig.
	many := make([]render3d.SpotLight, render3d.MaxSpotLights+3)
	for i := range many {
		many[i] = spot
	}
	over := render3d.LightRig{Spot: many}.AppendUniformData(nil)
	if got := binary.LittleEndian.Uint32(over[24:]); got != render3d.MaxSpotLights {
		t.Errorf("spot count for %d lights = %d, want %d", len(many), got, render3d.MaxSpotLights)
	}
}

func TestSpotLightShadesLikePointLightInsideInnerCone(t *testing.T) {
	spot := testSpotLight()
	spot.InnerConeAngle, spot.OuterConeAngle = 0.6, 0.9
	point := render3d.PointLight{Position: spot.Position, Color: spot.Color, Intensity: spot.Intensity, Range: spot.Range}
	mat := render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.7, Y: 0.5, Z: 0.3}, Metallic: 0.3, Roughness: 0.4}
	amb := render3d.AmbientLight{Color: math3d.Vec3{X: 0.1, Y: 0.1, Z: 0.2}, Intensity: 0.5}
	eye := math3d.Vec3{X: 1, Y: 4, Z: 5}
	for _, p := range []math3d.Vec3{{}, {X: 0.5}, {X: -0.6, Z: 0.7}} {
		view := eye.Sub(p)
		got := render3d.ShadePoint(p, math3d.Vec3{Y: 1}, view, mat, render3d.LightRig{Ambient: amb, Spot: []render3d.SpotLight{spot}})
		want := render3d.ShadePoint(p, math3d.Vec3{Y: 1}, view, mat, render3d.LightRig{Ambient: amb, Point: []render3d.PointLight{point}})
		if got != want {
			t.Errorf("at %v: spot inside its inner cone = %v, point light = %v", p, got, want)
		}
	}

	// Outside the outer cone a spot light adds nothing beyond ambient.
	outside := math3d.Vec3{X: 4}
	got := render3d.ShadePoint(outside, math3d.Vec3{Y: 1}, eye.Sub(outside), mat, render3d.LightRig{Ambient: amb, Spot: []render3d.SpotLight{testSpotLight()}})
	want := render3d.ShadePoint(outside, math3d.Vec3{Y: 1}, eye.Sub(outside), mat, render3d.LightRig{Ambient: amb})
	if got != want {
		t.Errorf("outside the outer cone = %v, want ambient only %v", got, want)
	}
}

func TestSpotLightCombinesWithDirectionalAndPointLights(t *testing.T) {
	sun := render3d.DirectionalLight{Direction: math3d.Vec3{X: 0.3, Y: -1, Z: -0.2}, Color: math3d.Vec3{X: 1, Y: 0.95, Z: 0.9}, Intensity: 1.5}
	lamp := render3d.PointLight{Position: math3d.Vec3{X: -1, Y: 1, Z: 1}, Color: math3d.Vec3{X: 0.2, Y: 0.5, Z: 1}, Intensity: 3, Range: 6}
	flashlight := testSpotLight()
	mat := render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.6, Y: 0.6, Z: 0.6}, Metallic: 0.1, Roughness: 0.5}
	p := math3d.Vec3{X: 0.3, Z: 0.2}
	n := math3d.Vec3{Y: 1}
	v := math3d.Vec3{Y: 3, Z: 4}

	all := render3d.ShadePoint(p, n, v, mat, render3d.LightRig{Directional: []render3d.DirectionalLight{sun}, Point: []render3d.PointLight{lamp}, Spot: []render3d.SpotLight{flashlight}})
	sum := render3d.ShadePoint(p, n, v, mat, render3d.LightRig{Directional: []render3d.DirectionalLight{sun}}).
		Add(render3d.ShadePoint(p, n, v, mat, render3d.LightRig{Point: []render3d.PointLight{lamp}})).
		Add(render3d.ShadePoint(p, n, v, mat, render3d.LightRig{Spot: []render3d.SpotLight{flashlight}}))
	if !nearF32(all.X, sum.X, 1e-5) || !nearF32(all.Y, sum.Y, 1e-5) || !nearF32(all.Z, sum.Z, 1e-5) {
		t.Fatalf("sun + lamp + flashlight = %v, want the sum of each light alone %v", all, sum)
	}
	if spotOnly := render3d.ShadePoint(p, n, v, mat, render3d.LightRig{Spot: []render3d.SpotLight{flashlight}}); spotOnly.X <= 0 {
		t.Fatalf("flashlight alone = %v, want it to light the point", spotOnly)
	}
}

func TestSpotLightOppositeViewerStaysFinite(t *testing.T) {
	// The light shines up onto the surface while the viewer looks at it from below, so v + l is zero: the back-facing view must add nothing instead of turning the pixel into NaN.
	amb := render3d.AmbientLight{Color: math3d.Vec3{X: 0.2, Y: 0.2, Z: 0.2}, Intensity: 1}
	spot := render3d.SpotLight{Position: math3d.Vec3{Y: 2}, Direction: math3d.Vec3{Y: -1}, Color: math3d.Vec3One(), Intensity: 10, InnerConeAngle: 0.3, OuterConeAngle: 0.6}
	mat := render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}, Roughness: 0.3}
	got := render3d.ShadePoint(math3d.Vec3{}, math3d.Vec3{Y: 1}, math3d.Vec3{Y: -1}, mat, render3d.LightRig{Ambient: amb, Spot: []render3d.SpotLight{spot}})
	want := math3d.Vec3{X: 0.1, Y: 0.1, Z: 0.1}
	if !finite3(got) || !nearF32(got.X, want.X, 1e-6) || !nearF32(got.Y, want.Y, 1e-6) || !nearF32(got.Z, want.Z, 1e-6) {
		t.Fatalf("ShadePoint with the light opposite the viewer = %v, want ambient only %v", got, want)
	}
}

func TestSpotLightsSceneMatchesReference(t *testing.T) {
	const w, h = 160, 120
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 3.4, Z: 6.4}, math3d.DegToRad(45), float32(w)/float32(h), 0.1, 40)
	cam.LookAt(math3d.Vec3{Y: 0.3}, math3d.Vec3Up())

	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.1, Y: 0.12, Z: 0.16}, Intensity: 0.15},
		Directional: []render3d.DirectionalLight{
			{Direction: math3d.Vec3{X: 0.4, Y: -1, Z: -0.5}, Color: math3d.Vec3{X: 0.6, Y: 0.65, Z: 0.8}, Intensity: 0.25},
		},
		Spot: []render3d.SpotLight{
			{
				Position: math3d.Vec3{X: -1.8, Y: 3.6, Z: 1.2}, Direction: math3d.Vec3{X: 0.7, Y: -3.6, Z: -1.1},
				Color: math3d.Vec3{X: 1, Y: 0.72, Z: 0.38}, Intensity: 28, Range: 9,
				InnerConeAngle: math3d.DegToRad(14), OuterConeAngle: math3d.DegToRad(26),
			},
			{
				Position: math3d.Vec3{X: 2.2, Y: 3.2, Z: 0.8}, Direction: math3d.Vec3{X: -1.1, Y: -3.2, Z: -1.0},
				Color: math3d.Vec3{X: 0.3, Y: 0.6, Z: 1}, Intensity: 24, Range: 8,
				InnerConeAngle: math3d.DegToRad(8), OuterConeAngle: math3d.DegToRad(22),
			},
		},
	}

	img := renderLitScene(t, w, h, cam, rig, core.RGB(0x05070b), func(dev *gpu.Device) []litSceneObject {
		sphere, err := render3d.UploadMesh(nil, dev, uvSphereMesh(0.75, 24, 16), render3d.MeshUpload{Label: "sphere"})
		if err != nil {
			t.Fatalf("UploadMesh sphere: %v", err)
		}
		t.Cleanup(func() { _ = sphere.Close() })
		ground, err := render3d.UploadMesh(nil, dev, lightGroundPlaneMesh(4.5), render3d.MeshUpload{Label: "ground"})
		if err != nil {
			t.Fatalf("UploadMesh ground: %v", err)
		}
		t.Cleanup(func() { _ = ground.Close() })
		return []litSceneObject{
			{mesh: ground, model: math3d.Mat4Identity(), mat: render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.7, Y: 0.7, Z: 0.68}, Roughness: 0.8}},
			{mesh: sphere, model: math3d.Translate(math3d.Vec3{X: -1.2, Y: 0.75, Z: -0.4}), mat: render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.82, Y: 0.8, Z: 0.78}, Metallic: 0.05, Roughness: 0.45}},
			{mesh: sphere, model: math3d.Translate(math3d.Vec3{X: 1.2, Y: 0.75, Z: -0.6}), mat: render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.95, Y: 0.75, Z: 0.35}, Metallic: 1, Roughness: 0.3}},
		}
	})

	assertReference(t, "spot_lights_scene", img)

	// The ground under the warm spot is lit and warm, while ground between the two cones only gets ambient and the dim sun.
	warmHit := math3d.Vec3{X: -1.8 + 0.7*1.0, Y: 0, Z: 1.2 - 1.1*1.0}
	pt := func(p math3d.Vec3) (int, int) {
		s, _, ok := cam.WorldToScreen(p, w, h)
		if !ok {
			t.Fatalf("%v projects outside the screen", p)
		}
		return int(s.X), int(s.Y)
	}
	wx, wy := pt(warmHit)
	ox, oy := pt(math3d.Vec3{X: 0.2, Y: 0, Z: 2.2})
	warm := img.RGBAAt(wx, wy)
	outside := img.RGBAAt(ox, oy)
	if warm.R <= warm.B+30 {
		t.Errorf("ground under the warm spot %v is not warm-tinted", warm)
	}
	if int(warm.R) <= int(outside.R)+60 {
		t.Errorf("ground under the warm spot %v is not clearly brighter than ground outside both cones %v", warm, outside)
	}
}

func finite3(v math3d.Vec3) bool {
	for _, c := range [3]float32{v.X, v.Y, v.Z} {
		if math.IsNaN(float64(c)) || math.IsInf(float64(c), 0) {
			return false
		}
	}
	return true
}

func ExampleSpotLight() {
	flashlight := render3d.SpotLight{
		Position:       math3d.Vec3{Y: 2},
		Direction:      math3d.Vec3{Y: -1},
		Color:          math3d.Vec3One(),
		Intensity:      10,
		Range:          6,
		InnerConeAngle: math3d.DegToRad(15),
		OuterConeAngle: math3d.DegToRad(30),
	}
	rig := render3d.LightRig{
		Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{Y: -1}, Color: math3d.Vec3One(), Intensity: 0.5}},
		Spot:        []render3d.SpotLight{flashlight},
	}
	fmt.Println(rig.Validate())
	fmt.Printf("under the light: %.2f\n", flashlight.Attenuation(math3d.Vec3{}))
	fmt.Printf("outside the cone: %.2f\n", flashlight.Attenuation(math3d.Vec3{X: 2}))
	// Output:
	// <nil>
	// under the light: 0.24
	// outside the cone: 0.00
}

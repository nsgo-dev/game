package lights2d_test

import (
	"errors"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/lights2d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestSceneDrawAmbientAndLightsOps(t *testing.T) {
	pointLight := lights2d.NewLight(100, 100, 80, core.RGB(0xffaa44))
	spotLight := &lights2d.Light{
		X:          220,
		Y:          120,
		Radius:     90,
		Color:      core.RGB(0x66ccff),
		Intensity:  0.8,
		Falloff:    2,
		ConeAngle:  math.Pi / 4,
		ConeSpread: math.Pi / 3,
		Enabled:    true,
	}
	disabledLight := lights2d.NewLight(150, 150, 60, core.RGB(0xffffff))
	disabledLight.Enabled = false
	culledLight := lights2d.NewLight(900, 900, 50, core.RGB(0xff0000))

	ambient := core.RGBA(0x050814, 0.85)
	bounds := core.Bounds{
		Origin: core.Point{X: 0, Y: 0},
		Size:   core.Size{Width: 400, Height: 300},
	}

	scene := &lights2d.Scene{
		Ambient: ambient,
		Lights:  []*lights2d.Light{pointLight, spotLight, disabledLight, culledLight},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "default",
				Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
				Draw: func(d *core.Drawing, _ core.Size) {
					scene.Draw(d, bounds)
				},
			},
			core.Canvas{
				TestID: "screen",
				Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
				Draw: func(d *core.Drawing, _ core.Size) {
					screenScene := *scene
					screenScene.LightComposite = core.CompositeScreen
					screenScene.Draw(d, bounds)
				},
			},
		}}
	})

	ops := app.Find("default").Ops
	if len(ops) == 0 {
		t.Fatal("Scene.Draw recorded no operations")
	}

	var saves, restores, ambientFills, pointGradOps, spotGradOps, lighterComposites, pointArcs, spotArcs int
	var prevSpotHalf float64 = math.Inf(1)
	for _, op := range ops {
		switch op.Kind {
		case core.DrawSave:
			saves++
		case core.DrawRestore:
			restores++
		case core.DrawFillColor:
			if op.Color == ambient {
				ambientFills++
			}
		case core.DrawComposite:
			if op.Composite == core.CompositeLighter {
				lighterComposites++
			}
		case core.DrawAlpha:
			t.Errorf("Scene.Draw must not override caller global alpha (found DrawAlpha %v)", op.A)
		case core.DrawFillGradient:
			g := op.Gradient
			if g == nil || !g.Radial {
				t.Fatalf("expected radial gradient, got %+v", g)
			}
			if len(g.Stops) < 2 {
				t.Fatalf("expected at least 2 gradient stops, got %d", len(g.Stops))
			}
			// Verify radial gradient fades to 0 alpha at outer edge.
			_, _, _, firstA := g.Stops[0].Color.Components()
			_, _, _, lastA := g.Stops[len(g.Stops)-1].Color.Components()
			if lastA != 0 {
				t.Errorf("expected outer stop alpha 0, got %v", lastA)
			}
			if g.X1 == pointLight.X && g.Y1 == pointLight.Y && g.R2 == pointLight.Radius {
				pointGradOps++
				if firstA <= 0 {
					t.Errorf("expected positive point-light center alpha, got %v", firstA)
				}
				for i := 1; i < len(g.Stops); i++ {
					_, _, _, prevA := g.Stops[i-1].Color.Components()
					_, _, _, currA := g.Stops[i].Color.Components()
					if currA > prevA+1e-6 {
						t.Errorf("point light stop %d alpha %v increased over stop %d alpha %v", i, currA, i-1, prevA)
					}
				}
			} else if g.X1 == spotLight.X && g.Y1 == spotLight.Y && g.R2 == spotLight.Radius {
				spotGradOps++
				if firstA <= 0 {
					t.Errorf("expected positive spotlight wedge center alpha, got %v", firstA)
				}
			} else {
				t.Errorf("unexpected gradient center/radius: (%v,%v) r=%v", g.X1, g.Y1, g.R2)
			}
		case core.DrawArc:
			if math.Abs(float64(op.X-pointLight.X)) < 1e-4 && math.Abs(float64(op.Y-pointLight.Y)) < 1e-4 {
				pointArcs++
			} else if math.Abs(float64(op.X-spotLight.X)) < 1e-4 && math.Abs(float64(op.Y-spotLight.Y)) < 1e-4 {
				spotArcs++
				half := float64(spotLight.ConeSpread) * 0.5
				center := float64(spotLight.ConeAngle)
				wedgeHalf := float64(op.A1-op.A0) * 0.5
				wedgeCenter := float64(op.A0+op.A1) * 0.5
				if math.Abs(wedgeCenter-center) > 1e-4 {
					t.Errorf("spotlight wedge center = %v, want %v", wedgeCenter, center)
				}
				if wedgeHalf <= 0 || wedgeHalf >= half {
					t.Errorf("spotlight wedge half-angle %v outside (0, %v)", wedgeHalf, half)
				}
				if wedgeHalf >= prevSpotHalf {
					t.Errorf("spotlight wedges not nested strictly outside-in: %v after %v", wedgeHalf, prevSpotHalf)
				}
				prevSpotHalf = wedgeHalf
			}
		}
	}

	if saves != restores {
		t.Errorf("unbalanced Save/Restore: %d saves vs %d restores", saves, restores)
	}
	if ambientFills != 1 {
		t.Errorf("expected 1 ambient fill color op, got %d", ambientFills)
	}
	if pointGradOps != 1 || pointArcs != 1 {
		t.Errorf("expected 1 point light gradient and 1 arc, got grads=%d arcs=%d", pointGradOps, pointArcs)
	}
	if spotGradOps < 4 || spotArcs != spotGradOps {
		t.Errorf("expected multiple concentric spotlight wedges, got grads=%d arcs=%d", spotGradOps, spotArcs)
	}
	if lighterComposites != 2 {
		t.Errorf("expected 2 CompositeLighter ops (point + spot; disabled/culled skipped), got %d", lighterComposites)
	}

	// Verify CompositeScreen mode override.
	screenOps := app.Find("screen").Ops
	var screenComposites int
	for _, op := range screenOps {
		if op.Kind == core.DrawComposite && op.Composite == core.CompositeScreen {
			screenComposites++
		}
	}
	if screenComposites != 2 {
		t.Errorf("expected 2 CompositeScreen ops, got %d", screenComposites)
	}
}

func TestSceneLightCompositeModes(t *testing.T) {
	// Nil scene is safe and reports default CompositeLighter.
	var nilScene *lights2d.Scene
	nilScene.SetLightComposite(core.CompositeSourceOver)
	if got := nilScene.EffectiveLightComposite(); got != core.CompositeLighter {
		t.Fatalf("nil.EffectiveLightComposite() = %v, want CompositeLighter", got)
	}

	bounds := core.Bounds{Size: core.Size{Width: 300, Height: 200}}
	makeScene := func() *lights2d.Scene {
		return &lights2d.Scene{
			Ambient: core.RGBA(0x050814, 0.75),
			Lights: []*lights2d.Light{
				lights2d.NewLight(80, 100, 60, core.RGB(0xffaa44)),
				lights2d.NewLight(200, 100, 70, core.RGB(0x66ccff)),
			},
		}
	}

	defaultScene := makeScene()
	if got := defaultScene.EffectiveLightComposite(); got != core.CompositeLighter {
		t.Fatalf("default EffectiveLightComposite() = %v, want CompositeLighter", got)
	}

	sourceOverScene := makeScene()
	sourceOverScene.SetLightComposite(core.CompositeSourceOver)
	if sourceOverScene.LightComposite != core.CompositeSourceOver {
		t.Fatalf("SetLightComposite(SourceOver) left LightComposite = %v", sourceOverScene.LightComposite)
	}
	if got := sourceOverScene.EffectiveLightComposite(); got != core.CompositeSourceOver {
		t.Fatalf("SourceOver EffectiveLightComposite() = %v, want CompositeSourceOver", got)
	}

	screenScene := makeScene()
	screenScene.LightComposite = core.CompositeScreen
	if got := screenScene.EffectiveLightComposite(); got != core.CompositeScreen {
		t.Fatalf("Screen EffectiveLightComposite() = %v, want CompositeScreen", got)
	}

	switchedBackScene := makeScene()
	switchedBackScene.SetLightComposite(core.CompositeSourceOver)
	switchedBackScene.SetLightComposite(core.CompositeLighter)
	if got := switchedBackScene.EffectiveLightComposite(); got != core.CompositeLighter {
		t.Fatalf("switchedBack EffectiveLightComposite() = %v, want CompositeLighter", got)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "default",
				Style:  core.Style{Width: core.Pt(300), Height: core.Pt(200)},
				Draw:   func(d *core.Drawing, _ core.Size) { defaultScene.Draw(d, bounds) },
			},
			core.Canvas{
				TestID: "sourceOver",
				Style:  core.Style{Width: core.Pt(300), Height: core.Pt(200)},
				Draw:   func(d *core.Drawing, _ core.Size) { sourceOverScene.Draw(d, bounds) },
			},
			core.Canvas{
				TestID: "screen",
				Style:  core.Style{Width: core.Pt(300), Height: core.Pt(200)},
				Draw:   func(d *core.Drawing, _ core.Size) { screenScene.Draw(d, bounds) },
			},
			core.Canvas{
				TestID: "switchedBack",
				Style:  core.Style{Width: core.Pt(300), Height: core.Pt(200)},
				Draw:   func(d *core.Drawing, _ core.Size) { switchedBackScene.Draw(d, bounds) },
			},
		}}
	})

	// lightGlowComposites returns the CompositeOp active at each light glow's DrawFillGradient op.
	lightGlowComposites := func(ops []core.DrawOp) []core.CompositeOp {
		var stack []core.CompositeOp
		cur := core.CompositeSourceOver
		var got []core.CompositeOp
		for _, op := range ops {
			switch op.Kind {
			case core.DrawSave:
				stack = append(stack, cur)
			case core.DrawRestore:
				if len(stack) > 0 {
					cur = stack[len(stack)-1]
					stack = stack[:len(stack)-1]
				}
			case core.DrawComposite:
				cur = op.Composite
			case core.DrawFillGradient:
				got = append(got, cur)
			}
		}
		return got
	}

	cases := []struct {
		id   string
		want core.CompositeOp
	}{
		{id: "default", want: core.CompositeLighter},
		{id: "sourceOver", want: core.CompositeSourceOver},
		{id: "screen", want: core.CompositeScreen},
		{id: "switchedBack", want: core.CompositeLighter},
	}
	for _, tc := range cases {
		comps := lightGlowComposites(app.Find(tc.id).Ops)
		if len(comps) != 2 {
			t.Fatalf("%s: expected 2 light glow ops, got %d (%v)", tc.id, len(comps), comps)
		}
		for i, got := range comps {
			if got != tc.want {
				t.Errorf("%s: light %d composite = %v, want %v", tc.id, i, got, tc.want)
			}
		}
	}
}

func TestLightAttenuationAndSpotlightCone(t *testing.T) {
	// Omnidirectional light with default quadratic falloff (Falloff == 0 -> 2).
	omni := &lights2d.Light{
		X:         0,
		Y:         0,
		Radius:    100,
		Color:     core.RGB(0xffffff),
		Intensity: 1,
		Enabled:   true,
	}

	a0 := omni.AttenuationAt(0, 0)
	a25 := omni.AttenuationAt(25, 0)
	a50 := omni.AttenuationAt(50, 0)
	a75 := omni.AttenuationAt(75, 0)
	a100 := omni.AttenuationAt(100, 0)
	a120 := omni.AttenuationAt(120, 0)

	if math.Abs(float64(a0-1.0)) > 1e-5 {
		t.Errorf("at center: got %v, want 1", a0)
	}
	if math.Abs(float64(a50-0.25)) > 1e-5 {
		t.Errorf("at half radius with quadratic falloff: got %v, want 0.25", a50)
	}
	if !(a0 > a25 && a25 > a50 && a50 > a75 && a75 > a100) {
		t.Errorf("attenuation not strictly decreasing: %v, %v, %v, %v, %v", a0, a25, a50, a75, a100)
	}
	if a100 != 0 || a120 != 0 {
		t.Errorf("expected 0 attenuation at/beyond radius, got %v, %v", a100, a120)
	}

	// ConeSpread >= 2*pi must behave identically to omnidirectional.
	omniWide := *omni
	omniWide.ConeSpread = 2 * math.Pi
	for _, pt := range [][2]float32{{50, 0}, {-50, 0}, {0, 50}, {0, -50}} {
		if got, want := omniWide.AttenuationAt(pt[0], pt[1]), omni.AttenuationAt(pt[0], pt[1]); math.Abs(float64(got-want)) > 1e-5 {
			t.Errorf("ConeSpread=2pi at %v: got %v, want %v", pt, got, want)
		}
	}

	// Linear falloff check.
	linear := *omni
	linear.Falloff = 1
	if got := linear.AttenuationAt(50, 0); math.Abs(float64(got-0.5)) > 1e-5 {
		t.Errorf("linear falloff at half radius: got %v, want 0.5", got)
	}

	// Spotlight aimed along +X with 90-degree spread (pi/2).
	spot := &lights2d.Light{
		X:          0,
		Y:          0,
		Radius:     100,
		Color:      core.RGB(0xff8000),
		Intensity:  1,
		Falloff:    2,
		ConeAngle:  0,
		ConeSpread: math.Pi / 2,
		Enabled:    true,
	}
	onAxis := spot.AttenuationAt(50, 0)
	offAngle := spot.AttenuationAt(50*float32(math.Cos(math.Pi/8)), 50*float32(math.Sin(math.Pi/8)))
	outsidePosY := spot.AttenuationAt(0, 50)
	behind := spot.AttenuationAt(-50, 0)

	if math.Abs(float64(onAxis-0.25)) > 1e-5 {
		t.Errorf("spotlight on-axis attenuation: got %v, want 0.25", onAxis)
	}
	if !(offAngle > 0 && offAngle < onAxis) {
		t.Errorf("spotlight off-axis should attenuate smoothly: onAxis=%v offAngle=%v", onAxis, offAngle)
	}
	if outsidePosY != 0 || behind != 0 {
		t.Errorf("spotlight should emit 0 outside cone: +Y=%v -X=%v", outsidePosY, behind)
	}

	// EvaluateAt combines ambient and lights.
	scene := &lights2d.Scene{
		Ambient: core.RGB(0x102030),
		Lights:  []*lights2d.Light{omni},
	}
	r, g, b := scene.EvaluateAt(50, 0)
	ar, ag, ab, _ := scene.Ambient.Components()
	if math.Abs(float64(r)-ar-0.25) > 1e-4 || math.Abs(float64(g)-ag-0.25) > 1e-4 || math.Abs(float64(b)-ab-0.25) > 1e-4 {
		t.Errorf("EvaluateAt(50,0) = (%v,%v,%v), want (%v,%v,%v)", r, g, b, ar+0.25, ag+0.25, ab+0.25)
	}
}

func TestVisibilityPolygonEmptyScene(t *testing.T) {
	if poly := lights2d.VisibilityPolygon(core.Point{X: 10, Y: 20}, 0, nil); poly != nil {
		t.Fatalf("expected nil for radius 0, got %v", poly)
	}
	poly := lights2d.VisibilityPolygon(core.Point{X: 100, Y: 50}, 40, nil)
	if len(poly) != 4 {
		t.Fatalf("expected 4 bounding-box corners with no occluders, got %d: %+v", len(poly), poly)
	}
	if !lights2d.PointInPolygon(core.Point{X: 100, Y: 50}, poly) {
		t.Error("origin must lie inside visibility polygon")
	}
	if !lights2d.PointInPolygon(core.Point{X: 139, Y: 89}, poly) {
		t.Error("interior corner point must lie inside visibility polygon")
	}
	if lights2d.PointInPolygon(core.Point{X: 141, Y: 50}, poly) {
		t.Error("point outside radius box must lie outside visibility polygon")
	}
}

func TestVisibilityPolygonBoxOccluderAndShadows(t *testing.T) {
	origin := core.Point{X: 50, Y: 100}
	box := lights2d.RectOccluder(core.Bounds{
		Origin: core.Point{X: 100, Y: 80},
		Size:   core.Size{Width: 40, Height: 40},
	})
	occluders := []lights2d.Occluder{box}

	poly := lights2d.VisibilityPolygon(origin, 200, occluders)
	if len(poly) < 6 {
		t.Fatalf("expected shadowed visibility polygon with >= 6 vertices, got %d: %+v", len(poly), poly)
	}

	// Points directly behind the box must lie strictly outside the visibility
	// polygon, fail Visible(), and receive zero direct light.
	shadowed := []core.Point{
		{X: 160, Y: 100},
		{X: 200, Y: 100},
		{X: 180, Y: 110},
		{X: 180, Y: 90},
	}
	// Points in front of the box, opposite the box, and clear above/below the umbra.
	lit := []core.Point{
		{X: 80, Y: 100},
		{X: 20, Y: 100},
		{X: 120, Y: 40},
		{X: 120, Y: 160},
		{X: 180, Y: 20},
		{X: 180, Y: 180},
	}

	light := lights2d.NewLight(50, 100, 200, core.RGB(0xffffff))
	light.CastShadows = true
	scene := &lights2d.Scene{
		Ambient:   core.RGBA(0x000000, 0.8),
		Lights:    []*lights2d.Light{light},
		Occluders: occluders,
	}

	for _, pt := range shadowed {
		if lights2d.Visible(origin, pt, occluders) {
			t.Errorf("Visible(%v, %v) = true, want false (behind box)", origin, pt)
		}
		if lights2d.PointInPolygon(pt, poly) {
			t.Errorf("PointInPolygon(%v) = true, want false (behind box)", pt)
		}
		r, g, b := scene.EvaluateAt(float32(pt.X), float32(pt.Y))
		if r != 0 || g != 0 || b != 0 {
			t.Errorf("EvaluateAt(%v) behind box = (%v,%v,%v), want (0,0,0)", pt, r, g, b)
		}
	}

	for _, pt := range lit {
		if !lights2d.Visible(origin, pt, occluders) {
			t.Errorf("Visible(%v, %v) = false, want true (lit side)", origin, pt)
		}
		if !lights2d.PointInPolygon(pt, poly) {
			t.Errorf("PointInPolygon(%v) = false, want true (lit side)", pt)
		}
		r, g, b := scene.EvaluateAt(float32(pt.X), float32(pt.Y))
		if r <= 0 || g <= 0 || b <= 0 {
			t.Errorf("EvaluateAt(%v) on lit side = (%v,%v,%v), want > 0", pt, r, g, b)
		}
	}

	// DisableShadows lets the light through the occluder without touching the light.
	scene.DisableShadows = true
	if r, _, _ := scene.EvaluateAt(160, 100); r <= 0 {
		t.Errorf("with DisableShadows, EvaluateAt(160,100) = %v, want > 0", r)
	}
	if !light.CastShadows {
		t.Error("DisableShadows changed the light's CastShadows")
	}
	scene.DisableShadows = false
	if r, _, _ := scene.EvaluateAt(160, 100); r != 0 {
		t.Errorf("with shadows back on, EvaluateAt(160,100) = %v, want 0", r)
	}

	// Disabling CastShadows allows the light to pass through the occluder.
	light.CastShadows = false
	r, _, _ := scene.EvaluateAt(160, 100)
	if r <= 0 {
		t.Errorf("with CastShadows=false, EvaluateAt(160,100) = %v, want > 0", r)
	}
}

func TestVisibilityPolygonMultipleOccludersAndLongWall(t *testing.T) {
	origin := core.Point{X: 0, Y: 0}
	occluders := []lights2d.Occluder{
		// Two pillars with a narrow corridor along Y = 0.
		lights2d.RectOccluder(core.Bounds{
			Origin: core.Point{X: 40, Y: -60},
			Size:   core.Size{Width: 20, Height: 50},
		}),
		lights2d.RectOccluder(core.Bounds{
			Origin: core.Point{X: 40, Y: 10},
			Size:   core.Size{Width: 20, Height: 50},
		}),
		// A long wall extending beyond the light radius on both ends.
		lights2d.SegmentOccluder(lights2d.Segment{
			A: core.Point{X: -30, Y: -300},
			B: core.Point{X: -30, Y: 300},
		}),
	}

	poly := lights2d.VisibilityPolygon(origin, 120, occluders)
	if len(poly) < 4 {
		t.Fatalf("expected valid polygon, got %d vertices", len(poly))
	}

	// Through the corridor between the two pillars: must be visible.
	corridor := core.Point{X: 90, Y: 0}
	if !lights2d.Visible(origin, corridor, occluders) || !lights2d.PointInPolygon(corridor, poly) {
		t.Errorf("point %v through gap between pillars should be visible", corridor)
	}

	// Behind upper pillar, lower pillar, and long wall: must be shadowed.
	for _, pt := range []core.Point{{X: 90, Y: -50}, {X: 90, Y: 50}, {X: -60, Y: 0}, {X: -60, Y: 80}} {
		if lights2d.Visible(origin, pt, occluders) {
			t.Errorf("point %v should be occluded by Visible()", pt)
		}
		if lights2d.PointInPolygon(pt, poly) {
			t.Errorf("point %v should lie outside visibility polygon", pt)
		}
	}
}

func TestSceneDrawShadowClipOps(t *testing.T) {
	light := lights2d.NewLight(50, 100, 150, core.RGB(0xffeeaa))
	light.CastShadows = true
	scene := &lights2d.Scene{
		Ambient: core.RGBA(0x000010, 0.7),
		Lights:  []*lights2d.Light{light},
		Occluders: []lights2d.Occluder{
			lights2d.RectOccluder(core.Bounds{
				Origin: core.Point{X: 100, Y: 80},
				Size:   core.Size{Width: 30, Height: 40},
			}),
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "shadows",
			Style:  core.Style{Width: core.Pt(300), Height: core.Pt(200)},
			Draw: func(d *core.Drawing, size core.Size) {
				scene.Draw(d, core.Bounds{Size: size})
			},
		}
	})

	ops := app.Find("shadows").Ops
	var clips, fills, gradIdx, clipIdx int
	gradIdx, clipIdx = -1, -1
	for i, op := range ops {
		switch op.Kind {
		case core.DrawClip:
			clips++
			clipIdx = i
		case core.DrawFillGradient:
			gradIdx = i
		case core.DrawFill:
			fills++
		}
	}
	// Expect 2 clips: outer bounds clip + light visibility polygon clip.
	if clips != 2 {
		t.Errorf("expected 2 DrawClip ops (bounds + visibility polygon), got %d", clips)
	}
	if clipIdx == -1 || gradIdx == -1 || clipIdx > gradIdx {
		t.Errorf("expected visibility clip (idx %d) before gradient fill (idx %d)", clipIdx, gradIdx)
	}
	if fills < 1 {
		t.Errorf("expected light fill op, got %d", fills)
	}
}

// makeHemisphereSprite builds a w x w white diffuse disc with a matching
// tangent-space hemisphere normal map. Pixels outside the disc are transparent,
// and a single border pixel is given alpha=128 to test alpha preservation.
func makeHemisphereSprite(size int) (diffuse, normalMap *image.NRGBA) {
	diffuse = image.NewNRGBA(image.Rect(0, 0, size, size))
	normalMap = image.NewNRGBA(image.Rect(0, 0, size, size))
	center := float64(size) * 0.5
	radius := center - 2.0

	for y := range size {
		for x := range size {
			dx := (float64(x) + 0.5 - center) / radius
			dy := (float64(y) + 0.5 - center) / radius
			r2 := dx*dx + dy*dy
			if r2 > 1.0 {
				diffuse.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 0})
				normalMap.SetNRGBA(x, y, color.NRGBA{R: 128, G: 128, B: 255, A: 255})
				continue
			}
			nz := math.Sqrt(math.Max(0, 1.0-r2))
			diffuse.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			normalMap.SetNRGBA(x, y, color.NRGBA{
				R: uint8(math.Round((dx + 1.0) * 0.5 * 255.0)),
				G: uint8(math.Round((dy + 1.0) * 0.5 * 255.0)),
				B: uint8(math.Round((nz + 1.0) * 0.5 * 255.0)),
				A: 255,
			})
		}
	}
	// Mark one translucent pixel near the center for alpha-preservation checks.
	diffuse.SetNRGBA(size/2, size/2, color.NRGBA{R: 255, G: 255, B: 255, A: 128})
	return diffuse, normalMap
}

func TestShadeNormalMapHemisphereDirectionalSymmetry(t *testing.T) {
	const size = 32
	diffuse, normalMap := makeHemisphereSprite(size)

	ambient := core.RGB(0x101010)
	leftLight := lights2d.NewLight(-40, 16, 200, core.RGB(0xffffff))
	rightLight := lights2d.NewLight(72, 16, 200, core.RGB(0xffffff)) // mirror across x = 16

	litLeft := lights2d.ShadeNormalMap(diffuse, normalMap, 0, 0, ambient, []*lights2d.Light{leftLight}, 20)
	litRight := lights2d.ShadeNormalMap(diffuse, normalMap, 0, 0, ambient, []*lights2d.Light{rightLight}, 20)

	// Measure average brightness of left half vs right half of the opaque disc.
	avgHalf := func(img *image.NRGBA, x0, x1 int) float64 {
		var sum float64
		var count int
		for y := range size {
			for x := x0; x < x1; x++ {
				c := img.NRGBAAt(x, y)
				if c.A == 255 {
					sum += float64(c.R)
					count++
				}
			}
		}
		return sum / float64(count)
	}

	leftLitL := avgHalf(litLeft, 0, size/2)
	leftLitR := avgHalf(litLeft, size/2, size)
	rightLitL := avgHalf(litRight, 0, size/2)
	rightLitR := avgHalf(litRight, size/2, size)

	if leftLitL <= leftLitR+30 {
		t.Errorf("left-lit hemisphere did not brighten left half over right: left=%v right=%v", leftLitL, leftLitR)
	}
	if rightLitR <= rightLitL+30 {
		t.Errorf("right-lit hemisphere did not brighten right half over left: left=%v right=%v", rightLitL, rightLitR)
	}

	// Left-lit and right-lit must mirror each other across x = size/2.
	for y := range size {
		for x := range size {
			a := litLeft.NRGBAAt(x, y)
			b := litRight.NRGBAAt(size-1-x, y)
			// Skip the single translucent center pixel which isn't symmetric by x.
			if diffuse.NRGBAAt(x, y).A != diffuse.NRGBAAt(size-1-x, y).A {
				continue
			}
			diffR := int(a.R) - int(b.R)
			if diffR < 0 {
				diffR = -diffR
			}
			if diffR > 1 {
				t.Fatalf("mirror mismatch at (%d,%d): leftLit=%+v rightLitMirror=%+v", x, y, a, b)
			}
		}
	}

	// Verify transparent corners and translucent center preserve alpha cleanly.
	for _, corner := range [][2]int{{0, 0}, {size - 1, 0}, {0, size - 1}, {size - 1, size - 1}} {
		if got := litLeft.NRGBAAt(corner[0], corner[1]); got.A != 0 {
			t.Errorf("transparent pixel at %v got alpha %d, want 0", corner, got.A)
		}
	}
	if got := litLeft.NRGBAAt(size/2, size/2); got.A != 128 {
		t.Errorf("translucent center pixel got alpha %d, want 128", got.A)
	}
}

func TestShadeNormalMapRespectsOccludersAndSpotlights(t *testing.T) {
	diffuse, normalMap := makeHemisphereSprite(16)
	light := lights2d.NewLight(-30, 8, 150, core.RGB(0xffffff))
	light.CastShadows = true

	// Wall directly between light (-30, 8) and sprite [0..16, 0..16].
	wall := lights2d.RectOccluder(core.Bounds{
		Origin: core.Point{X: -12, Y: -20},
		Size:   core.Size{Width: 4, Height: 60},
	})
	ambient := core.RGB(0x202020)

	unblocked := &lights2d.Scene{Ambient: ambient, Lights: []*lights2d.Light{light}}
	blocked := &lights2d.Scene{Ambient: ambient, Lights: []*lights2d.Light{light}, Occluders: []lights2d.Occluder{wall}}

	imgOpen := unblocked.ShadeNormalMap(diffuse, normalMap, 0, 0, 20)
	imgBlocked := blocked.ShadeNormalMap(diffuse, normalMap, 0, 0, 20)

	cOpen := imgOpen.NRGBAAt(5, 8)
	cBlocked := imgBlocked.NRGBAAt(5, 8)
	if cOpen.R <= cBlocked.R+20 {
		t.Errorf("expected unblocked pixel (%d) much brighter than shadowed pixel (%d)", cOpen.R, cBlocked.R)
	}
	if cBlocked.R != 0x20 {
		t.Errorf("expected shadowed pixel to match pure ambient 0x20, got 0x%02x", cBlocked.R)
	}

	// Spotlight aimed away (-X) should also leave sprite at pure ambient.
	light.CastShadows = false
	light.ConeAngle = math.Pi
	light.ConeSpread = math.Pi / 3
	imgAway := unblocked.ShadeNormalMap(diffuse, normalMap, 0, 0, 20)
	if got := imgAway.NRGBAAt(5, 8).R; got != 0x20 {
		t.Errorf("spotlight aimed away gave 0x%02x, want pure ambient 0x20", got)
	}
}

func TestNormalSpriteCachingAndDraw(t *testing.T) {
	diffuse, normalMap := makeHemisphereSprite(16)
	ns := lights2d.NewNormalSprite(diffuse, normalMap)
	ns.X, ns.Y = 20, 30

	light := lights2d.NewLight(0, 38, 120, core.RGB(0xffffff))
	scene := &lights2d.Scene{
		Ambient: core.RGB(0x181818),
		Lights:  []*lights2d.Light{light},
	}

	src1 := ns.ImageSource(scene)
	if src1.Pixels == nil {
		t.Fatal("ImageSource.Pixels is nil")
	}
	if src1.Version != 1 {
		t.Fatalf("expected initial Version 1, got %d", src1.Version)
	}
	if src1.Size.Width != 16 || src1.Size.Height != 16 {
		t.Fatalf("unexpected ImageSource.Size: %+v", src1.Size)
	}

	// Verify premultiplied RGBA invariants on translucent pixel (8, 8).
	premul := src1.Pixels.RGBAAt(8, 8)
	if premul.A != 128 {
		t.Errorf("premultiplied center alpha = %d, want 128", premul.A)
	}
	if premul.R > premul.A || premul.G > premul.A || premul.B > premul.A {
		t.Errorf("invalid premultiplied pixel %+v (RGB > A)", premul)
	}

	// Second call with unchanged state must hit cache (same Version).
	src2 := ns.ImageSource(scene)
	if src2.Version != src1.Version {
		t.Errorf("unchanged scene incremented Version from %d to %d", src1.Version, src2.Version)
	}

	// Moving the light invalidates cache and bumps Version.
	light.X = 60
	src3 := ns.ImageSource(scene)
	if src3.Version != 2 {
		t.Errorf("moving light gave Version %d, want 2", src3.Version)
	}

	// Explicit Invalidate() forces recomputation even with unchanged scene.
	ns.Invalidate()
	src4 := ns.ImageSource(scene)
	if src4.Version != 3 {
		t.Errorf("Invalidate() gave Version %d, want 3", src4.Version)
	}

	// NormalSprite.Draw records a DrawImage op carrying Pixels.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "normal",
			Style:  core.Style{Width: core.Pt(100), Height: core.Pt(100)},
			Draw: func(d *core.Drawing, _ core.Size) {
				ns.Draw(d, scene)
			},
		}
	})
	ops := app.Find("normal").Ops
	if len(ops) != 1 || ops[0].Kind != core.DrawImage {
		t.Fatalf("expected 1 DrawImage op, got %+v", ops)
	}
	if ops[0].Image == nil || ops[0].Image.Pixels == nil {
		t.Fatalf("DrawImage op missing Pixels: %+v", ops[0].Image)
	}
	if ops[0].X != 20 || ops[0].Y != 30 {
		t.Errorf("DrawImage placed at (%v, %v), want (20, 30)", ops[0].X, ops[0].Y)
	}
}

func TestNormalSpriteHonorsDisableShadows(t *testing.T) {
	diffuse, normalMap := makeHemisphereSprite(16)
	ns := lights2d.NewNormalSprite(diffuse, normalMap)
	light := lights2d.NewLight(-30, 8, 150, core.RGB(0xffffff))
	light.CastShadows = true
	wall := lights2d.RectOccluder(core.Bounds{
		Origin: core.Point{X: -12, Y: -20},
		Size:   core.Size{Width: 4, Height: 60},
	})
	scene := &lights2d.Scene{Ambient: core.RGB(0x202020), Lights: []*lights2d.Light{light}, Occluders: []lights2d.Occluder{wall}}

	shadowed := ns.ImageSource(scene)
	if got := shadowed.Pixels.RGBAAt(5, 8).R; got != 0x20 {
		t.Fatalf("shadowed pixel R = 0x%02x, want pure ambient 0x20", got)
	}
	shadowedVersion := shadowed.Version

	scene.DisableShadows = true
	lit := ns.ImageSource(scene)
	if lit.Version == shadowedVersion {
		t.Fatalf("enabling DisableShadows kept Version %d; the cached shading is stale", lit.Version)
	}
	if got := lit.Pixels.RGBAAt(5, 8).R; got <= 0x20+20 {
		t.Errorf("with DisableShadows, pixel R = 0x%02x, want much brighter than ambient 0x20", got)
	}
	if want := scene.ShadeNormalMap(diffuse, normalMap, 0, 0, ns.LightZ).NRGBAAt(5, 8).R; lit.Pixels.RGBAAt(5, 8).R != want { // (5, 8) is opaque, so premultiplying leaves it unchanged
		t.Errorf("NormalSprite and Scene.ShadeNormalMap disagree with DisableShadows set")
	}

	scene.DisableShadows = false
	again := ns.ImageSource(scene)
	if again.Version == lit.Version {
		t.Fatalf("clearing DisableShadows kept Version %d; the cached shading is stale", again.Version)
	}
	if got := again.Pixels.RGBAAt(5, 8).R; got != 0x20 {
		t.Errorf("after clearing DisableShadows, pixel R = 0x%02x, want pure ambient 0x20", got)
	}
}

// evalRecordedAdditiveOps evaluates the recorded Scene.Draw ops (ambient rect +
// additive radial-gradient wedges) at world point (x, y) in premultiplied sRGB
// [0, 1], giving platform-independent verification alongside nsgotest.Render.
func evalRecordedAdditiveOps(ops []core.DrawOp, x, y float64) (r, g, b float64) {
	var grad *core.CanvasGradient
	var fill core.Color
	comp := core.CompositeSourceOver
	type state struct {
		grad *core.CanvasGradient
		fill core.Color
		comp core.CompositeOp
	}
	var stack []state
	var hasArc bool
	var arcX, arcY, arcR, arcA0, arcA1 float64
	var hasRect bool
	var rx, ry, rw, rh float64

	pointInArcWedge := func(px, py float64) bool {
		dx, dy := px-arcX, py-arcY
		if dx*dx+dy*dy > arcR*arcR {
			return false
		}
		sweep := arcA1 - arcA0
		if sweep >= 2*math.Pi-1e-6 {
			return true
		}
		ang := math.Atan2(dy, dx)
		rel := math.Mod(ang-arcA0, 2*math.Pi)
		if rel < 0 {
			rel += 2 * math.Pi
		}
		return rel <= sweep
	}

	for _, op := range ops {
		switch op.Kind {
		case core.DrawSave:
			stack = append(stack, state{grad: grad, fill: fill, comp: comp})
		case core.DrawRestore:
			if n := len(stack); n > 0 {
				s := stack[n-1]
				stack = stack[:n-1]
				grad, fill, comp = s.grad, s.fill, s.comp
			}
		case core.DrawComposite:
			comp = op.Composite
		case core.DrawFillColor:
			fill, grad = op.Color, nil
		case core.DrawFillGradient:
			grad = op.Gradient
		case core.DrawBeginPath:
			hasArc, hasRect = false, false
		case core.DrawRect:
			hasRect = true
			rx, ry, rw, rh = float64(op.X), float64(op.Y), float64(op.W), float64(op.H)
		case core.DrawArc:
			hasArc = true
			arcX, arcY, arcR = float64(op.X), float64(op.Y), float64(op.R)
			arcA0, arcA1 = float64(op.A0), float64(op.A1)
		case core.DrawFill:
			if hasRect && grad == nil && x >= rx && x <= rx+rw && y >= ry && y <= ry+rh {
				fr, fg, fb, fa := fill.Components()
				if comp == core.CompositeSourceOver {
					r = r*(1-fa) + fr*fa
					g = g*(1-fa) + fg*fa
					b = b*(1-fa) + fb*fa
				}
			}
			if hasArc && grad != nil && grad.Radial && pointInArcWedge(x, y) {
				dist := math.Hypot(x-float64(grad.X1), y-float64(grad.Y1))
				t := dist / float64(grad.R2)
				c := grad.At(t)
				cr, cg, cb, ca := c.Components()
				switch comp {
				case core.CompositeLighter:
					r = min(1, r+cr*ca)
					g = min(1, g+cg*ca)
					b = min(1, b+cb*ca)
				case core.CompositeScreen:
					sr, sg, sb := cr*ca, cg*ca, cb*ca
					r = 1 - (1-r)*(1-sr)
					g = 1 - (1-g)*(1-sg)
					b = 1 - (1-b)*(1-sb)
				default:
					r = r*(1-ca) + cr*ca
					g = g*(1-ca) + cg*ca
					b = b*(1-ca) + cb*ca
				}
			}
		}
	}
	return r, g, b
}

func TestSceneDrawSpotlightSmoothstepMatchesEvaluateAt(t *testing.T) {
	const W, H = 360, 360
	bounds := core.Bounds{Size: core.Size{Width: W, Height: H}}
	cx, cy := float32(180), float32(180)

	cases := []struct {
		name      string
		light     lights2d.Light
		ambient   core.Color
		radiiFrac []float64
		degStep   float64
		tol       float64
	}{
		{
			name: "issue1584_90deg_east",
			light: lights2d.Light{
				X: cx, Y: cy, Radius: 150,
				Color: core.RGB(0xffdd99), Intensity: 1.0, Falloff: 2.0,
				ConeAngle: 0, ConeSpread: math.Pi / 2, Enabled: true,
			},
			ambient:   core.RGBA(0x050814, 0.90),
			radiiFrac: []float64{0.25, 0.5, 0.75},
			degStep:   5,
			tol:       0.045,
		},
		{
			name: "wrap_around_west_pi",
			light: lights2d.Light{
				X: cx, Y: cy, Radius: 140,
				Color: core.RGB(0x88ccff), Intensity: 1.0, Falloff: 2.0,
				ConeAngle: math.Pi, ConeSpread: math.Pi / 2, Enabled: true,
			},
			ambient:   core.RGBA(0x080c18, 0.85),
			radiiFrac: []float64{0.3, 0.55},
			degStep:   5,
			tol:       0.045,
		},
		{
			name: "narrow_cone",
			light: lights2d.Light{
				X: cx, Y: cy, Radius: 150,
				Color: core.RGB(0xffffff), Intensity: 1.0, Falloff: 2.0,
				ConeAngle: -math.Pi / 4, ConeSpread: math.Pi / 8, Enabled: true,
			},
			ambient:   core.RGBA(0x000000, 1.0),
			radiiFrac: []float64{0.4, 0.65},
			degStep:   2,
			tol:       0.05,
		},
		{
			name: "wide_cone_270deg",
			light: lights2d.Light{
				X: cx, Y: cy, Radius: 140,
				Color: core.RGB(0xffaa66), Intensity: 0.9, Falloff: 2.0,
				ConeAngle: math.Pi / 3, ConeSpread: 1.5 * math.Pi, Enabled: true,
			},
			ambient:   core.RGBA(0x04060c, 0.90),
			radiiFrac: []float64{0.35, 0.6},
			degStep:   15,
			tol:       0.045,
		},
		{
			name: "default_zero_falloff_and_high_intensity",
			light: lights2d.Light{
				X: cx, Y: cy, Radius: 140,
				Color: core.RGB(0xffeeaa), Intensity: 2.0, Falloff: 0, // 0 defaults to quadratic 2
				ConeAngle: 0, ConeSpread: math.Pi / 2, Enabled: true,
			},
			ambient:   core.RGBA(0x000000, 1.0),
			radiiFrac: []float64{0.35, 0.6},
			degStep:   5,
			tol:       0.05,
		},
		{
			name: "linear_falloff",
			light: lights2d.Light{
				X: cx, Y: cy, Radius: 140,
				Color: core.RGB(0x66ffaa), Intensity: 1.0, Falloff: 1.0,
				ConeAngle: math.Pi / 2, ConeSpread: math.Pi / 3, Enabled: true,
			},
			ambient:   core.RGBA(0x000000, 1.0),
			radiiFrac: []float64{0.3, 0.5, 0.7},
			degStep:   5,
			tol:       0.05,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := tc.light
			scene := &lights2d.Scene{
				Ambient: tc.ambient,
				Lights:  []*lights2d.Light{&l},
			}
			d := &core.Drawing{}
			scene.Draw(d, bounds)

			img, renderErr := nsgotest.Render(d, bounds.Size, 1)
			hasNative := true
			if errors.Is(renderErr, nsgotest.ErrNoPlayer) {
				hasNative = false
			} else if renderErr != nil {
				t.Fatalf("nsgotest.Render: %v", renderErr)
			}

			halfDeg := float64(l.ConeSpread) * 0.5 * 180 / math.Pi
			spanDeg := min(180.0, halfDeg+15)

			for _, frac := range tc.radiiFrac {
				dist := float64(l.Radius) * frac
				for offDeg := -spanDeg; offDeg <= spanDeg; offDeg += tc.degStep {
					ang := float64(l.ConeAngle) + offDeg*math.Pi/180
					ix := int(math.Round(float64(l.X) + dist*math.Cos(ang)))
					iy := int(math.Round(float64(l.Y) + dist*math.Sin(ang)))
					if ix < 1 || ix >= W-1 || iy < 1 || iy >= H-1 {
						continue
					}
					px, py := float32(ix)+0.5, float32(iy)+0.5
					er, eg, eb := scene.EvaluateAt(px, py)
					wantR := min(1, float64(er))
					wantG := min(1, float64(eg))
					wantB := min(1, float64(eb))

					// Platform-independent op evaluation check.
					opR, opG, opB := evalRecordedAdditiveOps(d.Ops, float64(px), float64(py))
					if err := max(math.Abs(opR-wantR), max(math.Abs(opG-wantG), math.Abs(opB-wantB))); err > tc.tol {
						t.Errorf("ops at r=%.2fR offDeg=%.1f (%d,%d): got (%.3f,%.3f,%.3f), EvaluateAt (%.3f,%.3f,%.3f), diff=%.4f > %.4f",
							frac, offDeg, ix, iy, opR, opG, opB, wantR, wantG, wantB, err, tc.tol)
					}

					// Native offscreen canvas pixel check (macOS).
					if hasNative {
						c := img.RGBAAt(ix, iy)
						gotR, gotG, gotB := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
						if err := max(math.Abs(gotR-wantR), max(math.Abs(gotG-wantG), math.Abs(gotB-wantB))); err > tc.tol {
							t.Errorf("rendered pixel at r=%.2fR offDeg=%.1f (%d,%d): got (%.3f,%.3f,%.3f), EvaluateAt (%.3f,%.3f,%.3f), diff=%.4f > %.4f",
								frac, offDeg, ix, iy, gotR, gotG, gotB, wantR, wantG, wantB, err, tc.tol)
						}
					}
				}
			}
		})
	}
}

func TestSceneDrawSpotlightReproIssue1584AndSymmetry(t *testing.T) {
	const W, H = 320, 240
	bounds := core.Bounds{Size: core.Size{Width: W, Height: H}}
	spot := &lights2d.Light{
		X:          60,
		Y:          120,
		Radius:     200,
		Color:      core.RGB(0xffffff),
		Intensity:  1.0,
		Falloff:    2.0,
		ConeAngle:  0,
		ConeSpread: math.Pi / 2,
		Enabled:    true,
	}
	scene := &lights2d.Scene{
		Ambient: core.RGBA(0x000000, 1.0),
		Lights:  []*lights2d.Light{spot},
	}
	d := &core.Drawing{}
	scene.Draw(d, bounds)

	sample := func(deg float64) (evalR, gotR float64) {
		rad := deg * math.Pi / 180
		ix := int(math.Round(60 + 100*math.Cos(rad)))
		iy := int(math.Round(120 + 100*math.Sin(rad)))
		px, py := float32(ix)+0.5, float32(iy)+0.5
		er, _, _ := scene.EvaluateAt(px, py)
		opR, _, _ := evalRecordedAdditiveOps(d.Ops, float64(px), float64(py))
		return float64(er), opR
	}

	// Regression for issue #1584: at 40 deg off-axis (u = 40/45), brightness must
	// drop dramatically below the on-axis value rather than staying equal to it.
	eval0, op0 := sample(0)
	eval22, op22 := sample(22.5)
	eval40, op40 := sample(40)
	eval50, op50 := sample(50)
	if eval50 != 0 {
		t.Fatalf("expected EvaluateAt at 50deg outside 90deg cone to be 0, got %v", eval50)
	}
	if !(op0 > op22 && op22 > op40 && op40 >= op50 && op50 == 0) {
		t.Fatalf("expected monotonic smoothstep drop (0 > 22.5 > 40 >= 50 == 0), got %.3f, %.3f, %.3f, %.3f", op0, op22, op40, op50)
	}
	if op40 > op0*0.20 {
		t.Fatalf("40deg off-axis brightness %.3f is too high relative to on-axis %.3f (eval=%.3f vs %.3f)", op40, op0, eval40, eval0)
	}
	if math.Abs(op22-eval22) > 0.03 {
		t.Errorf("22.5deg off-axis op=%.3f vs eval=%.3f", op22, eval22)
	}

	// Native pixel check when windowless canvas player is available.
	img, err := nsgotest.Render(d, bounds.Size, 1)
	if errors.Is(err, nsgotest.ErrNoPlayer) {
		return
	}
	if err != nil {
		t.Fatalf("nsgotest.Render: %v", err)
	}
	pixAt := func(deg float64) float64 {
		rad := deg * math.Pi / 180
		ix := int(math.Round(60 + 100*math.Cos(rad)))
		iy := int(math.Round(120 + 100*math.Sin(rad)))
		return float64(img.RGBAAt(ix, iy).R) / 255.0
	}
	p0, p22, p40, p50 := pixAt(0), pixAt(22.5), pixAt(40), pixAt(50)
	if !(p0 > p22 && p22 > p40 && p40 >= p50 && p50 == 0) {
		t.Fatalf("rendered pixels not monotonically decreasing: 0=%.3f 22.5=%.3f 40=%.3f 50=%.3f", p0, p22, p40, p50)
	}
	if p40 > p0*0.20 {
		t.Fatalf("rendered 40deg pixel %.3f is too bright relative to on-axis %.3f", p40, p0)
	}
	// Symmetry between +deg and -deg across the beam axis.
	for _, deg := range []float64{10, 20, 30, 40} {
		pos, neg := pixAt(deg), pixAt(-deg)
		if math.Abs(pos-neg) > 0.02 {
			t.Errorf("asymmetric spotlight at ±%.0f deg: +%.3f vs -%.3f", deg, pos, neg)
		}
	}
}

func TestSceneDrawSpotlightCompositeModesAndOmnidirectionalEdgeCases(t *testing.T) {
	const W, H = 240, 240
	bounds := core.Bounds{Size: core.Size{Width: W, Height: H}}
	ambient := core.RGBA(0x101830, 0.90)
	ar, ag, ab, aa := ambient.Components()
	dstR, dstG, dstB := ar*aa, ag*aa, ab*aa

	spot := func() *lights2d.Light {
		return &lights2d.Light{
			X: 40, Y: 120, Radius: 160,
			Color: core.RGB(0xffcc88), Intensity: 1.0, Falloff: 2.0,
			ConeAngle: 0, ConeSpread: math.Pi / 2, Enabled: true,
		}
	}

	// Verify CompositeScreen and CompositeSourceOver preserve full on-axis peak
	// brightness and smoothly attenuate off-axis without multi-pass darkening.
	for _, tc := range []struct {
		name string
		set  func(*lights2d.Scene)
		want func(lr, lg, lb, atten float64) (float64, float64, float64)
	}{
		{
			name: "screen",
			set:  func(s *lights2d.Scene) { s.LightComposite = core.CompositeScreen },
			want: func(lr, lg, lb, atten float64) (float64, float64, float64) {
				return 1 - (1-dstR)*(1-lr*atten), 1 - (1-dstG)*(1-lg*atten), 1 - (1-dstB)*(1-lb*atten)
			},
		},
		{
			name: "source_over",
			set:  func(s *lights2d.Scene) { s.SetLightComposite(core.CompositeSourceOver) },
			want: func(lr, lg, lb, atten float64) (float64, float64, float64) {
				return dstR*(1-atten) + lr*atten, dstG*(1-atten) + lg*atten, dstB*(1-atten) + lb*atten
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := spot()
			scene := &lights2d.Scene{Ambient: ambient, Lights: []*lights2d.Light{l}}
			tc.set(scene)
			d := &core.Drawing{}
			scene.Draw(d, bounds)

			img, err := nsgotest.Render(d, bounds.Size, 1)
			hasNative := true
			if errors.Is(err, nsgotest.ErrNoPlayer) {
				hasNative = false
			} else if err != nil {
				t.Fatalf("nsgotest.Render: %v", err)
			}

			lr, lg, lb, _ := l.Color.Components()
			for _, deg := range []float64{0, 15, 25, 35, 40, 55} {
				rad := deg * math.Pi / 180
				ix := int(math.Round(40 + 60*math.Cos(rad)))
				iy := int(math.Round(120 + 60*math.Sin(rad)))
				px, py := float32(ix)+0.5, float32(iy)+0.5
				atten := float64(l.AttenuationAt(px, py))
				wr, wg, wb := tc.want(lr, lg, lb, atten)

				opR, opG, opB := evalRecordedAdditiveOps(d.Ops, float64(px), float64(py))
				if diff := max(math.Abs(opR-wr), max(math.Abs(opG-wg), math.Abs(opB-wb))); diff > 0.05 {
					t.Errorf("%s ops at deg=%.0f: got (%.3f,%.3f,%.3f), want (%.3f,%.3f,%.3f)", tc.name, deg, opR, opG, opB, wr, wg, wb)
				}
				if hasNative {
					c := img.RGBAAt(ix, iy)
					gr, gg, gb := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
					if diff := max(math.Abs(gr-wr), max(math.Abs(gg-wg), math.Abs(gb-wb))); diff > 0.05 {
						t.Errorf("%s pixel at deg=%.0f: got (%.3f,%.3f,%.3f), want (%.3f,%.3f,%.3f)", tc.name, deg, gr, gg, gb, wr, wg, wb)
					}
				}
			}
		})
	}

	// ConeSpread <= 0 and ConeSpread >= 2*pi must remain omnidirectional single-arc draws.
	for _, spread := range []float32{0, -1, 2 * math.Pi, 3 * math.Pi} {
		l := &lights2d.Light{
			X: 120, Y: 120, Radius: 80,
			Color: core.RGB(0xffffff), Intensity: 1.0, Falloff: 2.0,
			ConeSpread: spread, Enabled: true,
		}
		scene := &lights2d.Scene{Lights: []*lights2d.Light{l}}
		d := &core.Drawing{}
		scene.Draw(d, bounds)
		arcs := 0
		for _, op := range d.Ops {
			if op.Kind == core.DrawArc {
				arcs++
			}
		}
		if arcs != 1 {
			t.Errorf("ConeSpread=%v drew %d arcs, want 1 omnidirectional circle", spread, arcs)
		}
	}

	// Spotlight with CastShadows clips cleanly against occluders while preserving smoothstep on the lit side.
	l := spot()
	l.CastShadows = true
	wall := lights2d.RectOccluder(core.Bounds{
		Origin: core.Point{X: 90, Y: 125},
		Size:   core.Size{Width: 12, Height: 60},
	})
	shadowScene := &lights2d.Scene{
		Ambient:   core.RGBA(0x000000, 1.0),
		Lights:    []*lights2d.Light{l},
		Occluders: []lights2d.Occluder{wall},
	}
	d := &core.Drawing{}
	shadowScene.Draw(d, bounds)
	if img, err := nsgotest.Render(d, bounds.Size, 1); err == nil {
		// Lower half (y=150, behind wall at x=130) must stay dark; upper mirror (y=90, x=130) must be lit.
		litPix := img.RGBAAt(130, 90)
		shadPix := img.RGBAAt(130, 150)
		if shadPix.R > 2 {
			t.Errorf("shadowed spotlight pixel behind wall = %+v, want dark ambient", shadPix)
		}
		if litPix.R < 20 {
			t.Errorf("unblocked spotlight pixel = %+v, want brightly lit", litPix)
		}
	} else if !errors.Is(err, nsgotest.ErrNoPlayer) {
		t.Fatalf("nsgotest.Render shadow spotlight: %v", err)
	}
}

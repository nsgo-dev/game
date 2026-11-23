package skeletal_test

import (
	_ "embed"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/skeletal"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

//go:embed testdata/runner.skel.yaml
var runnerYAML []byte

const ms = time.Millisecond

var runnerAtlas = &sprite.Atlas{
	Image: core.ImageSource{URI: "asset:runner.png"},
	Frames: map[string]sprite.Frame{
		"arm_back":    {Name: "arm_back", X: 0, Y: 0, W: 6, H: 14, Width: 6, Height: 14},
		"leg_back":    {Name: "leg_back", X: 8, Y: 0, W: 6, H: 20, Width: 6, Height: 20},
		"torso":       {Name: "torso", X: 16, Y: 0, W: 12, H: 18, Width: 12, Height: 18},
		"head":        {Name: "head", X: 32, Y: 0, W: 12, H: 12, Width: 12, Height: 12},
		"head_blink":  {Name: "head_blink", X: 48, Y: 0, W: 12, H: 12, Width: 12, Height: 12},
		"leg_front":   {Name: "leg_front", X: 64, Y: 0, W: 6, H: 20, Width: 6, Height: 20},
		"arm_front":   {Name: "arm_front", X: 72, Y: 0, W: 6, H: 14, Width: 6, Height: 14},
		"torso_armor": {Name: "torso_armor", X: 80, Y: 0, W: 12, H: 18, Width: 12, Height: 18},
		"helmet":      {Name: "helmet", X: 96, Y: 0, W: 12, H: 12, Width: 12, Height: 12},
		"sword":       {Name: "sword", X: 112, Y: 0, W: 4, H: 16, Width: 4, Height: 16},
	},
}

func loadRunner(t *testing.T) *skeletal.SkeletonData {
	t.Helper()
	data, err := skeletal.Load(runnerYAML)
	if err != nil {
		t.Fatalf("Load(runner.skel.yaml): %v", err)
	}
	return data
}

func near(a, b, eps float32) bool {
	return math.Abs(float64(a-b)) <= float64(eps)
}

func checkBone(t *testing.T, name string, got, want skeletal.BoneTransform) {
	t.Helper()
	const eps = 1e-4
	if !near(got.X, want.X, eps) || !near(got.Y, want.Y, eps) ||
		!near(got.Rotation, want.Rotation, eps) ||
		!near(got.ScaleX, want.ScaleX, eps) || !near(got.ScaleY, want.ScaleY, eps) {
		t.Fatalf("bone %q = %+v, want %+v", name, got, want)
	}
}

func sameOps(t *testing.T, got, want []core.DrawOp) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d ops, want %d:\n%+v\nwant:\n%+v", len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		gf := []float32{g.X, g.Y, g.X1, g.Y1, g.X2, g.Y2, g.W, g.H, g.R, g.A, g.A0, g.A1, g.RY}
		wf := []float32{w.X, w.Y, w.X1, w.Y1, w.X2, w.Y2, w.W, w.H, w.R, w.A, w.A0, w.A1, w.RY}
		for j := range gf {
			if !near(gf[j], wf[j], 1e-4) {
				t.Fatalf("op %d is %+v, want %+v", i, g, w)
			}
		}
		if g.Kind != w.Kind || !reflect.DeepEqual(g.Image, w.Image) {
			t.Fatalf("op %d is %+v, want %+v", i, g, w)
		}
		g.X, g.Y, g.X1, g.Y1, g.X2, g.Y2, g.W, g.H, g.R, g.A, g.A0, g.A1, g.RY = w.X, w.Y, w.X1, w.Y1, w.X2, w.Y2, w.W, w.H, w.R, w.A, w.A0, w.A1, w.RY
		g.Image = w.Image
		if !reflect.DeepEqual(g, w) {
			t.Fatalf("op %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLoadRunnerAndDefaults(t *testing.T) {
	data := loadRunner(t)
	if data.Version != skeletal.FormatVersion || data.Name != "runner" {
		t.Fatalf("version %d name %q, want %d runner", data.Version, data.Name, skeletal.FormatVersion)
	}
	if len(data.Bones) != 7 {
		t.Fatalf("%d bones, want 7", len(data.Bones))
	}
	for _, b := range data.Bones {
		if b.ScaleX != 1 || b.ScaleY != 1 {
			t.Fatalf("bone %q scale (%v, %v), want (1, 1)", b.Name, b.ScaleX, b.ScaleY)
		}
	}
	if len(data.Slots) != 6 {
		t.Fatalf("%d slots, want 6", len(data.Slots))
	}
	for _, sl := range data.Slots {
		if sl.Alpha != 1 {
			t.Fatalf("slot %q alpha %v, want 1", sl.Name, sl.Alpha)
		}
	}
	if len(data.Skins) != 2 {
		t.Fatalf("%d skins, want 2", len(data.Skins))
	}
	if data.Animations["walk"].Duration != 800*ms || !data.Animations["walk"].Loop {
		t.Fatalf("walk anim = %+v, want 800ms loop", data.Animations["walk"])
	}
	if data.Animations["run"].Duration != 400*ms || !data.Animations["run"].Loop {
		t.Fatalf("run anim = %+v, want 400ms loop", data.Animations["run"])
	}
	if data.Animations["jump"].Duration != 500*ms || data.Animations["jump"].Loop {
		t.Fatalf("jump anim = %+v, want 500ms non-loop", data.Animations["jump"])
	}
}

func TestLoadValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{
			name: "invalid YAML",
			doc:  `{`,
			want: "skeletal:",
		},
		{
			name: "missing version",
			doc:  `{"bones":[{"name":"root"}]}`,
			want: "unsupported version 0",
		},
		{
			name: "unsupported version",
			doc:  `{"version":99,"bones":[{"name":"root"}]}`,
			want: "unsupported version 99",
		},
		{
			name: "duplicate bone",
			doc:  `{"version":1,"bones":[{"name":"root"},{"name":"root"}]}`,
			want: `duplicate bone "root"`,
		},
		{
			name: "unknown parent",
			doc:  `{"version":1,"bones":[{"name":"torso","parent":"missing"}]}`,
			want: `unknown parent "missing"`,
		},
		{
			name: "self cycle",
			doc:  `{"version":1,"bones":[{"name":"a","parent":"a"}]}`,
			want: `bone cycle`,
		},
		{
			name: "multi-bone cycle",
			doc:  `{"version":1,"bones":[{"name":"a","parent":"c"},{"name":"b","parent":"a"},{"name":"c","parent":"b"}]}`,
			want: `bone cycle`,
		},
		{
			name: "duplicate slot",
			doc:  `{"version":1,"bones":[{"name":"root"}],"slots":[{"name":"s","bone":"root"},{"name":"s","bone":"root"}]}`,
			want: `duplicate slot "s"`,
		},
		{
			name: "slot unknown bone",
			doc:  `{"version":1,"bones":[{"name":"root"}],"slots":[{"name":"s","bone":"nope"}]}`,
			want: `unknown bone "nope"`,
		},
		{
			name: "slot alpha out of range",
			doc:  `{"version":1,"bones":[{"name":"root"}],"slots":[{"name":"s","bone":"root","alpha":1.5}]}`,
			want: `alpha`,
		},
		{
			name: "skin unknown slot",
			doc:  `{"version":1,"bones":[{"name":"root"}],"slots":[{"name":"s","bone":"root"}],"skins":{"default":{"ghost":{"frame":"x"}}}}`,
			want: `unknown slot "ghost"`,
		},
		{
			name: "animation unknown bone",
			doc:  `{"version":1,"bones":[{"name":"root"}],"animations":{"a":{"bones":{"nope":{"rotate":[{"time":0,"angle":1}]}}}}}`,
			want: `unknown bone "nope"`,
		},
		{
			name: "animation unknown slot",
			doc:  `{"version":1,"bones":[{"name":"root"}],"animations":{"a":{"slots":{"nope":{"alpha":[{"time":0,"value":1}]}}}}}`,
			want: `unknown slot "nope"`,
		},
		{
			name: "negative duration",
			doc:  `{"version":1,"bones":[{"name":"root"}],"animations":{"a":{"duration":-1}}}`,
			want: `negative duration`,
		},
		{
			name: "unsorted keyframes",
			doc:  `{"version":1,"bones":[{"name":"root"}],"animations":{"a":{"bones":{"root":{"rotate":[{"time":0.5,"angle":0},{"time":0.2,"angle":1}]}}}}}`,
			want: `precedes previous`,
		},
		{
			name: "invalid curve",
			doc:  `{"version":1,"bones":[{"name":"root"}],"animations":{"a":{"bones":{"root":{"rotate":[{"time":0,"angle":0,"curve":"elastic"}]}}}}}`,
			want: `unknown curve`,
		},
		{
			name: "invalid bezier x",
			doc:  `{"version":1,"bones":[{"name":"root"}],"animations":{"a":{"bones":{"root":{"rotate":[{"time":0,"angle":0,"curve":"bezier","bezier":[-0.1,0,0.5,1]}]}}}}}`,
			want: `control points`,
		},
		{
			name: "slot alpha keyframe out of range",
			doc:  `{"version":1,"bones":[{"name":"root"}],"slots":[{"name":"s","bone":"root"}],"animations":{"a":{"slots":{"s":{"alpha":[{"time":0,"value":-0.2}]}}}}}`,
			want: `out of range`,
		},
		{
			name: "empty document",
			doc:  "  \n",
			want: "empty rig document",
		},
		{
			name: "unknown top-level field",
			doc:  "version: 1\nbones: [{name: root}]\nrig: x\n",
			want: "field rig not found",
		},
		{
			name: "unknown keyframe field",
			doc:  "version: 1\nbones: [{name: root}]\nanimations:\n  a:\n    bones:\n      root:\n        rotate: [{time: 0, angle: 0, ease: in}]\n",
			want: "field ease not found",
		},
		{
			name: "duplicate key",
			doc:  "version: 1\nversion: 1\n",
			want: "already defined",
		},
		{
			name: "trailing document",
			doc:  "version: 1\nbones: [{name: root}]\n---\nversion: 1\n",
			want: "trailing YAML document",
		},
		{
			name: "NaN bone offset",
			doc:  "version: 1\nbones: [{name: root, x: .nan}]\n",
			want: "not finite",
		},
		{
			name: "infinite attachment scale",
			doc:  "version: 1\nbones: [{name: root}]\nslots: [{name: s, bone: root}]\nskins: {default: {s: {frame: f, scaleX: .inf}}}\n",
			want: "not finite",
		},
		{
			name: "NaN slot alpha",
			doc:  "version: 1\nbones: [{name: root}]\nslots: [{name: s, bone: root, alpha: .nan}]\n",
			want: "out of range",
		},
		{
			name: "NaN rotate angle",
			doc:  "version: 1\nbones: [{name: root}]\nanimations: {a: {bones: {root: {rotate: [{time: 0, angle: .nan}]}}}}\n",
			want: "not finite",
		},
		{
			name: "NaN duration",
			doc:  "version: 1\nbones: [{name: root}]\nanimations: {a: {duration: .nan}}\n",
			want: "not a number of seconds",
		},
		{
			name: "duration beyond MaxSeconds",
			doc:  "version: 1\nbones: [{name: root}]\nanimations: {a: {duration: 1e7}}\n",
			want: "not a number of seconds",
		},
		{
			name: "keyframe time beyond MaxSeconds",
			doc:  "version: 1\nbones: [{name: root}]\nanimations: {a: {bones: {root: {rotate: [{time: 1e300, angle: 0}]}}}}\n",
			want: "invalid time",
		},
		{
			name: "bezier points on a linear curve",
			doc:  "version: 1\nbones: [{name: root}]\nanimations: {a: {bones: {root: {rotate: [{time: 0, angle: 0, bezier: [0, 0, 1, 1]}]}}}}\n",
			want: "bezier control points given",
		},
		{
			name: "NaN bezier point",
			doc:  "version: 1\nbones: [{name: root}]\nanimations: {a: {bones: {root: {rotate: [{time: 0, angle: 0, curve: bezier, bezier: [0, .nan, 1, 1]}]}}}}\n",
			want: "not finite",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := skeletal.Load([]byte(tc.doc))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestChildBeforeParentAndDerivedDuration(t *testing.T) {
	raw := []byte(`version: 1
bones:
  - {name: child, parent: parent, x: 10}
  - {name: parent, y: -5, rotation: 1.5707963}
animations:
  ping:
    bones:
      child:
        translate:
          - {time: 0, x: 0, y: 0}
          - {time: 0.35, x: 5, y: 0}
`)
	data, err := skeletal.Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := data.Animations["ping"].Duration; got != 350*ms {
		t.Fatalf("derived duration = %v, want 350ms", got)
	}
	skel := skeletal.NewSkeleton(data, nil)
	skel.X, skel.Y = 20, 30
	skel.UpdateWorldTransforms()

	// parent is at (20, 25) rotated +pi/2 (clockwise), so child's local (10, 0) lands at (20, 35).
	got, ok := skel.WorldBone("child")
	if !ok {
		t.Fatal("missing child bone")
	}
	checkBone(t, "child", got, skeletal.BoneTransform{X: 20, Y: 35, Rotation: math.Pi / 2, ScaleX: 1, ScaleY: 1})
}

func TestBindPoseForwardKinematics(t *testing.T) {
	data := loadRunner(t)
	skel := skeletal.NewSkeleton(data, runnerAtlas)
	skel.X, skel.Y = 100, 200
	skel.UpdateWorldTransforms()

	want := map[string]skeletal.BoneTransform{
		"root":  {X: 100, Y: 200, Rotation: 0, ScaleX: 1, ScaleY: 1},
		"torso": {X: 100, Y: 182, Rotation: 0, ScaleX: 1, ScaleY: 1},
		"head":  {X: 100, Y: 166, Rotation: 0, ScaleX: 1, ScaleY: 1},
		"arm_l": {X: 95, Y: 168, Rotation: 0, ScaleX: 1, ScaleY: 1},
		"arm_r": {X: 105, Y: 168, Rotation: 0, ScaleX: 1, ScaleY: 1},
		"leg_l": {X: 97, Y: 182, Rotation: 0, ScaleX: 1, ScaleY: 1},
		"leg_r": {X: 103, Y: 182, Rotation: 0, ScaleX: 1, ScaleY: 1},
	}
	for name, w := range want {
		got, ok := skel.WorldBone(name)
		if !ok {
			t.Fatalf("WorldBone(%q) missing", name)
		}
		checkBone(t, name, got, w)
	}
	if _, ok := skel.WorldBone("no_such_bone"); ok {
		t.Fatal("WorldBone returned ok for unknown bone")
	}

	// Root rotated +pi/2 (clockwise) and scaled 2x: local (0, -34) -> scaled (0, -68) -> rotated (+68, 0).
	skel.Rotation = math.Pi / 2
	skel.ScaleX, skel.ScaleY = 2, 2
	skel.UpdateWorldTransforms()
	head, _ := skel.WorldBone("head")
	checkBone(t, "head rotated+scaled", head, skeletal.BoneTransform{
		X: 168, Y: 200, Rotation: math.Pi / 2, ScaleX: 2, ScaleY: 2,
	})

	// FlipX mirrors X positions across root and negates child rotations.
	skel.Rotation = 0
	skel.ScaleX, skel.ScaleY = 1, 1
	skel.FlipX = true
	if err := skel.Play("run", true); err != nil {
		t.Fatal(err)
	}
	// At run t=0: torso local (0, -16), rot=+0.25, scale=(1.05, 0.95).
	// Under FlipX: torso world X=100, Y=184, rot=-0.25, scale=(-1.05, 0.95).
	torso, _ := skel.WorldBone("torso")
	checkBone(t, "torso flipped", torso, skeletal.BoneTransform{
		X: 100, Y: 184, Rotation: -0.25, ScaleX: -1.05, ScaleY: 0.95,
	})
	// head local (0, -16), rot=-0.2 relative to torso:
	// scaled=(0, -16*0.95=-15.2); rotated by -0.25: dx = -sin(-0.25)*(-15.2) = -15.2*sin(0.25), dy = cos(-0.25)*(-15.2).
	// Under negative handedness (ScaleX*ScaleY < 0), world rot = -0.25 - (-0.2) = -0.05.
	headFlip, _ := skel.WorldBone("head")
	sinT, cosT := float32(math.Sin(0.25)), float32(math.Cos(0.25))
	checkBone(t, "head flipped", headFlip, skeletal.BoneTransform{
		X:        100 - 15.2*sinT,
		Y:        184 - 15.2*cosT,
		Rotation: -0.05,
		ScaleX:   -1.05,
		ScaleY:   0.95,
	})
}

func TestBindPoseDrawMatchesHandWrittenOps(t *testing.T) {
	data := loadRunner(t)
	skel := skeletal.NewSkeleton(data, runnerAtlas)
	skel.X, skel.Y = 100, 200

	// Hand-built sprite batch for the exact same bind pose in slot draw order.
	mk := func(frame string, x, y, ox, oy float32) *sprite.Sprite {
		s := sprite.NewSprite(runnerAtlas.Frames[frame])
		s.X, s.Y, s.OriginX, s.OriginY = x, y, ox, oy
		return &s
	}
	handBatch := &sprite.Batch{
		Atlas: runnerAtlas,
		Sprites: []*sprite.Sprite{
			mk("arm_back", 95, 168, 3, 2),
			mk("leg_back", 97, 182, 3, 2),
			mk("torso", 100, 182, 6, 16),
			mk("head", 100, 166, 6, 12),
			mk("leg_front", 103, 182, 3, 2),
			mk("arm_front", 105, 168, 3, 2),
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "skel", Style: core.Style{Width: core.Pt(300), Height: core.Pt(300)}, Draw: func(d *core.Drawing, _ core.Size) {
				skel.Draw(d)
			}},
			core.Canvas{TestID: "batch", Style: core.Style{Width: core.Pt(300), Height: core.Pt(300)}, Draw: func(d *core.Drawing, _ core.Size) {
				handBatch.Draw(d)
			}},
			core.Canvas{TestID: "hand", Style: core.Style{Width: core.Pt(300), Height: core.Pt(300)}, Draw: func(d *core.Drawing, _ core.Size) {
				img := runnerAtlas.Image
				// Exact back-to-front DrawImageFrom calls: dest = (X - OriginX, Y - OriginY).
				d.DrawImageFrom(img, 0, 0, 6, 14, 92, 166, 6, 14)    // arm_back
				d.DrawImageFrom(img, 8, 0, 6, 20, 94, 180, 6, 20)    // leg_back
				d.DrawImageFrom(img, 16, 0, 12, 18, 94, 166, 12, 18) // torso
				d.DrawImageFrom(img, 32, 0, 12, 12, 94, 154, 12, 12) // head
				d.DrawImageFrom(img, 64, 0, 6, 20, 100, 180, 6, 20)  // leg_front
				d.DrawImageFrom(img, 72, 0, 6, 14, 102, 166, 6, 14)  // arm_front
			}},
		}}
	})

	got := app.Find("skel").Ops
	if n := len(gametest.ImageDraws(got)); n != 6 {
		t.Fatalf("got %d image draws, want 6", n)
	}
	sameOps(t, got, app.Find("batch").Ops)
	gametest.AssertSameImages(t, got, app.Find("hand").Ops)
}

func TestSkinSwitchingAndSlotAttachmentOverride(t *testing.T) {
	data := loadRunner(t)
	skel := skeletal.NewSkeleton(data, runnerAtlas)
	skel.X, skel.Y = 50, 100

	if skel.Skin() != "default" {
		t.Fatalf("initial skin %q, want default", skel.Skin())
	}
	if err := skel.SetSkin("missing"); err == nil {
		t.Fatal("SetSkin(missing) succeeded, want error")
	}
	if skel.Skin() != "default" {
		t.Fatalf("skin changed to %q after failed SetSkin", skel.Skin())
	}

	if err := skel.SetSkin("armored"); err != nil {
		t.Fatal(err)
	}
	skel.SetAttachment("arm_r", "sword")
	skel.SetAttachment("leg_l", "") // hide back leg

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "skel", Style: core.Style{Width: core.Pt(200), Height: core.Pt(200)}, Draw: func(d *core.Drawing, _ core.Size) {
				skel.Draw(d)
			}},
		}}
	})
	ims := gametest.ImageDraws(app.Find("skel").Ops)
	// 5 visible slots in order: arm_l (arm_back, X=0), torso (torso_armor, X=80), head (helmet, X=96), leg_r (leg_front, X=64), arm_r (sword, X=112).
	wantSrcX := []float32{0, 80, 96, 64, 112}
	if len(ims) != len(wantSrcX) {
		t.Fatalf("got %d image draws, want %d: %+v", len(ims), len(wantSrcX), ims)
	}
	for i, wx := range wantSrcX {
		if ims[i].SX != wx {
			t.Fatalf("image draw %d src X = %v, want %v", i, ims[i].SX, wx)
		}
	}

	// Clearing the override restores leg_l from "default" fallback.
	skel.ClearAttachment("leg_l")
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "skel", Style: core.Style{Width: core.Pt(200), Height: core.Pt(200)}, Draw: func(d *core.Drawing, _ core.Size) {
				skel.Draw(d)
			}},
		}}
	})
	if got := len(gametest.ImageDraws(app2.Find("skel").Ops)); got != 6 {
		t.Fatalf("after ClearAttachment got %d image draws, want 6", got)
	}
}

func TestAnimationPlaybackOnGameLoop(t *testing.T) {
	data := loadRunner(t)
	walker := skeletal.NewSkeleton(data, runnerAtlas)
	jumper := skeletal.NewSkeleton(data, runnerAtlas)

	if err := walker.Play("nope", true); err == nil {
		t.Fatal("Play(nope) succeeded, want error")
	}
	if err := walker.Crossfade("nope", 100*ms, true); err == nil {
		t.Fatal("Crossfade(nope) succeeded, want error")
	}

	if err := walker.Play("walk", true); err != nil {
		t.Fatal(err)
	}
	if err := jumper.Play("jump", false); err != nil {
		t.Fatal(err)
	}

	l := &game.Loop{Rate: 100, Update: func(dt time.Duration) {
		walker.Update(dt)
		jumper.Update(dt)
	}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})

	// At 50ms: jumper is still on stepped crouch (+4 -> torso Y = -14).
	app.Advance(50 * ms)
	jt, _ := jumper.WorldBone("torso")
	checkBone(t, "jumper stepped crouch", jt, skeletal.BoneTransform{X: 0, Y: -14, Rotation: 0, ScaleX: 1, ScaleY: 1})

	// At 100ms: walker's bezier [0.25, 0.1, 0.25, 1.0] eases out rapidly (past linear midpoint -19).
	app.Advance(50 * ms)
	wt, _ := walker.WorldBone("torso")
	if wt.Y >= -19.2 || wt.Y <= -20 {
		t.Fatalf("walker bezier torso Y at 100ms = %v, want between -20 and -19.2", wt.Y)
	}
	// Jumper hits peak launch at 100ms: Y = -18 + (-12) = -30.
	jt, _ = jumper.WorldBone("torso")
	checkBone(t, "jumper launch", jt, skeletal.BoneTransform{X: 0, Y: -30, Rotation: 0, ScaleX: 1, ScaleY: 1})

	// At 250ms: jumper slot arm_l alpha is 0.5 (emits Save + SetAlpha(0.5) + Transform + DrawImageFrom + Restore).
	app.Advance(150 * ms)
	appAlpha := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "j", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				jumper.Draw(d)
			}},
		}}
	})
	foundHalfAlpha := false
	for _, op := range appAlpha.Find("j").Ops {
		if op.Kind == core.DrawAlpha && near(op.A, 0.5, 1e-4) {
			foundHalfAlpha = true
		}
	}
	if !foundHalfAlpha {
		t.Fatalf("expected SetAlpha(0.5) op for jumper at 250ms, ops: %+v", appAlpha.Find("j").Ops)
	}

	// Advance to 650ms: walker's head slot switches to head_blink (src X=48); jumper (500ms one-shot) is Done.
	app.Advance(400 * ms)
	if !jumper.Done() {
		t.Fatal("one-shot jump is not Done after 650ms")
	}
	if walker.Done() {
		t.Fatal("looping walk reported Done")
	}
	jt, _ = jumper.WorldBone("torso")
	checkBone(t, "jumper settled", jt, skeletal.BoneTransform{X: 0, Y: -18, Rotation: 0, ScaleX: 1, ScaleY: 1})

	appBlink := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "w", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) {
				walker.Draw(d)
			}},
		}}
	})
	foundBlink := false
	for _, im := range gametest.ImageDraws(appBlink.Find("w").Ops) {
		if im.SX == 48 {
			foundBlink = true
		}
	}
	if !foundBlink {
		t.Fatalf("expected head_blink frame (src X=48) at 650ms, ops: %+v", appBlink.Find("w").Ops)
	}

	// Advance to 1000ms (1.0s): walk (0.8s loop) wraps to 0.2s where torso Y = -20 and leg_l rot = 0.
	app.Advance(350 * ms)
	wt, _ = walker.WorldBone("torso")
	checkBone(t, "walker wrapped torso", wt, skeletal.BoneTransform{X: 0, Y: -20, Rotation: 0.05, ScaleX: 1, ScaleY: 1})
	wl, _ := walker.WorldBone("leg_l")
	if !near(wl.Rotation, 0.05, 1e-4) {
		t.Fatalf("walker wrapped leg_l world rot = %v, want 0.05 (torso 0.05 + leg_l 0)", wl.Rotation)
	}
}

func TestShortestAngleWrapAcrossPi(t *testing.T) {
	raw := []byte(`version: 1
bones:
  - {name: arm}
animations:
  neg:
    duration: 1
    bones:
      arm:
        rotate:
          - {time: 0, angle: -3}
  pos:
    duration: 1
    bones:
      arm:
        rotate:
          - {time: 0, angle: 3}
  wrap:
    duration: 0.4
    bones:
      arm:
        rotate:
          - {time: 0, angle: 3}
          - {time: 0.4, angle: -3}
`)
	data, err := skeletal.Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	skel := skeletal.NewSkeleton(data, nil)
	if err := skel.Play("wrap", false); err != nil {
		t.Fatal(err)
	}

	l := &game.Loop{Rate: 100, Update: func(dt time.Duration) { skel.Update(dt) }}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})

	// Shortest arc from +3.0 to -3.0 goes through +pi (delta = 2*pi - 6.0 ≈ +0.2831853).
	delta := float32(2*math.Pi - 6.0)
	steps := []struct {
		advance time.Duration
		wantCos float32
		wantSin float32
	}{
		{0, float32(math.Cos(3.0)), float32(math.Sin(3.0))},
		{100 * ms, float32(math.Cos(float64(3.0 + delta*0.25))), float32(math.Sin(float64(3.0 + delta*0.25)))},
		{100 * ms, -1, 0}, // exact pi at 50%!
		{100 * ms, float32(math.Cos(float64(3.0 + delta*0.75))), float32(math.Sin(float64(3.0 + delta*0.75)))},
		{100 * ms, float32(math.Cos(-3.0)), float32(math.Sin(-3.0))},
	}
	for i, st := range steps {
		if st.advance > 0 {
			app.Advance(st.advance)
		}
		b, _ := skel.WorldBone("arm")
		gotCos, gotSin := float32(math.Cos(float64(b.Rotation))), float32(math.Sin(float64(b.Rotation)))
		if !near(gotCos, st.wantCos, 1e-4) || !near(gotSin, st.wantSin, 1e-4) {
			t.Fatalf("step %d: rot=%v (cos=%v, sin=%v), want (cos=%v, sin=%v)", i, b.Rotation, gotCos, gotSin, st.wantCos, st.wantSin)
		}
	}

	// Crossfade from +3.0 to -3.0 must also cross +/-pi rather than 0.
	if err := skel.Play("pos", true); err != nil {
		t.Fatal(err)
	}
	if err := skel.Crossfade("neg", 200*ms, true); err != nil {
		t.Fatal(err)
	}
	app.Advance(100 * ms) // 50% crossfade -> +/-pi (cos = -1, not +1).
	b, _ := skel.WorldBone("arm")
	if !near(float32(math.Cos(float64(b.Rotation))), -1, 1e-4) {
		t.Fatalf("crossfade midpoint across pi gave rot=%v (cos=%v), want +/-pi (cos=-1)", b.Rotation, math.Cos(float64(b.Rotation)))
	}
}

func TestWalkToRunCrossfadeBlend(t *testing.T) {
	data := loadRunner(t)
	skel := skeletal.NewSkeleton(data, runnerAtlas)
	if err := skel.Play("walk", true); err != nil {
		t.Fatal(err)
	}

	l := &game.Loop{Rate: 100, Update: func(dt time.Duration) { skel.Update(dt) }}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})

	// Advance walk to t=0.20s so walk's 0.20..0.40s window is purely linear on every track.
	app.Advance(200 * ms)

	// Begin a 200ms crossfade from walk (at 0.20s) into run (starting at 0.00s).
	if err := skel.Crossfade("run", 200*ms, true); err != nil {
		t.Fatal(err)
	}
	if skel.Animation() != "run" || !skel.Blending() {
		t.Fatalf("animation=%q blending=%v, want run true", skel.Animation(), skel.Blending())
	}

	lerp := func(a, b, w float32) float32 { return a + (b-a)*w }

	// Check exact intermediate poses at 0%, 25%, 50%, 75% and 100% blend progress.
	for i, w := range []float32{0, 0.25, 0.5, 0.75, 1.0} {
		if i > 0 {
			app.Advance(50 * ms)
		}
		walkT := 0.20 + 0.20*w
		runT := 0.20 * w

		// Expected walk local pose at walkT in [0.20, 0.40]:
		walkTorsoY := float32(-20.0 + 10.0*(walkT-0.20))
		walkTorsoRot := float32(0.05)
		walkLegLRot := float32(-0.4 + 2.0*walkT)
		walkArmLRot := float32(0.4 - 2.0*walkT)

		// Expected run local pose at runT in [0.00, 0.20]:
		runTorsoY := float32(-16.0 - 20.0*runT)
		runTorsoRot := float32(0.25)
		runTorsoSX := float32(1.05 - 0.5*runT)
		runTorsoSY := float32(0.95 + 0.5*runT)
		runLegLRot := float32(-0.8 + 8.0*runT)
		runArmLRot := float32(0.8 - 8.0*runT)

		wantTorso := skeletal.BoneTransform{
			X:        0,
			Y:        lerp(walkTorsoY, runTorsoY, w),
			Rotation: lerp(walkTorsoRot, runTorsoRot, w),
			ScaleX:   lerp(1, runTorsoSX, w),
			ScaleY:   lerp(1, runTorsoSY, w),
		}
		gotTorso, _ := skel.WorldBone("torso")
		checkBone(t, "torso", gotTorso, wantTorso)

		wantLegLRot := wantTorso.Rotation + lerp(walkLegLRot, runLegLRot, w)
		gotLegL, _ := skel.WorldBone("leg_l")
		sinT, cosT := float32(math.Sin(float64(wantTorso.Rotation))), float32(math.Cos(float64(wantTorso.Rotation)))
		wantLegL := skeletal.BoneTransform{
			X:        wantTorso.X + cosT*(-3*wantTorso.ScaleX),
			Y:        wantTorso.Y + sinT*(-3*wantTorso.ScaleX),
			Rotation: wantLegLRot,
			ScaleX:   wantTorso.ScaleX,
			ScaleY:   wantTorso.ScaleY,
		}
		checkBone(t, "leg_l", gotLegL, wantLegL)

		wantArmLRot := wantTorso.Rotation + lerp(walkArmLRot, runArmLRot, w)
		gotArmL, _ := skel.WorldBone("arm_l")
		if !near(gotArmL.Rotation, wantArmLRot, 1e-4) {
			t.Fatalf("blend %.0f%%: arm_l rot = %v, want %v", w*100, gotArmL.Rotation, wantArmLRot)
		}

		wantBlending := w < 1.0
		if skel.Blending() != wantBlending {
			t.Fatalf("blend %.0f%%: Blending() = %v, want %v", w*100, skel.Blending(), wantBlending)
		}
	}

	// Advancing another 100ms continues purely in run (at run t=0.30s: torso Y = -18, scale = (1, 1), leg_l local rot = 0).
	app.Advance(100 * ms)
	gotTorso, _ := skel.WorldBone("torso")
	checkBone(t, "post-blend run torso", gotTorso, skeletal.BoneTransform{
		X: 0, Y: -18, Rotation: 0.25, ScaleX: 1, ScaleY: 1,
	})
	gotLegL, _ := skel.WorldBone("leg_l")
	if !near(gotLegL.Rotation, 0.25, 1e-4) {
		t.Fatalf("post-blend run leg_l rot = %v, want 0.25", gotLegL.Rotation)
	}

	// Interrupting a crossfade mid-blend snapshots the current pose with zero pop.
	if err := skel.Crossfade("walk", 200*ms, true); err != nil {
		t.Fatal(err)
	}
	app.Advance(100 * ms) // mid-blend
	midTorso, _ := skel.WorldBone("torso")
	if err := skel.Crossfade("jump", 100*ms, false); err != nil {
		t.Fatal(err)
	}
	afterInterrupt, _ := skel.WorldBone("torso")
	checkBone(t, "interrupt crossfade continuity", afterInterrupt, midTorso)
}

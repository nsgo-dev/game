package pixelfx_test

import (
	"encoding/json"
	"testing"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/internal/pixelfxtest"
)

// rigPoses are the poses tools/pixelfxref draws with pixelfx's rig into testdata/ref/rig: keep the two lists in step.
var rigPoses = map[string]pixelfx.Pose{
	"stand":      {},
	"ready":      pixelfx.Ready,
	"sword":      pixelfx.Ready.With(pixelfx.SA.Is(-0.6), pixelfx.FX.Is(12), pixelfx.FY.Is(-24)).Hold("sword"),
	"sword-two":  pixelfx.Pose{}.With(pixelfx.Two.On(), pixelfx.SA.Is(-1.2), pixelfx.FX.Is(6), pixelfx.FY.Is(-30), pixelfx.Crouch.Is(2)).Hold("sword"),
	"greatsword": pixelfx.Pose{}.With(pixelfx.Two.On(), pixelfx.SA.Is(-2.2), pixelfx.FX.Is(-4), pixelfx.FY.Is(-36), pixelfx.Lean.Is(-2)).Hold("greatsword"),
	"dagger":     pixelfx.Pose{}.With(pixelfx.SA.Is(0.3), pixelfx.SB.Is(2.6), pixelfx.FX.Is(13), pixelfx.FY.Is(-20), pixelfx.BX.Is(-8), pixelfx.BY.Is(-22)).Hold("dagger").HoldOff("dagger"),
	"spear":      pixelfx.Pose{}.With(pixelfx.Two.On(), pixelfx.SA.Is(0.1), pixelfx.FX.Is(10), pixelfx.FY.Is(-22), pixelfx.Lean.Is(3), pixelfx.Crouch.Is(3)).Hold("spear"),
	"hammer":     pixelfx.Pose{}.With(pixelfx.Two.On(), pixelfx.SA.Is(-1.9), pixelfx.FX.Is(2), pixelfx.FY.Is(-38), pixelfx.Yaw.Is(0.4)).Hold("hammer"),
	"scythe":     pixelfx.Pose{}.With(pixelfx.Two.On(), pixelfx.SA.Is(-1.4), pixelfx.FX.Is(4), pixelfx.FY.Is(-34), pixelfx.Face.Is(-1)).Hold("scythe"),
	"claw":       pixelfx.Pose{}.With(pixelfx.SA.Is(0.2), pixelfx.FX.Is(14), pixelfx.FY.Is(-18), pixelfx.BX.Is(9), pixelfx.BY.Is(-26), pixelfx.ArmsFront.On()).Hold("claw").HoldOff("claw"),
	"bow":        pixelfx.Pose{}.With(pixelfx.Pull.On(), pixelfx.Nocked.On(), pixelfx.SA.Is(0.05), pixelfx.FX.Is(14), pixelfx.FY.Is(-26), pixelfx.BX.Is(2), pixelfx.BY.Is(-26)).Hold("bow"),
	"pistol":     pixelfx.Pose{}.With(pixelfx.SA.Is(0), pixelfx.FX.Is(15), pixelfx.FY.Is(-27), pixelfx.ToeF.On()).Hold("pistol"),
	"launcher":   pixelfx.Pose{}.With(pixelfx.Two.On(), pixelfx.SA.Is(0.15), pixelfx.FX.Is(8), pixelfx.FY.Is(-27), pixelfx.WL.Is(0.8), pixelfx.WZ.Is(-1)).Hold("launcher"),
	"handle":     pixelfx.Pose{}.With(pixelfx.SA.Is(-0.4), pixelfx.FX.Is(10), pixelfx.FY.Is(-20), pixelfx.HX.Is(1), pixelfx.HY.Is(-1), pixelfx.Twist.Is(2), pixelfx.Turn.Is(0.3)).Hold("handle"),
	"air":        pixelfx.Pose{}.With(pixelfx.Y.Is(-20), pixelfx.Crouch.Is(-2), pixelfx.FX.Is(-8), pixelfx.FY.Is(-40), pixelfx.BX.Is(10), pixelfx.BY.Is(-42), pixelfx.FFX.Is(5), pixelfx.FFY.Is(-9), pixelfx.FBX.Is(-4), pixelfx.FBY.Is(-6), pixelfx.ToeB.On()),
	"spin":       pixelfx.Pose{}.With(pixelfx.Yaw.Is(2.4), pixelfx.SA.Is(1.2), pixelfx.FX.Is(10), pixelfx.FY.Is(-20), pixelfx.FZ.Is(6), pixelfx.BZ.Is(-6), pixelfx.FZZ.Is(3), pixelfx.BZZ.Is(-3)).Hold("sword"),
}

// The rig reference images: each pose's pixels with its feet at (rigX, rigY), and its source pixels.
const rigW, rigH, rigX, rigY = 200, 140, 100, 110

func TestRigMatchesPixelfx(t *testing.T) {
	rig := pixelfx.NewRig()
	for name, pose := range rigPoses {
		r := rig.Posed(pose)
		color, src := make([]byte, rigW*rigH*4), make([]byte, rigW*rigH*4)
		for i := range r.N {
			x, y := rigX+int(r.Xs[i]), rigY+int(r.Ys[i])
			if x < 0 || y < 0 || x >= rigW || y >= rigH {
				continue
			}
			j, c, s := (y*rigW+x)*4, r.Color[i], int(r.Src[i])+1
			color[j], color[j+1], color[j+2], color[j+3] = byte(c), byte(c>>8), byte(c>>16), 255
			src[j], src[j+1], src[j+3] = byte(s), byte(s>>8), 255
			if r.Ink[i] == 1 {
				src[j+2] = 255
			}
		}
		for _, img := range []struct {
			file string
			pix  []byte
		}{{name + ".png", color}, {name + "-src.png", src}} {
			w, h, want, err := pixelfxtest.ReadPNG(pixelfxtest.Ref(t, "rig", img.file))
			if err != nil {
				t.Fatal(err)
			}
			if w != rigW || h != rigH {
				t.Fatalf("%s is %dx%d", img.file, w, h)
			}
			d := pixelfxtest.Compare(img.pix, want, w, h)
			t.Logf("%s: %d pixels differ, largest difference %d", img.file, d.Differing, d.MaxDelta)
			if !d.OK() {
				t.Errorf("%s: %d of %d pixels differ from pixelfx's rig (largest difference %d), first at (%d, %d)", img.file, d.Differing, d.Total, d.MaxDelta, d.FirstX, d.FirstY)
			}
		}
	}
}

func TestRigCachesPosesThatRoundAlike(t *testing.T) {
	rig := pixelfx.NewRig()
	a := rig.Posed(pixelfx.Ready.With(pixelfx.FX.Is(11.2)))
	if b := rig.Posed(pixelfx.Ready.With(pixelfx.FX.Is(10.8))); a != b {
		t.Fatal("poses that round to the same pixels were drawn twice")
	}
	if c := rig.Posed(pixelfx.Ready.With(pixelfx.FX.Is(12))); a == c {
		t.Fatal("poses a pixel apart share a drawing")
	}
	if d := rig.Posed(pixelfx.Ready.With(pixelfx.Y.Is(-3))); d == a {
		t.Fatal("a pose in the air shares the drawing of one on the floor")
	}
}

func TestSkeletonAndParts(t *testing.T) {
	s := pixelfx.SkeletonOf(pixelfx.Ready)
	if len(s.Joints) != 20 || len(s.Bones) != 19 || len(s.Handles) != 4 {
		t.Fatalf("%d joints, %d bones, %d handles", len(s.Joints), len(s.Bones), len(s.Handles))
	}
	if head, ankle := s.Joints["head"], s.Joints["ankleN"]; head[1] >= ankle[1] {
		t.Fatalf("the head (%v) is not above the ankle (%v)", head, ankle)
	}
	parts := pixelfx.PosedParts(pixelfx.Ready.Hold("sword"))
	names := map[string]bool{}
	for _, p := range parts {
		names[p.Name] = true
	}
	for _, want := range []string{"head", "chest", "handN", "weapon"} {
		if !names[want] {
			t.Errorf("PosedParts has no %s: %v", want, names)
		}
	}
	if f := pixelfx.Shortfall(pixelfx.Pose{}.With(pixelfx.FX.Is(60), pixelfx.FY.Is(-30))); f < 20 {
		t.Errorf("a hand asked to reach 60 px falls %v short, want well over 20", f)
	}
}

func TestPoseAtStepsAndSnaps(t *testing.T) {
	a, b := pixelfx.Ready, pixelfx.Ready.With(pixelfx.FX.Is(23), pixelfx.X.Is(12))
	track := []pixelfx.Key{{T: 0, Pose: a}, {T: 1, Pose: b, How: "line"}, {T: 2, Pose: a, How: "snap"}}
	if x, _ := pixelfx.PoseAt(track, 0.5).Get(pixelfx.FX); x != 17 {
		t.Errorf("halfway along a line, FX is %v, want 17", x)
	}
	// limbs hold on 12 fps steps; the feet move smoothly
	p := pixelfx.PoseAt(track, 0.5+1.0/24)
	if x, _ := p.Get(pixelfx.FX); x != 17 {
		t.Errorf("half a step later, FX is %v, want it held at 17", x)
	}
	if x, _ := p.Get(pixelfx.X); x <= 6 {
		t.Errorf("half a step later, X is %v, want past 6", x)
	}
	if x, _ := pixelfx.PoseAt(track, 1.5).Get(pixelfx.FX); x != 23 {
		t.Errorf("before a snap, FX is %v, want the earlier key's 23", x)
	}
	if got := pixelfx.LerpPose(pixelfx.Pose{}.With(pixelfx.HX.Is(2)), pixelfx.Pose{}, 0.5); got.Or(pixelfx.HX, 0) != 1 {
		t.Errorf("an unsaid HX eases back to 0: %v", got.Or(pixelfx.HX, 0))
	}
}

func TestActTimingIsPlainData(t *testing.T) {
	e := &pixelfx.Effect{
		Meta: pixelfx.Meta{Size: [2]int{64, 64}, Origin: [2]int{32, 60}, Duration: 1, FPS: 30},
		Act: &pixelfx.Act{
			Targets: []float64{50},
			Poses:   []pixelfx.Key{{T: -0.3, Pose: pixelfx.Ready}, {T: 1.4, Pose: pixelfx.Ready.With(pixelfx.ToeF.On()).Hold("sword"), How: "snap"}},
			Hits:    []pixelfx.Hit{{T: 0.2, Stop: 0.1, Push: pixelfx.Val(0), Skin: "ice", At: pixelfx.Val(0)}, {T: 0.5, NoTarget: true}},
		},
		New: func(*pixelfx.Options) pixelfx.Instance { return &pixelfx.Funcs{} },
	}
	tm := pixelfx.ActTiming(e)
	if tm.Windup != 0.3 || tm.Recovery < 0.39 || tm.Recovery > 0.41 || len(tm.Hits) != 2 || tm.Hits[0].Push != 0 || tm.Hits[1].Push != 0 || *tm.Hits[0].At != 0 {
		t.Fatalf("timing %+v", tm)
	}
	data, err := json.Marshal(tm.Poses[1].Pose)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != `{"bx":9,"by":-21,"crouch":3,"fbx":-7,"ffx":7,"fx":11,"fy":-21,"toeF":true,"weapon":"sword"}` {
		t.Fatalf("pose JSON %s", got)
	}
}

func TestPerformanceRunsFromTheFirstPoseAndFreezes(t *testing.T) {
	var hits []float64
	e := &pixelfx.Effect{
		Meta: pixelfx.Meta{Size: [2]int{64, 64}, Origin: [2]int{32, 60}, Duration: 0.5, FPS: 30, Actor: 2},
		Act: &pixelfx.Act{
			Targets: []float64{40},
			Poses:   []pixelfx.Key{{T: -0.2, Pose: pixelfx.Ready}, {T: 0.6, Pose: pixelfx.Ready.With(pixelfx.FX.Is(14))}},
			Hits:    []pixelfx.Hit{{T: 0.1, Stop: 0.1, Shake: 0.2}},
			Dust:    []pixelfx.DustKick{{T: 0, X: 0}},
		},
		New: func(*pixelfx.Options) pixelfx.Instance { return &pixelfx.Funcs{} },
	}
	rig := pixelfx.NewRig()
	b := pixelfx.ActBounds(e, rig)
	pf := pixelfx.Perform(e, pixelfx.Options{X: -b.X0 + 6, Y: -b.Y0 + 6}, rig)
	pf.OnHit = func(h pixelfx.Hit) { hits = append(hits, h.T) }
	if pf.T() != -0.2 {
		t.Fatalf("starts at %v, want the first pose's -0.2", pf.T())
	}
	frame := pixelfx.NewFrame(int(b.X1-b.X0)+12, int(b.Y1-b.Y0)+12, 0, 0)
	frozen := 0
	for range 120 {
		pf.Update(1.0 / 60)
		if pf.Frozen() {
			frozen++
			if x, y := pf.Shake(); x == 0 && y == 0 && frozen == 1 {
				t.Error("no shake on the frame the hit lands")
			}
		}
		frame.Clear()
		pf.Draw(frame, true, true)
		if pf.Done() {
			break
		}
	}
	if !pf.Done() || len(hits) != 1 || frozen < 5 || frozen > 7 {
		t.Fatalf("done %v, hits %v, %d frozen frames (want about 6)", pf.Done(), hits, frozen)
	}
	if c, _ := count(frame); c < 200 {
		t.Fatalf("the last frame has %d pixels; the character and the target should be there", c)
	}
}

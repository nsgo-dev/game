package vehicle3d_test

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/game/pkg/vehicle3d"
)

func TestGhostRecorderInterval(t *testing.T) {
	r := &vehicle3d.GhostRecorder{Interval: 3}
	for i := range 10 {
		r.Record(physics3d.Vec3{X: float32(i)}, physics3d.Quat{})
	}
	g := r.Ghost(60, 9)
	if r.Steps() != 10 || len(g.Frames) != 4 || g.Steps() != 9 || g.Interval != 3 || g.Rate != 60 || g.LapSteps != 9 || g.Version != vehicle3d.GhostVersion {
		t.Fatalf("ghost = %+v, recorder steps %d", g, r.Steps())
	}
	for i, f := range g.Frames {
		if f.P[0] != float32(3*i) {
			t.Fatalf("frame %d at x = %v, want %d", i, f.P[0], 3*i)
		}
		if f.R != [4]float32{0, 0, 0, 1} {
			t.Fatalf("frame %d rotation %v, want identity", i, f.R)
		}
	}
	// The ghost is a copy: recording on does not change it.
	r.Reset()
	r.Record(physics3d.Vec3{X: 100}, physics3d.QuatIdentity())
	if g.Frames[0].P[0] != 0 || r.Steps() != 1 {
		t.Fatal("Reset or Record changed an earlier ghost")
	}
	var every vehicle3d.GhostRecorder
	every.Record(physics3d.Vec3{}, physics3d.QuatIdentity())
	every.Record(physics3d.Vec3{}, physics3d.QuatIdentity())
	if g := every.Ghost(60, 0); len(g.Frames) != 2 || g.Interval != 1 {
		t.Fatalf("zero Interval ghost = %+v", g)
	}
}

func TestGhostRecorderFinishEndsOnTheLine(t *testing.T) {
	frame := func(i int) physics3d.Vec3 { return physics3d.Vec3{X: float32(i)} }
	// A 7-step lap sampled every 3 steps: frames at 0, 3 and 6 from Record, and the finish at 7 from Finish.
	r := &vehicle3d.GhostRecorder{Interval: 3}
	for i := range 7 {
		r.Record(frame(i), physics3d.QuatIdentity())
	}
	r.Finish(frame(7), physics3d.QuatIdentity())
	g := r.Ghost(60, 7)
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(g.Frames) != 4 || g.Steps() != 7 || g.Frames[3].P[0] != 7 {
		t.Fatalf("off-grid finish ghost = %+v", g)
	}
	if p, _ := g.Pose(6.5); !approx(p.X, 6.5, 1e-6) {
		t.Fatalf("pose between the last sample and the finish = %+v", p)
	}
	if p, _ := g.Pose(4.5); !approx(p.X, 4.5, 1e-6) {
		t.Fatalf("pose between samples = %+v", p)
	}
	if p, _ := g.Pose(7); p.X != 7 {
		t.Fatalf("pose at the finish = %+v", p)
	}

	// A lap that is a multiple of Interval gets no extra frame.
	r.Reset()
	for i := range 6 {
		r.Record(frame(i), physics3d.QuatIdentity())
	}
	r.Finish(frame(6), physics3d.QuatIdentity())
	if g := r.Ghost(60, 6); len(g.Frames) != 3 || g.Steps() != 6 || g.Validate() != nil {
		t.Fatalf("on-grid finish ghost = %+v", g)
	}
}

func TestGhostPoseInterpolates(t *testing.T) {
	quarter := physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, math.Pi/2)
	g := &vehicle3d.Ghost{Version: 1, Rate: 60, Interval: 2, Frames: []vehicle3d.GhostFrame{
		{P: [3]float32{0, 0, 0}, R: [4]float32{0, 0, 0, 1}},
		{P: [3]float32{2, 4, -6}, R: [4]float32{quarter.X, quarter.Y, quarter.Z, quarter.W}},
	}}
	pos, rot := g.Pose(1)
	if pos != (physics3d.Vec3{X: 1, Y: 2, Z: -3}) {
		t.Fatalf("midpoint position = %+v", pos)
	}
	if a := rot.AngleTo(physics3d.QuatIdentity()); !approx(a, math.Pi/4, 1e-5) {
		t.Fatalf("midpoint rotation is %v rad from identity, want pi/4", a)
	}
	if p, _ := g.Pose(-5); p != (physics3d.Vec3{}) {
		t.Fatalf("before start = %+v", p)
	}
	if p, _ := g.Pose(math.NaN()); p != (physics3d.Vec3{}) {
		t.Fatalf("NaN step = %+v", p)
	}
	if p, _ := g.Pose(100); p != (physics3d.Vec3{X: 2, Y: 4, Z: -6}) {
		t.Fatalf("after end = %+v", p)
	}
	var empty vehicle3d.Ghost
	if p, r := empty.Pose(3); p != (physics3d.Vec3{}) || r != physics3d.QuatIdentity() || empty.Steps() != 0 {
		t.Fatal("empty ghost pose is not the origin")
	}
}

func TestGhostRoundTrip(t *testing.T) {
	g := &vehicle3d.Ghost{Version: 1, Name: "best", Rate: 60, Interval: 2, LapSteps: 1, Frames: []vehicle3d.GhostFrame{
		{P: [3]float32{0.1, 0.65, -1e-8}, R: [4]float32{0, 0.70710677, 0, 0.70710677}},
		{P: [3]float32{1e7, -3.25, 2}, R: [4]float32{0, 0, 0, -1}},
	}}
	a, err := vehicle3d.EncodeGhost(g)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(a), "- p: [0.1, 0.65, -1e-08]") {
		t.Fatalf("frame positions are not flow-style lines:\n%s", a)
	}
	got, err := vehicle3d.ParseGhost(a)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, g) {
		t.Fatalf("round trip changed the ghost:\n%+v\n%+v", got, g)
	}
	b, err := vehicle3d.EncodeGhost(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("EncodeGhost is not deterministic")
	}
}

func TestParseGhostRejects(t *testing.T) {
	for name, doc := range map[string]string{
		"empty":           "",
		"version":         "version: 2\nrate: 60\ninterval: 1\nframes: []\n",
		"rate":            "version: 1\nrate: 0\ninterval: 1\nframes: []\n",
		"interval":        "version: 1\nrate: 60\ninterval: 0\nframes: []\n",
		"lap steps":       "version: 1\nrate: 60\ninterval: 1\nlapSteps: -1\nframes: []\n",
		"unknown field":   "version: 1\nrate: 60\ninterval: 1\nframes: []\nspeed: 3\n",
		"unknown frame":   "version: 1\nrate: 60\ninterval: 1\nframes:\n  - {p: [0, 0, 0], r: [0, 0, 0, 1], v: 1}\n",
		"short position":  "version: 1\nrate: 60\ninterval: 1\nframes:\n  - {p: [0, 0], r: [0, 0, 0, 1]}\n",
		"nan position":    "version: 1\nrate: 60\ninterval: 1\nframes:\n  - {p: [.nan, 0, 0], r: [0, 0, 0, 1]}\n",
		"inf rotation":    "version: 1\nrate: 60\ninterval: 1\nframes:\n  - {p: [0, 0, 0], r: [0, 0, .inf, 1]}\n",
		"zero rotation":   "version: 1\nrate: 60\ninterval: 1\nframes:\n  - {p: [0, 0, 0], r: [0, 0, 0, 0]}\n",
		"trailing doc":    "version: 1\nrate: 60\ninterval: 1\nframes: []\n---\n{}\n",
		"huge interval":   "version: 1\nrate: 60\ninterval: 4611686018427387904\nframes:\n  - {p: [0, 0, 0], r: [0, 0, 0, 1]}\n  - {p: [0, 0, 0], r: [0, 0, 0, 1]}\n  - {p: [0, 0, 0], r: [0, 0, 0, 1]}\n",
		"lap past frames": "version: 1\nrate: 60\ninterval: 2\nlapSteps: 3\nframes:\n  - {p: [0, 0, 0], r: [0, 0, 0, 1]}\n  - {p: [0, 0, 0], r: [0, 0, 0, 1]}\n",
		"lap before last": "version: 1\nrate: 60\ninterval: 2\nlapSteps: 2\nframes:\n  - {p: [0, 0, 0], r: [0, 0, 0, 1]}\n  - {p: [0, 0, 0], r: [0, 0, 0, 1]}\n  - {p: [0, 0, 0], r: [0, 0, 0, 1]}\n",
		"lap no frames":   "version: 1\nrate: 60\ninterval: 1\nlapSteps: 3\nframes: []\n",
	} {
		if _, err := vehicle3d.ParseGhost([]byte(doc)); err == nil {
			t.Errorf("%s: ParseGhost accepted it", name)
		}
	}
	var nilGhost *vehicle3d.Ghost
	if _, err := vehicle3d.EncodeGhost(nilGhost); err == nil {
		t.Error("EncodeGhost accepted nil")
	}
}

func FuzzParseGhost(f *testing.F) {
	f.Add([]byte("version: 1\nrate: 60\ninterval: 1\nframes:\n  - {p: [0, 0.65, 14], r: [0, 0, 0, 1]}\n"))
	f.Add([]byte("version: 1\nrate: 30\ninterval: 4\nlapSteps: 9\nframes:\n  - {p: [0, 0, 0], r: [0, 0, 0, 1]}\n  - {p: [1, 0, 0], r: [0, 0, 0, 1]}\n  - {p: [2, 0, 0], r: [0, 0, 0, 1]}\n  - {p: [3, 0, 0], r: [0, 0, 0, 1]}\n"))
	f.Add([]byte("version: 1\nrate: 10\ninterval: 1\nframes:\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		g, err := vehicle3d.ParseGhost(data)
		if err != nil {
			return
		}
		g.Pose(float64(g.Steps()) / 2)
		out, err := vehicle3d.EncodeGhost(g)
		if err != nil {
			t.Fatalf("parsed ghost does not encode: %v", err)
		}
		again, err := vehicle3d.ParseGhost(out)
		if err != nil {
			t.Fatalf("encoded ghost does not parse: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(again, g) {
			t.Fatalf("round trip changed the ghost:\n%+v\n%+v", again, g)
		}
	})
}

func BenchmarkGhostPose(b *testing.B) {
	r := &vehicle3d.GhostRecorder{Interval: 2}
	for i := range 1000 {
		r.Record(physics3d.Vec3{X: float32(i)}, physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, float32(i)*0.01))
	}
	g := r.Ghost(60, 998)
	b.ReportAllocs()
	step := 0.0
	for b.Loop() {
		g.Pose(step)
		step += 0.37
		if step > 1000 {
			step = 0
		}
	}
}

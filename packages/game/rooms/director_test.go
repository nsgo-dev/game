package rooms_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/rooms"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const step = 10 * time.Millisecond

// twoRooms returns a map of room a at [0, 200) x [0, 100) and room b at [200, 500) x [0, 100), entered with tr, and a 100x100 camera directed through it.
func twoRooms(tr *rooms.Transition) (*rooms.Director, *tilemap.Camera) {
	m := &rooms.Map{Version: 1, Transition: tr, Rooms: []rooms.Room{
		{ID: "a", X: 0, Y: 0, Width: 200, Height: 100},
		{ID: "b", X: 200, Y: 0, Width: 300, Height: 100},
	}}
	if err := rooms.Validate(m); err != nil {
		panic(err)
	}
	cam := &tilemap.Camera{Width: 100, Height: 100}
	return rooms.NewDirector(m, cam), cam
}

func pt(x, y float32) core.Point { return core.Point{X: core.Points(x), Y: core.Points(y)} }

// requireInside fails unless the camera's visible view lies inside r.
func requireInside(t *testing.T, cam *tilemap.Camera, r *rooms.Room) {
	t.Helper()
	v := cam.VisibleBounds()
	const eps = 1e-3
	if float64(v.Origin.X) < float64(r.X)-eps || float64(v.Origin.Y) < float64(r.Y)-eps ||
		float64(v.Origin.X+v.Size.Width) > float64(r.X+r.Width)+eps || float64(v.Origin.Y+v.Size.Height) > float64(r.Y+r.Height)+eps {
		t.Fatalf("view %+v is not inside room %q %+v", v, r.ID, r.Bounds())
	}
}

func requireBounds(t *testing.T, cam *tilemap.Camera, r *rooms.Room) {
	t.Helper()
	if cam.Bounds == nil || *cam.Bounds != r.Bounds() {
		t.Fatalf("camera bounds = %v, want room %q %+v", cam.Bounds, r.ID, r.Bounds())
	}
}

func TestStartClampsToRoomWithoutTransition(t *testing.T) {
	d, cam := twoRooms(&rooms.Transition{Kind: rooms.KindSlide})
	if d.Start(pt(-10, 50)) || d.Room() != nil {
		t.Fatal("Start outside every room found a room")
	}
	if !d.Start(pt(10, 10)) {
		t.Fatal("Start inside room a failed")
	}
	if d.Room().ID != "a" || d.Previous() != nil || d.Transitioning() || d.Progress() != 1 {
		t.Fatalf("after Start: room %v, previous %v, transitioning %v", d.Room(), d.Previous(), d.Transitioning())
	}
	requireBounds(t, cam, d.Room())
	if cam.X != 50 || cam.Y != 50 {
		t.Fatalf("camera = (%v, %v), want clamped (50, 50)", cam.X, cam.Y)
	}
}

func TestUpdateBeforeStartEntersFirstRoom(t *testing.T) {
	d, cam := twoRooms(&rooms.Transition{Kind: rooms.KindSlide})
	if !d.Update(pt(300, 50), step) || d.Room().ID != "b" || d.Transitioning() {
		t.Fatalf("first Update: room %v, transitioning %v", d.Room(), d.Transitioning())
	}
	if cam.X != 300 || cam.Y != 50 {
		t.Fatalf("camera = (%v, %v), want (300, 50)", cam.X, cam.Y)
	}
}

func TestSlideClampsToNewRoomAndPansIntoIt(t *testing.T) {
	d, cam := twoRooms(&rooms.Transition{Kind: rooms.KindSlide, Duration: rooms.Duration(100 * time.Millisecond), Easing: rooms.EasingLinear})
	d.Start(pt(150, 50))
	if cam.X != 150 {
		t.Fatalf("camera X = %v, want 150", cam.X)
	}

	if !d.Update(pt(210, 50), step) {
		t.Fatal("crossing into room b did not report an entry")
	}
	b := d.Map.Room("b")
	if d.Room() != b || d.Previous().ID != "a" || !d.Transitioning() {
		t.Fatalf("room %v, previous %v, transitioning %v", d.Room(), d.Previous(), d.Transitioning())
	}
	if tr := d.Transition(); tr.Kind != rooms.KindSlide {
		t.Fatalf("transition = %+v", tr)
	}
	requireBounds(t, cam, b)

	// Room b's leftmost camera center is 250; the pan runs linearly from 150 to 250 over 10 steps.
	prev := cam.X
	for i := 2; i <= 10; i++ {
		if d.Update(pt(210, 50), step) {
			t.Fatalf("step %d reported a second entry", i)
		}
		want := 150 + 100*float32(i)/10
		if math.Abs(float64(cam.X-want)) > 1e-3 || cam.X <= prev {
			t.Fatalf("step %d: camera X = %v, want %v", i, cam.X, want)
		}
		prev = cam.X
	}
	if d.Transitioning() || d.FadeAlpha() != 0 {
		t.Fatal("slide still playing after its duration")
	}
	if cam.X != 250 || cam.Y != 50 {
		t.Fatalf("camera landed at (%v, %v), want (250, 50)", cam.X, cam.Y)
	}
	requireInside(t, cam, b)

	// After landing the camera follows the target and stays clamped to room b.
	d.Update(pt(480, 50), step)
	if cam.X != 450 {
		t.Fatalf("camera X = %v, want clamped 450", cam.X)
	}
	requireInside(t, cam, b)
}

func TestSlideTracksAMovingTarget(t *testing.T) {
	d, cam := twoRooms(&rooms.Transition{Kind: rooms.KindSlide, Duration: rooms.Duration(40 * time.Millisecond)})
	d.Start(pt(150, 50))
	x := float32(210)
	for d.Update(pt(x, 50), step) || d.Transitioning() {
		x += 30
	}
	if cam.X != x || d.Room().ID != "b" {
		t.Fatalf("camera X = %v, want the target's %v", cam.X, x)
	}
	requireInside(t, cam, d.Room())
}

func TestFadeCoversCutsAndUncovers(t *testing.T) {
	d, cam := twoRooms(&rooms.Transition{Kind: rooms.KindFade, Duration: rooms.Duration(100 * time.Millisecond), Color: "#ff0000"})
	d.Start(pt(150, 50))

	var alphas []float32
	var xs []float32
	entered := d.Update(pt(400, 50), step)
	alphas, xs = append(alphas, d.FadeAlpha()), append(xs, cam.X)
	for d.Transitioning() {
		d.Update(pt(400, 50), step)
		alphas, xs = append(alphas, d.FadeAlpha()), append(xs, cam.X)
	}
	if !entered || len(alphas) != 10 {
		t.Fatalf("entered %v, fade lasted %d steps, want 10", entered, len(alphas))
	}
	requireBounds(t, cam, d.Room())
	wantAlpha := []float32{0.2, 0.4, 0.6, 0.8, 1, 0.8, 0.6, 0.4, 0.2, 0}
	for i := range wantAlpha {
		if math.Abs(float64(alphas[i]-wantAlpha[i])) > 1e-6 {
			t.Fatalf("alpha at step %d = %v, want %v (all %v)", i+1, alphas[i], wantAlpha[i], alphas)
		}
	}
	// The camera holds in room a while the cover rises, then cuts to the target clamped in room b at the midpoint.
	for i, x := range xs {
		want := float32(150)
		if i >= 4 {
			want = 400
		}
		if x != want {
			t.Fatalf("camera X at step %d = %v, want %v (all %v)", i+1, x, want, xs)
		}
	}
	requireInside(t, cam, d.Room())
}

func TestInterruptedFadeKeepsItsCover(t *testing.T) {
	d, cam := twoRooms(&rooms.Transition{Kind: rooms.KindFade, Duration: rooms.Duration(100 * time.Millisecond)})
	d.Start(pt(150, 50))
	d.Update(pt(400, 50), step)
	for range 6 {
		d.Update(pt(400, 50), step)
	}
	if got := d.FadeAlpha(); math.Abs(float64(got-0.6)) > 1e-6 || d.Room().ID != "b" {
		t.Fatalf("alpha = %v in %v, want 0.6 in b", got, d.Room())
	}
	// Going back to room a mid-uncover covers again from the same opacity.
	if !d.Update(pt(100, 50), 0) {
		t.Fatal("going back did not report an entry")
	}
	if got := d.FadeAlpha(); math.Abs(float64(got-0.6)) > 1e-6 {
		t.Fatalf("alpha after re-entry = %v, want 0.6", got)
	}
	steps := 0
	for d.Transitioning() {
		d.Update(pt(100, 50), step)
		steps++
	}
	if steps != 7 || d.Room().ID != "a" || cam.X != 100 {
		t.Fatalf("re-entry fade took %d steps into %v with camera X %v", steps, d.Room(), cam.X)
	}
	requireInside(t, cam, d.Room())
}

func TestCutSnapsAtOnce(t *testing.T) {
	d, cam := twoRooms(nil)
	d.Smoothing = 1
	d.Start(pt(150, 50))
	if !d.Update(pt(210, 50), step) || d.Transitioning() {
		t.Fatal("cut did not enter at once")
	}
	if cam.X != 250 {
		t.Fatalf("camera X = %v, want 250", cam.X)
	}
	requireBounds(t, cam, d.Room())
	if d.Transition().Kind != rooms.KindCut || d.FadeAlpha() != 0 {
		t.Fatalf("transition = %+v", d.Transition())
	}
}

func TestRoomStaysWhileTargetIsInsideOrNowhere(t *testing.T) {
	m := &rooms.Map{Version: 1, Rooms: []rooms.Room{
		{ID: "a", X: 0, Y: 0, Width: 200, Height: 100},
		{ID: "b", X: 150, Y: 0, Width: 200, Height: 100},
	}}
	cam := &tilemap.Camera{Width: 100, Height: 100}
	d := rooms.NewDirector(m, cam)
	d.Start(pt(300, 50))
	// The overlap belongs to a in list order, but b keeps the target while it is still inside b.
	if d.Update(pt(160, 50), step) || d.Room().ID != "b" {
		t.Fatalf("room = %v, want b kept", d.Room())
	}
	if !d.Update(pt(100, 50), step) || d.Room().ID != "a" {
		t.Fatalf("room = %v, want a", d.Room())
	}
	// Outside every room the camera keeps to a.
	if d.Update(pt(-50, 50), step) || d.Room().ID != "a" {
		t.Fatalf("room = %v, want a kept", d.Room())
	}
	if cam.X != 50 {
		t.Fatalf("camera X = %v, want clamped 50", cam.X)
	}
}

func TestDirectorIsDeterministic(t *testing.T) {
	run := func() []float32 {
		d, cam := twoRooms(&rooms.Transition{Kind: rooms.KindSlide, Duration: rooms.Duration(70 * time.Millisecond)})
		d.Smoothing = 8
		var out []float32
		x := float32(20)
		for i := range 60 {
			if i%20 < 10 {
				x += 23
			} else {
				x -= 11
			}
			d.Update(pt(x, 50), step)
			out = append(out, cam.X, cam.Y, d.FadeAlpha())
		}
		return out
	}
	a, b := run(), run()
	for i := range a {
		if math.Float32bits(a[i]) != math.Float32bits(b[i]) {
			t.Fatalf("run differs at %d: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestNilCameraIsIgnored(t *testing.T) {
	d := rooms.NewDirector(&rooms.Map{Version: 1, Rooms: []rooms.Room{{ID: "a", Width: 1, Height: 1}}}, nil)
	if d.Start(pt(0, 0)) || d.Update(pt(0, 0), step) {
		t.Fatal("a Director without a camera entered a room")
	}
	d.DrawFade(nil)
}

func TestDrawFadePaintsTheCover(t *testing.T) {
	d, _ := twoRooms(&rooms.Transition{Kind: rooms.KindFade, Duration: rooms.Duration(100 * time.Millisecond), Color: "#336699"})
	d.Start(pt(150, 50))
	d.Update(pt(400, 50), 30*time.Millisecond)

	var tick *core.Signal[int]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		tick = core.NewSignal(cx, 0)
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "fade", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(dr *core.Drawing, _ core.Size) {
				_ = tick.Get()
				d.DrawFade(dr)
			}},
		}}
	})
	ops := app.Find("fade").Ops
	var alpha float32
	var color core.Color
	fills := 0
	for _, op := range ops {
		switch op.Kind {
		case core.DrawAlpha:
			alpha = op.A
		case core.DrawFillColor:
			color = op.Color
		case core.DrawFill:
			fills++
		}
	}
	if math.Abs(float64(alpha-0.6)) > 1e-6 || color != core.RGB(0x336699) || fills != 1 {
		t.Fatalf("fade ops: alpha %v, color %v, fills %d (%+v)", alpha, color, fills, ops)
	}

	// Once the fade ends nothing is drawn.
	app.Post(func() {
		for d.Transitioning() {
			d.Update(pt(400, 50), step)
		}
		tick.Set(1)
	})
	app.Settle()
	for _, op := range app.Find("fade").Ops {
		if op.Kind == core.DrawFill || op.Kind == core.DrawAlpha {
			t.Fatalf("finished fade still drew %+v", op)
		}
	}
}

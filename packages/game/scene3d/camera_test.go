package scene3d_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
)

func TestCameraEye(t *testing.T) {
	persp := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 1, Y: 2, Z: 3}, math3d.DegToRad(60), 1, 0.1, 100)
	if e := persp.Eye(); e != (math3d.Vec4{X: 1, Y: 2, Z: 3, W: 1}) {
		t.Fatalf("perspective Eye = %v, want (1, 2, 3, 1)", e)
	}

	ortho := scene3d.NewOrthographicCamera(math3d.Vec3{X: 4, Y: 4}, 10, 1, 0.1, 100)
	ortho.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	e := ortho.Eye()
	if e.W != 0 {
		t.Fatalf("orthographic Eye.W = %v, want 0", e.W)
	}
	// The direction toward the viewer is the same from every world point.
	want := ortho.Forward().Neg()
	if !(math3d.Vec3{X: e.X, Y: e.Y, Z: e.Z}).ApproxEqual(want, eps) {
		t.Fatalf("orthographic Eye = %v, want (%v, 0)", e, want)
	}
}

func TestCameraSetViewport(t *testing.T) {
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(60), 1, 0.1, 100)
	cam.SetViewport(1920, 1080)
	if !math3d.ApproxEqual(cam.Projection.Aspect, 16.0/9.0, eps) {
		t.Fatalf("Aspect = %v, want 16/9", cam.Projection.Aspect)
	}
	for _, size := range [][2]float32{{0, 10}, {10, 0}, {-1, 5}} {
		cam.SetViewport(size[0], size[1])
		if !math3d.ApproxEqual(cam.Projection.Aspect, 16.0/9.0, eps) {
			t.Fatalf("SetViewport(%v, %v) changed Aspect to %v", size[0], size[1], cam.Projection.Aspect)
		}
	}
}

func TestCameraLerp(t *testing.T) {
	a := scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(40), 1, 0.1, 100)
	b := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 10}, math3d.DegToRad(80), 2, 0.5, 200)
	b.Rot = math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(90))

	if got := a.Lerp(b, 0); !got.Pos.ApproxEqual(a.Pos, eps) || !got.Rot.ApproxEqual(a.Rot, eps) || got.Projection != a.Projection {
		t.Fatalf("Lerp(0) = %+v, want a", got)
	}
	if got := a.Lerp(b, 1); !got.Pos.ApproxEqual(b.Pos, eps) || !got.Rot.ApproxEqual(b.Rot, eps) || !projApprox(got.Projection, b.Projection) {
		t.Fatalf("Lerp(1) = %+v, want b", got)
	}
	mid := a.Lerp(b, 0.5)
	if !mid.Pos.ApproxEqual(math3d.Vec3{X: 5}, eps) {
		t.Fatalf("Lerp(0.5).Pos = %v, want (5, 0, 0)", mid.Pos)
	}
	if want := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(45)); !mid.Rot.ApproxEqual(want, eps) {
		t.Fatalf("Lerp(0.5).Rot = %v, want 45 degrees about +Y", mid.Rot)
	}
	p := mid.Projection
	if !math3d.ApproxEqual(p.FOVY, math3d.DegToRad(60), eps) || !math3d.ApproxEqual(p.Aspect, 1.5, eps) ||
		!math3d.ApproxEqual(p.Near, 0.3, eps) || !math3d.ApproxEqual(p.Far, 150, eps) {
		t.Fatalf("Lerp(0.5).Projection = %+v", p)
	}

	// Projections of different kinds do not blend: the first holds until t reaches 1.
	o := scene3d.NewOrthographicCamera(math3d.Vec3Zero(), 10, 1, 0.1, 100)
	if got := a.Lerp(o, 0.99).Projection; got != a.Projection {
		t.Fatalf("mixed-kind Lerp(0.99).Projection = %+v, want a's", got)
	}
	if got := a.Lerp(o, 1).Projection; got != o.Projection {
		t.Fatalf("mixed-kind Lerp(1).Projection = %+v, want o's", got)
	}
}

func projApprox(a, b scene3d.Projection) bool {
	return a.Kind == b.Kind && math3d.ApproxEqual(a.FOVY, b.FOVY, eps) && math3d.ApproxEqual(a.OrthoHeight, b.OrthoHeight, eps) &&
		math3d.ApproxEqual(a.Aspect, b.Aspect, eps) && math3d.ApproxEqual(a.Near, b.Near, eps) && math3d.ApproxEqual(a.Far, b.Far, eps)
}

// TestCameraInterpolatedOnGameLoop keeps the camera from the previous update and draws prev.Lerp(cam, alpha), so every drawn position lies on the path between the last two fixed-step poses and the drawn camera never runs ahead of the simulation.
func TestCameraInterpolatedOnGameLoop(t *testing.T) {
	cam := scene3d.NewOrthographicCamera(math3d.Vec3Zero(), 10, 1, 0.1, 100)
	prev := *cam
	const speed = 6 // world units a second along +X
	var (
		draws   int
		lastX   float32
		updates int
	)
	loop := &game.Loop{
		Rate: 30,
		Update: func(dt time.Duration) {
			prev = *cam
			cam.Pos.X += speed * float32(dt.Seconds())
			updates++
		},
		Draw: func(alpha float64) {
			drawn := prev.Lerp(cam, float32(alpha))
			if drawn.Pos.X < prev.Pos.X-eps || drawn.Pos.X > cam.Pos.X+eps {
				t.Errorf("drawn X %v outside [%v, %v]", drawn.Pos.X, prev.Pos.X, cam.Pos.X)
			}
			if drawn.Pos.X < lastX-eps {
				t.Errorf("drawn X went back from %v to %v", lastX, drawn.Pos.X)
			}
			want := math3d.Lerp(prev.Pos.X, cam.Pos.X, float32(alpha))
			if !math3d.ApproxEqual(drawn.Pos.X, want, eps) {
				t.Errorf("drawn X %v, want %v at alpha %v", drawn.Pos.X, want, alpha)
			}
			lastX = drawn.Pos.X
			draws++
		},
	}
	app := startLoop(t, loop)
	app.Advance(time.Second)
	if updates == 0 || draws <= updates/2 {
		t.Fatalf("updates=%d draws=%d, want both to run", updates, draws)
	}
}

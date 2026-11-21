package scene3d_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const eps = 1e-4

func unitBox() math3d.AABB {
	return math3d.AABB{
		Min: math3d.Vec3{X: -0.5, Y: -0.5, Z: -0.5},
		Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5},
	}
}

func startLoop(t *testing.T, l *game.Loop) *nsgotest.App {
	t.Helper()
	return nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})
}

func TestNodeHierarchyTransformsAndDirtyPropagation(t *testing.T) {
	parent := scene3d.NewNode("parent")
	child := scene3d.NewNode("child")
	grand := scene3d.NewNode("grand")

	if err := parent.AddChild(child); err != nil {
		t.Fatalf("AddChild(child): %v", err)
	}
	if err := child.AddChild(grand); err != nil {
		t.Fatalf("AddChild(grand): %v", err)
	}

	parent.SetPosition(math3d.Vec3{X: 10, Y: 0, Z: 0})
	parent.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(90)))
	child.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: -2})
	grand.SetPosition(math3d.Vec3{X: 0, Y: 3, Z: 0})
	grand.SetLocalBounds(unitBox())

	// Initially all nodes are dirty.
	if !parent.IsDirty() || !child.IsDirty() || !grand.IsDirty() {
		t.Fatalf("expected all nodes dirty before evaluation")
	}

	// Rotating (0, 0, -2) by +90° around +Y yields (-2, 0, 0), plus parent pos (10, 0, 0) => (8, 0, 0).
	// Grandchild adds +3 Y => (8, 3, 0).
	wt := grand.WorldTransform()
	wantPos := math3d.Vec3{X: 8, Y: 3, Z: 0}
	if !wt.Pos.ApproxEqual(wantPos, eps) {
		t.Fatalf("grand.WorldTransform().Pos = %v, want %v", wt.Pos, wantPos)
	}
	wm := grand.WorldMatrix()
	if got := wm.TransformPoint(math3d.Vec3Zero()); !got.ApproxEqual(wantPos, eps) {
		t.Fatalf("grand.WorldMatrix origin = %v, want %v", got, wantPos)
	}

	// Evaluating grand should have cleaned the entire ancestor chain.
	if parent.IsDirty() || child.IsDirty() || grand.IsDirty() {
		t.Fatalf("expected all nodes clean after WorldTransform/WorldMatrix")
	}

	// WorldBounds should reflect the world transform.
	wb := grand.WorldBounds()
	wantMin := math3d.Vec3{X: 7.5, Y: 2.5, Z: -0.5}
	wantMax := math3d.Vec3{X: 8.5, Y: 3.5, Z: 0.5}
	if !wb.Min.ApproxEqual(wantMin, eps) || !wb.Max.ApproxEqual(wantMax, eps) {
		t.Fatalf("grand.WorldBounds = [%v, %v], want [%v, %v]", wb.Min, wb.Max, wantMin, wantMax)
	}

	// Mutating parent must propagate dirty flag down to child and grand.
	parent.SetPosition(math3d.Vec3{X: 20, Y: 0, Z: 0})
	if !parent.IsDirty() || !child.IsDirty() || !grand.IsDirty() {
		t.Fatalf("expected dirty propagation to descendants after parent.SetPosition")
	}
	if got := grand.WorldTransform().Pos; !got.ApproxEqual(math3d.Vec3{X: 18, Y: 3, Z: 0}, eps) {
		t.Fatalf("grand pos after parent move = %v, want (18, 3, 0)", got)
	}

	// Direct mutation of Local fields without calling MarkDirty must also be detected.
	parent.Local.Pos.Y = 5
	if got := grand.WorldTransform().Pos; !got.ApproxEqual(math3d.Vec3{X: 18, Y: 8, Z: 0}, eps) {
		t.Fatalf("grand pos after direct parent.Local mutation = %v, want (18, 8, 0)", got)
	}

	// Non-uniform parent scale + rotated child: WorldMatrix composes exact matrices.
	nonUniformParent := scene3d.NewNode("nuParent")
	nonUniformParent.SetScale(math3d.Vec3{X: 2, Y: 1, Z: 1})
	rotChild := scene3d.NewNode("rotChild")
	rotChild.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3UnitZ(), math3d.DegToRad(90)))
	_ = nonUniformParent.AddChild(rotChild)
	wantMat := nonUniformParent.Local.ToMat4().Mul(rotChild.Local.ToMat4())
	if !rotChild.WorldMatrix().ApproxEqual(wantMat, eps) {
		t.Fatalf("WorldMatrix under non-uniform scale = %v, want %v", rotChild.WorldMatrix(), wantMat)
	}
}

func TestNodeCycleRejectionAndReparenting(t *testing.T) {
	a := scene3d.NewNode("a")
	b := scene3d.NewNode("b")
	c := scene3d.NewNode("c")

	if err := a.AddChild(nil); !errors.Is(err, scene3d.ErrNilNode) {
		t.Fatalf("AddChild(nil) = %v, want ErrNilNode", err)
	}
	if err := a.AddChild(a); !errors.Is(err, scene3d.ErrCycle) {
		t.Fatalf("AddChild(self) = %v, want ErrCycle", err)
	}
	_ = a.AddChild(b)
	_ = b.AddChild(c)

	if err := b.AddChild(a); !errors.Is(err, scene3d.ErrCycle) {
		t.Fatalf("AddChild(parent) = %v, want ErrCycle", err)
	}
	if err := c.AddChild(a); !errors.Is(err, scene3d.ErrCycle) {
		t.Fatalf("AddChild(grandparent) = %v, want ErrCycle", err)
	}
	if err := a.Reparent(c); !errors.Is(err, scene3d.ErrCycle) {
		t.Fatalf("Reparent(descendant) = %v, want ErrCycle", err)
	}
	if err := a.ReparentKeepWorld(c); !errors.Is(err, scene3d.ErrCycle) {
		t.Fatalf("ReparentKeepWorld(descendant) = %v, want ErrCycle", err)
	}
	if err := c.Attach("socket", a); !errors.Is(err, scene3d.ErrCycle) {
		t.Fatalf("Attach(ancestor) = %v, want ErrCycle", err)
	}
	// Failed Attach must not leave an orphan implicit socket node on c.
	if len(c.Children()) != 0 {
		t.Fatalf("failed Attach created unexpected children: %v", c.Children())
	}

	// Test ReparentKeepWorld preserves world position & rotation.
	a.SetPosition(math3d.Vec3{X: 10, Y: 0, Z: 0})
	b.SetPosition(math3d.Vec3{X: 0, Y: 4, Z: 0})
	c.SetPosition(math3d.Vec3{X: 2, Y: 1, Z: -3})
	beforeWorld := c.WorldTransform()

	newParent := scene3d.NewNode("newParent")
	newParent.SetPosition(math3d.Vec3{X: -5, Y: 2, Z: 1})
	newParent.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(45)))

	if err := c.ReparentKeepWorld(newParent); err != nil {
		t.Fatalf("ReparentKeepWorld: %v", err)
	}
	if c.Parent() != newParent {
		t.Fatalf("c.Parent() = %v, want newParent", c.Parent())
	}
	if !c.WorldTransform().ApproxEqual(beforeWorld, eps) {
		t.Fatalf("world transform changed after ReparentKeepWorld: got %+v, want %+v", c.WorldTransform(), beforeWorld)
	}

	// Test FindByName, Tags, Walk, and RemoveChild.
	c.AddTag("weapon")
	c.AddTag("weapon") // idempotent
	if !c.HasTag("weapon") || len(c.Tags) != 1 {
		t.Fatalf("unexpected Tags: %v", c.Tags)
	}
	if found := newParent.FindByName("c"); found != c {
		t.Fatalf("FindByName(c) = %v, want c", found)
	}
	if tagged := newParent.FindByTag("weapon"); len(tagged) != 1 || tagged[0] != c {
		t.Fatalf("FindByTag(weapon) = %v, want [c]", tagged)
	}
	var visited []string
	newParent.Walk(func(n *scene3d.Node) bool {
		visited = append(visited, n.Name)
		return n.Name != "newParent" // stop after root
	})
	if len(visited) != 1 || visited[0] != "newParent" {
		t.Fatalf("Walk early stop visited %v, want [newParent]", visited)
	}

	if !newParent.RemoveChild(c) || c.Parent() != nil {
		t.Fatalf("RemoveChild failed")
	}
}

func TestSocketsAndAttachments(t *testing.T) {
	hero := scene3d.NewNode("hero")
	handOffset := math3d.Transform{
		Pos:   math3d.Vec3{X: 1, Y: 1.2, Z: -0.5},
		Rot:   math3d.QuatIdentity(),
		Scale: math3d.Vec3One(),
	}
	hero.DefineSocket("right_hand", handOffset)

	sword := scene3d.NewNode("sword")
	sword.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: -0.5})
	if err := hero.Attach("right_hand", sword); err != nil {
		t.Fatalf("Attach sword: %v", err)
	}

	// Animate hero: translate and rotate 90° around Y.
	hero.SetPosition(math3d.Vec3{X: 5, Y: 0, Z: 2})
	hero.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(90)))

	// In hero space, sword is at (1, 1.2, -1). Rotated +90° around Y => (-1, 1.2, -1).
	// Plus hero pos (5, 0, 2) => (4, 1.2, 1).
	wantSwordPos := math3d.Vec3{X: 4, Y: 1.2, Z: 1}
	if got := sword.WorldTransform().Pos; !got.ApproxEqual(wantSwordPos, eps) {
		t.Fatalf("attached sword world pos = %v, want %v", got, wantSwordPos)
	}
	if attached := hero.Attached("right_hand"); len(attached) != 1 || attached[0] != sword {
		t.Fatalf("Attached(right_hand) = %v, want [sword]", attached)
	}

	// Attaching to an existing nested bone node by name.
	torso := scene3d.NewNode("torso")
	head := scene3d.NewNode("head")
	head.SetPosition(math3d.Vec3{X: 0, Y: 2, Z: 0})
	_ = hero.AddChild(torso)
	_ = torso.AddChild(head)

	helmet := scene3d.NewNode("helmet")
	if err := hero.Attach("head", helmet); err != nil {
		t.Fatalf("Attach helmet to head bone: %v", err)
	}
	if helmet.Parent() != head {
		t.Fatalf("helmet parent = %v, want head", helmet.Parent())
	}
	if got := helmet.WorldTransform().Pos; !got.ApproxEqual(math3d.Vec3{X: 5, Y: 2, Z: 2}, eps) {
		t.Fatalf("helmet world pos = %v, want (5, 2, 2)", got)
	}

	// Typed data attachments.
	type lightData struct{ Intensity float32 }
	sword.SetAttachment("light", lightData{Intensity: 4.5})
	if v, ok := scene3d.AttachmentAs[lightData](sword, "light"); !ok || v.Intensity != 4.5 {
		t.Fatalf("AttachmentAs[lightData] = (%+v, %v), want (4.5, true)", v, ok)
	}
	if _, ok := scene3d.AttachmentAs[string](sword, "light"); ok {
		t.Fatalf("AttachmentAs[string] unexpectedly succeeded")
	}
	sword.RemoveAttachment("light")
	if _, ok := sword.Attachment("light"); ok {
		t.Fatalf("Attachment still present after RemoveAttachment")
	}
}

func TestSceneSpatialQueries(t *testing.T) {
	sc := scene3d.NewScene()

	inFront := scene3d.NewNode("inFront")
	inFront.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: -5})
	inFront.SetLocalBounds(unitBox())

	further := scene3d.NewNode("further")
	further.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: -10})
	further.SetLocalBounds(unitBox())
	further.Layer = 1 << 1

	behind := scene3d.NewNode("behind")
	behind.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: 5})
	behind.SetLocalBounds(unitBox())

	hiddenParent := scene3d.NewNode("hiddenParent")
	hiddenParent.Visible = false
	hiddenChild := scene3d.NewNode("hiddenChild")
	hiddenChild.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: -3})
	hiddenChild.SetLocalBounds(unitBox())
	_ = hiddenParent.AddChild(hiddenChild)

	_ = sc.Add(inFront)
	_ = sc.Add(further)
	_ = sc.Add(behind)
	_ = sc.Add(hiddenParent)

	if hiddenChild.EffectiveVisible() {
		t.Fatalf("hiddenChild should not be effectively visible")
	}

	// Frustum query from origin looking down -Z.
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(60), 16.0/9.0, 0.1, 50)
	frustumNodes := sc.QueryFrustum(cam.ViewProj())
	if len(frustumNodes) != 2 {
		t.Fatalf("QueryFrustum returned %d nodes, want 2 (inFront, further)", len(frustumNodes))
	}

	// AABB query around z = -5.
	aabbNodes := sc.QueryAABB(math3d.AABB{
		Min: math3d.Vec3{X: -1, Y: -1, Z: -6},
		Max: math3d.Vec3{X: 1, Y: 1, Z: -4},
	})
	if len(aabbNodes) != 1 || aabbNodes[0] != inFront {
		t.Fatalf("QueryAABB = %v, want [inFront]", aabbNodes)
	}

	// Sphere query covering origin to z = 5.
	sphereNodes := sc.QuerySphere(math3d.Sphere{Center: math3d.Vec3{X: 0, Y: 0, Z: 4}, Radius: 1.0})
	if len(sphereNodes) != 1 || sphereNodes[0] != behind {
		t.Fatalf("QuerySphere = %v, want [behind]", sphereNodes)
	}

	// Raycast down -Z hits inFront then further in ascending T order, skipping hiddenChild.
	ray := math3d.Ray{Origin: math3d.Vec3Zero(), Dir: math3d.Vec3Forward()}
	hits := sc.Raycast(ray, scene3d.LayerAll)
	if len(hits) != 2 {
		t.Fatalf("Raycast hits = %d, want 2", len(hits))
	}
	if hits[0].Node != inFront || !math3d.ApproxEqual(hits[0].T, 4.5, eps) {
		t.Fatalf("hits[0] = (%v, T=%v), want (inFront, 4.5)", hits[0].Node.Name, hits[0].T)
	}
	if !hits[0].Normal.ApproxEqual(math3d.Vec3{X: 0, Y: 0, Z: 1}, eps) {
		t.Fatalf("hits[0].Normal = %v, want (0, 0, 1)", hits[0].Normal)
	}
	if hits[1].Node != further || !math3d.ApproxEqual(hits[1].T, 9.5, eps) {
		t.Fatalf("hits[1] = (%v, T=%v), want (further, 9.5)", hits[1].Node.Name, hits[1].T)
	}

	// Layer mask filtering for layer 1<<1 only hits further.
	first, ok := sc.RaycastFirst(ray, 1<<1)
	if !ok || first.Node != further {
		t.Fatalf("RaycastFirst(1<<1) = (%v, %v), want (further, true)", first.Node, ok)
	}
	if _, ok := sc.RaycastFirst(ray, 0); ok {
		t.Fatalf("RaycastFirst(0) should miss")
	}
}

func TestCameraMatricesAndScreenRayRoundTrip(t *testing.T) {
	// Perspective camera LookAt matches math3d.LookAt.
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 3, Y: 4, Z: 10}, math3d.DegToRad(60), 16.0/9.0, 0.5, 100)
	cam.LookAt(math3d.Vec3{X: 0, Y: 1, Z: 0}, math3d.Vec3Up())
	wantView := math3d.LookAt(cam.Pos, math3d.Vec3{X: 0, Y: 1, Z: 0}, math3d.Vec3Up())
	if !cam.ViewMatrix().ApproxEqual(wantView, eps) {
		t.Fatalf("ViewMatrix mismatch:\ngot  %v\nwant %v", cam.ViewMatrix(), wantView)
	}

	// WorldToScreen -> ScreenRay round-trip passes directly through the world point.
	const viewW, viewH = 1280, 720
	samplePoints := []math3d.Vec3{
		{X: 0, Y: 1, Z: 0},
		{X: -2, Y: 2.5, Z: -1},
		{X: 1.5, Y: -0.5, Z: 2},
	}
	for _, pt := range samplePoints {
		scr, depth, vis := cam.WorldToScreen(pt, viewW, viewH)
		if !vis || depth < 0 || depth > 1 {
			t.Fatalf("WorldToScreen(%v) = (%v, depth=%v, vis=%v), want visible", pt, scr, depth, vis)
		}
		ray := cam.ScreenRay(scr.X, scr.Y, viewW, viewH)
		tParam := pt.Sub(ray.Origin).Dot(ray.Dir)
		closest := ray.At(tParam)
		if !closest.ApproxEqual(pt, 1e-3) {
			t.Fatalf("perspective ScreenRay round-trip for %v: closest=%v (dist=%v)", pt, closest, closest.Dist(pt))
		}
	}

	// Orthographic camera: rays across different pixels are parallel and round-trip.
	ortho := scene3d.NewOrthographicCamera(math3d.Vec3{X: 0, Y: 0, Z: 10}, 10, 2.0, 0.5, 50)
	rayCenter := ortho.ScreenRay(viewW/2, viewH/2, viewW, viewH)
	rayCorner := ortho.ScreenRay(viewW*0.75, viewH*0.25, viewW, viewH)
	if !rayCenter.Dir.ApproxEqual(math3d.Vec3Forward(), eps) {
		t.Fatalf("ortho center ray dir = %v, want (0, 0, -1)", rayCenter.Dir)
	}
	if !rayCorner.Dir.ApproxEqual(math3d.Vec3Forward(), eps) {
		t.Fatalf("ortho corner ray dir = %v, want (0, 0, -1)", rayCorner.Dir)
	}
	// With orthoHeight=10, aspect=2 => width=20, x in [-10, 10], y in [-5, 5].
	// Screen (75%, 25%) => ndcX=+0.5 => x=+5, ndcY=+0.5 => y=+2.5.
	if !math3d.ApproxEqual(rayCorner.Origin.X, 5, eps) || !math3d.ApproxEqual(rayCorner.Origin.Y, 2.5, eps) {
		t.Fatalf("ortho corner ray origin = %v, want (5, 2.5, 9.5)", rayCorner.Origin)
	}
}

func TestOrbitControllerOnGameLoop(t *testing.T) {
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(60), 16.0/9.0, 0.1, 100)
	orbit := scene3d.NewOrbitController(math3d.Vec3Zero(), 10)
	orbit.MinDistance = 2
	orbit.MaxDistance = 20

	// Pitch clamping [-89°, +89°] and zoom clamping [MinDistance, MaxDistance].
	orbit.Orbit(0, math3d.DegToRad(200))
	if !math3d.ApproxEqual(orbit.Pitch, math3d.DegToRad(89), eps) {
		t.Fatalf("clamped max pitch = %v deg, want 89", math3d.RadToDeg(orbit.Pitch))
	}
	orbit.Orbit(0, -math3d.DegToRad(300))
	if !math3d.ApproxEqual(orbit.Pitch, -math3d.DegToRad(89), eps) {
		t.Fatalf("clamped min pitch = %v deg, want -89", math3d.RadToDeg(orbit.Pitch))
	}
	orbit.Zoom(-100)
	if orbit.Distance != 2 {
		t.Fatalf("clamped min distance = %v, want 2", orbit.Distance)
	}
	orbit.Zoom(100)
	if orbit.Distance != 20 {
		t.Fatalf("clamped max distance = %v, want 20", orbit.Distance)
	}

	// Panning in camera right/up plane at Yaw = 90° (camera at +X looking down -X: right = -Z, up = +Y).
	orbit.SetAngles(math3d.DegToRad(90), 0)
	orbit.SetDistance(10)
	orbit.SetTarget(math3d.Vec3Zero())
	orbit.Pan(3, 2)
	if !orbit.Target.ApproxEqual(math3d.Vec3{X: 0, Y: 2, Z: -3}, eps) {
		t.Fatalf("panned target at yaw=90° = %v, want (0, 2, -3)", orbit.Target)
	}

	// Damped orbit convergence stepped deterministically on game.Loop.
	orbit.SetTarget(math3d.Vec3Zero())
	orbit.SetAngles(0, 0)
	orbit.SetDistance(10)
	orbit.Snap()
	orbit.Damping = 10

	orbit.Orbit(math3d.DegToRad(90), math3d.DegToRad(30))
	orbit.Zoom(-4) // desired distance = 6

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			orbit.Update(cam, dt)
		},
	}
	app := startLoop(t, loop)

	// After 1 frame (~16.7ms), camera should have moved partially toward desired state.
	app.Advance(17 * time.Millisecond)
	if orbit.Yaw <= 0 || orbit.Yaw >= math3d.DegToRad(90) {
		t.Fatalf("expected partial damped yaw after 1 frame, got %v deg", math3d.RadToDeg(orbit.Yaw))
	}

	// After 2 seconds at Damping=10, camera converges cleanly to target pose.
	app.Advance(2 * time.Second)
	if !math3d.ApproxEqual(orbit.Yaw, math3d.DegToRad(90), 1e-3) ||
		!math3d.ApproxEqual(orbit.Pitch, math3d.DegToRad(30), 1e-3) ||
		!math3d.ApproxEqual(orbit.Distance, 6, 1e-3) {
		t.Fatalf("orbit did not converge: yaw=%v pitch=%v dist=%v",
			math3d.RadToDeg(orbit.Yaw), math3d.RadToDeg(orbit.Pitch), orbit.Distance)
	}
	if !math3d.ApproxEqual(cam.Pos.Dist(orbit.Target), 6, 1e-3) {
		t.Fatalf("camera distance from target = %v, want 6", cam.Pos.Dist(orbit.Target))
	}
	// Camera forward should point directly at orbit.Target.
	wantFwd := orbit.Target.Sub(cam.Pos).Norm()
	if !cam.Forward().ApproxEqual(wantFwd, 1e-3) {
		t.Fatalf("camera forward = %v, want %v", cam.Forward(), wantFwd)
	}
}

func TestFPSControllerOnGameLoop(t *testing.T) {
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(70), 16.0/9.0, 0.1, 100)
	fps := scene3d.NewFPSController(math3d.Vec3Zero())
	fps.MoveSpeed = 6

	// Pitch clamp check.
	fps.Look(0, math3d.DegToRad(150))
	if !math3d.ApproxEqual(fps.Pitch, math3d.DegToRad(89), eps) {
		t.Fatalf("fps clamped max pitch = %v deg, want 89", math3d.RadToDeg(fps.Pitch))
	}
	fps.Look(0, -math3d.DegToRad(300))
	if !math3d.ApproxEqual(fps.Pitch, -math3d.DegToRad(89), eps) {
		t.Fatalf("fps clamped min pitch = %v deg, want -89", math3d.RadToDeg(fps.Pitch))
	}

	// Turn +90° yaw (facing -X, right = -Z, up = +Y) and pitch 0.
	fps.Yaw = math3d.DegToRad(90)
	fps.Pitch = 0
	fps.Pos = math3d.Vec3Zero()

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			fps.Move(1, 0.5, 0.25, dt)
			fps.Apply(cam)
		},
	}
	app := startLoop(t, loop)
	app.Advance(time.Second)

	// After 1s at speed 6: forward (-X) = -6, right (-Z) = -3, up (+Y) = +1.5.
	wantPos := math3d.Vec3{X: -6, Y: 1.5, Z: -3}
	if !cam.Pos.ApproxEqual(wantPos, 1e-2) {
		t.Fatalf("fps cam.Pos = %v, want %v", cam.Pos, wantPos)
	}
	if !cam.Forward().ApproxEqual(math3d.Vec3{X: -1, Y: 0, Z: 0}, eps) {
		t.Fatalf("fps cam.Forward = %v, want (-1, 0, 0)", cam.Forward())
	}

	// Grounded mode keeps forward movement horizontal even when pitched upward.
	fps.Pos = math3d.Vec3Zero()
	fps.Yaw = 0
	fps.Pitch = math3d.DegToRad(45)
	fps.Grounded = true
	fps.Move(1, 0, 0, time.Second)
	if !fps.Pos.ApproxEqual(math3d.Vec3{X: 0, Y: 0, Z: -6}, 1e-3) {
		t.Fatalf("grounded fps move = %v, want (0, 0, -6)", fps.Pos)
	}
}

func TestThirdPersonControllerSmoothingAndWallObstruction(t *testing.T) {
	sc := scene3d.NewScene()
	const wallLayer uint32 = 1 << 2
	const playerLayer uint32 = 1 << 3

	player := scene3d.NewNode("player")
	player.Layer = playerLayer
	player.SetLocalBounds(unitBox())
	_ = sc.Add(player)

	wall := scene3d.NewNode("wall")
	wall.Layer = wallLayer
	wall.Visible = false // initially disabled while testing open-space smooth follow
	wall.SetPosition(math3d.Vec3{X: 10, Y: 1, Z: 2.5})
	wall.SetLocalBounds(math3d.AABB{
		Min: math3d.Vec3{X: -5, Y: -5, Z: -0.25},
		Max: math3d.Vec3{X: 5, Y: 5, Z: 0.25},
	})
	_ = sc.Add(wall)

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(60), 16.0/9.0, 0.1, 100)
	tp := scene3d.NewThirdPersonController()
	tp.Offset = math3d.Vec3{X: 0, Y: 1, Z: 0}
	tp.Distance = 5
	tp.Height = 2
	tp.LookAhead = 3
	tp.PositionSmoothing = 10
	tp.LookSmoothing = 12
	tp.ObstructionPadding = 0.2
	tp.ObstructionCheck = scene3d.SceneObstructionCheck(sc, wallLayer|playerLayer, player)

	// Start snapped behind player at origin facing -Z.
	targetPos := math3d.Vec3Zero()
	targetFwd := math3d.Vec3Forward()
	tp.Snap(targetPos, targetFwd)
	tp.Apply(cam)

	// Pivot = (0, 1, 0), desiredPos = (0, 3, 5), desiredLook = (0, 1, -3).
	if !cam.Pos.ApproxEqual(math3d.Vec3{X: 0, Y: 3, Z: 5}, eps) {
		t.Fatalf("snapped cam.Pos = %v, want (0, 3, 5)", cam.Pos)
	}
	if !tp.LookTarget.ApproxEqual(math3d.Vec3{X: 0, Y: 1, Z: -3}, eps) {
		t.Fatalf("snapped LookTarget = %v, want (0, 1, -3)", tp.LookTarget)
	}

	// Move player to (10, 0, 0) and step on game.Loop: verify smooth convergence.
	targetPos = math3d.Vec3{X: 10, Y: 0, Z: 0}
	player.SetPosition(targetPos)

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			tp.Update(cam, targetPos, targetFwd, dt)
		},
	}
	app := startLoop(t, loop)

	// After 1 frame, camera has moved smoothly partway (not snapped instantaneously).
	app.Advance(17 * time.Millisecond)
	if cam.Pos.X <= 0.1 || cam.Pos.X >= 9.9 {
		t.Fatalf("expected partial smooth convergence after 1 frame, got cam.Pos.X = %v", cam.Pos.X)
	}

	// After 2 seconds, camera converges to (10, 3, 5).
	app.Advance(2 * time.Second)
	if !cam.Pos.ApproxEqual(math3d.Vec3{X: 10, Y: 3, Z: 5}, 1e-3) {
		t.Fatalf("converged cam.Pos = %v, want (10, 3, 5)", cam.Pos)
	}

	// Enable wall between player pivot (10, 1, 0) and camera (10, 3, 5).
	// Wall front face lies at z = 2.25; camera must pull cleanly in front of z = 2.25.
	wall.Visible = true
	app.Advance(17 * time.Millisecond)
	if cam.Pos.Z >= 2.25 {
		t.Fatalf("camera failed to pull in front of wall: cam.Pos = %v, wall front Z = 2.25", cam.Pos)
	}
	if cam.Pos.Z <= 0.5 {
		t.Fatalf("camera pulled too far inside player: cam.Pos = %v", cam.Pos)
	}

	// Remove the wall and confirm the camera smoothly eases back out to Z = 5.
	wall.Visible = false
	app.Advance(17 * time.Millisecond)
	if cam.Pos.Z >= 4.5 {
		t.Fatalf("expected smooth ease-out after wall removal, got Z = %v", cam.Pos.Z)
	}
	app.Advance(2 * time.Second)
	if !cam.Pos.ApproxEqual(math3d.Vec3{X: 10, Y: 3, Z: 5}, 1e-3) {
		t.Fatalf("camera failed to recover after wall removal: cam.Pos = %v", cam.Pos)
	}
}

func TestPickPerspectiveOrthographicMeshAndSphere(t *testing.T) {
	const viewW, viewH = 800, 600
	const (
		layerBox    uint32 = 1 << 0
		layerMesh   uint32 = 1 << 1
		layerSphere uint32 = 1 << 2
	)

	sc := scene3d.NewScene()

	frontBox := scene3d.NewNode("frontBox")
	frontBox.Layer = layerBox
	frontBox.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: -4})
	frontBox.SetLocalBounds(unitBox())

	backBox := scene3d.NewNode("backBox")
	backBox.Layer = layerBox
	backBox.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: -8})
	backBox.SetLocalBounds(unitBox())

	hiddenBox := scene3d.NewNode("hiddenBox")
	hiddenBox.Layer = layerBox
	hiddenBox.Visible = false
	hiddenBox.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: -2})
	hiddenBox.SetLocalBounds(unitBox())

	// Triangle mesh positioned to the right (x = +3, z = -6):
	// Single right triangle occupying lower-left half of [-1, 1] x [-1, 1] so
	// clicking its upper-right AABB corner hits the AABB broad-phase but misses the triangle!
	triNode := scene3d.NewNode("triNode")
	triNode.Layer = layerMesh
	triNode.SetPosition(math3d.Vec3{X: 3, Y: 0, Z: -6})
	triNode.SetCollider(&scene3d.MeshCollider{
		Triangles: []math3d.Vec3{
			{X: -1, Y: -1, Z: 0},
			{X: 1, Y: -1, Z: 0},
			{X: -1, Y: 1, Z: 0},
		},
	})

	// Sphere positioned to the left (x = -3, z = -6) with radius 1:
	// Corner (-3 + 0.9, 0.9, -6) is inside its AABB [-1, 1]^2 but outside radius 1!
	sphereNode := scene3d.NewNode("sphereNode")
	sphereNode.Layer = layerSphere
	sphereNode.SetPosition(math3d.Vec3{X: -3, Y: 0, Z: -6})
	sphereNode.SetCollider(&scene3d.SphereCollider{Radius: 1})

	_ = sc.Add(frontBox)
	_ = sc.Add(backBox)
	_ = sc.Add(hiddenBox)
	_ = sc.Add(triNode)
	_ = sc.Add(sphereNode)

	// --- Perspective camera ---
	persp := scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(60), float32(viewW)/float32(viewH), 0.1, 100)

	// Center click hits frontBox (ignoring closer hiddenBox).
	hit, ok := scene3d.Pick(sc, persp, viewW/2, viewH/2, viewW, viewH, scene3d.LayerAll)
	if !ok || hit.Node != frontBox {
		t.Fatalf("persp center Pick = (%v, %v), want (frontBox, true)", hit.Node, ok)
	}
	if !hit.Point.ApproxEqual(math3d.Vec3{X: 0, Y: 0, Z: -3.5}, 1e-3) {
		t.Fatalf("frontBox hit.Point = %v, want (0, 0, -3.5)", hit.Point)
	}
	// Ray origin starts on near plane (z = -0.1), frontBox front face is at z = -3.5 => T = 3.4.
	if !math3d.ApproxEqual(hit.T, 3.4, 1e-3) {
		t.Fatalf("frontBox hit.T = %v, want 3.4", hit.T)
	}

	// PickAll at center returns frontBox then backBox sorted by ascending T.
	all := scene3d.PickAll(sc, persp, viewW/2, viewH/2, viewW, viewH, scene3d.LayerAll)
	if len(all) != 2 || all[0].Node != frontBox || all[1].Node != backBox {
		t.Fatalf("persp PickAll = %v, want [frontBox, backBox]", all)
	}

	// Layer mask excludes boxes when picking only layerMesh.
	if _, ok := scene3d.Pick(sc, persp, viewW/2, viewH/2, viewW, viewH, layerMesh); ok {
		t.Fatalf("expected center click with layerMesh mask to miss boxes")
	}

	// Clicking empty background corner misses.
	if _, ok := scene3d.Pick(sc, persp, 5, 5, viewW, viewH, scene3d.LayerAll); ok {
		t.Fatalf("expected background click to miss")
	}

	// Narrow-phase MeshCollider: hit triangle interior vs miss upper-right AABB corner.
	triHitScr, _, _ := persp.WorldToScreen(math3d.Vec3{X: 2.5, Y: -0.5, Z: -6}, viewW, viewH)
	triHit, ok := scene3d.Pick(sc, persp, triHitScr.X, triHitScr.Y, viewW, viewH, layerMesh)
	if !ok || triHit.Node != triNode || triHit.TriangleIndex != 0 {
		t.Fatalf("triangle interior Pick = (%+v, %v), want triNode tri 0", triHit, ok)
	}
	if !triHit.Point.ApproxEqual(math3d.Vec3{X: 2.5, Y: -0.5, Z: -6}, 1e-3) {
		t.Fatalf("triangle hit point = %v, want (2.5, -0.5, -6)", triHit.Point)
	}
	if !triHit.Normal.ApproxEqual(math3d.Vec3{X: 0, Y: 0, Z: 1}, 1e-3) {
		t.Fatalf("triangle hit normal = %v, want (0, 0, 1)", triHit.Normal)
	}

	triMissScr, _, _ := persp.WorldToScreen(math3d.Vec3{X: 3.8, Y: 0.8, Z: -6}, viewW, viewH)
	if _, ok := scene3d.Pick(sc, persp, triMissScr.X, triMissScr.Y, viewW, viewH, layerMesh); ok {
		t.Fatalf("expected click on empty AABB corner of MeshCollider to miss")
	}

	// Narrow-phase SphereCollider: hit sphere center vs miss AABB corner outside radius.
	sphHitScr, _, _ := persp.WorldToScreen(math3d.Vec3{X: -3, Y: 0, Z: -6}, viewW, viewH)
	sphHit, ok := scene3d.Pick(sc, persp, sphHitScr.X, sphHitScr.Y, viewW, viewH, layerSphere)
	if !ok || sphHit.Node != sphereNode {
		t.Fatalf("sphere center Pick = (%+v, %v), want sphereNode", sphHit, ok)
	}
	if !math3d.ApproxEqual(sphHit.Point.Dist(sphereNode.WorldTransform().Pos), 1.0, 1e-3) {
		t.Fatalf("sphere hit point %v not on unit sphere surface", sphHit.Point)
	}

	sphMissScr, _, _ := persp.WorldToScreen(math3d.Vec3{X: -2.05, Y: 0.95, Z: -6}, viewW, viewH)
	if _, ok := scene3d.Pick(sc, persp, sphMissScr.X, sphMissScr.Y, viewW, viewH, layerSphere); ok {
		t.Fatalf("expected click on AABB corner outside SphereCollider radius to miss")
	}

	// --- Orthographic camera ---
	ortho := scene3d.NewOrthographicCamera(math3d.Vec3Zero(), 8, float32(viewW)/float32(viewH), 0.1, 100)

	oHit, ok := scene3d.Pick(sc, ortho, viewW/2, viewH/2, viewW, viewH, scene3d.LayerAll)
	if !ok || oHit.Node != frontBox {
		t.Fatalf("ortho center Pick = (%v, %v), want (frontBox, true)", oHit.Node, ok)
	}
	oAll := scene3d.PickAll(sc, ortho, viewW/2, viewH/2, viewW, viewH, scene3d.LayerAll)
	if len(oAll) != 2 || oAll[0].Node != frontBox || oAll[1].Node != backBox {
		t.Fatalf("ortho PickAll = %v, want [frontBox, backBox]", oAll)
	}

	// Ortho off-center click directly hits the sphere on the left and triangle on the right.
	oSphScr, _, _ := ortho.WorldToScreen(math3d.Vec3{X: -3, Y: 0, Z: -6}, viewW, viewH)
	oSphHit, ok := scene3d.Pick(sc, ortho, oSphScr.X, oSphScr.Y, viewW, viewH, scene3d.LayerAll)
	if !ok || oSphHit.Node != sphereNode || !oSphHit.Point.ApproxEqual(math3d.Vec3{X: -3, Y: 0, Z: -5}, 1e-3) {
		t.Fatalf("ortho sphere Pick = (%+v, %v), want sphereNode at (-3, 0, -5)", oSphHit, ok)
	}
	if !oSphHit.Normal.ApproxEqual(math3d.Vec3{X: 0, Y: 0, Z: 1}, 1e-3) {
		t.Fatalf("ortho sphere hit normal = %v, want (0, 0, 1)", oSphHit.Normal)
	}

	oTriScr, _, _ := ortho.WorldToScreen(math3d.Vec3{X: 2.5, Y: -0.5, Z: -6}, viewW, viewH)
	oTriHit, ok := scene3d.Pick(sc, ortho, oTriScr.X, oTriScr.Y, viewW, viewH, scene3d.LayerAll)
	if !ok || oTriHit.Node != triNode {
		t.Fatalf("ortho triangle Pick = (%+v, %v), want triNode", oTriHit, ok)
	}

	// Ortho background miss.
	if _, ok := scene3d.Pick(sc, ortho, 10, 10, viewW, viewH, scene3d.LayerAll); ok {
		t.Fatalf("expected ortho background click to miss")
	}
}

func TestCameraSyncFromSocketAndColliderTransforms(t *testing.T) {
	var zeroScene scene3d.Scene // zero-value Scene must work safely
	rig := scene3d.NewNode("rig")
	rig.SetPosition(math3d.Vec3{X: 0, Y: 2, Z: 10})
	rig.DefineSocket("cam_mount", math3d.Transform{
		Pos:   math3d.Vec3{X: 0, Y: 1, Z: 0},
		Rot:   math3d.QuatIdentity(),
		Scale: math3d.Vec3One(),
	})
	camNode := scene3d.NewNode("camNode")
	if err := rig.Attach("cam_mount", camNode); err != nil {
		t.Fatalf("Attach camNode: %v", err)
	}
	_ = zeroScene.Add(rig)

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(60), 1, 0.1, 100)
	cam.SyncFromNode(camNode)
	if !cam.Pos.ApproxEqual(math3d.Vec3{X: 0, Y: 3, Z: 10}, eps) {
		t.Fatalf("SyncFromNode cam.Pos = %v, want (0, 3, 10)", cam.Pos)
	}

	// Non-uniformly scaled SphereCollider (stretched 2x along X -> hits at x=1.5, misses at x=2.5).
	ellipsoid := scene3d.NewNode("ellipsoid")
	ellipsoid.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: -5})
	ellipsoid.SetScale(math3d.Vec3{X: 2, Y: 1, Z: 1})
	ellipsoid.SetCollider(&scene3d.SphereCollider{Radius: 1})
	_ = zeroScene.Add(ellipsoid)

	hitRay := math3d.Ray{Origin: math3d.Vec3{X: 1.5, Y: 0, Z: 0}, Dir: math3d.Vec3Forward()}
	if h, ok := zeroScene.RaycastFirst(hitRay, scene3d.LayerAll); !ok || h.Node != ellipsoid {
		t.Fatalf("scaled SphereCollider ray at x=1.5 = (%+v, %v), want hit", h, ok)
	}
	missRay := math3d.Ray{Origin: math3d.Vec3{X: 2.5, Y: 0, Z: 0}, Dir: math3d.Vec3Forward()}
	if _, ok := zeroScene.RaycastFirst(missRay, scene3d.LayerAll); ok {
		t.Fatalf("scaled SphereCollider ray at x=2.5 should miss")
	}

	// Flat singular-scale MeshCollider (Scale.Z = 0) still intersects via world-space fallback.
	flat := scene3d.NewNode("flat")
	flat.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: -2})
	flat.SetScale(math3d.Vec3{X: 2, Y: 2, Z: 0})
	flat.SetCollider(&scene3d.MeshCollider{
		Triangles: []math3d.Vec3{
			{X: -1, Y: -1, Z: 0},
			{X: 1, Y: -1, Z: 0},
			{X: 0, Y: 1, Z: 0},
		},
	})
	_ = zeroScene.Add(flat)

	centerRay := math3d.Ray{Origin: math3d.Vec3Zero(), Dir: math3d.Vec3Forward()}
	if h, ok := zeroScene.RaycastFirst(centerRay, scene3d.LayerAll); !ok || h.Node != flat || !math3d.ApproxEqual(h.T, 2, eps) {
		t.Fatalf("singular-scale MeshCollider ray = (%+v, %v), want flat at T=2", h, ok)
	}

	// HierarchyBounds ignores hidden children.
	hb := zeroScene.Root().HierarchyBounds()
	if hb.Empty() || !hb.Contains(math3d.Vec3{X: 1.5, Y: 0, Z: -5}) {
		t.Fatalf("unexpected HierarchyBounds: %+v", hb)
	}
}

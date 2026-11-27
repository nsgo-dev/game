package scene3d_test

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
)

func TestSceneDirtyUpdatesAndVisibilityEnableComposition(t *testing.T) {
	sc := scene3d.NewScene()

	// Build a 3-level hierarchy: root -> 2 branches -> 5 leaves each (1 + 2 + 10 = 13 nodes).
	branchA := scene3d.NewNode("branchA")
	branchA.SetPosition(math3d.Vec3{X: 10, Y: 0, Z: 0})
	branchB := scene3d.NewNode("branchB")
	branchB.SetPosition(math3d.Vec3{X: -10, Y: 0, Z: 0})
	_ = sc.Add(branchA)
	_ = sc.Add(branchB)

	leavesA := make([]*scene3d.Node, 5)
	leavesB := make([]*scene3d.Node, 5)
	for i := range 5 {
		a := scene3d.NewNode("leafA")
		a.SetPosition(math3d.Vec3{X: float32(i), Y: 0, Z: -5})
		a.SetLocalBounds(unitBox())
		_ = branchA.AddChild(a)
		leavesA[i] = a

		b := scene3d.NewNode("leafB")
		b.SetPosition(math3d.Vec3{X: float32(i), Y: 0, Z: -5})
		b.SetLocalBounds(unitBox())
		_ = branchB.AddChild(b)
		leavesB[i] = b
	}

	// First UpdateTransforms recomputes all 13 nodes.
	if got := sc.UpdateTransforms(); got != 13 {
		t.Fatalf("initial UpdateTransforms() = %d, want 13", got)
	}
	if sc.DirtyUpdates() != 13 {
		t.Fatalf("DirtyUpdates() = %d, want 13", sc.DirtyUpdates())
	}
	for _, n := range leavesA {
		if n.DirtyUpdates() != 1 {
			t.Fatalf("leafA DirtyUpdates() = %d, want 1", n.DirtyUpdates())
		}
	}

	// Clean tree performs 0 recomputations.
	if got := sc.UpdateTransforms(); got != 0 {
		t.Fatalf("clean UpdateTransforms() = %d, want 0", got)
	}
	if sc.DirtyUpdates() != 0 {
		t.Fatalf("clean DirtyUpdates() = %d, want 0", sc.DirtyUpdates())
	}
	// Reading WorldMatrix/WorldTransform on a clean node must also perform 0 recomputations.
	_ = leavesA[0].WorldMatrix()
	_ = leavesA[0].WorldTransform()
	if leavesA[0].DirtyUpdates() != 1 {
		t.Fatalf("clean WorldMatrix recomputed node: DirtyUpdates() = %d, want 1", leavesA[0].DirtyUpdates())
	}

	// Moving only branchA dirties branchA + its 5 leaves = 6 nodes; branchB stays untouched.
	branchA.SetPosition(math3d.Vec3{X: 12, Y: 1, Z: 0})
	if got := sc.UpdateTransforms(); got != 6 {
		t.Fatalf("partial UpdateTransforms() = %d, want 6", got)
	}
	if sc.DirtyUpdates() != 6 {
		t.Fatalf("partial DirtyUpdates() = %d, want 6", sc.DirtyUpdates())
	}
	if leavesA[0].DirtyUpdates() != 2 {
		t.Fatalf("dirty leafA DirtyUpdates() = %d, want 2", leavesA[0].DirtyUpdates())
	}
	if leavesB[0].DirtyUpdates() != 1 {
		t.Fatalf("clean leafB DirtyUpdates() = %d, want 1", leavesB[0].DirtyUpdates())
	}

	// Reparenting via SetParent with keepWorld=true vs keepWorld=false.
	mover := leavesA[0]
	beforeWorld := mover.WorldTransform()
	if err := mover.SetParent(branchB, true); err != nil {
		t.Fatalf("SetParent(branchB, true): %v", err)
	}
	if !mover.WorldTransform().ApproxEqual(beforeWorld, eps) {
		t.Fatalf("SetParent(keepWorld=true) altered world transform: got %+v, want %+v",
			mover.WorldTransform(), beforeWorld)
	}
	savedLocal := math3d.Transform{
		Pos:   math3d.Vec3{X: 1, Y: 2, Z: 3},
		Rot:   math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(30)),
		Scale: math3d.Vec3One(),
	}
	mover.SetLocal(savedLocal)
	if err := mover.SetParent(branchA, false); err != nil {
		t.Fatalf("SetParent(branchA, false): %v", err)
	}
	if !mover.Local.ApproxEqual(savedLocal, eps) {
		t.Fatalf("SetParent(keepWorld=false) altered local transform: got %+v, want %+v",
			mover.Local, savedLocal)
	}
	wantWorld := branchA.WorldTransform().Mul(savedLocal)
	if !mover.WorldTransform().ApproxEqual(wantWorld, eps) {
		t.Fatalf("SetParent(keepWorld=false) world = %+v, want %+v", mover.WorldTransform(), wantWorld)
	}

	// Visibility & Enabled hierarchical composition.
	if !leavesA[1].EffectiveVisible() || !leavesA[1].EffectiveEnabled() {
		t.Fatalf("expected leafA[1] visible and enabled by default")
	}

	// Hiding branchA hides descendants while leaving them enabled.
	branchA.SetVisible(false)
	if leavesA[1].EffectiveVisible() {
		t.Fatalf("expected leafA[1].EffectiveVisible() == false when branchA hidden")
	}
	if !leavesA[1].EffectiveEnabled() {
		t.Fatalf("expected leafA[1].EffectiveEnabled() == true when branchA only hidden")
	}

	// Disabling branchB disables descendants while leaving Visible flag orthogonal.
	branchB.SetEnabled(false)
	if leavesB[1].EffectiveEnabled() {
		t.Fatalf("expected leafB[1].EffectiveEnabled() == false when branchB disabled")
	}
	if !leavesB[1].EffectiveVisible() {
		t.Fatalf("expected leafB[1].EffectiveVisible() == true when branchB only disabled")
	}

	// Spatial queries must exclude both hidden (branchA) and disabled (branchB) subtrees.
	hugeBox := math3d.AABB{
		Min: math3d.Vec3{X: -100, Y: -100, Z: -100},
		Max: math3d.Vec3{X: 100, Y: 100, Z: 100},
	}
	if got := sc.QueryAABB(hugeBox); len(got) != 0 {
		t.Fatalf("QueryAABB with all branches hidden/disabled = %d nodes, want 0", len(got))
	}

	// Re-enabling branchB immediately restores its 5 leaves to spatial queries.
	branchB.SetEnabled(true)
	if got := sc.QueryAABB(hugeBox); len(got) != 5 {
		t.Fatalf("QueryAABB after re-enabling branchB = %d nodes, want 5", len(got))
	}
	// Re-showing branchA restores all 10 leaves.
	branchA.SetVisible(true)
	if got := sc.QueryAABB(hugeBox); len(got) != 10 {
		t.Fatalf("QueryAABB after re-showing branchA = %d nodes, want 10", len(got))
	}
}

func TestAnimatedBoneSocketsAndDetachKeepWorld(t *testing.T) {
	hero := scene3d.NewNode("hero")
	hips := scene3d.NewNode("hips")
	spine := scene3d.NewNode("spine")
	shoulder := scene3d.NewNode("shoulder")
	elbow := scene3d.NewNode("elbow")
	hand := scene3d.NewNode("hand")

	hips.SetPosition(math3d.Vec3{X: 0, Y: 1.0, Z: 0})
	spine.SetPosition(math3d.Vec3{X: 0, Y: 0.5, Z: 0})
	shoulder.SetPosition(math3d.Vec3{X: 0.3, Y: 0.4, Z: 0})
	elbow.SetPosition(math3d.Vec3{X: 0.35, Y: 0, Z: 0})
	hand.SetPosition(math3d.Vec3{X: 0.3, Y: 0, Z: 0})

	_ = hero.AddChild(hips)
	_ = hips.AddChild(spine)
	_ = spine.AddChild(shoulder)
	_ = shoulder.AddChild(elbow)
	_ = elbow.AddChild(hand)

	gripOffset := math3d.Transform{
		Pos:   math3d.Vec3{X: 0.08, Y: -0.02, Z: -0.05},
		Rot:   math3d.QuatFromAxisAngle(math3d.Vec3UnitX(), math3d.DegToRad(-15)),
		Scale: math3d.Vec3One(),
	}
	gripSocket := hand.DefineSocket("weapon_grip", gripOffset)

	weapon := scene3d.NewNode("weapon")
	weaponLocal := math3d.Transform{
		Pos:   math3d.Vec3{X: 0, Y: 0.1, Z: -0.6},
		Rot:   math3d.QuatFromAxisAngle(math3d.Vec3UnitZ(), math3d.DegToRad(10)),
		Scale: math3d.Vec3{X: 1, Y: 1, Z: 1},
	}
	weapon.SetLocal(weaponLocal)
	if err := hero.Attach("weapon_grip", weapon); err != nil {
		t.Fatalf("Attach weapon to weapon_grip: %v", err)
	}
	if weapon.Parent() != gripSocket {
		t.Fatalf("weapon parent = %v, want gripSocket", weapon.Parent())
	}

	// Attach a second accessory directly to the nested "hand" joint by bone name.
	wristGuard := scene3d.NewNode("wristGuard")
	wristLocal := math3d.Transform{
		Pos:   math3d.Vec3{X: -0.05, Y: 0.02, Z: 0},
		Rot:   math3d.QuatIdentity(),
		Scale: math3d.Vec3One(),
	}
	wristGuard.SetLocal(wristLocal)
	if err := hero.Attach("hand", wristGuard); err != nil {
		t.Fatalf("Attach wristGuard to hand bone: %v", err)
	}
	if wristGuard.Parent() != hand {
		t.Fatalf("wristGuard parent = %v, want hand", wristGuard.Parent())
	}

	// Step through 10 animated skeletal pose frames and verify exact world transforms.
	for frame := range 10 {
		angle := float32(frame) * 0.25
		hero.SetPosition(math3d.Vec3{X: float32(frame) * 0.5, Y: 0, Z: -float32(frame) * 0.3})
		hero.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3Up(), angle*0.5))
		hips.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3UnitY(), angle*0.3))
		shoulder.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3UnitZ(), angle))
		elbow.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3UnitY(), -angle*1.2))
		hand.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3UnitX(), angle*0.7))

		wantWeaponMat := hero.Local.ToMat4().
			Mul(hips.Local.ToMat4()).
			Mul(spine.Local.ToMat4()).
			Mul(shoulder.Local.ToMat4()).
			Mul(elbow.Local.ToMat4()).
			Mul(hand.Local.ToMat4()).
			Mul(gripOffset.ToMat4()).
			Mul(weaponLocal.ToMat4())
		if !weapon.WorldMatrix().ApproxEqual(wantWeaponMat, 1e-3) {
			t.Fatalf("frame %d: weapon.WorldMatrix mismatch:\ngot  %v\nwant %v",
				frame, weapon.WorldMatrix(), wantWeaponMat)
		}

		wantWristMat := hero.Local.ToMat4().
			Mul(hips.Local.ToMat4()).
			Mul(spine.Local.ToMat4()).
			Mul(shoulder.Local.ToMat4()).
			Mul(elbow.Local.ToMat4()).
			Mul(hand.Local.ToMat4()).
			Mul(wristLocal.ToMat4())
		if !wristGuard.WorldMatrix().ApproxEqual(wantWristMat, 1e-3) {
			t.Fatalf("frame %d: wristGuard.WorldMatrix mismatch", frame)
		}
	}

	// Detach(keepWorld=true) mid-animation preserves exact world transform and matrix!
	preDetachTransform := weapon.WorldTransform()
	preDetachMat := weapon.WorldMatrix()
	if !weapon.Detach(true) {
		t.Fatalf("weapon.Detach(true) returned false")
	}
	if weapon.Parent() != nil {
		t.Fatalf("weapon still has parent %v after Detach", weapon.Parent())
	}
	if attached := hand.Attached("weapon_grip"); len(attached) != 0 {
		t.Fatalf("weapon_grip still has attached nodes: %v", attached)
	}
	if !weapon.WorldTransform().ApproxEqual(preDetachTransform, eps) {
		t.Fatalf("Detach(true) world transform = %+v, want %+v",
			weapon.WorldTransform(), preDetachTransform)
	}
	if !weapon.WorldMatrix().ApproxEqual(preDetachMat, 1e-3) {
		t.Fatalf("Detach(true) world matrix changed:\ngot  %v\nwant %v",
			weapon.WorldMatrix(), preDetachMat)
	}

	// Subsequent skeleton animation must NOT affect the detached weapon.
	hero.SetPosition(math3d.Vec3{X: 100, Y: 50, Z: -200})
	hand.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(180)))
	if !weapon.WorldTransform().ApproxEqual(preDetachTransform, eps) {
		t.Fatalf("detached weapon moved after subsequent hero animation")
	}

	// Detach(keepWorld=false) preserves Local transform as its new root/world transform.
	if !wristGuard.Detach(false) {
		t.Fatalf("wristGuard.Detach(false) returned false")
	}
	if wristGuard.Parent() != nil {
		t.Fatalf("wristGuard still has parent after Detach(false)")
	}
	if !wristGuard.Local.ApproxEqual(wristLocal, eps) || !wristGuard.WorldTransform().ApproxEqual(wristLocal, eps) {
		t.Fatalf("Detach(false) transform = %+v, want local %+v", wristGuard.WorldTransform(), wristLocal)
	}
	// Detaching an already-detached node returns false cleanly.
	if wristGuard.Detach(true) {
		t.Fatalf("second Detach should return false")
	}
}

func TestBVHExactEquivalenceAgainstBruteForce(t *testing.T) {
	rng := rand.New(rand.NewPCG(2026, 378))
	bvh := scene3d.NewBVH()

	type item struct {
		id     uint64
		handle scene3d.BVHHandle
		bounds math3d.AABB
	}

	randBox := func(spread float32) math3d.AABB {
		cx := (rng.Float32()*2 - 1) * spread
		cy := (rng.Float32()*2 - 1) * spread
		cz := (rng.Float32()*2 - 1) * spread
		// Mix tiny, normal, and large boxes.
		var hx, hy, hz float32
		switch rng.IntN(10) {
		case 0: // point-like degenerate AABB
			hx, hy, hz = 0, 0, 0
		case 1: // large structure
			hx = 5 + rng.Float32()*15
			hy = 5 + rng.Float32()*15
			hz = 5 + rng.Float32()*15
		default:
			hx = 0.2 + rng.Float32()*3.0
			hy = 0.2 + rng.Float32()*3.0
			hz = 0.2 + rng.Float32()*3.0
		}
		return math3d.AABB{
			Min: math3d.Vec3{X: cx - hx, Y: cy - hy, Z: cz - hz},
			Max: math3d.Vec3{X: cx + hx, Y: cy + hy, Z: cz + hz},
		}
	}

	const initialCount = 2500
	live := make(map[uint64]*item, initialCount)
	var nextID uint64 = 1

	insertOne := func(box math3d.AABB) {
		id := nextID
		nextID++
		h := bvh.Insert(id, box, id)
		if !h.Valid() {
			t.Fatalf("Insert returned invalid handle")
		}
		live[id] = &item{id: id, handle: h, bounds: box}
	}

	for range initialCount {
		insertOne(randBox(150))
	}
	if bvh.Len() != initialCount {
		t.Fatalf("BVH.Len() = %d, want %d", bvh.Len(), initialCount)
	}

	// Dynamic updates: small moves within fat AABB, teleports, and shrinks.
	allIDs := make([]uint64, 0, len(live))
	for id := range live {
		allIDs = append(allIDs, id)
	}
	slices.Sort(allIDs)
	for i := range 800 {
		id := allIDs[rng.IntN(len(allIDs))]
		it := live[id]
		var nb math3d.AABB
		switch i % 3 {
		case 0:
			d := math3d.Vec3{
				X: (rng.Float32()*2 - 1) * 0.03,
				Y: (rng.Float32()*2 - 1) * 0.03,
				Z: (rng.Float32()*2 - 1) * 0.03,
			}
			nb = math3d.AABB{Min: it.bounds.Min.Add(d), Max: it.bounds.Max.Add(d)}
		case 1:
			nb = randBox(150)
		case 2:
			c := it.bounds.Center()
			ext := math3d.Vec3{X: 0.05, Y: 0.05, Z: 0.05}
			nb = math3d.AABB{Min: c.Sub(ext), Max: c.Add(ext)}
		}
		if !bvh.Update(it.handle, nb) {
			t.Fatalf("BVH.Update failed for live id %d", id)
		}
		it.bounds = nb
	}

	// Dynamic removals (400 items).
	for i := range 400 {
		id := allIDs[i*5]
		it := live[id]
		bvh.Remove(it.handle)
		delete(live, id)
	}
	// Dynamic re-insertions (400 new items).
	for range 400 {
		insertOne(randBox(150))
	}
	if bvh.Len() != len(live) {
		t.Fatalf("BVH.Len() = %d, want %d", bvh.Len(), len(live))
	}

	verifyAllQueries := func(phase string) {
		t.Helper()

		var handleBuf []scene3d.BVHHandle
		var hitBuf []scene3d.BVHRayHit

		// 1. Frustum queries across 6 perspective and orthographic cameras.
		cams := []*scene3d.Camera{
			scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 10, Z: 80}, math3d.DegToRad(60), 16.0/9.0, 0.5, 160),
			scene3d.NewPerspectiveCamera(math3d.Vec3{X: -120, Y: 40, Z: -90}, math3d.DegToRad(45), 1.0, 1.0, 220),
			scene3d.NewPerspectiveCamera(math3d.Vec3{X: 50, Y: -30, Z: 20}, math3d.DegToRad(90), 4.0/3.0, 0.2, 80),
			scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 0, Z: 0}, math3d.DegToRad(35), 2.0, 5.0, 60),
			scene3d.NewOrthographicCamera(math3d.Vec3{X: 0, Y: 0, Z: 100}, 60, 1.5, 1.0, 200),
			scene3d.NewOrthographicCamera(math3d.Vec3{X: 80, Y: 80, Z: 80}, 40, 1.0, 0.5, 150),
		}
		cams[1].LookAt(math3d.Vec3{X: 10, Y: -5, Z: 15}, math3d.Vec3Up())
		cams[2].LookAt(math3d.Vec3{X: -40, Y: 20, Z: -50}, math3d.Vec3Up())
		cams[5].LookAt(math3d.Vec3Zero(), math3d.Vec3Up())

		for ci, cam := range cams {
			fr := cam.Frustum()
			gotHandles := bvh.QueryFrustum(fr, handleBuf[:0])
			handleBuf = gotHandles

			gotIDs := make([]uint64, len(gotHandles))
			for i, h := range gotHandles {
				id, ok := bvh.ID(h)
				if !ok {
					t.Fatalf("%s frustum[%d]: invalid handle returned", phase, ci)
				}
				gotIDs[i] = id
			}
			slices.Sort(gotIDs)

			var wantIDs []uint64
			for id, it := range live {
				if fr.IntersectsAABB(it.bounds) {
					wantIDs = append(wantIDs, id)
				}
			}
			slices.Sort(wantIDs)

			if !slices.Equal(gotIDs, wantIDs) {
				t.Fatalf("%s frustum[%d]: BVH returned %d hits, brute force returned %d hits",
					phase, ci, len(gotIDs), len(wantIDs))
			}
		}

		// 2. AABB queries across 10 regions (including empty, point, small, large).
		regions := []math3d.AABB{
			math3d.AABBEmpty(),
			{Min: math3d.Vec3Zero(), Max: math3d.Vec3Zero()},
			{Min: math3d.Vec3{X: -20, Y: -20, Z: -20}, Max: math3d.Vec3{X: 20, Y: 20, Z: 20}},
			{Min: math3d.Vec3{X: 50, Y: -10, Z: 10}, Max: math3d.Vec3{X: 120, Y: 60, Z: 80}},
			{Min: math3d.Vec3{X: -200, Y: -200, Z: -200}, Max: math3d.Vec3{X: 200, Y: 200, Z: 200}},
			{Min: math3d.Vec3{X: 500, Y: 500, Z: 500}, Max: math3d.Vec3{X: 600, Y: 600, Z: 600}},
		}
		for range 4 {
			regions = append(regions, randBox(100))
		}
		for ri, reg := range regions {
			gotHandles := bvh.QueryAABB(reg, handleBuf[:0])
			handleBuf = gotHandles
			gotIDs := make([]uint64, len(gotHandles))
			for i, h := range gotHandles {
				id, _ := bvh.ID(h)
				gotIDs[i] = id
			}
			slices.Sort(gotIDs)

			var wantIDs []uint64
			for id, it := range live {
				if it.bounds.Intersects(reg) {
					wantIDs = append(wantIDs, id)
				}
			}
			slices.Sort(wantIDs)
			if !slices.Equal(gotIDs, wantIDs) {
				t.Fatalf("%s AABB[%d]: BVH=%d, brute=%d", phase, ri, len(gotIDs), len(wantIDs))
			}
		}

		// 3. Sphere queries across 10 spheres.
		spheres := []math3d.Sphere{
			{Center: math3d.Vec3Zero(), Radius: -1},
			{Center: math3d.Vec3Zero(), Radius: 0},
			{Center: math3d.Vec3Zero(), Radius: 25},
			{Center: math3d.Vec3{X: 80, Y: -40, Z: 30}, Radius: 50},
			{Center: math3d.Vec3{X: -60, Y: 70, Z: -50}, Radius: 15},
			{Center: math3d.Vec3{X: 0, Y: 0, Z: 0}, Radius: 400},
			{Center: math3d.Vec3{X: 900, Y: 900, Z: 900}, Radius: 5},
		}
		for range 3 {
			spheres = append(spheres, math3d.Sphere{
				Center: math3d.Vec3{
					X: (rng.Float32()*2 - 1) * 100,
					Y: (rng.Float32()*2 - 1) * 100,
					Z: (rng.Float32()*2 - 1) * 100,
				},
				Radius: 10 + rng.Float32()*40,
			})
		}
		for si, sp := range spheres {
			gotHandles := bvh.QuerySphere(sp.Center, sp.Radius, handleBuf[:0])
			handleBuf = gotHandles
			gotIDs := make([]uint64, len(gotHandles))
			for i, h := range gotHandles {
				id, _ := bvh.ID(h)
				gotIDs[i] = id
			}
			slices.Sort(gotIDs)

			var wantIDs []uint64
			for id, it := range live {
				if sp.IntersectsAABB(it.bounds) {
					wantIDs = append(wantIDs, id)
				}
			}
			slices.Sort(wantIDs)
			if !slices.Equal(gotIDs, wantIDs) {
				t.Fatalf("%s Sphere[%d]: BVH=%d, brute=%d", phase, si, len(gotIDs), len(wantIDs))
			}
		}

		// 4. RaycastAll and Raycast across diverse rays and maxDist limits.
		type rayCase struct {
			ray     math3d.Ray
			maxDist float32
		}
		rayCases := []rayCase{
			{math3d.Ray{Origin: math3d.Vec3{X: 0, Y: 0, Z: 200}, Dir: math3d.Vec3{X: 0, Y: 0, Z: -1}}, 400},
			{math3d.Ray{Origin: math3d.Vec3{X: -200, Y: 0, Z: 0}, Dir: math3d.Vec3{X: 1, Y: 0, Z: 0}}, 150},
			{math3d.Ray{Origin: math3d.Vec3{X: 0, Y: -200, Z: 0}, Dir: math3d.Vec3{X: 0, Y: 1, Z: 0}}, float32(math.Inf(1))},
			{math3d.Ray{Origin: math3d.Vec3{X: -150, Y: -150, Z: -150}, Dir: math3d.Vec3{X: 1, Y: 1, Z: 1}.Norm()}, 600},
			{math3d.Ray{Origin: math3d.Vec3{X: 120, Y: -80, Z: 90}, Dir: math3d.Vec3{X: -0.7, Y: 0.4, Z: -0.5}.Norm()}, 250},
			{math3d.Ray{Origin: math3d.Vec3Zero(), Dir: math3d.Vec3{X: 0.3, Y: -0.8, Z: 0.5}.Norm()}, 80},
			{math3d.Ray{Origin: math3d.Vec3Zero(), Dir: math3d.Vec3Zero()}, 10},
			{math3d.Ray{Origin: math3d.Vec3{X: 500, Y: 500, Z: 500}, Dir: math3d.Vec3{X: 1, Y: 0, Z: 0}}, 100},
			{math3d.Ray{Origin: math3d.Vec3Zero(), Dir: math3d.Vec3Forward()}, 0},
		}
		for range 4 {
			o := math3d.Vec3{
				X: (rng.Float32()*2 - 1) * 180,
				Y: (rng.Float32()*2 - 1) * 180,
				Z: (rng.Float32()*2 - 1) * 180,
			}
			d := math3d.Vec3{
				X: rng.Float32()*2 - 1,
				Y: rng.Float32()*2 - 1,
				Z: rng.Float32()*2 - 1,
			}.Norm()
			rayCases = append(rayCases, rayCase{math3d.Ray{Origin: o, Dir: d}, 200})
		}

		for rci, rc := range rayCases {
			got := bvh.RaycastAll(rc.ray, rc.maxDist, hitBuf[:0])
			hitBuf = got

			var want []scene3d.BVHRayHit
			for id, it := range live {
				if tMin, _, ok := rc.ray.IntersectAABB(it.bounds); ok && tMin <= rc.maxDist {
					want = append(want, scene3d.BVHRayHit{
						Handle:   it.handle,
						ID:       id,
						UserData: id,
						Distance: tMin,
					})
				}
			}
			slices.SortFunc(want, func(a, b scene3d.BVHRayHit) int {
				if a.Distance < b.Distance {
					return -1
				}
				if a.Distance > b.Distance {
					return 1
				}
				if a.ID < b.ID {
					return -1
				}
				if a.ID > b.ID {
					return 1
				}
				return 0
			})

			if len(got) != len(want) {
				t.Fatalf("%s ray[%d]: BVH returned %d hits, brute force returned %d hits",
					phase, rci, len(got), len(want))
			}
			for i := range got {
				if got[i].ID != want[i].ID || !math3d.ApproxEqual(got[i].Distance, want[i].Distance, 1e-4) {
					t.Fatalf("%s ray[%d] hit[%d]: got (ID=%d, dist=%v), want (ID=%d, dist=%v)",
						phase, rci, i, got[i].ID, got[i].Distance, want[i].ID, want[i].Distance)
				}
				if i > 0 && got[i].Distance < got[i-1].Distance {
					t.Fatalf("%s ray[%d]: RaycastAll not sorted ascending at %d", phase, rci, i)
				}
			}

			// Verify Raycast early stop visits exactly 1 hit when callback returns false.
			if len(want) >= 2 {
				calls := 0
				bvh.Raycast(rc.ray, rc.maxDist, func(h scene3d.BVHHandle, hitDist float32) bool {
					calls++
					return false
				})
				if calls != 1 {
					t.Fatalf("%s ray[%d]: Raycast early stop invoked callback %d times, want 1", phase, rci, calls)
				}
			}
		}
	}

	// Verify before Rebuild (dynamic AVL tree) AND after Rebuild (binned SAH tree).
	verifyAllQueries("dynamic")
	bvh.Rebuild()
	verifyAllQueries("sah-rebuilt")
}

func TestSceneBVHExactEquivalenceDynamic(t *testing.T) {
	rng := rand.New(rand.NewPCG(77, 88))
	sc := scene3d.NewScene()

	groups := make([]*scene3d.Node, 20)
	for g := range groups {
		grp := scene3d.NewNode("group")
		grp.SetPosition(math3d.Vec3{
			X: (rng.Float32()*2 - 1) * 50,
			Y: (rng.Float32()*2 - 1) * 20,
			Z: (rng.Float32()*2 - 1) * 50,
		})
		_ = sc.Add(grp)
		groups[g] = grp
	}

	nodes := make([]*scene3d.Node, 0, 1000)
	for i := range 1000 {
		n := scene3d.NewNode("obj")
		n.SetPosition(math3d.Vec3{
			X: (rng.Float32()*2 - 1) * 30,
			Y: (rng.Float32()*2 - 1) * 30,
			Z: (rng.Float32()*2 - 1) * 30,
		})
		if i%10 == 0 {
			n.SetCollider(&scene3d.SphereCollider{Radius: 1.5})
		} else {
			ext := 0.5 + rng.Float32()*2.0
			n.SetLocalBounds(math3d.AABB{
				Min: math3d.Vec3{X: -ext, Y: -ext, Z: -ext},
				Max: math3d.Vec3{X: ext, Y: ext, Z: ext},
			})
		}
		if i%3 == 0 {
			n.Layer = 1 << 1
		}
		_ = groups[i%len(groups)].AddChild(n)
		nodes = append(nodes, n)
	}

	// Hide 2 groups, disable 2 groups, move 5 groups, reparent 30 nodes, remove 50 nodes.
	groups[0].Visible = false
	groups[1].Visible = false
	groups[2].Enabled = false
	groups[3].Enabled = false
	for g := 4; g < 9; g++ {
		groups[g].SetPosition(math3d.Vec3{X: float32(g) * 10, Y: 5, Z: -float32(g) * 8})
		groups[g].SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3Up(), float32(g)*0.3))
	}
	for i := range 30 {
		_ = nodes[i].ReparentKeepWorld(groups[(i+7)%len(groups)])
	}
	for i := 950; i < 1000; i++ {
		sc.Remove(nodes[i])
	}

	bruteVisible := func() []*scene3d.Node {
		var vis []*scene3d.Node
		sc.Walk(func(n *scene3d.Node) bool {
			if n.EffectiveVisible() && n.EffectiveEnabled() && !n.WorldBounds().Empty() {
				vis = append(vis, n)
			}
			return true
		})
		return vis
	}

	checkScene := func(label string) {
		t.Helper()
		vis := bruteVisible()

		cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 10, Z: 90}, math3d.DegToRad(60), 16.0/9.0, 0.5, 200)
		fr := cam.Frustum()
		gotFrustum := sc.CullFrustum(fr)
		wantFrustum := make(map[*scene3d.Node]bool)
		for _, n := range vis {
			if fr.IntersectsAABB(n.WorldBounds()) {
				wantFrustum[n] = true
			}
		}
		if len(gotFrustum) != len(wantFrustum) {
			t.Fatalf("%s CullFrustum got %d, want %d", label, len(gotFrustum), len(wantFrustum))
		}
		for _, n := range gotFrustum {
			if !wantFrustum[n] {
				t.Fatalf("%s CullFrustum returned unexpected node %v", label, n)
			}
		}

		sp := math3d.Sphere{Center: math3d.Vec3{X: 10, Y: 0, Z: -10}, Radius: 35}
		gotSphere := sc.QuerySphere(sp)
		wantSphere := make(map[*scene3d.Node]bool)
		for _, n := range vis {
			if sp.IntersectsAABB(n.WorldBounds()) {
				wantSphere[n] = true
			}
		}
		if len(gotSphere) != len(wantSphere) {
			t.Fatalf("%s QuerySphere got %d, want %d", label, len(gotSphere), len(wantSphere))
		}

		ray := math3d.Ray{Origin: math3d.Vec3{X: 0, Y: 0, Z: 120}, Dir: math3d.Vec3{X: 0.05, Y: -0.02, Z: -1}.Norm()}
		hits := sc.Raycast(ray, scene3d.LayerAll)
		first, ok := sc.RaycastFirst(ray, scene3d.LayerAll)
		if len(hits) == 0 {
			if ok {
				t.Fatalf("%s RaycastFirst hit when Raycast returned 0 hits", label)
			}
		} else {
			if !ok || first.Node != hits[0].Node || !math3d.ApproxEqual(first.T, hits[0].T, eps) {
				t.Fatalf("%s RaycastFirst = (%v, %v), want (%v, %v)",
					label, first.Node, first.T, hits[0].Node, hits[0].T)
			}
		}
	}

	checkScene("incremental")
	sc.RebuildBVH()
	checkScene("rebuilt")
}

func TestBVHCull50kObjects(t *testing.T) {
	const count = 50_000
	rng := rand.New(rand.NewPCG(50000, 378))

	bvh := scene3d.NewBVH()
	boxes := make([]math3d.AABB, count)
	for i := range count {
		cx := (rng.Float32()*2 - 1) * 500
		cy := (rng.Float32()*2 - 1) * 100
		cz := (rng.Float32()*2 - 1) * 500
		hx := 0.5 + rng.Float32()*1.5
		hy := 0.5 + rng.Float32()*1.5
		hz := 0.5 + rng.Float32()*1.5
		b := math3d.AABB{
			Min: math3d.Vec3{X: cx - hx, Y: cy - hy, Z: cz - hz},
			Max: math3d.Vec3{X: cx + hx, Y: cy + hy, Z: cz + hz},
		}
		boxes[i] = b
		bvh.Insert(uint64(i), b, nil)
	}

	// Narrow camera frustum viewing a modest slice of the 1000x200x1000 world.
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 10, Z: 200}, math3d.DegToRad(50), 16.0/9.0, 1.0, 120.0)
	frustum := cam.Frustum()

	// Brute force reference count.
	bruteCount := 0
	for i := range boxes {
		if frustum.IntersectsAABB(boxes[i]) {
			bruteCount++
		}
	}
	if bruteCount == 0 || bruteCount > count/5 {
		t.Fatalf("unexpected bruteCount %d for 50k frustum test", bruteCount)
	}

	// Dynamic BVH query matches brute force and visits only a small fraction of nodes.
	buf := make([]scene3d.BVHHandle, 0, bruteCount+256)
	got := bvh.QueryFrustum(frustum, buf[:0])
	if len(got) != bruteCount {
		t.Fatalf("dynamic BVH QueryFrustum(50k) = %d hits, want %d", len(got), bruteCount)
	}
	totalBVHNodes := 2*count - 1
	dynVisited := bvh.LastQueryVisited()
	if dynVisited >= totalBVHNodes/10 {
		t.Fatalf("dynamic BVH visited %d / %d nodes (expected < 10%% pruning)", dynVisited, totalBVHNodes)
	}

	// SAH Rebuild improves pruning even further and still matches brute force exactly.
	bvh.Rebuild()
	got = bvh.QueryFrustum(frustum, buf[:0])
	if len(got) != bruteCount {
		t.Fatalf("rebuilt BVH QueryFrustum(50k) = %d hits, want %d", len(got), bruteCount)
	}
	sahVisited := bvh.LastQueryVisited()
	if sahVisited >= totalBVHNodes/10 {
		t.Fatalf("SAH BVH visited %d / %d nodes (expected < 10%% pruning)", sahVisited, totalBVHNodes)
	}

	// Verify 0 allocations on slice-reusing queries.
	var rayHits []scene3d.BVHRayHit
	rayHits = make([]scene3d.BVHRayHit, 0, 256)
	ray := math3d.Ray{Origin: math3d.Vec3{X: 0, Y: 10, Z: 200}, Dir: math3d.Vec3Forward()}
	region := math3d.AABB{
		Min: math3d.Vec3{X: -20, Y: -20, Z: -20},
		Max: math3d.Vec3{X: 20, Y: 20, Z: 20},
	}

	if allocs := testing.AllocsPerRun(50, func() {
		buf = bvh.QueryFrustum(frustum, buf[:0])
	}); allocs != 0 {
		t.Fatalf("QueryFrustum allocated %v times/op, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(50, func() {
		buf = bvh.QueryAABB(region, buf[:0])
	}); allocs != 0 {
		t.Fatalf("QueryAABB allocated %v times/op, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(50, func() {
		buf = bvh.QuerySphere(math3d.Vec3Zero(), 25, buf[:0])
	}); allocs != 0 {
		t.Fatalf("QuerySphere allocated %v times/op, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(50, func() {
		rayHits = bvh.RaycastAll(ray, 500, rayHits[:0])
	}); allocs != 0 {
		t.Fatalf("RaycastAll allocated %v times/op, want 0", allocs)
	}
}

func build50kBVH(rebuild bool) (*scene3d.BVH, []scene3d.BVHHandle, []math3d.AABB, math3d.Frustum) {
	const count = 50_000
	rng := rand.New(rand.NewPCG(50000, 1))
	bvh := scene3d.NewBVH()
	handles := make([]scene3d.BVHHandle, count)
	boxes := make([]math3d.AABB, count)
	for i := range count {
		cx := (rng.Float32()*2 - 1) * 500
		cy := (rng.Float32()*2 - 1) * 100
		cz := (rng.Float32()*2 - 1) * 500
		hx := 0.5 + rng.Float32()*1.5
		hy := 0.5 + rng.Float32()*1.5
		hz := 0.5 + rng.Float32()*1.5
		b := math3d.AABB{
			Min: math3d.Vec3{X: cx - hx, Y: cy - hy, Z: cz - hz},
			Max: math3d.Vec3{X: cx + hx, Y: cy + hy, Z: cz + hz},
		}
		boxes[i] = b
		handles[i] = bvh.Insert(uint64(i), b, nil)
	}
	if rebuild {
		bvh.Rebuild()
	}
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 10, Z: 200}, math3d.DegToRad(50), 16.0/9.0, 1.0, 120.0)
	return bvh, handles, boxes, cam.Frustum()
}

func BenchmarkBVHCull50k(b *testing.B) {
	b.Run("Dynamic", func(b *testing.B) {
		bvh, _, _, frustum := build50kBVH(false)
		buf := make([]scene3d.BVHHandle, 0, 2048)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			buf = bvh.QueryFrustum(frustum, buf[:0])
		}
		if len(buf) == 0 {
			b.Fatal("expected hits")
		}
	})

	b.Run("SAHRebuilt", func(b *testing.B) {
		bvh, _, _, frustum := build50kBVH(true)
		buf := make([]scene3d.BVHHandle, 0, 2048)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			buf = bvh.QueryFrustum(frustum, buf[:0])
		}
		if len(buf) == 0 {
			b.Fatal("expected hits")
		}
	})

	b.Run("SceneCullFrustumInto", func(b *testing.B) {
		const count = 50_000
		rng := rand.New(rand.NewPCG(50000, 2))
		sc := scene3d.NewScene()
		box := unitBox()
		for range count {
			n := scene3d.NewNode("")
			n.SetPosition(math3d.Vec3{
				X: (rng.Float32()*2 - 1) * 500,
				Y: (rng.Float32()*2 - 1) * 100,
				Z: (rng.Float32()*2 - 1) * 500,
			})
			n.SetLocalBounds(box)
			_ = sc.Add(n)
		}
		sc.RebuildBVH()
		cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 10, Z: 200}, math3d.DegToRad(50), 16.0/9.0, 1.0, 120.0)
		frustum := cam.Frustum()
		out := make([]*scene3d.Node, 0, 2048)
		// Warm up scratch buffer.
		out = sc.CullFrustumInto(frustum, out[:0])

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			out = sc.CullFrustumInto(frustum, out[:0])
		}
		if len(out) == 0 {
			b.Fatal("expected hits")
		}
	})

	b.Run("RaycastAll", func(b *testing.B) {
		bvh, _, _, _ := build50kBVH(true)
		ray := math3d.Ray{Origin: math3d.Vec3{X: 0, Y: 10, Z: 500}, Dir: math3d.Vec3Forward()}
		hits := make([]scene3d.BVHRayHit, 0, 256)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			hits = bvh.RaycastAll(ray, 1000, hits[:0])
		}
	})

	b.Run("IncrementalMove100AndCull", func(b *testing.B) {
		bvh, handles, boxes, frustum := build50kBVH(true)
		buf := make([]scene3d.BVHHandle, 0, 2048)
		delta := math3d.Vec3{X: 0.5, Y: 0.1, Z: -0.25}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			for i := range 100 {
				boxes[i] = math3d.AABB{
					Min: boxes[i].Min.Add(delta),
					Max: boxes[i].Max.Add(delta),
				}
				bvh.Update(handles[i], boxes[i])
			}
			buf = bvh.QueryFrustum(frustum, buf[:0])
		}
	})
}

func build10kScene() (*scene3d.Scene, []*scene3d.Node) {
	// 100 groups * 100 nodes per group (nested 2 levels: 10 mid * 9 children + 10 mid = 100 each) = 10,000 nodes.
	sc := scene3d.NewScene()
	groups := make([]*scene3d.Node, 100)
	for g := range 100 {
		grp := scene3d.NewNode("")
		grp.SetPosition(math3d.Vec3{X: float32(g) * 5, Y: 0, Z: 0})
		_ = sc.Add(grp)
		groups[g] = grp

		for m := range 10 {
			mid := scene3d.NewNode("")
			mid.SetPosition(math3d.Vec3{X: 0, Y: float32(m), Z: 0})
			mid.SetRotation(math3d.QuatFromAxisAngle(math3d.Vec3Up(), float32(m)*0.1))
			_ = grp.AddChild(mid)
			for c := range 9 {
				leaf := scene3d.NewNode("")
				leaf.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: -float32(c + 1)})
				_ = mid.AddChild(leaf)
			}
		}
	}
	sc.UpdateTransforms()
	return sc, groups
}

func BenchmarkScene10kNodesUpdate(b *testing.B) {
	b.Run("FullDirty", func(b *testing.B) {
		sc, _ := build10kScene()
		root := sc.Root()
		var x float32
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			x += 0.01
			root.SetPosition(math3d.Vec3{X: x, Y: 0, Z: 0})
			sc.UpdateTransforms()
		}
	})

	b.Run("PartialDirty1Pct", func(b *testing.B) {
		sc, groups := build10kScene()
		var x float32
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			x += 0.01
			// 1 group = 100 nodes (~1% of 10,000 nodes).
			groups[0].SetPosition(math3d.Vec3{X: x, Y: 0, Z: 0})
			sc.UpdateTransforms()
		}
	})

	b.Run("CleanNoOp", func(b *testing.B) {
		sc, _ := build10kScene()
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if sc.UpdateTransforms() != 0 {
				b.Fatal("expected 0 dirty updates on clean scene")
			}
		}
	})
}

func TestBVHRejectsNonFiniteBoundsAndRays(t *testing.T) {
	// #546: NaN bounds used to poison the tree; NaN rays could report a hit.
	b := scene3d.NewBVH()
	box := math3d.AABB{Min: math3d.Vec3{X: -0.5, Y: -0.5, Z: -0.5}, Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}
	if h := b.Insert(1, box, nil); h == (scene3d.BVHHandle{}) {
		t.Fatal("finite insert returned an invalid handle")
	}
	nan := float32(math.NaN())
	bad := math3d.AABB{Min: math3d.Vec3{X: nan, Y: -0.5, Z: -0.5}, Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}
	if h := b.Insert(2, bad, nil); h != (scene3d.BVHHandle{}) {
		t.Fatal("non-finite insert returned a valid handle")
	}
	region := math3d.AABB{Min: math3d.Vec3{X: -2, Y: -2, Z: -2}, Max: math3d.Vec3{X: 2, Y: 2, Z: 2}}
	hits := b.QueryAABB(region, nil)
	if len(hits) != 1 {
		t.Fatalf("QueryAABB after rejected insert = %d hits, want 1", len(hits))
	}
	visited := 0
	b.Raycast(math3d.Ray{Origin: math3d.Vec3Zero(), Dir: math3d.Vec3{X: nan, Y: 0, Z: -1}}, 10, func(scene3d.BVHHandle, float32) bool {
		visited++
		return true
	})
	if visited != 0 {
		t.Fatalf("non-finite ray visited %d leaves, want 0", visited)
	}
}

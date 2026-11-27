package entities_test

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/entities"
)

func approxEq(a, b, eps float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= eps
}

func approxAngleEq(a, b, eps float32) bool {
	d := math.Mod(float64(a-b)+math.Pi, 2*math.Pi)
	if d < 0 {
		d += 2 * math.Pi
	}
	d -= math.Pi
	return math.Abs(d) <= float64(eps)
}

func assertNear(t *testing.T, label string, got, want, eps float32) {
	t.Helper()
	if !approxEq(got, want, eps) {
		t.Fatalf("%s = %v, want %v (eps=%v)", label, got, want, eps)
	}
}

func assertAngleNear(t *testing.T, label string, got, want, eps float32) {
	t.Helper()
	if !approxAngleEq(got, want, eps) {
		t.Fatalf("%s = %v rad, want %v rad (eps=%v)", label, got, want, eps)
	}
}

func TestTransform2DDefaultsAndHelpers(t *testing.T) {
	var zero entities.Transform2D
	if !zero.Visible() || zero.Hidden {
		t.Fatalf("zero Transform2D should be visible: Hidden=%v Visible=%v", zero.Hidden, zero.Visible())
	}
	if zero.ScaleX != 0 || zero.ScaleY != 0 {
		t.Fatalf("zero Transform2D scale = (%v, %v), want (0, 0)", zero.ScaleX, zero.ScaleY)
	}

	id := entities.IdentityTransform2D()
	if id.X != 0 || id.Y != 0 || id.Rotation != 0 || id.ScaleX != 1 || id.ScaleY != 1 || !id.Visible() {
		t.Fatalf("unexpected IdentityTransform2D: %+v", id)
	}

	tr := entities.NewTransform2D(12, -34)
	if tr.X != 12 || tr.Y != -34 || tr.ScaleX != 1 || tr.ScaleY != 1 || !tr.Visible() {
		t.Fatalf("unexpected NewTransform2D: %+v", tr)
	}
	tr.SetVisible(false)
	if tr.Visible() || !tr.Hidden {
		t.Fatalf("SetVisible(false) failed: %+v", tr)
	}
	tr.SetVisible(true)
	if !tr.Visible() || tr.Hidden {
		t.Fatalf("SetVisible(true) failed: %+v", tr)
	}
}

func TestHierarchyMultiLevelCompositionAndVisibility(t *testing.T) {
	w := entities.NewWorld()
	h := entities.NewHierarchy2D(w)

	root := w.Spawn()
	mid := w.Spawn()
	leaf := w.Spawn()

	// Root at (100, 50), rotated +90 deg (pi/2), non-uniform scale (2, 3).
	h.SetLocal(root, entities.Transform2D{
		X:        100,
		Y:        50,
		Rotation: float32(math.Pi / 2),
		ScaleX:   2,
		ScaleY:   3,
	})
	// Mid at local (10, 4), rotated 0, scale (0.5, 2).
	// Under root:
	//   root matrix maps (x, y) -> (-3*y + 100, 2*x + 50)
	//   so mid origin (10, 4) -> (100 - 12, 50 + 20) = (88, 70).
	//   mid world scale = (2*0.5, 3*2) = (1, 6), rotation = pi/2.
	h.SetLocal(mid, entities.Transform2D{
		X:      10,
		Y:      4,
		ScaleX: 0.5,
		ScaleY: 2,
	})
	// Leaf at local (5, -2), rotated +90 deg (pi/2), scale (2, 1).
	// Under mid (world matrix: a=0, b=1, c=-6, d=0, tx=88, ty=70):
	//   leaf origin (5, -2) -> (88 + (-6)*(-2), 70 + 1*5) = (100, 75).
	//   leaf local matrix: [0, 2, -1, 0, 5, -2]
	//   leaf world matrix = [0,1,-6,0,88,70] * [0,2,-1,0,5,-2]
	//     wa = 0*0 + (-6)*2 = -12
	//     wb = 1*0 + 0*2    = 0
	//     wc = 0*(-1) + (-6)*0 = 0
	//     wd = 1*(-1) + 0*0 = -1
	//   => rotation = pi, scaleX = 12, scaleY = 1 (det = 12 > 0).
	h.SetLocal(leaf, entities.Transform2D{
		X:        5,
		Y:        -2,
		Rotation: float32(math.Pi / 2),
		ScaleX:   2,
		ScaleY:   1,
	})

	if err := h.SetParent(mid, root, false); err != nil {
		t.Fatalf("SetParent(mid, root): %v", err)
	}
	if err := h.SetParent(leaf, mid, false); err != nil {
		t.Fatalf("SetParent(leaf, mid): %v", err)
	}

	if updated := h.Sync(); updated != 3 {
		t.Fatalf("initial Sync() = %d, want 3", updated)
	}

	// Verify mid world transform.
	midWT, ok := h.WorldTransform(mid)
	if !ok {
		t.Fatalf("WorldTransform(mid) not found")
	}
	assertNear(t, "mid.X", midWT.X, 88, 1e-4)
	assertNear(t, "mid.Y", midWT.Y, 70, 1e-4)
	assertAngleNear(t, "mid.Rotation", midWT.Rotation, float32(math.Pi/2), 1e-4)
	assertNear(t, "mid.ScaleX", midWT.ScaleX, 1, 1e-4)
	assertNear(t, "mid.ScaleY", midWT.ScaleY, 6, 1e-4)
	if !midWT.WorldVisible {
		t.Fatalf("mid.WorldVisible = false, want true")
	}

	// Verify leaf world transform and point round-trip.
	leafWT, ok := h.WorldTransform(leaf)
	if !ok {
		t.Fatalf("WorldTransform(leaf) not found")
	}
	assertNear(t, "leaf.X", leafWT.X, 100, 1e-4)
	assertNear(t, "leaf.Y", leafWT.Y, 75, 1e-4)
	assertAngleNear(t, "leaf.Rotation", leafWT.Rotation, float32(math.Pi), 1e-4)
	assertNear(t, "leaf.ScaleX", leafWT.ScaleX, 12, 1e-4)
	assertNear(t, "leaf.ScaleY", leafWT.ScaleY, 1, 1e-4)
	if !leafWT.WorldVisible {
		t.Fatalf("leaf.WorldVisible = false, want true")
	}

	// TransformPoint & InverseTransformPoint round-trip, including non-uniform scaled + rotated shear case.
	wx, wy := leafWT.TransformPoint(3, 7)
	assertNear(t, "TransformPoint.X", wx, 100-12*3, 1e-4)
	assertNear(t, "TransformPoint.Y", wy, 75-1*7, 1e-4)
	lx, ly := leafWT.InverseTransformPoint(wx, wy)
	assertNear(t, "InverseTransformPoint.X", lx, 3, 1e-4)
	assertNear(t, "InverseTransformPoint.Y", ly, 7, 1e-4)

	// Verify ECS query visibility of cached WorldTransform2D.
	queried := 0
	entities.Each1[entities.WorldTransform2D](w, func(_ entities.Entity, _ *entities.WorldTransform2D) bool {
		queried++
		return true
	})
	if queried != 3 {
		t.Fatalf("Each1[WorldTransform2D] visited %d entities, want 3", queried)
	}

	// Verify sheared matrix TransformPoint / InverseTransformPoint exact round-trip.
	h.SetRotation(mid, float32(math.Pi/4))
	h.Sync()
	sheared, _ := h.WorldTransform(leaf)
	swx, swy := sheared.TransformPoint(-4.5, 9.25)
	slx, sly := sheared.InverseTransformPoint(swx, swy)
	assertNear(t, "sheared InverseTransformPoint.X", slx, -4.5, 1e-4)
	assertNear(t, "sheared InverseTransformPoint.Y", sly, 9.25, 1e-4)

	// Singular matrix returns (0, 0) safely.
	singular := entities.WorldTransform2D{Matrix: [6]float32{0, 0, 0, 1, 10, 20}}
	zlx, zly := singular.InverseTransformPoint(15, 25)
	if zlx != 0 || zly != 0 || math.IsNaN(float64(zlx)) || math.IsNaN(float64(zly)) {
		t.Fatalf("singular InverseTransformPoint = (%v, %v), want (0, 0)", zlx, zly)
	}

	// Visibility inheritance: hiding mid hides leaf even though leaf local Visible is true.
	h.SetVisible(mid, false)
	if updated := h.Sync(); updated != 2 {
		t.Fatalf("Sync after hiding mid = %d, want 2", updated)
	}
	rWT, _ := h.WorldTransform(root)
	mWT, _ := h.WorldTransform(mid)
	lWT, _ := h.WorldTransform(leaf)
	if !rWT.WorldVisible || mWT.WorldVisible || lWT.WorldVisible {
		t.Fatalf("visibility = (%v, %v, %v), want (true, false, false)", rWT.WorldVisible, mWT.WorldVisible, lWT.WorldVisible)
	}
	if lp, _ := h.Local(leaf); !lp.Visible() {
		t.Fatalf("leaf local Visible should remain true when parent is hidden")
	}

	// Hiding root hides everything; unhiding root leaves mid and leaf still hidden.
	h.SetVisible(root, false)
	h.Sync()
	if wt, _ := h.WorldTransform(root); wt.WorldVisible {
		t.Fatalf("root should be hidden")
	}
	h.SetVisible(root, true)
	h.Sync()
	rWT, _ = h.WorldTransform(root)
	mWT, _ = h.WorldTransform(mid)
	lWT, _ = h.WorldTransform(leaf)
	if !rWT.WorldVisible || mWT.WorldVisible || lWT.WorldVisible {
		t.Fatalf("after unhiding root: visibility = (%v, %v, %v), want (true, false, false)", rWT.WorldVisible, mWT.WorldVisible, lWT.WorldVisible)
	}

	// Unhiding mid makes mid and leaf visible again.
	h.SetVisible(mid, true)
	h.Sync()
	mWT, _ = h.WorldTransform(mid)
	lWT, _ = h.WorldTransform(leaf)
	if !mWT.WorldVisible || !lWT.WorldVisible {
		t.Fatalf("after unhiding mid: visibility = (%v, %v), want (true, true)", mWT.WorldVisible, lWT.WorldVisible)
	}
}

func TestHierarchyDirtyPropagationAndLazyResolution(t *testing.T) {
	w := entities.NewWorld()
	h := w.Hierarchy2D()

	root := w.Spawn()
	branchA := w.Spawn()
	leafA1 := w.Spawn()
	leafA2 := w.Spawn()
	branchB := w.Spawn()
	leafB1 := w.Spawn()
	leafB2 := w.Spawn()

	for _, e := range []entities.Entity{root, branchA, leafA1, leafA2, branchB, leafB1, leafB2} {
		h.SetLocal(e, entities.NewTransform2D(1, 0))
	}
	_ = h.SetParent(branchA, root, false)
	_ = h.SetParent(leafA1, branchA, false)
	_ = h.SetParent(leafA2, branchA, false)
	_ = h.SetParent(branchB, root, false)
	_ = h.SetParent(leafB1, branchB, false)
	_ = h.SetParent(leafB2, branchB, false)

	// Initial Sync updates all 7 nodes.
	if got := h.Sync(); got != 7 {
		t.Fatalf("initial Sync = %d, want 7", got)
	}
	// Clean Sync updates 0 nodes.
	if got := h.Sync(); got != 0 {
		t.Fatalf("clean Sync = %d, want 0", got)
	}

	// Modifying branchA marks ONLY branchA, leafA1, leafA2 dirty.
	h.SetPosition(branchA, 10, 0)
	if h.IsDirty(root) || h.IsDirty(branchB) || h.IsDirty(leafB1) || h.IsDirty(leafB2) {
		t.Fatalf("unrelated branch was dirtied")
	}
	if !h.IsDirty(branchA) || !h.IsDirty(leafA1) || !h.IsDirty(leafA2) {
		t.Fatalf("branchA subtree should be dirty")
	}
	if got := h.Sync(); got != 3 {
		t.Fatalf("branchA Sync = %d, want 3", got)
	}
	if got := h.Sync(); got != 0 {
		t.Fatalf("second Sync = %d, want 0", got)
	}

	// Direct component mutation + MarkDirty on a single leaf updates only that leaf.
	if p, ok := entities.Get[entities.Transform2D](w, leafB1); ok {
		p.X = 50
	}
	h.MarkDirty(leafB1)
	if got := h.Sync(); got != 1 {
		t.Fatalf("leafB1 Sync = %d, want 1", got)
	}
	if wt, _ := h.WorldTransform(leafB1); wt.X != 52 {
		t.Fatalf("leafB1 world X = %v, want 52", wt.X)
	}

	// Lazy WorldTransform resolution on one branch while whole tree is dirty.
	h.SetPosition(root, 100, 0)
	wtA1, ok := h.WorldTransform(leafA1)
	if !ok {
		t.Fatalf("WorldTransform(leafA1) failed")
	}
	// root(100) + branchA(10) + leafA1(1) = 111.
	assertNear(t, "lazy leafA1.X", wtA1.X, 111, 1e-4)

	// root, branchA, leafA1 are now clean; leafA2, branchB, leafB1, leafB2 remain dirty (4 nodes).
	if h.IsDirty(root) || h.IsDirty(branchA) || h.IsDirty(leafA1) {
		t.Fatalf("lazily resolved path should be clean")
	}
	if !h.IsDirty(leafA2) || !h.IsDirty(branchB) || !h.IsDirty(leafB1) || !h.IsDirty(leafB2) {
		t.Fatalf("sibling branches should remain dirty until Sync")
	}
	if got := h.Sync(); got != 4 {
		t.Fatalf("remaining Sync after lazy resolution = %d, want 4", got)
	}
	if wtB2, _ := h.WorldTransform(leafB2); wtB2.X != 102 {
		t.Fatalf("leafB2 world X = %v, want 102", wtB2.X)
	}

	// Re-dirtying a node that was lazily resolved before Sync must still be updated by Sync.
	h.SetPosition(root, 200, 0)
	_, _ = h.WorldTransform(branchA) // cleans root & branchA; promotes leafA1, leafA2, branchB
	h.SetPosition(branchA, 20, 0)    // re-dirties branchA before Sync
	if got := h.Sync(); got != 6 {   // branchA, leafA1, leafA2, branchB, leafB1, leafB2
		t.Fatalf("Sync after re-dirtying lazily resolved branchA = %d, want 6", got)
	}
	if wtA1, _ := h.WorldTransform(leafA1); wtA1.X != 221 {
		t.Fatalf("leafA1 world X after re-dirty = %v, want 221", wtA1.X)
	}
}

func TestHierarchyReparentKeepWorldAndDetach(t *testing.T) {
	w := entities.NewWorld()
	h := entities.NewHierarchy2D(w)

	parent1 := w.Spawn()
	parent2 := w.Spawn()
	child := w.Spawn()
	grand := w.Spawn()

	h.SetLocal(parent1, entities.Transform2D{
		X:        50,
		Y:        -20,
		Rotation: 0.65,
		ScaleX:   2,
		ScaleY:   2,
	})
	h.SetLocal(parent2, entities.Transform2D{
		X:        -120,
		Y:        80,
		Rotation: -1.1,
		ScaleX:   0.5,
		ScaleY:   0.5,
	})
	h.SetLocal(child, entities.Transform2D{
		X:        12,
		Y:        -7,
		Rotation: 0.4,
		ScaleX:   1.5,
		ScaleY:   0.75,
	})
	h.SetLocal(grand, entities.Transform2D{
		X:        3,
		Y:        4,
		Rotation: -0.25,
		ScaleX:   2,
		ScaleY:   1.25,
	})

	_ = h.SetParent(child, parent1, false)
	_ = h.SetParent(grand, child, false)
	h.Sync()

	beforeChild, _ := h.WorldTransform(child)
	beforeGrand, _ := h.WorldTransform(grand)
	bx, by := beforeChild.TransformPoint(5, -3)

	// Reparent child from parent1 to parent2 preserving world transform.
	if err := h.SetParent(child, parent2, true); err != nil {
		t.Fatalf("SetParent keepWorld: %v", err)
	}
	if p, ok := h.Parent(child); !ok || p != parent2 {
		t.Fatalf("Parent(child) = (%v, %v), want (%v, true)", p, ok, parent2)
	}
	if h.ChildCount(parent1) != 0 || h.ChildCount(parent2) != 1 {
		t.Fatalf("child counts = (%d, %d), want (0, 1)", h.ChildCount(parent1), h.ChildCount(parent2))
	}

	h.Sync()
	afterChild, _ := h.WorldTransform(child)
	afterGrand, _ := h.WorldTransform(grand)

	assertNear(t, "keepWorld child.X", afterChild.X, beforeChild.X, 1e-3)
	assertNear(t, "keepWorld child.Y", afterChild.Y, beforeChild.Y, 1e-3)
	assertAngleNear(t, "keepWorld child.Rotation", afterChild.Rotation, beforeChild.Rotation, 1e-3)
	assertNear(t, "keepWorld child.ScaleX", afterChild.ScaleX, beforeChild.ScaleX, 1e-3)
	assertNear(t, "keepWorld child.ScaleY", afterChild.ScaleY, beforeChild.ScaleY, 1e-3)
	ax, ay := afterChild.TransformPoint(5, -3)
	assertNear(t, "keepWorld TransformPoint.X", ax, bx, 1e-3)
	assertNear(t, "keepWorld TransformPoint.Y", ay, by, 1e-3)

	assertNear(t, "keepWorld grand.X", afterGrand.X, beforeGrand.X, 1e-3)
	assertNear(t, "keepWorld grand.Y", afterGrand.Y, beforeGrand.Y, 1e-3)
	assertAngleNear(t, "keepWorld grand.Rotation", afterGrand.Rotation, beforeGrand.Rotation, 1e-3)

	// Detach with keepWorld = true preserves world transform at root.
	h.Detach(child, true)
	if p, ok := h.Parent(child); ok || p != entities.Zero {
		t.Fatalf("after Detach Parent(child) = (%v, %v), want (Zero, false)", p, ok)
	}
	h.Sync()
	detachedChild, _ := h.WorldTransform(child)
	assertNear(t, "Detach keepWorld X", detachedChild.X, beforeChild.X, 1e-3)
	assertNear(t, "Detach keepWorld Y", detachedChild.Y, beforeChild.Y, 1e-3)
	assertAngleNear(t, "Detach keepWorld Rotation", detachedChild.Rotation, beforeChild.Rotation, 1e-3)
	assertNear(t, "Detach keepWorld ScaleX", detachedChild.ScaleX, beforeChild.ScaleX, 1e-3)
	assertNear(t, "Detach keepWorld ScaleY", detachedChild.ScaleY, beforeChild.ScaleY, 1e-3)

	// Reparent with keepWorld = false keeps local Transform2D intact.
	h.SetLocal(child, entities.NewTransform2D(10, 20))
	if err := h.SetParent(child, parent1, false); err != nil {
		t.Fatalf("SetParent keepWorld=false: %v", err)
	}
	if loc, _ := h.Local(child); loc.X != 10 || loc.Y != 20 {
		t.Fatalf("keepWorld=false mutated local transform: %+v", loc)
	}
	// Detach with keepWorld = false keeps local Transform2D intact.
	h.Detach(child, false)
	h.Sync()
	if wt, _ := h.WorldTransform(child); wt.X != 10 || wt.Y != 20 {
		t.Fatalf("Detach keepWorld=false world = (%v, %v), want (10, 20)", wt.X, wt.Y)
	}

	// Reparent with keepWorld = true onto singular (zero-scale) parent produces no NaN/Inf.
	zeroParent := w.Spawn()
	h.SetLocal(zeroParent, entities.Transform2D{X: 10, Y: 20, ScaleX: 0, ScaleY: 0})
	if err := h.SetParent(child, zeroParent, true); err != nil {
		t.Fatalf("SetParent onto zero-scale parent: %v", err)
	}
	if loc, _ := h.Local(child); math.IsNaN(float64(loc.X)) || math.IsInf(float64(loc.X), 0) || math.IsNaN(float64(loc.ScaleX)) {
		t.Fatalf("singular reparent produced invalid float: %+v", loc)
	}
}

func TestHierarchyDeterministicChildOrder(t *testing.T) {
	w := entities.NewWorld()
	h := entities.NewHierarchy2D(w)

	p := w.Spawn()
	c1, c2, c3, c4 := w.Spawn(), w.Spawn(), w.Spawn(), w.Spawn()

	for _, c := range []entities.Entity{c1, c2, c3, c4} {
		if err := h.SetParent(c, p, false); err != nil {
			t.Fatalf("SetParent: %v", err)
		}
	}
	want := []entities.Entity{c1, c2, c3, c4}
	if got := h.Children(p); !slices.Equal(got, want) {
		t.Fatalf("Children = %v, want %v", got, want)
	}

	// Reparenting c2 to the same parent is a no-op and preserves order.
	h.Sync()
	if err := h.SetParent(c2, p, false); err != nil {
		t.Fatalf("SetParent same parent: %v", err)
	}
	if got := h.Children(p); !slices.Equal(got, want) {
		t.Fatalf("Children after no-op SetParent = %v, want %v", got, want)
	}
	if got := h.Sync(); got != 0 {
		t.Fatalf("no-op SetParent dirtied %d nodes, want 0", got)
	}

	// Detaching middle child preserves remaining sibling order.
	h.Detach(c2, false)
	wantAfter := []entities.Entity{c1, c3, c4}
	var iterated []entities.Entity
	h.EachChild(p, func(e entities.Entity) bool {
		iterated = append(iterated, e)
		return true
	})
	if !slices.Equal(iterated, wantAfter) {
		t.Fatalf("EachChild after detach = %v, want %v", iterated, wantAfter)
	}
}

func TestHierarchyCycleAndDeadEntityRejection(t *testing.T) {
	w := entities.NewWorld()
	h := entities.NewHierarchy2D(w)

	a := w.Spawn()
	b := w.Spawn()
	c := w.Spawn()

	// Self-parent rejected.
	if err := h.SetParent(a, a, false); !errors.Is(err, entities.ErrCycle) {
		t.Fatalf("self-parent err = %v, want ErrCycle", err)
	}

	if err := h.SetParent(b, a, false); err != nil {
		t.Fatalf("SetParent(b, a): %v", err)
	}
	if err := h.SetParent(c, b, false); err != nil {
		t.Fatalf("SetParent(c, b): %v", err)
	}

	// Ancestor-to-descendant cycle rejected without mutating links.
	if err := h.SetParent(a, c, false); !errors.Is(err, entities.ErrCycle) {
		t.Fatalf("cycle SetParent(a, c) err = %v, want ErrCycle", err)
	}
	if err := h.SetParent(a, b, false); !errors.Is(err, entities.ErrCycle) {
		t.Fatalf("cycle SetParent(a, b) err = %v, want ErrCycle", err)
	}
	if _, ok := h.Parent(a); ok {
		t.Fatalf("failed cycle SetParent mutated a's parent")
	}

	// Dead and Zero entities rejected.
	dead := w.Spawn()
	w.Destroy(dead)
	if err := h.SetParent(entities.Zero, a, false); !errors.Is(err, entities.ErrDeadEntity) {
		t.Fatalf("Zero child err = %v, want ErrDeadEntity", err)
	}
	if err := h.SetParent(dead, a, false); !errors.Is(err, entities.ErrDeadEntity) {
		t.Fatalf("dead child err = %v, want ErrDeadEntity", err)
	}
	if err := h.SetParent(a, dead, false); !errors.Is(err, entities.ErrDeadEntity) {
		t.Fatalf("dead parent err = %v, want ErrDeadEntity", err)
	}

	// SetParent(c, Zero, false) detaches c cleanly.
	if err := h.SetParent(c, entities.Zero, false); err != nil {
		t.Fatalf("SetParent(c, Zero): %v", err)
	}
	if _, ok := h.Parent(c); ok {
		t.Fatalf("c should be detached")
	}

	// Disposed world rejected.
	w.Dispose()
	if err := h.SetParent(b, a, false); !errors.Is(err, entities.ErrWorldDisposed) {
		t.Fatalf("disposed SetParent err = %v, want ErrWorldDisposed", err)
	}
}

func TestHierarchyRemovePoliciesAndNoDanglingPointers(t *testing.T) {
	t.Run("RemoveCascade", func(t *testing.T) {
		w := entities.NewWorld()
		h := entities.NewHierarchy2D(w)

		root := w.Spawn()
		parent := w.Spawn()
		sibling := w.Spawn()
		child1 := w.Spawn()
		child2 := w.Spawn()
		grand := w.Spawn()

		_ = h.SetParent(parent, root, false)
		_ = h.SetParent(sibling, root, false)
		_ = h.SetParent(child1, parent, false)
		_ = h.SetParent(child2, parent, false)
		_ = h.SetParent(grand, child1, false)
		h.Sync()

		h.DestroyWithPolicy(parent, entities.RemoveCascade)

		for _, dead := range []entities.Entity{parent, child1, child2, grand} {
			if w.Alive(dead) {
				t.Fatalf("entity %v should be destroyed by RemoveCascade", dead)
			}
			if _, ok := h.Parent(dead); ok {
				t.Fatalf("destroyed entity %v still reports a parent", dead)
			}
		}
		if !w.Alive(root) || !w.Alive(sibling) {
			t.Fatalf("root and sibling must stay alive")
		}
		if got := h.Children(root); !slices.Equal(got, []entities.Entity{sibling}) {
			t.Fatalf("root children after cascade = %v, want [%v]", got, sibling)
		}
		if w.Count() != 2 {
			t.Fatalf("World.Count = %d, want 2", w.Count())
		}

		// Slot recycling after cascade must not inherit stale links.
		fresh := w.Spawn()
		h.SetLocal(fresh, entities.NewTransform2D(7, 8))
		if _, ok := h.Parent(fresh); ok {
			t.Fatalf("recycled entity %v inherited stale parent", fresh)
		}
		if h.ChildCount(fresh) != 0 {
			t.Fatalf("recycled entity %v inherited stale children", fresh)
		}
		h.Sync()
		if wt, ok := h.WorldTransform(fresh); !ok || wt.X != 7 || wt.Y != 8 {
			t.Fatalf("recycled entity world = (%+v, %v), want (7, 8)", wt, ok)
		}
	})

	t.Run("RemoveDetach", func(t *testing.T) {
		w := entities.NewWorld()
		h := entities.NewHierarchy2D(w)

		root := w.Spawn()
		parent := w.Spawn()
		child := w.Spawn()
		grand := w.Spawn()

		h.SetLocal(root, entities.NewTransform2D(100, 0))
		h.SetLocal(parent, entities.NewTransform2D(50, 0))
		h.SetLocal(child, entities.NewTransform2D(10, 5))
		h.SetLocal(grand, entities.NewTransform2D(2, 3))

		_ = h.SetParent(parent, root, false)
		_ = h.SetParent(child, parent, false)
		_ = h.SetParent(grand, child, false)
		h.Sync()

		h.DestroyWithPolicy(parent, entities.RemoveDetach)

		if w.Alive(parent) {
			t.Fatalf("parent should be destroyed")
		}
		if !w.Alive(root) || !w.Alive(child) || !w.Alive(grand) {
			t.Fatalf("root, child, grand must stay alive")
		}
		if h.ChildCount(root) != 0 {
			t.Fatalf("root should have 0 children, got %d", h.ChildCount(root))
		}
		if _, ok := h.Parent(child); ok {
			t.Fatalf("detached child should have no parent")
		}
		if p, ok := h.Parent(grand); !ok || p != child {
			t.Fatalf("grand parent = (%v, %v), want (%v, true)", p, ok, child)
		}
		h.Sync()
		cWT, _ := h.WorldTransform(child)
		gWT, _ := h.WorldTransform(grand)
		assertNear(t, "detached child.X", cWT.X, 10, 1e-4)
		assertNear(t, "detached child.Y", cWT.Y, 5, 1e-4)
		assertNear(t, "detached grand.X", gWT.X, 12, 1e-4)
		assertNear(t, "detached grand.Y", gWT.Y, 8, 1e-4)
	})

	t.Run("RemoveDetachKeepWorld", func(t *testing.T) {
		w := entities.NewWorld()
		h := entities.NewHierarchy2D(w)

		parent := w.Spawn()
		child := w.Spawn()
		grand := w.Spawn()

		h.SetLocal(parent, entities.Transform2D{X: 100, Y: 200, Rotation: float32(math.Pi / 2), ScaleX: 2, ScaleY: 2})
		h.SetLocal(child, entities.Transform2D{X: 10, Y: 0, ScaleX: 1, ScaleY: 1})
		h.SetLocal(grand, entities.Transform2D{X: 5, Y: 0, ScaleX: 1, ScaleY: 1})
		_ = h.SetParent(child, parent, false)
		_ = h.SetParent(grand, child, false)
		h.Sync()

		beforeC, _ := h.WorldTransform(child)
		beforeG, _ := h.WorldTransform(grand)

		h.DestroyWithPolicy(parent, entities.RemoveDetachKeepWorld)
		if w.Alive(parent) || !w.Alive(child) || !w.Alive(grand) {
			t.Fatalf("unexpected liveness after RemoveDetachKeepWorld")
		}
		h.Sync()
		afterC, _ := h.WorldTransform(child)
		afterG, _ := h.WorldTransform(grand)
		assertNear(t, "child.X", afterC.X, beforeC.X, 1e-3)
		assertNear(t, "child.Y", afterC.Y, beforeC.Y, 1e-3)
		assertAngleNear(t, "child.Rotation", afterC.Rotation, beforeC.Rotation, 1e-3)
		assertNear(t, "grand.X", afterG.X, beforeG.X, 1e-3)
		assertNear(t, "grand.Y", afterG.Y, beforeG.Y, 1e-3)
	})

	t.Run("WorldDestroyHooksDefaultPolicy", func(t *testing.T) {
		w := entities.NewWorld()
		h := entities.NewHierarchy2D(w)

		// Default policy is RemoveDetach.
		p1 := w.Spawn()
		c1 := w.Spawn()
		h.SetLocal(p1, entities.NewTransform2D(10, 0))
		h.SetLocal(c1, entities.NewTransform2D(5, 0))
		_ = h.SetParent(c1, p1, false)
		h.Sync()

		w.Destroy(p1)
		if !w.Alive(c1) {
			t.Fatalf("default World.Destroy should detach child, not destroy it")
		}
		if _, ok := h.Parent(c1); ok {
			t.Fatalf("c1 still has dangling parent after World.Destroy(p1)")
		}
		h.Sync()
		if wt, _ := h.WorldTransform(c1); wt.X != 5 {
			t.Fatalf("c1 world X after parent World.Destroy = %v, want 5", wt.X)
		}

		// Configurable to RemoveCascade.
		h.SetRemovePolicy(entities.RemoveCascade)
		p2 := w.Spawn()
		c2 := w.Spawn()
		_ = h.SetParent(c2, p2, false)
		w.Destroy(p2)
		if w.Alive(p2) || w.Alive(c2) {
			t.Fatalf("World.Destroy with RemoveCascade should destroy both parent and child")
		}
	})

	t.Run("StaleDirtyRootDoesNotCorruptRecycledSlot", func(t *testing.T) {
		w := entities.NewWorld()
		h := entities.NewHierarchy2D(w)

		e1 := w.Spawn()
		h.SetLocal(e1, entities.NewTransform2D(999, 999)) // queued in dirtyRoots
		w.Destroy(e1)

		e2 := w.Spawn() // recycles e1's slot with bumped generation
		h.SetLocal(e2, entities.NewTransform2D(1, 2))
		if updated := h.Sync(); updated != 1 {
			t.Fatalf("Sync with stale dirty root = %d, want 1", updated)
		}
		if wt, ok := h.WorldTransform(e2); !ok || wt.X != 1 || wt.Y != 2 {
			t.Fatalf("e2 world = (%+v, %v), want (1, 2)", wt, ok)
		}
	})
}

func TestHierarchySteadyStateZeroAllocs(t *testing.T) {
	w := entities.NewWorld()
	h := entities.NewHierarchy2D(w)

	root := w.Spawn()
	child := w.Spawn()
	leaf := w.Spawn()
	h.SetLocal(root, entities.NewTransform2D(1, 2))
	h.SetLocal(child, entities.NewTransform2D(3, 4))
	h.SetLocal(leaf, entities.NewTransform2D(5, 6))
	_ = h.SetParent(child, root, false)
	_ = h.SetParent(leaf, child, false)
	h.Sync()

	allocs := testing.AllocsPerRun(200, func() {
		h.SetPosition(root, 10, 20)
		h.SetRotation(child, 0.5)
		if updated := h.Sync(); updated != 3 {
			t.Fatalf("updated = %d, want 3", updated)
		}
		if _, ok := h.WorldTransform(leaf); !ok {
			t.Fatalf("WorldTransform(leaf) missing")
		}
	})
	if allocs != 0 {
		t.Fatalf("steady-state update + Sync allocs = %v, want 0", allocs)
	}
}

func buildHierarchy10k(b *testing.B) (*entities.World, *entities.Hierarchy2D, []entities.Entity, entities.Entity) {
	b.Helper()
	// 100 roots * 10 branches * 9 leaves + 100 roots + 1000 branches = 10,000 nodes:
	// Specifically: 100 roots, each with 9 branches, each branch with 10 leaves = 100 + 900 + 9000 = 10,000 nodes!
	w := entities.NewWorld()
	h := entities.NewHierarchy2D(w)

	roots := make([]entities.Entity, 0, 100)
	var sampleBranch entities.Entity
	total := 0
	for r := 0; r < 100; r++ {
		root := w.Spawn()
		h.SetLocal(root, entities.Transform2D{
			X:        float32(r * 10),
			Y:        float32(r * 5),
			Rotation: 0.1,
			ScaleX:   1,
			ScaleY:   1,
		})
		roots = append(roots, root)
		total++
		for br := 0; br < 9; br++ {
			branch := w.Spawn()
			h.SetLocal(branch, entities.Transform2D{
				X:        float32(br + 1),
				Y:        float32(br - 2),
				Rotation: 0.25,
				ScaleX:   1.1,
				ScaleY:   0.9,
			})
			_ = h.SetParent(branch, root, false)
			if sampleBranch == entities.Zero {
				sampleBranch = branch
			}
			total++
			for lf := 0; lf < 10; lf++ {
				leaf := w.Spawn()
				h.SetLocal(leaf, entities.Transform2D{
					X:        float32(lf),
					Y:        float32(-lf),
					Rotation: 0.05,
					ScaleX:   1,
					ScaleY:   1,
				})
				_ = h.SetParent(leaf, branch, false)
				total++
			}
		}
	}
	if total != 10_000 {
		b.Fatalf("built %d nodes, want 10000", total)
	}
	if updated := h.Sync(); updated != 10_000 {
		b.Fatalf("warmup Sync = %d, want 10000", updated)
	}
	return w, h, roots, sampleBranch
}

func BenchmarkHierarchySync10k(b *testing.B) {
	_, h, roots, _ := buildHierarchy10k(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x := float32(i)
		for _, r := range roots {
			h.SetPosition(r, x, 0)
		}
		if updated := h.Sync(); updated != 10_000 {
			b.Fatalf("Sync = %d, want 10000", updated)
		}
	}
}

func BenchmarkHierarchyPartialDirty10k(b *testing.B) {
	_, h, _, sampleBranch := buildHierarchy10k(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.SetPosition(sampleBranch, float32(i), 1)
		if updated := h.Sync(); updated != 11 {
			b.Fatalf("Sync = %d, want 11", updated)
		}
	}
}

func TestHierarchyKeepWorldRefusesShear(t *testing.T) {
	// #546: a non-uniformly scaled rotated parent can require a sheared local
	// transform; decomposing it into TRS silently moved the child.
	w := entities.NewWorld()
	h := entities.NewHierarchy2D(w)
	parent := w.Spawn()
	child := w.Spawn()
	h.SetLocal(parent, entities.Transform2D{Rotation: 0.7, ScaleX: 3, ScaleY: 1})
	h.SetLocal(child, entities.Transform2D{Rotation: 0.4, ScaleX: 1, ScaleY: 1})
	if err := h.SetParent(child, parent, true); !errors.Is(err, entities.ErrNotDecomposable) {
		t.Fatalf("SetParent keepWorld = %v, want ErrNotDecomposable", err)
	}
	if p, ok := h.Parent(child); ok {
		t.Fatalf("child was reparented to %v despite the refusal", p)
	}
	if got, _ := h.Local(child); got.Rotation != 0.4 || got.ScaleX != 1 || got.ScaleY != 1 {
		t.Fatalf("local transform changed to %+v", got)
	}
}

func TestGenericSetOfTransform2DMarksSubtreeDirty(t *testing.T) {
	w := entities.NewWorld()
	h := w.Hierarchy2D()
	parent, child := w.Spawn(), w.Spawn()
	h.SetLocal(parent, entities.NewTransform2D(0, 0))
	h.SetLocal(child, entities.NewTransform2D(1, 0))
	if err := h.SetParent(child, parent, false); err != nil {
		t.Fatal(err)
	}
	h.Sync()
	entities.Set(w, parent, entities.NewTransform2D(10, 0))
	wt, ok := h.WorldTransform(child)
	if !ok || wt.X != 11 {
		t.Fatalf("child world X after entities.Set on the parent = %v (ok %v), want 11", wt.X, ok)
	}
}

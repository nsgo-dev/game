package scene3d

import (
	"math/rand/v2"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

func validateBVH(t *testing.T, b *BVH) {
	t.Helper()
	if b.count == 0 {
		if b.root != bvhNull {
			t.Fatalf("empty BVH has non-null root %d", b.root)
		}
		return
	}
	if b.root == bvhNull {
		t.Fatalf("non-empty BVH (count=%d) has null root", b.count)
	}
	if b.nodes[b.root].parent != bvhNull {
		t.Fatalf("root %d has non-null parent %d", b.root, b.nodes[b.root].parent)
	}

	seenLeaves := make(map[int32]bool, b.count)
	var check func(idx, expectedParent int32) int32
	check = func(idx, expectedParent int32) int32 {
		if idx < 0 || int(idx) >= len(b.nodes) {
			t.Fatalf("node index %d out of bounds (len=%d)", idx, len(b.nodes))
		}
		n := &b.nodes[idx]
		if n.parent != expectedParent {
			t.Fatalf("node %d parent = %d, want %d", idx, n.parent, expectedParent)
		}
		if n.left == bvhNull {
			if n.right != bvhNull {
				t.Fatalf("leaf node %d has non-null right %d", idx, n.right)
			}
			if n.height != 0 {
				t.Fatalf("leaf node %d height = %d, want 0", idx, n.height)
			}
			if n.leafIdx < 0 || int(n.leafIdx) >= len(b.leaves) {
				t.Fatalf("leaf node %d has invalid leafIdx %d", idx, n.leafIdx)
			}
			if seenLeaves[n.leafIdx] {
				t.Fatalf("duplicate leafIdx %d in tree", n.leafIdx)
			}
			seenLeaves[n.leafIdx] = true
			leaf := &b.leaves[n.leafIdx]
			if !leaf.alive {
				t.Fatalf("reachable leaf %d marked dead", n.leafIdx)
			}
			if leaf.nodeIdx != idx {
				t.Fatalf("leaf %d nodeIdx = %d, want %d", n.leafIdx, leaf.nodeIdx, idx)
			}
			if !aabbContains(n.fatMin, n.fatMax, leaf.tight.Min, leaf.tight.Max) {
				t.Fatalf("leaf %d fat [%v, %v] does not contain tight [%v, %v]",
					n.leafIdx, n.fatMin, n.fatMax, leaf.tight.Min, leaf.tight.Max)
			}
			return 0
		}
		if n.right == bvhNull {
			t.Fatalf("internal node %d has null right", idx)
		}
		if n.leafIdx != bvhNull {
			t.Fatalf("internal node %d has non-null leafIdx %d", idx, n.leafIdx)
		}
		lh := check(n.left, idx)
		rh := check(n.right, idx)
		wantH := 1 + max(lh, rh)
		if n.height != wantH {
			t.Fatalf("internal node %d height = %d, want %d", idx, n.height, wantH)
		}
		l := &b.nodes[n.left]
		r := &b.nodes[n.right]
		uMin, uMax := aabbUnion(l.fatMin, l.fatMax, r.fatMin, r.fatMax)
		if n.fatMin != uMin || n.fatMax != uMax {
			t.Fatalf("internal node %d fat [%v, %v] != union [%v, %v]",
				idx, n.fatMin, n.fatMax, uMin, uMax)
		}
		return wantH
	}

	check(b.root, bvhNull)

	if len(seenLeaves) != b.count {
		t.Fatalf("reachable leaves = %d, count = %d", len(seenLeaves), b.count)
	}
	aliveCount := 0
	for i := range b.leaves {
		if b.leaves[i].alive {
			aliveCount++
			if !seenLeaves[int32(i)] {
				t.Fatalf("alive leaf %d not reachable from root", i)
			}
		}
	}
	if aliveCount != b.count {
		t.Fatalf("alive leaves = %d, count = %d", aliveCount, b.count)
	}
}

func TestBVHTreeInvariantsDynamicAndRebuild(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 99))
	bvh := NewBVH()

	randBox := func(spread float32) math3d.AABB {
		cx := (rng.Float32()*2 - 1) * spread
		cy := (rng.Float32()*2 - 1) * spread
		cz := (rng.Float32()*2 - 1) * spread
		hx := 0.1 + rng.Float32()*2.0
		hy := 0.1 + rng.Float32()*2.0
		hz := 0.1 + rng.Float32()*2.0
		return math3d.AABB{
			Min: math3d.Vec3{X: cx - hx, Y: cy - hy, Z: cz - hz},
			Max: math3d.Vec3{X: cx + hx, Y: cy + hy, Z: cz + hz},
		}
	}

	handles := make([]BVHHandle, 0, 1500)
	for i := range 1500 {
		h := bvh.Insert(uint64(i+1), randBox(100), i)
		handles = append(handles, h)
	}
	validateBVH(t, bvh)

	// Height should stay well-balanced (O(log N)) after 1500 dynamic inserts.
	if h := bvh.Height(); h > 24 {
		t.Fatalf("dynamic BVH height after 1500 inserts = %d, expected <= 24", h)
	}

	// Small moves (inside fat AABB), large moves (reinsert), and shrinks.
	for i := range 1000 {
		h := handles[rng.IntN(len(handles))]
		cur, ok := bvh.Bounds(h)
		if !ok {
			t.Fatalf("missing bounds for live handle")
		}
		switch i % 3 {
		case 0:
			// Tiny nudge inside fat margin.
			delta := math3d.Vec3{X: 0.01, Y: -0.01, Z: 0.01}
			if !bvh.Update(h, math3d.AABB{Min: cur.Min.Add(delta), Max: cur.Max.Add(delta)}) {
				t.Fatalf("small Update failed")
			}
		case 1:
			// Teleport across world.
			if !bvh.Update(h, randBox(100)) {
				t.Fatalf("large Update failed")
			}
		case 2:
			// Shrink box dramatically.
			c := cur.Center()
			ext := math3d.Vec3{X: 0.02, Y: 0.02, Z: 0.02}
			if !bvh.Update(h, math3d.AABB{Min: c.Sub(ext), Max: c.Add(ext)}) {
				t.Fatalf("shrink Update failed")
			}
		}
	}
	validateBVH(t, bvh)

	// Remove 500 entries and verify stale handles are rejected.
	for i := range 500 {
		h := handles[i]
		bvh.Remove(h)
		if bvh.Contains(h) {
			t.Fatalf("handle still alive after Remove")
		}
		if bvh.Update(h, randBox(10)) {
			t.Fatalf("Update succeeded on removed handle")
		}
		// Double remove must be a safe no-op.
		bvh.Remove(h)
	}
	handles = handles[500:]
	validateBVH(t, bvh)

	// Re-insert 500 new entries reusing freed slots and verify generation bump.
	for i := range 500 {
		handles = append(handles, bvh.Insert(uint64(5000+i), randBox(100), 5000+i))
	}
	validateBVH(t, bvh)

	// SAH Rebuild and verify invariants + subsequent dynamic mutations still hold.
	bvh.Rebuild()
	validateBVH(t, bvh)

	for i := range 200 {
		h := handles[i]
		if !bvh.Update(h, randBox(120)) {
			t.Fatalf("post-Rebuild Update failed")
		}
	}
	for i := 200; i < 300; i++ {
		bvh.Remove(handles[i])
	}
	validateBVH(t, bvh)

	// Drain completely to empty and rebuild empty.
	for i := range handles {
		bvh.Remove(handles[i])
	}
	if bvh.Len() != 0 {
		t.Fatalf("Len() = %d after removing all, want 0", bvh.Len())
	}
	validateBVH(t, bvh)
	bvh.Rebuild()
	validateBVH(t, bvh)
}

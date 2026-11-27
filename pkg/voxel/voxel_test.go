package voxel_test

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/voxel"
)

func testRegistry(t *testing.T) (reg *voxel.Registry, stone, dirt, grass, glass, water voxel.BlockID) {
	t.Helper()
	reg = voxel.NewRegistry()
	stone = reg.MustRegister(voxel.BlockDef{Name: "stone", MaterialID: 0})
	dirt = reg.MustRegister(voxel.BlockDef{Name: "dirt", MaterialID: 0})
	grass = reg.MustRegister(voxel.BlockDef{Name: "grass", MaterialID: 1})
	glass = reg.MustRegister(voxel.BlockDef{Name: "glass", Transparent: true, MaterialID: 2})
	water = reg.MustRegister(voxel.BlockDef{Name: "water", Transparent: true, NonSolid: true, MaterialID: 3})
	return reg, stone, dirt, grass, glass, water
}

func TestRegistryOperations(t *testing.T) {
	reg := voxel.NewRegistry()
	if def, ok := reg.Get(voxel.Air); !ok || def.Name != "air" || !def.Transparent || !def.NonSolid {
		t.Fatalf("Air definition = %+v, ok=%v", def, ok)
	}
	if reg.IsOpaque(voxel.Air) || reg.IsSolid(voxel.Air) || !reg.IsTransparent(voxel.Air) {
		t.Fatal("Air opacity/solidity mismatch")
	}

	stone, err := reg.Register(voxel.BlockDef{Name: "stone"})
	if err != nil || stone != 1 {
		t.Fatalf("Register stone = (%d, %v), want (1, nil)", stone, err)
	}
	explicit, err := reg.Register(voxel.BlockDef{ID: 10, Name: "gold", MaterialID: 7})
	if err != nil || explicit != 10 {
		t.Fatalf("Register gold = (%d, %v), want (10, nil)", explicit, err)
	}
	if _, err := reg.Register(voxel.BlockDef{Name: ""}); !errors.Is(err, voxel.ErrInvalidBlock) {
		t.Fatalf("empty name error = %v, want ErrInvalidBlock", err)
	}
	if _, err := reg.Register(voxel.BlockDef{Name: "air"}); !errors.Is(err, voxel.ErrDuplicateBlock) {
		t.Fatalf("reserved air error = %v, want ErrDuplicateBlock", err)
	}
	if _, err := reg.Register(voxel.BlockDef{Name: "stone"}); !errors.Is(err, voxel.ErrDuplicateBlock) {
		t.Fatalf("duplicate name error = %v, want ErrDuplicateBlock", err)
	}
	if _, err := reg.Register(voxel.BlockDef{ID: 10, Name: "dup_id"}); !errors.Is(err, voxel.ErrDuplicateBlock) {
		t.Fatalf("duplicate ID error = %v, want ErrDuplicateBlock", err)
	}

	// Unregistered non-zero IDs fall back to opaque, solid, MaterialID 0.
	if !reg.IsOpaque(999) || !reg.IsSolid(999) || reg.MaterialID(999) != 0 {
		t.Fatal("unregistered block ID fallback mismatch")
	}

	byName, ok := reg.ByName("gold")
	if !ok || byName.ID != 10 || byName.MaterialID != 7 {
		t.Fatalf("ByName(gold) = %+v, %v", byName, ok)
	}
	all := reg.All()
	if len(all) != 2 || all[0].ID != 1 || all[1].ID != 10 {
		t.Fatalf("All() = %+v, want [1, 10]", all)
	}
	cloned := reg.Clone()
	if _, ok := cloned.ByName("gold"); !ok {
		t.Fatal("Clone missing gold block")
	}
}

func TestWorldConfigAndNegativeCoordinates(t *testing.T) {
	if _, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 12}); err == nil {
		t.Fatal("expected error for non-power-of-two chunk size 12")
	}
	if _, err := voxel.NewWorld(voxel.WorldConfig{VoxelSize: -1}); err == nil {
		t.Fatal("expected error for negative voxel size")
	}

	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 16, VoxelSize: 2})
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	cases := []struct {
		pos        voxel.BlockCoord
		wantCC     voxel.ChunkCoord
		wantLocalX int
		wantLocalY int
		wantLocalZ int
	}{
		{voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.ChunkCoord{X: 0, Y: 0, Z: 0}, 0, 0, 0},
		{voxel.BlockCoord{X: 15, Y: 16, Z: 31}, voxel.ChunkCoord{X: 0, Y: 1, Z: 1}, 15, 0, 15},
		{voxel.BlockCoord{X: -1, Y: -16, Z: -17}, voxel.ChunkCoord{X: -1, Y: -1, Z: -2}, 15, 0, 15},
	}
	for _, tc := range cases {
		cc, lx, ly, lz := w.BlockToChunk(tc.pos)
		if cc != tc.wantCC || lx != tc.wantLocalX || ly != tc.wantLocalY || lz != tc.wantLocalZ {
			t.Fatalf("BlockToChunk(%+v) = (%+v, %d,%d,%d), want (%+v, %d,%d,%d)",
				tc.pos, cc, lx, ly, lz, tc.wantCC, tc.wantLocalX, tc.wantLocalY, tc.wantLocalZ)
		}
		origin := w.ChunkOrigin(cc)
		rebuilt := origin.Add(voxel.BlockCoord{X: lx, Y: ly, Z: lz})
		if rebuilt != tc.pos {
			t.Fatalf("round-trip coord %+v != %+v", rebuilt, tc.pos)
		}
		w.SetBlock(tc.pos, 1)
		if got := w.Block(tc.pos); got != 1 {
			t.Fatalf("Block(%+v) = %d, want 1", tc.pos, got)
		}
	}

	// WorldToBlock with VoxelSize = 2.
	if got := w.WorldToBlock(math3d.Vec3{X: -0.1, Y: 3.9, Z: -4.0}); got != (voxel.BlockCoord{X: -1, Y: 1, Z: -2}) {
		t.Fatalf("WorldToBlock = %+v, want {-1, 1, -2}", got)
	}
}

func TestDirtyChunkTrackingOnEdit(t *testing.T) {
	reg, stone, dirt, _, _, _ := testRegistry(t)
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 16, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}

	// Build two adjacent solid chunks: (0,0,0) and (1,0,0).
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 31, Y: 15, Z: 15}, stone)
	mesher := voxel.NewMesher()
	initial := mesher.RemeshDirty(w, voxel.MeshOptions{})
	if len(initial) != 2 {
		t.Fatalf("initial RemeshDirty count = %d, want 2", len(initial))
	}
	if w.DirtyCount() != 0 {
		t.Fatalf("DirtyCount after initial remesh = %d, want 0", w.DirtyCount())
	}

	// 1. Interior edit: must dirty and remesh ONLY its own chunk.
	w.SetBlock(voxel.BlockCoord{X: 5, Y: 5, Z: 5}, voxel.Air)
	if dirty := w.DirtyChunks(); len(dirty) != 1 || dirty[0] != (voxel.ChunkCoord{X: 0, Y: 0, Z: 0}) {
		t.Fatalf("interior edit dirtied %v, want only (0,0,0)", dirty)
	}
	rebuilt := mesher.RemeshDirty(w, voxel.MeshOptions{})
	if len(rebuilt) != 1 || rebuilt[0].Coord != (voxel.ChunkCoord{X: 0, Y: 0, Z: 0}) {
		t.Fatalf("interior edit remeshed %d chunks, want 1", len(rebuilt))
	}

	// 2. Boundary edit replacing opaque stone -> opaque dirt: neighbor face
	// visibility and AO do not change, so ONLY chunk (0,0,0) is dirtied.
	w.SetBlock(voxel.BlockCoord{X: 15, Y: 8, Z: 8}, dirt)
	if dirty := w.DirtyChunks(); len(dirty) != 1 || dirty[0] != (voxel.ChunkCoord{X: 0, Y: 0, Z: 0}) {
		t.Fatalf("opaque->opaque boundary edit dirtied %v, want only (0,0,0)", dirty)
	}
	mesher.RemeshDirty(w, voxel.MeshOptions{})

	// 3. Boundary edit carving stone -> air at x=15 exposes (16,8,8) in chunk (1,0,0):
	// BOTH chunk (0,0,0) and chunk (1,0,0) must be dirtied and remeshed.
	w.SetBlock(voxel.BlockCoord{X: 15, Y: 8, Z: 8}, voxel.Air)
	dirty := w.DirtyChunks()
	if len(dirty) != 2 || dirty[0] != (voxel.ChunkCoord{X: 0, Y: 0, Z: 0}) || dirty[1] != (voxel.ChunkCoord{X: 1, Y: 0, Z: 0}) {
		t.Fatalf("opaque->air boundary edit dirtied %v, want [(0,0,0) (1,0,0)]", dirty)
	}
	rebuilt = mesher.RemeshDirty(w, voxel.MeshOptions{})
	if len(rebuilt) != 2 {
		t.Fatalf("boundary edit remeshed %d chunks, want 2", len(rebuilt))
	}
	// Chunk (1,0,0) previously had 5 quads (shared -X face culled); carving one
	// block at x=15 exposes one -X quad so it now has 6 quads.
	if rebuilt[1].QuadCount != 6 {
		t.Fatalf("chunk (1,0,0) QuadCount = %d, want 6", rebuilt[1].QuadCount)
	}

	// 4. Clearing a chunk completely still reports it dirty once so callers
	// can drop its GPU mesh, and PruneEmptyChunks frees it afterwards.
	w.SetBlock(voxel.BlockCoord{X: 50, Y: 0, Z: 0}, stone)
	mesher.RemeshDirty(w, voxel.MeshOptions{})
	w.SetBlock(voxel.BlockCoord{X: 50, Y: 0, Z: 0}, voxel.Air)
	emptyMeshes := mesher.RemeshDirty(w, voxel.MeshOptions{})
	if len(emptyMeshes) != 1 || !emptyMeshes[0].Empty() {
		t.Fatalf("clearing last block returned %+v, want 1 empty ChunkMesh", emptyMeshes)
	}
	if pruned := w.PruneEmptyChunks(); pruned != 1 {
		t.Fatalf("PruneEmptyChunks = %d, want 1", pruned)
	}
}

func assertValidCCWMesh(t *testing.T, cm voxel.ChunkMesh) {
	t.Helper()
	if err := cm.Mesh.Validate(); err != nil {
		t.Fatalf("render3d.Mesh.Validate failed: %v", err)
	}
	if len(cm.Mesh.Indices)%6 != 0 {
		t.Fatalf("indices length %d is not a multiple of 6", len(cm.Mesh.Indices))
	}
	if cm.QuadCount != len(cm.Mesh.Indices)/6 {
		t.Fatalf("QuadCount = %d, want %d", cm.QuadCount, len(cm.Mesh.Indices)/6)
	}
	for i := 0; i < len(cm.Mesh.Indices); i += 3 {
		i0, i1, i2 := cm.Mesh.Indices[i], cm.Mesh.Indices[i+1], cm.Mesh.Indices[i+2]
		v0 := cm.Mesh.Positions[i0]
		v1 := cm.Mesh.Positions[i1]
		v2 := cm.Mesh.Positions[i2]
		n := cm.Mesh.Normals[i0]
		cross := v1.Sub(v0).Cross(v2.Sub(v0))
		if dot := cross.Dot(n); dot <= 0 {
			t.Fatalf("triangle %d has non-CCW winding: cross=%+v normal=%+v dot=%v", i/3, cross, n, dot)
		}
	}
}

func TestGreedyMeshingReductionAndWinding(t *testing.T) {
	reg, stone, dirt, grass, _, _ := testRegistry(t)
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 16, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}

	// Single voxel: 6 quads, valid CCW winding on every face.
	w.SetBlock(voxel.BlockCoord{X: 3, Y: 4, Z: 5}, stone)
	single := w.MeshChunk(voxel.ChunkCoord{}, voxel.MeshOptions{})
	if single.QuadCount != 6 {
		t.Fatalf("single voxel QuadCount = %d, want 6", single.QuadCount)
	}
	assertValidCCWMesh(t, single)

	// Full 16x16x16 solid chunk: greedy merges all 1536 faces into 6 quads!
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 15, Y: 15, Z: 15}, stone)
	greedy := w.MeshChunk(voxel.ChunkCoord{}, voxel.MeshOptions{})
	naive := w.MeshChunk(voxel.ChunkCoord{}, voxel.MeshOptions{DisableGreedy: true})
	assertValidCCWMesh(t, greedy)
	assertValidCCWMesh(t, naive)

	if naive.QuadCount != 6*16*16 {
		t.Fatalf("naive 16^3 cube QuadCount = %d, want %d", naive.QuadCount, 6*16*16)
	}
	if greedy.QuadCount != 6 {
		t.Fatalf("greedy 16^3 cube QuadCount = %d, want 6", greedy.QuadCount)
	}

	// Multi-material chunk: stone (mat 0), dirt (mat 0), grass (mat 1).
	w.Clear()
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 7, Y: 0, Z: 15}, stone)
	w.FillBox(voxel.BlockCoord{X: 8, Y: 0, Z: 0}, voxel.BlockCoord{X: 15, Y: 0, Z: 15}, grass)
	w.SetBlock(voxel.BlockCoord{X: 0, Y: 1, Z: 0}, dirt)
	multi := w.MeshChunk(voxel.ChunkCoord{}, voxel.MeshOptions{WorldSpace: true, UVMode: voxel.UVUnit})
	assertValidCCWMesh(t, multi)
	if len(multi.SubMeshes) != 2 {
		t.Fatalf("SubMeshes len = %d, want 2 (materials 0 and 1)", len(multi.SubMeshes))
	}
	if multi.SubMeshes[0].MaterialID != 0 || multi.SubMeshes[1].MaterialID != 1 {
		t.Fatalf("SubMeshes order = %+v, want MaterialIDs [0, 1]", multi.SubMeshes)
	}
	if multi.SubMeshes[0].FirstIndex != 0 ||
		multi.SubMeshes[1].FirstIndex != multi.SubMeshes[0].IndexCount ||
		multi.SubMeshes[0].IndexCount+multi.SubMeshes[1].IndexCount != uint32(len(multi.Mesh.Indices)) {
		t.Fatalf("SubMeshes ranges do not partition indices: %+v (total %d)", multi.SubMeshes, len(multi.Mesh.Indices))
	}
}

func TestCrossChunkAndTransparentCulling(t *testing.T) {
	reg, stone, _, _, glass, water := testRegistry(t)
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 8, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}

	// Two adjacent full chunks: shared boundary face must be completely culled.
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 15, Y: 7, Z: 7}, stone)
	cm0 := w.MeshChunk(voxel.ChunkCoord{X: 0}, voxel.MeshOptions{})
	cm1 := w.MeshChunk(voxel.ChunkCoord{X: 1}, voxel.MeshOptions{})
	if cm0.QuadCount != 5 || cm1.QuadCount != 5 {
		t.Fatalf("adjacent solid chunks QuadCount = (%d, %d), want (5, 5)", cm0.QuadCount, cm1.QuadCount)
	}

	// IgnoreNeighbors keeps all 6 faces on chunk (0,0,0).
	cm0NoNb := w.MeshChunk(voxel.ChunkCoord{X: 0}, voxel.MeshOptions{IgnoreNeighbors: true})
	if cm0NoNb.QuadCount != 6 {
		t.Fatalf("IgnoreNeighbors QuadCount = %d, want 6", cm0NoNb.QuadCount)
	}

	// Transparent culling rules:
	// - Stone (0,0,0) next to Glass (1,0,0): stone emits 6 faces (visible behind glass),
	//   glass emits 5 faces (face pressed against opaque stone is culled).
	// - Glass (1,0,0) next to Glass (2,0,0): shared glass-glass face is culled.
	// - Glass (2,0,0) next to Water (3,0,0): both emit faces between different transparent types.
	w.Clear()
	w.SetBlock(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, stone)
	w.SetBlock(voxel.BlockCoord{X: 1, Y: 0, Z: 0}, glass)
	w.SetBlock(voxel.BlockCoord{X: 2, Y: 0, Z: 0}, glass)
	w.SetBlock(voxel.BlockCoord{X: 3, Y: 0, Z: 0}, water)

	// Count quads with greedy disabled so we can verify exact face count:
	// Stone: 6
	// Glass@1: 4 (culled -X against stone, culled +X against glass@2)
	// Glass@2: 5 (culled -X against glass@1, keeps +X against water@3)
	// Water@3: 6 (keeps -X against glass@2)
	// Total = 6 + 4 + 5 + 6 = 21 faces.
	cm := w.MeshChunk(voxel.ChunkCoord{}, voxel.MeshOptions{DisableGreedy: true})
	if cm.QuadCount != 21 {
		t.Fatalf("transparent culling face count = %d, want 21", cm.QuadCount)
	}
	assertValidCCWMesh(t, cm)
}

func TestAmbientOcclusionAndCrossChunkAO(t *testing.T) {
	reg, stone, _, _, _, _ := testRegistry(t)
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 8, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}

	// Flat floor at y=0 across chunks (0,0,0) and (1,0,0).
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 15, Y: 0, Z: 7}, stone)
	// Place an occluding wall at x=8, y=1 in chunk (1,0,0); it must darken
	// the +Y top-face corners at x=7 in chunk (0,0,0).
	w.FillBox(voxel.BlockCoord{X: 8, Y: 1, Z: 0}, voxel.BlockCoord{X: 8, Y: 1, Z: 7}, stone)

	cm0 := w.MeshChunk(voxel.ChunkCoord{X: 0}, voxel.MeshOptions{AmbientOcclusion: true})
	assertValidCCWMesh(t, cm0)
	if len(cm0.AO) != len(cm0.Mesh.Positions) {
		t.Fatalf("AO len = %d, want %d", len(cm0.AO), len(cm0.Mesh.Positions))
	}

	hasDarkened := false
	hasFullLit := false
	for i, ao := range cm0.AO {
		if cm0.Mesh.Normals[i] == (math3d.Vec3{Y: 1}) {
			if ao < 3 {
				hasDarkened = true
			}
			if ao == 3 {
				hasFullLit = true
			}
		}
	}
	if !hasDarkened || !hasFullLit {
		t.Fatalf("expected both occluded and unoccluded +Y vertices across chunk boundary (darkened=%v, lit=%v)", hasDarkened, hasFullLit)
	}

	// Verify removing the cross-chunk occluder at (8,1,3) dirties chunk (0,0,0) too!
	w.ClearDirty(voxel.ChunkCoord{X: 0})
	w.ClearDirty(voxel.ChunkCoord{X: 1})
	w.SetBlock(voxel.BlockCoord{X: 8, Y: 1, Z: 3}, voxel.Air)
	if !w.IsDirty(voxel.ChunkCoord{X: 0}) || !w.IsDirty(voxel.ChunkCoord{X: 1}) {
		t.Fatalf("removing cross-chunk AO occluder dirtied %v, want both (0,0,0) and (1,0,0)", w.DirtyChunks())
	}

	if voxel.AOFactor(0) >= voxel.AOFactor(1) || voxel.AOFactor(1) >= voxel.AOFactor(2) || voxel.AOFactor(2) >= voxel.AOFactor(3) {
		t.Fatal("AOFactor must be strictly increasing from 0 to 3")
	}
}

func TestRaycastDDA(t *testing.T) {
	reg, stone, _, _, _, water := testRegistry(t)
	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 8, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}

	// Place a target block at (10, 2, -5) (crosses chunk boundaries and negative Z).
	target := voxel.BlockCoord{X: 10, Y: 2, Z: -5}
	w.SetBlock(target, stone)
	// Place water directly in front of it along -X.
	w.SetBlock(voxel.BlockCoord{X: 7, Y: 2, Z: -5}, water)

	// Default raycast ignores non-solid water and hits stone on FaceNegX.
	ray := math3d.Ray{
		Origin: math3d.Vec3{X: 2.5, Y: 2.5, Z: -4.5},
		Dir:    math3d.Vec3{X: 3, Y: 0, Z: 0}, // intentionally non-unit
	}
	hit, ok := w.Raycast(ray, 20)
	if !ok {
		t.Fatal("expected raycast hit")
	}
	if hit.Block != target {
		t.Fatalf("hit.Block = %+v, want %+v", hit.Block, target)
	}
	if hit.Face != voxel.FaceNegX || hit.Normal != (math3d.Vec3{X: -1}) {
		t.Fatalf("hit.Face=%v Normal=%+v, want -X", hit.Face, hit.Normal)
	}
	if hit.Adjacent != (voxel.BlockCoord{X: 9, Y: 2, Z: -5}) {
		t.Fatalf("hit.Adjacent = %+v, want {9, 2, -5}", hit.Adjacent)
	}
	if !math3d.ApproxEqual(hit.Distance, 7.5, 1e-4) {
		t.Fatalf("hit.Distance = %v, want 7.5", hit.Distance)
	}
	if hit.BlockID != stone {
		t.Fatalf("hit.BlockID = %d, want %d", hit.BlockID, stone)
	}

	// Sandbox placement workflow: place at hit.Adjacent and raycast again.
	w.SetBlock(hit.Adjacent, stone)
	hit2, ok := w.Raycast(ray, 20)
	if !ok || hit2.Block != hit.Adjacent {
		t.Fatalf("after placing at Adjacent, raycast hit %+v (ok=%v), want %+v", hit2.Block, ok, hit.Adjacent)
	}

	// IncludeNonSolid hits water first.
	waterHit, ok := w.RaycastWith(ray, 20, voxel.RaycastOptions{IncludeNonSolid: true})
	if !ok || waterHit.BlockID != water || waterHit.Block != (voxel.BlockCoord{X: 7, Y: 2, Z: -5}) {
		t.Fatalf("IncludeNonSolid hit = %+v (ok=%v), want water at (7,2,-5)", waterHit, ok)
	}

	// Max distance cutoff.
	if _, ok := w.Raycast(ray, 5); ok {
		t.Fatal("expected miss when target is beyond maxDist")
	}

	// Starting inside a solid block hits at distance 0.
	insideHit, ok := w.Raycast(math3d.Ray{
		Origin: math3d.Vec3{X: 10.5, Y: 2.5, Z: -4.5},
		Dir:    math3d.Vec3{X: 0, Y: -1, Z: 0},
	}, 10)
	if !ok || insideHit.Block != target || insideHit.Distance != 0 || insideHit.Face != voxel.FacePosY {
		t.Fatalf("inside raycast = %+v (ok=%v)", insideHit, ok)
	}

	// Invalid rays return false.
	if _, ok := w.Raycast(math3d.Ray{Origin: math3d.Vec3{}, Dir: math3d.Vec3{}}, 10); ok {
		t.Fatal("expected miss for zero ray direction")
	}
	if _, ok := w.Raycast(math3d.Ray{Origin: math3d.Vec3{X: float32(math.NaN())}, Dir: math3d.Vec3{X: 1}}, 10); ok {
		t.Fatal("expected miss for NaN origin")
	}

	// Test all 6 face directions around a single isolated block.
	w.Clear()
	center := voxel.BlockCoord{X: 0, Y: 0, Z: 0}
	w.SetBlock(center, stone)
	for face := range voxel.Face(voxel.FaceCount) {
		n := face.Normal()
		origin := math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}.Add(n.Scale(5))
		h, ok := w.Raycast(math3d.Ray{Origin: origin, Dir: n.Neg()}, 10)
		if !ok || h.Block != center || h.Face != face || h.Adjacent != face.Step() {
			t.Fatalf("face %v: got hit=%+v ok=%v", face, h, ok)
		}
	}
}

func TestYAMLRoundTripDeterminismAndValidation(t *testing.T) {
	reg, stone, dirt, grass, glass, water := testRegistry(t)
	w1, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 8, VoxelSize: 1, Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	// Populate chunks in one order in w1 and the reverse order in w2.
	w1.FillBox(voxel.BlockCoord{X: -8, Y: 0, Z: -8}, voxel.BlockCoord{X: 7, Y: 2, Z: 7}, stone)
	w1.SetBlock(voxel.BlockCoord{X: 0, Y: 3, Z: 0}, grass)
	w1.SetBlock(voxel.BlockCoord{X: 1, Y: 3, Z: 0}, glass)
	w1.SetBlock(voxel.BlockCoord{X: 2, Y: 3, Z: 0}, water)
	w1.SetBlock(voxel.BlockCoord{X: -1, Y: 2, Z: -1}, dirt)

	w2, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 8, VoxelSize: 1, Registry: reg.Clone()})
	if err != nil {
		t.Fatal(err)
	}
	w2.SetBlock(voxel.BlockCoord{X: -1, Y: 2, Z: -1}, dirt)
	w2.SetBlock(voxel.BlockCoord{X: 2, Y: 3, Z: 0}, water)
	w2.SetBlock(voxel.BlockCoord{X: 1, Y: 3, Z: 0}, glass)
	w2.SetBlock(voxel.BlockCoord{X: 0, Y: 3, Z: 0}, grass)
	w2.FillBox(voxel.BlockCoord{X: 7, Y: 2, Z: 7}, voxel.BlockCoord{X: -8, Y: 0, Z: -8}, stone)
	w2.SetBlock(voxel.BlockCoord{X: -1, Y: 2, Z: -1}, dirt)

	data1, err := voxel.SaveYAML(w1, "Island")
	if err != nil {
		t.Fatalf("SaveYAML w1: %v", err)
	}
	data2, err := voxel.SaveYAML(w2, "Island")
	if err != nil {
		t.Fatalf("SaveYAML w2: %v", err)
	}
	if !bytes.Equal(data1, data2) {
		t.Fatalf("SaveYAML output is not deterministic across insertion orders:\n--- w1 ---\n%s\n--- w2 ---\n%s", data1, data2)
	}
	if !strings.HasPrefix(string(data1), voxel.SchemaComment+"\n") {
		t.Fatalf("YAML output missing schema comment prefix:\n%s", data1)
	}

	// Load -> Save round trip must be byte-for-byte identical.
	loaded, name, err := voxel.LoadYAML(data1)
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if name != "Island" {
		t.Fatalf("loaded name = %q, want Island", name)
	}
	data3, err := voxel.SaveYAML(loaded, name)
	if err != nil {
		t.Fatalf("SaveYAML after LoadYAML: %v", err)
	}
	if !bytes.Equal(data1, data3) {
		t.Fatalf("round-trip SaveYAML changed bytes:\n--- before ---\n%s\n--- after ---\n%s", data1, data3)
	}
	// Loaded chunks are marked dirty so initial meshing picks them all up.
	if loaded.DirtyCount() != loaded.ChunkCount() {
		t.Fatalf("loaded DirtyCount=%d, want ChunkCount=%d", loaded.DirtyCount(), loaded.ChunkCount())
	}

	// Validation error cases.
	badCases := []struct {
		name      string
		yaml      string
		wantField string
	}{
		{
			name: "empty",
			yaml: "   \n",
		},
		{
			name: "unknown field",
			yaml: "version: 1\nchunkSize: 8\nunknownKey: true\n",
		},
		{
			name: "trailing document",
			yaml: "version: 1\nchunkSize: 8\n---\nversion: 1\n",
		},
		{
			name:      "bad version",
			yaml:      "version: 2\nchunkSize: 8\n",
			wantField: "version",
		},
		{
			name:      "non-power-of-two chunkSize",
			yaml:      "version: 1\nchunkSize: 10\n",
			wantField: "chunkSize",
		},
		{
			name:      "negative voxelSize",
			yaml:      "version: 1\nchunkSize: 8\nvoxelSize: -0.5\n",
			wantField: "voxelSize",
		},
		{
			name:      "zero palette id",
			yaml:      "version: 1\nchunkSize: 4\npalette:\n  - id: 0\n    name: stone\n",
			wantField: "palette[0].id",
		},
		{
			name:      "reserved air name",
			yaml:      "version: 1\nchunkSize: 4\npalette:\n  - id: 1\n    name: air\n",
			wantField: "palette[0].name",
		},
		{
			name:      "duplicate palette id",
			yaml:      "version: 1\nchunkSize: 4\npalette:\n  - id: 1\n    name: stone\n  - id: 1\n    name: dirt\n",
			wantField: "palette[1].id",
		},
		{
			name:      "duplicate chunk",
			yaml:      "version: 1\nchunkSize: 4\nchunks:\n  - x: 0\n    y: 0\n    z: 0\n    runs: ['1*64']\n  - x: 0\n    y: 0\n    z: 0\n    runs: ['1*64']\n",
			wantField: "chunks[1]",
		},
		{
			name:      "short run total",
			yaml:      "version: 1\nchunkSize: 4\nchunks:\n  - x: 0\n    y: 0\n    z: 0\n    runs: ['1*60']\n",
			wantField: "chunks[0].runs",
		},
		{
			name:      "bad run token",
			yaml:      "version: 1\nchunkSize: 4\nchunks:\n  - x: 0\n    y: 0\n    z: 0\n    runs: ['1*zero']\n",
			wantField: "chunks[0].runs[0]",
		},
	}

	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := voxel.ParseWith([]byte(tc.yaml), "world.voxel.yaml")
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			le, ok := loaderr.As(err)
			if !ok {
				t.Fatalf("expected *loaderr.Error, got %T: %v", err, err)
			}
			if le.File != "world.voxel.yaml" {
				t.Fatalf("File = %q, want world.voxel.yaml", le.File)
			}
			if tc.wantField != "" && le.Field != tc.wantField {
				t.Fatalf("Field = %q, want %q (err=%v)", le.Field, tc.wantField, err)
			}
		})
	}
}

// buildTerrain256 creates a 256x256 voxel terrain (16x16 horizontal chunks at
// 16x16x16 each) with undulating hills, layered stone/dirt/grass, and water.
func buildTerrain256(b testing.TB) (*voxel.World, voxel.BlockID) {
	b.Helper()
	reg := voxel.NewRegistry()
	stone := reg.MustRegister(voxel.BlockDef{Name: "stone", MaterialID: 0})
	dirt := reg.MustRegister(voxel.BlockDef{Name: "dirt", MaterialID: 0})
	grass := reg.MustRegister(voxel.BlockDef{Name: "grass", MaterialID: 1})
	water := reg.MustRegister(voxel.BlockDef{Name: "water", Transparent: true, NonSolid: true, MaterialID: 2})

	w, err := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 16, Registry: reg})
	if err != nil {
		b.Fatal(err)
	}
	const seaLevel = 5
	for z := range 256 {
		fz := float64(z) * 0.05
		for x := range 256 {
			fx := float64(x) * 0.05
			h := int(7.5 + 4.0*math.Sin(fx)*math.Cos(fz) + 2.0*math.Sin(fx*0.5+fz*0.5))
			if h < 1 {
				h = 1
			}
			if h > 14 {
				h = 14
			}
			for y := 0; y <= h; y++ {
				var id voxel.BlockID
				switch {
				case y == h && h >= seaLevel:
					id = grass
				case y >= h-2:
					id = dirt
				default:
					id = stone
				}
				w.SetBlock(voxel.BlockCoord{X: x, Y: y, Z: z}, id)
			}
			for y := h + 1; y <= seaLevel; y++ {
				w.SetBlock(voxel.BlockCoord{X: x, Y: y, Z: z}, water)
			}
		}
	}
	return w, stone
}

func TestArea256x256SingleEditAndFrameBudget(t *testing.T) {
	w, stone := buildTerrain256(t)
	if w.ChunkCount() != 256 {
		t.Fatalf("256x256 area ChunkCount = %d, want 256", w.ChunkCount())
	}

	mesher := voxel.NewMesher()
	allMeshes := mesher.RemeshDirty(w, voxel.MeshOptions{AmbientOcclusion: true})
	if len(allMeshes) != 256 {
		t.Fatalf("initial remesh count = %d, want 256", len(allMeshes))
	}
	totalQuads := 0
	for _, cm := range allMeshes {
		totalQuads += cm.QuadCount
		if !cm.Empty() {
			if err := cm.Mesh.Validate(); err != nil {
				t.Fatalf("chunk %+v invalid mesh: %v", cm.Coord, err)
			}
		}
	}
	if totalQuads == 0 {
		t.Fatal("expected non-zero quads in 256x256 terrain")
	}

	// Edit a single interior block in chunk (8, 0, 8) and measure remesh time.
	editPos := voxel.BlockCoord{X: 8*16 + 7, Y: 10, Z: 8*16 + 7}
	start := time.Now()
	w.SetBlock(editPos, stone)
	rebuilt := mesher.RemeshDirty(w, voxel.MeshOptions{AmbientOcclusion: true})
	elapsed := time.Since(start)

	if len(rebuilt) != 1 || rebuilt[0].Coord != (voxel.ChunkCoord{X: 8, Y: 0, Z: 8}) {
		t.Fatalf("single edit remeshed %d chunks (%v), want only (8,0,8)", len(rebuilt), rebuilt)
	}
	if elapsed > 16*time.Millisecond {
		t.Fatalf("single edit remesh took %v, exceeding 60fps frame budget (16.6ms)", elapsed)
	}
}

func BenchmarkRemeshSingleEdit256x256(b *testing.B) {
	w, stone := buildTerrain256(b)
	mesher := voxel.NewMesher()
	mesher.RemeshDirty(w, voxel.MeshOptions{AmbientOcclusion: true})

	pos := voxel.BlockCoord{X: 8*16 + 7, Y: 12, Z: 8*16 + 7}
	opts := voxel.MeshOptions{AmbientOcclusion: true}
	b.ResetTimer()
	for i := range b.N {
		if i%2 == 0 {
			w.SetBlock(pos, stone)
		} else {
			w.SetBlock(pos, voxel.Air)
		}
		rebuilt := mesher.RemeshDirty(w, opts)
		if len(rebuilt) != 1 {
			b.Fatalf("expected 1 remeshed chunk, got %d", len(rebuilt))
		}
	}
}

func BenchmarkMeshFull256x256Area(b *testing.B) {
	w, _ := buildTerrain256(b)
	mesher := voxel.NewMesher()
	coords := w.ChunkCoords()
	opts := voxel.MeshOptions{}
	b.ResetTimer()
	for range b.N {
		for _, cc := range coords {
			_ = mesher.MeshChunk(w, cc, opts)
		}
	}
}

func BenchmarkRaycast256x256(b *testing.B) {
	w, _ := buildTerrain256(b)
	ray := math3d.Ray{
		Origin: math3d.Vec3{X: 10, Y: 20, Z: 10},
		Dir:    math3d.Vec3{X: 1, Y: -0.35, Z: 0.8},
	}
	b.ResetTimer()
	for range b.N {
		if _, ok := w.Raycast(ray, 128); !ok {
			b.Fatal("expected terrain hit")
		}
	}
}

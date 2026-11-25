package world3d_test

import (
	"encoding/binary"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

func TestRotateGridOrientationReachesAll24(t *testing.T) {
	// BFS from orientation 0 using X, Y, Z quarter-turns must visit all 24 cube orientations.
	visited := make(map[uint8]bool, world3d.GridOrientationCount)
	queue := []uint8{0}
	visited[0] = true
	axes := []world3d.GridAxis{world3d.GridAxisX, world3d.GridAxisY, world3d.GridAxisZ}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, axis := range axes {
			next := world3d.RotateGridOrientation(cur, axis, 1)
			if next >= world3d.GridOrientationCount {
				t.Fatalf("RotateGridOrientation(%d, %d, 1) = %d out of range", cur, axis, next)
			}
			// Inverse step returns to cur, and 4 steps return to cur.
			if back := world3d.RotateGridOrientation(next, axis, -1); back != cur {
				t.Fatalf("RotateGridOrientation(%d, %d, +1 then -1) = %d, want %d", cur, axis, back, cur)
			}
			o := cur
			for range 4 {
				o = world3d.RotateGridOrientation(o, axis, 1)
			}
			if o != cur {
				t.Fatalf("4 quarter-turns of %d around axis %d gave %d", cur, axis, o)
			}
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	if len(visited) != world3d.GridOrientationCount {
		t.Fatalf("visited %d orientations via X/Y/Z quarter-turns, want %d", len(visited), world3d.GridOrientationCount)
	}

	// FindGridOrientation round-trips every GridOrientationMat4.
	for i := range uint8(world3d.GridOrientationCount) {
		got, ok := world3d.FindGridOrientation(world3d.GridOrientationMat4(i))
		if !ok || got != i {
			t.Fatalf("FindGridOrientation(GridOrientationMat4(%d)) = (%d, %v)", i, got, ok)
		}
	}
}

func TestMeshLibraryItemsAndGLB(t *testing.T) {
	// Build a valid GLB container wrapping sampleDungeonGLTF.
	jsonBytes := []byte(sampleDungeonGLTF)
	for len(jsonBytes)%4 != 0 {
		jsonBytes = append(jsonBytes, ' ')
	}
	glb := make([]byte, 12+8+len(jsonBytes))
	binary.LittleEndian.PutUint32(glb[0:4], 0x46546C67) // "glTF"
	binary.LittleEndian.PutUint32(glb[4:8], 2)
	binary.LittleEndian.PutUint32(glb[8:12], uint32(len(glb)))
	binary.LittleEndian.PutUint32(glb[12:16], uint32(len(jsonBytes)))
	binary.LittleEndian.PutUint32(glb[16:20], 0x4E4F534A) // "JSON"
	copy(glb[20:], jsonBytes)

	lib, err := world3d.LoadMeshLibraryFromGLTF(glb)
	if err != nil {
		t.Fatalf("LoadMeshLibraryFromGLTF(.glb): %v", err)
	}
	items := lib.Items()
	if len(items) != 2 {
		t.Fatalf("Items len = %d, want 2", len(items))
	}
	if items[0].Name != "stone_floor" || items[1].Name != "stone_pillar" {
		t.Fatalf("Items = %+v, want [stone_floor, stone_pillar]", items)
	}
	// Mutating returned slice's ColliderBoxes must not affect the library.
	items[0].ColliderBoxes[0] = math3d.AABB{}
	orig, _ := lib.Get(items[0].ID)
	if orig.ColliderBoxes[0].Empty() {
		t.Fatal("mutating Items() aliased MeshLibrary internal ColliderBoxes")
	}

	// EachCell visits every occupied cell.
	g := world3d.NewGridMap(math3d.Vec3{X: 2, Y: 2, Z: 2}, lib)
	g.SetCell(world3d.GridCoord{X: 0, Y: 0, Z: 0}, items[0].ID, 0)
	g.SetCell(world3d.GridCoord{X: 2, Y: 1, Z: -1}, items[1].ID, 7)
	seen := 0
	g.EachCell(func(c world3d.GridCoord, cell world3d.GridCell) bool {
		seen++
		return true
	})
	if seen != 2 {
		t.Fatalf("EachCell visited %d cells, want 2", seen)
	}
}

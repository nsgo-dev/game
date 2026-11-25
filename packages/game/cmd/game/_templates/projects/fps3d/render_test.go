package main

import (
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/render3d"
)

func TestLevelMeshesCoverTheFloorWallsAndCrates(t *testing.T) {
	walls, crates, cells := 0, 0, 0
	for _, row := range arena.Layout {
		walls += strings.Count(row, string(cellWall))
		crates += strings.Count(row, string(cellCrate))
		cells += len(row)
	}
	level := levelMeshes(arena)
	// Twelve triangles a box: a floor tile under every cell but the walls, a box per wall and per crate.
	want := []int{(cells - walls) * 12, walls * 12, crates * 12}
	if len(level) != len(want) {
		t.Fatalf("levelMeshes returned %d meshes, want the floor, the walls and the crates", len(level))
	}
	for i, l := range level {
		if err := l.mesh.Validate(); err != nil {
			t.Fatal(err)
		}
		if got := len(l.mesh.Indices) / 3; got != want[i] {
			t.Errorf("level mesh %d has %d triangles, want %d", i, got, want[i])
		}
	}
	// The floor's top is the ground the player walks on, inside the border walls.
	if b := level[0].mesh.Bounds(); b.Max.Y != 0 || b.Min.X != arena.CellSize || b.Max.X != float32(len(arena.Layout[0])-1)*arena.CellSize {
		t.Errorf("the floor spans %+v", b)
	}
	if err := unitCube().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDynamicDrawsFollowTheMatch(t *testing.T) {
	m := quietMatch()
	cam := playerCamera(m, 1)
	// Two pickups and the weapon.
	if got := len(dynamicDraws(nil, m, cam, 1)); got != 3 {
		t.Fatalf("an empty arena has %d dynamic draws, want 3", got)
	}
	placeEnemy(m, "drone", m.cellCenter(gridCell{8, 2}))
	m.step(controls{fire: true}, step)
	// Each enemy is a body and a visor; the shot adds a tracer.
	if got := len(dynamicDraws(nil, m, cam, 1)); got != 6 {
		t.Fatalf("one enemy and one tracer give %d dynamic draws, want 6", got)
	}
}

func TestLightRigIsValidAndReusesItsSlices(t *testing.T) {
	m := quietMatch()
	rig := lightRig(render3d.LightRig{}, m)
	if err := rig.Validate(); err != nil {
		t.Fatal(err)
	}
	if want := len(m.spawns) + len(m.pickups); len(rig.Point) != want {
		t.Fatalf("the rig has %d point lights, want one per spawn and pickup, %d", len(rig.Point), want)
	}
	m.pickups[0].respawn = 5
	again := lightRig(rig, m)
	if len(again.Point) != len(rig.Point)-1 || &again.Point[0] != &rig.Point[0] {
		t.Fatal("a taken pickup still glows, or the rig did not reuse its slice")
	}
	if allocs := testing.AllocsPerRun(10, func() { rig = lightRig(rig, m) }); allocs != 0 {
		t.Fatalf("lightRig allocates %v times a frame", allocs)
	}
}

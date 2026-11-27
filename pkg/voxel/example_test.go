package voxel_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/voxel"
)

func ExampleWorld() {
	reg := voxel.NewRegistry()
	stone := reg.MustRegister(voxel.BlockDef{Name: "stone"})
	glass := reg.MustRegister(voxel.BlockDef{Name: "glass", Transparent: true})

	w, _ := voxel.NewWorld(voxel.WorldConfig{ChunkSize: 16, Registry: reg})
	w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 15, Y: 0, Z: 15}, stone)
	w.SetBlock(voxel.BlockCoord{X: 4, Y: 1, Z: 4}, glass)

	mesher := voxel.NewMesher()
	meshes := mesher.RemeshDirty(w, voxel.MeshOptions{AmbientOcclusion: true})
	fmt.Printf("initial chunks meshed: %d\n", len(meshes))

	// Raycast down onto the glass block and place a stone block on top of it.
	hit, ok := w.Raycast(math3d.Ray{
		Origin: math3d.Vec3{X: 4.5, Y: 5, Z: 4.5},
		Dir:    math3d.Vec3{X: 0, Y: -1, Z: 0},
	}, 10)
	if ok {
		w.SetBlock(hit.Adjacent, stone)
		updated := mesher.RemeshDirty(w, voxel.MeshOptions{AmbientOcclusion: true})
		fmt.Printf("hit=%v placed=%v remeshed=%d\n", hit.Block, hit.Adjacent, len(updated))
	}

	// Output:
	// initial chunks meshed: 1
	// hit={4 1 4} placed={4 2 4} remeshed=1
}

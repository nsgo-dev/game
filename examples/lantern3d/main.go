// Command lantern3d is an interactive 3D glTF scene example built with the NSGo
// game layer (gltf, math3d, scene3d, render3d, scene.HUD3D, game.Loop) on a
// portable gpu.View.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(gameProject.WindowSize()),
		nsgo.WindowConstraints(gameProject.WindowConstraints()),
		nsgo.Appearance(gameProject.Appearance()),
	)
}

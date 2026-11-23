// Command starburst is a top-down twin-stick / auto-aim arena shooter demonstrating
// high sprite counts (1000+ sprites through one sprite.Batch), deterministic
// particles.Emitter bursts, camera shake, circle collisions, and native NSGo HUD controls.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(
		Root,
		nsgo.WindowSize(gameProject.WindowSize()),
		nsgo.WindowConstraints(gameProject.WindowConstraints()),
		nsgo.Appearance(gameProject.Appearance()),
	)
}

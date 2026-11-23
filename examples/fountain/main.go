// Command fountain is a particle stress scene: four seeded particles.Emitter fountains hold 5000 live particles, drawn through sprite.Batch on a fixed-step game.Loop, for measuring frame rate with --nsgo-fps.
package main

import (
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

func main() {
	nsgo.Run(
		Root,
		nsgo.WindowSize(VirtualWidth, VirtualHeight),
		nsgo.Appearance(core.AppearanceDark),
		nsgo.WindowConstraints(core.WindowConstraints{MinWidth: VirtualWidth / 2, MinHeight: VirtualHeight / 2}),
	)
}

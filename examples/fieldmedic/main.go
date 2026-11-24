// Command fieldmedic is Field Medic, a small game whose patients bleed hit points until the player heals them by pressing them. Its ward is a scene file whose objects carry a health component, and it ships its own scene editor (cmd/editor) with the health editor plugin (package healthplugin) built in, as a game with editor plugins does.
package main

import (
	"github.com/nsgo-dev/nsgo"
)

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(gameProject.WindowSize()),
		nsgo.WindowConstraints(gameProject.WindowConstraints()),
		nsgo.Appearance(gameProject.Appearance()),
	)
}

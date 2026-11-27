// Command mygame is a 2D real-time strategy skirmish built with NSGo and game: gather crystals, build barracks, train an army and destroy the opponent's headquarters before it destroys yours.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(Project.WindowSize()),
		nsgo.WindowConstraints(Project.WindowConstraints()),
		nsgo.Appearance(Project.Appearance()),
	)
}

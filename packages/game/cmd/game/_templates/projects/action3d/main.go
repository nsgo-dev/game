// Command mygame is a third-person 3D action adventure built with NSGo and game.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(Project.WindowSize()),
		nsgo.WindowConstraints(Project.WindowConstraints()),
		nsgo.Appearance(Project.Appearance()),
	)
}

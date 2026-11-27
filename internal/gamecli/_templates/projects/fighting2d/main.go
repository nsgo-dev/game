// Command fighting2d is a one-on-one 2D fighting game built with NSGo and game.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(Project.WindowSize()),
		nsgo.WindowConstraints(Project.WindowConstraints()),
		nsgo.Appearance(Project.Appearance()),
	)
}

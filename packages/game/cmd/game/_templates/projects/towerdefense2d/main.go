// Command bramblegate is a 2D tower defense game built with NSGo and game: build and upgrade towers beside a road to stop the waves of enemies walking it before they reach the gate.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(Project.WindowSize()),
		nsgo.WindowConstraints(Project.WindowConstraints()),
		nsgo.Appearance(Project.Appearance()),
	)
}

// Command sparkslide is a 2D grid puzzle built with NSGo and game: slide a spark across the board to light every lamp in as few moves as you can.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(Project.WindowSize()),
		nsgo.WindowConstraints(Project.WindowConstraints()),
		nsgo.Appearance(Project.Appearance()),
	)
}

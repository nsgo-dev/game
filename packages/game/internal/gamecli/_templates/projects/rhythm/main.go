// Command pulselanes is a four-lane rhythm game built with NSGo and game: notes fall to the beat of a song, and the player hits them as they reach the line.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(Project.WindowSize()),
		nsgo.WindowConstraints(Project.WindowConstraints()),
		nsgo.Appearance(Project.Appearance()),
	)
}

// Command mygame is a vertical scrolling shooter built with NSGo and game: enemy waves, bullet patterns, power-ups, a boss with phases, lives and a score.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(Project.WindowSize()),
		nsgo.WindowConstraints(Project.WindowConstraints()),
		nsgo.Appearance(Project.Appearance()),
	)
}

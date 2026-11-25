// Command suitladders is a single-player card game built with NSGo and game: climb four suit ladders from 1 to 9 with a hand of five cards, a draw pile and a discard pile that can be dealt again twice.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(Project.WindowSize()),
		nsgo.WindowConstraints(Project.WindowConstraints()),
		nsgo.Appearance(Project.Appearance()),
	)
}

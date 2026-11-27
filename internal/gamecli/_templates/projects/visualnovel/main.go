// Command mygame is a short branching visual novel built with NSGo and game: two characters, backgrounds, choices that change the ending, a backlog, auto and skip modes, save slots and two languages.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(Project.WindowSize()),
		nsgo.WindowConstraints(Project.WindowConstraints()),
		nsgo.Appearance(Project.Appearance()),
	)
}

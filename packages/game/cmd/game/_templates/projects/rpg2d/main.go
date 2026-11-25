// Command hollowbrook is a small top-down RPG built with NSGo and game: a town and a meadow to walk, people to talk to, a pack of items and equipment, one quest, turn-based battles and three save slots.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(Project.WindowSize()),
		nsgo.WindowConstraints(Project.WindowConstraints()),
		nsgo.Appearance(Project.Appearance()),
	)
}

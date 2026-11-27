// Command relayyard is a first-person shooter arena built with NSGo and game: clear three waves of drones and wardens with a hitscan rifle and a projectile launcher.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(Project.WindowSize()),
		nsgo.WindowConstraints(Project.WindowConstraints()),
		nsgo.Appearance(Project.Appearance()),
	)
}

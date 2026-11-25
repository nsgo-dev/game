// Command hearthkeeper is an idle game built with NSGo and game: keep a fire fed, buy generators and upgrades with the embers it makes, earn while away, scatter the ashes for a lasting bonus and light the Everflame.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root,
		nsgo.WindowSize(Project.WindowSize()),
		nsgo.WindowConstraints(Project.WindowConstraints()),
		nsgo.Appearance(Project.Appearance()),
	)
}

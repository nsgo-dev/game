// Command slidegem is a touch-first 6x6 match-3 sliding-gem puzzle game built
// with the NSGo 2D game SDK (game.Loop, tween, sprite, particles, screen,
// input, sound, save, random) and zero external asset files.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root, nsgo.WindowSize(440, 660))
}

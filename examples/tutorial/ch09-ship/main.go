// Command ch09-ship is chapter 9 of the Star Catcher tutorial, the finished game: an app icon, a name and version for the stores, a pause when the app goes to the background, and release builds for every target.
package main

import "github.com/nsgo-dev/nsgo"

//go:generate go run ./tools/genicon assets/Icon.png

func main() { nsgo.Run(Root) }

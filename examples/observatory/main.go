// Command observatory plays a short visual novel scene with the vn package: two characters' portraits with expressions, a text box with name plates, voice lines, a choice, a backlog, and auto and skip modes. Its portraits and voices are drawn and synthesized in code, so it needs no asset files.
//
// Tap the stage to show the rest of a line or go on, tap a choice to pick it, and use Auto, Skip and Log at the top. Choose to watch the scene again and Skip runs through the lines already read.
package main

import "github.com/nsgo-dev/nsgo"

func main() {
	nsgo.Run(Root, nsgo.WindowSize(width, height))
}

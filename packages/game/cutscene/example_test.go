package cutscene_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/cutscene"
	"github.com/nsgo-dev/nsgo/core"
)

const introFile = `version: 1
source: asset:cutscenes/intro.mp4
group: cutscenes
skippable: true
skipAfter: 1.5s
fit: cover
`

// Parse reads a cutscene file, refusing unknown keys, and Encode writes it back unchanged.
func ExampleParse() {
	c, err := cutscene.Parse([]byte(introFile))
	fmt.Println(err, c.Source, c.Skippable, time.Duration(c.SkipAfter), c.Fit)
	out, _ := cutscene.Encode(c)
	fmt.Println(string(out) == introFile)
	_, err = cutscene.Parse([]byte(introFile + "volume: 0.5\n"))
	fmt.Println(err != nil)
	// Output:
	// <nil> asset:cutscenes/intro.mp4 true 1.5s cover
	// true
	// true
}

// A Player pauses the game's loop while a cutscene plays and sets it back before calling done. Here a root scope stands in for the app and the game skips the cutscene itself; in an app, mount Player.View over the game's view so the video shows and a press, a skip key or back skips it.
func ExamplePlayer() {
	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()
	loop := &game.Loop{Update: func(time.Duration) {}}
	defer loop.Run(root)()
	player := cutscene.NewPlayer(root, cutscene.Options{Loop: loop})

	err := player.Play(cutscene.Cutscene{Source: "asset:cutscenes/intro.mp4", Skippable: true}, func(r cutscene.Result) {
		fmt.Println("done:", r.Reason, "loop paused:", loop.Paused)
	})
	fmt.Println(err, player.Playing(), loop.Paused, player.CanSkip())
	fmt.Println(player.Skip(), player.Playing())

	// A cutscene with no source plays nothing.
	fmt.Println(player.Play(cutscene.Cutscene{}, nil) != nil)
	// Output:
	// <nil> true true true
	// done: skipped loop paused: false
	// true false
	// true
}

package placeholder_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/placeholder"
	"github.com/nsgo-dev/game/packages/game/sprite"
)

// Sheet loads the player's sprite sheet, whose run animation a sprite plays.
func ExampleSheet() {
	sheet, err := placeholder.Sheet("player")
	if err != nil {
		fmt.Println(err)
		return
	}
	run := sheet.Animations["run"]
	fmt.Println(len(sheet.Frames), "frames")
	fmt.Println("run:", len(run.Frames), "frames of", run.Frames[0].Duration, "looping:", run.Loop)
	hero := sprite.NewSprite(run.Frames[0].Frame)
	fmt.Println(hero.Frame.Name, "origin", hero.OriginX, hero.OriginY)
	// Output:
	// 8 frames
	// run: 4 frames of 100ms looping: true
	// player_2 origin 8 8
}

// Level loads the sample level and finds where the player starts.
func ExampleLevel() {
	level, err := placeholder.Level()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(level.Width, "x", level.Height, "tiles of", level.TileWidth)
	for _, o := range level.ObjectLayer("objects").Objects {
		if o.Type == "spawn" {
			fmt.Println("spawn at", o.X, o.Y)
		}
	}
	// Output:
	// 40 x 15 tiles of 16
	// spawn at 40 208
}

// Sound reads a sound effect, ready for NSGo's audio package to load and play.
func ExampleSound() {
	jump, err := placeholder.Sound("jump")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(jump.Data[:4]), string(jump.Data[8:12]))
	fmt.Println(placeholder.License)
	// Output:
	// RIFF WAVE
	// CC0-1.0
}

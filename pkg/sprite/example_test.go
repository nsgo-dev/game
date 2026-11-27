package sprite_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// heroJSON is the json-array data Aseprite exports for a two-frame run cycle tagged "run".
const heroJSON = `{
	"frames": [
		{"filename": "hero 0.aseprite", "frame": {"x": 0, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}, "duration": 100},
		{"filename": "hero 1.aseprite", "frame": {"x": 16, "y": 0, "w": 16, "h": 16}, "sourceSize": {"w": 16, "h": 16}, "duration": 150}
	],
	"meta": {"scale": "1", "frameTags": [{"name": "run", "from": 0, "to": 1, "direction": "forward"}]}
}`

// LoadAtlas reads a TexturePacker or Aseprite export; Aseprite's tags become Animations.
func ExampleLoadAtlas() {
	atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:hero.png"}, []byte(heroJSON))
	if err != nil {
		fmt.Println(err)
		return
	}
	f := atlas.Frames["hero 1.aseprite"]
	fmt.Println(len(atlas.Frames), f.X, f.W)
	run := atlas.Animations["run"]
	fmt.Println(len(run.Frames), run.Loop)
	// Output:
	// 2 16 16
	// 2 true
}

// An Animation advances through its frames on the game's fixed step, each frame for its own duration.
func ExampleAnimation() {
	atlas, _ := sprite.LoadAtlas(core.ImageSource{URI: "asset:hero.png"}, []byte(heroJSON))
	run := atlas.Animations["run"]
	for range 6 {
		run.Update(50 * time.Millisecond)
		fmt.Print(run.Index(), " ")
	}
	fmt.Println()
	// Output:
	// 0 1 1 1 0 0
}

// A Sprite places a frame in the world; its Bounds account for position, origin and scale.
func ExampleSprite() {
	s := sprite.NewSprite(sprite.Frame{W: 16, H: 16, Width: 16, Height: 16})
	s.X, s.Y = 100, 50
	s.OriginX, s.OriginY = 8, 16 // the bottom middle of the 16x16 frame sits at (X, Y)
	s.ScaleX, s.ScaleY = 2, 2
	fmt.Println(s.Bounds())
	// Output:
	// {84 18 32 32}
}

package scene_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// A Manager keeps a stack of scenes: Push pauses the scene below, such as gameplay under a pause menu, and Pop resumes it. Switch replaces the stack, animating the transition over display frames; here a manual clock stands in for the display.
func ExampleManager() {
	clock := core.NewManualClock(time.Unix(0, 0))
	frameWanted := false
	root := core.NewRoot(core.RootOptions{Clock: clock, RequestFrame: func() { frameWanted = true }})
	defer root.Dispose() // disposing the root exits the scene still on the stack
	frame := func(d time.Duration) {
		clock.Advance(d)
		if frameWanted {
			frameWanted = false
			root.RunFrame()
		}
	}

	m := scene.NewManager(root)
	logged := func(name string) *scene.Controller {
		return &scene.Controller{
			Enter:  func(*core.Scope) { fmt.Println("enter", name) },
			Exit:   func() { fmt.Println("exit", name) },
			Pause:  func() { fmt.Println("pause", name) },
			Resume: func() { fmt.Println("resume", name) },
		}
	}
	m.Push(logged("level"))
	m.Push(logged("pause menu"))
	fmt.Println("depth", m.Depth())
	m.Pop()
	m.Switch(logged("title"), scene.Fade(500*time.Millisecond, core.RGB(0x000000)))
	frame(0)
	frame(250 * time.Millisecond)
	fmt.Println(m.Transitioning(), m.TransitionProgress())
	frame(250 * time.Millisecond)
	fmt.Println(m.Transitioning())
	// Output:
	// enter level
	// pause level
	// enter pause menu
	// depth 2
	// exit pause menu
	// resume level
	// enter title
	// true 0.5
	// exit level
	// false
	// exit title
}

// A Scene draws its layers through a camera, and HitTest finds the topmost sprite under a point on the screen.
func ExampleScene_HitTest() {
	cam := &tilemap.Camera{X: 100, Y: 0, Zoom: 1, Width: 320, Height: 180}
	s := scene.NewScene(cam)
	world := s.AddLayer(scene.NewLayer("world", scene.SpaceWorld, 0))
	chest := sprite.NewSprite(sprite.Frame{W: 16, H: 16, Width: 16, Height: 16})
	chest.X, chest.Y = 200, 50
	world.AddBatch(&sprite.Batch{Sprites: []*sprite.Sprite{&chest}})

	viewPoint := cam.WorldToView(core.Point{X: 208, Y: 58})
	hit, ok := s.HitTest(viewPoint)
	fmt.Println(ok, hit.Layer.Name, hit.Sprite == &chest, hit.Point)
	_, ok = s.HitTest(core.Point{X: 0, Y: 0})
	fmt.Println(ok)
	// Output:
	// true world true {208 58}
	// false
}

// A parallax layer scrolls slower than the camera, which makes a background look far away.
func ExampleNewParallaxLayer() {
	hills := scene.NewParallaxLayer("hills", -1, 0.5, 0)
	fmt.Println(hills.Name, hills.Space == scene.SpaceWorld, hills.ParallaxX, hills.ParallaxY)
	// Output:
	// hills true 0.5 0
}

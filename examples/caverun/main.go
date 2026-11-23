// Command caverun is the playable 2D side-scrolling platformer sample for
// github.com/nsgo-dev/game. It demonstrates the full 2D SDK stack — fixed-step
// game.Loop, screen.Viewport, sprite.Atlas/Batch/Animation, tilemap.LoadTiled
// and Camera, physics.MoveAndSlide, particles.Emitter, bitmap text.Font,
// scene.Manager transitions, input.Actions, and sound.Mixer — layered beneath
// native NSGo HUD and menu views.
package main

import (
	"fmt"

	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

func main() {
	nsgo.Run(Root, nsgo.WindowSize(960, 540))
}

// Root builds the Cave Run application root view.
func Root(cx *core.Scope) core.Node {
	return rootWithHook(cx, nil)
}

func rootWithHook(cx *core.Scope, onReady func(*caveRun)) core.Node {
	// The 16:9 cave plays sideways: an iPhone or Android phone turns to landscape and stays there while the game runs.
	cx.RequestOrientation(core.OrientationLandscape)
	g, err := newCaveRun(cx)
	if err != nil {
		return core.View{
			Style: core.Style{
				Width:          core.Pct(100),
				Height:         core.Pct(100),
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Background:     core.RGB(0x111827),
			},
			Children: []core.Node{
				core.Text{
					Content: fmt.Sprintf("Cave Run failed to load assets: %v", err),
					Style:   core.Style{Color: core.RGB(0xf87171), FontSize: 14},
				},
			},
		}
	}
	if onReady != nil {
		onReady(g)
	}
	return g.render(cx)
}

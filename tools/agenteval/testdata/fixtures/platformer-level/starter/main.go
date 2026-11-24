package main

import (
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

func main() {
	nsgo.Run(Root)
}

// Root starts the minimal starter game before the platformer level is built.
func Root(cx *core.Scope) core.Node {
	sc := scene.NewScene(nil)
	sc.AddLayer(scene.NewLayer("terrain", scene.SpaceWorld, 0))
	world := entities.NewWorld()
	world.Attach(cx)

	loop := &game.Loop{
		Rate:   60,
		Update: func(time.Duration) {},
		Draw:   func(float64) {},
	}
	loop.Run(cx)
	_, _ = mcp.Attach(cx, mcp.Config{
		Scene: sc,
		World: world,
		Loop:  loop,
	})
	return core.View{}
}

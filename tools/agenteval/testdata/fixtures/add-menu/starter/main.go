package main

import (
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

func main() {
	nsgo.Run(Root)
}

// Root starts the starter game with only a gameplay scene and no menu.
func Root(cx *core.Scope) core.Node {
	gameplayScene := scene.NewScene(nil)
	gameplayScene.AddLayer(scene.NewLayer("arena", scene.SpaceWorld, 0))
	gameplayCtrl := &scene.Controller{Scene: gameplayScene}

	mgr := scene.NewManager(cx)
	mgr.Push(gameplayCtrl)

	world := entities.NewWorld()
	world.Attach(cx)

	loop := &game.Loop{
		Rate:   60,
		Update: func(dt time.Duration) { mgr.Update(dt) },
		Draw:   func(float64) {},
	}
	loop.Run(cx)

	_, _ = mcp.Attach(cx, mcp.Config{
		Scenes: mgr,
		SceneName: func(c *scene.Controller) string {
			if c == gameplayCtrl {
				return "gameplay"
			}
			return ""
		},
		World: world,
		Loop:  loop,
	})
	return core.View{}
}

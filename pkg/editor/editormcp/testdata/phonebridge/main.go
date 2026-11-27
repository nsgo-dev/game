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

type posComp struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

type healthComp struct {
	HP  int `json:"hp"`
	Max int `json:"max"`
}

func main() {
	nsgo.Run(Root)
}

// Root runs a minimal game with a fixed-step Loop, ECS World, Scene, and mcp.Bridge
// so Play-mode web builds connect to the editor's LAN MCP bridge and answer tool calls.
func Root(cx *core.Scope) core.Node {
	world := entities.NewWorld()
	world.Attach(cx)
	entities.RegisterCodec[posComp](world, "pos")
	entities.RegisterCodec[healthComp](world, "health")

	player := world.Spawn()
	entities.Set(world, player, posComp{X: 64, Y: 96})
	entities.Set(world, player, healthComp{HP: 100, Max: 100})

	gameplay := scene.NewScene(nil)
	gameplay.AddLayer(scene.NewLayer("world", scene.SpaceWorld, 0))
	ctrl := &scene.Controller{Scene: gameplay}
	scenes := scene.NewManager(cx)
	scenes.Push(ctrl)

	loop := &game.Loop{
		Rate:   60,
		Paused: true,
		Update: func(time.Duration) {},
		Draw:   func(float64) {},
	}
	loop.StepNow(5)
	loop.Run(cx)

	_, _ = mcp.Attach(cx, mcp.Config{
		Scenes: scenes,
		SceneName: func(c *scene.Controller) string {
			if c == ctrl {
				return "dungeon_floor_1"
			}
			return ""
		},
		World: world,
		EntityName: func(e entities.Entity) string {
			if e == player {
				return "knight"
			}
			return ""
		},
		Loop: loop,
		StateValues: func() map[string]any {
			return map[string]any{
				"level": "floor_1",
				"score": 420,
			}
		},
	})

	return core.View{
		Children: []core.Node{
			core.Text{Content: "PhoneBridgeTest Ready"},
		},
	}
}

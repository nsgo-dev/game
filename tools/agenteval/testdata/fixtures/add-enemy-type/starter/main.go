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

// Pos stores a 2D position.
type Pos struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

func main() {
	nsgo.Run(Root)
}

// Root starts the starter arena with only the player entity.
func Root(cx *core.Scope) core.Node {
	sc := scene.NewScene(nil)
	sc.AddLayer(scene.NewLayer("actors", scene.SpaceWorld, 10))
	world := entities.NewWorld()
	world.Attach(cx)
	_ = entities.RegisterCodec[Pos](world, "position")

	player := world.Spawn()
	entities.Set(world, player, Pos{X: 40, Y: 100})
	names := map[entities.Entity]string{player: "player"}

	loop := &game.Loop{
		Rate:   60,
		Update: func(time.Duration) {},
		Draw:   func(float64) {},
	}
	loop.Run(cx)
	_, _ = mcp.Attach(cx, mcp.Config{
		Scene:      sc,
		World:      world,
		EntityName: func(e entities.Entity) string { return names[e] },
		Loop:       loop,
	})
	return core.View{}
}

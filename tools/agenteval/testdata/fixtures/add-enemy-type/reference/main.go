package main

import (
	_ "embed"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

//go:embed scenes/arena.scene.yaml
var arenaYAML []byte

// Pos stores a 2D entity position readable over MCP.
type Pos struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

// Enemy holds patrol behavior and health for an enemy entity.
type Enemy struct {
	Kind  string  `json:"kind"`
	HP    int     `json:"hp"`
	Speed float32 `json:"speed"`
	Dir   float32 `json:"dir"`
	MinX  float32 `json:"min_x"`
	MaxX  float32 `json:"max_x"`
}

func main() {
	nsgo.Run(Root)
}

// Root boots the reference arena with a patrolling enemy type exposed over MCP.
func Root(cx *core.Scope) core.Node {
	doc, err := sceneformat.Parse(arenaYAML)
	if err != nil {
		panic(err)
	}

	sc := scene.NewScene(nil)
	sc.AddLayer(scene.NewLayer("actors", scene.SpaceWorld, 10))

	world := entities.NewWorld()
	world.Attach(cx)
	if err := entities.RegisterCodec[Pos](world, "position"); err != nil {
		panic(err)
	}
	if err := entities.RegisterCodec[Enemy](world, "enemy"); err != nil {
		panic(err)
	}

	names := make(map[entities.Entity]string)
	tags := make(map[entities.Entity][]string)
	var enemyEnts []entities.Entity

	for _, layer := range doc.Layers {
		for _, obj := range layer.Objects {
			e := world.Spawn()
			entities.Set(world, e, Pos{X: obj.X, Y: obj.Y})
			displayName := obj.Name
			if displayName == "" {
				displayName = obj.ID
			}
			names[e] = displayName
			if obj.Prefab == "enemy" {
				tags[e] = []string{"enemy"}
				entities.Set(world, e, Enemy{
					Kind:  "slime",
					HP:    3,
					Speed: 48,
					Dir:   1,
					MinX:  120,
					MaxX:  240,
				})
				enemyEnts = append(enemyEnts, e)
			}
		}
	}

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			secs := float32(dt.Seconds())
			for _, e := range enemyEnts {
				pos, okP := entities.Get[Pos](world, e)
				en, okE := entities.Get[Enemy](world, e)
				if !okP || !okE {
					continue
				}
				pos.X += en.Dir * en.Speed * secs
				if pos.X >= en.MaxX {
					pos.X = en.MaxX
					en.Dir = -1
				} else if pos.X <= en.MinX {
					pos.X = en.MinX
					en.Dir = 1
				}
			}
		},
		Draw: func(float64) {},
	}
	loop.Run(cx)

	if _, err := mcp.Attach(cx, mcp.Config{
		Scene:      sc,
		World:      world,
		EntityName: func(e entities.Entity) string { return names[e] },
		EntityTags: func(e entities.Entity) []string { return tags[e] },
		Loop:       loop,
	}); err != nil {
		panic(err)
	}
	return core.View{}
}

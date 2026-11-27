package main

import (
	_ "embed"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/platformer"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

//go:embed scenes/level1.scene.yaml
var level1YAML []byte

// Pos stores a 2D entity position readable over MCP.
type Pos struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

func main() {
	nsgo.Run(Root)
}

// Root boots the reference platformer level and attaches MCP inspection and loop controls.
func Root(cx *core.Scope) core.Node {
	doc, err := sceneformat.Parse(level1YAML)
	if err != nil {
		panic(err)
	}

	cam := &tilemap.Camera{X: 160, Y: 90, Width: 320, Height: 180}
	sc := scene.NewScene(cam)
	terrain := scene.NewLayer("terrain", scene.SpaceWorld, 0)
	actors := scene.NewLayer("actors", scene.SpaceWorld, 10)
	sc.AddLayer(terrain)
	sc.AddLayer(actors)

	world := entities.NewWorld()
	world.Attach(cx)
	if err := entities.RegisterCodec[Pos](world, "position"); err != nil {
		panic(err)
	}

	var (
		spawnX    float32 = 24
		spawnY    float32 = 128
		goalX     float32 = 344
		goalY     float32 = 80
		platforms []*physics.MovingPlatform
	)
	for _, layer := range doc.Layers {
		for _, obj := range layer.Objects {
			switch obj.ID {
			case "player_spawn":
				spawnX, spawnY = obj.X, obj.Y
			case "goal":
				goalX, goalY = obj.X, obj.Y
			default:
				if obj.Prefab == "platform" {
					w := propFloat(obj.Properties, "width", 64)
					h := propFloat(obj.Properties, "height", 16)
					platforms = append(platforms, &physics.MovingPlatform{
						Body: physics.NewBoxBody(obj.X, obj.Y, w, h),
					})
				}
			}
		}
	}

	player := world.Spawn()
	entities.Set(world, player, Pos{X: spawnX, Y: spawnY})
	goal := world.Spawn()
	entities.Set(world, goal, Pos{X: goalX, Y: goalY})

	names := map[entities.Entity]string{
		player: "player",
		goal:   "goal",
	}
	layers := map[entities.Entity]string{
		player: "actors",
		goal:   "actors",
	}

	body := physics.NewCharacterBody(physics.NewBoxBody(spawnX, spawnY, 12, 16))
	ctrl, err := platformer.NewController(body, platformer.DefaultConfig())
	if err != nil {
		panic(err)
	}

	actions := input.NewActions()
	actions.Bind("left", input.Key("ArrowLeft"))
	actions.Bind("right", input.Key("ArrowRight"))
	actions.Bind("jump", input.Key("Space"))
	actions.BindAxis("move", "left", "right")

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			actions.Step()
			in := platformer.Input{
				Move:        float32(actions.Axis("move")),
				JumpPressed: actions.JustPressed("jump"),
				JumpHeld:    actions.Pressed("jump"),
			}
			ctrl.Step(in, nil, nil, platforms, dt)
			b := body.Body.Shape.Bounds()
			if p, ok := entities.Get[Pos](world, player); ok {
				p.X = b.X
				p.Y = b.Y
			}
		},
		Draw: func(float64) {},
	}
	loop.Run(cx)

	if _, err := mcp.Attach(cx, mcp.Config{
		Scene:       sc,
		World:       world,
		EntityName:  func(e entities.Entity) string { return names[e] },
		EntityLayer: func(e entities.Entity) string { return layers[e] },
		Actions:     actions,
		Loop:        loop,
	}); err != nil {
		panic(err)
	}
	return core.View{}
}

func propFloat(m map[string]any, key string, fallback float32) float32 {
	if m == nil {
		return fallback
	}
	switch v := m[key].(type) {
	case int:
		return float32(v)
	case float64:
		return float32(v)
	default:
		return fallback
	}
}

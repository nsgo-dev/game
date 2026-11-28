package gameplugin_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/gameplugin"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

type exampleHealth struct {
	HP int `json:"hp"`
}

func ExampleInstall() {
	var ran []string
	healthPlugin := gameplugin.Plugin{
		Name:   "health",
		Module: "example.com/healthkit",
		API:    gameplugin.APIVersion,
		Components: []sceneformat.ComponentType{{
			Name: "health",
			Fields: []sceneformat.Field{
				{Name: "hp", Kind: sceneformat.FieldInt, Default: 10},
			},
			Spawn: func(w *entities.World, e entities.Entity, fields map[string]any) error {
				entities.Set(w, e, exampleHealth{HP: fields["hp"].(int)})
				return nil
			},
		}},
		Systems: []gameplugin.System{
			{
				Name:  "health.regen",
				Stage: gameplugin.StageUpdate,
				Fn: func(*entities.World, time.Duration) {
					ran = append(ran, "health.regen")
				},
			},
		},
		Codecs: func(w *entities.World) error {
			return entities.RegisterCodec[exampleHealth](w, "health")
		},
	}

	inputPlugin := gameplugin.Plugin{
		Name: "controls",
		API:  gameplugin.APIVersion,
		Systems: []gameplugin.System{
			{
				Name:  "controls.poll",
				Stage: gameplugin.StageInput,
				Fn: func(*entities.World, time.Duration) {
					ran = append(ran, "controls.poll")
				},
			},
		},
	}

	host := &gameplugin.Host{
		Components: sceneformat.NewComponentRegistry(),
		Scheduler:  entities.NewScheduler(),
	}
	if err := gameplugin.Install(host, healthPlugin, inputPlugin); err != nil {
		panic(err)
	}

	world := entities.NewWorld()
	if err := host.OnWorld(world); err != nil {
		panic(err)
	}
	if err := host.Scheduler.Step(world, time.Second/60); err != nil {
		panic(err)
	}

	for _, p := range host.Plugins() {
		fmt.Println("plugin:", p.Name)
	}
	fmt.Println("components:", host.Components.Names())
	fmt.Println("ran:", ran)
	// Output:
	// plugin: health
	// plugin: controls
	// components: [health]
	// ran: [controls.poll health.regen]
}

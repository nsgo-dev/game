package plugin_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/plugin"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

type exampleHealth struct {
	HP int `json:"hp"`
}

func ExampleInstall() {
	var ran []string
	healthPlugin := plugin.Plugin{
		Name:   "health",
		Module: "example.com/healthkit",
		API:    plugin.APIVersion,
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
		Systems: []plugin.System{
			{
				Name:  "health.regen",
				Stage: plugin.StageUpdate,
				Fn: func(*entities.World, time.Duration) {
					ran = append(ran, "health.regen")
				},
			},
		},
		Codecs: func(w *entities.World) error {
			return entities.RegisterCodec[exampleHealth](w, "health")
		},
	}

	inputPlugin := plugin.Plugin{
		Name: "controls",
		API:  plugin.APIVersion,
		Systems: []plugin.System{
			{
				Name:  "controls.poll",
				Stage: plugin.StageInput,
				Fn: func(*entities.World, time.Duration) {
					ran = append(ran, "controls.poll")
				},
			},
		},
	}

	host := &plugin.Host{
		Components: sceneformat.NewComponentRegistry(),
		Scheduler:  entities.NewScheduler(),
	}
	if err := plugin.Install(host, healthPlugin, inputPlugin); err != nil {
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

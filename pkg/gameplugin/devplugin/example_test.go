package devplugin_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/gameplugin"
	"github.com/nsgo-dev/game/pkg/gameplugin/devplugin"
	"github.com/nsgo-dev/game/pkg/mcp"
)

func ExampleInstall() {
	bridge := mcp.New(mcp.Config{})
	host := &devplugin.Host{Bridge: bridge}

	kit := devplugin.Plugin{
		Name:   "healthkit",
		Module: "example.com/healthkit",
		API:    gameplugin.APIVersion,
		Prefabs: []devplugin.PrefabSpec{
			{Prefab: entities.NewPrefab("patient")},
		},
	}

	if err := devplugin.Install(host, kit); err != nil {
		panic(err)
	}
	fmt.Println("installed:", host.Has("healthkit"))
	fmt.Println("has patient prefab:", bridge.HasPrefab("patient"))
	// Output:
	// installed: true
	// has patient prefab: true
}

package devtools_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/game/pkg/plugin"
	"github.com/nsgo-dev/game/pkg/plugin/devtools"
)

func ExampleInstall() {
	bridge := mcp.New(mcp.Config{})
	host := &devtools.Host{Bridge: bridge}

	kit := devtools.Plugin{
		Name:   "healthkit",
		Module: "example.com/healthkit",
		API:    plugin.APIVersion,
		Prefabs: []devtools.PrefabSpec{
			{Prefab: entities.NewPrefab("patient")},
		},
	}

	if err := devtools.Install(host, kit); err != nil {
		panic(err)
	}
	fmt.Println("installed:", host.Has("healthkit"))
	fmt.Println("has patient prefab:", bridge.HasPrefab("patient"))
	// Output:
	// installed: true
	// has patient prefab: true
}

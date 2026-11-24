package mcp_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/mcp"
)

// FormatEntityRef and ParseEntityRef convert live generational entity handles to and from stable MCP references.
func ExampleFormatEntityRef() {
	w := entities.NewWorld()
	defer w.Dispose()

	e := w.Spawn()
	ref := mcp.FormatEntityRef(e)
	parsed, err := mcp.ParseEntityRef(ref)
	fmt.Println(ref, err == nil, parsed == e)
	// Output:
	// entity:0:1 true true
}

// New configures an MCP Bridge and registers custom component inspectors before attaching to a core.Scope.
func ExampleNew() {
	type Velocity struct {
		VX, VY float32
	}

	w := entities.NewWorld()
	defer w.Dispose()

	bridge := mcp.New(mcp.Config{World: w})
	_ = mcp.RegisterComponent[Velocity](bridge, "velocity")
	for _, tool := range bridge.Tools() {
		fmt.Println(tool.Name)
	}
	// Output:
	// game_scenes_list
	// game_layers_list
	// game_entities_list
	// game_entity_inspect
	// game_component_get
}

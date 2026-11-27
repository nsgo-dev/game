package mcp_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/mcp"
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
	// game_state_dump
	// game_state_capture
	// game_state_diff
	// game_loop_status
	// game_loop_pause
	// game_loop_resume
	// game_loop_step
	// game_loop_time_scale
	// game_loop_seed
	// game_input_action
	// game_input_axis
	// game_input_pointer
	// game_input_actions_list
	// game_tunables_list
	// game_tunable_get
	// game_tunable_set
	// game_console_commands_list
	// game_console_exec
	// game_vars_list
	// game_var_get
	// game_var_set
	// game_prefabs_list
	// game_entity_spawn
	// game_entity_destroy
	// game_component_set
	// game_component_remove
	// game_replay_status
	// game_replay_record_start
	// game_replay_record_stop
	// game_replay_play
	// game_replay_stop
	// game_replay_list
	// game_perf_readout
	// game_perf_zones
	// game_perf_reset
	// game_sound_readout
	// game_assets_list
	// game_asset_reload
	// game_assets_reload_group
	// game_screenshot
	// game_log_read
}

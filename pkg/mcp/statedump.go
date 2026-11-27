package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/statedump"
	"github.com/nsgo-dev/nsgo/automation"
)

// ToolStateDump captures a deterministic snapshot of the running game's entities,
// registered components, scene state, loop counters, and custom state values as both
// structured JSON and canonical statedump YAML.
const ToolStateDump = "game_state_dump"

// StateDumpResult is the JSON result of game_state_dump.
type StateDumpResult struct {
	Version         int                        `json:"version"`
	Rate            int                        `json:"rate,omitempty"`
	Steps           uint64                     `json:"steps,omitempty"`
	GameTimeSeconds float64                    `json:"game_time_seconds,omitempty"`
	Scene           *statedump.SceneState      `json:"scene,omitempty"`
	Entities        []entities.EntitySnapshot  `json:"entities,omitempty"`
	Values          map[string]json.RawMessage `json:"values,omitempty"`
	YAML            string                     `json:"yaml"`
}

var schemaStateDump = json.RawMessage(`{
	"type": "object",
	"additionalProperties": false,
	"properties": {}
}`)

func (b *Bridge) stateDumpTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolStateDump,
			Description: "Capture a deterministic state dump of the running game's entities, codec components, scene state, loop step count/rate, and custom state values as both structured JSON and canonical statedump YAML.",
			InputSchema: schemaStateDump,
			Handler:     b.handleStateDump,
		},
	}
}

func (b *Bridge) handleStateDump(_ context.Context, raw json.RawMessage) (any, error) {
	if _, err := decodeArgs[struct{}](raw); err != nil {
		return nil, err
	}
	dump, err := statedump.Capture(statedump.Source{
		World:     b.activeWorld(),
		Scenes:    b.cfg.Scenes,
		SceneName: b.cfg.SceneName,
		Values:    b.cfg.StateValues,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp: state dump: %w", err)
	}
	if dump.Scene == nil && b.standaloneScene() != nil {
		dump.Scene = &statedump.SceneState{
			Depth:   1,
			Current: "main",
		}
	}
	var (
		steps           uint64
		gameTimeSeconds float64
	)
	if loop := b.activeLoop(); loop != nil {
		dump.Rate = loop.StepRate()
		steps = loop.Steps()
		if steps > uint64(math.MaxInt) {
			dump.Steps = math.MaxInt
		} else {
			dump.Steps = int(steps)
		}
		gameTimeSeconds = loop.ElapsedSeconds()
	}
	yamlBytes, err := statedump.Encode(dump)
	if err != nil {
		return nil, fmt.Errorf("mcp: encode state dump: %w", err)
	}
	return StateDumpResult{
		Version:         dump.Version,
		Rate:            dump.Rate,
		Steps:           steps,
		GameTimeSeconds: gameTimeSeconds,
		Scene:           dump.Scene,
		Entities:        dump.Entities,
		Values:          dump.Values,
		YAML:            string(yamlBytes),
	}, nil
}

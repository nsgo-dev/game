package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/automation"
)

// ToolSoundReadout reads live active voice counts, global voice budget, per-concurrency-group counts and counters,
// and per-bus ducking gains from the configured sound.Mixer.
const ToolSoundReadout = "game_sound_readout"

var schemaSoundReadout = json.RawMessage(`{
	"type": "object",
	"additionalProperties": false,
	"properties": {}
}`)

func (b *Bridge) soundTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolSoundReadout,
			Description: "Read live audio voice telemetry matching the debug overlay: active and fading voice counts, global voice budget, per-concurrency-group counts and refusal/steal/retrigger counters, and per-bus ducking gain multipliers.",
			InputSchema: schemaSoundReadout,
			Handler:     b.handleSoundReadout,
		},
	}
}

func (b *Bridge) activeSound() *sound.Mixer {
	if b == nil {
		return nil
	}
	if b.cfg.SoundFunc != nil {
		if m := b.cfg.SoundFunc(); m != nil {
			return m
		}
	}
	if b.cfg.Sound != nil {
		return b.cfg.Sound
	}
	if b.cfg.Overlay != nil {
		return b.cfg.Overlay.Sound()
	}
	return nil
}

func (b *Bridge) handleSoundReadout(_ context.Context, raw json.RawMessage) (any, error) {
	if len(raw) > 0 && string(raw) != "null" && string(raw) != "{}" {
		var empty struct{}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&empty); err != nil {
			return nil, err
		}
	}
	m := b.activeSound()
	if m == nil {
		return nil, errors.New("mcp: no sound mixer configured")
	}
	return m.DebugSnapshot(), nil
}

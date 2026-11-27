//go:build js && wasm

package game

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/nsgo-dev/game/internal/webmcpbridge"
	"github.com/nsgo-dev/nsgo/core"
)

func (l *Loop) attachWebBridge(cx *core.Scope) func() {
	if l == nil || cx == nil {
		return func() {}
	}
	return webmcpbridge.Register(webmcpbridge.Config{
		Source:   "game.Loop",
		Priority: webmcpbridge.PriorityLoop,
		Post:     cx.Post,
		ListTools: func() []webmcpbridge.ToolInfo {
			return []webmcpbridge.ToolInfo{
				{Name: "game_state_dump", Description: "Capture the running game's state dump (loop rate, steps and YAML)."},
				{Name: "game_loop_status", Description: "Report the game loop's paused state, time scale, step rate, steps and elapsed seconds."},
				{Name: "game_loop_pause", Description: "Pause the game loop's fixed updates."},
				{Name: "game_loop_resume", Description: "Resume the game loop's fixed updates."},
				{Name: "game_loop_step", Description: "Synchronously advance the game loop by count fixed updates."},
				{Name: "game_loop_time_scale", Description: "Set the game loop's simulation time scale."},
			}
		},
		CallTool: func(_ context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
			return l.callWebTool(name, args)
		},
	})
}

func (l *Loop) callWebTool(name string, raw json.RawMessage) (json.RawMessage, error) {
	status := func() map[string]any {
		return map[string]any{
			"paused":            l.Paused,
			"time_scale":        l.EffectiveTimeScale(),
			"step_seconds":      l.StepDuration().Seconds(),
			"step_hz":           l.StepRate(),
			"steps":             l.Steps(),
			"game_time_seconds": l.ElapsedSeconds(),
			"seed_configured":   false,
		}
	}
	decodeStrict := func(dst any) error {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			return nil
		}
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.DisallowUnknownFields()
		if err := dec.Decode(dst); err != nil {
			return fmt.Errorf("game: invalid arguments: %w", err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return errors.New("game: invalid arguments: unexpected trailing data")
		}
		return nil
	}

	switch name {
	case "game_state_dump":
		var in struct{}
		if err := decodeStrict(&in); err != nil {
			return nil, err
		}
		rate := l.StepRate()
		steps := l.Steps()
		yamlText := fmt.Sprintf("version: 1\nrate: %d\nsteps: %d\n", rate, steps)
		return json.Marshal(map[string]any{
			"version":           1,
			"rate":              rate,
			"steps":             steps,
			"game_time_seconds": l.ElapsedSeconds(),
			"yaml":              yamlText,
		})
	case "game_loop_status":
		var in struct{}
		if err := decodeStrict(&in); err != nil {
			return nil, err
		}
		return json.Marshal(status())
	case "game_loop_pause":
		var in struct{}
		if err := decodeStrict(&in); err != nil {
			return nil, err
		}
		l.SetPaused(true)
		return json.Marshal(status())
	case "game_loop_resume":
		var in struct{}
		if err := decodeStrict(&in); err != nil {
			return nil, err
		}
		l.SetPaused(false)
		return json.Marshal(status())
	case "game_loop_step":
		var in struct {
			Count *int `json:"count"`
		}
		if err := decodeStrict(&in); err != nil {
			return nil, err
		}
		count := 1
		if in.Count != nil {
			count = *in.Count
		}
		if count < 1 || count > 3600 {
			return nil, fmt.Errorf("game_loop_step: count must be between 1 and 3600, got %d", count)
		}
		before := l.ElapsedSeconds()
		ran := l.StepNow(count)
		after := l.ElapsedSeconds()
		return json.Marshal(map[string]any{
			"steps_advanced":    ran,
			"total_steps":       l.Steps(),
			"game_time_seconds": after,
			"delta_seconds":     after - before,
			"paused":            l.Paused,
		})
	case "game_loop_time_scale":
		var in struct {
			TimeScale *float64 `json:"time_scale"`
		}
		if err := decodeStrict(&in); err != nil {
			return nil, err
		}
		if in.TimeScale == nil || math.IsNaN(*in.TimeScale) || math.IsInf(*in.TimeScale, 0) || *in.TimeScale < 0 {
			return nil, errors.New("game_loop_time_scale: time_scale must be a finite number >= 0")
		}
		l.SetTimeScale(*in.TimeScale)
		return json.Marshal(status())
	default:
		return nil, fmt.Errorf("game: tool %q is not available without mcp.Attach", name)
	}
}

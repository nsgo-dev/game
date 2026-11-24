package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/nsgo/automation"
)

// Loop-control tool names registered by Bridge with github.com/nsgo-dev/nsgo/automation.
const (
	// ToolLoopStatus reports the game loop's current paused state, time scale, step rate, step count and simulated game time.
	ToolLoopStatus = "game_loop_status"
	// ToolLoopPause pauses the game loop's fixed updates without accumulating a catch-up backlog.
	ToolLoopPause = "game_loop_pause"
	// ToolLoopResume resumes a paused game loop.
	ToolLoopResume = "game_loop_resume"
	// ToolLoopStep synchronously advances the game loop by a requested number of fixed updates.
	ToolLoopStep = "game_loop_step"
	// ToolLoopTimeScale sets the game loop's simulation time scale.
	ToolLoopTimeScale = "game_loop_time_scale"
	// ToolLoopSeed sets the game's deterministic random seed.
	ToolLoopSeed = "game_loop_seed"
)

// MaxLoopSteps is the maximum number of fixed updates a single game_loop_step call may advance.
const MaxLoopSteps = 3600

// LoopStatusResult is the JSON result of game_loop_status, game_loop_pause,
// game_loop_resume and game_loop_time_scale.
type LoopStatusResult struct {
	Paused          bool    `json:"paused"`
	TimeScale       float64 `json:"time_scale"`
	StepSeconds     float64 `json:"step_seconds"`
	StepHz          int     `json:"step_hz"`
	Steps           uint64  `json:"steps"`
	GameTimeSeconds float64 `json:"game_time_seconds"`
	SeedConfigured  bool    `json:"seed_configured"`
}

// LoopStepResult is the JSON result of game_loop_step.
type LoopStepResult struct {
	StepsAdvanced   int     `json:"steps_advanced"`
	TotalSteps      uint64  `json:"total_steps"`
	GameTimeSeconds float64 `json:"game_time_seconds"`
	DeltaSeconds    float64 `json:"delta_seconds"`
	Paused          bool    `json:"paused"`
}

// LoopSeedResult is the JSON result of game_loop_seed.
type LoopSeedResult struct {
	Seed uint64 `json:"seed"`
}

var (
	schemaLoopEmpty = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {}
	}`)

	schemaLoopStep = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"count": {
				"type": "integer",
				"minimum": 1,
				"maximum": 3600,
				"description": "Number of fixed updates to advance synchronously (default 1, max 3600)."
			}
		}
	}`)

	schemaLoopTimeScale = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["time_scale"],
		"properties": {
			"time_scale": {
				"type": "number",
				"minimum": 0,
				"description": "Simulation time scale (finite, >= 0; 0 resets to the default 1x speed)."
			}
		}
	}`)

	schemaLoopSeed = json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["seed"],
		"properties": {
			"seed": {
				"type": "integer",
				"minimum": 0,
				"description": "Unsigned 64-bit deterministic random seed."
			}
		}
	}`)
)

// SetLoop replaces the static Loop configured on b.
// If Config.LoopFunc is set and returns a non-nil loop, LoopFunc continues to take precedence.
func (b *Bridge) SetLoop(l *game.Loop) {
	if b == nil {
		return
	}
	b.cfg.Loop = l
}

func (b *Bridge) loopTools() []automation.Tool {
	return []automation.Tool{
		{
			Name:        ToolLoopStatus,
			Description: "Report the game loop's paused state, effective time scale, fixed step duration and rate, total steps, simulated game time and whether random seeding is configured.",
			InputSchema: schemaLoopEmpty,
			Handler:     b.handleLoopStatus,
		},
		{
			Name:        ToolLoopPause,
			Description: "Pause the game loop's fixed updates without accumulating a catch-up backlog, returning the updated loop status.",
			InputSchema: schemaLoopEmpty,
			Handler:     b.handleLoopPause,
		},
		{
			Name:        ToolLoopResume,
			Description: "Resume the game loop's fixed updates, returning the updated loop status.",
			InputSchema: schemaLoopEmpty,
			Handler:     b.handleLoopResume,
		},
		{
			Name:        ToolLoopStep,
			Description: "Synchronously advance the game loop by count fixed updates (default 1, max 3600), whether paused or running, advancing exact simulated game time.",
			InputSchema: schemaLoopStep,
			Handler:     b.handleLoopStep,
		},
		{
			Name:        ToolLoopTimeScale,
			Description: "Set the game loop's simulation time scale (finite, >= 0; 0 resets to default 1x speed), returning the updated loop status.",
			InputSchema: schemaLoopTimeScale,
			Handler:     b.handleLoopTimeScale,
		},
		{
			Name:        ToolLoopSeed,
			Description: "Set the game's deterministic random seed via the configured random stream, stream manager or seed callback.",
			InputSchema: schemaLoopSeed,
			Handler:     b.handleLoopSeed,
		},
	}
}

func (b *Bridge) activeLoop() *game.Loop {
	if b == nil {
		return nil
	}
	if b.cfg.LoopFunc != nil {
		if l := b.cfg.LoopFunc(); l != nil {
			return l
		}
	}
	return b.cfg.Loop
}

func (b *Bridge) requireLoop() (*game.Loop, error) {
	l := b.activeLoop()
	if l == nil {
		return nil, errors.New("mcp: no game loop configured")
	}
	return l, nil
}

func (b *Bridge) seedConfigured() bool {
	if b == nil {
		return false
	}
	return b.cfg.Random != nil || b.cfg.RandomStreams != nil || b.cfg.SetSeed != nil
}

func (b *Bridge) loopStatus(l *game.Loop) LoopStatusResult {
	hz := l.StepRate()
	return LoopStatusResult{
		Paused:          l.Paused,
		TimeScale:       l.EffectiveTimeScale(),
		StepSeconds:     1.0 / float64(hz),
		StepHz:          hz,
		Steps:           l.Steps(),
		GameTimeSeconds: l.ElapsedSeconds(),
		SeedConfigured:  b.seedConfigured(),
	}
}

func (b *Bridge) handleLoopStatus(_ context.Context, raw json.RawMessage) (any, error) {
	if _, err := decodeArgs[struct{}](raw); err != nil {
		return nil, err
	}
	l, err := b.requireLoop()
	if err != nil {
		return nil, err
	}
	return b.loopStatus(l), nil
}

func (b *Bridge) handleLoopPause(_ context.Context, raw json.RawMessage) (any, error) {
	if _, err := decodeArgs[struct{}](raw); err != nil {
		return nil, err
	}
	l, err := b.requireLoop()
	if err != nil {
		return nil, err
	}
	l.SetPaused(true)
	return b.loopStatus(l), nil
}

func (b *Bridge) handleLoopResume(_ context.Context, raw json.RawMessage) (any, error) {
	if _, err := decodeArgs[struct{}](raw); err != nil {
		return nil, err
	}
	l, err := b.requireLoop()
	if err != nil {
		return nil, err
	}
	l.SetPaused(false)
	return b.loopStatus(l), nil
}

type loopStepArgs struct {
	Count *int `json:"count"`
}

func (b *Bridge) handleLoopStep(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[loopStepArgs](raw)
	if err != nil {
		return nil, err
	}
	count := 1
	if args.Count != nil {
		count = *args.Count
	}
	if count < 1 || count > MaxLoopSteps {
		return nil, fmt.Errorf("mcp: count must be between 1 and %d (got %d)", MaxLoopSteps, count)
	}
	l, err := b.requireLoop()
	if err != nil {
		return nil, err
	}

	rateBefore := l.StepRate()
	elapsedBefore := l.ElapsedSeconds()
	ran := l.StepNow(count)
	elapsedAfter := l.ElapsedSeconds()

	delta := elapsedAfter - elapsedBefore
	if l.StepRate() == rateBefore {
		exact := float64(ran) / float64(rateBefore)
		if math.Abs(delta-exact) <= 1e-9 {
			delta = exact
		}
	}

	return LoopStepResult{
		StepsAdvanced:   ran,
		TotalSteps:      l.Steps(),
		GameTimeSeconds: elapsedAfter,
		DeltaSeconds:    delta,
		Paused:          l.Paused,
	}, nil
}

type loopTimeScaleArgs struct {
	TimeScale *float64 `json:"time_scale"`
}

func (b *Bridge) handleLoopTimeScale(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[loopTimeScaleArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.TimeScale == nil {
		return nil, errors.New("mcp: time_scale is required")
	}
	scale := *args.TimeScale
	if math.IsNaN(scale) || math.IsInf(scale, 0) || scale < 0 {
		return nil, fmt.Errorf("mcp: time_scale must be finite and >= 0 (got %v)", scale)
	}
	if scale == 0 {
		scale = 0
	}
	l, err := b.requireLoop()
	if err != nil {
		return nil, err
	}
	l.SetTimeScale(scale)
	return b.loopStatus(l), nil
}

type loopSeedArgs struct {
	Seed *uint64 `json:"seed"`
}

func (b *Bridge) handleLoopSeed(_ context.Context, raw json.RawMessage) (any, error) {
	args, err := decodeArgs[loopSeedArgs](raw)
	if err != nil {
		return nil, err
	}
	if args.Seed == nil {
		return nil, errors.New("mcp: seed is required")
	}
	if !b.seedConfigured() {
		return nil, errors.New("mcp: no random seed target configured (set Config.Random, Config.RandomStreams, or Config.SetSeed)")
	}
	seed := *args.Seed
	if b.cfg.Random != nil {
		b.cfg.Random.SetSeed(seed)
	}
	if b.cfg.RandomStreams != nil {
		b.cfg.RandomStreams.SetSeed(seed)
	}
	if b.cfg.SetSeed != nil {
		b.cfg.SetSeed(seed)
	}
	return LoopSeedResult{Seed: seed}, nil
}

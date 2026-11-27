package playtest

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/nsgo-dev/game/pkg/random"
	"github.com/nsgo-dev/game/pkg/replay"
)

// InputState holds the action presses and axis values a Bot chooses for one fixed simulation step.
type InputState struct {
	// Actions maps named actions (such as "left", "right", "jump") to whether they are held on this step.
	Actions map[string]bool
	// Axes maps named axes (such as "move_x") to their value on this step.
	Axes map[string]float64
}

// Bot decides one fixed step of player inputs from the game's observation and a deterministic random stream.
type Bot interface {
	// Decide returns the inputs to hold for the given zero-based simulation step.
	Decide(step int, dt time.Duration, obs any, rng *random.Stream) InputState
}

// BotChannels is optionally implemented by a Bot to declare the default action and axis names recorded into each run's replay.Recording.
type BotChannels interface {
	Channels() (actions, axes []string)
}

// BotFactory creates a fresh Bot instance for one seeded run from the playtest BotConfig.
type BotFactory func(cfg BotConfig) (Bot, error)

var (
	botRegistryMu sync.RWMutex
	botRegistry   = map[string]BotFactory{}
)

// RegisterBot registers a named bot profile factory so playtest.yaml files can select it by bot.profile.
// It panics if name is empty or factory is nil.
func RegisterBot(name string, factory BotFactory) {
	if name == "" {
		panic("playtest: empty bot profile name")
	}
	if factory == nil {
		panic("playtest: nil bot factory for " + name)
	}
	botRegistryMu.Lock()
	botRegistry[name] = factory
	botRegistryMu.Unlock()
}

// NewBot instantiates the Bot named by cfg.Profile from the registered bot profiles.
func NewBot(cfg BotConfig) (Bot, error) {
	botRegistryMu.RLock()
	factory, ok := botRegistry[cfg.Profile]
	botRegistryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("playtest: unknown bot profile %q (available: %v)", cfg.Profile, BotProfiles())
	}
	return factory(cfg)
}

// BotProfiles returns the sorted names of all registered bot profiles.
func BotProfiles() []string {
	botRegistryMu.RLock()
	defer botRegistryMu.RUnlock()
	return slices.Sorted(maps.Keys(botRegistry))
}

var _ replay.ActionSource = (*BotActionSource)(nil)

// BotActionSource adapts a Bot and an observation callback into a replay.ActionSource.
// Wrapping a BotActionSource in replay.NewRecorder lets the game step through replay.ActionSource unchanged while every bot decision is recorded into a deterministic replay.Recording.
type BotActionSource struct {
	bot     Bot
	observe func() any
	rng     *random.Stream
	dt      time.Duration
	step    int

	prevActions map[string]bool
	currActions map[string]bool
	currAxes    map[string]float64
}

// NewBotActionSource returns an ActionSource driven by bot with deterministic stream rng and fixed step duration dt.
func NewBotActionSource(bot Bot, observe func() any, rng *random.Stream, dt time.Duration) *BotActionSource {
	if dt <= 0 {
		dt = time.Second / defaultStepRate
	}
	return &BotActionSource{
		bot:         bot,
		observe:     observe,
		rng:         rng,
		dt:          dt,
		prevActions: make(map[string]bool),
		currActions: make(map[string]bool),
		currAxes:    make(map[string]float64),
	}
}

// Step queries the bot for the current step's InputState and updates the action and axis snapshots.
func (b *BotActionSource) Step() {
	clear(b.prevActions)
	for k, v := range b.currActions {
		if v {
			b.prevActions[k] = true
		}
	}
	clear(b.currActions)
	clear(b.currAxes)

	var obs any
	if b.observe != nil {
		obs = b.observe()
	}
	if b.bot != nil {
		state := b.bot.Decide(b.step, b.dt, obs, b.rng)
		for k, v := range state.Actions {
			if v {
				b.currActions[k] = true
			}
		}
		for k, v := range state.Axes {
			if isFinite(v) && v != 0 {
				b.currAxes[k] = cleanZero(v)
			}
		}
	}
	b.step++
}

// Pressed reports whether action name is pressed on the current step.
func (b *BotActionSource) Pressed(name string) bool {
	return b.currActions[name]
}

// JustPressed reports whether action name transitioned from released to pressed on the current step.
func (b *BotActionSource) JustPressed(name string) bool {
	return b.currActions[name] && !b.prevActions[name]
}

// JustReleased reports whether action name transitioned from pressed to released on the current step.
func (b *BotActionSource) JustReleased(name string) bool {
	return !b.currActions[name] && b.prevActions[name]
}

// Axis returns the value of axis name on the current step, falling back to 1 when action name is pressed, matching input.Actions and replay.Player.
func (b *BotActionSource) Axis(name string) float64 {
	if v, ok := b.currAxes[name]; ok {
		return v
	}
	if b.currActions[name] {
		return 1
	}
	return 0
}

// StepIndex returns how many steps have been evaluated.
func (b *BotActionSource) StepIndex() int {
	return b.step
}

func clampFloat(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return min(max(v, lo), hi)
}

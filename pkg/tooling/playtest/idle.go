package playtest

import (
	"time"

	"github.com/nsgo-dev/game/pkg/random"
)

// ProfileIdle is the built-in no-input bot used by idle-loss baselines.
const ProfileIdle = "idle"

func init() { RegisterBot(ProfileIdle, NewIdle) }

// Idle never presses an action or moves an axis.
type Idle struct{}

// NewIdle builds the idle bot; it ignores cfg because the bot has no parameters.
func NewIdle(_ BotConfig) (Bot, error) { return Idle{}, nil }

// Decide returns an empty InputState on every frame without reading the observation.
func (Idle) Decide(_ int, _ time.Duration, _ any, _ *random.Stream) InputState { return InputState{} }

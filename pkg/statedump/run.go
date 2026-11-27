package statedump

import (
	"errors"
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/scene"
)

// Source names the parts of a game that Capture reads. Every field is optional.
type Source struct {
	// World is the entity world; only components registered with entities.RegisterCodec appear in the dump.
	World *entities.World
	// Scenes is the game's scene stack.
	Scenes *scene.Manager
	// SceneName names a scene controller for SceneState.Current; nil leaves Current empty.
	SceneName func(*scene.Controller) string
	// Values returns game-wide state outside the world, each value encoded with encoding/json.
	Values func() map[string]any
}

// Sim is a headless simulation that Run steps: Step advances it by one fixed step and Source says what to dump afterwards.
type Sim struct {
	Source
	// Step advances the simulation by dt; like a game.Loop's Update, it must call the ActionSource's Step once, or Run returns an error.
	Step func(dt time.Duration)
}

// Capture returns the state src names as a Dump with FormatVersion and no seed, rate or step count; the caller fills those in when it knows them.
func Capture(src Source) (*Dump, error) {
	d := &Dump{Version: FormatVersion}
	if src.World != nil {
		snap, err := src.World.Snapshot()
		if err != nil {
			return nil, fmt.Errorf("statedump: %w", err)
		}
		if len(snap.Entities) > 0 {
			d.Entities = snap.Entities
		}
	}
	if src.Scenes != nil {
		s := &SceneState{
			Depth:         src.Scenes.Depth(),
			Transitioning: src.Scenes.Transitioning(),
		}
		if s.Transitioning {
			s.Progress = src.Scenes.TransitionProgress()
		}
		if cur := src.Scenes.Current(); cur != nil && src.SceneName != nil {
			s.Current = src.SceneName(cur)
		}
		d.Scene = s
	}
	if src.Values != nil {
		for name, v := range src.Values() {
			if err := d.SetValue(name, v); err != nil {
				return nil, err
			}
		}
	}
	if err := Validate(d); err != nil {
		return nil, err
	}
	return d, nil
}

// Run plays rec headless against the simulation makeSim builds and returns the state after rec.TotalSteps steps. It returns an error when the steps did not advance the ActionSource once each, because a sim that never reads the recording would otherwise dump the same state for any input.
//
// makeSim receives rec.Seed and a *replay.Player as its ActionSource, as with replay.Run, but Run needs no testing.TB, so a command or an agent's tool can call it. Each step passes rec.StepDuration(), the fixed dt of a game.Loop at rec.Rate.
func Run(rec *replay.Recording, makeSim func(seed uint64, src replay.ActionSource) (Sim, error)) (*Dump, error) {
	if err := rec.Validate(); err != nil {
		return nil, fmt.Errorf("statedump: %w", err)
	}
	if makeSim == nil {
		return nil, errors.New("statedump: nil makeSim")
	}
	player := replay.NewPlayer(rec)
	sim, err := makeSim(rec.Seed, player)
	if err != nil {
		return nil, fmt.Errorf("statedump: make sim: %w", err)
	}
	if sim.Step == nil {
		return nil, errors.New("statedump: sim has no Step")
	}
	dt := rec.StepDuration()
	for range rec.TotalSteps {
		sim.Step(dt)
	}
	// A sim that skips src.Step ignores every recorded input, and its dump would pass for a faithful replay.
	if got := player.StepIndex(); got != rec.TotalSteps {
		return nil, fmt.Errorf("statedump: sim stepped the ActionSource %d times, want %d", got, rec.TotalSteps)
	}
	d, err := Capture(sim.Source)
	if err != nil {
		return nil, err
	}
	d.Seed = rec.Seed
	d.Rate = rec.Rate
	d.Steps = rec.TotalSteps
	return d, nil
}

package stats

// GameplayEffect applies a named definition to a sheet with deterministic
// fixed-step duration and periodic ticks.
type GameplayEffect struct {
	Definition Effect
	Handle     ID
	Remaining  int
	NextTick   int
	Stacks     int
}

func (s *Sheet) ApplyEffect(e Effect, source string) (GameplayEffect, error) {
	if e.Kind == "" {
		e.Kind = "timed"
	}
	if e.Kind != "instant" && e.Kind != "timed" && e.Kind != "infinite" {
		return GameplayEffect{}, ErrInvalidSteps
	}
	if e.Period < 0 {
		return GameplayEffect{}, ErrInvalidSteps
	}
	g := GameplayEffect{Definition: e, Remaining: e.Steps, NextTick: e.Period, Stacks: 1}
	for _, m := range e.Modifiers {
		id, err := s.AddTimed(source, m, e.Steps)
		if err != nil {
			return GameplayEffect{}, err
		}
		if g.Handle == 0 {
			g.Handle = id
		}
	}
	return g, nil
}

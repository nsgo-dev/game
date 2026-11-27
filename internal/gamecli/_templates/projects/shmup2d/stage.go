package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"time"

	"go.yaml.in/yaml/v3"
)

// stageVersion is the stage file schema version that parseStage accepts and encodeStage writes.
const stageVersion = 1

// Paths an enemy can fly, as an enemy kind's path field names them.
const (
	// pathDive flies straight down at speed.
	pathDive = "dive"
	// pathWeave flies down at speed, swaying amplitude points either side of where it came in, once every period.
	pathWeave = "weave"
	// pathCross flies across the screen at speed, rightwards when it comes in on the left half and leftwards on the right half, drifting down at a fifth of its speed.
	pathCross = "cross"
	// pathHover flies down to stopY, holds there for hold and then leaves downwards.
	pathHover = "hover"
)

// Items an enemy wave can drop, as a wave's drop field names them.
const (
	// itemPower raises the ship's weapon by one level, up to the last weapon.
	itemPower = "power"
	// itemBomb gives the ship one more bomb.
	itemBomb = "bomb"
	// itemLife gives the ship one more life.
	itemLife = "life"
)

// Ways a boss moves in a phase, as a phase's move field names them.
const (
	// moveHold keeps the boss where it is.
	moveHold = "hold"
	// moveSway swings the boss from side to side across the top of the screen.
	moveSway = "sway"
	// moveChase drifts the boss towards the ship's side of the screen.
	moveChase = "chase"
)

// stageFile is a stage: the ship, the enemy kinds, the waves that send them in and the boss that ends it. It is written in YAML, data/stage.yaml, so a designer edits a stage without touching Go.
type stageFile struct {
	// Version is the schema version; it must equal stageVersion.
	Version int `yaml:"version"`
	// Name is what the stage is called, shown as it starts.
	Name string `yaml:"name"`
	// Seed seeds every enemy's bullet patterns, so a stage plays out the same way for the same inputs.
	Seed uint64 `yaml:"seed"`
	// ScrollSpeed is how fast the starfield scrolls past, in points a second.
	ScrollSpeed float32 `yaml:"scrollSpeed"`
	// Ship is the player's ship.
	Ship shipDef `yaml:"ship"`
	// Enemies are the enemy kinds the waves send, each with a unique name.
	Enemies []enemyDef `yaml:"enemies"`
	// Waves are the enemy waves, in the order they arrive.
	Waves []waveDef `yaml:"waves"`
	// Boss is the boss that arrives after the last wave; defeating it clears the stage.
	Boss bossDef `yaml:"boss"`
}

// shipDef is the player's ship: how it moves, what it fires and what it starts with.
type shipDef struct {
	// Speed is how fast the ship moves, in points a second.
	Speed float32 `yaml:"speed"`
	// FocusSpeed is how fast it moves while focus is held, for threading through dense patterns.
	FocusSpeed float32 `yaml:"focusSpeed"`
	// Radius is the ship's hit radius, much smaller than its picture, as the genre expects.
	Radius float32 `yaml:"radius"`
	// Lives is how many lives the ship starts with.
	Lives int `yaml:"lives"`
	// Bombs is how many bombs it starts each life with.
	Bombs int `yaml:"bombs"`
	// Invulnerable is how long the ship cannot be hit after it loses a life or sets off a bomb.
	Invulnerable time.Duration `yaml:"invulnerable"`
	// Weapons are the bullet patterns the ship fires at each weapon level, the first at the start; a power item moves it to the next.
	Weapons []string `yaml:"weapons"`
	// Damage is how much each of the ship's bullets takes off an enemy's hit points.
	Damage int `yaml:"damage"`
	// BombDamage is how much a bomb takes off every enemy on the screen and off the boss.
	BombDamage int `yaml:"bombDamage"`
}

// enemyDef is a kind of enemy.
type enemyDef struct {
	// Name identifies the kind for the waves.
	Name string `yaml:"name"`
	// Frame is the atlas frame that draws it.
	Frame string `yaml:"frame"`
	// HP is how many hit points it has.
	HP int `yaml:"hp"`
	// Radius is its hit radius.
	Radius float32 `yaml:"radius"`
	// Score is what destroying it adds to the score.
	Score int `yaml:"score"`
	// Path is how it flies: dive, weave, cross or hover.
	Path string `yaml:"path"`
	// Speed is how fast it flies along its path, in points a second.
	Speed float32 `yaml:"speed"`
	// Amplitude is how far a weave path sways either side.
	Amplitude float32 `yaml:"amplitude,omitempty"`
	// Period is how long a weave path takes to sway there and back.
	Period time.Duration `yaml:"period,omitempty"`
	// StopY is how far down a hover path stops.
	StopY float32 `yaml:"stopY,omitempty"`
	// Hold is how long a hover path stays stopped.
	Hold time.Duration `yaml:"hold,omitempty"`
	// Pattern is the bullet pattern it fires; empty fires nothing.
	Pattern string `yaml:"pattern,omitempty"`
}

// waveDef is a wave: count enemies of one kind coming in one after another.
type waveDef struct {
	// At is when the wave starts, from the start of the stage.
	At time.Duration `yaml:"at"`
	// Enemy is the name of the kind of enemy it sends.
	Enemy string `yaml:"enemy"`
	// Count is how many it sends, from 1 to maxWaveCount.
	Count int `yaml:"count"`
	// Every is the time between two of them.
	Every time.Duration `yaml:"every,omitempty"`
	// X is where the first comes in across the screen, and Y how far down; enemies usually come in above the top edge, at a negative Y.
	X float32 `yaml:"x"`
	Y float32 `yaml:"y"`
	// StepX moves each enemy after the first this far across from the one before.
	StepX float32 `yaml:"stepX,omitempty"`
	// Drop is the item that the last enemy of the wave to be destroyed leaves behind, when the ship destroys the whole wave: power, bomb or life. Empty drops nothing.
	Drop string `yaml:"drop,omitempty"`
}

// bossDef is the boss at the end of the stage.
type bossDef struct {
	// Name is the boss's name, which the HUD shows while it fights.
	Name string `yaml:"name"`
	// Frame is the atlas frame that draws it.
	Frame string `yaml:"frame"`
	// At is when it arrives, from the start of the stage; it must come after the last wave starts.
	At time.Duration `yaml:"at"`
	// HP is how many hit points it has across all its phases.
	HP int `yaml:"hp"`
	// Radius is its hit radius.
	Radius float32 `yaml:"radius"`
	// Score is what defeating it adds to the score.
	Score int `yaml:"score"`
	// Speed is how fast it moves, in points a second.
	Speed float32 `yaml:"speed"`
	// Y is how far down the screen it settles.
	Y float32 `yaml:"y"`
	// Phases are its attack phases, from the first; each starts once the boss's hit points fall to its Below fraction.
	Phases []phaseDef `yaml:"phases"`
}

// phaseDef is one of the boss's attack phases.
type phaseDef struct {
	// Below is the fraction of the boss's hit points at or below which the phase starts: 1 for the first phase, falling for each after it.
	Below float32 `yaml:"below"`
	// Move is how the boss moves in the phase: hold, sway or chase.
	Move string `yaml:"move"`
	// Patterns are the bullet patterns it fires together in the phase.
	Patterns []string `yaml:"patterns"`
}

// parseStage decodes and validates a stage file. Unknown fields, trailing YAML documents and invalid stages are refused.
func parseStage(data []byte) (*stageFile, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("stage: empty file")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var s stageFile
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("stage: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("stage: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("stage: decode: %w", err)
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// encodeStage validates s and writes it as YAML with 2-space indents, its fields in a fixed order and optional fields left out when zero, so the same stage always encodes to the same bytes and parseStage reads them back unchanged.
func encodeStage(s *stageFile) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(s); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("stage: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("stage: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// enemy returns the enemy kind called name.
func (s *stageFile) enemy(name string) (*enemyDef, bool) {
	for i := range s.Enemies {
		if s.Enemies[i].Name == name {
			return &s.Enemies[i], true
		}
	}
	return nil, false
}

// validate checks the stage's version and every value in it, and that the waves name enemy kinds it has. Bullet pattern and frame names are checked against the pattern file and atlas by newWorld, which has them.
func (s *stageFile) validate() error {
	if s == nil {
		return errors.New("stage: nil stage")
	}
	if s.Version != stageVersion {
		return fmt.Errorf("stage: version %d, want %d", s.Version, stageVersion)
	}
	if s.Name == "" {
		return errors.New("stage: no name")
	}
	if !positive(s.ScrollSpeed) {
		return fmt.Errorf("stage: scrollSpeed %v is not above 0", s.ScrollSpeed)
	}
	if err := s.Ship.validate(); err != nil {
		return err
	}
	if len(s.Enemies) == 0 {
		return errors.New("stage: no enemies")
	}
	for i := range s.Enemies {
		e := &s.Enemies[i]
		if err := e.validate(); err != nil {
			return err
		}
		if slices.ContainsFunc(s.Enemies[:i], func(o enemyDef) bool { return o.Name == e.Name }) {
			return fmt.Errorf("stage: enemy %q is defined twice", e.Name)
		}
	}
	if len(s.Waves) == 0 {
		return errors.New("stage: no waves")
	}
	for i, w := range s.Waves {
		if err := w.validate(); err != nil {
			return fmt.Errorf("stage: wave %d: %w", i, err)
		}
		if _, ok := s.enemy(w.Enemy); !ok {
			return fmt.Errorf("stage: wave %d: no enemy %q", i, w.Enemy)
		}
		if i > 0 && w.At < s.Waves[i-1].At {
			return fmt.Errorf("stage: wave %d starts at %v, before wave %d at %v", i, w.At, i-1, s.Waves[i-1].At)
		}
	}
	if err := s.Boss.validate(); err != nil {
		return err
	}
	if last := s.Waves[len(s.Waves)-1].At; s.Boss.At <= last {
		return fmt.Errorf("stage: boss arrives at %v, not after the last wave at %v", s.Boss.At, last)
	}
	return nil
}

func (d *shipDef) validate() error {
	switch {
	case !positive(d.Speed):
		return fmt.Errorf("stage: ship speed %v is not above 0", d.Speed)
	case !positive(d.FocusSpeed):
		return fmt.Errorf("stage: ship focusSpeed %v is not above 0", d.FocusSpeed)
	case !positive(d.Radius):
		return fmt.Errorf("stage: ship radius %v is not above 0", d.Radius)
	case d.Lives < 1:
		return fmt.Errorf("stage: ship lives %d is not at least 1", d.Lives)
	case d.Bombs < 0:
		return fmt.Errorf("stage: ship bombs %d is negative", d.Bombs)
	case d.Invulnerable < 0:
		return fmt.Errorf("stage: ship invulnerable %v is negative", d.Invulnerable)
	case len(d.Weapons) == 0:
		return errors.New("stage: ship has no weapons")
	case slices.Contains(d.Weapons, ""):
		return errors.New("stage: ship has a weapon with no pattern")
	case d.Damage < 1:
		return fmt.Errorf("stage: ship damage %d is not at least 1", d.Damage)
	case d.BombDamage < 0:
		return fmt.Errorf("stage: ship bombDamage %d is negative", d.BombDamage)
	}
	return nil
}

func (e *enemyDef) validate() error {
	if e.Name == "" {
		return errors.New("stage: an enemy has no name")
	}
	fail := func(format string, args ...any) error {
		return fmt.Errorf("stage: enemy %q: %s", e.Name, fmt.Sprintf(format, args...))
	}
	switch {
	case e.Frame == "":
		return fail("no frame")
	case e.HP < 1:
		return fail("hp %d is not at least 1", e.HP)
	case !positive(e.Radius):
		return fail("radius %v is not above 0", e.Radius)
	case e.Score < 0:
		return fail("score %d is negative", e.Score)
	case !positive(e.Speed):
		return fail("speed %v is not above 0", e.Speed)
	case !finite(e.Amplitude) || e.Amplitude < 0:
		return fail("amplitude %v is negative", e.Amplitude)
	case e.Period < 0 || e.Hold < 0:
		return fail("period %v or hold %v is negative", e.Period, e.Hold)
	case !finite(e.StopY):
		return fail("stopY %v is not a number", e.StopY)
	}
	switch e.Path {
	case pathDive, pathCross:
	case pathWeave:
		if e.Period <= 0 {
			return fail("a weave path needs a period")
		}
	case pathHover:
		if e.StopY <= 0 {
			return fail("a hover path needs a stopY above 0")
		}
	default:
		return fail("path %q is not dive, weave, cross or hover", e.Path)
	}
	return nil
}

// maxWaveCount is the most enemies one wave may send, which keeps a typo in a count from spawning millions of enemies in one step.
const maxWaveCount = 1000

func (w *waveDef) validate() error {
	switch {
	case w.At < 0:
		return fmt.Errorf("at %v is negative", w.At)
	case w.Count < 1 || w.Count > maxWaveCount:
		return fmt.Errorf("count %d is not from 1 to %d", w.Count, maxWaveCount)
	case w.Every < 0:
		return fmt.Errorf("every %v is negative", w.Every)
	case w.Count > 1 && w.Every > (math.MaxInt64-w.At)/time.Duration(w.Count-1):
		return fmt.Errorf("the last of %d enemies every %v from %v comes later than a time can hold", w.Count, w.Every, w.At)
	case !finite(w.X) || !finite(w.Y) || !finite(w.StepX):
		return errors.New("x, y or stepX is not a number")
	}
	switch w.Drop {
	case "", itemPower, itemBomb, itemLife:
	default:
		return fmt.Errorf("drop %q is not power, bomb or life", w.Drop)
	}
	return nil
}

func (b *bossDef) validate() error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("stage: boss: %s", fmt.Sprintf(format, args...))
	}
	switch {
	case b.Name == "":
		return fail("no name")
	case b.Frame == "":
		return fail("no frame")
	case b.At <= 0:
		return fail("at %v is not after the start", b.At)
	case b.HP < 1:
		return fail("hp %d is not at least 1", b.HP)
	case !positive(b.Radius):
		return fail("radius %v is not above 0", b.Radius)
	case b.Score < 0:
		return fail("score %d is negative", b.Score)
	case !positive(b.Speed):
		return fail("speed %v is not above 0", b.Speed)
	case !positive(b.Y):
		return fail("y %v is not above 0", b.Y)
	case len(b.Phases) == 0:
		return fail("no phases")
	}
	for i, p := range b.Phases {
		switch {
		case !finite(p.Below) || p.Below <= 0 || p.Below > 1:
			return fail("phase %d: below %v is not in (0, 1]", i, p.Below)
		case i == 0 && p.Below != 1:
			return fail("phase 0: below %v, want 1, so the boss starts in it", p.Below)
		case i > 0 && p.Below >= b.Phases[i-1].Below:
			return fail("phase %d: below %v is not under phase %d's %v", i, p.Below, i-1, b.Phases[i-1].Below)
		case len(p.Patterns) == 0 || slices.Contains(p.Patterns, ""):
			return fail("phase %d: no patterns, or one with no name", i)
		}
		switch p.Move {
		case moveHold, moveSway, moveChase:
		default:
			return fail("phase %d: move %q is not hold, sway or chase", i, p.Move)
		}
	}
	return nil
}

func finite(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }

func positive(v float32) bool { return finite(v) && v > 0 }

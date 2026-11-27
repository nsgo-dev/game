package main

import (
	"math/rand/v2"
	"slices"

	"github.com/nsgo-dev/game/pkg/fighting"
)

// jumpInScript is the CPU's jump-in: a jump forward and a heavy air attack on the way down.
var jumpInScript = []scriptStep{
	{dir: fighting.UpForward, steps: 3},
	{dir: fighting.Neutral, steps: 14},
	{dir: fighting.Neutral, buttons: []string{"heavy"}, steps: 2},
	{dir: fighting.Neutral, steps: 16},
}

// closeRange is how near the CPU walks before it strikes: inside the reach of every fighter's light attack.
const closeRange = 52

// lowScript is the CPU's low poke.
var lowScript = []scriptStep{
	{dir: fighting.Down, buttons: []string{"light"}, steps: 2},
	{dir: fighting.Down, steps: 10},
}

// cpu plays a fighter for the computer. It decides every Reaction steps from what it sees, as a player would, and plays its decisions as input, through the same recognizer and the same rules as a person: a special move is a script of directions and presses, not a shortcut.
type cpu struct {
	cfg *cpuConfig
	rng *rand.Rand
	// plan is the script being played, at its step at with left steps of it to go.
	plan     []scriptStep
	at, left int
	// hold is the direction held, relative to the side the fighter faces, while no script plays.
	hold fighting.Direction
	wait int
}

func newCPU(cfg *cpuConfig) *cpu {
	return &cpu{cfg: cfg, rng: rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9e3779b97f4a7c15)), hold: fighting.Neutral}
}

// control returns the CPU's input for me on this step.
func (c *cpu) control(m *match, me *player) control {
	if m.phase != phaseFight || me.health == 0 {
		c.plan, c.hold, c.wait = nil, fighting.Neutral, 0
		return neutral
	}
	if c.plan == nil {
		if c.wait > 0 {
			c.wait--
		} else {
			c.decide(m, me)
			c.wait = c.cfg.Reaction
		}
	}
	if c.plan == nil {
		return control{dir: me.facing().Relative(c.hold)}
	}
	step := c.plan[c.at]
	in := control{dir: me.facing().Relative(step.dir), held: buttonMask(me, step.buttons)}
	if c.left--; c.left <= 0 {
		if c.at++; c.at < len(c.plan) {
			c.left = c.plan[c.at].steps
		} else {
			c.plan = nil
		}
	}
	return in
}

// buttonMask returns the held mask for the named buttons of me's move set.
func buttonMask(me *player, buttons []string) uint32 {
	var held uint32
	for _, b := range buttons {
		if i := slices.Index(me.fighter.buttons, b); i >= 0 {
			held |= 1 << i
		}
	}
	return held
}

// play starts a script.
func (c *cpu) play(s []scriptStep) {
	c.plan, c.at, c.left = s, 0, s[0].steps
	c.hold = fighting.Neutral
}

// roll reports whether a percentage chance came up.
func (c *cpu) roll(percent int) bool { return c.rng.IntN(100) < percent }

// decide picks what to do next from the distance, the opponent's attacks, jumps and projectile.
func (c *cpu) decide(m *match, me *player) {
	o := m.opponent(me)
	d := abs32(o.f.X - me.f.X)
	c.hold = fighting.Neutral
	switch {
	case m.threatened(me):
		if c.roll(c.cfg.Block) {
			c.hold = fighting.Back
			if attackLevel(o) == levelLow {
				c.hold = fighting.DownBack
			}
		}
	case o.airborne && d < 140 && c.roll(c.cfg.AntiAir):
		c.play(me.fighter.scripts["antiAir"])
	case d > 150 && me.shot == nil && c.roll(c.cfg.Projectile):
		c.play(me.fighter.scripts["projectile"])
	case d > closeRange:
		if d < 150 && c.roll(c.cfg.JumpIn) {
			c.play(jumpInScript)
		} else {
			c.hold = fighting.Forward
		}
	default:
		switch r := c.rng.IntN(100); {
		case r < 50:
			c.play(me.fighter.scripts["combo"])
		case r < 75:
			c.play(lowScript)
		case r < 90:
			c.hold = fighting.Back
		}
	}
}

// attackLevel returns the hit level of what p attacks with: its move, or else its projectile.
func attackLevel(p *player) string {
	if attacking(&p.f) {
		return p.fighter.extra(p.moveName()).Level
	}
	if p.shot != nil {
		return p.fighter.extra(p.shot.f.Move().Name).Level
	}
	return levelMid
}

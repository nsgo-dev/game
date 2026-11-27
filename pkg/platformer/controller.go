package platformer

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
)

// Input is one fixed step's player input.
type Input struct {
	// Move is the horizontal axis in [-1, 1], negative to the left; values outside are clamped and NaN reads as 0.
	Move float32
	// JumpPressed reports that the jump button went down during this step.
	JumpPressed bool
	// JumpHeld reports that the jump button is down at the end of this step.
	JumpHeld bool
}

// JumpKind says which jump a step started.
type JumpKind uint8

const (
	// JumpNone means the step started no jump.
	JumpNone JumpKind = iota
	// JumpGround means the step jumped from the ground, or within CoyoteTime of leaving it.
	JumpGround
	// JumpWall means the step jumped off a wall, or within WallCoyoteTime of leaving it.
	JumpWall
)

// String returns "none", "ground" or "wall".
func (k JumpKind) String() string {
	switch k {
	case JumpGround:
		return "ground"
	case JumpWall:
		return "wall"
	default:
		return "none"
	}
}

// StepResult reports what one Controller.Step did.
type StepResult struct {
	// Move is the result of the step's CharacterBody.Move.
	Move physics.MoveResult
	// Jump is the jump the step started, if any.
	Jump JumpKind
	// JumpCut reports that the step cut a rising jump short because the button was let go.
	JumpCut bool
	// WallSliding reports that the step capped the fall at WallSlideSpeed.
	WallSliding bool
	// InputLocked reports that the step ignored horizontal input because a wall jump happened less than WallJumpLock earlier.
	InputLocked bool
	// WallSide is -1 for a wall on the left, 1 for one on the right and 0 for none: the wall the body touches or can still wall jump from at the start of the step.
	WallSide int
}

// window tracks the time since an event and whether it is still within a duration.
type window struct {
	age   time.Duration
	steps time.Duration // steps advanced since the event, counted as a Duration so it adds to age
	on    bool
}

// start opens the window at the current step.
func (w *window) start() { *w = window{on: true} }

// advance moves the window on by one step of dt and closes it once d has passed. A window opened on a step is always open on that step, so a zero duration lasts exactly that step.
//
// game.Loop's dt is time.Second/Rate truncated to whole nanoseconds, so each step can be up to 1ns shorter than its nominal length (16666666ns at 60 Hz). The window allows 1ns per step for that, so 100ms at 60 Hz lasts 6 steps, as it would with exact steps, rather than 7.
func (w *window) advance(dt, d time.Duration) {
	if !w.on {
		return
	}
	w.age += dt
	w.steps++
	if w.age+w.steps >= d {
		w.on = false
	}
}

// Controller drives a physics.CharacterBody as a platformer character with coyote time, jump buffering, variable jump height, wall slides and wall jumps. Call Step once per fixed game.Loop step; it reads the contacts the body's previous Move left on its Body, sets the velocity from the input and the Config, and then moves the body.
//
// Every timing is measured in step time: an event on a step opens a window that stays open on later steps while less than its duration has passed since that step, so at a fixed dt a duration D covers ceil(D/dt) steps counting the event's own step, and a zero duration covers only that step. Each step is allowed 1ns of slack for game.Loop's dt being truncated to whole nanoseconds, so at 60 Hz a 100ms window covers 6 steps.
//
// A rising controller jump is cut only while the body's VY is the one the previous Step left; a VY the game sets in between, such as a spring, ends the jump and is never cut. A body that is rising in the air cannot use coyote time either, so a jump press does not replace such a launch with a ground jump.
type Controller struct {
	// Body is the character the controller moves.
	Body *physics.CharacterBody

	cfg Config

	coyote   window  // since the body was last on the ground
	buffer   window  // since the jump button was last pressed
	wall     window  // since the body last touched a wall in the air
	lock     window  // since the last wall jump
	wallSide int     // the wall the wall window refers to
	jumping  bool    // rising from a controller jump that may still be cut
	leftVY   float32 // the body's VY at the end of the previous Step, to tell when the game changed it
}

// NewController returns a Controller moving body with cfg, or an error from cfg.Validate.
func NewController(body *physics.CharacterBody, cfg Config) (*Controller, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Controller{Body: body, cfg: cfg}, nil
}

// Config returns the controller's tunables.
func (c *Controller) Config() Config { return c.cfg }

// SetConfig replaces the controller's tunables after checking them with Validate; running timers keep their elapsed time and are measured against the new durations from the next step.
func (c *Controller) SetConfig(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	c.cfg = cfg
	return nil
}

// Reset forgets every timer and the jump in progress, such as after a respawn; it leaves the body as it is.
func (c *Controller) Reset() {
	c.coyote, c.buffer, c.wall, c.lock = window{}, window{}, window{}, window{}
	c.wallSide = 0
	c.jumping = false
	c.leftVY = 0
}

// Step runs one fixed step of dt: it advances the timers, applies horizontal input, starts a buffered ground or wall jump when one is allowed, cuts a rising jump whose button is up, applies gravity with the wall-slide and fall caps, and moves the body against grid, slopes and platforms with CharacterBody.Move. A nil controller or body, or dt <= 0, does nothing.
func (c *Controller) Step(in Input, grid physics.TileGrid, slopes []physics.Slope, platforms []*physics.MovingPlatform, dt time.Duration) StepResult {
	return c.stepWith(in, dt, func() physics.MoveResult {
		return c.Body.Move(grid, slopes, platforms, dt)
	})
}

// StepWorld runs one fixed step of dt like Step, moving the body against world (its Bodies, Tiles and static RigidBody colliders in world.RigidWorld) together with slopes and platforms via CharacterBody.MoveWorld.
func (c *Controller) StepWorld(in Input, world *physics.World, slopes []physics.Slope, platforms []*physics.MovingPlatform, dt time.Duration) StepResult {
	return c.stepWith(in, dt, func() physics.MoveResult {
		return c.Body.MoveWorld(world, slopes, platforms, dt)
	})
}

func (c *Controller) stepWith(in Input, dt time.Duration, moveFn func() physics.MoveResult) StepResult {
	if c == nil || c.Body == nil || c.Body.Body == nil || dt <= 0 {
		return StepResult{}
	}
	b := c.Body.Body
	cfg := &c.cfg
	secs := float32(dt.Seconds())

	c.coyote.advance(dt, cfg.CoyoteTime)
	c.buffer.advance(dt, cfg.JumpBuffer)
	c.wall.advance(dt, cfg.WallCoyoteTime)
	c.lock.advance(dt, cfg.WallJumpLock)
	if !c.wall.on {
		c.wallSide = 0
	}

	// A VY the game set since the previous Step, such as a spring, ends the controller jump: that launch is never cut.
	if c.jumping && b.VY != c.leftVY {
		c.jumping = false
	}

	// Contacts from the previous Move.
	grounded := c.Body.Grounded
	touching := 0
	if grounded {
		c.coyote.start()
		c.wall = window{}
		c.wallSide = 0
		c.jumping = false
	} else {
		switch {
		case b.OnLeftWall:
			touching = -1
		case b.OnRightWall:
			touching = 1
		}
		if touching != 0 {
			c.wall.start()
			c.wallSide = touching
		}
		// Coyote time is for running off a ledge: a body already rising in the air, such as from a spring the game set, cannot ground jump and replace that launch.
		if b.VY < 0 {
			c.coyote = window{}
		}
	}
	if in.JumpPressed {
		c.buffer.start()
	}

	res := StepResult{WallSide: c.wallSide, InputLocked: c.lock.on}

	move := in.Move
	if move != move {
		move = 0
	}
	move = min(max(move, -1), 1)

	// Horizontal input, unless a wall jump locked it.
	if !c.lock.on && cfg.RunSpeed > 0 {
		target := move * cfg.RunSpeed
		accel := cfg.AirAccel
		if grounded {
			accel = cfg.GroundAccel
			if move == 0 {
				accel = cfg.GroundDecel
			}
		}
		b.VX = approach(b.VX, target, accel*secs)
	}

	// Jumps: a buffered press leaves the ground first, then a wall.
	if c.buffer.on {
		switch {
		case c.coyote.on:
			b.VY = -cfg.JumpSpeed
			res.Jump = JumpGround
		case c.wall.on && cfg.WallJumpSpeedY > 0:
			b.VX = float32(-c.wallSide) * cfg.WallJumpSpeedX
			b.VY = -cfg.WallJumpSpeedY
			c.lock.start()
			res.Jump = JumpWall
		}
		if res.Jump != JumpNone {
			c.buffer = window{}
			c.coyote = window{}
			c.wall = window{}
			c.wallSide = 0
			c.jumping = true
		}
	}

	// Variable jump height: cut once when the button is up while still rising.
	if c.jumping {
		switch {
		case b.VY >= 0:
			c.jumping = false
		case !in.JumpHeld:
			if -b.VY > cfg.JumpCutMinSpeed {
				b.VY *= cfg.JumpCut
				res.JumpCut = true
			}
			c.jumping = false
		}
	}

	b.VY += cfg.Gravity * secs

	// Wall slide: falling against a wall the input pushes into.
	if res.Jump == JumpNone && touching != 0 && cfg.WallSlideSpeed > 0 && !c.lock.on && b.VY > cfg.WallSlideSpeed && sign(move) == touching {
		b.VY = cfg.WallSlideSpeed
		res.WallSliding = true
	}
	if cfg.MaxFallSpeed > 0 && b.VY > cfg.MaxFallSpeed {
		b.VY = cfg.MaxFallSpeed
	}

	res.Move = moveFn()
	c.leftVY = b.VY
	return res
}

// approach moves v toward target by at most step, or straight to it when step is zero.
func approach(v, target, step float32) float32 {
	if step <= 0 {
		return target
	}
	d := target - v
	if float32(math.Abs(float64(d))) <= step {
		return target
	}
	if d > 0 {
		return v + step
	}
	return v - step
}

// sign returns -1, 0 or 1 for v's sign.
func sign(v float32) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 0
}

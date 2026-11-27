package platformer_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/platformer"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
)

const step = 10 * time.Millisecond

// near reports whether a and b differ by less than 1e-3, for velocities that add an acceleration times a float32 step.
func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

// bare returns a config with every helper off and no gravity, so a test turns on only what it checks.
func bare() platformer.Config {
	return platformer.Config{Version: platformer.CurrentVersion, JumpSpeed: 300, JumpCut: 1}
}

func newController(t *testing.T, cfg platformer.Config, x, y float32) *platformer.Controller {
	t.Helper()
	c, err := platformer.NewController(physics.NewCharacterBody(physics.NewBoxBody(x, y, 12, 16)), cfg)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	return c
}

// stepFree steps c with no geometry, so the body never touches anything unless a test sets its contacts first.
func stepFree(c *platformer.Controller, in platformer.Input) platformer.StepResult {
	return c.Step(in, nil, nil, nil, step)
}

func TestCoyoteTimeWindowPerStep(t *testing.T) {
	for _, tc := range []struct {
		coyote time.Duration
		after  int // steps since the last grounded step when jump is pressed
		want   platformer.JumpKind
	}{
		{50 * time.Millisecond, 1, platformer.JumpGround},
		{50 * time.Millisecond, 4, platformer.JumpGround},
		{50 * time.Millisecond, 5, platformer.JumpNone},
		{50 * time.Millisecond, 6, platformer.JumpNone},
		{45 * time.Millisecond, 4, platformer.JumpGround},
		{45 * time.Millisecond, 5, platformer.JumpNone},
		{0, 0, platformer.JumpGround},
		{0, 1, platformer.JumpNone},
	} {
		cfg := bare()
		cfg.CoyoteTime = tc.coyote
		c := newController(t, cfg, 0, 0)
		c.Body.Grounded = true
		if tc.after == 0 {
			if got := stepFree(c, platformer.Input{JumpPressed: true, JumpHeld: true}).Jump; got != tc.want {
				t.Fatalf("coyote %v grounded: jump %v, want %v", tc.coyote, got, tc.want)
			}
			continue
		}
		stepFree(c, platformer.Input{}) // last grounded step; the free move leaves the ground
		for i := 1; i < tc.after; i++ {
			if r := stepFree(c, platformer.Input{}); r.Jump != platformer.JumpNone {
				t.Fatalf("jumped without a press on step %d", i)
			}
		}
		got := stepFree(c, platformer.Input{JumpPressed: true, JumpHeld: true})
		if got.Jump != tc.want {
			t.Fatalf("coyote %v, press %d steps after leaving the ground: jump %v, want %v", tc.coyote, tc.after, got.Jump, tc.want)
		}
		if tc.want == platformer.JumpGround && c.Body.Body.VY != -cfg.JumpSpeed {
			t.Fatalf("coyote jump VY = %v, want %v", c.Body.Body.VY, -cfg.JumpSpeed)
		}
	}
}

func TestCoyoteJumpOnlyOnce(t *testing.T) {
	cfg := bare()
	cfg.CoyoteTime = 100 * time.Millisecond
	c := newController(t, cfg, 0, 0)
	c.Body.Grounded = true
	stepFree(c, platformer.Input{})
	if r := stepFree(c, platformer.Input{JumpPressed: true}); r.Jump != platformer.JumpGround {
		t.Fatalf("first coyote press: %v", r.Jump)
	}
	if r := stepFree(c, platformer.Input{JumpPressed: true}); r.Jump != platformer.JumpNone {
		t.Fatalf("a second press inside the same coyote window jumped again: %v", r.Jump)
	}
}

func TestJumpBufferWindowPerStep(t *testing.T) {
	for _, tc := range []struct {
		buffer time.Duration
		land   int // steps after the press at which the body is grounded
		want   platformer.JumpKind
	}{
		{50 * time.Millisecond, 1, platformer.JumpGround},
		{50 * time.Millisecond, 4, platformer.JumpGround},
		{50 * time.Millisecond, 5, platformer.JumpNone},
		{0, 1, platformer.JumpNone},
	} {
		cfg := bare()
		cfg.JumpBuffer = tc.buffer
		c := newController(t, cfg, 0, 0)
		if r := stepFree(c, platformer.Input{JumpPressed: true, JumpHeld: true}); r.Jump != platformer.JumpNone {
			t.Fatalf("jumped in the air without coyote: %v", r.Jump)
		}
		for i := 1; i < tc.land; i++ {
			stepFree(c, platformer.Input{JumpHeld: true})
		}
		c.Body.Grounded = true // the previous step landed
		got := stepFree(c, platformer.Input{JumpHeld: true})
		if got.Jump != tc.want {
			t.Fatalf("buffer %v, landing %d steps after the press: jump %v, want %v", tc.buffer, tc.land, got.Jump, tc.want)
		}
	}
}

func TestBufferedJumpFiresOnLanding(t *testing.T) {
	grid := physics.NewGrid(10, 10, 16, 16)
	grid.FillRect(0, 8, 10, 2, physics.TileSolid) // floor top at y=128
	cfg := bare()
	cfg.Gravity = 600
	cfg.JumpBuffer = 100 * time.Millisecond
	c := newController(t, cfg, 40, 100)

	pressed := -1
	for i := range 200 {
		in := platformer.Input{JumpHeld: pressed >= 0}
		if pressed < 0 && c.Body.Body.Shape.Box.Bottom() > 124 && c.Body.Body.VY > 0 {
			in = platformer.Input{JumpPressed: true, JumpHeld: true}
			pressed = i
		}
		wasGrounded := c.Body.Grounded
		r := c.Step(in, grid, nil, nil, step)
		if r.Jump == platformer.JumpGround {
			if !wasGrounded {
				t.Fatalf("jumped on step %d before landing", i)
			}
			if pressed < 0 || i == pressed {
				t.Fatalf("jump on step %d was not buffered (press on step %d)", i, pressed)
			}
			if c.Body.Body.VY >= 0 || c.Body.Grounded {
				t.Fatalf("buffered jump did not leave the ground: VY=%v grounded=%v", c.Body.Body.VY, c.Body.Grounded)
			}
			return
		}
	}
	t.Fatalf("buffered jump never fired (press on step %d)", pressed)
}

func TestVariableJumpHeight(t *testing.T) {
	cfg := bare()
	cfg.JumpCut = 0.5
	cfg.JumpCutMinSpeed = 100

	t.Run("release while rising cuts once", func(t *testing.T) {
		c := newController(t, cfg, 0, 0)
		c.Body.Grounded = true
		if r := stepFree(c, platformer.Input{JumpPressed: true, JumpHeld: true}); r.Jump != platformer.JumpGround || r.JumpCut {
			t.Fatalf("held jump: %+v", r)
		}
		if r := stepFree(c, platformer.Input{JumpHeld: true}); r.JumpCut || c.Body.Body.VY != -300 {
			t.Fatalf("holding cut the jump: VY=%v", c.Body.Body.VY)
		}
		if r := stepFree(c, platformer.Input{}); !r.JumpCut || c.Body.Body.VY != -150 {
			t.Fatalf("release: cut=%v VY=%v, want cut and -150", r.JumpCut, c.Body.Body.VY)
		}
		stepFree(c, platformer.Input{JumpHeld: true})
		if r := stepFree(c, platformer.Input{}); r.JumpCut || c.Body.Body.VY != -150 {
			t.Fatalf("a second release cut again: VY=%v", c.Body.Body.VY)
		}
	})

	t.Run("tap cuts on the jump's own step", func(t *testing.T) {
		c := newController(t, cfg, 0, 0)
		c.Body.Grounded = true
		r := stepFree(c, platformer.Input{JumpPressed: true})
		if r.Jump != platformer.JumpGround || !r.JumpCut || c.Body.Body.VY != -150 {
			t.Fatalf("tap: %+v VY=%v", r, c.Body.Body.VY)
		}
	})

	t.Run("release below the minimum speed", func(t *testing.T) {
		slow := cfg
		slow.Gravity = 22000 // 220 points/s less each step
		c := newController(t, slow, 0, 0)
		c.Body.Grounded = true
		stepFree(c, platformer.Input{JumpPressed: true, JumpHeld: true}) // -300 + 220 = -80
		if got := c.Body.Body.VY; !near(got, -80) {
			t.Fatalf("VY after jump = %v, want -80", got)
		}
		if r := stepFree(c, platformer.Input{}); r.JumpCut {
			t.Fatalf("cut a release below jump_cut_min_speed")
		}
	})

	t.Run("rising without a jump is never cut", func(t *testing.T) {
		c := newController(t, cfg, 0, 0)
		c.Body.Body.VY = -300 // a spring, not a jump
		if r := stepFree(c, platformer.Input{}); r.JumpCut || c.Body.Body.VY != -300 {
			t.Fatalf("cut a spring launch: VY=%v", c.Body.Body.VY)
		}
	})
}

// wallSetup returns a controller in the air touching the solid column 0 of a grid, pushed into it so the next step sees the contact.
func wallSetup(t *testing.T, cfg platformer.Config) (*platformer.Controller, *physics.Grid) {
	t.Helper()
	grid := physics.NewGrid(10, 20, 16, 16)
	grid.FillRect(0, 0, 1, 20, physics.TileSolid)
	c := newController(t, cfg, 16, 40)
	return c, grid
}

func TestWallSlideClamp(t *testing.T) {
	cfg := bare()
	cfg.Gravity = 6000 // 60 points/s each step
	cfg.RunSpeed = 100
	cfg.WallSlideSpeed = 40
	c, grid := wallSetup(t, cfg)

	push := platformer.Input{Move: -1}
	r := c.Step(push, grid, nil, nil, step)
	if r.WallSliding || !near(c.Body.Body.VY, 60) || !r.Move.OnLeftWall {
		t.Fatalf("first step: sliding=%v VY=%v onLeftWall=%v", r.WallSliding, c.Body.Body.VY, r.Move.OnLeftWall)
	}
	for i := range 5 {
		r = c.Step(push, grid, nil, nil, step)
		if !r.WallSliding || r.WallSide != -1 || c.Body.Body.VY != 40 {
			t.Fatalf("slide step %d: sliding=%v side=%d VY=%v", i, r.WallSliding, r.WallSide, c.Body.Body.VY)
		}
	}
	// Letting go of the wall falls freely again.
	r = c.Step(platformer.Input{}, grid, nil, nil, step)
	if r.WallSliding || !near(c.Body.Body.VY, 100) {
		t.Fatalf("not pushing still slid: sliding=%v VY=%v", r.WallSliding, c.Body.Body.VY)
	}
}

func TestWallSlideNeedsInputTowardWall(t *testing.T) {
	cfg := bare()
	cfg.Gravity = 6000
	cfg.WallSlideSpeed = 40
	c := newController(t, cfg, 0, 0)
	for _, in := range []platformer.Input{{Move: 0}, {Move: 1}} {
		c.Body.Body.OnLeftWall = true
		c.Body.Body.VY = 100
		if r := stepFree(c, in); r.WallSliding {
			t.Fatalf("slid with move %v away from the wall", in.Move)
		}
	}
}

func TestMaxFallSpeed(t *testing.T) {
	cfg := bare()
	cfg.Gravity = 10000
	cfg.MaxFallSpeed = 250
	c := newController(t, cfg, 0, 0)
	want := []float32{100, 200, 250, 250}
	for i, w := range want {
		stepFree(c, platformer.Input{})
		if !near(c.Body.Body.VY, w) {
			t.Fatalf("step %d VY=%v, want %v", i, c.Body.Body.VY, w)
		}
	}
}

func TestWallJumpDirectionAndLockout(t *testing.T) {
	cfg := bare()
	cfg.RunSpeed = 100
	cfg.WallSlideSpeed = 40
	cfg.WallJumpSpeedX = 150
	cfg.WallJumpSpeedY = 250
	cfg.WallJumpLock = 30 * time.Millisecond
	c, grid := wallSetup(t, cfg)

	toward := platformer.Input{Move: -1, JumpHeld: true}
	c.Step(platformer.Input{Move: -1}, grid, nil, nil, step)
	r := c.Step(platformer.Input{Move: -1, JumpPressed: true, JumpHeld: true}, grid, nil, nil, step)
	if r.Jump != platformer.JumpWall {
		t.Fatalf("jump against the left wall: %v", r.Jump)
	}
	if c.Body.Body.VX != 150 || c.Body.Body.VY != -250 {
		t.Fatalf("wall jump velocity = (%v, %v), want (150, -250)", c.Body.Body.VX, c.Body.Body.VY)
	}
	// The lock ignores input toward the wall for 30 ms: the next two steps.
	for i := 1; i <= 2; i++ {
		r = c.Step(toward, grid, nil, nil, step)
		if !r.InputLocked || c.Body.Body.VX != 150 {
			t.Fatalf("lock step %d: locked=%v VX=%v", i, r.InputLocked, c.Body.Body.VX)
		}
	}
	r = c.Step(toward, grid, nil, nil, step)
	if r.InputLocked || c.Body.Body.VX != -100 {
		t.Fatalf("after the lock: locked=%v VX=%v, want input back and -100", r.InputLocked, c.Body.Body.VX)
	}
}

func TestWallJumpFromRightWall(t *testing.T) {
	cfg := bare()
	cfg.WallJumpSpeedX = 120
	cfg.WallJumpSpeedY = 200
	c := newController(t, cfg, 0, 0)
	c.Body.Body.OnRightWall = true
	r := stepFree(c, platformer.Input{JumpPressed: true, JumpHeld: true})
	if r.Jump != platformer.JumpWall || r.WallSide != 1 || c.Body.Body.VX != -120 || c.Body.Body.VY != -200 {
		t.Fatalf("right wall jump: %+v v=(%v, %v)", r, c.Body.Body.VX, c.Body.Body.VY)
	}
}

func TestWallCoyoteWindowPerStep(t *testing.T) {
	for _, tc := range []struct {
		after int
		want  platformer.JumpKind
	}{
		{1, platformer.JumpWall},
		{2, platformer.JumpWall},
		{3, platformer.JumpNone},
	} {
		cfg := bare()
		cfg.WallJumpSpeedX = 100
		cfg.WallJumpSpeedY = 200
		cfg.WallCoyoteTime = 30 * time.Millisecond
		c := newController(t, cfg, 0, 0)
		c.Body.Body.OnLeftWall = true
		stepFree(c, platformer.Input{}) // last step touching the wall
		for i := 1; i < tc.after; i++ {
			stepFree(c, platformer.Input{})
		}
		got := stepFree(c, platformer.Input{JumpPressed: true, JumpHeld: true})
		if got.Jump != tc.want {
			t.Fatalf("press %d steps after leaving the wall: %v, want %v", tc.after, got.Jump, tc.want)
		}
	}
}

func TestGroundJumpBeatsWallJump(t *testing.T) {
	cfg := bare()
	cfg.WallJumpSpeedX = 100
	cfg.WallJumpSpeedY = 200
	c := newController(t, cfg, 0, 0)
	c.Body.Grounded = true
	c.Body.Body.OnLeftWall = true
	if r := stepFree(c, platformer.Input{JumpPressed: true}); r.Jump != platformer.JumpGround {
		t.Fatalf("grounded against a wall: %v, want ground jump", r.Jump)
	}
}

func TestWallJumpDisabledKeepsBuffer(t *testing.T) {
	cfg := bare()
	cfg.JumpBuffer = 50 * time.Millisecond
	c := newController(t, cfg, 0, 0)
	c.Body.Body.OnLeftWall = true
	if r := stepFree(c, platformer.Input{JumpPressed: true, JumpHeld: true}); r.Jump != platformer.JumpNone {
		t.Fatalf("wall jump with wall_jump_speed_y 0: %v", r.Jump)
	}
	c.Body.Grounded = true
	if r := stepFree(c, platformer.Input{JumpHeld: true}); r.Jump != platformer.JumpGround {
		t.Fatalf("the press was not kept for the landing: %v", r.Jump)
	}
}

func TestHorizontalAcceleration(t *testing.T) {
	cfg := bare()
	cfg.RunSpeed = 100
	cfg.GroundAccel = 4000 // 40 a step
	cfg.GroundDecel = 6000 // 60 a step
	cfg.AirAccel = 2000    // 20 a step
	c := newController(t, cfg, 0, 0)

	c.Body.Grounded = true
	stepFree(c, platformer.Input{Move: 1})
	if !near(c.Body.Body.VX, 40) {
		t.Fatalf("ground accel VX=%v, want 40", c.Body.Body.VX)
	}
	stepFree(c, platformer.Input{Move: 1}) // now airborne
	if !near(c.Body.Body.VX, 60) {
		t.Fatalf("air accel VX=%v, want 60", c.Body.Body.VX)
	}
	c.Body.Grounded = true
	stepFree(c, platformer.Input{})
	if c.Body.Body.VX != 0 {
		t.Fatalf("ground decel VX=%v, want 0", c.Body.Body.VX)
	}
	c.Body.Grounded = true
	stepFree(c, platformer.Input{Move: 7}) // clamped to 1
	if !near(c.Body.Body.VX, 40) {
		t.Fatalf("clamped input VX=%v, want 40", c.Body.Body.VX)
	}
}

func TestRunSpeedZeroLeavesVelocity(t *testing.T) {
	c := newController(t, bare(), 0, 0)
	c.Body.Body.VX = 33
	stepFree(c, platformer.Input{Move: -1})
	if c.Body.Body.VX != 33 {
		t.Fatalf("run_speed 0 changed VX to %v", c.Body.Body.VX)
	}
}

func TestResetForgetsTimers(t *testing.T) {
	cfg := bare()
	cfg.JumpBuffer = time.Second
	c := newController(t, cfg, 0, 0)
	stepFree(c, platformer.Input{JumpPressed: true})
	c.Reset()
	c.Body.Grounded = true
	if r := stepFree(c, platformer.Input{}); r.Jump != platformer.JumpNone {
		t.Fatalf("buffered press survived Reset: %v", r.Jump)
	}
}

func TestStepIgnoresInvalidCalls(t *testing.T) {
	var nilc *platformer.Controller
	if r := nilc.Step(platformer.Input{}, nil, nil, nil, step); r.Jump != platformer.JumpNone || r.Move.Contacts != nil {
		t.Fatalf("nil controller: %+v", r)
	}
	c := newController(t, bare(), 0, 0)
	c.Body.Grounded = true
	if r := c.Step(platformer.Input{JumpPressed: true}, nil, nil, nil, 0); r.Jump != platformer.JumpNone || c.Body.Body.VY != 0 {
		t.Fatalf("dt 0 did something: %+v", r)
	}
}

func TestNewControllerRejectsInvalidConfig(t *testing.T) {
	cfg := bare()
	cfg.JumpCut = 2
	if _, err := platformer.NewController(nil, cfg); err == nil {
		t.Fatal("NewController accepted jump_cut 2")
	}
	c := newController(t, bare(), 0, 0)
	if err := c.SetConfig(platformer.Config{}); err == nil {
		t.Fatal("SetConfig accepted version 0")
	}
	if c.Config() != bare() {
		t.Fatal("a refused SetConfig changed the config")
	}
}

func TestJumpKindString(t *testing.T) {
	for k, want := range map[platformer.JumpKind]string{platformer.JumpNone: "none", platformer.JumpGround: "ground", platformer.JumpWall: "wall"} {
		if k.String() != want {
			t.Fatalf("%d.String() = %q, want %q", k, k.String(), want)
		}
	}
}

func BenchmarkControllerStep(b *testing.B) {
	grid := physics.NewGrid(64, 16, 16, 16)
	grid.FillRect(0, 14, 64, 2, physics.TileSolid)
	c, err := platformer.NewController(physics.NewCharacterBody(physics.NewBoxBody(100, 180, 12, 16)), platformer.DefaultConfig())
	if err != nil {
		b.Fatal(err)
	}
	dt := time.Second / 60
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		in := platformer.Input{Move: 1, JumpPressed: i%40 == 0, JumpHeld: i%40 < 10}
		if c.Body.Body.Shape.Box.X > 900 {
			c.Body.Body.Shape.Box.X = 100
		}
		c.Step(in, grid, nil, nil, dt)
	}
}

func TestCoyoteDoesNotReplaceGameLaunch(t *testing.T) {
	cfg := bare()
	cfg.CoyoteTime = 100 * time.Millisecond
	c := newController(t, cfg, 0, 0)
	c.Body.Grounded = true
	stepFree(c, platformer.Input{}) // last grounded step
	c.Body.Body.VY = -600           // a spring the game set
	r := stepFree(c, platformer.Input{JumpPressed: true, JumpHeld: true})
	if r.Jump != platformer.JumpNone || c.Body.Body.VY != -600 {
		t.Fatalf("press inside coyote_time while launched: jump %v VY=%v, want none and -600", r.Jump, c.Body.Body.VY)
	}
}

func TestGameLaunchDuringJumpIsNotCut(t *testing.T) {
	cfg := bare()
	cfg.JumpCut = 0.5
	c := newController(t, cfg, 0, 0)
	c.Body.Grounded = true
	if r := stepFree(c, platformer.Input{JumpPressed: true, JumpHeld: true}); r.Jump != platformer.JumpGround {
		t.Fatalf("jump: %v", r.Jump)
	}
	c.Body.Body.VY = -600 // a spring hit while still rising with the button held
	if r := stepFree(c, platformer.Input{}); r.JumpCut || c.Body.Body.VY != -600 {
		t.Fatalf("release after a spring: cut=%v VY=%v, want no cut and -600", r.JumpCut, c.Body.Body.VY)
	}
}

func TestJumpCutZeroStopsTheRise(t *testing.T) {
	cfg := bare()
	cfg.JumpCut = 0
	c := newController(t, cfg, 0, 0)
	c.Body.Grounded = true
	stepFree(c, platformer.Input{JumpPressed: true, JumpHeld: true})
	if r := stepFree(c, platformer.Input{}); !r.JumpCut || c.Body.Body.VY != 0 {
		t.Fatalf("jump_cut 0 release: cut=%v VY=%v, want cut and 0", r.JumpCut, c.Body.Body.VY)
	}
}

// TestWindowsAtSixtyHertz steps at game.Loop's default dt, time.Second/60 truncated to 16666666ns, where a whole number of nominal steps must not gain a step.
func TestWindowsAtSixtyHertz(t *testing.T) {
	dt := time.Second / 60
	free := func(c *platformer.Controller, in platformer.Input) platformer.StepResult {
		return c.Step(in, nil, nil, nil, dt)
	}

	t.Run("coyote 100ms covers 6 steps", func(t *testing.T) {
		for after, want := range map[int]platformer.JumpKind{5: platformer.JumpGround, 6: platformer.JumpNone} {
			cfg := bare()
			cfg.CoyoteTime = 100 * time.Millisecond
			c := newController(t, cfg, 0, 0)
			c.Body.Grounded = true
			free(c, platformer.Input{})
			for i := 1; i < after; i++ {
				free(c, platformer.Input{})
			}
			if got := free(c, platformer.Input{JumpPressed: true}).Jump; got != want {
				t.Fatalf("press %d steps after leaving the ground: %v, want %v", after, got, want)
			}
		}
	})

	t.Run("jump buffer 100ms covers 6 steps", func(t *testing.T) {
		for land, want := range map[int]platformer.JumpKind{5: platformer.JumpGround, 6: platformer.JumpNone} {
			cfg := bare()
			cfg.JumpBuffer = 100 * time.Millisecond
			c := newController(t, cfg, 0, 0)
			free(c, platformer.Input{JumpPressed: true, JumpHeld: true})
			for i := 1; i < land; i++ {
				free(c, platformer.Input{JumpHeld: true})
			}
			c.Body.Grounded = true
			if got := free(c, platformer.Input{JumpHeld: true}).Jump; got != want {
				t.Fatalf("landing %d steps after the press: %v, want %v", land, got, want)
			}
		}
	})

	t.Run("wall jump lock 150ms covers 9 steps", func(t *testing.T) {
		cfg := bare()
		cfg.RunSpeed = 100
		cfg.WallJumpSpeedX = 150
		cfg.WallJumpSpeedY = 250
		cfg.WallJumpLock = 150 * time.Millisecond
		c := newController(t, cfg, 0, 0)
		c.Body.Body.OnLeftWall = true
		if r := free(c, platformer.Input{JumpPressed: true, JumpHeld: true}); r.Jump != platformer.JumpWall {
			t.Fatalf("wall jump: %v", r.Jump)
		}
		for i := 1; i <= 8; i++ {
			if r := free(c, platformer.Input{Move: -1}); !r.InputLocked {
				t.Fatalf("step %d after the wall jump was not locked", i)
			}
		}
		if r := free(c, platformer.Input{Move: -1}); r.InputLocked {
			t.Fatal("step 9 after the wall jump was still locked")
		}
	})
}

func TestNonFiniteMove(t *testing.T) {
	cfg := bare()
	cfg.RunSpeed = 100
	for _, tc := range []struct {
		move float32
		want float32
	}{
		{float32(math.NaN()), 0},
		{float32(math.Inf(1)), 100},
		{float32(math.Inf(-1)), -100},
	} {
		c := newController(t, cfg, 0, 0)
		c.Body.Body.VX = 50
		c.Body.Grounded = true
		stepFree(c, platformer.Input{Move: tc.move})
		if c.Body.Body.VX != tc.want {
			t.Fatalf("move %v: VX=%v, want %v", tc.move, c.Body.Body.VX, tc.want)
		}
	}
}

func TestBufferedPressBecomesWallJump(t *testing.T) {
	cfg := bare()
	cfg.JumpBuffer = 50 * time.Millisecond
	cfg.WallJumpSpeedX = 100
	cfg.WallJumpSpeedY = 200
	c := newController(t, cfg, 0, 0)
	if r := stepFree(c, platformer.Input{JumpPressed: true, JumpHeld: true}); r.Jump != platformer.JumpNone {
		t.Fatalf("press in open air: %v", r.Jump)
	}
	stepFree(c, platformer.Input{JumpHeld: true})
	c.Body.Body.OnLeftWall = true // the previous step reached the wall
	if r := stepFree(c, platformer.Input{JumpHeld: true}); r.Jump != platformer.JumpWall || c.Body.Body.VX != 100 {
		t.Fatalf("buffered press on reaching the wall: %v VX=%v, want wall jump and 100", r.Jump, c.Body.Body.VX)
	}
}

func TestSetConfigKeepsElapsedTime(t *testing.T) {
	for _, tc := range []struct {
		coyote time.Duration
		want   platformer.JumpKind
	}{
		{20 * time.Millisecond, platformer.JumpNone},   // 30ms have passed by the press
		{50 * time.Millisecond, platformer.JumpGround}, // still open
	} {
		cfg := bare()
		cfg.CoyoteTime = 100 * time.Millisecond
		c := newController(t, cfg, 0, 0)
		c.Body.Grounded = true
		stepFree(c, platformer.Input{})
		stepFree(c, platformer.Input{})
		stepFree(c, platformer.Input{})
		cfg.CoyoteTime = tc.coyote
		if err := c.SetConfig(cfg); err != nil {
			t.Fatal(err)
		}
		if got := stepFree(c, platformer.Input{JumpPressed: true}).Jump; got != tc.want {
			t.Fatalf("coyote shortened to %v: %v, want %v", tc.coyote, got, tc.want)
		}
	}
}

func TestControllerStepWorldDoorOneWayAndSlopes(t *testing.T) {
	const dt = time.Second / 60

	grid := physics.NewGrid(20, 10, 16, 16)
	grid.FillRect(0, 8, 20, 2, physics.TileSolid) // floor top at Y=128

	w := &physics.World{Tiles: grid, GravityY: 999} // StepWorld must not double-apply World.GravityY when Body.World is nil
	door := physics.NewBoxBody(96, 80, 8, 48)
	door.Tag = "door"
	owPlat := physics.NewBoxBody(24, 104, 40, 8)
	owPlat.OneWay = true
	w.Add(door, owPlat)

	cfg := platformer.DefaultConfig()
	c := newController(t, cfg, 32, 80)
	w.Add(c.Body.Body)

	// 1. Land on the one-way body.
	for range 30 {
		c.StepWorld(platformer.Input{}, w, nil, nil, dt)
	}
	if !c.Body.Grounded || !near(c.Body.Body.Shape.Box.Bottom(), 104) {
		t.Fatalf("failed to land on one-way body: grounded=%v bottom=%v", c.Body.Grounded, c.Body.Body.Shape.Box.Bottom())
	}

	// 2. Drop through the one-way body onto the tile floor.
	c.Body.Body.OneWayDrop = true
	for i := range 30 {
		if i == 4 {
			c.Body.Body.OneWayDrop = false
		}
		c.StepWorld(platformer.Input{}, w, nil, nil, dt)
	}
	if !c.Body.Grounded || !near(c.Body.Body.Shape.Box.Bottom(), 128) {
		t.Fatalf("failed to drop through one-way body: grounded=%v bottom=%v", c.Body.Grounded, c.Body.Body.Shape.Box.Bottom())
	}

	// 3. Run right into the closed door body: stopped flush and wall-jump works off it.
	for range 60 {
		c.StepWorld(platformer.Input{Move: 1}, w, nil, nil, dt)
	}
	if !near(c.Body.Body.Shape.Box.Right(), 96) || !c.Body.Body.OnRightWall {
		t.Fatalf("controller not stopped by closed door: right=%v onRightWall=%v", c.Body.Body.Shape.Box.Right(), c.Body.Body.OnRightWall)
	}

	// 4. Remove the door body: controller walks right through it and onto a moving platform.
	w.Remove(door)
	plat := &physics.MovingPlatform{
		Body:   physics.NewBoxBody(120, 128, 48, 8),
		OneWay: true,
	}
	w.Add(plat.Body)
	for range 45 {
		plat.DeltaX, plat.DeltaY = 0, 0
		c.StepWorld(platformer.Input{Move: 1}, w, nil, []*physics.MovingPlatform{plat}, dt)
	}
	if c.Body.Body.Shape.Box.X <= 110 {
		t.Fatalf("controller did not walk through removed door: X=%v", c.Body.Body.Shape.Box.X)
	}
}

func TestControllerStepZeroAllocs(t *testing.T) {
	const dt = time.Second / 60

	grid := physics.NewGrid(20, 12, 16, 16)
	grid.FillRect(0, 10, 20, 2, physics.TileSolid) // floor top at Y=160
	grid.FillRect(19, 0, 1, 12, physics.TileSolid) // right wall X=304..320

	t.Run("Step", func(t *testing.T) {
		c := newController(t, platformer.DefaultConfig(), 64, 144)
		run := func() {
			c.Body.Body.Shape.Box.X = 64
			c.Body.Body.Shape.Box.Y = 144
			c.Step(platformer.Input{Move: 1}, grid, nil, nil, dt)
		}
		run()
		if !c.Body.Grounded {
			t.Fatal("expected grounded after warm-up step")
		}
		gamedebug.AssertZeroAllocs(t, 100, run)
	})

	t.Run("StepWorld", func(t *testing.T) {
		w := &physics.World{Tiles: grid}
		door := physics.NewBoxBody(160, 80, 16, 80)
		c := newController(t, platformer.DefaultConfig(), 148, 144)
		w.Add(door, c.Body.Body)
		run := func() {
			c.Body.Body.Shape.Box.X = 148
			c.Body.Body.Shape.Box.Y = 144
			c.StepWorld(platformer.Input{Move: 1}, w, nil, nil, dt)
		}
		run()
		if !c.Body.Grounded || !c.Body.Body.OnRightWall {
			t.Fatalf("expected grounded and right wall after warm-up, got grounded=%v wall=%v", c.Body.Grounded, c.Body.Body.OnRightWall)
		}
		gamedebug.AssertZeroAllocs(t, 100, run)
	})
}

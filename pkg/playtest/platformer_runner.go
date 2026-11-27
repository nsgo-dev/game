package playtest

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/random"
)

// ProfilePlatformerRunner is the built-in greedy side-scrolling platformer bot profile.
const ProfilePlatformerRunner = "platformer-runner"

func init() {
	RegisterBot(ProfilePlatformerRunner, NewPlatformerRunner)
}

// PlatformerObservation is the per-step world view a platformer simulation supplies to the "platformer-runner" bot.
type PlatformerObservation struct {
	// PlayerX and PlayerY are the player's current world position (box center).
	PlayerX, PlayerY float64
	// PlayerVX and PlayerVY are the player's current velocity in points per second.
	PlayerVX, PlayerVY float64
	// Grounded reports whether the player is currently standing on ground.
	Grounded bool
	// OnWall is -1 when touching a wall on the left, 1 on the right, and 0 otherwise.
	OnWall int
	// ExitX and ExitY are the target exit or goal world coordinates.
	ExitX, ExitY float64
	// GapAhead reports a drop-off or missing floor tile immediately ahead in the travel direction.
	GapAhead bool
	// WallAhead reports a solid wall or step immediately ahead in the travel direction.
	WallAhead bool
	// HazardAhead reports a hazard (such as spikes or a patrolling enemy) immediately ahead.
	HazardAhead bool
	// HazardDistance is the horizontal distance to the nearest hazard ahead, when known.
	HazardDistance float64
	// LedgeAbove reports a reachable higher platform or ledge ahead and above the player.
	LedgeAbove bool
}

// PlatformerRunner is the built-in "platformer-runner" bot: a greedy runner toward ExitX that jumps at ledges, gaps and hazards with deterministic human reaction jitter.
type PlatformerRunner struct {
	reactionMin time.Duration
	reactionMax time.Duration
	jumpHoldMin time.Duration
	jumpHoldMax time.Duration

	leftAction  string
	rightAction string
	jumpAction  string
	moveAxis    string

	// Per-run state.
	reacting      bool
	reactSteps    int
	holdingJump   bool
	holdStepsLeft int
	wasTrigger    bool
	lastDir       int
	stuckSteps    int
	lastProgressX float64
	hasProgress   bool
}

// NewPlatformerRunner constructs a PlatformerRunner from a playtest Bot configuration.
//
// Supported optional Bot.Params:
//   - "leftAction" (string, default "left")
//   - "rightAction" (string, default "right")
//   - "jumpAction" (string, default "jump")
//   - "moveAxis" (string, default "move_x")
//   - "jumpHoldMs" (number, default 220): base jump hold duration in milliseconds.
//   - "jumpHoldJitterMs" (number, default 40): +/- jitter applied to jump hold duration.
func NewPlatformerRunner(cfg BotConfig) (Bot, error) {
	b := &PlatformerRunner{
		reactionMin: cfg.ReactionMin,
		reactionMax: cfg.ReactionMax,
		jumpHoldMin: 180 * time.Millisecond,
		jumpHoldMax: 260 * time.Millisecond,
		leftAction:  "left",
		rightAction: "right",
		jumpAction:  "jump",
		moveAxis:    "move_x",
		lastDir:     1,
	}
	if b.reactionMax < b.reactionMin {
		b.reactionMax = b.reactionMin
	}
	if cfg.Params != nil {
		if v, ok := cfg.Params["leftAction"]; ok {
			s, ok := v.(string)
			if !ok || s == "" {
				return nil, fmt.Errorf("playtest: platformer-runner param leftAction must be a non-empty string")
			}
			b.leftAction = s
		}
		if v, ok := cfg.Params["rightAction"]; ok {
			s, ok := v.(string)
			if !ok || s == "" {
				return nil, fmt.Errorf("playtest: platformer-runner param rightAction must be a non-empty string")
			}
			b.rightAction = s
		}
		if v, ok := cfg.Params["jumpAction"]; ok {
			s, ok := v.(string)
			if !ok || s == "" {
				return nil, fmt.Errorf("playtest: platformer-runner param jumpAction must be a non-empty string")
			}
			b.jumpAction = s
		}
		if v, ok := cfg.Params["moveAxis"]; ok {
			s, ok := v.(string)
			if !ok || s == "" {
				return nil, fmt.Errorf("playtest: platformer-runner param moveAxis must be a non-empty string")
			}
			b.moveAxis = s
		}
		baseHold := 220.0
		holdJitter := 40.0
		if v, ok := cfg.Params["jumpHoldMs"]; ok {
			n, ok := asFloat64(v)
			if !ok || n < 0 {
				return nil, fmt.Errorf("playtest: platformer-runner param jumpHoldMs must be a non-negative number")
			}
			baseHold = n
		}
		if v, ok := cfg.Params["jumpHoldJitterMs"]; ok {
			n, ok := asFloat64(v)
			if !ok || n < 0 {
				return nil, fmt.Errorf("playtest: platformer-runner param jumpHoldJitterMs must be a non-negative number")
			}
			holdJitter = n
		}
		lo := max(0.0, baseHold-holdJitter)
		hi := baseHold + holdJitter
		b.jumpHoldMin = time.Duration(lo * float64(time.Millisecond))
		b.jumpHoldMax = time.Duration(hi * float64(time.Millisecond))
	}
	return b, nil
}

// Channels declares the default action and axis names that platformer-runner drives.
func (b *PlatformerRunner) Channels() (actions, axes []string) {
	return []string{b.leftAction, b.rightAction, b.jumpAction}, []string{b.moveAxis}
}

// Decide picks the next horizontal movement and jump inputs for a platformer step.
func (b *PlatformerRunner) Decide(_ int, dt time.Duration, rawObs any, rng *random.Stream) InputState {
	var obs PlatformerObservation
	switch v := rawObs.(type) {
	case PlatformerObservation:
		obs = v
	case *PlatformerObservation:
		if v != nil {
			obs = *v
		}
	}

	dir := b.lastDir
	dx := obs.ExitX - obs.PlayerX
	if math.Abs(dx) > 2 {
		if dx > 0 {
			dir = 1
		} else {
			dir = -1
		}
	}
	b.lastDir = dir

	// Detect stalling while grounded so the runner attempts a jump over unflagged steps.
	if !b.hasProgress {
		b.lastProgressX = obs.PlayerX
		b.hasProgress = true
	} else if math.Abs(obs.PlayerX-b.lastProgressX) >= 1.5 {
		b.lastProgressX = obs.PlayerX
		b.stuckSteps = 0
	} else if obs.Grounded {
		b.stuckSteps++
	}
	stuckLimit := durationToSteps(350*time.Millisecond, dt)
	if stuckLimit < 1 {
		stuckLimit = 1
	}
	stuck := b.stuckSteps >= stuckLimit

	// Trigger jump on gaps, walls, hazards, reachable ledges above, wall contact in air, or getting stuck.
	trigger := obs.GapAhead || obs.WallAhead || obs.HazardAhead || obs.LedgeAbove || (!obs.Grounded && obs.OnWall != 0 && obs.OnWall == dir) || stuck

	// Continue an active jump hold until the timer expires or the player starts falling/lands.
	if b.holdingJump {
		if b.holdStepsLeft <= 0 || (obs.Grounded && obs.PlayerVY >= 0) || obs.PlayerVY > 20 {
			b.holdingJump = false
			b.holdStepsLeft = 0
		} else {
			b.holdStepsLeft--
		}
	}

	// Reaction jitter state machine: on a new trigger, wait a deterministic human reaction delay before pressing jump.
	if !b.holdingJump {
		if trigger {
			if !b.reacting && !b.wasTrigger {
				b.reacting = true
				b.reactSteps = sampleDurationSteps(b.reactionMin, b.reactionMax, dt, rng)
			} else if !b.reacting && obs.Grounded {
				// Still facing a trigger after landing from a previous jump: queue a new reaction.
				b.reacting = true
				b.reactSteps = sampleDurationSteps(b.reactionMin, b.reactionMax, dt, rng)
			}
			if b.reacting {
				if b.reactSteps <= 0 {
					b.reacting = false
					b.holdingJump = true
					b.holdStepsLeft = sampleDurationSteps(b.jumpHoldMin, b.jumpHoldMax, dt, rng)
					if b.holdStepsLeft < 1 {
						b.holdStepsLeft = 1
					}
					b.stuckSteps = 0
				} else {
					b.reactSteps--
				}
			}
		} else {
			b.reacting = false
			b.reactSteps = 0
		}
	}
	b.wasTrigger = trigger

	actions := map[string]bool{
		b.leftAction:  dir < 0,
		b.rightAction: dir > 0,
		b.jumpAction:  b.holdingJump,
	}
	axes := map[string]float64{
		b.moveAxis: clampFloat(float64(dir), -1, 1),
	}
	return InputState{Actions: actions, Axes: axes}
}

// ObservePlatformerGrid builds a PlatformerObservation by probing a physics.TileGrid and optional hazard boxes ahead of playerBox toward (exitX, exitY).
// Lookahead distances default to sensible values relative to the grid's tile width when lookahead <= 0.
func ObservePlatformerGrid(grid physics.TileGrid, playerBox physics.Box, vx, vy float32, grounded bool, onWall int, exitX, exitY float32, hazards []physics.Box, lookahead float32) PlatformerObservation {
	cx, cy := playerBox.Center()
	obs := PlatformerObservation{
		PlayerX:  float64(cx),
		PlayerY:  float64(cy),
		PlayerVX: float64(vx),
		PlayerVY: float64(vy),
		Grounded: grounded,
		OnWall:   onWall,
		ExitX:    float64(exitX),
		ExitY:    float64(exitY),
	}
	dir := float32(1)
	if exitX < cx {
		dir = -1
	}
	if grid == nil {
		return obs
	}
	tw, th := grid.TileSize()
	if tw <= 0 || th <= 0 {
		return obs
	}
	var ox, oy float32
	if og, ok := grid.(physics.TileGridOrigin); ok {
		ox, oy = og.GridOrigin()
	}
	if lookahead <= 0 {
		lookahead = tw * 1.5
	}

	frontX := playerBox.Right() + 2
	if dir < 0 {
		frontX = playerBox.Left() - 2
	}
	probeX := frontX + float32(float32(dir*tw)*0.6)
	footY := playerBox.Bottom() + 2
	midY := cy

	// Check for a solid wall or step directly ahead within lookahead.
	for _, dist := range []float32{2, tw * 0.6, lookahead} {
		px := frontX + float32(dir*dist)
		col := int(math.Floor(float64((px - ox) / tw)))
		rowMid := int(math.Floor(float64((midY - oy) / th)))
		rowLow := int(math.Floor(float64((playerBox.Bottom() - 2 - oy) / th)))
		if grid.TileFlagsAt(col, rowMid)&physics.TileSolid != 0 || grid.TileFlagsAt(col, rowLow)&physics.TileSolid != 0 {
			obs.WallAhead = true
			break
		}
	}

	// Check for a floor gap ahead when standing on ground.
	if grounded {
		col := int(math.Floor(float64((probeX - ox) / tw)))
		rowBelow := int(math.Floor(float64((footY - oy) / th)))
		rowBelow2 := rowBelow + 1
		t1 := grid.TileFlagsAt(col, rowBelow)
		t2 := grid.TileFlagsAt(col, rowBelow2)
		if t1&(physics.TileSolid|physics.TileOneWay) == 0 && t2&(physics.TileSolid|physics.TileOneWay) == 0 {
			obs.GapAhead = true
		}
	}

	// Check for a reachable ledge above ahead when the exit is higher than the player.
	if grounded && exitY < cy-th {
		for _, dist := range []float32{tw * 0.5, tw * 1.5} {
			col := int(math.Floor(float64((cx + float32(dir*dist) - ox) / tw)))
			rowUp := int(math.Floor(float64((playerBox.Top() - float32(th*0.8) - oy) / th)))
			t := grid.TileFlagsAt(col, rowUp)
			if t&(physics.TileOneWay|physics.TileSolid) != 0 {
				obs.LedgeAbove = true
				break
			}
		}
	}

	// Check for hazards ahead within lookahead*2 horizontally and overlapping vertical band.
	hazardRange := lookahead * 2.2
	minDist := math.Inf(1)
	for _, h := range hazards {
		hcx, hcy := h.Center()
		deltaX := (hcx - cx) * dir
		if deltaX < -playerBox.W*0.5 || deltaX > hazardRange {
			continue
		}
		if math.Abs(float64(hcy-cy)) > float64(playerBox.H+h.H) {
			continue
		}
		if float64(deltaX) < minDist {
			minDist = float64(deltaX)
		}
	}
	if !math.IsInf(minDist, 1) {
		obs.HazardAhead = true
		obs.HazardDistance = cleanZero(max(0, minDist))
	}
	return obs
}

func sampleDurationSteps(minD, maxD, dt time.Duration, rng *random.Stream) int {
	if maxD < minD {
		maxD = minD
	}
	d := minD
	if maxD > minD && rng != nil {
		span := float64(maxD - minD)
		d = minD + time.Duration(rng.Float64()*span)
	}
	return durationToSteps(d, dt)
}

func durationToSteps(d, dt time.Duration) int {
	if d <= 0 || dt <= 0 {
		return 0
	}
	return int(math.Round(d.Seconds() / dt.Seconds()))
}

func asFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float64:
		return x, isFinite(x)
	case float32:
		return float64(x), isFinite(float64(x))
	default:
		return 0, false
	}
}

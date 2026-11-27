package playtest

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/random"
)

// ProfileShooter is the built-in shoot-'em-up bot profile for shmup2d templates.
const ProfileShooter = "shooter"

const maxShooterLookaheadMs = 60_000

func init() {
	RegisterBot(ProfileShooter, NewShooterBot)
}

// ShooterBullet describes one active enemy projectile on the playfield.
type ShooterBullet struct {
	X, Y   float64
	VX, VY float64
	Radius float64
}

// ShooterTarget describes one active enemy or boss on the playfield.
type ShooterTarget struct {
	X, Y   float64
	Radius float64
	IsBoss bool
}

// ShooterPowerUp describes one collectible power-up drifting on the playfield.
type ShooterPowerUp struct {
	ID   string
	Kind string
	X, Y float64
}

// ShooterObservation is the per-step world view a shoot-'em-up simulation supplies to the "shooter" bot.
type ShooterObservation struct {
	// PlayfieldWidth and PlayfieldHeight are the stage dimensions in game points.
	PlayfieldWidth, PlayfieldHeight float64
	// ShipX, ShipY and ShipRadius are the player ship's coordinates and hit radius.
	ShipX, ShipY, ShipRadius float64
	// ShipAlive reports whether the player ship is alive.
	ShipAlive bool
	// Invulnerable reports whether the ship is currently invulnerable.
	Invulnerable bool
	// Bombs is the number of bombs currently available.
	Bombs int
	// Wave is the 1-based current wave index (0 if not in a wave).
	Wave int
	// Bullets lists active enemy projectiles.
	Bullets []ShooterBullet
	// Enemies lists active on-screen enemies and bosses.
	Enemies []ShooterTarget
	// PowerUps lists active power-ups drifting on the playfield.
	PowerUps []ShooterPowerUp
}

type shooterSnapshot struct {
	bullets  []ShooterBullet
	enemies  []ShooterTarget
	powerUps []ShooterPowerUp
}

// ShooterBot is the built-in "shooter" bot: it fires constantly, dodges incoming bullets with a fixed (or bounded) reaction time delay, and collects power-ups within a configurable radius.
type ShooterBot struct {
	reactionMin  time.Duration
	reactionMax  time.Duration
	powerUpRange float64
	dangerRange  float64
	lookahead    time.Duration
	useBomb      bool

	fireAction  string
	focusAction string
	bombAction  string
	moveXAxis   string
	moveYAxis   string

	// Per-run deterministic state.
	history     []shooterSnapshot
	dodgeBias   float64
	biasInit    bool
	delaySteps  int
	delayInited bool
}

// NewShooterBot constructs a ShooterBot from a playtest BotConfig.
//
// If both ReactionMin and ReactionMax are 0, ShooterBot defaults to a fixed 180ms reaction time.
//
// Supported optional Bot.Params:
//   - "powerUpRange" (number, default 80): distance in points within which the bot steers toward a power-up.
//   - "dangerRange" (number, default 36): clearance distance in points for dodging incoming bullets.
//   - "lookaheadMs" (number, default 400): straight-line bullet prediction horizon in milliseconds.
//   - "useBomb" (bool, default false): whether to trigger a bomb on an unavoidable hit.
//   - "fireAction" (string, default "fire")
//   - "focusAction" (string, default "focus")
//   - "bombAction" (string, default "bomb")
//   - "moveXAxis" (string, default "moveX")
//   - "moveYAxis" (string, default "moveY")
func NewShooterBot(cfg BotConfig) (Bot, error) {
	rMin := cfg.ReactionMin
	rMax := cfg.ReactionMax
	if rMin == 0 && rMax == 0 {
		rMin = 180 * time.Millisecond
		rMax = 180 * time.Millisecond
	}
	if rMax < rMin {
		rMax = rMin
	}
	b := &ShooterBot{
		reactionMin:  rMin,
		reactionMax:  rMax,
		powerUpRange: 80,
		dangerRange:  36,
		lookahead:    400 * time.Millisecond,
		useBomb:      false,
		fireAction:   "fire",
		focusAction:  "focus",
		bombAction:   "bomb",
		moveXAxis:    "moveX",
		moveYAxis:    "moveY",
	}
	if cfg.Params != nil {
		if v, ok := cfg.Params["powerUpRange"]; ok {
			n, ok := asFloat64(v)
			if !ok || n < 0 {
				return nil, fmt.Errorf("playtest: shooter param powerUpRange must be a non-negative number")
			}
			b.powerUpRange = n
		}
		if v, ok := cfg.Params["dangerRange"]; ok {
			n, ok := asFloat64(v)
			if !ok || n <= 0 {
				return nil, fmt.Errorf("playtest: shooter param dangerRange must be a positive number")
			}
			b.dangerRange = n
		}
		if v, ok := cfg.Params["lookaheadMs"]; ok {
			n, ok := asFloat64(v)
			if !ok || n < 0 || n > maxShooterLookaheadMs {
				return nil, fmt.Errorf("playtest: shooter param lookaheadMs must be in [0, %d]", maxShooterLookaheadMs)
			}
			b.lookahead = time.Duration(n * float64(time.Millisecond))
		}
		if v, ok := cfg.Params["useBomb"]; ok {
			flag, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("playtest: shooter param useBomb must be a boolean")
			}
			b.useBomb = flag
		}
		strParams := []struct {
			key string
			dst *string
		}{
			{"fireAction", &b.fireAction},
			{"focusAction", &b.focusAction},
			{"bombAction", &b.bombAction},
			{"moveXAxis", &b.moveXAxis},
			{"moveYAxis", &b.moveYAxis},
		}
		for _, sp := range strParams {
			if v, ok := cfg.Params[sp.key]; ok {
				s, ok := v.(string)
				if !ok || s == "" {
					return nil, fmt.Errorf("playtest: shooter param %s must be a non-empty string", sp.key)
				}
				*sp.dst = s
			}
		}
	}
	return b, nil
}

// Channels declares the default action and axis names that shooter drives.
func (b *ShooterBot) Channels() (actions, axes []string) {
	return []string{b.fireAction, b.focusAction, b.bombAction}, []string{b.moveXAxis, b.moveYAxis}
}

// Decide picks ship movement, firing, focus and optional bomb inputs for one step using a reaction-delayed perception buffer.
func (b *ShooterBot) Decide(_ int, dt time.Duration, rawObs any, rng *random.Stream) InputState {
	var obs ShooterObservation
	switch v := rawObs.(type) {
	case ShooterObservation:
		obs = v
	case *ShooterObservation:
		if v != nil {
			obs = *v
		}
	default:
		return InputState{}
	}

	if !b.biasInit {
		b.biasInit = true
		if rng != nil && rng.Float64() < 0.5 {
			b.dodgeBias = -1
		} else {
			b.dodgeBias = 1
		}
	}
	if !b.delayInited {
		b.delayInited = true
		b.delaySteps = sampleDurationSteps(b.reactionMin, b.reactionMax, dt, rng)
		if b.delaySteps < 0 {
			b.delaySteps = 0
		}
	}

	// Push current observation into the perception delay queue.
	snap := shooterSnapshot{
		bullets:  slices.Clone(obs.Bullets),
		enemies:  slices.Clone(obs.Enemies),
		powerUps: slices.Clone(obs.PowerUps),
	}
	b.history = append(b.history, snap)
	maxHist := b.delaySteps + 1
	if len(b.history) > maxHist {
		drop := len(b.history) - maxHist
		copy(b.history, b.history[drop:])
		clear(b.history[maxHist:])
		b.history = b.history[:maxHist]
	}

	if !obs.ShipAlive {
		return InputState{}
	}

	// Perceived world is the snapshot from reactionSteps ago (or empty before reaction time has elapsed).
	var perceived shooterSnapshot
	if len(b.history) > b.delaySteps {
		perceived = b.history[len(b.history)-1-b.delaySteps]
	}

	w := obs.PlayfieldWidth
	h := obs.PlayfieldHeight
	if !isFinite(w) || w <= 0 {
		w = 270
	}
	if !isFinite(h) || h <= 0 {
		h = 480
	}
	shipX := obs.ShipX
	shipY := obs.ShipY
	if !isFinite(shipX) {
		shipX = w / 2
	}
	if !isFinite(shipY) {
		shipY = h * 0.84
	}
	shipRadius := obs.ShipRadius
	if !isFinite(shipRadius) || shipRadius < 0 {
		shipRadius = 0
	}

	// Default anchor: center bottom-fifth of the playfield.
	tx := w / 2
	ty := h * 0.84

	// 1. Seek the nearest power-up within powerUpRange.
	bestPU := math.Inf(1)
	foundPU := false
	for _, pu := range perceived.powerUps {
		if !isFinite(pu.X) || !isFinite(pu.Y) {
			continue
		}
		d := math.Hypot(pu.X-shipX, pu.Y-shipY)
		if d <= b.powerUpRange && d < bestPU {
			bestPU = d
			tx = pu.X
			ty = max(pu.Y, h*0.55)
			foundPU = true
		}
	}

	// 2. Otherwise align horizontally under the boss or the lowest on-screen enemy.
	if !foundPU {
		var boss *ShooterTarget
		for i := range perceived.enemies {
			if perceived.enemies[i].IsBoss && isFinite(perceived.enemies[i].X) && isFinite(perceived.enemies[i].Y) {
				boss = &perceived.enemies[i]
				break
			}
		}
		if boss != nil {
			tx = boss.X
		} else {
			lowestY := -1.0
			for _, e := range perceived.enemies {
				if !isFinite(e.X) || !isFinite(e.Y) {
					continue
				}
				if e.Y > lowestY && e.Y < shipY-32 {
					lowestY = e.Y
					tx = e.X
				}
			}
		}
	}

	ax := (tx - shipX) / 40.0
	ay := (ty - shipY) / 40.0

	// 3. Dodge incoming bullets and ramming enemies from the reaction-delayed snapshot.
	var rx, ry float64
	threat := math.Inf(1)
	push := func(px, py, radius float64) {
		if !isFinite(px) || !isFinite(py) || !isFinite(radius) {
			return
		}
		dx := shipX - px
		dy := shipY - py
		d := math.Hypot(dx, dy) - radius - shipRadius
		if d > b.dangerRange {
			return
		}
		if d < 0.5 {
			d = 0.5
		}
		if d < threat {
			threat = d
		}
		if math.Hypot(dx, dy) < 1e-3 {
			// Exact head-on collision: break symmetry deterministically using seeded dodgeBias.
			dx = b.dodgeBias
			dy = 0.25
		}
		inv := 65.0 / (d * d)
		// Explicit float64 conversions inhibit arm64 FMA contraction for cross-arch determinism.
		rx += float64(dx * inv)
		ry += float64(dy * inv)
	}

	// Advance each perceived bullet by its own velocity over [0, lookahead] to dodge its trajectory.
	sampleStep := 80 * time.Millisecond
	if b.lookahead <= 0 {
		for _, bul := range perceived.bullets {
			push(bul.X, bul.Y, bul.Radius)
		}
	} else {
		for _, bul := range perceived.bullets {
			for t := time.Duration(0); t <= b.lookahead; t += sampleStep {
				secs := t.Seconds()
				push(bul.X+float64(bul.VX*secs), bul.Y+float64(bul.VY*secs), bul.Radius)
			}
		}
	}
	for _, e := range perceived.enemies {
		push(e.X, e.Y+12, e.Radius+4)
	}

	// Soft playfield wall repulsion prevents cornering during intense bullet patterns.
	rx += 28.0/max(shipX-10, 1) - 28.0/max(w-10-shipX, 1)
	ry += 28.0/max(shipY-float64(h*0.45), 1) - 28.0/max(h-10-shipY, 1)

	mx := clampFloat(ax+rx, -1, 1)
	my := clampFloat(ay+ry, -1, 1)
	focus := threat < 12
	bomb := b.useBomb && threat < 3 && !obs.Invulnerable && obs.Bombs > 0

	actions := map[string]bool{
		b.fireAction:  true,
		b.focusAction: focus,
		b.bombAction:  bomb,
	}
	axes := map[string]float64{
		b.moveXAxis: mx,
		b.moveYAxis: my,
	}
	return InputState{Actions: actions, Axes: axes}
}

// RecordShooterWave registers waveLabel (such as "wave 1") on the "hits_per_wave" series with 0 initial hits so waves with zero hits still appear in the report.
func RecordShooterWave(metrics *Collector, waveLabel string, targetMaxHits *float64) {
	if metrics == nil || waveLabel == "" {
		return
	}
	metrics.ConfigureSeries("hits_per_wave", "Hits taken per wave", "hits", AggMedian, targetMaxHits)
	metrics.AddSeries("hits_per_wave", waveLabel, 0)
}

// RecordShooterHit records a ship hit at (x, y) during waveLabel on both the "hit" event stream and the "hits_per_wave" series.
func RecordShooterHit(metrics *Collector, waveLabel string, x, y float64) {
	if metrics == nil {
		return
	}
	metrics.Event("hit", x, y, waveLabel)
	metrics.Inc("hits_taken")
	if waveLabel != "" {
		metrics.ConfigureSeries("hits_per_wave", "Hits taken per wave", "hits", AggMedian, nil)
		metrics.AddSeries("hits_per_wave", waveLabel, 1)
	}
}

// RecordShooterPowerUp records a spawned or collected power-up so power-ups taken % is tracked accurately.
func RecordShooterPowerUp(metrics *Collector, collected bool, x, y float64, kind string) {
	if metrics == nil {
		return
	}
	if collected {
		metrics.Event("powerup", x, y, kind)
		metrics.Inc("powerups_taken")
	} else {
		metrics.Inc("powerups_total")
	}
}

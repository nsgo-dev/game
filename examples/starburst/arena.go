package main

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// ArenaWidth and ArenaHeight define the playable world bounds that the camera pans over.
	ArenaWidth  = 960
	ArenaHeight = 540

	// StepRate and FixedDT match the 60 Hz fixed-step game.Loop.
	StepRate = 60
	FixedDT  = time.Second / StepRate

	// DefaultSeed seeds deterministic waves, spawns, and particle streams.
	DefaultSeed uint64 = 0x57A8B057

	playerMaxHP     = 5
	playerRadius    = 7.0
	playerAccel     = 840.0
	playerMaxSpeed  = 168.0
	playerDrag      = 6.5
	playerInvulnSec = 0.90
	playerFireRate  = 0.11
	novaCooldownSec = 3.50
	novaBlastRadius = 92.0
	orbMagnetRadius = 68.0
	orbRadius       = 4.5
	bulletRadius    = 3.5

	maxEnemies      = 1200
	maxBullets      = 512
	maxOrbs         = 512
	maxBatchSprites = 2600

	// StressMode sprite population targets (>= 1000 active moving sprites at all times).
	stressTargetEnemies = 760
	stressTargetBullets = 200
	stressTargetOrbs    = 100

	// Fixed-size spatial hash grid for O(N) zero-alloc circle collision queries.
	gridCols  = 16
	gridRows  = 9
	gridCells = gridCols * gridRows
	gridCellW = float32(ArenaWidth) / float32(gridCols)
	gridCellH = float32(ArenaHeight) / float32(gridRows)

	keyHoldDuration = 0.19
)

// Touch control regions in virtual coordinates, as game.yaml binds them into input.Actions.
var (
	touchMoveLeftRect  = projectRegion("move_left")
	touchMoveRightRect = projectRegion("move_right")
	touchMoveUpRect    = projectRegion("move_up")
	touchMoveDownRect  = projectRegion("move_down")
	touchFireRect      = projectRegion("fire")
	touchNovaRect      = projectRegion("nova")
)

// EnemyKind identifies the visual frame and behavior of a hostile unit.
type EnemyKind uint8

const (
	EnemySwarm EnemyKind = iota
	EnemyDrone
)

// Player holds the starfighter's kinematic and combat state.
type Player struct {
	X, Y           float32
	VX, VY         float32
	AimAngle       float32
	HP             int
	MaxHP          int
	InvulnTimer    float32
	FireCooldown   float32
	NovaCooldown   float32
	ThrusterAccum  float32
	RecoilOffset   float32
	ShotParity     bool
	ShieldedStress bool
}

// Enemy is a pooled hostile unit in the arena.
type Enemy struct {
	Kind      EnemyKind
	X, Y      float32
	VX, VY    float32
	Radius    float32
	Speed     float32
	Angle     float32
	Spin      float32
	OrbitSign float32
	HP        int
	MaxHP     int
	Score     int
	HitFlash  float32
}

// Bullet is a pooled player plasma projectile.
type Bullet struct {
	X, Y   float32
	VX, VY float32
	Angle  float32
	Radius float32
	Damage int
	Life   float32
}

// Orb is a pooled glowing XP/score drop pulled magnetically toward the player.
type Orb struct {
	X, Y   float32
	VX, VY float32
	Value  int
	Phase  float32
	Life   float32
}

type starDot struct {
	x, y     float32
	size     float32
	parallax float32
	color    core.Color
}

// ControlInput holds normalized per-step movement, aim, and action state from any input device.
//
// Touch/mouse (`input.Actions`) and keyboard listeners populate this struct today.
// Hardware gamepad bindings await nsgo#1049; when available, poll left/right sticks and triggers
// directly into MoveX/MoveY, AimStickX/AimStickY, Fire, and Nova with no simulation changes.
type ControlInput struct {
	MoveX, MoveY float32
	// AimWorldX/AimWorldY set an explicit world target (e.g. mouse cursor) when HasWorldAim is true.
	AimWorldX, AimWorldY float32
	HasWorldAim          bool
	// AimStickX/AimStickY accept twin-stick directional aim (ready for gamepad bindings in nsgo#1049).
	AimStickX, AimStickY float32
	Fire                 bool
	Nova                 bool
}

// HUDState is the lightweight snapshot mirrored into native NSGo HUD signals.
type HUDState struct {
	Wave          int
	Score         int
	Combo         int
	HP            int
	MaxHP         int
	ActiveSprites int
	ActiveEnemies int
	StressMode    bool
	Paused        bool
	GameOver      bool
	NovaReady     float32
}

// Arena owns the entire deterministic Starburst simulation, object pools, particle emitters,
// camera, viewport, and single reusable sprite.Batch.
type Arena struct {
	Atlas    *sprite.Atlas
	Camera   tilemap.Camera
	Viewport screen.Viewport

	seed     uint64
	streams  *random.Streams
	waveRNG  *random.Stream
	spawnRNG *random.Stream
	fxRNG    *random.Stream

	Player Player

	enemies []Enemy
	bullets []Bullet
	orbs    []Orb

	// Preallocated intrusive spatial grid for O(N) circle-circle collision queries.
	cellHead  [gridCells]int
	enemyNext [maxEnemies]int

	// Three deterministic particle emitters whose live particles draw through the single batch.
	explosions particles.Emitter
	sparks     particles.Emitter
	thruster   particles.Emitter

	// Single reusable sprite batch for ship, enemies, bullets, orbs, rings, and particles.
	spritePool []sprite.Sprite
	batch      sprite.Batch

	// Cached atlas frames to avoid map lookups in hot loops.
	frameShip   sprite.Frame
	frameDrone  sprite.Frame
	frameSwarm  sprite.Frame
	frameBullet sprite.Frame
	frameOrb    sprite.Frame
	frameSpark  sprite.Frame
	frameRing   sprite.Frame

	stars [72]starDot

	// Wave and scoring progression.
	Wave            int
	Score           int
	Combo           int
	comboTimer      float32
	waveRemaining   int
	spawnTimer      float32
	waveBannerTimer float32
	elapsed         float32
	Steps           uint64

	// Mode & lifecycle flags.
	StressMode bool
	Paused     bool
	GameOver   bool

	// Visual juice: screen flash & nova expanding ring.
	flashColor    core.Color
	flashAlpha    float32
	novaRingTimer float32
	novaRingX     float32
	novaRingY     float32

	// Input & keyboard hold-decay timers (since NSGo reports key-down + repeat without key-up).
	Control  ControlInput
	keyUp    float32
	keyDown  float32
	keyLeft  float32
	keyRight float32
	keyNova  bool

	// Touch/aim indicators for subtle visual feedback.
	ShowTouchHints bool
	worldBounds    core.Bounds
}

// NewArena creates a deterministic Starburst arena with a procedural pixel-art atlas
// and preallocated pools ready for 60 Hz simulation and 1000+ sprite rendering.
func NewArena(seed uint64) *Arena {
	atlas := NewProceduralAtlas()
	a := &Arena{
		Atlas:    atlas,
		Viewport: gameViewport,
		worldBounds: core.Bounds{
			Origin: core.Point{X: 0, Y: 0},
			Size:   core.Size{Width: ArenaWidth, Height: ArenaHeight},
		},
		enemies:     make([]Enemy, 0, maxEnemies),
		bullets:     make([]Bullet, 0, maxBullets),
		orbs:        make([]Orb, 0, maxOrbs),
		spritePool:  make([]sprite.Sprite, 0, maxBatchSprites),
		frameShip:   atlas.Frames["ship"],
		frameDrone:  atlas.Frames["drone"],
		frameSwarm:  atlas.Frames["swarm"],
		frameBullet: atlas.Frames["bullet"],
		frameOrb:    atlas.Frames["orb"],
		frameSpark:  atlas.Frames["spark"],
		frameRing:   atlas.Frames["ring"],
	}
	a.batch = sprite.Batch{
		Atlas:   atlas,
		Sprites: make([]*sprite.Sprite, 0, maxBatchSprites),
	}
	a.Camera = tilemap.Camera{
		X:         ArenaWidth * 0.5,
		Y:         ArenaHeight * 0.5,
		Zoom:      1,
		Width:     VirtualWidth,
		Height:    VirtualHeight,
		Bounds:    &a.worldBounds,
		ShakeSeed: seed ^ 0x51A4E,
	}
	a.Reset(seed)
	return a
}

// Reset reinitializes the entire arena deterministically from seed.
func (a *Arena) Reset(seed uint64) {
	if seed == 0 {
		seed = DefaultSeed
	}
	a.seed = seed
	a.streams = random.NewStreams(seed)
	a.waveRNG = a.streams.Stream("waves")
	a.spawnRNG = a.streams.Stream("spawns")
	a.fxRNG = a.streams.Stream("fx")

	a.Player = Player{
		X:        ArenaWidth * 0.5,
		Y:        ArenaHeight * 0.5,
		HP:       playerMaxHP,
		MaxHP:    playerMaxHP,
		AimAngle: 0,
	}

	a.enemies = a.enemies[:0]
	a.bullets = a.bullets[:0]
	a.orbs = a.orbs[:0]

	a.Wave = 1
	a.Score = 0
	a.Combo = 1
	a.comboTimer = 0
	a.elapsed = 0
	a.Steps = 0
	a.Paused = false
	a.GameOver = false
	a.StressMode = false
	a.flashAlpha = 0
	a.novaRingTimer = 0
	a.keyUp, a.keyDown, a.keyLeft, a.keyRight = 0, 0, 0, 0
	a.keyNova = false

	a.Camera.X = a.Player.X
	a.Camera.Y = a.Player.Y
	a.Camera.ShakeSeed = seed ^ 0x51A4E
	a.Camera.Shake(0, 0)
	a.Camera.Clamp()

	a.initStarfield()
	a.initEmitters()
	a.beginWave(1)
	a.rebuildBatch()
}

// Restart restarts the current session deterministically. If StressMode was enabled,
// it stays enabled and re-seeds the 1000+ sprite showcase cleanly.
func (a *Arena) Restart() {
	stress := a.StressMode
	a.Reset(a.seed)
	if stress {
		a.SetStressMode(true)
	}
}

// SetStressMode toggles the 1000-sprite stress benchmark mode.
func (a *Arena) SetStressMode(on bool) {
	if a.StressMode == on {
		if on {
			a.ensureStressPopulation()
			a.rebuildBatch()
		}
		return
	}
	if !on {
		a.Reset(a.seed)
		return
	}
	a.StressMode = true
	a.Paused = false
	a.GameOver = false
	a.Player.HP = a.Player.MaxHP
	a.Player.ShieldedStress = true
	a.Player.InvulnTimer = 0
	a.ensureStressPopulation()
	a.explosions.X = a.Player.X
	a.explosions.Y = a.Player.Y
	a.explosions.Burst(48)
	a.Camera.Shake(5.0, 220*time.Millisecond)
	a.triggerFlash(core.RGB(0x38BDF8), 0.22)
	a.rebuildBatch()
}

// ToggleStressMode flips StressMode and returns its new value.
func (a *Arena) ToggleStressMode() bool {
	a.SetStressMode(!a.StressMode)
	return a.StressMode
}

// SetPaused pauses or resumes simulation updates.
func (a *Arena) SetPaused(paused bool) {
	if a.GameOver {
		return
	}
	a.Paused = paused
}

// TogglePaused flips the pause flag and returns the new state.
func (a *Arena) TogglePaused() bool {
	if a.GameOver {
		return false
	}
	a.Paused = !a.Paused
	return a.Paused
}

// PressMoveKey registers a keyboard movement key press/repeat with smooth hold decay.
func (a *Arena) PressMoveKey(dir string) {
	switch dir {
	case "up", "w":
		a.keyUp = keyHoldDuration
	case "down", "s":
		a.keyDown = keyHoldDuration
	case "left", "a":
		a.keyLeft = keyHoldDuration
	case "right", "d":
		a.keyRight = keyHoldDuration
	}
}

// TriggerNova queues a Nova radial shockwave for the next fixed step.
func (a *Arena) TriggerNova() {
	a.keyNova = true
}

// ActiveSprites returns the exact count of sprites currently batched for drawing.
func (a *Arena) ActiveSprites() int {
	return len(a.batch.Sprites)
}

// ActiveEnemies returns the number of live enemies in the arena.
func (a *Arena) ActiveEnemies() int {
	return len(a.enemies)
}

// ActiveBullets returns the number of live bullets in the arena.
func (a *Arena) ActiveBullets() int {
	return len(a.bullets)
}

// ActiveOrbs returns the number of live XP orbs in the arena.
func (a *Arena) ActiveOrbs() int {
	return len(a.orbs)
}

// ActiveParticles returns the total number of live particles across all emitters.
func (a *Arena) ActiveParticles() int {
	return a.explosions.Count() + a.sparks.Count() + a.thruster.Count()
}

// HUD returns a snapshot of the arena telemetry for the native NSGo HUD.
func (a *Arena) HUD() HUDState {
	novaReady := float32(1)
	if a.Player.NovaCooldown > 0 {
		novaReady = 1 - a.Player.NovaCooldown/novaCooldownSec
		if novaReady < 0 {
			novaReady = 0
		}
	}
	return HUDState{
		Wave:          a.Wave,
		Score:         a.Score,
		Combo:         a.Combo,
		HP:            a.Player.HP,
		MaxHP:         a.Player.MaxHP,
		ActiveSprites: len(a.batch.Sprites),
		ActiveEnemies: len(a.enemies),
		StressMode:    a.StressMode,
		Paused:        a.Paused,
		GameOver:      a.GameOver,
		NovaReady:     novaReady,
	}
}

func (a *Arena) initStarfield() {
	palette := [4]core.Color{
		core.RGBA(0x94A3B8, 0.35),
		core.RGBA(0x38BDF8, 0.45),
		core.RGBA(0xE2E8F0, 0.55),
		core.RGBA(0xA855F7, 0.38),
	}
	for i := range a.stars {
		a.stars[i] = starDot{
			x:        a.fxRNG.Range32(0, ArenaWidth),
			y:        a.fxRNG.Range32(0, ArenaHeight),
			size:     a.fxRNG.Range32(1.0, 2.2),
			parallax: a.fxRNG.Range32(0.18, 0.45),
			color:    palette[a.fxRNG.Intn(len(palette))],
		}
	}
}

func (a *Arena) initEmitters() {
	a.explosions = particles.Emitter{
		Atlas:            a.Atlas,
		Frames:           []sprite.Frame{a.frameSpark, a.frameRing},
		Shape:            particles.Circle(6),
		Rate:             0,
		MaxParticles:     512,
		LifetimeMin:      220 * time.Millisecond,
		LifetimeMax:      540 * time.Millisecond,
		SpeedMin:         38,
		SpeedMax:         168,
		AngleMin:         0,
		AngleMax:         2 * math.Pi,
		RotationSpeedMin: -5,
		RotationSpeedMax: 5,
		SizeCurve: particles.FloatCurve{
			{T: 0, Value: 1.35},
			{T: 0.4, Value: 0.95},
			{T: 1, Value: 0.15},
		},
		AlphaCurve: particles.FloatCurve{
			{T: 0, Value: 1},
			{T: 0.65, Value: 0.75},
			{T: 1, Value: 0},
		},
		Seed: a.seed ^ 0xE1105104,
		Z:    6,
	}

	a.sparks = particles.Emitter{
		Atlas:        a.Atlas,
		Frames:       []sprite.Frame{a.frameSpark},
		Shape:        particles.Point(),
		Rate:         0,
		MaxParticles: 256,
		LifetimeMin:  120 * time.Millisecond,
		LifetimeMax:  280 * time.Millisecond,
		SpeedMin:     60,
		SpeedMax:     195,
		AngleMin:     0,
		AngleMax:     2 * math.Pi,
		SizeCurve: particles.FloatCurve{
			{T: 0, Value: 1.1},
			{T: 1, Value: 0.15},
		},
		AlphaCurve: particles.FloatCurve{
			{T: 0, Value: 1},
			{T: 1, Value: 0},
		},
		Seed: a.seed ^ 0x59A4B5,
		Z:    6,
	}

	a.thruster = particles.Emitter{
		Atlas:        a.Atlas,
		Frames:       []sprite.Frame{a.frameSpark},
		Shape:        particles.Circle(2.2),
		Rate:         0,
		MaxParticles: 160,
		LifetimeMin:  140 * time.Millisecond,
		LifetimeMax:  300 * time.Millisecond,
		SpeedMin:     26,
		SpeedMax:     72,
		AngleMin:     0,
		AngleMax:     2 * math.Pi,
		SizeCurve: particles.FloatCurve{
			{T: 0, Value: 1.0},
			{T: 1, Value: 0.1},
		},
		AlphaCurve: particles.FloatCurve{
			{T: 0, Value: 0.9},
			{T: 1, Value: 0},
		},
		Seed: a.seed ^ 0x748057,
		Z:    1,
	}
}

func (a *Arena) beginWave(wave int) {
	a.Wave = wave
	a.waveBannerTimer = 1.6
	total := 10 + wave*5
	if total > 140 {
		total = 140
	}
	a.waveRemaining = total
	a.spawnTimer = 0.05

	// Spawn an immediate vanguard around the player so combat begins right away.
	vanguard := min(6+wave, total)
	for i := range vanguard {
		angle := float64(i) * (2 * math.Pi / float64(vanguard))
		dist := float32(118 + a.spawnRNG.Range32(0, 44))
		kind := EnemySwarm
		if i%4 == 3 && wave >= 1 {
			kind = EnemyDrone
		}
		a.spawnEnemyAt(kind, a.Player.X+float32(math.Cos(angle))*dist, a.Player.Y+float32(math.Sin(angle))*dist)
		a.waveRemaining--
	}
}

// Step advances the simulation by one fixed step dt (typically FixedDT = 1/60s).
func (a *Arena) Step(dt time.Duration) {
	if dt <= 0 || a.Paused || a.GameOver {
		return
	}
	dtSec := float32(dt.Seconds())
	a.elapsed += dtSec
	a.Steps++

	if a.flashAlpha > 0 {
		a.flashAlpha -= dtSec * 1.9
		if a.flashAlpha < 0 {
			a.flashAlpha = 0
		}
	}
	if a.novaRingTimer > 0 {
		a.novaRingTimer -= dtSec
		if a.novaRingTimer < 0 {
			a.novaRingTimer = 0
		}
	}
	if a.waveBannerTimer > 0 {
		a.waveBannerTimer -= dtSec
		if a.waveBannerTimer < 0 {
			a.waveBannerTimer = 0
		}
	}
	if a.comboTimer > 0 {
		a.comboTimer -= dtSec
		if a.comboTimer <= 0 {
			a.Combo = 1
			a.comboTimer = 0
		}
	}

	a.stepPlayer(dtSec)
	a.stepSpawning(dtSec)
	a.stepEnemies(dtSec)
	a.rebuildSpatialGrid()
	a.stepBullets(dtSec)
	a.stepPlayerEnemyCollisions()
	a.stepOrbs(dtSec)

	a.explosions.Update(dt)
	a.sparks.Update(dt)
	a.thruster.Update(dt)

	a.Camera.Follow(core.Point{X: core.Points(a.Player.X), Y: core.Points(a.Player.Y)}, 7.5, dt)
	a.Camera.Update(dt)

	a.rebuildBatch()
}

func (a *Arena) stepPlayer(dtSec float32) {
	p := &a.Player

	// Decay keyboard impulse hold timers smoothly.
	a.keyUp = max(0, a.keyUp-dtSec)
	a.keyDown = max(0, a.keyDown-dtSec)
	a.keyLeft = max(0, a.keyLeft-dtSec)
	a.keyRight = max(0, a.keyRight-dtSec)

	moveX := a.Control.MoveX
	moveY := a.Control.MoveY
	if a.keyLeft > 0 {
		moveX -= 1
	}
	if a.keyRight > 0 {
		moveX += 1
	}
	if a.keyUp > 0 {
		moveY -= 1
	}
	if a.keyDown > 0 {
		moveY += 1
	}

	magSq := moveX*moveX + moveY*moveY
	if magSq > 1 {
		inv := float32(1 / math.Sqrt(float64(magSq)))
		moveX *= inv
		moveY *= inv
	}

	p.VX += moveX * playerAccel * dtSec
	p.VY += moveY * playerAccel * dtSec

	// Exponential drag for snappy twin-stick responsiveness.
	drag := float32(math.Exp(-float64(playerDrag * dtSec)))
	p.VX *= drag
	p.VY *= drag

	speedSq := p.VX*p.VX + p.VY*p.VY
	if speedSq > playerMaxSpeed*playerMaxSpeed {
		scale := playerMaxSpeed / float32(math.Sqrt(float64(speedSq)))
		p.VX *= scale
		p.VY *= scale
	}

	p.X += p.VX * dtSec
	p.Y += p.VY * dtSec

	// Clamp to arena interior with gentle bounce.
	const margin = 16.0
	if p.X < margin {
		p.X = margin
		if p.VX < 0 {
			p.VX = -p.VX * 0.35
		}
	} else if p.X > ArenaWidth-margin {
		p.X = ArenaWidth - margin
		if p.VX > 0 {
			p.VX = -p.VX * 0.35
		}
	}
	if p.Y < margin {
		p.Y = margin
		if p.VY < 0 {
			p.VY = -p.VY * 0.35
		}
	} else if p.Y > ArenaHeight-margin {
		p.Y = ArenaHeight - margin
		if p.VY > 0 {
			p.VY = -p.VY * 0.35
		}
	}

	if p.InvulnTimer > 0 {
		p.InvulnTimer = max(0, p.InvulnTimer-dtSec)
	}
	if p.FireCooldown > 0 {
		p.FireCooldown = max(0, p.FireCooldown-dtSec)
	}
	if p.NovaCooldown > 0 {
		p.NovaCooldown = max(0, p.NovaCooldown-dtSec)
	}
	if p.RecoilOffset > 0 {
		p.RecoilOffset = max(0, p.RecoilOffset-dtSec*18)
	}

	// Resolve aim: twin-stick vector -> mouse world cursor -> nearest enemy auto-aim.
	autoTarget := false
	stickSq := a.Control.AimStickX*a.Control.AimStickX + a.Control.AimStickY*a.Control.AimStickY
	if stickSq > 0.04 {
		p.AimAngle = float32(math.Atan2(float64(a.Control.AimStickY), float64(a.Control.AimStickX)))
	} else if a.Control.HasWorldAim {
		dx := a.Control.AimWorldX - p.X
		dy := a.Control.AimWorldY - p.Y
		if dx*dx+dy*dy > 4 {
			p.AimAngle = float32(math.Atan2(float64(dy), float64(dx)))
		}
	} else if idx := a.nearestEnemyIndex(); idx >= 0 {
		e := &a.enemies[idx]
		p.AimAngle = float32(math.Atan2(float64(e.Y-p.Y), float64(e.X-p.X)))
		autoTarget = true
	}

	// Emit thruster particles behind the ship when moving or during stress mode.
	speed := float32(math.Sqrt(float64(p.VX*p.VX + p.VY*p.VY)))
	emitRate := float32(18)
	if speed > 22 {
		emitRate = 55 + speed*0.35
	} else if a.StressMode {
		emitRate = 75
	}
	p.ThrusterAccum += emitRate * dtSec
	if p.ThrusterAccum >= 1 {
		count := int(p.ThrusterAccum)
		p.ThrusterAccum -= float32(count)
		backAngle := p.AimAngle + math.Pi
		if speed > 25 {
			backAngle = float32(math.Atan2(float64(-p.VY), float64(-p.VX)))
		}
		sin, cos := math.Sincos(float64(backAngle))
		a.thruster.X = p.X + float32(cos)*7
		a.thruster.Y = p.Y + float32(sin)*7
		a.thruster.AngleMin = backAngle - 0.35
		a.thruster.AngleMax = backAngle + 0.35
		a.thruster.Burst(count)
	}

	// Fire plasma bolts automatically when enemies are present or while fire button is held.
	shouldFire := a.Control.Fire || a.StressMode || autoTarget || len(a.enemies) > 0
	if shouldFire && p.FireCooldown <= 0 {
		a.firePlayerVolley()
	}

	// Nova radial shockwave.
	wantNova := a.Control.Nova || a.keyNova
	a.keyNova = false
	if wantNova && p.NovaCooldown <= 0 {
		a.executeNova()
	}
}

func (a *Arena) firePlayerVolley() {
	p := &a.Player
	if a.StressMode {
		// 6-way rotating starburst volley to keep the bullet storm brisk.
		p.FireCooldown = 0.045
		base := p.AimAngle + a.elapsed*2.4
		for k := range 6 {
			ang := base + float32(k)*(math.Pi/3)
			a.spawnBullet(p.X, p.Y, ang, 285, 1)
		}
		return
	}

	p.FireCooldown = playerFireRate
	p.RecoilOffset = 2.2
	p.ShotParity = !p.ShotParity

	// Twin offset cannons with a subtle spread at high combo.
	sin, cos := math.Sincos(float64(p.AimAngle))
	perpX, perpY := float32(-sin), float32(cos)
	side := float32(3.2)
	if p.ShotParity {
		side = -side
	}
	muzzleX := p.X + float32(cos)*9 + perpX*side
	muzzleY := p.Y + float32(sin)*9 + perpY*side
	a.spawnBullet(muzzleX, muzzleY, p.AimAngle, 340, 1)

	if a.Wave >= 2 || a.Combo >= 4 {
		a.spawnBullet(p.X+float32(cos)*8-perpX*side, p.Y+float32(sin)*8-perpY*side, p.AimAngle+side*0.018, 330, 1)
	}
}

func (a *Arena) executeNova() {
	p := &a.Player
	p.NovaCooldown = novaCooldownSec
	a.novaRingTimer = 0.42
	a.novaRingX = p.X
	a.novaRingY = p.Y

	blast := physics.Circle{X: p.X, Y: p.Y, R: novaBlastRadius}
	write := 0
	for i := range a.enemies {
		e := a.enemies[i]
		ec := physics.Circle{X: e.X, Y: e.Y, R: e.Radius}
		if hit := physics.TestCircleCircle(ec, blast); hit.Hit {
			e.HP -= 3
			e.HitFlash = 0.14
			if e.HP <= 0 {
				a.onEnemyDestroyed(e)
				continue
			}
			// Push surviving armored drones outward along the contact normal.
			e.VX += hit.NormalX * 190
			e.VY += hit.NormalY * 190
		}
		a.enemies[write] = e
		write++
	}
	a.enemies = a.enemies[:write]

	a.explosions.X = p.X
	a.explosions.Y = p.Y
	a.explosions.Burst(36)
	a.Camera.Shake(7.0, 280*time.Millisecond)
	a.triggerFlash(core.RGB(0x38BDF8), 0.28)
}

func (a *Arena) stepSpawning(dtSec float32) {
	if a.StressMode {
		a.ensureStressPopulation()
		return
	}

	if a.waveRemaining > 0 {
		a.spawnTimer -= dtSec
		if a.spawnTimer <= 0 && len(a.enemies) < maxEnemies {
			batchSize := 1 + a.Wave/2
			if batchSize > 4 {
				batchSize = 4
			}
			for range batchSize {
				if a.waveRemaining <= 0 || len(a.enemies) >= maxEnemies {
					break
				}
				kind := EnemySwarm
				droneChance := min(0.45, 0.16+float64(a.Wave)*0.04)
				if a.waveRNG.Chance(droneChance) {
					kind = EnemyDrone
				}
				a.spawnEnemyEdge(kind)
				a.waveRemaining--
			}
			interval := max(float32(0.24), 0.82-float32(a.Wave)*0.055)
			a.spawnTimer = interval
		}
		return
	}

	if len(a.enemies) == 0 {
		a.Score += a.Wave * 100
		a.explosions.X = a.Player.X
		a.explosions.Y = a.Player.Y
		a.explosions.Burst(24)
		a.Camera.Shake(4.2, 200*time.Millisecond)
		a.triggerFlash(core.RGB(0x34D399), 0.18)
		a.beginWave(a.Wave + 1)
	}
}

func (a *Arena) ensureStressPopulation() {
	for len(a.enemies) < stressTargetEnemies && len(a.enemies) < maxEnemies {
		kind := EnemySwarm
		if len(a.enemies)%5 == 0 {
			kind = EnemyDrone
		}
		// Distribute in vibrant swirling rings across the arena outside the immediate player center.
		ang := a.spawnRNG.Range(0, 2*math.Pi)
		dist := a.spawnRNG.Range32(48, 430)
		x := clampf(a.Player.X+float32(math.Cos(ang))*dist, 24, ArenaWidth-24)
		y := clampf(a.Player.Y+float32(math.Sin(ang))*dist, 24, ArenaHeight-24)
		a.spawnEnemyAt(kind, x, y)
	}
	for len(a.bullets) < stressTargetBullets && len(a.bullets) < maxBullets {
		ang := a.spawnRNG.Range32(0, 2*math.Pi)
		dist := a.spawnRNG.Range32(12, 340)
		x := clampf(a.Player.X+float32(math.Cos(float64(ang)))*dist, 16, ArenaWidth-16)
		y := clampf(a.Player.Y+float32(math.Sin(float64(ang)))*dist, 16, ArenaHeight-16)
		shootAng := a.spawnRNG.Range32(0, 2*math.Pi)
		a.spawnBullet(x, y, shootAng, 240, 1)
	}
	for len(a.orbs) < stressTargetOrbs && len(a.orbs) < maxOrbs {
		x := a.spawnRNG.Range32(32, ArenaWidth-32)
		y := a.spawnRNG.Range32(32, ArenaHeight-32)
		a.spawnOrb(x, y, 10)
	}
}

func (a *Arena) spawnEnemyEdge(kind EnemyKind) {
	// Spawn just outside the visible camera view, clamped inside arena bounds.
	vis := a.Camera.VisibleBounds()
	minX := max(float32(20), float32(vis.Origin.X)-36)
	maxX := min(float32(ArenaWidth-20), float32(vis.Origin.X+vis.Size.Width)+36)
	minY := max(float32(20), float32(vis.Origin.Y)-36)
	maxY := min(float32(ArenaHeight-20), float32(vis.Origin.Y+vis.Size.Height)+36)

	var x, y float32
	switch a.spawnRNG.Intn(4) {
	case 0:
		x, y = a.spawnRNG.Range32(minX, maxX), minY
	case 1:
		x, y = a.spawnRNG.Range32(minX, maxX), maxY
	case 2:
		x, y = minX, a.spawnRNG.Range32(minY, maxY)
	default:
		x, y = maxX, a.spawnRNG.Range32(minY, maxY)
	}
	a.spawnEnemyAt(kind, x, y)
}

func (a *Arena) spawnEnemyAt(kind EnemyKind, x, y float32) {
	if len(a.enemies) >= maxEnemies {
		return
	}
	x = clampf(x, 16, ArenaWidth-16)
	y = clampf(y, 16, ArenaHeight-16)
	orbit := float32(1)
	if a.spawnRNG.Bool() {
		orbit = -1
	}
	switch kind {
	case EnemyDrone:
		a.enemies = append(a.enemies, Enemy{
			Kind:      EnemyDrone,
			X:         x,
			Y:         y,
			Radius:    8.5,
			Speed:     54 + float32(a.Wave)*2.2 + a.spawnRNG.Range32(0, 12),
			Spin:      a.spawnRNG.Range32(-2.2, 2.2),
			OrbitSign: orbit,
			HP:        3,
			MaxHP:     3,
			Score:     50,
		})
	default:
		a.enemies = append(a.enemies, Enemy{
			Kind:      EnemySwarm,
			X:         x,
			Y:         y,
			Radius:    6.0,
			Speed:     86 + float32(a.Wave)*3.0 + a.spawnRNG.Range32(0, 18),
			OrbitSign: orbit,
			HP:        1,
			MaxHP:     1,
			Score:     15,
		})
	}
}

func (a *Arena) spawnBullet(x, y, angle, speed float32, damage int) {
	if len(a.bullets) >= maxBullets {
		return
	}
	sin, cos := math.Sincos(float64(angle))
	a.bullets = append(a.bullets, Bullet{
		X:      x,
		Y:      y,
		VX:     float32(cos) * speed,
		VY:     float32(sin) * speed,
		Angle:  angle,
		Radius: bulletRadius,
		Damage: damage,
		Life:   1.55,
	})
}

func (a *Arena) spawnOrb(x, y float32, value int) {
	if len(a.orbs) >= maxOrbs {
		return
	}
	ang := a.fxRNG.Range(0, 2*math.Pi)
	spd := a.fxRNG.Range32(14, 42)
	a.orbs = append(a.orbs, Orb{
		X:     x,
		Y:     y,
		VX:    float32(math.Cos(ang)) * spd,
		VY:    float32(math.Sin(ang)) * spd,
		Value: value,
		Phase: a.fxRNG.Range32(0, 2*math.Pi),
		Life:  12.0,
	})
}

func (a *Arena) stepEnemies(dtSec float32) {
	px, py := a.Player.X, a.Player.Y
	for i := range a.enemies {
		e := &a.enemies[i]
		dx := px - e.X
		dy := py - e.Y
		distSq := dx*dx + dy*dy
		var dirX, dirY float32
		if distSq > 1e-4 {
			inv := float32(1 / math.Sqrt(float64(distSq)))
			dirX = dx * inv
			dirY = dy * inv
		}

		// Swarmers weave tangentially; in StressMode they swirl gracefully around the player.
		tanWeight := float32(0.18)
		if a.StressMode {
			tanWeight = 0.62
		} else if e.Kind == EnemyDrone {
			tanWeight = 0.08
		}
		steerX := dirX - dirY*e.OrbitSign*tanWeight
		steerY := dirY + dirX*e.OrbitSign*tanWeight
		sm := float32(math.Sqrt(float64(steerX*steerX + steerY*steerY)))
		if sm > 1e-4 {
			steerX /= sm
			steerY /= sm
		}

		targetVX := steerX * e.Speed
		targetVY := steerY * e.Speed
		blend := min(float32(1), dtSec*5.0)
		e.VX += (targetVX - e.VX) * blend
		e.VY += (targetVY - e.VY) * blend
		e.X += e.VX * dtSec
		e.Y += e.VY * dtSec

		// Keep enemies inside arena bounds.
		if e.X < 10 {
			e.X = 10
			e.VX = absf(e.VX)
		} else if e.X > ArenaWidth-10 {
			e.X = ArenaWidth - 10
			e.VX = -absf(e.VX)
		}
		if e.Y < 10 {
			e.Y = 10
			e.VY = absf(e.VY)
		} else if e.Y > ArenaHeight-10 {
			e.Y = ArenaHeight - 10
			e.VY = -absf(e.VY)
		}

		if e.Kind == EnemyDrone {
			e.Angle += e.Spin * dtSec
		} else if e.VX*e.VX+e.VY*e.VY > 1 {
			e.Angle = float32(math.Atan2(float64(e.VY), float64(e.VX)))
		}
		if e.HitFlash > 0 {
			e.HitFlash = max(0, e.HitFlash-dtSec)
		}
	}
}

func (a *Arena) rebuildSpatialGrid() {
	for i := range a.cellHead {
		a.cellHead[i] = -1
	}
	for i := range a.enemies {
		c := cellIndex(a.enemies[i].X, a.enemies[i].Y)
		a.enemyNext[i] = a.cellHead[c]
		a.cellHead[c] = i
	}
}

func cellCoord(x, y float32) (col, row int) {
	col = int(x / gridCellW)
	if col < 0 {
		col = 0
	} else if col >= gridCols {
		col = gridCols - 1
	}
	row = int(y / gridCellH)
	if row < 0 {
		row = 0
	} else if row >= gridRows {
		row = gridRows - 1
	}
	return col, row
}

func cellIndex(x, y float32) int {
	col, row := cellCoord(x, y)
	return row*gridCols + col
}

func (a *Arena) stepBullets(dtSec float32) {
	bWrite := 0
	anyKilled := false

	for i := range a.bullets {
		b := a.bullets[i]
		b.X += b.VX * dtSec
		b.Y += b.VY * dtSec
		b.Life -= dtSec

		if b.Life <= 0 || b.X < -12 || b.X > ArenaWidth+12 || b.Y < -12 || b.Y > ArenaHeight+12 {
			continue
		}

		bc := physics.Circle{X: b.X, Y: b.Y, R: b.Radius}
		col, row := cellCoord(b.X, b.Y)
		hitEnemy := false

		// Probe the bullet's 3x3 neighborhood in the spatial grid using physics.TestCircleCircle.
		for ry := max(0, row-1); ry <= min(gridRows-1, row+1) && !hitEnemy; ry++ {
			rowOff := ry * gridCols
			for cx := max(0, col-1); cx <= min(gridCols-1, col+1) && !hitEnemy; cx++ {
				for ei := a.cellHead[rowOff+cx]; ei >= 0; ei = a.enemyNext[ei] {
					e := &a.enemies[ei]
					if e.HP <= 0 {
						continue
					}
					ec := physics.Circle{X: e.X, Y: e.Y, R: e.Radius}
					if contact := physics.TestCircleCircle(bc, ec); contact.Hit {
						e.HP -= b.Damage
						e.HitFlash = 0.11
						e.VX += b.VX * 0.14
						e.VY += b.VY * 0.14
						a.sparks.X = contact.PointX
						a.sparks.Y = contact.PointY
						if a.StressMode {
							a.sparks.Burst(2)
						} else {
							a.sparks.Burst(6)
						}
						if e.HP <= 0 {
							a.onEnemyDestroyed(*e)
							anyKilled = true
						}
						hitEnemy = true
						break
					}
				}
			}
		}

		if !hitEnemy {
			a.bullets[bWrite] = b
			bWrite++
		}
	}
	a.bullets = a.bullets[:bWrite]

	if anyKilled {
		eWrite := 0
		for i := range a.enemies {
			if a.enemies[i].HP > 0 {
				a.enemies[eWrite] = a.enemies[i]
				eWrite++
			}
		}
		a.enemies = a.enemies[:eWrite]
	}
}

func (a *Arena) onEnemyDestroyed(e Enemy) {
	a.Score += e.Score * a.Combo
	if a.Combo < 8 {
		a.Combo++
	}
	a.comboTimer = 2.6

	a.explosions.X = e.X
	a.explosions.Y = e.Y
	if a.StressMode {
		a.explosions.Burst(4)
	} else if e.Kind == EnemyDrone {
		a.explosions.Burst(18)
		a.Camera.Shake(5.0, 210*time.Millisecond)
		a.triggerFlash(core.RGB(0xF59E0B), 0.10)
		a.spawnOrb(e.X-4, e.Y, 20)
		a.spawnOrb(e.X+4, e.Y, 20)
	} else {
		a.explosions.Burst(10)
		a.Camera.Shake(2.8, 120*time.Millisecond)
		a.spawnOrb(e.X, e.Y, 10)
	}
}

func (a *Arena) stepPlayerEnemyCollisions() {
	if a.StressMode || a.Player.ShieldedStress || a.Player.InvulnTimer > 0 {
		return
	}
	pc := physics.Circle{X: a.Player.X, Y: a.Player.Y, R: playerRadius}
	for i := range a.enemies {
		e := &a.enemies[i]
		if e.HP <= 0 {
			continue
		}
		ec := physics.Circle{X: e.X, Y: e.Y, R: e.Radius}
		if hit := physics.TestCircleCircle(pc, ec); hit.Hit {
			a.Player.HP--
			a.Player.InvulnTimer = playerInvulnSec
			a.Combo = 1
			a.comboTimer = 0
			a.Player.VX += hit.NormalX * 150
			a.Player.VY += hit.NormalY * 150

			a.explosions.X = hit.PointX
			a.explosions.Y = hit.PointY
			a.explosions.Burst(22)
			a.Camera.Shake(8.0, 300*time.Millisecond)
			a.triggerFlash(core.RGB(0xEF4444), 0.34)

			if a.Player.HP <= 0 {
				a.Player.HP = 0
				a.GameOver = true
				a.explosions.X = a.Player.X
				a.explosions.Y = a.Player.Y
				a.explosions.Burst(42)
			}
			return
		}
	}
}

func (a *Arena) stepOrbs(dtSec float32) {
	px, py := a.Player.X, a.Player.Y
	pc := physics.Circle{X: px, Y: py, R: playerRadius + 3}
	drag := float32(math.Exp(-float64(3.0 * dtSec)))
	write := 0

	for i := range a.orbs {
		o := a.orbs[i]
		o.Life -= dtSec
		o.Phase += dtSec * 5.2
		if o.Life <= 0 {
			continue
		}

		dx := px - o.X
		dy := py - o.Y
		distSq := dx*dx + dy*dy
		if distSq < orbMagnetRadius*orbMagnetRadius && distSq > 1e-4 {
			dist := float32(math.Sqrt(float64(distSq)))
			pull := (1 - dist/orbMagnetRadius) * 420
			o.VX += (dx / dist) * pull * dtSec
			o.VY += (dy / dist) * pull * dtSec
		} else if a.StressMode {
			// Gentle swirl in stress mode so every orb stays visibly in motion.
			o.VX += float32(math.Cos(float64(o.Phase))) * 18 * dtSec
			o.VY += float32(math.Sin(float64(o.Phase))) * 18 * dtSec
		}

		o.VX *= drag
		o.VY *= drag
		o.X = clampf(o.X+o.VX*dtSec, 10, ArenaWidth-10)
		o.Y = clampf(o.Y+o.VY*dtSec, 10, ArenaHeight-10)

		oc := physics.Circle{X: o.X, Y: o.Y, R: orbRadius}
		if physics.TestCircleCircle(pc, oc).Hit {
			a.Score += o.Value * a.Combo
			a.sparks.X = o.X
			a.sparks.Y = o.Y
			a.sparks.Burst(3)
			continue
		}

		a.orbs[write] = o
		write++
	}
	a.orbs = a.orbs[:write]
}

func (a *Arena) nearestEnemyIndex() int {
	if len(a.enemies) == 0 {
		return -1
	}
	px, py := a.Player.X, a.Player.Y
	bestIdx := -1
	bestDistSq := float32(math.MaxFloat32)
	for i := range a.enemies {
		dx := a.enemies[i].X - px
		dy := a.enemies[i].Y - py
		d2 := dx*dx + dy*dy
		if d2 < bestDistSq {
			bestDistSq = d2
			bestIdx = i
		}
	}
	return bestIdx
}

func (a *Arena) triggerFlash(c core.Color, alpha float32) {
	if alpha >= a.flashAlpha {
		a.flashColor = c
		a.flashAlpha = alpha
	}
}

// rebuildBatch packs all active game entities and emitter particles into a single
// reusable sprite.Batch in ascending Z order with zero allocations.
func (a *Arena) rebuildBatch() {
	a.spritePool = a.spritePool[:0]
	a.batch.Sprites = a.batch.Sprites[:0]

	push := func(s sprite.Sprite) {
		if len(a.spritePool) >= cap(a.spritePool) {
			return
		}
		idx := len(a.spritePool)
		a.spritePool = append(a.spritePool, s)
		a.batch.Sprites = append(a.batch.Sprites, &a.spritePool[idx])
	}

	// Z=1: Thruster exhaust trail particles.
	a.pushEmitterParticles(&a.thruster, push)

	// Z=2: Glowing XP orbs.
	for i := range a.orbs {
		o := &a.orbs[i]
		pulse := float32(0.85 + 0.22*math.Sin(float64(o.Phase)))
		s := sprite.NewSprite(a.frameOrb)
		s.X, s.Y = o.X, o.Y
		s.ScaleX, s.ScaleY = pulse, pulse
		s.Rotation = o.Phase * 0.5
		s.Z = 2
		push(s)
	}

	// Z=3: Hostile swarmers and hunter drones.
	for i := range a.enemies {
		e := &a.enemies[i]
		frame := a.frameSwarm
		if e.Kind == EnemyDrone {
			frame = a.frameDrone
		}
		scale := float32(1)
		if e.HitFlash > 0 {
			scale = 1.24
		}
		s := sprite.NewSprite(frame)
		s.X, s.Y = e.X, e.Y
		s.Rotation = e.Angle
		s.ScaleX, s.ScaleY = scale, scale
		s.Z = 3
		push(s)
	}

	// Z=4: Plasma bullets.
	for i := range a.bullets {
		b := &a.bullets[i]
		s := sprite.NewSprite(a.frameBullet)
		s.X, s.Y = b.X, b.Y
		s.Rotation = b.Angle
		s.Z = 4
		push(s)
	}

	// Z=5: Player starfighter (blinks cleanly while invulnerable).
	if !a.GameOver {
		alpha := float32(1)
		if a.Player.InvulnTimer > 0 && int(a.Player.InvulnTimer*18)%2 == 0 {
			alpha = 0.45
		}
		sin, cos := math.Sincos(float64(a.Player.AimAngle))
		s := sprite.NewSprite(a.frameShip)
		s.X = a.Player.X - float32(cos)*a.Player.RecoilOffset
		s.Y = a.Player.Y - float32(sin)*a.Player.RecoilOffset
		s.Rotation = a.Player.AimAngle
		s.Alpha = alpha
		s.Z = 5
		push(s)
	}

	// Z=6: Hit sparks & explosion bursts from deterministic particles.Emitter instances.
	a.pushEmitterParticles(&a.sparks, push)
	a.pushEmitterParticles(&a.explosions, push)

	// Z=7: Expanding Nova shockwave ring sprite.
	if a.novaRingTimer > 0 {
		t := 1 - a.novaRingTimer/0.42
		scale := 1.0 + t*(novaBlastRadius*2/16.0)
		s := sprite.NewSprite(a.frameRing)
		s.X, s.Y = a.novaRingX, a.novaRingY
		s.ScaleX, s.ScaleY = scale, scale
		s.Alpha = clamp01(1 - t)
		s.Z = 7
		push(s)
	}
}

func (a *Arena) pushEmitterParticles(e *particles.Emitter, push func(sprite.Sprite)) {
	ps := e.Particles()
	for i := range ps {
		p := &ps[i]
		if p.Alpha <= 0 || p.Size <= 0 {
			continue
		}
		ox, oy := p.Frame.PivotX, p.Frame.PivotY
		if ox == 0 && oy == 0 {
			ox, oy = p.Frame.W*0.5, p.Frame.H*0.5
		}
		push(sprite.Sprite{
			Frame:    p.Frame,
			X:        p.X,
			Y:        p.Y,
			OriginX:  ox,
			OriginY:  oy,
			Rotation: p.Rotation,
			ScaleX:   p.Size,
			ScaleY:   p.Size,
			Alpha:    p.Alpha,
			Z:        e.Z,
		})
	}
}

// Draw renders the entire Starburst scene inside the letterboxed 480x270 virtual viewport.
func (a *Arena) Draw(d *core.Drawing, windowSize core.Size) {
	a.Viewport.Draw(d, windowSize, func(d *core.Drawing, size core.Size) {
		a.DrawWorld(d)
	})
}

// DrawWorld renders the 480x270 virtual frame (backdrop, parallax stars, camera-transformed
// arena grid, single sprite.Batch, reticle, and screen-space flash).
func (a *Arena) DrawWorld(d *core.Drawing) {
	// Deep space backdrop.
	d.SetFillColor(core.RGB(0x070B16)).FillRect(0, 0, VirtualWidth, VirtualHeight)

	// Subtle parallax starfield.
	camX := a.Camera.X
	camY := a.Camera.Y
	for i := range a.stars {
		st := &a.stars[i]
		sx := wrapf(st.x-camX*st.parallax, VirtualWidth)
		sy := wrapf(st.y-camY*st.parallax, VirtualHeight)
		d.SetFillColor(st.color).FillRect(sx, sy, st.size, st.size)
	}

	// World-space layer transformed by tilemap.Camera (including explosion/damage screen shake).
	d.Save()
	a.Camera.Apply(d)

	a.drawArenaFloor(d)

	// Draw all ships, enemies, bullets, orbs, rings, and live emitter particles through ONE batch.
	a.batch.Draw(d)

	// Draw crisp targeting reticle when aiming with mouse cursor.
	if a.Control.HasWorldAim && !a.GameOver {
		ax, ay := a.Control.AimWorldX, a.Control.AimWorldY
		d.SetStrokeColor(core.RGBA(0x38BDF8, 0.72)).SetLineWidth(1.0)
		d.BeginPath().Arc(ax, ay, 5.5, 0, 2*math.Pi, false).Stroke()
		d.BeginPath().
			MoveTo(ax-9, ay).LineTo(ax-4, ay).
			MoveTo(ax+4, ay).LineTo(ax+9, ay).
			MoveTo(ax, ay-9).LineTo(ax, ay-4).
			MoveTo(ax, ay+4).LineTo(ax, ay+9).
			Stroke()
	}

	d.Restore()

	// Subtle touch pad outlines when touch input has been used.
	if a.ShowTouchHints {
		a.drawTouchHints(d)
	}

	// Screen flash overlay (explosion / nova / damage feedback).
	if a.flashAlpha > 0.005 {
		d.Save()
		d.SetAlpha(a.flashAlpha)
		d.SetFillColor(a.flashColor)
		d.FillRect(0, 0, VirtualWidth, VirtualHeight)
		d.Restore()
	}
}

func (a *Arena) drawArenaFloor(d *core.Drawing) {
	// Subtle tactical grid inside the visible camera bounds.
	vis := a.Camera.VisibleBounds()
	const step = 48
	startX := max(0, int(vis.Origin.X)/step*step)
	endX := min(ArenaWidth, int(vis.Origin.X+vis.Size.Width)+step)
	startY := max(0, int(vis.Origin.Y)/step*step)
	endY := min(ArenaHeight, int(vis.Origin.Y+vis.Size.Height)+step)

	d.SetStrokeColor(core.RGBA(0x1E293B, 0.55)).SetLineWidth(1.0).BeginPath()
	for x := startX; x <= endX; x += step {
		d.MoveTo(float32(x), float32(startY)).LineTo(float32(x), float32(endY))
	}
	for y := startY; y <= endY; y += step {
		d.MoveTo(float32(startX), float32(y)).LineTo(float32(endX), float32(y))
	}
	d.Stroke()

	// Glowing neon arena perimeter.
	d.SetStrokeColor(core.RGBA(0x38BDF8, 0.65)).SetLineWidth(2.0).
		StrokeRect(2, 2, ArenaWidth-4, ArenaHeight-4)
}

func (a *Arena) drawTouchHints(d *core.Drawing) {
	d.SetStrokeColor(core.RGBA(0x38BDF8, 0.25)).SetLineWidth(1.0)
	for _, r := range []core.Bounds{touchMoveLeftRect, touchMoveRightRect, touchMoveUpRect, touchMoveDownRect, touchNovaRect} {
		d.StrokeRect(float32(r.Origin.X), float32(r.Origin.Y), float32(r.Size.Width), float32(r.Size.Height))
	}
}

// NewProceduralAtlas builds a self-contained 128x32 premultiplied pixel-art RGBA atlas
// containing all 7 Starburst sprites: ship, drone, swarm, bullet, orb, spark, ring.
func NewProceduralAtlas() *sprite.Atlas {
	img := image.NewRGBA(image.Rect(0, 0, 128, 32))

	drawShipFrame(img, 0, 0)
	drawDroneFrame(img, 16, 0)
	drawSwarmFrame(img, 32, 0)
	drawBulletFrame(img, 48, 0)
	drawOrbFrame(img, 56, 0)
	drawSparkFrame(img, 64, 0)
	drawRingFrame(img, 72, 0)

	frames := map[string]sprite.Frame{
		"ship":   {Name: "ship", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16, PivotX: 8, PivotY: 8},
		"drone":  {Name: "drone", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16, PivotX: 8, PivotY: 8},
		"swarm":  {Name: "swarm", X: 32, Y: 0, W: 12, H: 12, Width: 12, Height: 12, PivotX: 6, PivotY: 6},
		"bullet": {Name: "bullet", X: 48, Y: 0, W: 8, H: 8, Width: 8, Height: 8, PivotX: 4, PivotY: 4},
		"orb":    {Name: "orb", X: 56, Y: 0, W: 8, H: 8, Width: 8, Height: 8, PivotX: 4, PivotY: 4},
		"spark":  {Name: "spark", X: 64, Y: 0, W: 6, H: 6, Width: 6, Height: 6, PivotX: 3, PivotY: 3},
		"ring":   {Name: "ring", X: 72, Y: 0, W: 16, H: 16, Width: 16, Height: 16, PivotX: 8, PivotY: 8},
	}
	return &sprite.Atlas{
		Image: core.ImageSource{
			Pixels: img,
			Size:   core.Size{Width: 128, Height: 32},
		},
		Frames: frames,
	}
}

func setPremul(img *image.RGBA, x, y int, r, g, b, a uint8) {
	if !image.Pt(x, y).In(img.Bounds()) {
		return
	}
	pr := uint8((uint16(r) * uint16(a)) / 255)
	pg := uint8((uint16(g) * uint16(a)) / 255)
	pb := uint8((uint16(b) * uint16(a)) / 255)
	img.SetRGBA(x, y, color.RGBA{R: pr, G: pg, B: pb, A: a})
}

func drawPattern(img *image.RGBA, ox, oy int, rows []string, pal map[byte][4]uint8) {
	for y, row := range rows {
		for x := 0; x < len(row); x++ {
			ch := row[x]
			if c, ok := pal[ch]; ok {
				setPremul(img, ox+x, oy+y, c[0], c[1], c[2], c[3])
			}
		}
	}
}

func drawShipFrame(img *image.RGBA, ox, oy int) {
	// Sleek cyan/white twin-fin starfighter facing +X (16x16).
	rows := []string{
		"................",
		"...cc...........",
		"..cCCc..........",
		"..cCCCcc........",
		"...cCCCCcc......",
		".oo.cCWWCCcc....",
		".oO.cCWWWWCCcc..",
		".oO.cCWWWWWCCCW.",
		".oO.cCWWWWWCCCW.",
		".oO.cCWWWWCCcc..",
		".oo.cCWWCCcc....",
		"...cCCCCcc......",
		"..cCCCcc........",
		"..cCCc..........",
		"...cc...........",
		"................",
	}
	pal := map[byte][4]uint8{
		'c': {14, 165, 233, 255},
		'C': {56, 189, 248, 255},
		'W': {240, 249, 255, 255},
		'o': {251, 146, 60, 220},
		'O': {254, 240, 138, 255},
	}
	drawPattern(img, ox, oy, rows, pal)
}

func drawDroneFrame(img *image.RGBA, ox, oy int) {
	// Armored crimson/amber octagonal hunter drone (16x16).
	rows := []string{
		".....rrrrrr.....",
		"...rrRRRRRRrr...",
		"..rRRrrrrrrRRr..",
		".rRrr......rrRr.",
		".rRr..aaaa..rRr.",
		"rRr..aAAAAa..rRr",
		"rRr.aAAYYAAa.rRr",
		"rRr.aAYWWYAa.rRr",
		"rRr.aAYWWYAa.rRr",
		"rRr.aAAYYAAa.rRr",
		"rRr..aAAAAa..rRr",
		".rRr..aaaa..rRr.",
		".rRrr......rrRr.",
		"..rRRrrrrrrRRr..",
		"...rrRRRRRRrr...",
		".....rrrrrr.....",
	}
	pal := map[byte][4]uint8{
		'r': {185, 28, 28, 255},
		'R': {244, 63, 94, 255},
		'a': {217, 119, 6, 255},
		'A': {251, 191, 36, 255},
		'Y': {254, 240, 138, 255},
		'W': {255, 255, 255, 255},
	}
	drawPattern(img, ox, oy, rows, pal)
}

func drawSwarmFrame(img *image.RGBA, ox, oy int) {
	// Fast violet/magenta chevron swarmer dart facing +X (12x12).
	rows := []string{
		"............",
		"..pp........",
		"..pPPp......",
		"...pPPPp....",
		"....pPWWPp..",
		"...pPPWWPPW.",
		"...pPPWWPPW.",
		"....pPWWPp..",
		"...pPPPp....",
		"..pPPp......",
		"..pp........",
		"............",
	}
	pal := map[byte][4]uint8{
		'p': {147, 51, 234, 255},
		'P': {232, 121, 249, 255},
		'W': {253, 244, 255, 255},
	}
	drawPattern(img, ox, oy, rows, pal)
}

func drawBulletFrame(img *image.RGBA, ox, oy int) {
	// Bright cyan-white plasma bolt facing +X (8x8).
	rows := []string{
		"........",
		"..gggg..",
		".gCCCCg.",
		"gCWWWWCg",
		"gCWWWWCg",
		".gCCCCg.",
		"..gggg..",
		"........",
	}
	pal := map[byte][4]uint8{
		'g': {56, 189, 248, 130},
		'C': {125, 211, 252, 240},
		'W': {255, 255, 255, 255},
	}
	drawPattern(img, ox, oy, rows, pal)
}

func drawOrbFrame(img *image.RGBA, ox, oy int) {
	// Glowing emerald XP crystal diamond (8x8).
	rows := []string{
		"...gg...",
		"..gEEg..",
		".gEWWGg.",
		"gEWWWWGg",
		"gEWWWWGg",
		".gEWWGg.",
		"..gEEg..",
		"...gg...",
	}
	pal := map[byte][4]uint8{
		'g': {16, 185, 129, 130},
		'E': {52, 211, 153, 255},
		'G': {110, 231, 183, 255},
		'W': {236, 253, 245, 255},
	}
	drawPattern(img, ox, oy, rows, pal)
}

func drawSparkFrame(img *image.RGBA, ox, oy int) {
	// Warm gold/orange glowing spark diamond (6x6).
	rows := []string{
		"..gg..",
		".gYYg.",
		"gYWWYg",
		"gYWWYg",
		".gYYg.",
		"..gg..",
	}
	pal := map[byte][4]uint8{
		'g': {249, 115, 22, 145},
		'Y': {251, 191, 36, 255},
		'W': {254, 249, 195, 255},
	}
	drawPattern(img, ox, oy, rows, pal)
}

func drawRingFrame(img *image.RGBA, ox, oy int) {
	// Smooth glowing shockwave ring (16x16).
	const cx, cy = 7.5, 7.5
	for y := range 16 {
		for x := range 16 {
			dx := float64(x) - cx
			dy := float64(y) - cy
			dist := math.Hypot(dx, dy)
			delta := math.Abs(dist - 5.8)
			if delta < 2.2 {
				alpha := uint8(clamp01(float32(1-delta/2.2)) * 245)
				setPremul(img, ox+x, oy+y, 125, 211, 252, alpha)
			}
		}
	}
}

func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clamp01(v float32) float32 {
	return clampf(v, 0, 1)
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func wrapf(v, span float32) float32 {
	v = float32(math.Mod(float64(v), float64(span)))
	if v < 0 {
		v += span
	}
	return v
}

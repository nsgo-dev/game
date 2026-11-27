package main

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/text"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	maxHearts = 3

	playerBoxW      float32 = 10
	playerBoxH      float32 = 14
	playerSpriteOX  float32 = -3
	playerSpriteOY  float32 = -2
	playerMoveSpeed float32 = 98
	playerGroundAcc float32 = 960
	playerAirAcc    float32 = 680
	playerFriction  float32 = 1150
	playerGravity   float32 = 720
	playerMaxFall   float32 = 320
	playerJumpVel   float32 = -284

	coyoteDuration     = 100 * time.Millisecond
	jumpBufferDuration = 100 * time.Millisecond
	invulnDuration     = 1200 * time.Millisecond
	heartClearBonus    = 50
)

type levelOutcome uint8

const (
	outcomeNone levelOutcome = iota
	outcomeVictory
	outcomeGameOver
)

type stepInput struct {
	moveAxis     float32 // [-1, 1]
	jumpPressed  bool    // edge: just pressed this step
	jumpHeld     bool    // level: currently held
	jumpReleased bool    // edge: just released this step
}

type levelCallbacks struct {
	onJump func()
	onGem  func(value int)
	onHurt func(hearts int)
	onWin  func(score, gems, hearts int)
	onLose func(score, gems int)
}

type gemEntity struct {
	x, y      float32
	value     int
	phase     float64
	collected bool
	anim      sprite.Animation
	spr       sprite.Sprite
}

type slimeEntity struct {
	x, y       float32
	prevX      float32
	minX, maxX float32
	speed      float32
	dir        float32
	anim       sprite.Animation
	spr        sprite.Sprite
}

type portalEntity struct {
	x, y float32
	anim sprite.Animation
	spr  sprite.Sprite
}

type floatingPopup struct {
	text    string
	x, y    float32
	age     time.Duration
	maxLife time.Duration
}

type levelState struct {
	assets     *gameAssets
	tm         *tilemap.Tilemap
	terrain    *tilemap.TileLayer
	cam        tilemap.Camera
	shakeScale float32
	sc         *scene.Scene
	batch      sprite.Batch

	// Player state.
	playerBody      *physics.Body
	prevPlayerX     float32
	prevPlayerY     float32
	spawnX, spawnY  float32
	safeX, safeY    float32
	facingLeft      bool
	wasGrounded     bool
	coyoteTimer     time.Duration
	jumpBufferTimer time.Duration
	invulnTimer     time.Duration

	animIdle sprite.Animation
	animRun  sprite.Animation
	animJump sprite.Animation
	curAnim  string
	heroSpr  sprite.Sprite

	// Level entities.
	gems   []gemEntity
	slimes []slimeEntity
	portal portalEntity

	// Particles & floating text popups.
	dustEmitter  particles.Emitter
	sparkEmitter particles.Emitter
	popups       []floatingPopup

	// swarm is the 1000-firefly stress swarm, nil while it is off.
	swarm *fireflySwarm

	// Run metrics.
	hearts        int
	score         int
	gemsCollected int
	elapsed       time.Duration
	steps         int
	outcome       levelOutcome
	callbacks     levelCallbacks
}

func newLevelState(assets *gameAssets, cb levelCallbacks) (*levelState, error) {
	tm, err := assets.loadLevelMap()
	if err != nil {
		return nil, err
	}

	wb := tm.WorldBounds()
	l := &levelState{
		assets:     assets,
		tm:         tm,
		terrain:    tm.Layer("terrain"),
		shakeScale: 1,
		hearts:     maxHearts,
		callbacks:  cb,
		cam: tilemap.Camera{
			Width:     virtualWidth,
			Height:    virtualHeight,
			Zoom:      1,
			Bounds:    &wb,
			ShakeSeed: 42,
		},
		batch: sprite.Batch{
			Atlas: assets.atlas,
		},
		animIdle: assets.atlas.Animations["idle"],
		animRun:  assets.atlas.Animations["run"],
		animJump: assets.atlas.Animations["jump"],
		curAnim:  "idle",
		heroSpr:  sprite.NewSprite(assets.atlas.Frames["hero_idle_0"]),
		spawnX:   32,
		spawnY:   176,
	}
	l.heroSpr.Z = 20

	// Parse objects layer for spawn, gems, slimes, and goal portal.
	if objLayer := tm.ObjectLayer("objects"); objLayer != nil {
		for _, obj := range objLayer.Objects {
			switch obj.Type {
			case "spawn":
				l.spawnX = obj.X
				l.spawnY = obj.Y
			case "gem":
				val := 100
				if v, ok := obj.Properties.Int("value"); ok && v > 0 {
					val = v
				}
				anim := assets.atlas.Animations["gem"]
				// Stagger animation phase deterministically by gem index.
				anim.Update(time.Duration(len(l.gems)*75) * time.Millisecond)
				spr := sprite.NewSprite(anim.Frame())
				spr.Z = 10
				l.gems = append(l.gems, gemEntity{
					x:     obj.X,
					y:     obj.Y,
					value: val,
					phase: float64(len(l.gems)) * 0.9,
					anim:  anim,
					spr:   spr,
				})
			case "slime":
				minX := obj.X - 32
				maxX := obj.X + 32
				speed := float32(36)
				if v, ok := obj.Properties.Float("patrolMinX"); ok {
					minX = float32(v)
				}
				if v, ok := obj.Properties.Float("patrolMaxX"); ok {
					maxX = float32(v)
				}
				if v, ok := obj.Properties.Float("speed"); ok && v > 0 {
					speed = float32(v)
				}
				anim := assets.atlas.Animations["slime"]
				spr := sprite.NewSprite(anim.Frame())
				spr.Z = 15
				l.slimes = append(l.slimes, slimeEntity{
					x:     obj.X,
					y:     obj.Y,
					prevX: obj.X,
					minX:  minX,
					maxX:  maxX,
					speed: speed,
					dir:   1,
					anim:  anim,
					spr:   spr,
				})
			case "goal":
				anim := assets.atlas.Animations["portal"]
				spr := sprite.NewSprite(anim.Frame())
				spr.Z = 8
				l.portal = portalEntity{
					x:    obj.X,
					y:    obj.Y,
					anim: anim,
					spr:  spr,
				}
			}
		}
	}

	// Place player body at spawn, sitting flush on the tile floor.
	bx := l.spawnX - playerSpriteOX
	by := l.spawnY - playerSpriteOY
	l.playerBody = physics.NewBoxBody(bx, by, playerBoxW, playerBoxH)
	l.prevPlayerX = bx
	l.prevPlayerY = by
	l.safeX = bx
	l.safeY = by
	l.wasGrounded = true

	// Center camera on player initially, clamped to level bounds.
	l.cam.X = bx + playerBoxW/2 + 20
	l.cam.Y = by + playerBoxH/2 - 12
	l.cam.Clamp()

	// Configure deterministic particle emitters using atlas spark frames.
	sparkFrames := []sprite.Frame{
		assets.atlas.Frames["spark_0"],
		assets.atlas.Frames["spark_1"],
		assets.atlas.Frames["spark_2"],
	}
	l.dustEmitter = particles.Emitter{
		Atlas:        assets.atlas,
		Frames:       sparkFrames[:2],
		Shape:        particles.Rect(8, 2),
		MaxParticles: 48,
		LifetimeMin:  180 * time.Millisecond,
		LifetimeMax:  320 * time.Millisecond,
		SpeedMin:     14,
		SpeedMax:     36,
		AngleMin:     -math.Pi * 0.9,
		AngleMax:     -math.Pi * 0.1,
		GravityY:     90,
		SizeCurve: particles.FloatCurve{
			{T: 0, Value: 0.65},
			{T: 1, Value: 0.2},
		},
		AlphaCurve: particles.FloatCurve{
			{T: 0, Value: 0.9},
			{T: 1, Value: 0},
		},
		Seed: 101,
		Z:    25,
	}
	l.sparkEmitter = particles.Emitter{
		Atlas:        assets.atlas,
		Frames:       sparkFrames,
		Shape:        particles.Circle(4),
		MaxParticles: 96,
		LifetimeMin:  240 * time.Millisecond,
		LifetimeMax:  520 * time.Millisecond,
		SpeedMin:     26,
		SpeedMax:     85,
		AngleMin:     0,
		AngleMax:     math.Pi * 2,
		GravityY:     120,
		SizeCurve: particles.FloatCurve{
			{T: 0, Value: 0.9},
			{T: 1, Value: 0.15},
		},
		AlphaCurve: particles.FloatCurve{
			{T: 0, Value: 1},
			{T: 1, Value: 0},
		},
		Seed: 202,
		Z:    30,
	}

	// Build scene graph: world tilemap layer + world entities/particles/popups layer.
	l.sc = scene.NewScene(&l.cam)
	worldLayer := scene.NewLayer("world", scene.SpaceWorld, 0)
	worldLayer.AddTilemap(l.tm)
	l.sc.AddLayer(worldLayer)

	entityLayer := scene.NewLayer("entities", scene.SpaceWorld, 10)
	entityLayer.AddBatch(&l.batch)
	fxDrawable := scene.NewDrawable(func(d *core.Drawing) {
		l.dustEmitter.Draw(d)
		l.sparkEmitter.Draw(d)
		l.drawPopups(d)
	})
	fxDrawable.Z = 30
	entityLayer.AddDrawable(fxDrawable)
	l.sc.AddLayer(entityLayer)

	l.syncSprites(1, screen.Viewport{PixelSnap: true})
	return l, nil
}

// updateDemo advances ambient animations and a gentle camera pan for the title backdrop.
func (l *levelState) updateDemo(dt time.Duration) {
	l.elapsed += dt
	l.sc.Update(dt)
	l.animIdle.Update(dt)
	l.portal.anim.Update(dt)
	for i := range l.gems {
		l.gems[i].anim.Update(dt)
	}
	// updateDemo runs without the level's own update, so snapshot the previous
	// positions itself; syncSprites interpolates against prevX.
	for i := range l.slimes {
		l.slimes[i].prevX = l.slimes[i].x
		l.updateSlime(&l.slimes[i], dt)
	}
	l.dustEmitter.Update(dt)
	l.sparkEmitter.Update(dt)
	l.updatePopups(dt)
	l.updateSwarm(dt)

	wb := l.tm.WorldBounds()
	minCX := l.cam.Width / 2
	maxCX := float32(wb.Size.Width) - l.cam.Width/2
	if maxCX > minCX {
		t := 0.5 - 0.5*math.Cos(l.elapsed.Seconds()*0.32)
		l.cam.X = minCX + float32(t)*(maxCX-minCX)
	}
	l.cam.Y = float32(wb.Size.Height) - l.cam.Height/2
	l.cam.Clamp()
}

// updateEffects advances particles, popups, camera shake, and ambient animations during victory/game-over.
func (l *levelState) updateEffects(dt time.Duration) {
	l.elapsed += dt
	l.sc.Update(dt)
	l.portal.anim.Update(dt)
	l.dustEmitter.Update(dt)
	l.sparkEmitter.Update(dt)
	l.updatePopups(dt)
	l.updateSwarm(dt)
}

// update advances one fixed 60 Hz gameplay step.
func (l *levelState) update(dt time.Duration, in stepInput) {
	if l.outcome != outcomeNone {
		l.updateEffects(dt)
		return
	}

	l.elapsed += dt
	l.steps++

	secs := float32(dt.Seconds())

	// Snapshot positions for render interpolation.
	pb := l.playerBody.Bounds()
	l.prevPlayerX = pb.X
	l.prevPlayerY = pb.Y
	for i := range l.slimes {
		l.slimes[i].prevX = l.slimes[i].x
	}

	// Update timers.
	if l.invulnTimer > 0 {
		l.invulnTimer -= dt
		if l.invulnTimer < 0 {
			l.invulnTimer = 0
		}
	}
	if l.wasGrounded {
		l.coyoteTimer = coyoteDuration
	} else if l.coyoteTimer > 0 {
		l.coyoteTimer -= dt
		if l.coyoteTimer < 0 {
			l.coyoteTimer = 0
		}
	}
	if in.jumpPressed {
		l.jumpBufferTimer = jumpBufferDuration
	} else if l.jumpBufferTimer > 0 {
		l.jumpBufferTimer -= dt
		if l.jumpBufferTimer < 0 {
			l.jumpBufferTimer = 0
		}
	}

	// Horizontal movement with crisp acceleration & ground friction.
	targetVX := in.moveAxis * playerMoveSpeed
	if in.moveAxis < -0.05 {
		l.facingLeft = true
	} else if in.moveAxis > 0.05 {
		l.facingLeft = false
	}

	accel := playerAirAcc
	if l.wasGrounded {
		if in.moveAxis == 0 {
			accel = playerFriction
		} else {
			accel = playerGroundAcc
		}
	}
	l.playerBody.VX = approach(l.playerBody.VX, targetVX, accel*secs)

	// Jump execution (coyote time + jump buffer).
	if l.jumpBufferTimer > 0 && l.coyoteTimer > 0 {
		l.playerBody.VY = playerJumpVel
		l.jumpBufferTimer = 0
		l.coyoteTimer = 0
		l.wasGrounded = false
		l.emitDust(pb.X+pb.W/2, pb.Bottom())
		if l.callbacks.onJump != nil {
			l.callbacks.onJump()
		}
	}

	// Variable jump height on early jump release.
	if in.jumpReleased && l.playerBody.VY < -110 {
		l.playerBody.VY *= 0.55
	}

	// Apply gravity and advance via physics.MoveAndSlide against tilemap.
	l.playerBody.VY += playerGravity * secs
	if l.playerBody.VY > playerMaxFall {
		l.playerBody.VY = playerMaxFall
	}

	wasAirborne := !l.wasGrounded
	res := physics.MoveAndSlide(l.playerBody, l.tm, dt)
	l.wasGrounded = res.Grounded
	pb = l.playerBody.Bounds()

	// Emit subtle landing dust puff when touching down from a fall.
	if wasAirborne && res.Grounded {
		l.emitDust(pb.X+pb.W/2, pb.Bottom())
	}

	// Clamp player horizontally inside world bounds.
	wb := l.tm.WorldBounds()
	worldW := float32(wb.Size.Width)
	worldH := float32(wb.Size.Height)
	if pb.X < 0 {
		l.playerBody.Translate(-pb.X, 0)
		l.playerBody.VX = 0
		pb = l.playerBody.Bounds()
	} else if pb.Right() > worldW {
		l.playerBody.Translate(worldW-pb.Right(), 0)
		l.playerBody.VX = 0
		pb = l.playerBody.Bounds()
	}

	// Update slimes.
	// updateDemo runs without the level's own update, so snapshot the previous
	// positions itself; syncSprites interpolates against prevX.
	for i := range l.slimes {
		l.slimes[i].prevX = l.slimes[i].x
		l.updateSlime(&l.slimes[i], dt)
	}

	// Update safe respawn ledge whenever firmly supported across both feet and clear of hazards/slimes.
	if res.Grounded && l.hasSafeFooting(pb) && !l.overlapsHazard(pb) && !l.overlapsAnySlime(pb) {
		l.safeX = pb.X
		l.safeY = pb.Y
	}

	// Check lava / out-of-bounds hazard.
	if l.overlapsHazard(pb) || pb.Top() > worldH {
		l.handleLavaHit()
		pb = l.playerBody.Bounds()
	}

	// Check slime contacts.
	if l.outcome == outcomeNone && l.invulnTimer <= 0 {
		for i := range l.slimes {
			sb := l.slimes[i].hitbox()
			if physics.TestBoxBox(pb, sb).Hit {
				l.hurtPlayer(sb.X+sb.W/2, false)
				break
			}
		}
	}

	// Check gem collectibles.
	if l.outcome == outcomeNone {
		for i := range l.gems {
			g := &l.gems[i]
			if g.collected {
				continue
			}
			g.anim.Update(dt)
			gb := physics.Box{X: g.x + 2, Y: g.y + 2, W: 12, H: 12}
			if physics.TestBoxBox(pb, gb).Hit {
				g.collected = true
				l.gemsCollected++
				l.score += g.value
				l.emitSpark(g.x+8, g.y+8, 10)
				l.addPopup(fmt.Sprintf("+%d", g.value), g.x+8, g.y)
				if l.callbacks.onGem != nil {
					l.callbacks.onGem(g.value)
				}
			}
		}
	}

	// Check exit portal goal.
	if l.outcome == outcomeNone {
		l.portal.anim.Update(dt)
		goalBox := physics.Box{X: l.portal.x + 2, Y: l.portal.y + 2, W: 12, H: 14}
		if physics.TestBoxBox(pb, goalBox).Hit {
			l.score += l.hearts * heartClearBonus
			l.outcome = outcomeVictory
			l.emitSpark(l.portal.x+8, l.portal.y+8, 24)
			l.addPopup("CLEAR!", l.portal.x+8, l.portal.y-6)
			if l.callbacks.onWin != nil {
				l.callbacks.onWin(l.score, l.gemsCollected, l.hearts)
			}
		}
	}

	// Advance player sprite animation state.
	l.updatePlayerAnimation(dt)

	// Advance tilemap animations, camera shake, particles, and popups.
	l.sc.Update(dt)
	l.dustEmitter.Update(dt)
	l.sparkEmitter.Update(dt)
	l.updatePopups(dt)
	l.updateSwarm(dt)

	// Smooth camera follow with slight horizontal look-ahead, clamped to world bounds.
	lookAhead := float32(18)
	if l.facingLeft {
		lookAhead = -18
	}
	target := core.Point{
		X: core.Points(pb.X + pb.W/2 + lookAhead),
		Y: core.Points(pb.Y + pb.H/2 - 10),
	}
	l.cam.Follow(target, 7.5, dt)
}

func (l *levelState) updateSlime(s *slimeEntity, dt time.Duration) {
	s.anim.Update(dt)
	s.x += s.dir * s.speed * float32(dt.Seconds())
	if s.x <= s.minX {
		s.x = s.minX
		s.dir = 1
	} else if s.x >= s.maxX {
		s.x = s.maxX
		s.dir = -1
	}
}

func (s *slimeEntity) hitbox() physics.Box {
	return physics.Box{X: s.x + 2, Y: s.y + 6, W: 12, H: 10}
}

func (l *levelState) overlapsAnySlime(pb physics.Box) bool {
	for i := range l.slimes {
		if physics.TestBoxBox(pb, l.slimes[i].hitbox()).Hit {
			return true
		}
	}
	return false
}

func (l *levelState) hasSafeFooting(pb physics.Box) bool {
	row := int(math.Floor(float64((pb.Bottom() + 1) / l.tm.TileHeight)))
	leftCol := int(math.Floor(float64((pb.X + 1) / l.tm.TileWidth)))
	rightCol := int(math.Floor(float64((pb.Right() - 1) / l.tm.TileWidth)))
	return l.tm.TileFlagsAt(leftCol, row) != 0 && l.tm.TileFlagsAt(rightCol, row) != 0
}

func (l *levelState) overlapsHazard(pb physics.Box) bool {
	if l.terrain == nil {
		return false
	}
	// Shrink slightly horizontally so grazing a pit edge feels fair.
	left := pb.X + 2
	right := pb.Right() - 2
	if right <= left {
		left, right = pb.X, pb.Right()
	}
	minCol := int(math.Floor(float64(left / l.tm.TileWidth)))
	maxCol := int(math.Floor(float64((right - 0.01) / l.tm.TileWidth)))
	minRow := int(math.Floor(float64(pb.Y / l.tm.TileHeight)))
	maxRow := int(math.Floor(float64((pb.Bottom() - 0.01) / l.tm.TileHeight)))

	for row := minRow; row <= maxRow; row++ {
		if row < 0 || row >= l.terrain.Height {
			continue
		}
		for col := minCol; col <= maxCol; col++ {
			if col < 0 || col >= l.terrain.Width {
				continue
			}
			gid := l.terrain.Data[row*l.terrain.Width+col]
			if gid == 0 {
				continue
			}
			if props := l.tm.TileProperties(gid); props != nil {
				if haz, ok := props.Bool("hazard"); ok && haz {
					return true
				}
			}
		}
	}
	return false
}

func (l *levelState) handleLavaHit() {
	pb := l.playerBody.Bounds()
	if l.invulnTimer <= 0 {
		l.hurtPlayer(pb.X+pb.W/2, true)
	}
	if l.outcome == outcomeNone {
		// Respawn explorer back on the last safe ledge.
		l.playerBody.Shape = physics.BoxShape(physics.Box{
			X: l.safeX,
			Y: l.safeY,
			W: playerBoxW,
			H: playerBoxH,
		})
		l.prevPlayerX = l.safeX
		l.prevPlayerY = l.safeY
		l.playerBody.VX = 0
		l.playerBody.VY = 0
		l.wasGrounded = true
	}
}

func (l *levelState) hurtPlayer(sourceX float32, fromLava bool) {
	l.hearts--
	if l.hearts < 0 {
		l.hearts = 0
	}
	l.invulnTimer = invulnDuration
	if l.shakeScale > 0 {
		l.cam.Shake(4.5*l.shakeScale, 240*time.Millisecond)
	}

	pb := l.playerBody.Bounds()
	l.emitSpark(pb.X+pb.W/2, pb.Y+pb.H/2, 8)
	l.addPopup("-1 HP", pb.X+pb.W/2, pb.Y-4)

	if !fromLava {
		// Knock player away from the slime.
		if pb.X+pb.W/2 < sourceX {
			l.playerBody.VX = -95
		} else {
			l.playerBody.VX = 95
		}
		l.playerBody.VY = -165
		l.wasGrounded = false
	}

	if l.callbacks.onHurt != nil {
		l.callbacks.onHurt(l.hearts)
	}

	if l.hearts <= 0 {
		l.outcome = outcomeGameOver
		if l.callbacks.onLose != nil {
			l.callbacks.onLose(l.score, l.gemsCollected)
		}
	}
}

func (l *levelState) updatePlayerAnimation(dt time.Duration) {
	nextAnim := "idle"
	if !l.wasGrounded {
		nextAnim = "jump"
	} else if math.Abs(float64(l.playerBody.VX)) > 8 {
		nextAnim = "run"
	}

	if nextAnim != l.curAnim {
		l.curAnim = nextAnim
		switch nextAnim {
		case "idle":
			l.animIdle.Reset()
		case "run":
			l.animRun.Reset()
		case "jump":
			l.animJump.Reset()
		}
	}

	switch l.curAnim {
	case "idle":
		l.animIdle.Update(dt)
	case "run":
		l.animRun.Update(dt)
	case "jump":
		l.animJump.Update(dt)
	}
}

func (l *levelState) emitDust(x, y float32) {
	l.dustEmitter.X = x
	l.dustEmitter.Y = y
	l.dustEmitter.Burst(5)
}

func (l *levelState) emitSpark(x, y float32, count int) {
	l.sparkEmitter.X = x
	l.sparkEmitter.Y = y
	l.sparkEmitter.Burst(count)
}

func (l *levelState) addPopup(msg string, x, y float32) {
	l.popups = append(l.popups, floatingPopup{
		text:    msg,
		x:       x,
		y:       y,
		maxLife: 700 * time.Millisecond,
	})
}

func (l *levelState) updatePopups(dt time.Duration) {
	if len(l.popups) == 0 {
		return
	}
	dy := float32(dt.Seconds()) * 22
	n := 0
	for _, p := range l.popups {
		p.age += dt
		p.y -= dy
		if p.age < p.maxLife {
			l.popups[n] = p
			n++
		}
	}
	l.popups = l.popups[:n]
}

func (l *levelState) drawPopups(d *core.Drawing) {
	if l.assets.font == nil || len(l.popups) == 0 {
		return
	}
	for _, p := range l.popups {
		alpha := float32(1.0 - float64(p.age)/float64(p.maxLife))
		if alpha <= 0 {
			continue
		}
		l.assets.font.Draw(d, p.text, p.x, p.y, text.Options{
			Align: text.AlignCenter,
			Alpha: alpha,
			Scale: 1,
		})
	}
}

// syncSprites updates sprite positions with alpha interpolation and rebuilds the batch slice without reallocating.
func (l *levelState) syncSprites(alpha float64, vp screen.Viewport) {
	t := float32(alpha)
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}

	l.batch.Sprites = l.batch.Sprites[:0]

	// Portal.
	l.portal.spr.Frame = l.portal.anim.Frame()
	l.portal.spr.X, l.portal.spr.Y = vp.Snap(l.portal.x, l.portal.y)
	l.batch.Sprites = append(l.batch.Sprites, &l.portal.spr)

	// Uncollected gems with gentle bob.
	for i := range l.gems {
		g := &l.gems[i]
		if g.collected {
			continue
		}
		bob := float32(math.Sin(l.elapsed.Seconds()*3.5+g.phase)) * 1.5
		g.spr.Frame = g.anim.Frame()
		g.spr.X, g.spr.Y = vp.Snap(g.x, g.y+bob)
		l.batch.Sprites = append(l.batch.Sprites, &g.spr)
	}

	// Patrolling slimes.
	for i := range l.slimes {
		s := &l.slimes[i]
		ix := s.prevX + (s.x-s.prevX)*t
		s.spr.Frame = s.anim.Frame()
		s.spr.X, s.spr.Y = vp.Snap(ix, s.y)
		s.spr.FlipX = s.dir < 0
		l.batch.Sprites = append(l.batch.Sprites, &s.spr)
	}

	// Stress swarm, laid over the camera's view.
	if l.swarm != nil {
		l.batch.Sprites = l.swarm.sync(l.batch.Sprites, t, l.cam.X-l.cam.Width/2, l.cam.Y-l.cam.Height/2, vp)
	}

	// Explorer hero.
	pb := l.playerBody.Bounds()
	px := l.prevPlayerX + (pb.X-l.prevPlayerX)*t
	py := l.prevPlayerY + (pb.Y-l.prevPlayerY)*t
	switch l.curAnim {
	case "run":
		l.heroSpr.Frame = l.animRun.Frame()
	case "jump":
		l.heroSpr.Frame = l.animJump.Frame()
	default:
		l.heroSpr.Frame = l.animIdle.Frame()
	}
	l.heroSpr.X, l.heroSpr.Y = vp.Snap(px+playerSpriteOX, py+playerSpriteOY)
	l.heroSpr.FlipX = l.facingLeft
	if l.invulnTimer > 0 && (l.invulnTimer/(80*time.Millisecond))%2 == 0 {
		l.heroSpr.Alpha = 0.35
	} else {
		l.heroSpr.Alpha = 1
	}
	l.batch.Sprites = append(l.batch.Sprites, &l.heroSpr)
}

// setSwarm turns the 1000-firefly stress swarm on or off. A swarm turned on again starts afresh from the same seed.
func (l *levelState) setSwarm(on bool) {
	switch {
	case on && l.swarm == nil:
		l.swarm = newFireflySwarm(l.assets.atlas, l.cam.Width, l.cam.Height)
		// Grow the batch once, so syncSprites appends the swarm without reallocating.
		l.batch.Sprites = slices.Grow(l.batch.Sprites[:0], l.spriteCount())
	case !on:
		l.swarm = nil
	}
}

func (l *levelState) updateSwarm(dt time.Duration) {
	if l.swarm != nil {
		l.swarm.update(dt)
	}
}

// spriteCount returns how many sprites the level's batch draws this frame: the portal, the gems not yet collected, the slimes, the swarm and the explorer.
func (l *levelState) spriteCount() int {
	n := 2 + len(l.slimes)
	for i := range l.gems {
		if !l.gems[i].collected {
			n++
		}
	}
	if l.swarm != nil {
		n += len(l.swarm.flies)
	}
	return n
}

func (l *levelState) draw(d *core.Drawing, alpha float64, vp screen.Viewport) {
	l.syncSprites(alpha, vp)
	l.sc.Draw(d)
}

func approach(cur, target, maxDelta float32) float32 {
	if cur < target {
		cur += maxDelta
		if cur > target {
			return target
		}
		return cur
	}
	cur -= maxDelta
	if cur < target {
		return target
	}
	return cur
}

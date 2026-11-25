package main

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/bullets"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/game/packages/game/sprite"
)

// controls is what the player asks of the ship in one fixed step, gathered from every device by the game's input code, so the world never reads a device.
type controls struct {
	// MoveX and MoveY steer the ship, each in [-1, 1]: keys, the D-pad and the left stick.
	MoveX, MoveY float32
	// DragX and DragY move the ship by this many points, as a finger or the mouse dragged it since the last step.
	DragX, DragY float32
	// Fire holds the ship's guns down.
	Fire bool
	// Focus slows the ship to its focus speed, for threading through bullets.
	Focus bool
	// Bomb sets off a bomb; it is true for the one step the bomb button went down in.
	Bomb bool
}

// outcome is how a run of the stage stands.
type outcome uint8

const (
	// running is a run still being played.
	running outcome = iota
	// cleared is a run that defeated the boss: the win.
	cleared
	// lost is a run whose ship lost its last life: the loss.
	lost
)

// event is something that happened in a step that the game plays a sound for.
type event uint8

const (
	eventShot event = iota
	eventHit
	eventExplosion
	eventPowerUp
	eventBomb
	eventLifeLost
	eventBossPhase
)

// endDelay is how long the world plays on after the boss falls or the ship is lost, so the explosion shows before the end screen.
const endDelay = 2 * time.Second

// itemSpeed is how fast a dropped item drifts down, and itemRadius how near the ship collects it.
const (
	itemSpeed  = 45
	itemRadius = 12
)

// Caps on what items can raise.
const (
	maxLives = 6
	maxBombs = 6
	// overPowerScore is what a power item is worth once the weapon is at its last level.
	overPowerScore = 1000
)

// ship is the player's ship.
type ship struct {
	x, y, prevX, prevY float32
	lives, bombs       int
	// weapon indexes the stage's ship weapons.
	weapon int
	// invulnerable counts down the time it cannot be hit.
	invulnerable time.Duration
	alive        bool
	sprite       sprite.Sprite
}

// enemy is one enemy in flight, of a kind from the stage, sent by a wave.
type enemy struct {
	def                *enemyDef
	wave               int
	x, y, prevX, prevY float32
	originX            float32
	age, held          time.Duration
	hp                 int
	// flash counts down the time it shows it was hit.
	flash   time.Duration
	emitter *bullets.Emitter
	sprite  sprite.Sprite
	dead    bool
}

// waveState follows one wave: how many it has sent, how many are still in flight or to come, and how many the ship destroyed.
type waveState struct {
	sent, left, destroyed int
}

// item is a dropped item drifting down to be collected.
type item struct {
	kind   string
	x, y   float32
	sprite sprite.Sprite
}

// boss is the stage's boss once it has arrived.
type boss struct {
	def                *bossDef
	x, y, prevX, prevY float32
	hp, phase          int
	// entered is whether it has reached its place, before which it neither fires nor takes damage.
	entered  bool
	swayLeft bool
	flash    time.Duration
	emitters []*bullets.Emitter
	sprite   sprite.Sprite
	dead     bool
}

// world is one run of a stage: the ship, the enemies, the boss and every bullet, item and explosion, stepped at a fixed rate with the player's controls. It knows nothing of devices, screens or sound, so tests drive it directly; it reports what the game should play in events.
type world struct {
	stage    *stageFile
	patterns *bullets.File
	atlas    *sprite.Atlas
	width    float32
	height   float32

	elapsed time.Duration
	scroll  float32
	ship    ship
	enemies []*enemy
	waves   []waveState
	items   []*item
	boss    *boss
	spawned int

	shipShots  *bullets.Field
	enemyShots *bullets.Field
	weapon     *bullets.Emitter
	explosions *particles.Emitter

	score   int
	outcome outcome
	// ending counts down endDelay once the run has an outcome.
	ending time.Duration
	// bombFlash counts down the screen flash of a bomb.
	bombFlash time.Duration
	// events are what happened in the last step.
	events []event

	batch   sprite.Batch
	sprites []*sprite.Sprite
}

// newWorld starts a run of stage on a playfield of width by height points, with bullets from patterns drawn from atlas and explosions as explosion configures them. It checks every pattern and frame the stage names.
func newWorld(stage *stageFile, patterns *bullets.File, atlas *sprite.Atlas, explosion particles.Config, width, height float32) (*world, error) {
	w := &world{stage: stage, patterns: patterns, atlas: atlas, width: width, height: height}
	if err := w.check(); err != nil {
		return nil, err
	}
	bounds := math2d.Rect{W: width, H: height}
	w.shipShots = bullets.NewField(atlas, 256)
	w.shipShots.Bounds = bounds
	w.enemyShots = bullets.NewField(atlas, 1024)
	w.enemyShots.Bounds = bounds
	w.explosions = &particles.Emitter{Atlas: atlas}
	w.explosions.ReloadConfig(explosion)
	w.waves = make([]waveState, len(stage.Waves))
	for i, wave := range stage.Waves {
		w.waves[i].left = wave.Count
	}
	def := &stage.Ship
	w.ship = ship{x: width / 2, y: height - 60, lives: def.Lives, bombs: def.Bombs, alive: true, sprite: sprite.NewSprite(atlas.Frames["ship"])}
	w.ship.prevX, w.ship.prevY = w.ship.x, w.ship.y
	w.arm()
	w.batch.Atlas = atlas
	return w, nil
}

// check reports the first bullet pattern or atlas frame the stage names that the pattern file or atlas lacks.
func (w *world) check() error {
	pattern := func(what, name string) error {
		if _, ok := w.patterns.Pattern(name); !ok {
			return fmt.Errorf("stage: %s names pattern %q, which data/patterns.yaml lacks", what, name)
		}
		return nil
	}
	frame := func(what, name string) error {
		if _, ok := w.atlas.Frames[name]; !ok {
			return fmt.Errorf("stage: %s names frame %q, which the atlas lacks", what, name)
		}
		return nil
	}
	for _, name := range []string{"ship", "item_" + itemPower, "item_" + itemBomb, "item_" + itemLife} {
		if err := frame("the game", name); err != nil {
			return err
		}
	}
	for _, name := range w.stage.Ship.Weapons {
		if err := pattern("the ship", name); err != nil {
			return err
		}
	}
	for _, e := range w.stage.Enemies {
		if err := frame("enemy "+e.Name, e.Frame); err != nil {
			return err
		}
		if e.Pattern != "" {
			if err := pattern("enemy "+e.Name, e.Pattern); err != nil {
				return err
			}
		}
	}
	if err := frame("the boss", w.stage.Boss.Frame); err != nil {
		return err
	}
	for _, p := range w.stage.Boss.Phases {
		for _, name := range p.Patterns {
			if err := pattern("the boss", name); err != nil {
				return err
			}
		}
	}
	return nil
}

// arm gives the ship the emitter of its current weapon.
func (w *world) arm() {
	p, _ := w.patterns.Pattern(w.stage.Ship.Weapons[w.ship.weapon])
	// check found the pattern, so NewEmitter only fails on a pattern Parse would have refused.
	w.weapon, _ = bullets.NewEmitter(w.shipShots, p, 0)
	w.weapon.Stopped = true
}

// step moves the run on by dt with the player's controls: the stage's timeline, then movement, then firing, then collisions.
func (w *world) step(dt time.Duration, in controls) {
	w.events = w.events[:0]
	secs := float32(dt.Seconds())
	w.elapsed += dt
	w.scroll += w.stage.ScrollSpeed * secs
	w.bombFlash = max(0, w.bombFlash-dt)
	if w.outcome != running {
		in = controls{}
		w.ending = max(0, w.ending-dt)
	}

	w.spawnWaves()
	w.spawnBoss()
	w.moveShip(dt, in)
	w.moveEnemies(dt)
	w.moveBoss(dt)
	w.moveItems(secs)

	w.shipShots.Update(dt)
	w.enemyShots.Update(dt)
	w.fire(dt, in)
	w.explosions.Update(dt)

	if in.Bomb {
		w.bomb()
	}
	w.hitEnemies()
	w.hitShip()
	w.collectItems()
	w.removeGone()
}

// done reports whether the run has an outcome and has shown it for endDelay, when the game shows its end screen.
func (w *world) done() bool { return w.outcome != running && w.ending == 0 }

// spawnWaves sends every enemy whose time has come.
func (w *world) spawnWaves() {
	for i, wave := range w.stage.Waves {
		st := &w.waves[i]
		for st.sent < wave.Count && w.elapsed >= wave.At+time.Duration(st.sent)*wave.Every {
			def, _ := w.stage.enemy(wave.Enemy)
			x := wave.X + float32(st.sent)*wave.StepX
			e := &enemy{def: def, wave: i, x: x, y: wave.Y, prevX: x, prevY: wave.Y, originX: x, hp: def.HP, sprite: sprite.NewSprite(w.atlas.Frames[def.Frame])}
			if def.Pattern != "" {
				p, _ := w.patterns.Pattern(def.Pattern)
				w.spawned++
				e.emitter, _ = bullets.NewEmitter(w.enemyShots, p, w.stage.Seed*1000+uint64(w.spawned))
				e.emitter.Stopped = true
			}
			w.enemies = append(w.enemies, e)
			st.sent++
		}
	}
}

// spawnBoss brings the boss in at its time, above the top of the screen.
func (w *world) spawnBoss() {
	def := &w.stage.Boss
	if w.boss != nil || w.elapsed < def.At {
		return
	}
	b := &boss{def: def, x: w.width / 2, y: -def.Radius * 2, hp: def.HP, sprite: sprite.NewSprite(w.atlas.Frames[def.Frame])}
	b.prevX, b.prevY = b.x, b.y
	w.boss = b
	w.startPhase(0)
}

// startPhase arms the boss with the patterns of phase i.
func (w *world) startPhase(i int) {
	b := w.boss
	b.phase = i
	b.emitters = b.emitters[:0]
	for j, name := range b.def.Phases[i].Patterns {
		p, _ := w.patterns.Pattern(name)
		e, _ := bullets.NewEmitter(w.enemyShots, p, w.stage.Seed*7919+uint64(i*16+j))
		e.Stopped = !b.entered
		b.emitters = append(b.emitters, e)
	}
}

// moveShip moves the ship by the controls, inside the playfield, and runs down its invulnerability.
func (w *world) moveShip(dt time.Duration, in controls) {
	s := &w.ship
	s.prevX, s.prevY = s.x, s.y
	s.invulnerable = max(0, s.invulnerable-dt)
	if !s.alive {
		return
	}
	speed := w.stage.Ship.Speed
	if in.Focus {
		speed = w.stage.Ship.FocusSpeed
	}
	mx, my := clampf(in.MoveX, -1, 1), clampf(in.MoveY, -1, 1)
	// A diagonal is no faster than a straight line.
	if l := float32(math.Hypot(float64(mx), float64(my))); l > 1 {
		mx, my = mx/l, my/l
	}
	secs := float32(dt.Seconds())
	const margin = 8
	s.x = clampf(s.x+mx*speed*secs+in.DragX, margin, w.width-margin)
	s.y = clampf(s.y+my*speed*secs+in.DragY, margin, w.height-margin)
}

// moveEnemies flies every enemy along its path and keeps its guns quiet while it is off the screen.
func (w *world) moveEnemies(dt time.Duration) {
	secs := float32(dt.Seconds())
	for _, e := range w.enemies {
		e.prevX, e.prevY = e.x, e.y
		e.age += dt
		e.flash = max(0, e.flash-dt)
		d := e.def
		switch d.Path {
		case pathDive:
			e.y += d.Speed * secs
		case pathWeave:
			e.y += d.Speed * secs
			turn := 2 * math.Pi * e.age.Seconds() / d.Period.Seconds()
			e.x = e.originX + d.Amplitude*float32(math.Sin(turn))
		case pathCross:
			dir := float32(1)
			if e.originX > w.width/2 {
				dir = -1
			}
			e.x += dir * d.Speed * secs
			e.y += d.Speed * 0.2 * secs
		case pathHover:
			if e.y < d.StopY || e.held >= d.Hold {
				e.y += d.Speed * secs
			} else {
				e.held += dt
			}
		}
		if e.emitter != nil {
			e.emitter.Stopped = !w.onScreen(e.x, e.y) || !w.ship.alive
		}
	}
}

// moveBoss brings the boss down to its place, then moves it as its phase says.
func (w *world) moveBoss(dt time.Duration) {
	b := w.boss
	if b == nil || b.dead {
		return
	}
	b.prevX, b.prevY = b.x, b.y
	b.flash = max(0, b.flash-dt)
	step := b.def.Speed * float32(dt.Seconds())
	if !b.entered {
		b.y = min(b.def.Y, b.y+step)
		if b.y >= b.def.Y {
			b.entered = true
			for _, e := range b.emitters {
				e.Stopped = false
			}
		}
		return
	}
	edge := b.def.Radius + 20
	target := w.width / 2
	switch b.def.Phases[b.phase].Move {
	case moveSway:
		if b.swayLeft {
			target = edge
		} else {
			target = w.width - edge
		}
		if math.Abs(float64(b.x-target)) <= float64(step) {
			b.swayLeft = !b.swayLeft
		}
	case moveChase:
		target = clampf(w.ship.x, edge, w.width-edge)
		step *= 0.6
	}
	b.x += clampf(target-b.x, -step, step)
	for _, e := range b.emitters {
		e.Stopped = !w.ship.alive
	}
}

// moveItems drifts every item down.
func (w *world) moveItems(secs float32) {
	for _, it := range w.items {
		it.y += itemSpeed * secs
	}
}

// fire fires the ship's weapon while its guns are held, and every enemy's and the boss's patterns at the ship.
func (w *world) fire(dt time.Duration, in controls) {
	s := &w.ship
	w.weapon.Stopped = !in.Fire || !s.alive
	w.weapon.X, w.weapon.Y = s.x, s.y-10
	w.weapon.TargetX, w.weapon.TargetY = s.x, s.y-100
	if w.weapon.Update(dt) > 0 {
		w.events = append(w.events, eventShot)
	}
	for _, e := range w.enemies {
		if e.emitter != nil {
			e.emitter.X, e.emitter.Y = e.x, e.y
			e.emitter.TargetX, e.emitter.TargetY = s.x, s.y
			e.emitter.Update(dt)
		}
	}
	if b := w.boss; b != nil && !b.dead {
		for _, e := range b.emitters {
			e.X, e.Y = b.x, b.y+b.def.Radius*0.5
			e.TargetX, e.TargetY = s.x, s.y
			e.Update(dt)
		}
	}
}

// bomb sets off a bomb when the ship has one: it clears every enemy bullet, damages every enemy on the screen and the boss, and shields the ship for a while.
func (w *world) bomb() {
	s := &w.ship
	if !s.alive || s.bombs == 0 {
		return
	}
	s.bombs--
	s.invulnerable = max(s.invulnerable, w.stage.Ship.Invulnerable)
	w.bombFlash = 400 * time.Millisecond
	w.enemyShots.Clear()
	w.events = append(w.events, eventBomb)
	damage := w.stage.Ship.BombDamage
	for _, e := range w.enemies {
		if !e.dead && w.onScreen(e.x, e.y) {
			w.damageEnemy(e, damage)
		}
	}
	if b := w.boss; b != nil && b.entered && !b.dead {
		w.damageBoss(damage)
	}
}

// hitEnemies takes the ship's bullets that hit an enemy or the boss out of the field and damages what they hit.
func (w *world) hitEnemies() {
	damage := w.stage.Ship.Damage
	w.shipShots.RemoveFunc(func(b *bullets.Bullet) bool {
		hit := b.Circle()
		for _, e := range w.enemies {
			if !e.dead && w.onScreen(e.x, e.y) && hit.Overlaps(math2d.Circle{Center: math2d.V2(e.x, e.y), Radius: e.def.Radius}) {
				w.damageEnemy(e, damage)
				return true
			}
		}
		if bs := w.boss; bs != nil && bs.entered && !bs.dead && hit.Overlaps(math2d.Circle{Center: math2d.V2(bs.x, bs.y), Radius: bs.def.Radius}) {
			w.damageBoss(damage)
			return true
		}
		return false
	})
}

// damageEnemy takes damage off e, and destroys it, scoring it and maybe dropping its wave's item, once it has none left.
func (w *world) damageEnemy(e *enemy, damage int) {
	e.hp -= damage
	e.flash = 80 * time.Millisecond
	if e.hp > 0 {
		w.events = append(w.events, eventHit)
		return
	}
	e.dead = true
	w.score += e.def.Score
	w.explode(e.x, e.y, 18)
	st := &w.waves[e.wave]
	st.destroyed++
	st.left--
	wave := w.stage.Waves[e.wave]
	if st.left == 0 && st.destroyed == wave.Count && wave.Drop != "" {
		w.items = append(w.items, &item{kind: wave.Drop, x: e.x, y: e.y, sprite: sprite.NewSprite(w.atlas.Frames["item_"+wave.Drop])})
	}
}

// damageBoss takes damage off the boss, moves it to the next phase its hit points reach, and defeats it, clearing the stage, once it has none left.
func (w *world) damageBoss(damage int) {
	b := w.boss
	b.hp = max(0, b.hp-damage)
	b.flash = 60 * time.Millisecond
	if b.hp == 0 {
		b.dead = true
		w.score += b.def.Score
		w.explode(b.x, b.y, 80)
		w.enemyShots.Clear()
		for _, e := range w.enemies {
			if !e.dead {
				e.dead = true
				w.explode(e.x, e.y, 12)
			}
		}
		w.finish(cleared)
		return
	}
	w.events = append(w.events, eventHit)
	next := b.phase + 1
	if next < len(b.def.Phases) && float32(b.hp) <= b.def.Phases[next].Below*float32(b.def.HP) {
		// A new phase starts from a clear screen, as the genre does, so the player sees the new pattern begin.
		w.enemyShots.Clear()
		w.startPhase(next)
		w.events = append(w.events, eventBossPhase)
	}
}

// hitShip costs the ship a life when an enemy bullet, an enemy or the boss touches it while it can be hit.
func (w *world) hitShip() {
	s := &w.ship
	if !s.alive || s.invulnerable > 0 || w.outcome != running {
		return
	}
	body := math2d.Circle{Center: math2d.V2(s.x, s.y), Radius: w.stage.Ship.Radius}
	hit := w.enemyShots.RemoveFunc(func(b *bullets.Bullet) bool { return b.Circle().Overlaps(body) }) > 0
	for _, e := range w.enemies {
		if !hit && !e.dead && body.Overlaps(math2d.Circle{Center: math2d.V2(e.x, e.y), Radius: e.def.Radius}) {
			hit = true
			w.damageEnemy(e, e.hp)
		}
	}
	if b := w.boss; !hit && b != nil && !b.dead && body.Overlaps(math2d.Circle{Center: math2d.V2(b.x, b.y), Radius: b.def.Radius}) {
		hit = true
	}
	if !hit {
		return
	}
	s.lives--
	w.explode(s.x, s.y, 30)
	w.enemyShots.Clear()
	if s.lives == 0 {
		s.alive = false
		w.finish(lost)
		return
	}
	w.events = append(w.events, eventLifeLost)
	def := &w.stage.Ship
	s.invulnerable = def.Invulnerable
	s.bombs = max(s.bombs, def.Bombs)
	if s.weapon > 0 {
		s.weapon--
		w.arm()
	}
}

// collectItems applies every item the ship touches.
func (w *world) collectItems() {
	s := &w.ship
	if !s.alive {
		return
	}
	n := 0
	for _, it := range w.items {
		dx, dy := it.x-s.x, it.y-s.y
		if dx*dx+dy*dy > itemRadius*itemRadius {
			w.items[n] = it
			n++
			continue
		}
		w.events = append(w.events, eventPowerUp)
		switch it.kind {
		case itemPower:
			if s.weapon < len(w.stage.Ship.Weapons)-1 {
				s.weapon++
				w.arm()
			} else {
				w.score += overPowerScore
			}
		case itemBomb:
			s.bombs = min(maxBombs, s.bombs+1)
		case itemLife:
			s.lives = min(maxLives, s.lives+1)
		}
	}
	clear(w.items[n:])
	w.items = w.items[:n]
}

// removeGone drops destroyed enemies and those that flew off, and items that drifted off the bottom.
func (w *world) removeGone() {
	n := 0
	for _, e := range w.enemies {
		gone := e.x < -48 || e.x > w.width+48 || e.y > w.height+48
		if !e.dead && gone {
			w.waves[e.wave].left--
		}
		if !e.dead && !gone {
			w.enemies[n] = e
			n++
		}
	}
	clear(w.enemies[n:])
	w.enemies = w.enemies[:n]
	n = 0
	for _, it := range w.items {
		if it.y < w.height+16 {
			w.items[n] = it
			n++
		}
	}
	clear(w.items[n:])
	w.items = w.items[:n]
}

// finish gives the run its outcome, the first one only.
func (w *world) finish(o outcome) {
	if w.outcome != running {
		return
	}
	w.outcome = o
	w.ending = endDelay
	w.events = append(w.events, eventExplosion)
}

// explode bursts count sparks at (x, y).
func (w *world) explode(x, y float32, count int) {
	w.explosions.X, w.explosions.Y = x, y
	w.explosions.Burst(count)
	w.events = append(w.events, eventExplosion)
}

// onScreen reports whether (x, y) is on the playfield, where enemies fire and can be hit.
func (w *world) onScreen(x, y float32) bool {
	return x >= 0 && x <= w.width && y >= 0 && y <= w.height
}

// bossHealth returns the boss's name and the fraction of its hit points it has left, and false while no boss is fighting.
func (w *world) bossHealth() (string, float32, bool) {
	b := w.boss
	if b == nil || b.dead {
		return "", 0, false
	}
	return b.def.Name, float32(b.hp) / float32(b.def.HP), true
}

func clampf(v, lo, hi float32) float32 { return max(lo, min(hi, v)) }

func lerpf(a, b, t float32) float32 { return a + (b-a)*t }

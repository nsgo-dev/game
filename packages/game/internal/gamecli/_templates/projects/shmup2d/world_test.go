package main

import (
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/bullets"
)

// newTestWorld starts a run of the stage the game ships, on the playfield game.yaml sets.
func newTestWorld(t testing.TB) *world {
	t.Helper()
	data, err := loadData()
	if err != nil {
		t.Fatal(err)
	}
	w, err := data.newRun(viewport.Width, viewport.Height)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// runUntil steps w with the controls play decides until it has an outcome and has shown it, or limit has passed, and returns the steps it took.
func runUntil(w *world, limit time.Duration, play func(*world) controls) int {
	n := 0
	for ; w.elapsed < limit && !w.done(); n++ {
		w.step(step, play(w))
	}
	return n
}

// shield keeps w's ship from being hit for the rest of a test, so a test can watch the stage play out around it.
func shield(w *world) { w.ship.invulnerable = time.Hour }

// stepFor steps w for d with no controls.
func stepFor(w *world, d time.Duration) {
	for end := w.elapsed + d; w.elapsed < end; {
		w.step(step, controls{})
	}
}

// waveEnemies returns the enemies of wave i still in flight.
func waveEnemies(w *world, i int) []*enemy {
	var out []*enemy
	for _, e := range w.enemies {
		if e.wave == i && !e.dead {
			out = append(out, e)
		}
	}
	return out
}

// TestPilotClearsTheStage plays the stage with the test pilot, which wins it, as a player who dodges and shoots does.
func TestPilotClearsTheStage(t *testing.T) {
	w := newTestWorld(t)
	var p pilot
	runUntil(w, 3*time.Minute, p.decide)
	if w.outcome != cleared {
		_, hp, _ := w.bossHealth()
		t.Fatalf("the pilot did not clear the stage: outcome %d at %v, boss health %.2f", w.outcome, w.elapsed, hp)
	}
}

// TestIdleShipIsLost leaves the ship where it starts, never firing, and the enemies shoot it down.
func TestIdleShipIsLost(t *testing.T) {
	w := newTestWorld(t)
	runUntil(w, 3*time.Minute, func(*world) controls { return controls{} })
	if w.outcome != lost {
		t.Fatalf("a ship that never moves or fires was not lost: outcome %d at %v", w.outcome, w.elapsed)
	}
}

func TestWavesSendTheirEnemiesOnTime(t *testing.T) {
	w := newTestWorld(t)
	shield(w)
	first := w.stage.Waves[0]
	stepFor(w, first.At-step)
	if len(w.enemies) != 0 {
		t.Fatalf("%d enemies before the first wave's time", len(w.enemies))
	}
	stepFor(w, step)
	if got := waveEnemies(w, 0); len(got) != 1 || got[0].x != first.X || got[0].def.Name != first.Enemy {
		t.Fatalf("the first wave's first enemy did not come in at x %v", first.X)
	}
	stepFor(w, time.Duration(first.Count-1)*first.Every)
	got := waveEnemies(w, 0)
	if len(got) != first.Count {
		t.Fatalf("the first wave has sent %d enemies, want %d", len(got), first.Count)
	}
	if last := got[len(got)-1]; last.originX != first.X+float32(first.Count-1)*first.StepX {
		t.Fatalf("the wave's last enemy came in at x %v, want each one stepX further", last.originX)
	}
}

func TestDestroyingAWholeWaveDropsItsItem(t *testing.T) {
	w := newTestWorld(t)
	shield(w)
	first := w.stage.Waves[0]
	stepFor(w, first.At+time.Duration(first.Count)*first.Every+500*time.Millisecond)
	enemies := waveEnemies(w, 0)
	for _, e := range enemies[:len(enemies)-1] {
		w.damageEnemy(e, e.hp)
	}
	if len(w.items) != 0 {
		t.Fatal("the wave dropped its item before its last enemy fell")
	}
	last := enemies[len(enemies)-1]
	w.damageEnemy(last, last.hp)
	if len(w.items) != 1 || w.items[0].kind != first.Drop || w.items[0].x != last.x {
		t.Fatalf("destroying the whole wave dropped %d items, want one %s where its last enemy fell", len(w.items), first.Drop)
	}
	if want := first.Count * w.stage.Enemies[0].Score; w.score != want {
		t.Fatalf("score %d, want %d for the wave", w.score, want)
	}
}

func TestAWaveThatGetsAwayDropsNothing(t *testing.T) {
	w := newTestWorld(t)
	shield(w)
	first := w.stage.Waves[0]
	stepFor(w, first.At+time.Duration(first.Count)*first.Every+500*time.Millisecond)
	enemies := waveEnemies(w, 0)
	for _, e := range enemies[1:] {
		w.damageEnemy(e, e.hp)
	}
	// The ship moves out of the way, so the one left flies off the bottom.
	w.ship.x = w.width - 10
	stepFor(w, 5*time.Second)
	if len(waveEnemies(w, 0)) != 0 || w.waves[0].left != 0 {
		t.Fatal("the enemy left did not fly off")
	}
	if len(w.items) != 0 {
		t.Fatal("a wave that one enemy escaped from dropped its item")
	}
}

func TestItemsPowerUpTheShip(t *testing.T) {
	w := newTestWorld(t)
	s := &w.ship
	drop := func(kind string) {
		w.items = append(w.items, &item{kind: kind, x: s.x, y: s.y})
		w.step(step, controls{})
	}
	weapons := len(w.stage.Ship.Weapons)
	for i := 1; i < weapons; i++ {
		drop(itemPower)
		if s.weapon != i || w.weapon.Pattern().Name != w.stage.Ship.Weapons[i] {
			t.Fatalf("power item %d left the ship on weapon %d (%s)", i, s.weapon, w.weapon.Pattern().Name)
		}
	}
	score := w.score
	drop(itemPower)
	if s.weapon != weapons-1 || w.score != score+overPowerScore {
		t.Fatalf("a power item at the last weapon: weapon %d, score +%d; want the weapon kept and %d scored", s.weapon, w.score-score, overPowerScore)
	}
	bombs, lives := s.bombs, s.lives
	drop(itemBomb)
	drop(itemLife)
	if s.bombs != bombs+1 || s.lives != lives+1 {
		t.Fatalf("bomb and life items: bombs %d -> %d, lives %d -> %d", bombs, s.bombs, lives, s.lives)
	}
	for range maxLives + 2 {
		drop(itemLife)
	}
	if s.lives != maxLives {
		t.Fatalf("lives %d after many life items, want the cap %d", s.lives, maxLives)
	}
}

// enemyBulletAt fires one enemy bullet from a test pattern, then moves it to (x, y).
func enemyBulletAt(t *testing.T, w *world, x, y float32) {
	t.Helper()
	p := &bullets.Pattern{Name: "test", Kind: bullets.Ring, Count: 1, Speed: 1, Shots: 1, Radius: 3}
	e, err := bullets.NewEmitter(w.enemyShots, p, 1)
	if err != nil {
		t.Fatal(err)
	}
	e.Update(step)
	all := w.enemyShots.Bullets()
	all[len(all)-1].X, all[len(all)-1].Y = x, y
}

func TestBombClearsBulletsAndShieldsTheShip(t *testing.T) {
	w := newTestWorld(t)
	s := &w.ship
	enemyBulletAt(t, w, s.x+40, s.y)
	bombs := s.bombs
	w.step(step, controls{Bomb: true})
	if w.enemyShots.Len() != 0 || s.bombs != bombs-1 || s.invulnerable == 0 {
		t.Fatalf("a bomb left %d bullets, bombs %d -> %d, invulnerable %v", w.enemyShots.Len(), bombs, s.bombs, s.invulnerable)
	}
	s.bombs = 0
	enemyBulletAt(t, w, s.x+40, s.y)
	w.step(step, controls{Bomb: true})
	if w.enemyShots.Len() == 0 {
		t.Fatal("a ship with no bombs set one off")
	}
}

func TestAHitCostsALifeAndAPowerLevel(t *testing.T) {
	w := newTestWorld(t)
	s := &w.ship
	s.weapon = 2
	w.arm()
	s.bombs = 0
	enemyBulletAt(t, w, s.x, s.y)
	enemyBulletAt(t, w, s.x+60, s.y)
	w.step(step, controls{})
	def := &w.stage.Ship
	if s.lives != def.Lives-1 || s.weapon != 1 || s.bombs != def.Bombs || w.enemyShots.Len() != 0 {
		t.Fatalf("after a hit: lives %d, weapon %d, bombs %d, %d bullets left; want a life and a level lost, bombs restocked and the screen cleared", s.lives, s.weapon, s.bombs, w.enemyShots.Len())
	}
	// It cannot be hit again until its invulnerability runs out.
	enemyBulletAt(t, w, s.x, s.y)
	w.step(step, controls{})
	if s.lives != def.Lives-1 {
		t.Fatal("the ship was hit again while invulnerable")
	}
	stepFor(w, def.Invulnerable)
	enemyBulletAt(t, w, s.x, s.y)
	w.step(step, controls{})
	if s.lives != def.Lives-2 {
		t.Fatal("the ship could not be hit once its invulnerability ran out")
	}
}

func TestLosingTheLastLifeEndsTheRun(t *testing.T) {
	w := newTestWorld(t)
	w.ship.lives = 1
	enemyBulletAt(t, w, w.ship.x, w.ship.y)
	w.step(step, controls{})
	if w.outcome != lost || w.ship.alive || w.done() {
		t.Fatalf("outcome %d, alive %v, done %v; want the run lost and its end showing", w.outcome, w.ship.alive, w.done())
	}
	stepFor(w, endDelay)
	if !w.done() {
		t.Fatal("the run was not done once its end had shown")
	}
}

// bossFight steps w to the boss's arrival and until it has entered, with the ship shielded.
func bossFight(t *testing.T) *world {
	t.Helper()
	w := newTestWorld(t)
	shield(w)
	stepFor(w, w.stage.Boss.At)
	for w.boss == nil || !w.boss.entered {
		w.step(step, controls{})
		if w.elapsed > w.stage.Boss.At+time.Minute {
			t.Fatal("the boss never entered")
		}
	}
	return w
}

func TestBossChangesPhaseAtItsThresholds(t *testing.T) {
	w := bossFight(t)
	b := w.boss
	if b.phase != 0 || len(b.emitters) != len(b.def.Phases[0].Patterns) {
		t.Fatalf("the boss entered in phase %d", b.phase)
	}
	for i := 1; i < len(b.def.Phases); i++ {
		threshold := int(b.def.Phases[i].Below * float32(b.def.HP))
		w.damageBoss(b.hp - threshold - 1)
		if b.phase != i-1 {
			t.Fatalf("the boss moved to phase %d above its threshold", b.phase)
		}
		w.damageBoss(1)
		if b.phase != i || b.emitters[0].Pattern().Name != b.def.Phases[i].Patterns[0] {
			t.Fatalf("the boss at %d hit points is in phase %d, want %d", b.hp, b.phase, i)
		}
	}
	w.damageBoss(b.hp)
	if w.outcome != cleared || !b.dead || w.score < b.def.Score {
		t.Fatalf("defeating the boss: outcome %d, score %d", w.outcome, w.score)
	}
}

func TestABigHitGoesStraightToTheLastPhaseItPasses(t *testing.T) {
	w := bossFight(t)
	b := w.boss
	last := len(b.def.Phases) - 1
	w.events = w.events[:0]
	w.damageBoss(b.hp - int(b.def.Phases[last].Below*float32(b.def.HP)))
	if b.phase != last {
		t.Fatalf("a hit past every phase's mark left the boss in phase %d, want %d", b.phase, last)
	}
	changes := 0
	for _, ev := range w.events {
		if ev == eventBossPhase {
			changes++
		}
	}
	if changes != 1 {
		t.Fatalf("the hit changed the boss's phase %d times, want once", changes)
	}
}

// TestNewWorldRefusesAPatternWithAFrameTheAtlasLacks checks that a pattern naming a frame the atlas does not paint fails as the run starts, for the ship's, an enemy's and the boss's patterns alike, rather than when the pattern is first used.
func TestNewWorldRefusesAPatternWithAFrameTheAtlasLacks(t *testing.T) {
	data, err := loadData()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{"ship": data.stage.Ship.Weapons[len(data.stage.Ship.Weapons)-1]}
	for _, e := range data.stage.Enemies {
		if e.Pattern != "" {
			names["enemy"] = e.Pattern
		}
	}
	phases := data.stage.Boss.Phases
	names["boss"] = phases[len(phases)-1].Patterns[0]
	for who, name := range names {
		t.Run(who, func(t *testing.T) {
			data, err := loadData()
			if err != nil {
				t.Fatal(err)
			}
			p, _ := data.patterns.Pattern(name)
			p.Frame = "unpainted"
			if _, err := data.newRun(viewport.Width, viewport.Height); err == nil || !strings.Contains(err.Error(), `"unpainted"`) {
				t.Fatalf("newRun with pattern %q naming an unpainted frame = %v, want an error naming it", name, err)
			}
		})
	}
}

func TestShotsDamageTheBossOnlyOnceItHasEntered(t *testing.T) {
	w := newTestWorld(t)
	shield(w)
	stepFor(w, w.stage.Boss.At+step)
	b := w.boss
	if b == nil || b.entered {
		t.Fatal("the boss did not start entering at its time")
	}
	w.ship.x = b.x
	for !b.entered {
		w.step(step, controls{Fire: true})
		if !b.entered && b.hp != b.def.HP {
			t.Fatal("the boss took damage before it entered")
		}
	}
	for range 120 {
		w.step(step, controls{Fire: true})
	}
	if b.hp >= b.def.HP {
		t.Fatal("two seconds of fire under the boss did not damage it")
	}
}

// TestRunsAreDeterministic plays the stage twice with the same pilot and checks that both runs play out the same, step for step, as replays and lockstep play need.
func TestRunsAreDeterministic(t *testing.T) {
	a, b := newTestWorld(t), newTestWorld(t)
	var p pilot
	for i := range 60 * 60 {
		a.step(step, p.decide(a))
		b.step(step, p.decide(b))
		if a.ship.x != b.ship.x || a.ship.y != b.ship.y || a.score != b.score || a.enemyShots.Len() != b.enemyShots.Len() {
			t.Fatalf("the runs differ at step %d", i)
		}
	}
}

// BenchmarkStep measures a fixed step of the boss fight, its busiest part, with the pilot playing.
func BenchmarkStep(b *testing.B) {
	w := newTestWorld(b)
	shield(w)
	var p pilot
	stepFor(w, w.stage.Boss.At+5*time.Second)
	b.ReportAllocs()
	for b.Loop() {
		w.step(step, p.decide(w))
	}
}

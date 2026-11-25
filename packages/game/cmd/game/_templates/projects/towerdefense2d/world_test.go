package main

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/ai"
)

// maxGameSteps bounds a played game: ten minutes of steps, well past the level's length.
const maxGameSteps = 10 * 60 * StepRate

func newTestWorld(t testing.TB) *World {
	t.Helper()
	d := MustLoadData()
	return NewWorld(d, makeAtlas(d))
}

func TestRouteFollowsTheRoadFromSpawnToGate(t *testing.T) {
	w := newTestWorld(t)
	route := w.Route()
	first, _ := w.CellAt(route[0].X, route[0].Y)
	last, _ := w.CellAt(route[len(route)-1].X, route[len(route)-1].Y)
	if l := w.Data.Level; l.Tile(first.Col, first.Row) != TileSpawn || l.Tile(last.Col, last.Row) != TileGate {
		t.Fatalf("route runs from %v to %v, want the spawn to the gate", first, last)
	}
	for i, p := range route {
		c, _ := w.CellAt(p.X, p.Y)
		if w.Map.FlagsAt(c.Col, c.Row)&flagRoad == 0 {
			t.Fatalf("route point %d, cell %v, is off the road", i, c)
		}
		if i > 0 {
			prev, _ := w.CellAt(route[i-1].X, route[i-1].Y)
			if d := abs(c.Col-prev.Col) + abs(c.Row-prev.Row); d != 1 {
				t.Fatalf("route jumps from %v to %v", prev, c)
			}
		}
	}
	if want := float32(len(route)-1) * w.Data.Level.TileSize; w.RouteLength() != want {
		t.Fatalf("route length %v, want %v", w.RouteLength(), want)
	}
}

func TestBuildUpgradeAndSell(t *testing.T) {
	w := newTestWorld(t)
	bolt, _ := w.Data.Towers.Tower("bolt")
	cell := ai.Cell{Col: 5, Row: 4}
	start := w.Money

	if _, err := w.Build(bolt, ai.Cell{Col: 1, Row: 1}); err == nil {
		t.Fatal("built a tower on the road")
	}
	if _, err := w.Build(bolt, ai.Cell{Col: 0, Row: 0}); err == nil {
		t.Fatal("built a tower on a rock")
	}
	tower, err := w.Build(bolt, cell)
	if err != nil {
		t.Fatal(err)
	}
	if w.Money != start-bolt.Cost || w.Buildable(cell) {
		t.Fatalf("after building: money %d, buildable %v; want %d and false", w.Money, w.Buildable(cell), start-bolt.Cost)
	}
	if _, err := w.Build(bolt, cell); err == nil {
		t.Fatal("built a second tower on the same cell")
	}

	damage := tower.Stats.Value(statDamage)
	if err := w.Upgrade(tower); err != nil {
		t.Fatal(err)
	}
	if tower.Level != 1 || tower.Stats.Value(statDamage) <= damage {
		t.Fatalf("after an upgrade: level %d, damage %v (was %v)", tower.Level, tower.Stats.Value(statDamage), damage)
	}
	spent := bolt.Cost + bolt.Upgrades[0].Cost
	if w.Money != start-spent || tower.Spent != spent {
		t.Fatalf("after an upgrade: money %d, spent %d; want %d and %d", w.Money, tower.Spent, start-spent, spent)
	}

	w.Money = 0
	if err := w.Upgrade(tower); err == nil || tower.Level != 1 {
		t.Fatal("upgraded with no money")
	}
	w.Money = 1000
	for tower.Level < len(bolt.Upgrades) {
		if err := w.Upgrade(tower); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Upgrade(tower); err == nil {
		t.Fatal("upgraded past the last upgrade")
	}

	before := w.Money
	value := w.SellValue(tower)
	if err := w.Sell(tower); err != nil {
		t.Fatal(err)
	}
	if w.Money != before+value || value != int(float64(tower.Spent)*w.Data.Towers.Refund) || !w.Buildable(cell) {
		t.Fatalf("after selling: money %d (+%d), buildable %v", w.Money, value, w.Buildable(cell))
	}
}

func TestEnemiesThatReachTheGateCostLives(t *testing.T) {
	w := newTestWorld(t)
	if !w.CallWave() || w.CallWave() {
		t.Fatal("the first wave was not called once")
	}
	first := w.Data.Level.Waves[0].Groups[0]
	def, _ := w.Data.Level.Enemy(first.Enemy)
	for range maxGameSteps {
		if w.Outcome != Playing || (!w.Spawning() && len(w.Enemies) == 0) {
			break
		}
		w.Step()
	}
	if want := w.Data.Level.Lives - first.Count*def.Leak; w.Lives != want {
		t.Fatalf("lives %d after an undefended first wave, want %d", w.Lives, want)
	}
	if w.Money != w.Data.Level.Money+w.Data.Level.Waves[0].Bonus {
		t.Fatalf("money %d, want the start and the wave's bonus", w.Money)
	}
	if !w.CanCallWave() {
		t.Fatal("the second wave cannot be called once the first is over")
	}
}

func TestFrostSlowsAndMortarSplashes(t *testing.T) {
	w := newTestWorld(t)
	frost, _ := w.Data.Towers.Tower("frost")
	w.Money = 1000
	if _, err := w.Build(frost, ai.Cell{Col: 3, Row: 2}); err != nil {
		t.Fatal(err)
	}
	w.CallWave()
	slowed := false
	for range 20 * StepRate {
		w.Step()
		for _, e := range w.Enemies {
			if e.Chilled() {
				slowed = true
				if e.Stats.Value(statSpeed) >= e.Stats.Base(statSpeed) {
					t.Fatalf("a chilled enemy walks at %v, its full speed", e.Stats.Value(statSpeed))
				}
			}
		}
	}
	if !slowed {
		t.Fatal("the frost tower slowed no enemy")
	}

	w.Reset()
	mortar, _ := w.Data.Towers.Tower("mortar")
	w.Money = 1000
	tower, err := w.Build(mortar, ai.Cell{Col: 5, Row: 4})
	if err != nil {
		t.Fatal(err)
	}
	// Two enemies side by side, both inside the shell's splash.
	sprout, _ := w.Data.Level.Enemy("sprout")
	w.addEnemy(sprout)
	w.addEnemy(sprout)
	w.Enemies[0].Progress, w.Enemies[1].Progress = 150, 156
	for range 3 * StepRate {
		w.Step()
		if len(w.Enemies) == 2 && w.Enemies[0].Health < w.Enemies[0].Stats.Value(statHealth) {
			break
		}
	}
	if len(w.Enemies) != 2 || w.Enemies[1].Health >= w.Enemies[1].Stats.Value(statHealth) {
		t.Fatalf("the mortar's first hit did not splash both enemies (tower aims %v)", tower.Aim)
	}
}

func TestPlanWinsAndThinPlanLoses(t *testing.T) {
	w := newTestWorld(t)
	playWorld(t, w, winningPlan, maxGameSteps)
	if w.Outcome != Won || w.Wave != w.Waves() || w.Lives <= 0 {
		t.Fatalf("the winning plan ended %v on wave %d with %d lives", w.Outcome, w.Wave, w.Lives)
	}
	if _, err := w.Build(&w.Data.Towers.Towers[0], ai.Cell{Col: 2, Row: 0}); err == nil {
		t.Fatal("built a tower after the game was won")
	}

	w.Reset()
	playWorld(t, w, thinPlan, maxGameSteps)
	if w.Outcome != Lost || w.Lives != 0 || len(w.Enemies) != 0 {
		t.Fatalf("the thin plan ended %v with %d lives and %d enemies", w.Outcome, w.Lives, len(w.Enemies))
	}
	steps := w.steps
	w.Step()
	if w.steps != steps {
		t.Fatal("a lost game kept running")
	}
}

func TestWorldIsDeterministic(t *testing.T) {
	a, b := newTestWorld(t), newTestWorld(t)
	playWorld(t, a, winningPlan, maxGameSteps)
	playWorld(t, b, winningPlan, maxGameSteps)
	if a.steps != b.steps || a.Money != b.Money || a.Lives != b.Lives || a.Outcome != b.Outcome {
		t.Fatalf("two games of the same plan differ: %d steps, %d money, %d lives, %v against %d, %d, %d, %v", a.steps, a.Money, a.Lives, a.Outcome, b.steps, b.Money, b.Lives, b.Outcome)
	}
}

func BenchmarkWorldStep(b *testing.B) {
	w := newTestWorld(b)
	w.Money = 10000
	for _, wave := range winningPlan {
		for _, o := range wave {
			if o.kind == "" {
				tower, _ := w.TowerAt(o.cell)
				_ = w.Upgrade(tower)
			} else {
				def, _ := w.Data.Towers.Tower(o.kind)
				_, _ = w.Build(def, o.cell)
			}
		}
	}
	for b.Loop() {
		if w.Outcome != Playing || between(w) {
			if w.Outcome != Playing || w.Wave == w.Waves() {
				money := w.Money
				towers := w.Towers
				w.Reset()
				w.Money, w.Towers = money, towers
			}
			w.CallWave()
		}
		w.Step()
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

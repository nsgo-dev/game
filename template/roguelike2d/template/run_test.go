package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/loot"
	"github.com/nsgo-dev/game/pkg/procgen"
)

// testSeeds is how many seeds the generation and balance tests play.
const testSeeds = 40

func startRun(t *testing.T, seed uint64) *run {
	t.Helper()
	r, err := newRun(mustLoadData(), seed, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestFloorsAreSound generates every floor of many seeds and checks what the rules rely on: the floor is one connected region, the exit is reachable, nothing stands on a wall or on another thing, monsters spawn away from the start, the last floor's exit is the relic with its guardian on it, and the same seed builds the same floor.
func TestFloorsAreSound(t *testing.T) {
	data := mustLoadData()
	for seed := uint64(1); seed <= testSeeds; seed++ {
		for i := range data.dungeon.Floors {
			f, err := generateFloor(data.dungeon, i, seed, loot.NewRoller(data.loot, floorStream(seed, i, "loot")))
			if err != nil {
				t.Fatalf("seed %d floor %d: %v", seed, i+1, err)
			}
			if !f.grid.Connected(procgen.IsFloor) {
				t.Errorf("seed %d floor %d is not one connected region", seed, i+1)
			}
			dist := distances(f.grid, f.start)
			if d := dist[f.exit.Y*f.grid.Width+f.exit.X]; d < 10 {
				t.Errorf("seed %d floor %d: the exit is %d steps from the start, want at least 10", seed, i+1, d)
			}
			used := map[cell]bool{f.start: true}
			for _, e := range f.foes() {
				at, kind, guard := f.at(e), f.foe(e).Kind, f.guards(e)
				if !f.walkable(at) || (used[at] && !guard) {
					t.Errorf("seed %d floor %d: the %s is on a wall or a taken cell %v", seed, i+1, kind, at)
				}
				used[at] = true
				if d := dist[at.Y*f.grid.Width+at.X]; !guard && d < minMonsterDistance {
					t.Errorf("seed %d floor %d: the %s spawned %d steps from the start", seed, i+1, kind, d)
				}
			}
			for _, e := range f.loot() {
				if at := f.at(e); !f.walkable(at) || at == f.start || at == f.exit {
					t.Errorf("seed %d floor %d: the %s is on a wall, the start or the exit", seed, i+1, f.lootOf(e).Item)
				}
			}
			if err := f.checkActors(data.dungeon); err != nil {
				t.Errorf("seed %d floor %d: %v", seed, i+1, err)
			}
			last := i == len(data.dungeon.Floors)-1
			if f.relic != last {
				t.Errorf("seed %d floor %d: relic is %v", seed, i+1, f.relic)
			}
			if g := data.dungeon.Floors[i].Guardian; g != "" {
				if e, ok := f.foeAt(f.exit); !ok || f.foe(e).Kind != g || !f.guards(e) {
					t.Errorf("seed %d floor %d: the exit is not guarded by the %s", seed, i+1, g)
				}
			}
			again, _ := generateFloor(data.dungeon, i, seed, loot.NewRoller(data.loot, floorStream(seed, i, "loot")))
			a, _ := again.actors.Snapshot()
			b, _ := f.actors.Snapshot()
			if !again.grid.Equal(f.grid) || !reflect.DeepEqual(a, b) {
				t.Errorf("seed %d floor %d: generating it again gave another floor", seed, i+1)
			}
		}
	}
}

// TestBotsWinAndLose plays many seeds with both bots: a run played to win usually wins and one that dives without healing usually dies, which is the balance data/dungeon.yaml is tuned for. Change the thresholds with the data.
func TestBotsWinAndLose(t *testing.T) {
	wins, deaths := 0, 0
	for seed := uint64(1); seed <= testSeeds; seed++ {
		if playRun(startRun(t, seed), botClear, 20000) == won {
			wins++
		}
		if playRun(startRun(t, seed), botDive, 20000) == lost {
			deaths++
		}
	}
	t.Logf("of %d seeds, the clearing bot won %d and the diving bot died in %d", testSeeds, wins, deaths)
	if wins < testSeeds/2 {
		t.Errorf("the clearing bot won %d of %d runs, want at least half", wins, testSeeds)
	}
	if deaths < testSeeds*3/4 {
		t.Errorf("the diving bot died in %d of %d runs, want at least three quarters", deaths, testSeeds)
	}
}

// emptyRun returns a run on the first floor of seed with every monster and item taken away, so a test places only what it needs.
func emptyRun(t *testing.T, seed uint64) *run {
	t.Helper()
	r := startRun(t, seed)
	r.floor.actors.Clear()
	return r
}

// place puts a monster of kind id next to the player, in the first open direction, and returns it.
func place(t *testing.T, r *run, id string) entities.Entity {
	t.Helper()
	def, ok := r.data.dungeon.monster(id)
	if !ok {
		t.Fatalf("no monster %q", id)
	}
	for _, st := range steps {
		at := cell{X: r.pos.X + st.X, Y: r.pos.Y + st.Y}
		if _, taken := r.floor.foeAt(at); r.floor.walkable(at) && !taken {
			e := r.floor.spawnFoe(def, at, false)
			r.look()
			return e
		}
	}
	t.Fatal("no open cell next to the player")
	return entities.Zero
}

func dirTo(r *run, c cell) cell { return cell{X: c.X - r.pos.X, Y: c.Y - r.pos.Y} }

func TestWallsTakeNoTurn(t *testing.T) {
	r := emptyRun(t, 3)
	// Stand on the first floor cell with a wall above it.
	g := r.floor.grid
	for i, v := range g.Cells {
		if c := (cell{X: i % g.Width, Y: i / g.Width}); procgen.IsFloor(v) && !r.floor.walkable(cell{X: c.X, Y: c.Y - 1}) {
			r.pos = c
			break
		}
	}
	if r.act(action{kind: actMove, dir: cell{Y: -1}}) || r.turns != 0 {
		t.Fatal("walking into a wall took a turn")
	}
}

func TestBumpingAttacksAndDefeatingGivesXP(t *testing.T) {
	r := emptyRun(t, 3)
	m := place(t, r, "moss-rat")
	at, def := r.floor.at(m), r.foeDef(r.floor.foe(m))
	for n := 0; n < 10 && len(r.floor.foes()) > 0; n++ {
		if !r.act(action{kind: actMove, dir: dirTo(r, at)}) {
			t.Fatal("attacking took no turn")
		}
	}
	if len(r.floor.foes()) != 0 || r.kills != 1 || r.xp != def.XP {
		t.Fatalf("after the fight: %d monsters, %d kills, %d xp; want 0, 1, %d", len(r.floor.foes()), r.kills, r.xp, def.XP)
	}
}

func TestDamageIsNeverBelowOne(t *testing.T) {
	r := emptyRun(t, 3)
	for n := 0; n < 100; n++ {
		if d := r.damage(1, 50); d != 1 {
			t.Fatalf("damage(1, 50) = %d, want 1", d)
		}
		if d := r.damage(10, 3); d < 7 || d > 9 {
			t.Fatalf("damage(10, 3) = %d, want 7 to 9", d)
		}
	}
}

func TestLevelsRaiseStats(t *testing.T) {
	r := emptyRun(t, 3)
	p := r.data.dungeon.Player
	health, attack := r.maxHealth(), r.attack()
	r.gainXP(p.LevelXP + 2*p.LevelXP)
	if r.level != 3 || r.xp != 0 {
		t.Fatalf("level %d with %d xp, want 3 with 0", r.level, r.xp)
	}
	if r.maxHealth() != health+2*p.LevelHealth || r.attack() != attack+2*p.LevelAttack {
		t.Fatalf("maxHealth %d attack %d, want %d and %d", r.maxHealth(), r.attack(), health+2*p.LevelHealth, attack+2*p.LevelAttack)
	}
}

func TestOnlyBetterGearIsEquipped(t *testing.T) {
	r := emptyRun(t, 3)
	base := r.attack()
	spike, _ := r.data.dungeon.item("iron-spike")
	pick, _ := r.data.dungeon.item("rusty-pick")
	r.take(spike, 1)
	if r.weapon != spike || r.attack() != base+2 {
		t.Fatalf("after the iron spike: weapon %s, attack %d; want iron-spike, %d", r.weapon.ID, r.attack(), base+2)
	}
	r.take(pick, 1)
	if r.weapon != spike || r.attack() != base+2 {
		t.Fatalf("the rusty pick replaced the iron spike")
	}
}

func TestTonicsWearOff(t *testing.T) {
	r := emptyRun(t, 3)
	base := r.attack()
	tonic, _ := r.data.dungeon.item("fury-tonic")
	e, _ := r.data.stats.Effect(tonic.Effect)
	r.take(tonic, 1)
	if r.attack() <= base {
		t.Fatalf("the fury tonic left attack at %d", r.attack())
	}
	for n := 0; n < e.Steps; n++ {
		if r.attack() <= base {
			t.Fatalf("the fury tonic wore off after %d turns, want %d", n, e.Steps)
		}
		r.act(action{kind: actWait})
	}
	if r.attack() != base {
		t.Fatalf("attack is %d after the tonic's %d turns, want %d", r.attack(), e.Steps, base)
	}
}

func TestQuaffHealsAndCoolsDown(t *testing.T) {
	r := emptyRun(t, 3)
	if r.act(action{kind: actAbility, ability: abilityQuaff}) || r.lastMessage() != "You are not hurt." {
		t.Fatalf("quaffing at full health: %q", r.lastMessage())
	}
	r.health = 5
	if !r.act(action{kind: actAbility, ability: abilityQuaff}) {
		t.Fatalf("quaffing while hurt was refused: %q", r.lastMessage())
	}
	if r.health < 5+r.data.dungeon.Player.DraughtHeal || r.draughts != r.data.dungeon.Player.Draughts-1 {
		t.Fatalf("after a draught: health %d, %d draughts", r.health, r.draughts)
	}
	r.health = 5
	if r.act(action{kind: actAbility, ability: abilityQuaff}) || r.lastMessage() != "You need a moment before another draught." {
		t.Fatalf("a second draught at once: %q", r.lastMessage())
	}
	r.draughts = 0
	for n := 0; n < 3; n++ {
		r.act(action{kind: actWait})
	}
	if r.act(action{kind: actAbility, ability: abilityQuaff}) || r.lastMessage() != "You have no draughts left." {
		t.Fatalf("quaffing with no draughts: %q", r.lastMessage())
	}
}

func TestCleaveHitsEveryNeighbour(t *testing.T) {
	r := emptyRun(t, 3)
	if r.act(action{kind: actAbility, ability: abilityCleave}) || r.lastMessage() != "There is nothing next to you to cleave." {
		t.Fatalf("cleaving nothing: %q", r.lastMessage())
	}
	a, b := place(t, r, "rust-crawler"), place(t, r, "rust-crawler")
	if !r.act(action{kind: actAbility, ability: abilityCleave}) {
		t.Fatalf("cleaving two crawlers was refused: %q", r.lastMessage())
	}
	full, _ := r.data.dungeon.monster("rust-crawler")
	for _, e := range []entities.Entity{a, b} {
		if foe, alive := entities.Get[Foe](r.floor.actors, e); alive && foe.Health == full.Health {
			t.Fatalf("the cleave missed a crawler: %d health left", foe.Health)
		}
	}
	if r.act(action{kind: actAbility, ability: abilityCleave}) {
		t.Fatal("cleaving again at once was allowed")
	}
}

func TestMonstersHuntAndStrike(t *testing.T) {
	r := emptyRun(t, 3)
	m := place(t, r, "gloom-bat")
	health := r.health
	r.act(action{kind: actWait})
	if !r.floor.foe(m).Hunting || r.health >= health {
		t.Fatalf("a bat next to the player: hunting %v, health %d of %d", r.floor.foe(m).Hunting, r.health, health)
	}
}

func TestDeathEndsTheRun(t *testing.T) {
	r := emptyRun(t, 3)
	place(t, r, "vault-warden")
	r.health = 1
	r.act(action{kind: actWait})
	if r.state != lost || r.killer != "Vault Warden" || r.health != 0 {
		t.Fatalf("state %v killer %q health %d, want lost to the Vault Warden at 0", r.state, r.killer, r.health)
	}
	if r.act(action{kind: actWait}) {
		t.Fatal("a lost run took another turn")
	}
}

// TestSameSeedSameRun plays a seed twice with the same bot and checks that the runs end the same way on the same turn, as replays and resumed runs rely on.
func TestSameSeedSameRun(t *testing.T) {
	a, b := startRun(t, 7), startRun(t, 7)
	playRun(a, botClear, 20000)
	playRun(b, botClear, 20000)
	if a.summarize() != b.summarize() {
		t.Fatalf("the same seed ended %+v and %+v", a.summarize(), b.summarize())
	}
}

// TestResumeRefusesBadSlots resumes a run saved mid-floor, then the same save with one thing wrong at a time: each must be refused, so the game starts a new run rather than play numbers the rules cannot handle.
func TestResumeRefusesBadSlots(t *testing.T) {
	r := startRun(t, winSeed)
	for n := 0; n < 30 && r.state == playing; n++ {
		r.act(nextAction(r, botClear))
	}
	good := r.save()
	if _, err := resumeRun(r.data, good, nil); err != nil {
		t.Fatalf("resuming a good save: %v", err)
	}
	wall := cell{}
	for i, v := range r.floor.grid.Cells {
		if !procgen.IsFloor(v) {
			wall = cell{X: i % r.floor.grid.Width, Y: i / r.floor.grid.Width}
			break
		}
	}
	firstFoe := func(s *runSave) *entities.ComponentSnapshot {
		for i := range s.Actors.Entities {
			for j := range s.Actors.Entities[i].Components {
				if c := &s.Actors.Entities[i].Components[j]; c.Type == codecFoe {
					return c
				}
			}
		}
		t.Fatal("the save holds no monster")
		return nil
	}
	for _, tc := range []struct {
		name string
		edit func(s *runSave)
	}{
		{"huge level", func(s *runSave) { s.Level = 1 << 60 }},
		{"negative xp", func(s *runSave) { s.XP = -1 }},
		{"xp past the level", func(s *runSave) { s.XP = s.Level * r.data.dungeon.Player.LevelXP }},
		{"negative draughts", func(s *runSave) { s.Draughts = -1 }},
		{"huge shards", func(s *runSave) { s.Shards = 1 << 40 }},
		{"negative turns", func(s *runSave) { s.Turns = -5 }},
		{"player in a wall", func(s *runSave) { s.Position = positionOf(wall) }},
		{"short seen", func(s *runSave) { s.Seen = s.Seen[:len(s.Seen)-1] }},
		{"unknown monster", func(s *runSave) { firstFoe(s).Data = json.RawMessage(`{"kind":"dragon","health":3}`) }},
		{"dead monster", func(s *runSave) { firstFoe(s).Data = json.RawMessage(`{"kind":"moss-rat","health":0}`) }},
		{"unknown component", func(s *runSave) { firstFoe(s).Type = "wings" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(good)
			var s runSave
			if err := json.Unmarshal(raw, &s); err != nil {
				t.Fatal(err)
			}
			tc.edit(&s)
			if _, err := resumeRun(r.data, s, nil); err == nil {
				t.Fatal("the save was resumed")
			}
		})
	}
}

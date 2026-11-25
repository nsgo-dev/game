package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/quest"
	"github.com/nsgo-dev/game/packages/game/save"
)

// newTestGame returns a game that has just started a new playthrough, saving to a temporary directory.
func newTestGame(t *testing.T) *Game {
	t.Helper()
	c, err := loadContent()
	if err != nil {
		t.Fatal(err)
	}
	g := newGame(c, save.NewFileStore(t.TempDir(), save.WithVersion(saveVersion)), "Test")
	g.pick("new")
	if g.mode != modeField {
		t.Fatalf("mode after New game = %v, want the field", g.mode)
	}
	return g
}

// fightFoe starts a battle with the foe called id on the field and attacks until it ends.
func fightFoe(t *testing.T, g *Game, id string) {
	t.Helper()
	g.enter(Place{Map: "field", At: Cell{1, 6}})
	for _, f := range g.foes {
		if f.ID == id {
			g.startBattle(f)
		}
	}
	if g.mode != modeBattle {
		t.Fatalf("no battle with %s", id)
	}
	for g.mode == modeBattle {
		g.pick("attack")
	}
}

func TestNewGameStartsWithAWoodenSwordAndCoins(t *testing.T) {
	g := newTestGame(t)
	p := &g.progress
	if g.hp != 30 || maxHP(p) != 30 || attack(p) != 5 || defense(p) != 1 {
		t.Fatalf("HP %d/%d, attack %d, defense %d; want 30/30, 5, 1", g.hp, maxHP(p), attack(p), defense(p))
	}
	if w, _ := g.gear.Equipped(slotWeapon); w != startWeapon {
		t.Fatalf("weapon %q, want %q", w, startWeapon)
	}
	if n := g.bag.Count(itemCoin); n != startCoins {
		t.Fatalf("coins %d, want %d", n, startCoins)
	}
}

func TestDefeatingASlimePaysCoins(t *testing.T) {
	g := newTestGame(t)
	fightFoe(t, g, "slime-road")
	if g.mode != modeField || !g.defeated["slime-road"] {
		t.Fatalf("mode %v, defeated %v", g.mode, g.defeated)
	}
	// Two blows of 5 beat its 6 HP, and it hits back once for 4 less 1.
	if g.hp != 27 || g.bag.Count(itemCoin) != startCoins+5 {
		t.Fatalf("HP %d, coins %d; want 27, %d", g.hp, g.bag.Count(itemCoin), startCoins+5)
	}
	g.enter(Place{Map: "field", At: Cell{1, 6}})
	for _, f := range g.foes {
		if f.ID == "slime-road" {
			t.Fatal("a defeated slime is back on the map")
		}
	}
}

// TestThornhideNeedsTheIronSword checks the game's balance: the starting sword loses to Thornhide, and the iron sword Bram sells wins.
func TestThornhideNeedsTheIronSword(t *testing.T) {
	g := newTestGame(t)
	fightFoe(t, g, "thornhide")
	if g.mode != modeLost || g.hp != 0 {
		t.Fatalf("with the wooden sword: mode %v, HP %d; want a loss", g.mode, g.hp)
	}

	g = newTestGame(t)
	if err := g.bag.AddAll("iron-sword", 1); err != nil {
		t.Fatal(err)
	}
	for i, s := range g.bag.Slots() {
		if s.Item == "iron-sword" {
			g.equip(i)
		}
	}
	if attack(&g.progress) != 9 {
		t.Fatalf("attack with the iron sword = %d, want 9", attack(&g.progress))
	}
	fightFoe(t, g, "thornhide")
	if g.mode != modeField || !g.defeated["thornhide"] || g.hp != 9 {
		t.Fatalf("with the iron sword: mode %v, HP %d; want a win on 9 HP", g.mode, g.hp)
	}
}

func TestBossesCannotBeFledFrom(t *testing.T) {
	g := newTestGame(t)
	g.enter(Place{Map: "field", At: Cell{1, 6}})
	for _, f := range g.foes {
		if f.ID == "thornhide" {
			g.startBattle(f)
		}
	}
	g.pick("flee")
	if g.mode != modeBattle {
		t.Fatal("fled from Thornhide")
	}
	g.battle.foe.kind.Boss = false
	g.pick("flee")
	if g.mode != modeField || g.grace != fleeGrace {
		t.Fatalf("mode %v, grace %v after fleeing", g.mode, g.grace)
	}
}

func TestQuestCountsWhatCameBeforeIt(t *testing.T) {
	g := newTestGame(t)
	if err := g.bag.AddAll("glowcap", 2); err != nil {
		t.Fatal(err)
	}
	g.defeated["thornhide"] = true
	g.onCommand(dialogueEvent("start", mainQuest))
	if got := g.quests.State(mainQuest); got != quest.Active {
		t.Fatalf("quest is %v", got)
	}
	gather, _ := g.quests.Objective(mainQuest, "gather")
	boss, _ := g.quests.Objective(mainQuest, "thornhide")
	if gather.Progress != 2 || boss.State != quest.ObjectiveDone {
		t.Fatalf("gather %d/%d, thornhide %v", gather.Progress, gather.Required, boss.State)
	}
	if g.questReady(mainQuest) {
		t.Fatal("ready with two glowcaps of three")
	}
	if got := g.questTracker(); got != "Gather glowcaps 2/3" {
		t.Fatalf("tracker %q", got)
	}
}

func TestPotionsHealUpToTheMaximum(t *testing.T) {
	g := newTestGame(t)
	if err := g.bag.AddAll(itemPotion, 1); err != nil {
		t.Fatal(err)
	}
	g.hp = 25
	g.drinkPotion()
	if g.hp != 30 || g.bag.Count(itemPotion) != 0 {
		t.Fatalf("HP %d, potions %d", g.hp, g.bag.Count(itemPotion))
	}
}

func TestPatrollingFoeWalksItsRouteAndBack(t *testing.T) {
	g := newTestGame(t)
	g.enter(Place{Map: "field", At: Cell{1, 6}})
	var slime *foeState
	for _, f := range g.foes {
		if f.ID == "slime-road" {
			slime = f
		}
	}
	seen := map[Cell]bool{}
	for range 30 * 60 {
		g.update(time.Second/60, intent{})
		seen[slime.at] = true
	}
	for x := 16; x <= 22; x++ {
		if !seen[Cell{x, 9}] {
			t.Fatalf("the slime never reached [%d, 9]; it went to %v", x, seen)
		}
	}
	if len(seen) != 7 {
		t.Fatalf("the slime left its route: %v", seen)
	}
}

func TestSaveAndLoadRestoreTheJourney(t *testing.T) {
	g := newTestGame(t)
	fightFoe(t, g, "slime-road")
	g.onCommand(dialogueEvent("start", mainQuest))
	g.pos, g.facing = Cell{20, 3}, dirUp
	g.arrive()
	want := g.snapshot()
	g.saveTo("slot2")
	if summary, ok := g.slotSummary("slot2"); !ok || summary != "Whisper Meadow · HP 27" {
		t.Fatalf("slot summary %q, %v", summary, ok)
	}

	g.startNewGame()
	if reflect.DeepEqual(g.snapshot(), want) {
		t.Fatal("a new game equals the saved one")
	}
	g.loadFrom("slot2")
	if got := g.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded %+v\nwant %+v", got, want)
	}
	if g.mode != modeField || g.mapID != "field" || g.foeAt(Cell{16, 9}) != nil {
		t.Fatalf("after loading: mode %v, map %s", g.mode, g.mapID)
	}
}

func TestRestoreRefusesASaveThatDoesNotFit(t *testing.T) {
	g := newTestGame(t)
	good := g.snapshot()
	for name, change := range map[string]func(*saveFile){
		"unknown map":   func(s *saveFile) { s.Map = "moon" },
		"in a tree":     func(s *saveFile) { s.At = Cell{0, 0} },
		"no facing":     func(s *saveFile) { s.Facing = dirNone },
		"unknown item":  func(s *saveFile) { s.Bag.Slots[0].Item = "dragon" },
		"unknown quest": func(s *saveFile) { s.Quests.Quests = append(s.Quests.Quests, quest.QuestRecord{ID: "nope"}) },
	} {
		s := g.snapshot()
		change(&s)
		if err := g.restore(s); err == nil {
			t.Errorf("%s: restore accepted the save", name)
		}
		if !reflect.DeepEqual(g.snapshot(), good) {
			t.Fatalf("%s: a refused save changed the game", name)
		}
	}
}

func TestABattleStartedMidStepFinishesTheStepAfterwards(t *testing.T) {
	g := newTestGame(t)
	g.enter(Place{Map: "field", At: Cell{33, 9}})
	var slime *foeState
	for _, f := range g.foes {
		if f.ID == "slime-river" {
			slime = f
		}
	}
	// The slime stands below the glowcap at [34, 9], walking up its patrol, and takes its next step one fixed step after the player starts stepping onto the glowcap.
	dt := time.Second / 60
	slime.at, slime.from, slime.stop, slime.wait = Cell{34, 10}, Cell{34, 10}, 1, dt+time.Millisecond
	g.update(dt, intent{dir: dirRight, turned: true})
	if g.pos != (Cell{34, 9}) || g.walk == 0 {
		t.Fatalf("the player is at %v, walk %v; want stepping onto [34, 9]", g.pos, g.walk)
	}
	g.update(dt, intent{dir: dirRight})
	if g.mode != modeBattle || g.battle.foe != slime {
		t.Fatalf("mode %v; want a battle with the slime", g.mode)
	}
	if g.bag.Count("glowcap") != 0 {
		t.Fatal("the glowcap was picked up before the battle")
	}
	for g.mode == modeBattle {
		g.pick("attack")
	}
	if g.mode != modeField || g.bag.Count("glowcap") != 1 || !g.taken["glowcap-river"] {
		t.Fatalf("after the battle: mode %v, glowcaps %d; want the glowcap picked up", g.mode, g.bag.Count("glowcap"))
	}
}

func TestBattlePotionWaitsForAWound(t *testing.T) {
	g := newTestGame(t)
	check(g.bag.AddAll(itemPotion, 1))
	g.enter(Place{Map: "field", At: Cell{1, 6}})
	g.startBattle(g.foes[0])
	if g.pick("potion"); g.bag.Count(itemPotion) != 1 {
		t.Fatal("a potion was drunk at full HP")
	}
	g.hp--
	if g.pick("potion"); g.bag.Count(itemPotion) != 0 {
		t.Fatal("no potion was drunk when wounded")
	}
}

func TestAmountChoosesTheArticle(t *testing.T) {
	for name, want := range map[string]string{"Potion": "a Potion", "Iron Sword": "an Iron Sword"} {
		if got := amount(name, 1); got != want {
			t.Errorf("amount(%q, 1) = %q, want %q", name, got, want)
		}
	}
	if got := amount("Coin", 3); got != "3 Coin" {
		t.Errorf("amount(Coin, 3) = %q", got)
	}
}

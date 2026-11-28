package savetest_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/ability"
	"github.com/nsgo-dev/game/pkg/dialogue"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/game/pkg/inventory"
	"github.com/nsgo-dev/game/pkg/quest"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/save/savetest"
	"github.com/nsgo-dev/game/pkg/stats"
)

type posComp struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func buildFullGame(t *testing.T) (
	*save.Game,
	*inventory.Container,
	*inventory.Equipment,
	*quest.Log,
	*stats.Sheet,
	*ability.Caster,
	*flow.Machine[string],
	*dialogue.Runner,
	*entities.World,
	*save.Persistence,
) {
	t.Helper()

	cat, err := inventory.Parse([]byte(`version: 1
items:
  - id: coin
    name: Coin
    maxStack: 99
  - id: sword
    name: Sword
    equip: weapon
`))
	if err != nil {
		t.Fatal(err)
	}
	bag, err := inventory.NewContainer(cat, 8)
	if err != nil {
		t.Fatal(err)
	}
	gear, err := inventory.NewEquipment(cat, inventory.EquipSlot{Name: "weapon", Kind: "weapon"})
	if err != nil {
		t.Fatal(err)
	}

	book, err := quest.Parse([]byte(`version: 1
quests:
  - id: hunt
    title: Hunt Slimes
    objectives:
      - id: slimes
        kind: defeat
        target: slime
        count: 3
`))
	if err != nil {
		t.Fatal(err)
	}
	quests, err := quest.NewLog(book)
	if err != nil {
		t.Fatal(err)
	}

	sheet, err := stats.NewSheet(
		stats.Def{Name: "hp", Base: 100},
		stats.Def{Name: "atk", Base: 10},
	)
	if err != nil {
		t.Fatal(err)
	}

	aset, err := ability.Parse([]byte(`version: 1
abilities:
  - id: dash
    action: dash
    cooldown: 1s
    charges: 2
    recharge: 2s
`))
	if err != nil {
		t.Fatal(err)
	}
	caster, err := ability.NewCaster(aset, 60)
	if err != nil {
		t.Fatal(err)
	}

	mach := flow.NewMachine("idle")
	mach.AddState("idle", flow.State{})
	mach.AddState("walk", flow.State{})

	script, err := dialogue.ParseYarn("title: Start\n---\nHello.\n===\n")
	if err != nil {
		t.Fatal(err)
	}
	runner := dialogue.NewRunner(script, dialogue.Options{})

	world := entities.NewWorld()
	if err := entities.RegisterCodec[posComp](world, "pos"); err != nil {
		t.Fatal(err)
	}

	pers := save.NewPersistence()

	g := save.NewGame()
	g.Register("bag", 1, bag.SavePart())
	g.Register("gear", 1, gear.SavePart())
	g.Register("quests", 1, quests.SavePart())
	g.Register("stats", 1, sheet.SavePart())
	g.Register("abilities", 1, caster.SavePart())
	g.Register("machine", 1, mach.SavePart())
	g.Register("dialogue", 1, runner.SavePart())
	g.Register("world", 1, world.SavePart())
	g.Register("persistence", 1, pers)

	return g, bag, gear, quests, sheet, caster, mach, runner, world, pers
}

func TestRoundTripAllBuiltInAdapters(t *testing.T) {
	g, bag, gear, quests, sheet, caster, mach, runner, world, pers := buildFullGame(t)

	// Mutate every subsystem away from defaults.
	if err := bag.AddAll("coin", 42); err != nil {
		t.Fatal(err)
	}
	if err := bag.AddAll("sword", 1); err != nil {
		t.Fatal(err)
	}
	if err := gear.Equip("weapon", bag, 1); err != nil {
		t.Fatal(err)
	}
	if err := quests.Start("hunt"); err != nil {
		t.Fatal(err)
	}
	if err := quests.SetProgress("hunt", "slimes", 2); err != nil {
		t.Fatal(err)
	}
	if err := sheet.SetBase("hp", 120); err != nil {
		t.Fatal(err)
	}
	if _, err := sheet.AddTimed("buff", stats.Modifier{Stat: "atk", Op: stats.OpAdd, Value: 5}, 30); err != nil {
		t.Fatal(err)
	}
	sheet.Step()

	if reason := caster.TryActivate("dash"); reason != ability.ReasonOK {
		t.Fatalf("TryActivate dash = %v", reason)
	}
	caster.Update(nil)

	mach.SetState("walk")
	mach.Update(250 * time.Millisecond)

	runner.SetVar("met_elder", true)
	if _, err := runner.Start("Start"); err != nil {
		t.Fatal(err)
	}

	e := world.Spawn()
	entities.Set(world, e, posComp{X: 12, Y: 34})

	pers.MarkDestroyed("scenes/cave.scene.yaml", "goblin_1")
	pers.SetSwitch("scenes/town.scene.yaml", "door_1", "A", true)

	// Round-trip into a brand-new game instance.
	savetest.RoundTrip(t, g, func() *save.Game {
		fresh, _, _, _, _, _, _, _, _, _ := buildFullGame(t)
		return fresh
	})

	// Also verify single-game RoundTrip (resets Resetter parts and reloads into g).
	savetest.RoundTrip(t, g)
}

type brokenNoRestorePart struct {
	count int
}

func (b *brokenNoRestorePart) SaveState() (any, error)         { return b.count, nil }
func (b *brokenNoRestorePart) LoadState(json.RawMessage) error { return nil } // BUG: forgets to restore!

type brokenResettablePart struct {
	count int
}

func (b *brokenResettablePart) SaveState() (any, error)         { return b.count, nil }
func (b *brokenResettablePart) LoadState(json.RawMessage) error { return nil } // BUG: forgets to restore!
func (b *brokenResettablePart) ResetState()                     { b.count = 0 }

func TestRoundTripCatchesPartThatDoesNotRestore(t *testing.T) {
	// Case 1: Fresh game factory catches a plain part whose LoadState is a no-op.
	src := save.NewGame()
	src.Register("broken", 1, &brokenNoRestorePart{count: 99})
	err := savetest.CheckRoundTrip(t.TempDir(), src, func() *save.Game {
		dst := save.NewGame()
		dst.Register("broken", 1, &brokenNoRestorePart{count: 0})
		return dst
	})
	if err == nil || !strings.Contains(err.Error(), `part "broken" did not round-trip`) {
		t.Fatalf("expected CheckRoundTrip to catch broken part, got: %v", err)
	}

	// Case 2: Single-game RoundTrip catches a Resetter part whose LoadState is a no-op.
	g := save.NewGame()
	g.Register("resettable", 1, &brokenResettablePart{count: 55})
	err = savetest.CheckRoundTrip(t.TempDir(), g)
	if err == nil || !strings.Contains(err.Error(), `part "resettable" did not round-trip`) {
		t.Fatalf("expected single-game CheckRoundTrip to catch broken Resetter part, got: %v", err)
	}

	// Case 3: Part that drops a field during load is caught.
	type pair struct {
		A int `json:"a"`
		B int `json:"b"`
	}
	drop := pair{A: 1, B: 2}
	gDrop := save.NewGame()
	gDrop.Register("pair", 1, save.FuncPart(
		func() (pair, error) { return drop, nil },
		func(v pair) error {
			drop = pair{A: v.A} // BUG: drops B!
			return nil
		},
	))
	err = savetest.CheckRoundTrip(t.TempDir(), gDrop)
	if err == nil || !strings.Contains(err.Error(), `part "pair" did not round-trip`) {
		t.Fatalf("expected CheckRoundTrip to catch dropped field, got: %v", err)
	}

	// Case 4: Mismatched part registrations between src and dst are caught.
	dstMismatch := save.NewGame()
	dstMismatch.Register("other", 1, &brokenNoRestorePart{})
	err = savetest.CheckRoundTripInto(t.TempDir(), src, dstMismatch)
	if err == nil || !strings.Contains(err.Error(), "part registration mismatch") {
		t.Fatalf("expected registration mismatch error, got: %v", err)
	}
}

package save_test

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/save"
)

func TestPersistenceOperationsAndRoundTrip(t *testing.T) {
	p := save.NewPersistence()
	p.MarkDestroyed("./scenes/cave.scene.yaml", "slime_1")
	p.SetPosition("scenes/cave.scene.yaml", "crate_1", 120, 48)
	p.SetRotation("scenes/cave.scene.yaml", "crate_1", 0.5)
	p.SetComponentField("scenes/cave.scene.yaml", "chest_1", "chest", "opened", true)
	p.SetComponentField("scenes/cave.scene.yaml", "chest_1", "chest", "gold", 25)
	p.SetComponentField("scenes/cave.scene.yaml", "chest_1", "chest", "weight", 3.5)
	p.SetSwitch("scenes/town.scene.yaml", "npc_elder", "A", true)
	p.SetSwitch("scenes/town.scene.yaml", "npc_elder", "B", false) // false is pruned
	p.SetVar("scenes/town.scene.yaml", "npc_elder", "talks", 4)

	if !p.IsDestroyed("scenes/cave.scene.yaml", "slime_1") {
		t.Fatal("slime_1 should be destroyed")
	}
	if x, y, ok := p.Position("scenes/cave.scene.yaml", "crate_1"); !ok || x != 120 || y != 48 {
		t.Fatalf("crate_1 position = (%v, %v, %v), want (120, 48, true)", x, y, ok)
	}
	if !p.Switch("scenes/town.scene.yaml", "npc_elder", "A") || p.Switch("scenes/town.scene.yaml", "npc_elder", "B") {
		t.Fatal("unexpected switch values")
	}
	if got := p.Scenes(); !slices.Equal(got, []string{"scenes/cave.scene.yaml", "scenes/town.scene.yaml"}) {
		t.Fatalf("Scenes = %v", got)
	}

	// Mutating returned copies must not alias internal state.
	st, ok := p.Object("scenes/cave.scene.yaml", "chest_1")
	if !ok {
		t.Fatal("chest_1 missing")
	}
	st.Components["chest"]["opened"] = false
	if v, _ := p.ComponentField("scenes/cave.scene.yaml", "chest_1", "chest", "opened"); v != true {
		t.Fatalf("Object returned aliased map: opened = %v", v)
	}

	// Save & Load through save.Game.
	store := save.NewFileStore(t.TempDir())
	g1 := save.NewGame()
	g1.Register("persistence", 1, p)
	if err := g1.Save(store, "slot1"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	p2 := save.NewPersistence()
	g2 := save.NewGame()
	g2.Register("persistence", 1, p2)
	if err := g2.Load(store, "slot1"); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Integer values inside Components and Vars must round-trip as int, floats as float64.
	gold, ok := p2.ComponentField("scenes/cave.scene.yaml", "chest_1", "chest", "gold")
	if !ok {
		t.Fatal("gold missing after load")
	}
	if gi, ok := gold.(int); !ok || gi != 25 {
		t.Fatalf("gold = %#v (%T), want int(25)", gold, gold)
	}
	weight, _ := p2.ComponentField("scenes/cave.scene.yaml", "chest_1", "chest", "weight")
	if wf, ok := weight.(float64); !ok || wf != 3.5 {
		t.Fatalf("weight = %#v (%T), want float64(3.5)", weight, weight)
	}
	talks, _ := p2.Var("scenes/town.scene.yaml", "npc_elder", "talks")
	if ti, ok := talks.(int); !ok || ti != 4 {
		t.Fatalf("talks = %#v (%T), want int(4)", talks, talks)
	}

	if !reflect.DeepEqual(p2.Snapshot(), p.Snapshot()) {
		t.Fatalf("snapshot mismatch:\ngot:  %+v\nwant: %+v", p2.Snapshot(), p.Snapshot())
	}

	// Deterministic JSON output.
	b1, err := json.Marshal(p.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	b2, err := json.Marshal(p2.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatalf("non-deterministic persistence JSON:\n%s\nvs\n%s", b1, b2)
	}

	// Reset when loading an older slot without "persistence".
	oldStore := save.NewFileStore(t.TempDir())
	var x int = 7
	oldGame := save.NewGame()
	oldGame.Register("x", 1, save.ValuePart(&x))
	if err := oldGame.Save(oldStore, "old"); err != nil {
		t.Fatal(err)
	}
	var x2 int
	newGame := save.NewGame()
	newGame.Register("x", 1, save.ValuePart(&x2))
	newGame.Register("persistence", 1, p2)
	if err := newGame.Load(oldStore, "old"); err != nil {
		t.Fatalf("Load old slot: %v", err)
	}
	if len(p2.Scenes()) != 0 {
		t.Fatalf("persistence not reset on missing part: %v", p2.Scenes())
	}
}

func TestPersistenceValidationAndPruning(t *testing.T) {
	p := save.NewPersistence()
	p.SetSwitch("s", "a", "k", true)
	p.SetSwitch("s", "a", "k", false)
	if len(p.Scenes()) != 0 {
		t.Fatalf("expected empty scene pruned, got %v", p.Scenes())
	}

	p.SetPosition3D("s", "a", 1, 2, 3)
	p.ClearObject("s", "a")
	if len(p.Scenes()) != 0 {
		t.Fatalf("ClearObject did not prune empty scene: %v", p.Scenes())
	}

	nan := float32(math.NaN())
	p.MarkDestroyed("s", "keep")
	err := p.Restore(save.PersistenceSnapshot{
		Scenes: map[string]map[string]save.ObjectState{
			"s": {"bad": {X: &nan}},
		},
	})
	if err == nil {
		t.Fatal("expected Restore to reject NaN")
	}
	if !p.IsDestroyed("s", "keep") {
		t.Fatal("failed Restore mutated existing Persistence")
	}
}

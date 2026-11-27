package sceneformat_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

type chestComp struct {
	Opened bool
	Gold   int
}

func TestPersistenceAcrossSceneLeaveAndReenter(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name: "chest",
		Fields: []sceneformat.Field{
			{Name: "opened", Kind: sceneformat.FieldBool, Default: false},
			{Name: "gold", Kind: sceneformat.FieldInt, Default: 50, Min: sceneformat.Float64(0)},
		},
		Spawn: func(w *entities.World, e entities.Entity, fields map[string]any) error {
			entities.Set(w, e, chestComp{
				Opened: fields["opened"].(bool),
				Gold:   fields["gold"].(int),
			})
			return nil
		},
	})
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "switch",
		Events: []sceneformat.Event{{Name: "pressed"}},
	})
	reg.MustRegister(sceneformat.ComponentType{
		Name:     "door",
		Handlers: []sceneformat.Handler{{Name: "open"}},
	})

	doc, err := sceneformat.ParseWith([]byte(`version: 1
layers:
  - name: world
    objects:
      - id: slime_1
        x: 10
        y: 20
      - id: crate_1
        x: 30
        y: 40
      - id: chest_1
        x: 50
        y: 60
        components:
          chest:
            opened: false
            gold: 50
      - id: btn_1
        x: 70
        y: 80
        components:
          switch: {}
      - id: door_1
        x: 90
        y: 80
        components:
          door: {}
connections:
  - from: btn_1
    event: switch.pressed
    to: door_1
    handler: door.open
`), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}

	const scenePath = "scenes/cave.scene.yaml"
	pers := save.NewPersistence()

	// First visit: spawn everything fresh.
	w1 := entities.NewWorld()
	spawned1, err := sceneformat.SpawnEntitiesWith(doc, w1, sceneformat.SpawnOptions{
		Components:  reg,
		Persistence: pers,
		ScenePath:   scenePath,
	})
	if err != nil {
		t.Fatalf("first SpawnEntitiesWith: %v", err)
	}
	if len(spawned1) != 5 {
		t.Fatalf("first visit spawned %d objects, want 5", len(spawned1))
	}

	// Player defeats slime_1, breaks btn_1 (which had a connection), pushes crate_1, and opens chest_1.
	w1.Destroy(spawned1["slime_1"])
	w1.Destroy(spawned1["btn_1"])
	w1.Hierarchy2D().SetLocal(spawned1["crate_1"], entities.Transform2D{
		X: 112, Y: 64, Rotation: 0.25, ScaleX: 1, ScaleY: 1,
	})
	pers.SetComponentField(scenePath, "chest_1", "chest", "opened", true)
	pers.SetComponentField(scenePath, "chest_1", "chest", "gold", 0)
	pers.SetSwitch(scenePath, "chest_1", "A", true)

	// Leave scene: capture destroyed & moved states.
	sceneformat.CapturePersistence(doc, w1, spawned1, pers, scenePath)

	// Save the game to a slot and load it into a brand-new Persistence (simulating quit & reload).
	store := save.NewFileStore(t.TempDir())
	saver := save.NewGame()
	saver.Register("persistence", 1, pers)
	if err := saver.Save(store, "slot1"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloadedPers := save.NewPersistence()
	loader := save.NewGame()
	loader.Register("persistence", 1, reloadedPers)
	if err := loader.Load(store, "slot1"); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Re-enter the scene with the reloaded Persistence.
	w2 := entities.NewWorld()
	spawned2, err := sceneformat.SpawnEntitiesWith(doc, w2, sceneformat.SpawnOptions{
		Components:  reg,
		Persistence: reloadedPers,
		ScenePath:   scenePath,
	})
	if err != nil {
		t.Fatalf("second SpawnEntitiesWith: %v", err)
	}
	if _, exists := spawned2["slime_1"]; exists {
		t.Fatal("destroyed slime_1 respawned on re-entry")
	}
	if _, exists := spawned2["btn_1"]; exists {
		t.Fatal("destroyed btn_1 respawned on re-entry")
	}
	if len(spawned2) != 3 {
		t.Fatalf("re-entry spawned %d objects, want 3", len(spawned2))
	}

	crateTr, ok := entities.Get[entities.Transform2D](w2, spawned2["crate_1"])
	if !ok || crateTr.X != 112 || crateTr.Y != 64 || crateTr.Rotation != 0.25 {
		t.Fatalf("crate_1 transform = %+v, want X=112 Y=64 Rot=0.25", crateTr)
	}
	chest, ok := entities.Get[chestComp](w2, spawned2["chest_1"])
	if !ok || !chest.Opened || chest.Gold != 0 {
		t.Fatalf("chest_1 = %+v, want {Opened:true Gold:0}", chest)
	}
	if !reloadedPers.Switch(scenePath, "chest_1", "A") {
		t.Fatal("chest_1 self switch A lost across save/load")
	}

	// Verify BuildWithPersistence also omits destroyed objects, prunes broken connections, and moves crate_1.
	built, err := sceneformat.BuildWithPersistence(doc, nil, reloadedPers, scenePath)
	if err != nil {
		t.Fatalf("BuildWithPersistence: %v", err)
	}
	if built.Objects["slime_1"] != nil || built.Objects["btn_1"] != nil {
		t.Fatal("BuildWithPersistence kept destroyed objects")
	}
	if len(built.Doc.Connections) != 0 {
		t.Fatalf("expected connection from destroyed btn_1 to be pruned, got %v", built.Doc.Connections)
	}
	if built.Objects["crate_1"].Doc.X != 112 || built.Objects["crate_1"].Doc.Y != 64 {
		t.Fatalf("built crate_1 at (%v,%v), want (112,64)", built.Objects["crate_1"].Doc.X, built.Objects["crate_1"].Doc.Y)
	}
	// Original doc must remain completely unmutated.
	if len(doc.Layers[0].Objects) != 5 || len(doc.Connections) != 1 || doc.Layers[0].Objects[1].X != 30 {
		t.Fatal("ApplyPersistence mutated original SceneDoc")
	}
}

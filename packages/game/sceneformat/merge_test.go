package sceneformat_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

func baseLevelScene() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version:       2,
		Name:          "Level 1",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Camera: &sceneformat.CameraDoc{
			X:    320,
			Y:    180,
			Zoom: sceneformat.Float32(1),
		},
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 10,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:   "knight",
						Name: "Knight",
						X:    400,
						Y:    222,
						Z:    10,
						Sprite: &sceneformat.SpriteRef{
							Atlas: "actors",
							Frame: "knight",
						},
					},
					{
						ID:     "door",
						Name:   "Door",
						Prefab: "door",
						X:      584,
						Y:      260,
						Components: map[string]any{
							"door": map[string]any{
								"locked": false,
							},
						},
					},
					{
						ID:     "slime_02",
						Prefab: "slime",
						X:      240,
						Y:      280,
						Components: map[string]any{
							"patrol": map[string]any{
								"from":  220,
								"to":    340,
								"speed": 24,
							},
						},
					},
				},
			},
		},
	}
}

func cloneDoc(t *testing.T, doc *sceneformat.SceneDoc) *sceneformat.SceneDoc {
	t.Helper()
	raw, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	cp, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return cp
}

func TestMergeMovesDifferentObjectsCleanly(t *testing.T) {
	base := baseLevelScene()
	ours := cloneDoc(t, base)
	theirs := cloneDoc(t, base)

	// Branch A moves knight and adds slime_03.
	ours.Layers[0].Objects[0].X = 420
	ours.Layers[0].Objects = append(ours.Layers[0].Objects, sceneformat.ObjectDoc{
		ID: "slime_03", X: 452, Y: 236,
	})

	// Branch B moves door and adds lever.
	theirs.Layers[0].Objects[1].X = 600
	theirs.Layers[0].Objects = append(theirs.Layers[0].Objects, sceneformat.ObjectDoc{
		ID: "lever", X: 360, Y: 238,
	})

	res, err := sceneformat.Merge(base, ours, theirs)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if res.HasConflicts() {
		t.Fatalf("expected clean merge, got conflicts: %+v", res.Conflicts)
	}
	if res.ConflictDoc("scenes/level_01.scene.yaml") != nil {
		t.Fatal("ConflictDoc must be nil on a clean merge")
	}

	objs := map[string]sceneformat.ObjectDoc{}
	for _, o := range res.Doc.Layers[0].Objects {
		objs[o.ID] = o
	}
	if objs["knight"].X != 420 {
		t.Errorf("knight.X = %v, want 420", objs["knight"].X)
	}
	if objs["door"].X != 600 {
		t.Errorf("door.X = %v, want 600", objs["door"].X)
	}
	if _, ok := objs["slime_03"]; !ok {
		t.Error("missing slime_03 from ours")
	}
	if _, ok := objs["lever"]; !ok {
		t.Error("missing lever from theirs")
	}

	// Merged result must encode canonically and round-trip cleanly.
	encoded, err := sceneformat.Encode(res.Doc)
	if err != nil {
		t.Fatalf("Encode merged: %v", err)
	}
	reparsed, err := sceneformat.Parse(encoded)
	if err != nil {
		t.Fatalf("Parse merged: %v", err)
	}
	encodedAgain, err := sceneformat.Encode(reparsed)
	if err != nil {
		t.Fatalf("re-Encode merged: %v", err)
	}
	if !bytes.Equal(encoded, encodedAgain) {
		t.Errorf("merged scene is not canonical:\n%s", encoded)
	}
}

func TestMergeDifferentFieldsOnSameObjectAndNestedMaps(t *testing.T) {
	base := baseLevelScene()
	ours := cloneDoc(t, base)
	theirs := cloneDoc(t, base)

	// Ours changes door's X, Z and adds open_sfx property.
	ours.Layers[0].Objects[1].X = 560
	ours.Layers[0].Objects[1].Z = 5
	ours.Layers[0].Objects[1].Properties = map[string]any{"open_sfx": "audio/door_creak.ogg"}
	// Ours changes slime_02 patrol speed.
	ours.Layers[0].Objects[2].Components["patrol"].(map[string]any)["speed"] = 30

	// Theirs changes door's Y, FlipX and locks it.
	theirs.Layers[0].Objects[1].Y = 270
	theirs.Layers[0].Objects[1].FlipX = true
	theirs.Layers[0].Objects[1].Components["door"].(map[string]any)["locked"] = true
	// Theirs changes slime_02 patrol end bound.
	theirs.Layers[0].Objects[2].Components["patrol"].(map[string]any)["to"] = 380

	res, err := sceneformat.Merge(base, ours, theirs)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if res.HasConflicts() {
		t.Fatalf("unexpected conflicts: %+v", res.Conflicts)
	}

	door := res.Doc.Layers[0].Objects[1]
	if door.X != 560 || door.Y != 270 || door.Z != 5 || !door.FlipX {
		t.Errorf("door fields = (x=%v,y=%v,z=%v,flipX=%v), want (560,270,5,true)", door.X, door.Y, door.Z, door.FlipX)
	}
	if door.Components["door"].(map[string]any)["locked"] != true {
		t.Errorf("door.components.door.locked = %v, want true", door.Components["door"])
	}
	if door.Properties["open_sfx"] != "audio/door_creak.ogg" {
		t.Errorf("door.properties.open_sfx = %v", door.Properties)
	}

	patrol := res.Doc.Layers[0].Objects[2].Components["patrol"].(map[string]any)
	if patrol["from"] != 220 || patrol["to"] != 380 || patrol["speed"] != 30 {
		t.Errorf("slime_02 patrol = %v, want from=220 to=380 speed=30", patrol)
	}
}

func TestMergeSameFieldConflictReportsBaseOursTheirs(t *testing.T) {
	base := baseLevelScene()
	ours := cloneDoc(t, base)
	theirs := cloneDoc(t, base)

	ours.Layers[0].Objects[1].X = 560
	theirs.Layers[0].Objects[1].X = 600

	res, err := sceneformat.Merge(base, ours, theirs)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !res.HasConflicts() {
		t.Fatal("expected conflict")
	}
	if len(res.Conflicts) != 1 {
		t.Fatalf("got %d conflicts, want 1: %+v", len(res.Conflicts), res.Conflicts)
	}
	c := res.Conflicts[0]
	if c.Kind != sceneformat.ConflictField || c.Scope != "object" || c.Layer != "actors" || c.Object != "door" || c.Field != "x" {
		t.Errorf("conflict = %+v", c)
	}
	if c.Base != 584 || c.Ours != 560 || c.Theirs != 600 {
		t.Errorf("values = (base=%#v, ours=%#v, theirs=%#v), want (584, 560, 600)", c.Base, c.Ours, c.Theirs)
	}

	// Conflict YAML must encode deterministically and round-trip cleanly.
	cdoc := res.ConflictDoc("scenes/level_01.scene.yaml")
	raw1, err := sceneformat.EncodeConflict(cdoc)
	if err != nil {
		t.Fatalf("EncodeConflict: %v", err)
	}
	parsed, err := sceneformat.ParseConflict(raw1)
	if err != nil {
		t.Fatalf("ParseConflict: %v", err)
	}
	raw2, err := sceneformat.EncodeConflict(parsed)
	if err != nil {
		t.Fatalf("re-EncodeConflict: %v", err)
	}
	if !bytes.Equal(raw1, raw2) {
		t.Errorf("conflict YAML changed on round-trip:\n--- first ---\n%s\n--- second ---\n%s", raw1, raw2)
	}
}

func TestMergeZeroAndFalseValuesPreservedInConflict(t *testing.T) {
	base := &sceneformat.SceneDoc{
		Version: 2,
		Layers: []sceneformat.LayerDoc{
			{
				Name: "main",
				Objects: []sceneformat.ObjectDoc{
					{ID: "hero", X: 0, Properties: map[string]any{"rate": 2.0}},
				},
			},
		},
	}
	ours := cloneDoc(t, base)
	theirs := cloneDoc(t, base)

	ours.Layers[0].Objects[0].X = 10
	theirs.Layers[0].Objects[0].X = 20
	ours.Layers[0].Objects[0].Properties["rate"] = 3.0
	theirs.Layers[0].Objects[0].Properties["rate"] = 4.0

	res, err := sceneformat.Merge(base, ours, theirs)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	raw, err := sceneformat.EncodeConflict(res.ConflictDoc("scenes/test.scene.yaml"))
	if err != nil {
		t.Fatalf("EncodeConflict: %v", err)
	}
	if !strings.Contains(string(raw), "base: 0\n") {
		t.Errorf("expected explicit base: 0 in conflict YAML:\n%s", raw)
	}
	if !strings.Contains(string(raw), "base: 2.0\n") {
		t.Errorf("expected float 2.0 preserved in conflict YAML:\n%s", raw)
	}
	parsed, err := sceneformat.ParseConflict(raw)
	if err != nil {
		t.Fatalf("ParseConflict: %v", err)
	}
	raw2, err := sceneformat.EncodeConflict(parsed)
	if err != nil {
		t.Fatalf("re-EncodeConflict: %v", err)
	}
	if !bytes.Equal(raw, raw2) {
		t.Errorf("round-trip mismatch:\n%s\nvs\n%s", raw, raw2)
	}
}

func TestMergeDeleteModifyConflictAndCleanDelete(t *testing.T) {
	base := baseLevelScene()
	ours := cloneDoc(t, base)
	theirs := cloneDoc(t, base)

	// Ours modifies slime_02 speed; theirs deletes slime_02.
	ours.Layers[0].Objects[2].Components["patrol"].(map[string]any)["speed"] = 30
	theirs.Layers[0].Objects = theirs.Layers[0].Objects[:2]

	res, err := sceneformat.Merge(base, ours, theirs)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(res.Conflicts) != 1 {
		t.Fatalf("got %d conflicts, want 1: %+v", len(res.Conflicts), res.Conflicts)
	}
	c := res.Conflicts[0]
	if c.Kind != sceneformat.ConflictDeleteModify || c.Object != "slime_02" || c.Theirs != nil || c.Ours == nil || c.Base == nil {
		t.Errorf("unexpected delete-modify conflict: %+v", c)
	}

	// Clean delete: ours leaves slime_02 alone and moves knight; theirs deletes slime_02.
	oursClean := cloneDoc(t, base)
	oursClean.Layers[0].Objects[0].X = 410
	resClean, err := sceneformat.Merge(base, oursClean, theirs)
	if err != nil {
		t.Fatalf("Merge clean delete: %v", err)
	}
	if resClean.HasConflicts() {
		t.Fatalf("expected clean delete merge, got %+v", resClean.Conflicts)
	}
	if len(resClean.Doc.Layers[0].Objects) != 2 {
		t.Fatalf("expected 2 surviving objects, got %d", len(resClean.Doc.Layers[0].Objects))
	}
}

func TestMergeReparentAndLayerMoveAcrossTree(t *testing.T) {
	base := &sceneformat.SceneDoc{
		Version: 2,
		Layers: []sceneformat.LayerDoc{
			{
				Name: "actors",
				Objects: []sceneformat.ObjectDoc{
					{
						ID: "hero",
						X:  10,
						Children: []sceneformat.ObjectDoc{
							{ID: "sword", X: 4},
						},
					},
					{ID: "chest", X: 100},
				},
			},
			{Name: "foreground", Order: 20},
		},
	}

	// Ours moves hero (with its child sword) to layer "foreground".
	// Theirs reparents sword under chest (in actors) and changes sword.Y to 8.
	// Because sword's parent reference only changed on theirs, both moves combine cleanly!
	ours := cloneDoc(t, base)
	hero := ours.Layers[0].Objects[0]
	ours.Layers[0].Objects = ours.Layers[0].Objects[1:]
	ours.Layers[1].Objects = []sceneformat.ObjectDoc{hero}

	theirs := cloneDoc(t, base)
	sword := theirs.Layers[0].Objects[0].Children[0]
	sword.Y = 8
	theirs.Layers[0].Objects[0].Children = nil
	theirs.Layers[0].Objects[1].Children = []sceneformat.ObjectDoc{sword}

	res, err := sceneformat.Merge(base, ours, theirs)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if res.HasConflicts() {
		t.Fatalf("expected clean reparent + layer move merge, got %+v", res.Conflicts)
	}

	// Verify hero is in foreground with no children, and sword is under chest in actors with Y=8.
	actors := res.Doc.Layers[0]
	fg := res.Doc.Layers[1]
	if len(fg.Objects) != 1 || fg.Objects[0].ID != "hero" || len(fg.Objects[0].Children) != 0 {
		t.Errorf("foreground = %+v", fg.Objects)
	}
	if len(actors.Objects) != 1 || actors.Objects[0].ID != "chest" || len(actors.Objects[0].Children) != 1 {
		t.Fatalf("actors = %+v", actors.Objects)
	}
	gotSword := actors.Objects[0].Children[0]
	if gotSword.ID != "sword" || gotSword.X != 4 || gotSword.Y != 8 {
		t.Errorf("sword = %+v, want id=sword x=4 y=8", gotSword)
	}
}

func TestMergeReparentCycleIsConflict(t *testing.T) {
	base := &sceneformat.SceneDoc{
		Version: 2,
		Layers: []sceneformat.LayerDoc{
			{
				Name: "main",
				Objects: []sceneformat.ObjectDoc{
					{ID: "a"},
					{ID: "b"},
				},
			},
		},
	}
	// Ours moves b under a.
	ours := cloneDoc(t, base)
	ours.Layers[0].Objects = []sceneformat.ObjectDoc{
		{ID: "a", Children: []sceneformat.ObjectDoc{{ID: "b"}}},
	}
	// Theirs moves a under b.
	theirs := cloneDoc(t, base)
	theirs.Layers[0].Objects = []sceneformat.ObjectDoc{
		{ID: "b", Children: []sceneformat.ObjectDoc{{ID: "a"}}},
	}

	res, err := sceneformat.Merge(base, ours, theirs)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !res.HasConflicts() {
		t.Fatal("expected cycle conflict")
	}
	foundMove := false
	for _, c := range res.Conflicts {
		if c.Kind == sceneformat.ConflictMove && strings.Contains(c.Message, "cycle") {
			foundMove = true
		}
	}
	if !foundMove {
		t.Errorf("expected ConflictMove cycle error, got %+v", res.Conflicts)
	}
}

func TestMergeConnectionsByIdentity(t *testing.T) {
	base := &sceneformat.SceneDoc{
		Version: 2,
		Layers: []sceneformat.LayerDoc{
			{
				Name: "main",
				Objects: []sceneformat.ObjectDoc{
					{ID: "btn1"},
					{ID: "btn2"},
					{ID: "door"},
					{ID: "lamp"},
				},
			},
		},
		Connections: []sceneformat.ConnectionDoc{
			{
				From: "btn1", Event: "pressed", To: "door", Handler: "open",
				Args: map[string]any{"speed": 1, "sound": "click"},
			},
		},
	}
	ours := cloneDoc(t, base)
	theirs := cloneDoc(t, base)

	// Ours flips Once on existing connection and adds btn2 -> lamp.
	ours.Connections[0].Once = true
	ours.Connections[0].Args["speed"] = 2
	ours.Connections = append(ours.Connections, sceneformat.ConnectionDoc{
		From: "btn2", Event: "pressed", To: "lamp", Handler: "toggle",
	})

	// Theirs changes sound arg on existing connection and adds btn1 -> lamp.
	theirs.Connections[0].Args["sound"] = "creak"
	theirs.Connections = append(theirs.Connections, sceneformat.ConnectionDoc{
		From: "btn1", Event: "pressed", To: "lamp", Handler: "on",
	})

	res, err := sceneformat.Merge(base, ours, theirs)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if res.HasConflicts() {
		t.Fatalf("unexpected conflicts: %+v", res.Conflicts)
	}
	if len(res.Doc.Connections) != 3 {
		t.Fatalf("got %d connections, want 3: %+v", len(res.Doc.Connections), res.Doc.Connections)
	}
	c0 := res.Doc.Connections[0]
	if !c0.Once || c0.Args["speed"] != 2 || c0.Args["sound"] != "creak" {
		t.Errorf("merged connection[0] = %+v", c0)
	}
}

func TestMergeValidationFailureBecomesConflict(t *testing.T) {
	t.Run("mesh_and_model_combined", func(t *testing.T) {
		base := &sceneformat.SceneDoc{
			Version: 2,
			Layers:  []sceneformat.LayerDoc{{Name: "main", Objects: []sceneformat.ObjectDoc{{ID: "prop"}}}},
		}
		ours := cloneDoc(t, base)
		theirs := cloneDoc(t, base)
		ours.Layers[0].Objects[0].Mesh = sceneformat.MeshBox
		theirs.Layers[0].Objects[0].Model = "models/crate.glb"

		res, err := sceneformat.Merge(base, ours, theirs)
		if err != nil {
			t.Fatalf("Merge: %v", err)
		}
		if len(res.Conflicts) != 1 || res.Conflicts[0].Kind != sceneformat.ConflictValidation {
			t.Fatalf("expected single ConflictValidation, got %+v", res.Conflicts)
		}
	})

	t.Run("connection_to_deleted_object", func(t *testing.T) {
		base := &sceneformat.SceneDoc{
			Version: 2,
			Layers: []sceneformat.LayerDoc{
				{Name: "main", Objects: []sceneformat.ObjectDoc{{ID: "btn"}, {ID: "door"}}},
			},
		}
		ours := cloneDoc(t, base)
		theirs := cloneDoc(t, base)
		// Ours deletes door cleanly; theirs connects btn to door.
		ours.Layers[0].Objects = ours.Layers[0].Objects[:1]
		theirs.Connections = []sceneformat.ConnectionDoc{
			{From: "btn", Event: "pressed", To: "door", Handler: "open"},
		}

		res, err := sceneformat.Merge(base, ours, theirs)
		if err != nil {
			t.Fatalf("Merge: %v", err)
		}
		if len(res.Conflicts) != 1 || res.Conflicts[0].Kind != sceneformat.ConflictValidation {
			t.Fatalf("expected ConflictValidation for dangling connection, got %+v", res.Conflicts)
		}
	})

	t.Run("sheared_sprite_from_combined_transforms", func(t *testing.T) {
		base := &sceneformat.SceneDoc{
			Version: 2,
			Layers: []sceneformat.LayerDoc{
				{
					Name: "main",
					Objects: []sceneformat.ObjectDoc{
						{
							ID: "parent",
							Children: []sceneformat.ObjectDoc{
								{
									ID:     "child",
									Sprite: &sceneformat.SpriteRef{Atlas: "a", Frame: "f"},
								},
							},
						},
					},
				},
			},
		}
		ours := cloneDoc(t, base)
		theirs := cloneDoc(t, base)
		ours.Layers[0].Objects[0].ScaleX = sceneformat.Float32(2)
		theirs.Layers[0].Objects[0].Children[0].Rotation = 0.5

		res, err := sceneformat.Merge(base, ours, theirs)
		if err != nil {
			t.Fatalf("Merge: %v", err)
		}
		if len(res.Conflicts) != 1 || res.Conflicts[0].Kind != sceneformat.ConflictValidation {
			t.Fatalf("expected ConflictValidation for sheared sprite, got %+v", res.Conflicts)
		}
	})
}

func TestMergeNilBaseAddAdd(t *testing.T) {
	ours := &sceneformat.SceneDoc{
		Version: 2,
		Name:    "Arena",
		Layers: []sceneformat.LayerDoc{
			{Name: "main", Objects: []sceneformat.ObjectDoc{{ID: "hero", X: 10}}},
		},
	}
	theirs := &sceneformat.SceneDoc{
		Version: 2,
		Name:    "Arena",
		Layers: []sceneformat.LayerDoc{
			{Name: "main", Objects: []sceneformat.ObjectDoc{{ID: "boss", X: 200}}},
		},
	}

	res, err := sceneformat.Merge(nil, ours, theirs)
	if err != nil {
		t.Fatalf("Merge(nil, ...): %v", err)
	}
	if res.HasConflicts() {
		t.Fatalf("expected clean add/add merge of disjoint objects, got %+v", res.Conflicts)
	}
	if len(res.Doc.Layers[0].Objects) != 2 {
		t.Fatalf("expected 2 objects, got %+v", res.Doc.Layers[0].Objects)
	}

	// Conflicting same-ID object with nil base produces ConflictAddAdd.
	theirsConflict := cloneDoc(t, theirs)
	theirsConflict.Layers[0].Objects = append(theirsConflict.Layers[0].Objects, sceneformat.ObjectDoc{ID: "hero", X: 99})
	res2, err := sceneformat.Merge(nil, ours, theirsConflict)
	if err != nil {
		t.Fatalf("Merge(nil, ...): %v", err)
	}
	if len(res2.Conflicts) != 1 || res2.Conflicts[0].Kind != sceneformat.ConflictAddAdd || res2.Conflicts[0].Object != "hero" {
		t.Fatalf("expected ConflictAddAdd on hero, got %+v", res2.Conflicts)
	}
}

func TestMergeTilemapsCameraAndOverrides(t *testing.T) {
	base := &sceneformat.SceneDoc{
		Version:  2,
		Inherits: "scenes/base.scene.yaml",
		Camera: &sceneformat.CameraDoc{
			X: 10, Y: 20, Zoom: sceneformat.Float32(1),
			Bounds: &sceneformat.BoundsDoc{X: 0, Y: 0, Width: 100, Height: 100},
		},
		Tilemaps: map[string]*sceneformat.TilemapDoc{
			"ground": {Source: "maps/v1.tmj"},
		},
		LayerOverrides: map[string]sceneformat.LayerOverride{
			"bg": {Order: sceneformat.Int(1)},
		},
		Overrides: map[string]sceneformat.ObjectOverride{
			"hero": {X: sceneformat.Float32(10)},
		},
	}
	ours := cloneDoc(t, base)
	theirs := cloneDoc(t, base)

	ours.AmbientColor = "#101824cc"
	ours.Camera.X = 50
	ours.Camera.Bounds.Width = 200
	ours.Tilemaps["ground"].Source = "maps/v2.tmj"
	bgOurs := ours.LayerOverrides["bg"]
	bgOurs.Visible = sceneformat.Bool(false)
	ours.LayerOverrides["bg"] = bgOurs
	heroOurs := ours.Overrides["hero"]
	heroOurs.Y = sceneformat.Float32(30)
	ours.Overrides["hero"] = heroOurs

	theirs.Camera.Y = 60
	theirs.Camera.Bounds.Height = 150
	theirs.Tilemaps["walls"] = &sceneformat.TilemapDoc{Source: "maps/walls.tmj"}
	bgTheirs := theirs.LayerOverrides["bg"]
	bgTheirs.Order = sceneformat.Int(2)
	theirs.LayerOverrides["bg"] = bgTheirs
	heroTheirs := theirs.Overrides["hero"]
	heroTheirs.Z = sceneformat.Int(5)
	theirs.Overrides["hero"] = heroTheirs

	res, err := sceneformat.Merge(base, ours, theirs)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if res.HasConflicts() {
		t.Fatalf("unexpected conflicts: %+v", res.Conflicts)
	}
	if res.Doc.AmbientColor != "#101824cc" {
		t.Errorf("ambientColor = %q, want #101824cc", res.Doc.AmbientColor)
	}
	if res.Doc.Camera.X != 50 || res.Doc.Camera.Y != 60 || res.Doc.Camera.Bounds.Width != 200 || res.Doc.Camera.Bounds.Height != 150 {
		t.Errorf("camera = %+v, bounds = %+v", res.Doc.Camera, res.Doc.Camera.Bounds)
	}
	if res.Doc.Tilemaps["ground"].Source != "maps/v2.tmj" || res.Doc.Tilemaps["walls"].Source != "maps/walls.tmj" {
		t.Errorf("tilemaps = %+v", res.Doc.Tilemaps)
	}
	if *res.Doc.LayerOverrides["bg"].Order != 2 || *res.Doc.LayerOverrides["bg"].Visible != false {
		t.Errorf("layerOverrides = %+v", res.Doc.LayerOverrides["bg"])
	}
	if *res.Doc.Overrides["hero"].Y != 30 || *res.Doc.Overrides["hero"].Z != 5 {
		t.Errorf("overrides = %+v", res.Doc.Overrides["hero"])
	}
}

func TestMergeInvalidInputsReturnError(t *testing.T) {
	valid := baseLevelScene()
	invalid := &sceneformat.SceneDoc{Version: 99}
	if _, err := sceneformat.Merge(nil, nil, valid); err == nil {
		t.Error("expected error for nil ours")
	}
	if _, err := sceneformat.Merge(nil, valid, nil); err == nil {
		t.Error("expected error for nil theirs")
	}
	if _, err := sceneformat.Merge(invalid, valid, valid); err == nil {
		t.Error("expected error for invalid base")
	}
	if _, err := sceneformat.Merge(valid, invalid, valid); err == nil {
		t.Error("expected error for invalid ours")
	}
	if _, err := sceneformat.Merge(valid, valid, invalid); err == nil {
		t.Error("expected error for invalid theirs")
	}
}

// TestMergeCoversAllDocumentFields guards against new fields being added to
// SceneDoc or its sub-structs without updating Merge.
func TestMergeCoversAllDocumentFields(t *testing.T) {
	wantCounts := map[reflect.Type]int{
		reflect.TypeFor[sceneformat.SceneDoc]():       15,
		reflect.TypeFor[sceneformat.CameraDoc]():      5,
		reflect.TypeFor[sceneformat.BoundsDoc]():      4,
		reflect.TypeFor[sceneformat.LayerDoc]():       12,
		reflect.TypeFor[sceneformat.TilemapDoc]():     11,
		reflect.TypeFor[sceneformat.TileLayerDoc]():   4,
		reflect.TypeFor[sceneformat.ObjectDoc]():      32,
		reflect.TypeFor[sceneformat.SpriteRef]():      2,
		reflect.TypeFor[sceneformat.LayerOverride]():  10,
		reflect.TypeFor[sceneformat.ObjectOverride](): 28,
		reflect.TypeFor[sceneformat.ConnectionDoc]():  6,
	}
	for typ, want := range wantCounts {
		if got := typ.NumField(); got != want {
			t.Errorf("%s has %d fields, Merge was written for %d; update merge.go and this test", typ.Name(), got, want)
		}
	}
}

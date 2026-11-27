package sceneformat_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// projectScenes reads the scene files of testdata/project.
var projectScenes = sceneformat.FSScenes{FS: os.DirFS("testdata/project")}

// projectResolver resolves the actor atlases and the scene files of testdata/project.
type projectResolver struct {
	sceneformat.AtlasMap
	sceneformat.FSScenes
}

var projectAssets = projectResolver{AtlasMap: caveResolver, FSScenes: projectScenes}

// memScenes is a SceneResolver over YAML sources held in memory, counting loads per path.
type memScenes struct {
	files map[string]string
	loads map[string]int
}

func newMemScenes(files map[string]string) *memScenes {
	return &memScenes{files: files, loads: make(map[string]int)}
}

func (m *memScenes) Scene(path string) (*sceneformat.SceneDoc, error) {
	src, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("no scene %q", path)
	}
	m.loads[path]++
	return sceneformat.Parse([]byte(src))
}

func readProject(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/project/" + path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return data
}

func TestLevelInstancesEnemyTenTimesAndRoundTrips(t *testing.T) {
	raw := readProject(t, "level.scene.yaml")
	doc, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !doc.RefersToScenes() {
		t.Fatal("RefersToScenes = false for a level of instances")
	}
	objs := doc.Layers[0].Objects
	if len(objs) != 10 {
		t.Fatalf("level holds %d objects, want 10", len(objs))
	}
	for i := range objs {
		if objs[i].Scene != "scenes/enemy.scene.yaml" || len(objs[i].Overrides) != 1 || objs[i].Children != nil {
			t.Fatalf("object %q = scene %q, %d overrides, %d children; want the enemy scene with one override and no children", objs[i].ID, objs[i].Scene, len(objs[i].Overrides), len(objs[i].Children))
		}
	}
	encoded, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(encoded, raw) {
		t.Fatalf("round trip changed the level:\n%s\n--- want ---\n%s", encoded, raw)
	}

	built, err := sceneformat.Build(doc, projectAssets)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if again, err := sceneformat.Encode(doc); err != nil || !bytes.Equal(again, raw) {
		t.Fatalf("Build changed the level document (err %v):\n%s", err, again)
	}
	if len(built.Objects) != 30 {
		t.Fatalf("built %d objects, want 30 (10 instances of a body and a torch)", len(built.Objects))
	}
	for i := 1; i <= 10; i++ {
		id := "enemy_" + strconv.Itoa(i)
		root := built.Object(id)
		if root == nil || len(root.Children) != 1 || root.Children[0].ID != id+"/body" {
			t.Fatalf("instance %q does not hold its body as its child: %+v", id, root)
		}
		body := root.Children[0]
		if len(body.Children) != 1 || body.Children[0].ID != id+"/torch" {
			t.Fatalf("%s/body children = %v, want [%s/torch]", id, objectIDs(body.Children), id)
		}
		if body.Sprite.X != float32(i*32) || body.Sprite.Y != 40 {
			t.Fatalf("%s/body sprite at (%v, %v), want (%v, 40)", id, body.Sprite.X, body.Sprite.Y, i*32)
		}
	}

	// Each override changes its own instance and no other.
	check := func(id string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", id, got, want)
		}
	}
	body := func(i int) *sceneformat.BuiltObject { return built.Object("enemy_" + strconv.Itoa(i) + "/body") }
	torch := func(i int) *sceneformat.BuiltObject { return built.Object("enemy_" + strconv.Itoa(i) + "/torch") }
	check("enemy_1/body alpha", body(1).Sprite.Alpha, float32(0.5))
	check("enemy_2/body alpha", body(2).Sprite.Alpha, float32(1))
	check("enemy_2/body health", body(2).Doc.Properties["health"], 1)
	check("enemy_3/body health", body(3).Doc.Properties["health"], 3)
	check("enemy_2/body speed", body(2).Doc.Properties["speed"], 40)
	check("enemy_3/torch x", torch(3).Sprite.X, float32(3*32-6))
	check("enemy_4/torch x", torch(4).Sprite.X, float32(4*32+6))
	check("enemy_4/body flipX", body(4).Sprite.FlipX, true)
	check("enemy_5/body scaleX", body(5).Sprite.ScaleX, float32(2))
	check("enemy_5/torch x", torch(5).Sprite.X, float32(5*32+12))
	check("enemy_6/torch z", torch(6).Sprite.Z, -1)
	check("enemy_7/body name", body(7).Doc.Name, "Captain")
	check("enemy_8/body frame", body(8).Doc.Sprite.Frame, "torch")
	check("enemy_9/torch alpha", torch(9).Sprite.Alpha, float32(0))
	check("enemy_10/body rotation", body(10).Sprite.Rotation, float32(0.5))
	check("enemy_1/body frame", body(1).Doc.Sprite.Frame, "hero")
}

func TestResolveLoadsEachSceneOnce(t *testing.T) {
	scenes := newMemScenes(map[string]string{
		"enemy.yaml": string(readProject(t, "scenes/enemy.scene.yaml")),
	})
	doc, err := sceneformat.Parse(bytes.ReplaceAll(readProject(t, "level.scene.yaml"), []byte("scenes/enemy.scene.yaml"), []byte("enemy.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := sceneformat.Resolve(doc, scenes)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if scenes.loads["enemy.yaml"] != 1 {
		t.Fatalf("enemy.yaml loaded %d times, want 1", scenes.loads["enemy.yaml"])
	}
	if resolved.RefersToScenes() {
		t.Fatal("a resolved scene still refers to scenes")
	}
	for _, o := range resolved.Layers[0].Objects {
		if o.Scene != "" || o.Overrides != nil {
			t.Fatalf("resolved object %q keeps scene %q and overrides %v", o.ID, o.Scene, o.Overrides)
		}
	}
	// The resolved copy shares nothing with what the resolver returned: changing it leaves a fresh resolve unchanged.
	resolved.Layers[0].Objects[0].Children[0].Properties["health"] = 99
	again, err := sceneformat.Resolve(doc, scenes)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Layers[0].Objects[0].Children[0].Properties["health"]; got != 3 {
		t.Fatalf("health after changing an earlier result = %v, want 3", got)
	}
}

func TestInheritedSceneChangesOnePropertyAndKeepsTheRest(t *testing.T) {
	raw := readProject(t, "scenes/fast_enemy.scene.yaml")
	doc, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if encoded, err := sceneformat.Encode(doc); err != nil || !bytes.Equal(encoded, raw) {
		t.Fatalf("round trip changed the inherited scene (err %v):\n%s", err, encoded)
	}
	fast, err := sceneformat.Resolve(doc, projectScenes)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	base, err := projectScenes.Scene("scenes/enemy.scene.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := base.Clone()
	want.Layers[0].Objects[0].Properties["speed"] = 80
	if !reflect.DeepEqual(fast, want) {
		t.Fatalf("inherited scene = %+v\nwant the base with speed 80: %+v", fast, want)
	}
	if base.Layers[0].Objects[0].Properties["speed"] != 40 {
		t.Fatal("resolving the inherited scene changed its base")
	}

	built, err := sceneformat.Build(doc, projectAssets)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := built.Object("body").Doc.Properties; got["speed"] != 80 || got["health"] != 3 {
		t.Fatalf("built body properties = %v, want speed 80 and health 3", got)
	}
	if built.Object("torch") == nil {
		t.Fatal("built inherited scene lost the base's torch")
	}
}

func TestInheritanceMergesSceneFieldsLayersAndObjects(t *testing.T) {
	scenes := newMemScenes(map[string]string{
		"base.yaml": `version: 2
name: Base
virtualWidth: 320
virtualHeight: 180
backgroundColor: "#000000"
tilemaps:
  floor: {tileWidth: 16, tileHeight: 16, width: 1, height: 1, tileset: cave_tiles, data: [1]}
layers:
  - name: ground
    tilemap: {ref: floor}
  - name: actors
    order: 5
    objects:
      - id: hero
        x: 10
        alpha: 0.5
        sprite: {atlas: actors, frame: hero}
        components:
          collider: {w: 12}
          health: {max: 3}
  - name: hud
    space: screen
`,
	})
	doc, err := sceneformat.Parse([]byte(`version: 2
inherits: base.yaml
name: Derived
layerOverrides:
  hud:
    visible: false
  actors:
    order: 0
overrides:
  hero:
    x: 0
    components:
      collider: {w: 20}
layers:
  - name: actors
    objects:
      - id: torch
        sprite: {atlas: actors, frame: torch}
  - name: fx
    order: 50
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := sceneformat.Resolve(doc, scenes)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Name != "Derived" || got.VirtualWidth != 320 || got.BackgroundColor != "#000000" || got.Tilemaps["floor"] == nil {
		t.Fatalf("scene fields = %q %v %q %v, want the derived name over the base's other fields", got.Name, got.VirtualWidth, got.BackgroundColor, got.Tilemaps)
	}
	var names []string
	for _, l := range got.Layers {
		names = append(names, l.Name)
	}
	if want := []string{"ground", "actors", "hud", "fx"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("layers = %v, want %v", names, want)
	}
	actors := got.Layers[1]
	if actors.Order != 0 || len(actors.Objects) != 2 || actors.Objects[1].ID != "torch" {
		t.Fatalf("actors = order %d, objects %d; want order 0 with the derived torch after the hero", actors.Order, len(actors.Objects))
	}
	hero := actors.Objects[0]
	if hero.X != 0 || hero.EffectiveAlpha() != 0.5 {
		t.Fatalf("hero x %v alpha %v, want x 0 (overridden to zero) and alpha 0.5 (kept)", hero.X, hero.EffectiveAlpha())
	}
	if !reflect.DeepEqual(hero.Components, map[string]any{"collider": map[string]any{"w": 20}, "health": map[string]any{"max": 3}}) {
		t.Fatalf("hero components = %v, want collider replaced and health kept", hero.Components)
	}
	if got.Layers[2].IsVisible() || got.Layers[2].EffectiveSpace() != sceneformat.SpaceScreen {
		t.Fatalf("hud = visible %v space %q, want hidden and still screen", got.Layers[2].IsVisible(), got.Layers[2].EffectiveSpace())
	}
	if _, err := sceneformat.Build(doc, struct {
		sceneformat.AtlasMap
		*memScenes
	}{caveResolver, scenes}); err != nil {
		t.Fatalf("Build: %v", err)
	}
}

func TestInstancesNestAndOverrideThroughPaths(t *testing.T) {
	scenes := newMemScenes(map[string]string{
		"gun.yaml": "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: barrel\n        x: 4\n",
		"ship.yaml": `version: 2
layers:
  - name: a
    objects:
      - id: hull
      - id: gun
        x: 10
        scene: gun.yaml
        overrides:
          barrel: {x: 5}
  - name: b
    objects:
      - id: flag
`,
		"fleet.yaml": `version: 2
inherits: ship.yaml
overrides:
  gun/barrel: {x: 7}
`,
	})
	doc, err := sceneformat.Parse([]byte(`version: 2
layers:
  - name: ships
    objects:
      - id: s1
        scene: ship.yaml
        overrides:
          gun/barrel: {x: 6}
        children:
          - id: s1_wake
      - id: s2
        scene: ship.yaml
      - id: s3
        scene: fleet.yaml
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := sceneformat.Resolve(doc, scenes)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	objs := got.Layers[0].Objects
	ids := func(o sceneformat.ObjectDoc) []string {
		var out []string
		for _, c := range o.Children {
			out = append(out, c.ID)
		}
		return out
	}
	if want := []string{"s1/hull", "s1/gun", "s1/flag", "s1_wake"}; !reflect.DeepEqual(ids(objs[0]), want) {
		t.Fatalf("s1 children = %v, want the ship's objects of every layer, then its own child: %v", ids(objs[0]), want)
	}
	barrel := func(o sceneformat.ObjectDoc) sceneformat.ObjectDoc { return o.Children[1].Children[0] }
	for i, want := range []struct {
		id string
		x  float32
	}{{"s1/gun/barrel", 6}, {"s2/gun/barrel", 5}, {"s3/gun/barrel", 7}} {
		if b := barrel(objs[i]); b.ID != want.id || b.X != want.x {
			t.Errorf("barrel %d = %q at x %v, want %q at x %v", i, b.ID, b.X, want.id, want.x)
		}
	}
}

func TestResolveRefusesCycles(t *testing.T) {
	files := map[string]string{
		"self.yaml":      "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: me\n        scene: self.yaml\n",
		"a.yaml":         "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: b\n        scene: b.yaml\n",
		"b.yaml":         "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: a\n        scene: a.yaml\n",
		"base.yaml":      "version: 2\ninherits: derived.yaml\n",
		"derived.yaml":   "version: 2\ninherits: base.yaml\n",
		"parent.yaml":    "version: 2\ninherits: child.yaml\n",
		"child.yaml":     "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: p\n        children:\n          - id: q\n            scene: parent.yaml\n",
		"inherits.yaml":  "version: 2\ninherits: inherits.yaml\n",
		"leaf.yaml":      "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: leaf\n",
		"diamond_a.yaml": "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: l1\n        scene: leaf.yaml\n      - id: l2\n        scene: leaf.yaml\n",
	}
	cases := []struct {
		root, want string
	}{
		{"self.yaml", `scene cycle "self.yaml" -> "self.yaml"`},
		{"a.yaml", `scene cycle "a.yaml" -> "b.yaml" -> "a.yaml"`},
		{"base.yaml", `scene cycle "base.yaml" -> "derived.yaml" -> "base.yaml"`},
		{"parent.yaml", `scene cycle "parent.yaml" -> "child.yaml" -> "parent.yaml"`},
		{"inherits.yaml", `scene cycle "inherits.yaml" -> "inherits.yaml"`},
	}
	for _, tc := range cases {
		t.Run(tc.root, func(t *testing.T) {
			doc, err := sceneformat.Parse([]byte("version: 2\nlayers:\n  - name: a\n    objects:\n      - id: root\n        scene: " + tc.root + "\n"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = sceneformat.Resolve(doc, newMemScenes(files))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Resolve error = %v, want %q", err, tc.want)
			}
			if _, err := sceneformat.Build(doc, struct {
				sceneformat.AtlasMap
				*memScenes
			}{caveResolver, newMemScenes(files)}); err == nil || !strings.Contains(err.Error(), "scene cycle") {
				t.Fatalf("Build error = %v, want a scene cycle", err)
			}
		})
	}
	// A scene instanced twice along separate branches is no cycle.
	doc, err := sceneformat.Parse([]byte("version: 2\nlayers:\n  - name: a\n    objects:\n      - id: d1\n        scene: diamond_a.yaml\n      - id: d2\n        scene: diamond_a.yaml\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sceneformat.Resolve(doc, newMemScenes(files)); err != nil {
		t.Fatalf("Resolve of a diamond: %v", err)
	}
}

func TestResolveRefusesBadReferences(t *testing.T) {
	files := map[string]string{
		"enemy.yaml":   string(readProject(t, "scenes/enemy.scene.yaml")),
		"tiled.yaml":   "version: 2\nlayers:\n  - name: floor\n    tilemap: {tileWidth: 1, tileHeight: 1, width: 1, height: 1, tileset: t, data: [1]}\n",
		"broken.yaml":  "version: 2\nlayers:\n  - name: a\n    bogus: 1\n",
		"base.yaml":    "version: 2\nlayers:\n  - name: actors\n    objects:\n      - id: hero\n",
		"v1.yaml":      "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: body\n",
		"sheared.yaml": "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: s\n        rotation: 0.5\n        sprite: {atlas: actors, frame: hero}\n",
	}
	cases := []struct {
		name, yaml, want string
	}{
		{"unknown_override", "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: e\n        scene: enemy.yaml\n        overrides:\n          head: {x: 1}\n", `object "e" overrides "head", which scene "enemy.yaml" does not hold`},
		{"override_of_prefixed_id", "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: e\n        scene: enemy.yaml\n        overrides:\n          e/body: {x: 1}\n", `overrides "e/body"`},
		{"missing_file", "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: e\n        scene: nowhere.yaml\n", `load scene "nowhere.yaml"`},
		{"invalid_file", "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: e\n        scene: broken.yaml\n", `load scene "broken.yaml"`},
		{"tilemap", "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: e\n        scene: tiled.yaml\n", `whose layer "floor" holds a tilemap`},
		{"id_collision", "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: e\n        scene: enemy.yaml\n      - id: e/body\n", `duplicate object id "e/body"`},
		{"shear_through_instance", "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: e\n        scaleX: 2\n        scene: sheared.yaml\n", "sheared"},
		{"extended_layer_sets_order", "version: 2\ninherits: base.yaml\nlayers:\n  - name: actors\n    order: 3\n", `layer "actors" extends the layer of base scene "base.yaml" and may only add objects`},
		{"unknown_layer_override", "version: 2\ninherits: base.yaml\nlayerOverrides:\n  hud: {visible: false}\n", `layerOverrides names layer "hud", which base scene "base.yaml" does not hold`},
		{"unknown_object_override", "version: 2\ninherits: base.yaml\noverrides:\n  villain: {x: 1}\n", `overrides names object "villain", which base scene "base.yaml" does not hold`},
		{"override_own_object", "version: 2\ninherits: base.yaml\noverrides:\n  sidekick: {x: 1}\nlayers:\n  - name: actors\n    objects:\n      - id: sidekick\n", `overrides names object "sidekick"`},
		{"derived_duplicates_base_id", "version: 2\ninherits: base.yaml\nlayers:\n  - name: actors\n    objects:\n      - id: hero\n", `duplicate object id "hero"`},
		{"unknown_tilemap_after_merge", "version: 2\ninherits: base.yaml\nlayers:\n  - name: floor\n    tilemap: {ref: nowhere}\n", `references unknown tilemap "nowhere"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := sceneformat.Parse([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			_, err = sceneformat.Resolve(doc, newMemScenes(files))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Resolve error = %v, want %q", err, tc.want)
			}
		})
	}

	// A version 1 scene may be instanced and inherited; only the referring scene needs version 2.
	doc, err := sceneformat.Parse([]byte("version: 2\ninherits: v1.yaml\noverrides:\n  body: {x: 2}\n"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := sceneformat.Resolve(doc, newMemScenes(files))
	if err != nil || got.Layers[0].Objects[0].X != 2 || got.Version != 2 {
		t.Fatalf("inheriting a version 1 scene = %+v, %v", got, err)
	}
}

func TestReferencesNeedAResolverAndResolvedScenes(t *testing.T) {
	doc, err := sceneformat.Parse(readProject(t, "level.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sceneformat.Build(doc, caveResolver); err == nil || !strings.Contains(err.Error(), "requires a SceneResolver") {
		t.Fatalf("Build without a SceneResolver: %v", err)
	}
	if _, err := sceneformat.Resolve(doc, nil); err == nil || !strings.Contains(err.Error(), "requires a SceneResolver") {
		t.Fatalf("Resolve without a SceneResolver: %v", err)
	}
	if _, err := sceneformat.SpawnEntities(doc, entities.NewWorld()); err == nil || !strings.Contains(err.Error(), "Resolve it first") {
		t.Fatalf("SpawnEntities of an unresolved scene: %v", err)
	}
	if _, err := sceneformat.BuildNodes3D(doc, scene3d.NewScene().Root()); err == nil || !strings.Contains(err.Error(), "Resolve it first") {
		t.Fatalf("BuildNodes3D of an unresolved scene: %v", err)
	}
	resolved, err := sceneformat.Resolve(doc, projectScenes)
	if err != nil {
		t.Fatal(err)
	}
	w := entities.NewWorld()
	spawned, err := sceneformat.SpawnEntities(resolved, w)
	if err != nil || len(spawned) != 30 {
		t.Fatalf("SpawnEntities of the resolved level = %d entities, %v; want 30", len(spawned), err)
	}
	if p, ok := w.Hierarchy2D().Parent(spawned["enemy_3/torch"]); !ok || p != spawned["enemy_3/body"] {
		t.Fatalf("enemy_3/torch parent = %v, want enemy_3/body", p)
	}
	nodes, err := sceneformat.BuildNodes3D(resolved, scene3d.NewScene().Root())
	if err != nil || nodes.Objects["enemy_10/body"] == nil {
		t.Fatalf("BuildNodes3D of the resolved level: %v", err)
	}
}

func TestResolveRefusesRunawayScenes(t *testing.T) {
	// Each scene instances the next 64 times, so the third level would hold 64^3 objects.
	files := map[string]string{"s3.yaml": "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: leaf\n"}
	for _, level := range []string{"s2", "s1", "s0"} {
		next := map[string]string{"s2": "s3", "s1": "s2", "s0": "s1"}[level]
		var b strings.Builder
		b.WriteString("version: 2\nlayers:\n  - name: a\n    objects:\n")
		for i := 0; i < 64; i++ {
			fmt.Fprintf(&b, "      - id: i%d\n        scene: %s.yaml\n", i, next)
		}
		files[level+".yaml"] = b.String()
	}
	doc, err := sceneformat.Parse([]byte(files["s0.yaml"]))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sceneformat.Resolve(doc, newMemScenes(files)); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("Resolve of a runaway scene: %v", err)
	}

	// A resolver that invents a new scene for every path cannot nest without bound.
	if _, err := sceneformat.Resolve(&sceneformat.SceneDoc{Version: 2, Inherits: "d0"}, deepScenes{}); err == nil || !strings.Contains(err.Error(), "deeper than") {
		t.Fatalf("Resolve of an endless chain: %v", err)
	}
}

// deepScenes returns, for path "dN", a scene that inherits "dN+1".
type deepScenes struct{}

func (deepScenes) Scene(path string) (*sceneformat.SceneDoc, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(path, "d"))
	if err != nil {
		return nil, err
	}
	return &sceneformat.SceneDoc{Version: 2, Inherits: "d" + strconv.Itoa(n+1)}, nil
}

func TestValidateSceneReferences(t *testing.T) {
	cases := []struct {
		name, yaml, want string
	}{
		{"v1_scene", "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: e\n        scene: e.yaml\n", "need version 2"},
		{"v1_inherits", "version: 1\ninherits: e.yaml\n", "need version 2"},
		{"v1_overrides", "version: 1\noverrides:\n  a: {x: 1}\n", "need version 2"},
		{"future_version", "version: 5\n", "unsupported version 5"},
		{"absolute_scene", "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: e\n        scene: /e.yaml\n", "must be a slash-separated path"},
		{"dotdot_inherits", "version: 2\ninherits: ../e.yaml\n", "must be a slash-separated path"},
		{"overrides_without_scene", "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: e\n        overrides:\n          b: {x: 1}\n", "overrides require scene"},
		{"overrides_without_inherits", "version: 2\noverrides:\n  b: {x: 1}\n", "require inherits"},
		{"layer_overrides_without_inherits", "version: 2\nlayerOverrides:\n  b: {visible: false}\n", "require inherits"},
		{"empty_override_key", "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: e\n        scene: e.yaml\n        overrides:\n          \"\": {x: 1}\n", "empty object id"},
		{"bad_override_alpha", "version: 2\nlayers:\n  - name: a\n    objects:\n      - id: e\n        scene: e.yaml\n        overrides:\n          b: {alpha: 2}\n", `overrides "b" alpha 2 must be in [0, 1]`},
		{"bad_override_sprite", "version: 2\ninherits: e.yaml\noverrides:\n  b: {sprite: {atlas: a}}\n", "sprite requires both atlas and frame"},
		{"bad_override_material", "version: 2\ninherits: e.yaml\noverrides:\n  b: {material: /m.yaml}\n", "material"},
		{"unknown_override_field", "version: 2\ninherits: e.yaml\noverrides:\n  b: {id: c}\n", `unknown field "id"`},
		{"override_children", "version: 2\ninherits: e.yaml\noverrides:\n  b: {children: []}\n", `unknown field "children"`},
		{"bad_layer_override_space", "version: 2\ninherits: e.yaml\nlayerOverrides:\n  a: {space: sky}\n", "invalid space"},
		{"empty_layer_override", "version: 2\ninherits: e.yaml\nlayerOverrides:\n  \"\": {visible: false}\n", "empty layer name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v, want %q", err, tc.want)
			}
		})
	}
	// A derived layer may name a tilemap of its base, which only Resolve can check.
	if _, err := sceneformat.Parse([]byte("version: 2\ninherits: e.yaml\nlayers:\n  - name: floor\n    tilemap: {ref: base_floor}\n")); err != nil {
		t.Fatalf("Parse of a derived tilemap ref: %v", err)
	}
}

func TestObjectOverrideCoversEveryObjectField(t *testing.T) {
	notOverridable := map[string]bool{"id": true, "children": true, "scene": true, "overrides": true}
	ot := reflect.TypeFor[sceneformat.ObjectDoc]()
	vt := reflect.TypeFor[sceneformat.ObjectOverride]()
	overrideKeys := make(map[string]bool)
	for i := range vt.NumField() {
		f := vt.Field(i)
		overrideKeys[strings.Split(f.Tag.Get("yaml"), ",")[0]] = true
	}
	for i := range ot.NumField() {
		f := ot.Field(i)
		key := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if notOverridable[key] != !overrideKeys[key] {
			t.Errorf("ObjectDoc field %q: overridable %v, in ObjectOverride %v", key, !notOverridable[key], overrideKeys[key])
		}
	}
	if len(overrideKeys) != ot.NumField()-len(notOverridable) {
		t.Errorf("ObjectOverride has %d fields, want %d", len(overrideKeys), ot.NumField()-len(notOverridable))
	}

	// Setting every override field changes every overridable field of a zero object. The 3D look fields exclude each other (a primitive, a model narrowed to a node or to a mesh, or a grid map), so each variant sets one valid combination of them, and together they set them all.
	variants := []map[string]string{
		{"mesh": "box", "model": "", "modelNode": "", "modelMesh": "", "gridMap": ""},
		{"mesh": "", "model": "m.glb", "modelNode": "n", "modelMesh": "", "gridMap": ""},
		{"mesh": "", "model": "m.glb", "modelNode": "", "modelMesh": "m", "gridMap": ""},
		{"mesh": "", "model": "", "modelNode": "", "modelMesh": "", "gridMap": "levels/keep.gridmap.yaml"},
	}
	set := make(map[string]bool)
	for _, look := range variants {
		var ov sceneformat.ObjectOverride
		ovv := reflect.ValueOf(&ov).Elem()
		for i := range ovv.NumField() {
			f := ovv.Field(i)
			key := strings.Split(vt.Field(i).Tag.Get("yaml"), ",")[0]
			switch f.Kind() {
			case reflect.Pointer:
				p := reflect.New(f.Type().Elem())
				switch e := p.Elem(); e.Kind() {
				case reflect.String:
					v, ok := look[key]
					if !ok {
						v = "v"
					}
					if v == "" {
						continue
					}
					e.SetString(v)
				case reflect.Float32:
					e.SetFloat(0.25)
				case reflect.Int:
					e.SetInt(2)
				case reflect.Bool:
					e.SetBool(true)
				case reflect.Struct:
					switch e.Type() {
					case reflect.TypeFor[sceneformat.SpriteRef]():
						e.Set(reflect.ValueOf(sceneformat.SpriteRef{Atlas: "a", Frame: "f"}))
					case reflect.TypeFor[sceneformat.OccluderDoc]():
						sz := [3]float32{2, 2, 2}
						e.Set(reflect.ValueOf(sceneformat.OccluderDoc{Mode: sceneformat.OccluderBox, Size: &sz}))
					case reflect.TypeFor[sceneformat.ParticlesRef]():
						e.Set(reflect.ValueOf(sceneformat.ParticlesRef{File: "fx/sparks.particles.yaml", Atlas: "fx"}))
					case reflect.TypeFor[sceneformat.LightDoc]():
						e.Set(reflect.ValueOf(sceneformat.LightDoc{Kind: sceneformat.LightPoint, Range: 5}))
					case reflect.TypeFor[sceneformat.Camera3DDoc]():
						e.Set(reflect.ValueOf(sceneformat.Camera3DDoc{Projection: sceneformat.ProjectionPerspective, FOV: sceneformat.Float32(60)}))
					case reflect.TypeFor[sceneformat.ReflectionProbeDoc]():
						sz := [3]float32{8, 4, 8}
						e.Set(reflect.ValueOf(sceneformat.ReflectionProbeDoc{Size: &sz, Cubemap: "probes/room.hdr"}))
					case reflect.TypeFor[sceneformat.LightProbeGridDoc]():
						sz := [3]float32{8, 4, 8}
						cnt := [3]int{2, 2, 2}
						e.Set(reflect.ValueOf(sceneformat.LightProbeGridDoc{Size: &sz, Counts: &cnt, Data: "probes/room.probes.yaml"}))
					default:
						t.Fatalf("unhandled struct override field type %v", e.Type())
					}
				}
				f.Set(p)
			case reflect.Map:
				f.Set(reflect.ValueOf(map[string]any{"k": 1}))
			}
		}
		doc, err := sceneformat.Resolve(&sceneformat.SceneDoc{
			Version:   sceneformat.CurrentVersion,
			Inherits:  "base.yaml",
			Overrides: map[string]sceneformat.ObjectOverride{"o": ov},
		}, newMemScenes(map[string]string{"base.yaml": "version: 4\nlayers:\n  - name: a\n    objects:\n      - id: o\n"}))
		if err != nil {
			t.Fatalf("Resolve with look %v: %v", look, err)
		}
		got := reflect.ValueOf(doc.Layers[0].Objects[0])
		for i := range got.NumField() {
			if !got.Field(i).IsZero() {
				set[strings.Split(ot.Field(i).Tag.Get("yaml"), ",")[0]] = true
			}
		}
	}
	for i := range ot.NumField() {
		key := strings.Split(ot.Field(i).Tag.Get("yaml"), ",")[0]
		if !notOverridable[key] && !set[key] {
			t.Errorf("override left %q unset", key)
		}
	}
}

func TestCloneIsDeep(t *testing.T) {
	doc, err := sceneformat.Parse([]byte(`version: 2
inherits: base.yaml
camera: {zoom: 2, bounds: {width: 1, height: 1}}
tilemaps:
  t: {tileWidth: 1, tileHeight: 1, width: 1, height: 1, tileset: s, data: [1]}
layerOverrides:
  a: {order: 1, visible: false}
overrides:
  o: {x: 1, properties: {list: [1, {k: v}]}}
layers:
  - name: b
    visible: true
    objects:
      - id: e
        alpha: 0.5
        scaleX: 2
        sprite: {atlas: a, frame: f}
        scene: e.yaml
        overrides:
          x: {name: n}
        properties:
          nested: {list: [1, 2]}
        children:
          - id: c
`))
	if err != nil {
		t.Fatal(err)
	}
	before, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	c := doc.Clone()
	if !reflect.DeepEqual(c, doc) {
		t.Fatal("Clone differs from its source")
	}
	*c.Camera.Zoom = 3
	c.Camera.Bounds.Width = 9
	c.Tilemaps["t"].Data[0] = 2
	*c.LayerOverrides["a"].Order = 7
	*c.Overrides["o"].X = 7
	c.Overrides["o"].Properties["list"].([]any)[1].(map[string]any)["k"] = "w"
	*c.Layers[0].Visible = false
	o := &c.Layers[0].Objects[0]
	*o.Alpha, *o.ScaleX = 1, 1
	o.Sprite.Frame = "g"
	*o.Overrides["x"].Name = "m"
	o.Properties["nested"].(map[string]any)["list"].([]any)[0] = 9
	o.Children[0].ID = "d"
	after, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("changing a clone changed its source:\n%s\n--- before ---\n%s", after, before)
	}
}

func FuzzResolveLevel(f *testing.F) {
	f.Add(readFuzzSeed(f, "project/level.scene.yaml"))
	f.Add(readFuzzSeed(f, "project/scenes/fast_enemy.scene.yaml"))
	f.Add([]byte("version: 2\ninherits: scenes/enemy.scene.yaml\nlayerOverrides:\n  root: {visible: false}\nlayers:\n  - name: root\n    objects:\n      - id: e\n        scene: scenes/fast_enemy.scene.yaml\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := sceneformat.Parse(data)
		if err != nil {
			return
		}
		before, err := sceneformat.Encode(doc)
		if err != nil {
			t.Fatalf("Encode of a parsed document: %v", err)
		}
		resolved, err := sceneformat.Resolve(doc, projectScenes)
		if after, encErr := sceneformat.Encode(doc); encErr != nil || !bytes.Equal(before, after) {
			t.Fatalf("Resolve changed its input (err %v)", encErr)
		}
		if err != nil {
			return
		}
		if resolved.RefersToScenes() {
			t.Fatal("a resolved scene still refers to scenes")
		}
		if err := sceneformat.Validate(resolved); err != nil {
			t.Fatalf("Resolve returned an invalid scene: %v", err)
		}
		if _, err := sceneformat.Encode(resolved); err != nil {
			t.Fatalf("Encode of a resolved scene: %v", err)
		}
	})
}

func BenchmarkResolveLevel(b *testing.B) {
	raw, err := os.ReadFile("testdata/project/level.scene.yaml")
	if err != nil {
		b.Fatal(err)
	}
	doc, err := sceneformat.Parse(raw)
	if err != nil {
		b.Fatal(err)
	}
	enemy, err := projectScenes.Scene("scenes/enemy.scene.yaml")
	if err != nil {
		b.Fatal(err)
	}
	scenes := fixedScenes{"scenes/enemy.scene.yaml": enemy}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := sceneformat.Resolve(doc, scenes); err != nil {
			b.Fatal(err)
		}
	}
}

// fixedScenes serves already parsed scenes, so a benchmark measures resolving rather than reading files.
type fixedScenes map[string]*sceneformat.SceneDoc

func (m fixedScenes) Scene(path string) (*sceneformat.SceneDoc, error) {
	d, ok := m[path]
	if !ok {
		return nil, fmt.Errorf("no scene %q", path)
	}
	return d, nil
}

func TestUIDIsTheScenesOwn(t *testing.T) {
	scenes := newMemScenes(map[string]string{
		"base.yaml": "version: 2\nuid: base000000001\nlayers:\n  - name: a\n",
	})
	const src = "version: 2\nuid: derv000000001\ninherits: base.yaml\n"
	doc, err := sceneformat.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := sceneformat.Encode(doc)
	if err != nil || string(out) != sceneformat.SchemaCommentV2+"\n"+src {
		t.Fatalf("Encode = %q, %v", out, err)
	}
	resolved, err := sceneformat.Resolve(doc, scenes)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.UID != "derv000000001" {
		t.Fatalf("resolved UID = %q, want the inheriting scene's own", resolved.UID)
	}
	for _, v := range []int{1, 2} {
		bad := fmt.Sprintf("version: %d\nuid: Derv000000001\n", v)
		_, err := sceneformat.Parse([]byte(bad))
		var fe *sceneformat.FieldError
		if !errors.As(err, &fe) || strings.Join(fe.Path, ".") != "uid" {
			t.Fatalf("version %d with a bad uid: %v", v, err)
		}
	}
}

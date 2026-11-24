package sceneformat_test

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
)

const treeEps = 1e-4

func near(a, b float32) bool { return math.Abs(float64(a-b)) <= treeEps }

func loadFleet(t *testing.T) *sceneformat.SceneDoc {
	t.Helper()
	doc, err := sceneformat.Parse(readTestdata(t, "fleet.scene.yaml"))
	if err != nil {
		t.Fatalf("Parse fleet: %v", err)
	}
	return doc
}

// fleetWorld is where each object of fleet.scene.yaml must land: ship sits at (100, 50) turned a quarter turn and scaled 2, so its child turret at local (10, 0) lands 20 below it, and barrel at local (5, 0) under turret lands 10 further, at half of turret's scale.
var fleetWorld = map[string]struct {
	parent       string
	x, y, rot, s float32
}{
	"ship":   {parent: "", x: 100, y: 50, rot: math.Pi / 2, s: 2},
	"turret": {parent: "ship", x: 100, y: 70, rot: math.Pi / 2, s: 2},
	"barrel": {parent: "turret", x: 100, y: 80, rot: math.Pi / 2, s: 1},
	"flag":   {parent: "ship", x: 116, y: 50, rot: math.Pi / 2, s: 2},
	"escort": {parent: "", x: 20, y: 20, rot: 0, s: 1},
	"panel":  {parent: "", x: 4, y: 4, rot: 0, s: 1},
	"label":  {parent: "panel", x: 6, y: 6, rot: 0, s: 1},
}

func TestNestedSceneRoundTripsByteForByte(t *testing.T) {
	raw := readTestdata(t, "fleet.scene.yaml")
	doc := loadFleet(t)
	encoded, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(encoded, raw) {
		t.Fatalf("Encode(Parse(file)) differs from the file:\n%s", encoded)
	}
	again, err := sceneformat.Parse(encoded)
	if err != nil {
		t.Fatalf("Parse re-encoded: %v", err)
	}
	if !reflect.DeepEqual(doc, again) {
		t.Fatalf("round trip changed the document")
	}
}

func TestWalkObjectsVisitsParentsBeforeChildren(t *testing.T) {
	doc := loadFleet(t)
	var got []string
	sceneformat.WalkObjects(doc.Layers[0].Objects, func(o, parent *sceneformat.ObjectDoc) bool {
		p := ""
		if parent != nil {
			p = parent.ID
		}
		got = append(got, p+">"+o.ID)
		return true
	})
	want := []string{">ship", "ship>turret", "turret>barrel", "ship>flag", ">escort"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("walk order = %v, want %v", got, want)
	}

	var stopped []string
	complete := sceneformat.WalkObjects(doc.Layers[0].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
		stopped = append(stopped, o.ID)
		return o.ID != "barrel"
	})
	if complete || !reflect.DeepEqual(stopped, []string{"ship", "turret", "barrel"}) {
		t.Fatalf("stopped walk = %v (complete %v), want it to end at barrel", stopped, complete)
	}
}

func TestBuildComposesNestedWorldTransforms(t *testing.T) {
	built, err := sceneformat.Build(loadFleet(t), caveResolver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(built.Objects) != len(fleetWorld) {
		t.Fatalf("built %d objects, want %d", len(built.Objects), len(fleetWorld))
	}
	for id, want := range fleetWorld {
		bo := built.Object(id)
		if bo == nil {
			t.Fatalf("object %q not built", id)
		}
		gotParent := ""
		if bo.Parent != nil {
			gotParent = bo.Parent.ID
			if !slices.Contains(bo.Parent.Children, bo) {
				t.Errorf("%q is missing from its parent's Children", id)
			}
		}
		if gotParent != want.parent {
			t.Errorf("%q parent = %q, want %q", id, gotParent, want.parent)
		}
		if !near(bo.World.Tx, want.x) || !near(bo.World.Ty, want.y) {
			t.Errorf("%q world position = (%v, %v), want (%v, %v)", id, bo.World.Tx, bo.World.Ty, want.x, want.y)
		}
		wantM := math2d.TRS(math2d.Vec2{X: want.x, Y: want.y}, want.rot, math2d.Vec2{X: want.s, Y: want.s})
		if !bo.World.ApproxEqual(wantM, treeEps) {
			t.Errorf("%q world = %+v, want %+v", id, bo.World, wantM)
		}
		if bo.Sprite != nil {
			sp := bo.Sprite
			if !near(sp.X, want.x) || !near(sp.Y, want.y) || !near(sp.Rotation, want.rot) || !near(sp.ScaleX, want.s) || !near(sp.ScaleY, want.s) {
				t.Errorf("%q sprite = (%v, %v) rot %v scale (%v, %v), want (%v, %v) rot %v scale %v", id, sp.X, sp.Y, sp.Rotation, sp.ScaleX, sp.ScaleY, want.x, want.y, want.rot, want.s)
			}
			// The drawn sprite must place its frame exactly where the composed world transform puts the object's origin.
			local := math2d.Affine{A: 1, D: 1, Tx: -sp.OriginX, Ty: -sp.OriginY}
			if !sp.Transform().ApproxEqual(bo.World.Mul(local), treeEps) {
				t.Errorf("%q sprite transform %+v, want %+v", id, sp.Transform(), bo.World.Mul(local))
			}
		}
	}

	ships := built.Layer("ships")
	if got := len(ships.Objects); got != 2 || ships.Objects[0].ID != "ship" || ships.Objects[1].ID != "escort" {
		t.Fatalf("layer ships has top-level objects %v, want [ship escort]", objectIDs(ships.Objects))
	}
	if a := built.Object("turret").WorldAlpha; !near(a, 0.25) {
		t.Errorf("turret world alpha = %v, want 0.25", a)
	}
	if a := built.Object("barrel").Sprite.Alpha; !near(a, 0.25) {
		t.Errorf("barrel sprite alpha = %v, want 0.25 inherited from its ancestors", a)
	}
	// Sprites sort by Z with ties in tree order: ship (z 0), barrel (z 0, after ship), turret (z 1).
	var drawn []*sceneformat.BuiltObject
	for _, b := range ships.Batches {
		for _, sp := range b.Sprites {
			for _, id := range []string{"ship", "turret", "barrel"} {
				if built.Object(id).Sprite == sp {
					drawn = append(drawn, built.Object(id))
				}
			}
		}
	}
	if got := objectIDs(drawn); !reflect.DeepEqual(got, []string{"ship", "barrel", "turret"}) {
		t.Fatalf("draw order = %v, want [ship barrel turret]", got)
	}
}

// nestedSpriteDoc is a one-layer scene holding parent with a single sprite child.
func nestedSpriteDoc(parent, child sceneformat.ObjectDoc) *sceneformat.SceneDoc {
	parent.ID = "parent"
	child.ID = "child"
	child.Sprite = &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"}
	parent.Children = []sceneformat.ObjectDoc{child}
	return &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers:  []sceneformat.LayerDoc{{Name: "a", Objects: []sceneformat.ObjectDoc{parent}}},
	}
}

func TestShearedSpriteIsRefusedByEveryEntryPoint(t *testing.T) {
	doc := nestedSpriteDoc(
		sceneformat.ObjectDoc{ScaleX: sceneformat.Float32(3)},
		sceneformat.ObjectDoc{Rotation: 0.5},
	)
	wantShear := func(what string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "sheared") || !strings.Contains(err.Error(), `"child"`) {
			t.Fatalf("%s error = %v, want a shear error naming child", what, err)
		}
	}
	// The shear depends only on the document, so the document checks refuse it before any asset is resolved.
	wantShear("Validate", sceneformat.Validate(doc))
	_, err := sceneformat.Encode(doc)
	wantShear("Encode", err)
	_, err = sceneformat.Build(doc, caveResolver)
	wantShear("Build", err)
	wantShear("CheckObjectTree", sceneformat.CheckObjectTree(&doc.Layers[0].Objects[0]))

	// Without a sprite there is nothing to draw, so the same tree builds and keeps its exact matrix.
	doc.Layers[0].Objects[0].Children[0].Sprite = nil
	if err := sceneformat.CheckObjectTree(&doc.Layers[0].Objects[0]); err != nil {
		t.Fatalf("CheckObjectTree without sprite: %v", err)
	}
	built, err := sceneformat.Build(doc, caveResolver)
	if err != nil {
		t.Fatalf("Build without sprite: %v", err)
	}
	want := math2d.Scale(3, 1).Mul(math2d.Rotate(0.5))
	if got := built.Object("child").World; !got.ApproxEqual(want, treeEps) {
		t.Fatalf("child world = %+v, want %+v", got, want)
	}
}

func TestCheckObjectTreeRefusesDeepTrees(t *testing.T) {
	root := sceneformat.ObjectDoc{ID: "n0"}
	leaf := &root
	for i := 1; i < sceneformat.MaxObjectDepth; i++ {
		leaf.Children = []sceneformat.ObjectDoc{{ID: "n" + strconv.Itoa(i)}}
		leaf = &leaf.Children[0]
	}
	if err := sceneformat.CheckObjectTree(&root); err != nil {
		t.Fatalf("CheckObjectTree at the depth limit: %v", err)
	}
	leaf.Children = []sceneformat.ObjectDoc{{ID: "too-deep"}}
	if err := sceneformat.CheckObjectTree(&root); err == nil || !strings.Contains(err.Error(), "too-deep") {
		t.Fatalf("CheckObjectTree past the depth limit: err = %v", err)
	}
}

func TestBuildDrawsMirroredAndCollapsedChildSprites(t *testing.T) {
	cases := []struct {
		name          string
		parent, child sceneformat.ObjectDoc
		// collapsed marks a world transform with parallel columns, which covers no area and draws at zero scale.
		collapsed bool
	}{
		{
			name:   "mirrored parent",
			parent: sceneformat.ObjectDoc{X: 10, Y: 20, ScaleX: sceneformat.Float32(-1)},
			child:  sceneformat.ObjectDoc{X: 5, Rotation: 0.6},
		},
		{
			name:   "mirrored child under a rotated parent",
			parent: sceneformat.ObjectDoc{X: 10, Rotation: 0.7, ScaleX: sceneformat.Float32(2), ScaleY: sceneformat.Float32(2)},
			child:  sceneformat.ObjectDoc{Y: 3, Rotation: 0.3, ScaleY: sceneformat.Float32(-1.5)},
		},
		{
			name:   "both axes mirrored",
			parent: sceneformat.ObjectDoc{Rotation: -1.2, ScaleX: sceneformat.Float32(-2), ScaleY: sceneformat.Float32(-2)},
			child:  sceneformat.ObjectDoc{X: 4, Rotation: 0.25},
		},
		{
			name:   "parent with zero scaleX keeps the child's turn",
			parent: sceneformat.ObjectDoc{X: 7, Rotation: 0.4, ScaleX: sceneformat.Float32(0)},
			child:  sceneformat.ObjectDoc{Y: 2},
		},
		{
			name:      "uniformly collapsed parent",
			parent:    sceneformat.ObjectDoc{X: 7, Y: 8, ScaleX: sceneformat.Float32(0), ScaleY: sceneformat.Float32(0)},
			child:     sceneformat.ObjectDoc{X: 5, Rotation: 0.9},
			collapsed: true,
		},
		{
			name:      "parent collapsed along one axis with a rotated child",
			parent:    sceneformat.ObjectDoc{X: 7, Y: 8, ScaleX: sceneformat.Float32(0)},
			child:     sceneformat.ObjectDoc{X: 5, Y: 1, Rotation: 0.9},
			collapsed: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := nestedSpriteDoc(tc.parent, tc.child)
			doc.Layers[0].Objects[0].Children[0].OriginX = 3
			doc.Layers[0].Objects[0].Children[0].OriginY = 2
			encoded, err := sceneformat.Encode(doc)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if _, err := sceneformat.Parse(encoded); err != nil {
				t.Fatalf("Parse: %v", err)
			}
			built, err := sceneformat.Build(doc, caveResolver)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			bo := built.Object("child")
			sp := bo.Sprite
			if sp == nil {
				t.Fatalf("child has no sprite")
			}
			if !near(sp.X, bo.World.Tx) || !near(sp.Y, bo.World.Ty) {
				t.Fatalf("sprite at (%v, %v), want the world origin (%v, %v)", sp.X, sp.Y, bo.World.Tx, bo.World.Ty)
			}
			if tc.collapsed {
				if sp.ScaleX != 0 || sp.ScaleY != 0 {
					t.Fatalf("collapsed sprite scale = (%v, %v), want (0, 0)", sp.ScaleX, sp.ScaleY)
				}
				return
			}
			local := math2d.Affine{A: 1, D: 1, Tx: -sp.OriginX, Ty: -sp.OriginY}
			if want := bo.World.Mul(local); !sp.Transform().ApproxEqual(want, treeEps) {
				t.Fatalf("sprite transform %+v (rot %v, scale (%v, %v)), want %+v", sp.Transform(), sp.Rotation, sp.ScaleX, sp.ScaleY, want)
			}
		})
	}
}

func TestSpawnEntitiesBuildsHierarchy2D(t *testing.T) {
	w := entities.NewWorld()
	spawned, err := sceneformat.SpawnEntities(loadFleet(t), w)
	if err != nil {
		t.Fatalf("SpawnEntities: %v", err)
	}
	if len(spawned) != len(fleetWorld) || w.Count() != len(fleetWorld) {
		t.Fatalf("spawned %d (world count %d), want %d", len(spawned), w.Count(), len(fleetWorld))
	}
	h := w.Hierarchy2D()
	h.Sync()
	for id, want := range fleetWorld {
		e := spawned[id]
		p, ok := h.Parent(e)
		if want.parent == "" {
			if ok {
				t.Errorf("%q has parent %v, want a root", id, p)
			}
		} else if !ok || p != spawned[want.parent] {
			t.Errorf("%q parent = %v (%v), want %q", id, p, ok, want.parent)
		}
		wt, ok := h.WorldTransform(e)
		if !ok {
			t.Fatalf("%q has no world transform", id)
		}
		if !near(wt.X, want.x) || !near(wt.Y, want.y) || !near(wt.Rotation, want.rot) || !near(wt.ScaleX, want.s) || !near(wt.ScaleY, want.s) {
			t.Errorf("%q world = (%v, %v) rot %v scale (%v, %v), want (%v, %v) rot %v scale %v", id, wt.X, wt.Y, wt.Rotation, wt.ScaleX, wt.ScaleY, want.x, want.y, want.rot, want.s)
		}
		wantVisible := id != "panel" && id != "label"
		if wt.WorldVisible != wantVisible {
			t.Errorf("%q world visible = %v, want %v", id, wt.WorldVisible, wantVisible)
		}
		ref, ok := entities.Get[sceneformat.ObjectRef](w, e)
		if !ok || ref.ID != id {
			t.Errorf("%q ObjectRef = %+v (%v)", id, ref, ok)
		}
	}
	if got := h.Children(spawned["ship"]); !reflect.DeepEqual(got, []entities.Entity{spawned["turret"], spawned["flag"]}) {
		t.Fatalf("ship children = %v, want [turret flag] in document order", got)
	}

	disposed := entities.NewWorld()
	disposed.Dispose()
	if _, err := sceneformat.SpawnEntities(loadFleet(t), disposed); err == nil {
		t.Fatal("SpawnEntities into a disposed world succeeded")
	}
	if _, err := sceneformat.SpawnEntities(loadFleet(t), nil); err == nil {
		t.Fatal("SpawnEntities into a nil world succeeded")
	}
}

func TestBuildNodes3DBuildsNodeTree(t *testing.T) {
	scene := scene3d.NewScene()
	nodes, err := sceneformat.BuildNodes3D(loadFleet(t), scene.Root())
	if err != nil {
		t.Fatalf("BuildNodes3D: %v", err)
	}
	if got := scene.Root().Children(); len(got) != 2 || got[0] != nodes.Layers["ships"] || got[1] != nodes.Layers["hud"] {
		t.Fatalf("root children are not the two layer nodes in order")
	}
	want3D := map[string]math3d.Vec3{
		"ship":   {X: 100, Y: 50, Z: 0},
		"turret": {X: 100, Y: 70, Z: 2},
		"barrel": {X: 100, Y: 80, Z: 2},
		"flag":   {X: 116, Y: 50, Z: 0},
		"escort": {X: 20, Y: 20, Z: 0},
		"panel":  {X: 4, Y: 4, Z: 0},
		"label":  {X: 6, Y: 6, Z: 0},
	}
	if len(nodes.Objects) != len(want3D) {
		t.Fatalf("built %d object nodes, want %d", len(nodes.Objects), len(want3D))
	}
	scene.UpdateTransforms()
	for id, wantPos := range want3D {
		n := nodes.Objects[id]
		if n.Name != id {
			t.Errorf("node name = %q, want %q", n.Name, id)
		}
		if o, ok := n.UserData.(*sceneformat.ObjectDoc); !ok || o.ID != id {
			t.Errorf("%q UserData = %#v", id, n.UserData)
		}
		wantParent := nodes.Layers["ships"]
		if p := fleetWorld[id].parent; p != "" {
			wantParent = nodes.Objects[p]
		} else if id == "panel" {
			wantParent = nodes.Layers["hud"]
		}
		if n.Parent() != wantParent {
			t.Errorf("%q parent = %v, want %v", id, n.Parent().Name, wantParent.Name)
		}
		got := n.WorldTransform().Pos
		if !near(got.X, wantPos.X) || !near(got.Y, wantPos.Y) || !near(got.Z, wantPos.Z) {
			t.Errorf("%q world position = %+v, want %+v", id, got, wantPos)
		}
	}
	barrel := nodes.Objects["barrel"].WorldTransform()
	if !near(barrel.Scale.X, 1) || !near(barrel.Scale.Z, 2) {
		t.Errorf("barrel world scale = %+v, want (1, 1, 2)", barrel.Scale)
	}
	if nodes.Objects["label"].EffectiveVisible() {
		t.Error("label in a hidden layer is effectively visible")
	}
	if !nodes.Objects["barrel"].EffectiveVisible() {
		t.Error("barrel in a visible layer is not effectively visible")
	}

	if _, err := sceneformat.BuildNodes3D(loadFleet(t), nil); err == nil {
		t.Fatal("BuildNodes3D under a nil parent succeeded")
	}
}

func TestObjectLocalTransform3DMatchesFixedAxisOrder(t *testing.T) {
	o := &sceneformat.ObjectDoc{RotX: 0.3, RotY: -0.7, RotZ: 1.1}
	got := sceneformat.ObjectLocalTransform3D(o)
	// Rotating X, then Y, then Z about fixed axes turns +X like this, as the editor's 3D viewport draws it.
	x, y, z := 1.0, 0.0, 0.0
	s, c := math.Sincos(0.3)
	y, z = y*c-z*s, y*s+z*c
	s, c = math.Sincos(-0.7)
	x, z = x*c+z*s, -x*s+z*c
	s, c = math.Sincos(1.1)
	x, y = x*c-y*s, x*s+y*c
	v := got.Rot.RotateVec3(math3d.Vec3{X: 1})
	if !near(v.X, float32(x)) || !near(v.Y, float32(y)) || !near(v.Z, float32(z)) {
		t.Fatalf("rotated +X = %+v, want (%v, %v, %v)", v, x, y, z)
	}
	if id := sceneformat.ObjectLocalTransform3D(&sceneformat.ObjectDoc{}); id != math3d.TransformIdentity() {
		t.Fatalf("zero object transform = %+v, want identity", id)
	}
}

func TestNestedValidationErrors(t *testing.T) {
	deep := "version: 1\nlayers:\n  - name: a\n    objects:\n"
	indent := "      "
	for i := 0; i <= sceneformat.MaxObjectDepth; i++ {
		deep += indent + "- id: o" + strconv.Itoa(i) + "\n"
		deep += indent + "  children:\n"
		indent += "    "
	}
	deep += indent + "- id: leaf\n"

	cases := []struct {
		name, yaml, wantSub string
	}{
		{name: "duplicate_id_parent_child", yaml: "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: dup\n        children:\n          - id: dup\n", wantSub: "duplicate object id \"dup\""},
		{name: "duplicate_id_across_branches", yaml: "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: p\n        children:\n          - id: c\n      - id: q\n        children:\n          - id: c\n", wantSub: "duplicate object id \"c\""},
		{name: "duplicate_id_child_and_other_layer", yaml: "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: p\n        children:\n          - id: c\n  - name: b\n    objects:\n      - id: c\n", wantSub: "duplicate object id \"c\""},
		{name: "empty_child_id", yaml: "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: p\n        children:\n          - name: Nameless\n", wantSub: "object \"p\" child 0 has an empty id"},
		{name: "unknown_child_field", yaml: "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: p\n        children:\n          - id: c\n            parent: p\n", wantSub: "not found"},
		{name: "bad_child_alpha", yaml: "version: 1\nlayers:\n  - name: a\n    objects:\n      - id: p\n        children:\n          - id: c\n            alpha: 2\n", wantSub: "object \"c\" alpha"},
		{name: "too_deep", yaml: deep, wantSub: "deeper than"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sceneformat.Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("Parse error = %v, want it to contain %q", err, tc.wantSub)
			}
		})
	}

	// MaxObjectDepth levels exactly are allowed.
	ok := &sceneformat.SceneDoc{Version: sceneformat.CurrentVersion, Layers: []sceneformat.LayerDoc{{Name: "a"}}}
	objs := &ok.Layers[0].Objects
	for i := 0; i < sceneformat.MaxObjectDepth; i++ {
		*objs = []sceneformat.ObjectDoc{{ID: "o" + strconv.Itoa(i)}}
		objs = &(*objs)[0].Children
	}
	if err := sceneformat.Validate(ok); err != nil {
		t.Fatalf("Validate at MaxObjectDepth: %v", err)
	}
}

func FuzzParseNestedRoundTrip(f *testing.F) {
	f.Add(readFuzzSeed(f, "fleet.scene.yaml"))
	f.Add(readFuzzSeed(f, "cave_room.scene.yaml"))
	f.Add([]byte("version: 1\nlayers:\n  - name: a\n    objects:\n      - id: p\n        children:\n          - id: c\n            x: 1\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := sceneformat.Parse(data)
		if err != nil {
			return
		}
		first, err := sceneformat.Encode(doc)
		if err != nil {
			t.Fatalf("Encode of a parsed document: %v", err)
		}
		again, err := sceneformat.Parse(first)
		if err != nil {
			t.Fatalf("Parse of encoded document: %v\n%s", err, first)
		}
		second, err := sceneformat.Encode(again)
		if err != nil {
			t.Fatalf("second Encode: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("encoding is not stable:\n%s\n---\n%s", first, second)
		}
	})
}

func BenchmarkBuildNestedScene(b *testing.B) {
	doc := &sceneformat.SceneDoc{Version: sceneformat.CurrentVersion, Layers: []sceneformat.LayerDoc{{Name: "a"}}}
	// 64 parents with 8 children of 4 grandchildren each: 2112 sprites.
	for p := 0; p < 64; p++ {
		parent := sceneformat.ObjectDoc{ID: "p" + strconv.Itoa(p), X: float32(p), Rotation: 0.1, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"}}
		for c := 0; c < 8; c++ {
			child := sceneformat.ObjectDoc{ID: parent.ID + "c" + strconv.Itoa(c), X: 4, Rotation: 0.2, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"}}
			for g := 0; g < 4; g++ {
				child.Children = append(child.Children, sceneformat.ObjectDoc{ID: child.ID + "g" + strconv.Itoa(g), Y: 2, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "torch"}})
			}
			parent.Children = append(parent.Children, child)
		}
		doc.Layers[0].Objects = append(doc.Layers[0].Objects, parent)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := sceneformat.Build(doc, caveResolver); err != nil {
			b.Fatal(err)
		}
	}
}

func readFuzzSeed(f *testing.F, name string) []byte {
	f.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		f.Fatalf("read testdata %q: %v", name, err)
	}
	return data
}

func objectIDs(objs []*sceneformat.BuiltObject) []string {
	ids := make([]string, len(objs))
	for i, o := range objs {
		ids[i] = o.ID
	}
	return ids
}

func TestSyncRecomposesAnEditedObjectAndItsChildren(t *testing.T) {
	built, err := sceneformat.Build(loadFleet(t), caveResolver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := map[string]sprite.Sprite{}
	for id, bo := range built.Objects {
		if bo.Sprite != nil {
			want[id] = *bo.Sprite
			bo.Sprite.X, bo.Sprite.Alpha = -999, 0
		}
	}
	for _, l := range built.Layers {
		for _, bo := range l.Objects {
			if !bo.Sync() {
				t.Fatalf("Sync(%q) reported a sheared sprite", bo.ID)
			}
		}
	}
	for id, sp := range want {
		if got := *built.Object(id).Sprite; !reflect.DeepEqual(got, sp) {
			t.Errorf("%q sprite after Sync = %+v, want %+v as built", id, got, sp)
		}
	}

	ship := built.Object("ship")
	before := map[string]math2d.Affine{}
	for _, c := range ship.Children {
		before[c.ID] = c.World
	}
	ship.Doc.X += 10
	ship.Doc.Alpha = sceneformat.Float32(0.5)
	if !ship.Sync() {
		t.Fatal("Sync(ship) reported a sheared sprite")
	}
	for _, c := range ship.Children {
		if !near(c.World.Tx, before[c.ID].Tx+10) || !near(c.World.Ty, before[c.ID].Ty) {
			t.Errorf("%q world position after moving its parent = (%v, %v), want (%v, %v)", c.ID, c.World.Tx, c.World.Ty, before[c.ID].Tx+10, before[c.ID].Ty)
		}
		if !near(c.WorldAlpha, c.Doc.EffectiveAlpha()*0.5) {
			t.Errorf("%q world alpha = %v, want %v", c.ID, c.WorldAlpha, c.Doc.EffectiveAlpha()*0.5)
		}
	}
}

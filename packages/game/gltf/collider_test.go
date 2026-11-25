package gltf_test

import (
	"encoding/json"
	"math"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
)

func TestParseNodeColliderSuffixes(t *testing.T) {
	cases := []struct {
		name     string
		wantOK   bool
		wantKind gltf.ColliderKind
		wantDraw bool
		wantBase string
	}{
		{"Floor", false, gltf.ColliderNone, true, ""},
		{"Floor-col", true, gltf.ColliderTriMesh, true, "Floor"},
		{"Floor-colonly", true, gltf.ColliderTriMesh, false, "Floor"},
		{"Wall-boxcol", true, gltf.ColliderBox, true, "Wall"},
		{"Wall-boxcolonly", true, gltf.ColliderBox, false, "Wall"},
		{"Orb-spherecol", true, gltf.ColliderSphere, true, "Orb"},
		{"Orb-spherecolonly", true, gltf.ColliderSphere, false, "Orb"},
		{"Pillar-capsulecol", true, gltf.ColliderCapsule, true, "Pillar"},
		{"Pillar-capsulecolonly", true, gltf.ColliderCapsule, false, "Pillar"},
		{"Ramp-convcol", true, gltf.ColliderConvex, true, "Ramp"},
		{"Ramp-convcolonly", true, gltf.ColliderConvex, false, "Ramp"},
		// Case-insensitive and Blender .001 duplicate suffixes.
		{"Crate-BoxColOnly", true, gltf.ColliderBox, false, "Crate"},
		{"Wall-colonly.001", true, gltf.ColliderTriMesh, false, "Wall"},
		{"Pillar-capsulecolonly.042", true, gltf.ColliderCapsule, false, "Pillar"},
		// Non-numeric dot suffix is not stripped.
		{"Wall-colonly.bak", false, gltf.ColliderNone, true, ""},
	}

	for _, tc := range cases {
		node := gltf.Node{Name: tc.name, Mesh: 0, WorldMatrix: math3d.Mat4Identity()}
		info, ok, err := gltf.ParseNodeCollider(node)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", tc.name, err)
		}
		if ok != tc.wantOK {
			t.Fatalf("%q: ok = %v, want %v", tc.name, ok, tc.wantOK)
		}
		if gotDraw := node.IsDrawable(); gotDraw != tc.wantDraw {
			t.Fatalf("%q: IsDrawable = %v, want %v", tc.name, gotDraw, tc.wantDraw)
		}
		if !ok {
			continue
		}
		if info.Kind != tc.wantKind || info.Draw != tc.wantDraw || info.BaseName != tc.wantBase {
			t.Fatalf("%q: got (kind=%q, draw=%v, base=%q), want (%q, %v, %q)",
				tc.name, info.Kind, info.Draw, info.BaseName, tc.wantKind, tc.wantDraw, tc.wantBase)
		}
		if info.Tag != tc.wantBase {
			t.Fatalf("%q: default Tag = %q, want %q", tc.name, info.Tag, tc.wantBase)
		}
	}
}

func TestParseNodeColliderExtras(t *testing.T) {
	t.Run("object full specification", func(t *testing.T) {
		node := gltf.Node{
			Name:        "TriggerZone",
			Mesh:        -1,
			WorldMatrix: math3d.Translate(math3d.Vec3{X: 1, Y: 2, Z: 3}),
			Extras: json.RawMessage(`{
				"collider": {
					"shape": "box",
					"draw": false,
					"trigger": true,
					"tag": "checkpoint",
					"center": [0, 1, 0],
					"size": [4, 2, 6],
					"friction": 0.8,
					"restitution": 0.25,
					"layer": 4,
					"mask": 7
				}
			}`),
		}
		info, ok, err := gltf.ParseNodeCollider(node)
		if err != nil || !ok {
			t.Fatalf("ParseNodeCollider = (%+v, %v, %v)", info, ok, err)
		}
		if info.Kind != gltf.ColliderBox || info.Draw || !info.IsTrigger || info.Tag != "checkpoint" {
			t.Fatalf("unexpected header: %+v", info)
		}
		if !info.HasCenter || info.Center != (math3d.Vec3{X: 0, Y: 1, Z: 0}) {
			t.Fatalf("Center = (%v, %v)", info.HasCenter, info.Center)
		}
		if !info.HasHalfExtents || info.HalfExtents != (math3d.Vec3{X: 2, Y: 1, Z: 3}) {
			t.Fatalf("HalfExtents = (%v, %v), want (2,1,3)", info.HasHalfExtents, info.HalfExtents)
		}
		if !info.HasFriction || math.Abs(float64(info.Friction-0.8)) > 1e-6 {
			t.Fatalf("Friction = (%v, %v)", info.HasFriction, info.Friction)
		}
		if !info.HasRestitution || math.Abs(float64(info.Restitution-0.25)) > 1e-6 {
			t.Fatalf("Restitution = (%v, %v)", info.HasRestitution, info.Restitution)
		}
		if !info.HasLayer || info.Layer != 4 || !info.HasMask || info.Mask != 7 {
			t.Fatalf("Layer/Mask = (%v:%d, %v:%d)", info.HasLayer, info.Layer, info.HasMask, info.Mask)
		}
	})

	t.Run("string and bool shorthands", func(t *testing.T) {
		n1 := gltf.Node{Name: "Ball", Mesh: 0, Extras: json.RawMessage(`{"collider": "sphere", "draw": false}`)}
		info, ok, err := gltf.ParseNodeCollider(n1)
		if err != nil || !ok || info.Kind != gltf.ColliderSphere || info.Draw || n1.IsDrawable() {
			t.Fatalf("string shorthand failed: (%+v, %v, %v), drawable=%v", info, ok, err, n1.IsDrawable())
		}

		n2 := gltf.Node{Name: "Mesh", Mesh: 0, Extras: json.RawMessage(`{"collider": true}`)}
		info, ok, err = gltf.ParseNodeCollider(n2)
		if err != nil || !ok || info.Kind != gltf.ColliderTriMesh || !info.Draw || !n2.IsDrawable() {
			t.Fatalf("bool true failed: (%+v, %v, %v)", info, ok, err)
		}

		// Explicitly disabling collider on a suffixed node.
		n3 := gltf.Node{Name: "Decor-colonly", Mesh: 0, Extras: json.RawMessage(`{"collider": false}`)}
		_, ok, err = gltf.ParseNodeCollider(n3)
		if err != nil || ok {
			t.Fatalf("bool false should disable collider: ok=%v err=%v", ok, err)
		}

		// Capsule extras with type alias and only:true.
		n4 := gltf.Node{Name: "Cap", Mesh: 0, Extras: json.RawMessage(`{"collider": {"type": "capsule", "only": true, "radius": 0.4, "height": 1.8}}`)}
		info, ok, err = gltf.ParseNodeCollider(n4)
		if err != nil || !ok || info.Kind != gltf.ColliderCapsule || info.Draw || !info.HasRadius || !info.HasHeight {
			t.Fatalf("capsule extras failed: (%+v, %v, %v)", info, ok, err)
		}
	})

	t.Run("malformed extras rejected", func(t *testing.T) {
		bad := []string{
			`{"collider": "torus"}`,
			`{"collider": 42}`,
			`{"collider": {"shape": "box", "center": [1, 2]}}`,
			`{"collider": {"shape": "box", "halfExtents": [-1, 1, 1]}}`,
			`{"collider": {"shape": "sphere", "radius": 0}}`,
			`{"collider": {"shape": "capsule", "height": -2}}`,
			`{"collider": {"shape": "box", "restitution": 1.5}}`,
			`{"collider": {"shape": "box", "friction": -0.1}}`,
		}
		for _, raw := range bad {
			node := gltf.Node{Name: "Bad", Mesh: 0, Extras: json.RawMessage(raw)}
			if _, _, err := gltf.ParseNodeCollider(node); err == nil {
				t.Fatalf("expected error for malformed extras %s", raw)
			}
		}
	})
}

func TestDocumentDrawableAndColliderNodes(t *testing.T) {
	doc := &gltf.Document{
		Scene: 0,
		Scenes: []gltf.Scene{
			{Name: "Main", Roots: []int{0}},
		},
		Meshes: []gltf.Mesh{
			{Name: "Box"},
		},
		Nodes: []gltf.Node{
			{Name: "Root", Mesh: -1, Children: []int{1, 2, 3, 4, 5}},
			{Name: "VisualFloor", Mesh: 0},
			{Name: "Floor-colonly", Mesh: 0},
			{Name: "Pillar-boxcol", Mesh: 0},
			{Name: "HiddenBox", Mesh: 0, Extras: json.RawMessage(`{"collider":{"shape":"box","draw":false}}`)},
			{Name: "EmptyTrigger", Mesh: -1, Extras: json.RawMessage(`{"collider":{"shape":"sphere","radius":2,"trigger":true}}`)},
		},
	}
	if err := doc.ComputeWorldMatrices(); err != nil {
		t.Fatal(err)
	}

	gotDrawable := doc.DrawableNodes()
	wantDrawable := []int{1, 3} // VisualFloor and Pillar-boxcol; Floor-colonly, HiddenBox, EmptyTrigger excluded.
	if !slices.Equal(gotDrawable, wantDrawable) {
		t.Fatalf("DrawableNodes = %v, want %v", gotDrawable, wantDrawable)
	}

	cols, err := doc.ColliderNodes()
	if err != nil {
		t.Fatalf("ColliderNodes error: %v", err)
	}
	if len(cols) != 4 {
		t.Fatalf("ColliderNodes count = %d, want 4 (%+v)", len(cols), cols)
	}
	wantIdx := []int{2, 3, 4, 5}
	for i, c := range cols {
		if c.NodeIndex != wantIdx[i] {
			t.Fatalf("cols[%d].NodeIndex = %d, want %d", i, c.NodeIndex, wantIdx[i])
		}
	}
}

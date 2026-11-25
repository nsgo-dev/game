package sceneformat_test

import (
	"math"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/world3d"
	"github.com/nsgo-dev/nsgo/core"
)

func TestNavLinkComponentValidation(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	if err := sceneformat.RegisterNavLink(reg); err != nil {
		t.Fatalf("RegisterNavLink: %v", err)
	}

	validYAML := `version: 2
layers:
  - name: world
    objects:
      - id: ledge_drop
        x: 9
        "y": 4
        posZ: 5
        components:
          nav_link:
            endX: 4
            endY: -4
            endZ: 0
            radius: 0.5
            bidirectional: false
            cost: 2.5
            area: 7
            flags: 8
            linkID: 101
            userData: drop
`
	doc, err := sceneformat.ParseWith([]byte(validYAML), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith valid nav_link: %v", err)
	}

	links3D, err := sceneformat.ExtractNavLinks3D(doc)
	if err != nil {
		t.Fatalf("ExtractNavLinks3D: %v", err)
	}
	if len(links3D) != 1 {
		t.Fatalf("got %d links, want 1", len(links3D))
	}
	l := links3D[0]
	if !l.Start.ApproxEqual(math3d.Vec3{X: 9, Y: 4, Z: 5}, 1e-5) ||
		!l.End.ApproxEqual(math3d.Vec3{X: 13, Y: 0, Z: 5}, 1e-5) {
		t.Fatalf("endpoints = %v -> %v, want (9,4,5)->(13,0,5)", l.Start, l.End)
	}
	if l.Radius != 0.5 || l.Bidirectional || l.Cost != 2.5 || l.Area != ai.NavAreaJump ||
		l.Flags != uint32(ai.NavFlagJump) || l.ID != 101 || l.UserData != "drop" {
		t.Fatalf("extracted link mismatch: %+v", l)
	}

	// Unknown field and out-of-range values must be rejected.
	for _, tc := range []struct {
		name, body, want string
	}{
		{"unknown field", "foo: 1", "foo"},
		{"negative radius", "radius: -1", "radius"},
		{"negative cost", "cost: -0.5", "cost"},
		{"area above 255", "area: 300", "area"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "version: 2\nlayers:\n  - name: l\n    objects:\n      - id: o\n        components:\n          nav_link:\n            " + tc.body + "\n"
			_, err := sceneformat.ParseWith([]byte(src), sceneformat.ParseOptions{Components: reg})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseWith(%s): err = %v, want mentioning %q", tc.name, err, tc.want)
			}
		})
	}
}

func TestExtractNavLinks2DAnd3DWithHierarchyAndInstancing(t *testing.T) {
	// 2D rotated parent + child nav_link, wired into an ai.NavMesh.
	halfPi := float32(math.Pi / 2)
	doc2D := &sceneformat.SceneDoc{
		Version: 2,
		Layers: []sceneformat.LayerDoc{
			{
				Name: "level",
				Objects: []sceneformat.ObjectDoc{
					{
						ID:       "platform",
						X:        10,
						Y:        5,
						Rotation: halfPi, // 90 deg CCW: local (0, -10) -> world (10, 0) => (20, 5)
						Children: []sceneformat.ObjectDoc{
							{
								ID: "jump_link",
								Components: map[string]any{
									sceneformat.NavLinkComponentName: map[string]any{
										"startX":   0.0,
										"startY":   1.0, // rotated -> (-1, 0) => world (9, 5)
										"endX":     0.0,
										"endY":     -11.0, // rotated -> (+11, 0) => world (21, 5)
										"area":     int(ai.NavAreaJump),
										"linkID":   9,
										"userData": "jump",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	links2D, err := sceneformat.ExtractNavLinks2D(doc2D)
	if err != nil {
		t.Fatalf("ExtractNavLinks2D: %v", err)
	}
	if len(links2D) != 1 {
		t.Fatalf("got %d 2D links, want 1", len(links2D))
	}
	if math.Hypot(float64(links2D[0].Start.X-9), float64(links2D[0].Start.Y-5)) > 1e-4 ||
		math.Hypot(float64(links2D[0].End.X-21), float64(links2D[0].End.Y-5)) > 1e-4 {
		t.Fatalf("2D transformed endpoints = %v -> %v, want (9,5)->(21,5)", links2D[0].Start, links2D[0].End)
	}

	nav2D := ai.BuildNavMesh([][]core.Point{
		{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}},
		{{X: 20, Y: 0}, {X: 30, Y: 0}, {X: 30, Y: 10}, {X: 20, Y: 10}},
	})
	for _, l := range links2D {
		if !nav2D.AddOffMeshLink(l) {
			t.Fatalf("AddOffMeshLink(%+v) returned false", l)
		}
	}
	res2D := nav2D.FindPathWithOptions(core.Point{X: 2, Y: 5}, core.Point{X: 28, Y: 5}, ai.PathOptions{})
	if !res2D.Found || len(res2D.Links) != 1 || res2D.Links[0].UserData != "jump" {
		t.Fatalf("2D path via scene nav_link failed: %+v", res2D)
	}

	// 3D instanced prefab scene carrying a bidirectional ladder nav_link.
	fsys := fstest.MapFS{
		"prefabs/ladder.scene.yaml": {Data: []byte(`version: 2
layers:
  - name: prefab
    objects:
      - id: link
        components:
          nav_link:
            startX: 0
            startY: 0
            startZ: 0
            endX: 0
            endY: 4
            endZ: 0
            radius: 0.5
            bidirectional: true
            area: 8
            linkID: 33
            userData: ladder
`)},
	}
	level := &sceneformat.SceneDoc{
		Version: 2,
		Layers: []sceneformat.LayerDoc{
			{
				Name: "world",
				Objects: []sceneformat.ObjectDoc{
					{
						ID:    "tower_ladder",
						X:     5,
						Y:     0,
						PosZ:  5,
						Scene: "prefabs/ladder.scene.yaml",
					},
				},
			},
		},
	}
	resolved, err := sceneformat.Resolve(level, sceneformat.FSScenes{FS: fsys})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	links3D, err := sceneformat.ExtractNavLinks3D(resolved)
	if err != nil {
		t.Fatalf("ExtractNavLinks3D: %v", err)
	}
	if len(links3D) != 1 {
		t.Fatalf("got %d 3D links, want 1", len(links3D))
	}
	nav3D := world3d.BuildNavMesh([][]math3d.Vec3{
		{{X: 0, Y: 0, Z: 0}, {X: 0, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 10}, {X: 10, Y: 0, Z: 0}},
		{{X: 0, Y: 4, Z: 0}, {X: 0, Y: 4, Z: 10}, {X: 10, Y: 4, Z: 10}, {X: 10, Y: 4, Z: 0}},
	}, math3d.DegToRad(45))
	for _, l := range links3D {
		if !nav3D.AddOffMeshLink(l) {
			t.Fatalf("3D AddOffMeshLink(%+v) returned false", l)
		}
	}
	res3D := nav3D.FindPathWithOptions(math3d.Vec3{X: 1, Y: 0, Z: 1}, math3d.Vec3{X: 9, Y: 4, Z: 9}, ai.PathOptions{})
	if !res3D.Found || len(res3D.Links) != 1 || res3D.Links[0].UserData != "ladder" || res3D.Links[0].LinkID != 33 {
		t.Fatalf("3D path via instanced scene ladder failed: %+v", res3D)
	}
}

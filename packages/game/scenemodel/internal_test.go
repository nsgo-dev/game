package scenemodel

import (
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
)

func TestTriangleListUnrollsStripsAndFans(t *testing.T) {
	cases := []struct {
		name    string
		mode    gltf.PrimitiveMode
		indices []uint32
		count   int
		want    []uint32
	}{
		{"list keeps indices", gltf.ModeTriangles, []uint32{2, 1, 0}, 3, []uint32{2, 1, 0}},
		{"strip", gltf.ModeTriangleStrip, nil, 5, []uint32{0, 1, 2, 2, 1, 3, 2, 3, 4}},
		{"indexed strip", gltf.ModeTriangleStrip, []uint32{4, 3, 2, 1}, 5, []uint32{4, 3, 2, 2, 3, 1}},
		{"fan", gltf.ModeTriangleFan, nil, 5, []uint32{0, 1, 2, 0, 2, 3, 0, 3, 4}},
	}
	for _, tc := range cases {
		got, err := triangleList(tc.mode, tc.indices, tc.count)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%s: triangleList = %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
	if _, err := triangleList(gltf.ModeTriangleFan, nil, 2); err == nil {
		t.Error("a fan of 2 vertices was accepted")
	}
}

func TestFlatShadedGivesFaceNormals(t *testing.T) {
	m := flatShaded(render3d.Mesh{
		Positions: []math3d.Vec3{{}, {X: 1}, {Y: 1}, {Z: 1}},
		UVs:       []math3d.Vec2{{}, {X: 1}, {Y: 1}, {X: 1, Y: 1}},
		Indices:   []uint32{0, 1, 2, 0, 3, 1},
	})
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(m.Positions) != 6 || len(m.Indices) != 0 || m.UVs[4] != (math3d.Vec2{X: 1, Y: 1}) {
		t.Fatalf("flatShaded = %+v, want six unindexed corners keeping their UVs", m)
	}
	for i, want := range []math3d.Vec3{{Z: 1}, {Z: 1}, {Z: 1}, {Y: 1}, {Y: 1}, {Y: 1}} {
		if !m.Normals[i].ApproxEqual(want, 1e-6) {
			t.Errorf("normal %d = %v, want %v", i, m.Normals[i], want)
		}
	}
}

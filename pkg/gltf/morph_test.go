package gltf_test

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/gltf"
	"github.com/nsgo-dev/game/pkg/math3d"
)

func loadAnimatedMorphCube(t *testing.T) *gltf.Document {
	t.Helper()
	dir := filepath.Join("testdata", "khronos", "AnimatedMorphCube", "glTF")
	doc, err := gltf.LoadFS(os.DirFS(dir), "AnimatedMorphCube.gltf")
	if err != nil {
		t.Fatalf("LoadFS AnimatedMorphCube: %v", err)
	}
	return doc
}

func TestAnimatedMorphCubePlayback(t *testing.T) {
	doc := loadAnimatedMorphCube(t)

	if len(doc.Meshes) != 1 || len(doc.Meshes[0].Primitives) != 1 {
		t.Fatalf("meshes=%d prims=%d, want 1 mesh with 1 primitive", len(doc.Meshes), len(doc.Meshes[0].Primitives))
	}
	prim := doc.Meshes[0].Primitives[0]
	if len(prim.Positions) != 24 || len(prim.Normals) != 24 || len(prim.Tangents) != 24 || len(prim.Indices) != 36 {
		t.Fatalf("prim counts pos=%d norm=%d tan=%d idx=%d, want 24/24/24/36",
			len(prim.Positions), len(prim.Normals), len(prim.Tangents), len(prim.Indices))
	}
	if len(prim.Targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(prim.Targets))
	}
	for ti, target := range prim.Targets {
		if len(target.Positions) != 24 || len(target.Normals) != 24 || len(target.Tangents) != 24 {
			t.Fatalf("target %d counts pos=%d norm=%d tan=%d, want 24/24/24",
				ti, len(target.Positions), len(target.Normals), len(target.Tangents))
		}
	}
	if len(doc.Animations) != 1 || doc.Animations[0].Name != "Square" {
		t.Fatalf("animations = %+v, want 1 animation named Square", doc.Animations)
	}
	anim := doc.Animations[0]
	if anim.Duration < 4.0 {
		t.Fatalf("animation duration = %v, want > 4.0s", anim.Duration)
	}

	// Default weights follow mesh.Weights = [0, 0].
	defaults := gltf.ResolveWeights(doc, 0)
	if len(defaults) != 2 || defaults[0] != 0 || defaults[1] != 0 {
		t.Fatalf("ResolveWeights(0) = %v, want [0 0]", defaults)
	}

	// Find peak times for target 0 ("thin") and target 1 ("angle") in the animation track.
	ch := anim.Channels[0]
	var peak0T, peak1T float32
	var maxW0, maxW1 float32
	for k, ts := range ch.Times {
		w0 := ch.Values[k*2]
		w1 := ch.Values[k*2+1]
		if w0 > maxW0 {
			maxW0 = w0
			peak0T = ts
		}
		if w1 > maxW1 {
			maxW1 = w1
			peak1T = ts
		}
	}
	if maxW0 < 0.9 || maxW1 < 0.9 {
		t.Fatalf("expected both targets to reach near 1.0, got maxW0=%v maxW1=%v", maxW0, maxW1)
	}

	// Evaluate t=0, mid-cycle, peak target 0, peak target 1, end, and wrapped loop time.
	sampleTimes := []float32{
		0,
		peak0T * 0.5,
		peak0T,
		(peak0T + peak1T) * 0.5,
		peak1T,
		anim.Duration,
		float32(math.Mod(float64(anim.Duration+0.75), float64(anim.Duration))),
	}

	var weights []float32
	var res gltf.MorphResult
	baseHeight := prim.Bounds.Size().Y
	sawThinned := false

	for _, ts := range sampleTimes {
		var ok bool
		weights, ok = gltf.SampleMorphWeights(anim, 0, ts, weights)
		if !ok || len(weights) != 2 {
			t.Fatalf("SampleMorphWeights(t=%v) ok=%v len=%d", ts, ok, len(weights))
		}
		gltf.ApplyMorphWeights(prim, weights, &res)

		// Verify exact glTF 2.0 blending equations for every vertex.
		for i := range prim.Positions {
			wantPos := prim.Positions[i].
				Add(prim.Targets[0].Positions[i].Scale(weights[0])).
				Add(prim.Targets[1].Positions[i].Scale(weights[1]))
			if !res.Positions[i].ApproxEqual(wantPos, 1e-6) {
				t.Fatalf("t=%v v=%d pos=%v want=%v", ts, i, res.Positions[i], wantPos)
			}

			wantNorm := prim.Normals[i].
				Add(prim.Targets[0].Normals[i].Scale(weights[0])).
				Add(prim.Targets[1].Normals[i].Scale(weights[1]))
			if weights[0] != 0 || weights[1] != 0 {
				wantNorm = wantNorm.Norm()
			}
			if !res.Normals[i].ApproxEqual(wantNorm, 1e-5) {
				t.Fatalf("t=%v v=%d norm=%v want=%v", ts, i, res.Normals[i], wantNorm)
			}

			baseTan := math3d.Vec3{X: prim.Tangents[i].X, Y: prim.Tangents[i].Y, Z: prim.Tangents[i].Z}
			wantTanXYZ := baseTan.
				Add(prim.Targets[0].Tangents[i].Scale(weights[0])).
				Add(prim.Targets[1].Tangents[i].Scale(weights[1]))
			if weights[0] != 0 || weights[1] != 0 {
				wantTanXYZ = wantTanXYZ.Norm()
			}
			gotTanXYZ := math3d.Vec3{X: res.Tangents[i].X, Y: res.Tangents[i].Y, Z: res.Tangents[i].Z}
			if !gotTanXYZ.ApproxEqual(wantTanXYZ, 1e-5) {
				t.Fatalf("t=%v v=%d tan.xyz=%v want=%v", ts, i, gotTanXYZ, wantTanXYZ)
			}
			if res.Tangents[i].W != prim.Tangents[i].W {
				t.Fatalf("t=%v v=%d tan.w=%v want=%v (handedness must be preserved)", ts, i, res.Tangents[i].W, prim.Tangents[i].W)
			}
		}

		// Target 0 ("thin") lifts the bottom face upward, compressing local Y extent sharply.
		if res.Bounds.Size().Y < baseHeight*0.25 {
			sawThinned = true
		}
	}

	if !sawThinned {
		t.Fatalf("expected morphed cube Y extent to compress below 25%% of base height %v at peak 'thin'", baseHeight)
	}

	// Verify steady-state zero allocations for sampling, weight resolution, and morph evaluation.
	weights = make([]float32, 0, 2)
	resolved := make([]float32, 0, 2)
	var scratch gltf.MorphResult
	weights, _ = gltf.SampleMorphWeights(anim, 0, peak0T, weights)
	gltf.ApplyMorphWeights(prim, weights, &scratch)
	resolved = gltf.ResolveWeightsInto(doc, 0, resolved)

	allocs := testing.AllocsPerRun(50, func() {
		weights, _ = gltf.SampleMorphWeights(anim, 0, peak0T*0.5, weights)
		resolved = gltf.ResolveWeightsInto(doc, 0, resolved)
		gltf.ApplyMorphWeights(prim, weights, &scratch)
	})
	if allocs != 0 {
		t.Errorf("steady-state morph evaluation allocated %v times, want 0", allocs)
	}
}

func TestMorphTargetsWithSparseAccessors(t *testing.T) {
	// 4 base vertices; sparse morph target 0 moves only vertices 1 and 3
	// using a zero-initialized (no bufferView) sparse accessor, and sparse
	// morph target 1 overrides vertex 2 on top of a dense base bufferView.
	var bin bytes.Buffer

	posOff := appendFloat32s(&bin,
		0, 0, 0,
		1, 0, 0,
		1, 1, 0,
		0, 1, 0,
	)
	posLen := bin.Len() - posOff

	normOff := appendFloat32s(&bin,
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
	)
	normLen := bin.Len() - normOff

	tanOff := appendFloat32s(&bin,
		1, 0, 0, 1,
		1, 0, 0, -1,
		1, 0, 0, 1,
		1, 0, 0, -1,
	)
	tanLen := bin.Len() - tanOff

	// Target 0 sparse indices: [1, 3] (UNSIGNED_SHORT)
	t0IdxOff := appendUint16s(&bin, 1, 3)
	t0IdxLen := bin.Len() - t0IdxOff

	// Target 0 sparse position deltas for vertices 1 and 3: (+2, 0, 0), (0, +4, 0)
	t0ValOff := appendFloat32s(&bin,
		2, 0, 0,
		0, 4, 0,
	)
	t0ValLen := bin.Len() - t0ValOff

	// Target 1 base deltas: (0, 0, 1) on every vertex, sparse override on vertex 2 -> (0, 0, 5)
	t1BaseOff := appendFloat32s(&bin,
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
	)
	t1BaseLen := bin.Len() - t1BaseOff

	t1IdxOff := appendUint16s(&bin, 2)
	t1IdxLen := bin.Len() - t1IdxOff

	t1ValOff := appendFloat32s(&bin, 0, 0, 5)
	t1ValLen := bin.Len() - t1ValOff

	raw := map[string]any{
		"asset":   map[string]any{"version": "2.0"},
		"buffers": []any{map[string]any{"byteLength": bin.Len()}},
		"bufferViews": []any{
			map[string]any{"buffer": 0, "byteOffset": posOff, "byteLength": posLen},
			map[string]any{"buffer": 0, "byteOffset": normOff, "byteLength": normLen},
			map[string]any{"buffer": 0, "byteOffset": tanOff, "byteLength": tanLen},
			map[string]any{"buffer": 0, "byteOffset": t0IdxOff, "byteLength": t0IdxLen},
			map[string]any{"buffer": 0, "byteOffset": t0ValOff, "byteLength": t0ValLen},
			map[string]any{"buffer": 0, "byteOffset": t1BaseOff, "byteLength": t1BaseLen},
			map[string]any{"buffer": 0, "byteOffset": t1IdxOff, "byteLength": t1IdxLen},
			map[string]any{"buffer": 0, "byteOffset": t1ValOff, "byteLength": t1ValLen},
		},
		"accessors": []any{
			map[string]any{"bufferView": 0, "componentType": 5126, "count": 4, "type": "VEC3"},
			map[string]any{"bufferView": 1, "componentType": 5126, "count": 4, "type": "VEC3"},
			map[string]any{"bufferView": 2, "componentType": 5126, "count": 4, "type": "VEC4"},
			// 3: zero-base sparse VEC3
			map[string]any{
				"componentType": 5126,
				"count":         4,
				"type":          "VEC3",
				"sparse": map[string]any{
					"count":   2,
					"indices": map[string]any{"bufferView": 3, "componentType": 5123},
					"values":  map[string]any{"bufferView": 4},
				},
			},
			// 4: dense base + sparse override VEC3
			map[string]any{
				"bufferView":    5,
				"componentType": 5126,
				"count":         4,
				"type":          "VEC3",
				"sparse": map[string]any{
					"count":   1,
					"indices": map[string]any{"bufferView": 6, "componentType": 5123},
					"values":  map[string]any{"bufferView": 7},
				},
			},
		},
		"meshes": []any{
			map[string]any{
				"weights": []float32{0.5, 0.25},
				"primitives": []any{
					map[string]any{
						"attributes": map[string]int{"POSITION": 0, "NORMAL": 1, "TANGENT": 2},
						"targets": []any{
							map[string]int{"POSITION": 3},
							map[string]int{"POSITION": 4},
						},
					},
				},
			},
		},
		"nodes": []any{
			map[string]any{"mesh": 0},
		},
	}

	jsonBytes, _ := json.Marshal(raw)
	doc, err := gltf.Load(buildGLB(jsonBytes, bin.Bytes()), nil)
	if err != nil {
		t.Fatalf("Load sparse morph GLB: %v", err)
	}

	weights := gltf.ResolveWeights(doc, 0)
	res := gltf.ApplyMorphWeights(doc.Meshes[0].Primitives[0], weights, nil)
	want := []math3d.Vec3{
		{X: 0, Y: 0, Z: 0.25},
		{X: 2, Y: 0, Z: 0.25},
		{X: 1, Y: 1, Z: 1.25},
		{X: 0, Y: 3, Z: 0.25},
	}
	for i := range want {
		if !res.Positions[i].ApproxEqual(want[i], 1e-6) {
			t.Fatalf("sparse morphed pos[%d] = %v, want %v", i, res.Positions[i], want[i])
		}
	}
}

func TestSampleMorphWeightsInterpolations(t *testing.T) {
	// 3 targets per keyframe across STEP, LINEAR, and CUBICSPLINE.
	t.Run("STEP", func(t *testing.T) {
		anim := gltf.Animation{
			Duration: 2.0,
			Channels: []gltf.AnimationChannel{
				{
					TargetNode:    1,
					Path:          gltf.PathWeights,
					Interpolation: gltf.InterpolationStep,
					Times:         []float32{0.0, 1.0, 2.0},
					Values: []float32{
						0.1, 0.2, 0.3,
						0.4, 0.5, 0.6,
						0.7, 0.8, 0.9,
					},
					Stride: 3,
				},
			},
		}
		cases := []struct {
			t    float32
			want []float32
		}{
			{-1.0, []float32{0.1, 0.2, 0.3}},
			{0.0, []float32{0.1, 0.2, 0.3}},
			{0.999, []float32{0.1, 0.2, 0.3}},
			{1.0, []float32{0.4, 0.5, 0.6}},
			{1.5, []float32{0.4, 0.5, 0.6}},
			{2.0, []float32{0.7, 0.8, 0.9}},
			{5.0, []float32{0.7, 0.8, 0.9}},
			{float32(math.NaN()), []float32{0.1, 0.2, 0.3}},
		}
		var buf []float32
		for _, tc := range cases {
			got, ok := gltf.SampleMorphWeights(anim, 1, tc.t, buf)
			if !ok {
				t.Fatalf("t=%v: ok=false, want true", tc.t)
			}
			buf = got
			for k := range tc.want {
				if !math3d.ApproxEqual(got[k], tc.want[k], 1e-6) {
					t.Errorf("STEP t=%v w[%d]=%v, want %v", tc.t, k, got[k], tc.want[k])
				}
			}
		}
	})

	t.Run("LINEAR", func(t *testing.T) {
		anim := gltf.Animation{
			Duration: 3.0,
			Channels: []gltf.AnimationChannel{
				{
					TargetNode:    2,
					Path:          gltf.PathTranslation, // non-weights channel ignored
					Interpolation: gltf.InterpolationLinear,
					Times:         []float32{0, 1},
					Values:        []float32{0, 0, 0, 1, 1, 1},
					Stride:        3,
				},
				{
					TargetNode:    2,
					Path:          gltf.PathWeights,
					Interpolation: gltf.InterpolationLinear,
					Times:         []float32{0.0, 1.0, 3.0},
					Values: []float32{
						0.0, 1.0, 0.5,
						1.0, 0.0, 0.5,
						0.2, 0.8, 0.1,
					},
					Stride: 3,
				},
			},
		}
		got, ok := gltf.SampleMorphWeights(anim, 2, 2.0, nil)
		if !ok {
			t.Fatal("ok=false, want true")
		}
		// Midpoint of [1, 3]: average of [1.0, 0.0, 0.5] and [0.2, 0.8, 0.1] = [0.6, 0.4, 0.3]
		want := []float32{0.6, 0.4, 0.3}
		for k := range want {
			if !math3d.ApproxEqual(got[k], want[k], 1e-6) {
				t.Errorf("LINEAR w[%d]=%v, want %v", k, got[k], want[k])
			}
		}
	})

	t.Run("CUBICSPLINE", func(t *testing.T) {
		// 2 keyframes at t=0 and t=2 (dt=2), 3 targets per keyframe => 3*3 = 9 floats per keyframe:
		// key 0: in=[0,0,0], v0=[0, 1, 0.5], out=[1, -0.5, 0]
		// key 1: in=[0.5, 0, -1], v1=[1, 0, 0.5], out=[0,0,0]
		ch := gltf.AnimationChannel{
			TargetNode:    0,
			Path:          gltf.PathWeights,
			Interpolation: gltf.InterpolationCubicSpline,
			Times:         []float32{0.0, 2.0},
			Values: []float32{
				// key 0: in, val, out
				0, 0, 0,
				0, 1, 0.5,
				1, -0.5, 0,
				// key 1: in, val, out
				0.5, 0, -1,
				1, 0, 0.5,
				0, 0, 0,
			},
			Stride: 3,
		}
		anim := gltf.Animation{Duration: 2.0, Channels: []gltf.AnimationChannel{ch}}

		// At s=0.5 (t=1.0), dt=2:
		// h00 = 0.5, h10 = 0.125, h01 = 0.5, h11 = -0.125
		// w0 = 0.5*0   + 0.125*2*1    + 0.5*1   + (-0.125)*2*0.5 = 0.625
		// w1 = 0.5*1   + 0.125*2*(-0.5) + 0.5*0 + (-0.125)*2*0   = 0.375
		// w2 = 0.5*0.5 + 0.125*2*0    + 0.5*0.5 + (-0.125)*2*(-1)= 0.75
		wantMid := []float32{0.625, 0.375, 0.75}
		got, ok := gltf.SampleMorphWeights(anim, 0, 1.0, nil)
		if !ok {
			t.Fatal("CUBICSPLINE ok=false")
		}
		for k := range wantMid {
			if !math3d.ApproxEqual(got[k], wantMid[k], 1e-6) {
				t.Errorf("CUBICSPLINE mid w[%d]=%v, want %v", k, got[k], wantMid[k])
			}
		}

		// Cross-check agreement with AnimationChannel.Sample across timestamps.
		for _, ts := range []float32{-0.5, 0, 0.25, 1.0, 1.75, 2.0, 3.0} {
			got, ok := gltf.SampleMorphWeights(anim, 0, ts, got)
			if !ok {
				t.Fatalf("t=%v: ok=false", ts)
			}
			ref := ch.Sample(ts).Weights
			for k := range ref {
				if !math3d.ApproxEqual(got[k], ref[k], 1e-6) {
					t.Errorf("t=%v w[%d]=%v, ref=%v", ts, k, got[k], ref[k])
				}
			}
		}
	})

	t.Run("missing and malformed channels", func(t *testing.T) {
		anim := gltf.Animation{
			Channels: []gltf.AnimationChannel{
				{TargetNode: 0, Path: gltf.PathWeights, Times: []float32{0, 1}, Values: []float32{1}, Stride: 2}, // truncated
				{TargetNode: 1, Path: gltf.PathWeights, Times: nil, Values: nil, Stride: 2},
			},
		}
		if _, ok := gltf.SampleMorphWeights(anim, 0, 0.5, nil); ok {
			t.Error("expected false for truncated values")
		}
		if _, ok := gltf.SampleMorphWeights(anim, 1, 0.5, nil); ok {
			t.Error("expected false for empty times")
		}
		if _, ok := gltf.SampleMorphWeights(anim, 99, 0.5, nil); ok {
			t.Error("expected false for unmatched node")
		}
	})
}

func TestResolveWeightsPrecedence(t *testing.T) {
	twoTargetMesh := gltf.Mesh{
		Weights: []float32{0.2, 0.8},
		Primitives: []gltf.Primitive{{
			Positions: []math3d.Vec3{{}, {X: 1}, {Y: 1}},
			Targets:   []gltf.MorphTarget{{}, {}},
		}},
	}
	noDefaultMesh := gltf.Mesh{
		Primitives: []gltf.Primitive{{
			Positions: []math3d.Vec3{{}, {X: 1}, {Y: 1}},
			Targets:   []gltf.MorphTarget{{}, {}, {}},
		}},
	}
	staticMesh := gltf.Mesh{
		Primitives: []gltf.Primitive{{Positions: []math3d.Vec3{{}, {X: 1}, {Y: 1}}}},
	}

	doc := &gltf.Document{
		Meshes: []gltf.Mesh{twoTargetMesh, noDefaultMesh, staticMesh},
		Nodes: []gltf.Node{
			{Mesh: 0, Weights: []float32{0.9, 0.1}}, // node overrides mesh
			{Mesh: 0},                               // falls back to mesh.Weights
			{Mesh: 1},                               // both empty -> zeros(3)
			{Mesh: 1, Weights: []float32{0.7}},      // shorter node.Weights -> zero padded
			{Mesh: 0, Weights: []float32{1, 2, 3}},  // longer node.Weights -> truncated to 2
			{Mesh: 2},                               // mesh with 0 targets -> nil
			{Mesh: -1},                              // no mesh -> nil
		},
	}

	cases := []struct {
		name string
		doc  *gltf.Document
		node int
		want []float32
	}{
		{"node overrides mesh", doc, 0, []float32{0.9, 0.1}},
		{"mesh fallback", doc, 1, []float32{0.2, 0.8}},
		{"zeros fallback", doc, 2, []float32{0, 0, 0}},
		{"shorter padded", doc, 3, []float32{0.7, 0, 0}},
		{"longer truncated", doc, 4, []float32{1, 2}},
		{"no targets", doc, 5, nil},
		{"no mesh", doc, 6, nil},
		{"negative node", doc, -1, nil},
		{"oob node", doc, 99, nil},
		{"nil doc", nil, 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gltf.ResolveWeights(tc.doc, tc.node)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("w[%d] = %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}

	// Mutating resolved slice must not mutate document weights.
	w := gltf.ResolveWeights(doc, 0)
	w[0] = -99
	if doc.Nodes[0].Weights[0] != 0.9 {
		t.Fatal("ResolveWeights aliased node.Weights")
	}
}

func TestEvaluateMorphTangentsAndDegenerateCases(t *testing.T) {
	prim := gltf.Primitive{
		Positions: []math3d.Vec3{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 0}},
		Tangents: []math3d.Vec4{
			{X: 1, Y: 0, Z: 0, W: 1},
			{X: 1, Y: 0, Z: 0, W: -1},
		},
		Targets: []gltf.MorphTarget{
			{
				Tangents: []math3d.Vec3{
					{X: 0, Y: 1, Z: 0},  // (1,0,0)+(0,1,0) -> (1/sqrt2, 1/sqrt2, 0), w=+1
					{X: -1, Y: 0, Z: 0}, // cancels to (0,0,0), w=-1
				},
			},
		},
	}

	// Zero weight preserves exact base tangents without altering W.
	zero := gltf.EvaluateMorphTangents(prim, []float32{0}, nil)
	if zero[0] != prim.Tangents[0] || zero[1] != prim.Tangents[1] {
		t.Fatalf("zero weight tangents = %v, want %v", zero, prim.Tangents)
	}

	// Weight 1 blends, renormalizes XYZ, handles degenerate zero vector without NaN, and preserves W.
	morphed := gltf.EvaluateMorphTangents(prim, []float32{1}, zero)
	invSqrt2 := float32(1 / math.Sqrt2)
	want0 := math3d.Vec4{X: invSqrt2, Y: invSqrt2, Z: 0, W: 1}
	if !math3d.ApproxEqual(morphed[0].X, want0.X, 1e-6) ||
		!math3d.ApproxEqual(morphed[0].Y, want0.Y, 1e-6) ||
		!math3d.ApproxEqual(morphed[0].Z, want0.Z, 1e-6) ||
		morphed[0].W != 1 {
		t.Fatalf("morphed[0] = %v, want %v", morphed[0], want0)
	}
	if morphed[1] != (math3d.Vec4{X: 0, Y: 0, Z: 0, W: -1}) {
		t.Fatalf("morphed[1] = %v, want {0 0 0 -1}", morphed[1])
	}

	// Empty base tangents returns empty slice.
	if got := gltf.EvaluateMorphTangents(gltf.Primitive{}, []float32{1}, nil); len(got) != 0 {
		t.Fatalf("empty prim tangents len = %d, want 0", len(got))
	}
}

package anim3d_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/anim3d"
	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// Compile-time check that *anim3d.Player satisfies anim3d.PoseSampler.
var _ anim3d.PoseSampler = (*anim3d.Player)(nil)

// gltfBinBuilder packs little-endian binary data and records glTF bufferViews
// and accessors for building valid glTF 2.0 fixtures loaded through gltf.Load.
type gltfBinBuilder struct {
	buf         bytes.Buffer
	bufferViews []map[string]any
	accessors   []map[string]any
}

func (b *gltfBinBuilder) align4() {
	for b.buf.Len()%4 != 0 {
		b.buf.WriteByte(0)
	}
}

func (b *gltfBinBuilder) addFloatAccessor(accType string, count int, vals []float32, withMinMax bool) int {
	b.align4()
	offset := b.buf.Len()
	for _, v := range vals {
		_ = binary.Write(&b.buf, binary.LittleEndian, v)
	}
	byteLen := b.buf.Len() - offset
	bvIdx := len(b.bufferViews)
	b.bufferViews = append(b.bufferViews, map[string]any{
		"buffer":     0,
		"byteOffset": offset,
		"byteLength": byteLen,
	})
	acc := map[string]any{
		"bufferView":    bvIdx,
		"componentType": int(gltf.ComponentFloat),
		"count":         count,
		"type":          accType,
	}
	if withMinMax && count > 0 {
		comps := len(vals) / count
		minV := make([]float32, comps)
		maxV := make([]float32, comps)
		copy(minV, vals[:comps])
		copy(maxV, vals[:comps])
		for i := 1; i < count; i++ {
			for c := range comps {
				v := vals[i*comps+c]
				if v < minV[c] {
					minV[c] = v
				}
				if v > maxV[c] {
					maxV[c] = v
				}
			}
		}
		acc["min"] = minV
		acc["max"] = maxV
	}
	accIdx := len(b.accessors)
	b.accessors = append(b.accessors, acc)
	return accIdx
}

func (b *gltfBinBuilder) addUint16Accessor(accType string, count int, vals []uint16) int {
	b.align4()
	offset := b.buf.Len()
	for _, v := range vals {
		_ = binary.Write(&b.buf, binary.LittleEndian, v)
	}
	byteLen := b.buf.Len() - offset
	bvIdx := len(b.bufferViews)
	b.bufferViews = append(b.bufferViews, map[string]any{
		"buffer":     0,
		"byteOffset": offset,
		"byteLength": byteLen,
	})
	accIdx := len(b.accessors)
	b.accessors = append(b.accessors, map[string]any{
		"bufferView":    bvIdx,
		"componentType": int(gltf.ComponentUnsignedShort),
		"count":         count,
		"type":          accType,
	})
	return accIdx
}

func (b *gltfBinBuilder) dataURI() string {
	return "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(b.buf.Bytes())
}

// buildCesiumManFixture constructs a valid glTF 2.0 humanoid rig modeled after
// Khronos's CesiumMan asset:
//   - Node 0: Z_UP root node authored as a 4x4 matrix (-90 deg around X)
//   - Node 1: Cesium_Man skinned mesh node under Z_UP (skin 0, mesh 0)
//   - Nodes 2..10: 9-bone skeleton (pelvis, chest, head, L/R thighs & shins, L/R arms)
//   - Exact inverseBindMatrices matching the bind pose
//   - Animation 0 ("Walk", 2.0s): CUBICSPLINE pelvis bob, LINEAR opposite-phase
//     leg & arm rotations (with hemisphere-flipped quaternion keyframes), LINEAR
//     chest scale breathing, STEP head turn, plus timed footstep extras events
//   - Animation 1 ("Idle", 1.0s): calm upright idle pose for crossfade tests
func buildCesiumManFixture(t testing.TB) *gltf.Document {
	t.Helper()

	var bin gltfBinBuilder

	// Minimal skinned triangle mesh referencing joints 0..8.
	posAcc := bin.addFloatAccessor("VEC3", 3, []float32{
		0, 0, 0,
		0.5, 0, 0,
		0, 1.7, 0,
	}, true)
	jointsAcc := bin.addUint16Accessor("VEC4", 3, []uint16{
		0, 3, 5, 0,
		1, 7, 8, 0,
		2, 4, 6, 0,
	})
	weightsAcc := bin.addFloatAccessor("VEC4", 3, []float32{
		0.6, 0.2, 0.2, 0,
		0.6, 0.2, 0.2, 0,
		0.6, 0.2, 0.2, 0,
	}, false)
	idxAcc := bin.addUint16Accessor("SCALAR", 3, []uint16{0, 1, 2})

	// Build bind-pose transforms to compute exact glTF inverseBindMatrices:
	// IBM[j] = inverse(W_bind[joint_j]) * W_bind[meshNode].
	zUp := math3d.RotateX(-math.Pi / 2)
	wMesh := zUp // Node 1 is a child of Z_UP with identity local transform.

	type boneDef struct {
		name     string
		parent   int // node index
		pos      math3d.Vec3
		children []int
	}
	// Node indices:
	// 0: Z_UP (children: 1, 2)
	// 1: Cesium_Man (mesh: 0, skin: 0)
	// 2: Skeleton_torso_joint_1 (pelvis, children: 3, 5, 7)
	// 3: Skeleton_torso_joint_2 (chest, children: 4, 9, 10)
	// 4: Skeleton_neck_joint (head)
	// 5: Skeleton_leg_L_thigh (children: 6)
	// 6: Skeleton_leg_L_shin
	// 7: Skeleton_leg_R_thigh (children: 8)
	// 8: Skeleton_leg_R_shin
	// 9: Skeleton_arm_L
	// 10: Skeleton_arm_R
	bones := []boneDef{
		{name: "Skeleton_torso_joint_1", parent: 0, pos: math3d.Vec3{X: 0, Y: 0, Z: 1.0}, children: []int{3, 5, 7}},
		{name: "Skeleton_torso_joint_2", parent: 2, pos: math3d.Vec3{X: 0, Y: 0, Z: 0.4}, children: []int{4, 9, 10}},
		{name: "Skeleton_neck_joint", parent: 3, pos: math3d.Vec3{X: 0, Y: 0, Z: 0.3}},
		{name: "Skeleton_leg_L_thigh", parent: 2, pos: math3d.Vec3{X: -0.15, Y: 0, Z: -0.1}, children: []int{6}},
		{name: "Skeleton_leg_L_shin", parent: 5, pos: math3d.Vec3{X: 0, Y: 0, Z: -0.45}},
		{name: "Skeleton_leg_R_thigh", parent: 2, pos: math3d.Vec3{X: 0.15, Y: 0, Z: -0.1}, children: []int{8}},
		{name: "Skeleton_leg_R_shin", parent: 7, pos: math3d.Vec3{X: 0, Y: 0, Z: -0.45}},
		{name: "Skeleton_arm_L", parent: 3, pos: math3d.Vec3{X: -0.25, Y: 0, Z: 0.2}},
		{name: "Skeleton_arm_R", parent: 3, pos: math3d.Vec3{X: 0.25, Y: 0, Z: 0.2}},
	}

	worldBind := make([]math3d.Mat4, 2+len(bones))
	worldBind[0] = zUp
	worldBind[1] = wMesh
	for i, b := range bones {
		nodeIdx := 2 + i
		worldBind[nodeIdx] = worldBind[b.parent].Mul(math3d.Translate(b.pos))
	}

	ibmFloats := make([]float32, 0, len(bones)*16)
	jointNodes := make([]int, len(bones))
	for i := range bones {
		nodeIdx := 2 + i
		jointNodes[i] = nodeIdx
		invJoint, ok := worldBind[nodeIdx].Inverse()
		if !ok {
			t.Fatalf("bind world matrix for node %d not invertible", nodeIdx)
		}
		ibm := invJoint.Mul(wMesh)
		ibmFloats = append(ibmFloats, ibm[:]...)
	}
	ibmAcc := bin.addFloatAccessor("MAT4", len(bones), ibmFloats, false)

	// Walk cycle keyframes (duration = 2.0s: 0.0, 0.5, 1.0, 1.5, 2.0).
	walkTimesAcc := bin.addFloatAccessor("SCALAR", 5, []float32{0.0, 0.5, 1.0, 1.5, 2.0}, true)

	// Pelvis translation (CUBICSPLINE): vertical bob (Z in Z-up space) + slight X sway.
	// 5 keyframes * 3 (inTangent, value, outTangent) * 3 floats.
	pelvisCubicVals := []float32{
		// t=0.0
		0, 0, 0, 0.0, 0.0, 1.00, 0, 0, 0.16,
		// t=0.5 (left step peak)
		0, 0, 0, -0.05, 0.0, 1.08, 0, 0, 0,
		// t=1.0 (mid stride)
		0, 0, -0.16, 0.0, 0.0, 1.00, 0, 0, 0.16,
		// t=1.5 (right step peak)
		0, 0, 0, 0.05, 0.0, 1.08, 0, 0, 0,
		// t=2.0 (loop closure)
		0, 0, -0.16, 0.0, 0.0, 1.00, 0, 0, 0,
	}
	pelvisPosAcc := bin.addFloatAccessor("VEC3", 15, pelvisCubicVals, false)

	// Helper to serialize quaternion keyframes.
	packQuats := func(qs ...math3d.Quat) []float32 {
		out := make([]float32, 0, len(qs)*4)
		for _, q := range qs {
			out = append(out, q.X, q.Y, q.Z, q.W)
		}
		return out
	}
	negQ := func(q math3d.Quat) math3d.Quat {
		return math3d.Quat{X: -q.X, Y: -q.Y, Z: -q.Z, W: -q.W}
	}

	xAxis := math3d.Vec3{X: 1}
	zAxis := math3d.Vec3{Z: 1}
	qId := math3d.QuatIdentity()
	qSwingFwd := math3d.QuatFromAxisAngle(xAxis, 0.6)
	qSwingBack := math3d.QuatFromAxisAngle(xAxis, -0.6)
	qKneeBend := math3d.QuatFromAxisAngle(xAxis, -0.7)

	// Left thigh: swings forward at 0.5, back at 1.5. Negate keyframe 1 to exercise
	// shortest-path hemisphere flip during LINEAR Slerp!
	thighLRotAcc := bin.addFloatAccessor("VEC4", 5, packQuats(
		qId, negQ(qSwingFwd), qId, qSwingBack, qId,
	), false)
	// Right thigh: opposite phase to left thigh.
	thighRRotAcc := bin.addFloatAccessor("VEC4", 5, packQuats(
		qId, qSwingBack, qId, negQ(qSwingFwd), qId,
	), false)
	// Shins bend during swing.
	shinLRotAcc := bin.addFloatAccessor("VEC4", 5, packQuats(
		qId, qKneeBend, qId, qId, qId,
	), false)
	shinRRotAcc := bin.addFloatAccessor("VEC4", 5, packQuats(
		qId, qId, qId, qKneeBend, qId,
	), false)
	// Arms counter-swing opposite to ipsilateral legs.
	armLRotAcc := bin.addFloatAccessor("VEC4", 5, packQuats(
		qId, math3d.QuatFromAxisAngle(xAxis, -0.4), qId, math3d.QuatFromAxisAngle(xAxis, 0.4), qId,
	), false)
	armRRotAcc := bin.addFloatAccessor("VEC4", 5, packQuats(
		qId, math3d.QuatFromAxisAngle(xAxis, 0.4), qId, math3d.QuatFromAxisAngle(xAxis, -0.4), qId,
	), false)
	// Chest subtle breathing scale (LINEAR).
	chestScaleAcc := bin.addFloatAccessor("VEC3", 5, []float32{
		1.00, 1.00, 1.00,
		1.04, 1.02, 1.00,
		1.00, 1.00, 1.00,
		1.04, 1.02, 1.00,
		1.00, 1.00, 1.00,
	}, false)
	// Head look left/right with STEP interpolation.
	headRotAcc := bin.addFloatAccessor("VEC4", 5, packQuats(
		qId,
		math3d.QuatFromAxisAngle(zAxis, 0.25),
		qId,
		math3d.QuatFromAxisAngle(zAxis, -0.25),
		qId,
	), false)

	// Idle animation (1.0s): upright pelvis + slight chest scale breath.
	idleTimesAcc := bin.addFloatAccessor("SCALAR", 2, []float32{0.0, 1.0}, true)
	idlePelvisPosAcc := bin.addFloatAccessor("VEC3", 2, []float32{
		0.0, 0.0, 1.00,
		0.0, 0.0, 1.02,
	}, false)
	idleThighRotAcc := bin.addFloatAccessor("VEC4", 2, packQuats(qId, qId), false)

	nodes := make([]map[string]any, 0, 2+len(bones))
	nodes = append(nodes,
		map[string]any{
			"name":     "Z_UP",
			"children": []int{1, 2},
			"matrix":   zUp[:],
		},
		map[string]any{
			"name": "Cesium_Man",
			"mesh": 0,
			"skin": 0,
		},
	)
	for _, b := range bones {
		n := map[string]any{
			"name":        b.name,
			"translation": []float32{b.pos.X, b.pos.Y, b.pos.Z},
		}
		if len(b.children) > 0 {
			n["children"] = b.children
		}
		nodes = append(nodes, n)
	}

	rawDoc := map[string]any{
		"asset": map[string]any{"version": "2.0", "generator": "nsgo-anim3d-test"},
		"scene": 0,
		"scenes": []map[string]any{
			{"name": "CesiumManScene", "nodes": []int{0}},
		},
		"nodes": nodes,
		"meshes": []map[string]any{
			{
				"name": "Cesium_Man_Mesh",
				"primitives": []map[string]any{
					{
						"attributes": map[string]int{
							"POSITION":  posAcc,
							"JOINTS_0":  jointsAcc,
							"WEIGHTS_0": weightsAcc,
						},
						"indices": idxAcc,
					},
				},
			},
		},
		"skins": []map[string]any{
			{
				"name":                "Cesium_Man_Skin",
				"skeleton":            2,
				"joints":              jointNodes,
				"inverseBindMatrices": ibmAcc,
			},
		},
		"animations": []map[string]any{
			{
				"name": "Walk",
				"extras": map[string]any{
					"events": []map[string]any{
						{"time": 0.5, "name": "footstep", "payload": "left"},
						{"time": 1.5, "name": "footstep", "payload": "right"},
					},
				},
				"samplers": []map[string]any{
					{"input": walkTimesAcc, "output": pelvisPosAcc, "interpolation": "CUBICSPLINE"},
					{"input": walkTimesAcc, "output": thighLRotAcc, "interpolation": "LINEAR"},
					{"input": walkTimesAcc, "output": thighRRotAcc, "interpolation": "LINEAR"},
					{"input": walkTimesAcc, "output": shinLRotAcc, "interpolation": "LINEAR"},
					{"input": walkTimesAcc, "output": shinRRotAcc, "interpolation": "LINEAR"},
					{"input": walkTimesAcc, "output": armLRotAcc, "interpolation": "LINEAR"},
					{"input": walkTimesAcc, "output": armRRotAcc, "interpolation": "LINEAR"},
					{"input": walkTimesAcc, "output": chestScaleAcc, "interpolation": "LINEAR"},
					{"input": walkTimesAcc, "output": headRotAcc, "interpolation": "STEP"},
				},
				"channels": []map[string]any{
					{"sampler": 0, "target": map[string]any{"node": 2, "path": "translation"}},
					{"sampler": 1, "target": map[string]any{"node": 5, "path": "rotation"}},
					{"sampler": 2, "target": map[string]any{"node": 7, "path": "rotation"}},
					{"sampler": 3, "target": map[string]any{"node": 6, "path": "rotation"}},
					{"sampler": 4, "target": map[string]any{"node": 8, "path": "rotation"}},
					{"sampler": 5, "target": map[string]any{"node": 9, "path": "rotation"}},
					{"sampler": 6, "target": map[string]any{"node": 10, "path": "rotation"}},
					{"sampler": 7, "target": map[string]any{"node": 3, "path": "scale"}},
					{"sampler": 8, "target": map[string]any{"node": 4, "path": "rotation"}},
				},
			},
			{
				"name": "Idle",
				"samplers": []map[string]any{
					{"input": idleTimesAcc, "output": idlePelvisPosAcc, "interpolation": "LINEAR"},
					{"input": idleTimesAcc, "output": idleThighRotAcc, "interpolation": "LINEAR"},
				},
				"channels": []map[string]any{
					{"sampler": 0, "target": map[string]any{"node": 2, "path": "translation"}},
					{"sampler": 1, "target": map[string]any{"node": 5, "path": "rotation"}},
				},
			},
		},
		"buffers": []map[string]any{
			{"byteLength": bin.buf.Len(), "uri": bin.dataURI()},
		},
		"bufferViews": bin.bufferViews,
		"accessors":   bin.accessors,
	}

	rawJSON, err := json.Marshal(rawDoc)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	doc, err := gltf.Load(rawJSON, nil)
	if err != nil {
		t.Fatalf("gltf.Load fixture: %v", err)
	}
	return doc
}

func assertMat4Finite(t *testing.T, label string, m math3d.Mat4) {
	t.Helper()
	for i, v := range m {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("%s[%d] is not finite: %v (mat=%v)", label, i, v, m)
		}
	}
}

func TestCesiumManWalkCycleDrivenByGameLoop(t *testing.T) {
	doc := buildCesiumManFixture(t)
	origPelvisPos := doc.Nodes[2].Translation
	origThighLRot := doc.Nodes[5].Rotation

	player, err := anim3d.NewPlayer(doc)
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}
	if player.Playing() {
		t.Fatal("new player should start stopped")
	}
	if player.Duration() != 2*time.Second {
		t.Fatalf("Duration = %v, want 2s", player.Duration())
	}

	// Bind pose before Play must yield identity skin matrices for every joint
	// because IBM[j] = inverse(W_bind[joint]) * W_bind[meshNode].
	bindSkin, err := player.SkinMatrices(0)
	if err != nil {
		t.Fatalf("bind SkinMatrices: %v", err)
	}
	if len(bindSkin) != 9 {
		t.Fatalf("skin joint count = %d, want 9", len(bindSkin))
	}
	for i, jm := range bindSkin {
		assertMat4Finite(t, "bindSkin", jm)
		if !jm.ApproxEqual(math3d.Mat4Identity(), 1e-4) {
			t.Fatalf("bind pose SkinMatrices[%d] = %v, want identity", i, jm)
		}
	}

	if err := player.PlayByName("Walk"); err != nil {
		t.Fatalf("PlayByName(Walk): %v", err)
	}
	if !player.Playing() || player.AnimationName() != "Walk" {
		t.Fatalf("playing=%v name=%q, want true Walk", player.Playing(), player.AnimationName())
	}

	// Drive playback from game.Loop fixed 60 Hz steps via nsgotest.
	var steps int
	var poseAtQuarter, poseAfterHalfStep, poseAtHalf, poseAtThreeQuarter, poseAtCycleEnd []math3d.Transform
	var skinAtHalf []math3d.Mat4
	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			player.Step(dt)
			steps++
			mats, err := player.SkinMatrices(0)
			if err != nil {
				t.Errorf("step %d SkinMatrices: %v", steps, err)
				return
			}
			for j, m := range mats {
				for k, v := range m {
					if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
						t.Errorf("step %d joint %d elem %d non-finite: %v", steps, j, k, v)
					}
				}
			}
			switch steps {
			case 30: // 30 * (1s/60) = 499.99998ms (20ns before 0.5s boundary)
				poseAtQuarter = player.LocalTransforms()
			case 31: // 516.67ms (strictly past 0.5s STEP boundary)
				poseAfterHalfStep = player.LocalTransforms()
			case 60: // 1.0s (mid stride)
				poseAtHalf = player.LocalTransforms()
				skinAtHalf = mats
			case 90: // 1.5s (right step peak)
				poseAtThreeQuarter = player.LocalTransforms()
			case 120: // 2.0s (wrapped back to start)
				poseAtCycleEnd = player.LocalTransforms()
			}
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		loop.Run(cx)
		return core.View{}
	})
	app.Advance(2 * time.Second)

	if steps != 120 {
		t.Fatalf("fixed steps = %d, want 120", steps)
	}

	// At t=0.5s: pelvis bobbed to (-0.05, 0, 1.08), left thigh swung +0.6 rad,
	// right thigh swung -0.6 rad, chest breathed to (1.04, 1.02, 1.0), head stepped to +0.25 rad.
	if !poseAtQuarter[2].Pos.ApproxEqual(math3d.Vec3{X: -0.05, Y: 0, Z: 1.08}, 2e-3) {
		t.Fatalf("t=0.5s pelvis pos = %v, want (-0.05, 0, 1.08)", poseAtQuarter[2].Pos)
	}
	wantThighL := math3d.QuatFromAxisAngle(math3d.Vec3{X: 1}, 0.6)
	wantThighR := math3d.QuatFromAxisAngle(math3d.Vec3{X: 1}, -0.6)
	if !poseAtQuarter[5].Rot.ApproxEqual(wantThighL, 2e-3) {
		t.Fatalf("t=0.5s L thigh rot = %v, want %v", poseAtQuarter[5].Rot, wantThighL)
	}
	if !poseAtQuarter[7].Rot.ApproxEqual(wantThighR, 2e-3) {
		t.Fatalf("t=0.5s R thigh rot = %v, want %v", poseAtQuarter[7].Rot, wantThighR)
	}
	if !poseAtQuarter[3].Scale.ApproxEqual(math3d.Vec3{X: 1.04, Y: 1.02, Z: 1.0}, 2e-3) {
		t.Fatalf("t=0.5s chest scale = %v, want (1.04, 1.02, 1)", poseAtQuarter[3].Scale)
	}
	// Step 30 lands 20ns before 0.5s (30 * (1s/60) = 499,999,980ns), so the STEP
	// head channel holds keyframe 0 until step 31 crosses 0.5s.
	if !poseAtQuarter[4].Rot.ApproxEqual(math3d.QuatIdentity(), 2e-3) {
		t.Fatalf("step 30 (pre-0.5s) STEP head rot = %v, want identity", poseAtQuarter[4].Rot)
	}
	if !poseAfterHalfStep[4].Rot.ApproxEqual(math3d.QuatFromAxisAngle(math3d.Vec3{Z: 1}, 0.25), 2e-3) {
		t.Fatalf("step 31 (post-0.5s) STEP head rot = %v, want +0.25 rad around Z", poseAfterHalfStep[4].Rot)
	}

	// At t=1.5s: legs swapped phase (left thigh -0.6 rad, right thigh +0.6 rad).
	if !poseAtThreeQuarter[5].Rot.ApproxEqual(wantThighR, 2e-3) {
		t.Fatalf("t=1.5s L thigh rot = %v, want %v", poseAtThreeQuarter[5].Rot, wantThighR)
	}
	if !poseAtThreeQuarter[7].Rot.ApproxEqual(wantThighL, 2e-3) {
		t.Fatalf("t=1.5s R thigh rot = %v, want %v", poseAtThreeQuarter[7].Rot, wantThighL)
	}

	// At t=1.0s (and t=2.0s wraparound): returns to neutral stride pose.
	if !poseAtHalf[5].Rot.ApproxEqual(math3d.QuatIdentity(), 2e-3) {
		t.Fatalf("t=1.0s L thigh rot = %v, want identity", poseAtHalf[5].Rot)
	}
	if !poseAtCycleEnd[2].Pos.ApproxEqual(math3d.Vec3{Z: 1.0}, 2e-3) {
		t.Fatalf("t=2.0s wrapped pelvis pos = %v, want (0, 0, 1)", poseAtCycleEnd[2].Pos)
	}
	if len(skinAtHalf) != 9 {
		t.Fatalf("skinAtHalf len = %d, want 9", len(skinAtHalf))
	}

	// Verify animated skin matrix at t=0.5s actually moves vertices away from bind pose!
	player.Seek(500 * time.Millisecond)
	animSkin, err := player.SkinMatrices(0)
	if err != nil {
		t.Fatalf("animated SkinMatrices: %v", err)
	}
	if animSkin[3].ApproxEqual(math3d.Mat4Identity(), 1e-2) {
		t.Fatal("animated L thigh skin matrix remained identity; expected stride rotation")
	}
	// SkinMatricesForNode on node 1 (Cesium_Man) must match SkinMatrices(0).
	byNode, err := player.SkinMatricesForNode(1)
	if err != nil {
		t.Fatalf("SkinMatricesForNode(1): %v", err)
	}
	for i := range animSkin {
		if !animSkin[i].ApproxEqual(byNode[i], 1e-6) {
			t.Fatalf("SkinMatricesForNode[%d] differs from SkinMatrices(0)", i)
		}
	}

	// Verify Player never mutated the shared *gltf.Document.
	if doc.Nodes[2].Translation != origPelvisPos || doc.Nodes[5].Rotation != origThighLRot {
		t.Fatal("Player mutated shared gltf.Document nodes")
	}

	// Verify two independent Players sharing the same doc stay completely isolated.
	playerB, err := anim3d.NewPlayer(doc)
	if err != nil {
		t.Fatalf("NewPlayer(doc) second instance: %v", err)
	}
	_ = playerB.Play(0)
	playerB.Seek(1500 * time.Millisecond)
	if player.LocalTransform(5).Rot.ApproxEqual(playerB.LocalTransform(5).Rot, 1e-2) {
		t.Fatal("two players sharing one gltf.Document interfered with each other")
	}
}

func TestPlayerInterpolationModesExact(t *testing.T) {
	// Verify STEP, LINEAR (including shortest-path quaternion hemisphere flip),
	// CUBICSPLINE (exact Hermite formula for translation, scale, and normalized
	// rotation), clamping outside keyframe range, and morph target weights.
	q0 := math3d.QuatIdentity()
	q90 := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/2)
	// Negated q90 represents the exact same orientation in the opposite hemisphere.
	q90Neg := math3d.Quat{X: -q90.X, Y: -q90.Y, Z: -q90.Z, W: -q90.W}

	doc := &gltf.Document{
		Nodes: []gltf.Node{
			{
				Name:        "linear_node",
				Parent:      -1,
				Translation: math3d.Vec3Zero(),
				Rotation:    math3d.QuatIdentity(),
				Scale:       math3d.Vec3One(),
				Weights:     []float32{0, 0},
			},
			{
				Name:        "step_node",
				Parent:      -1,
				Translation: math3d.Vec3Zero(),
				Rotation:    math3d.QuatIdentity(),
				Scale:       math3d.Vec3One(),
			},
			{
				Name:        "cubic_node",
				Parent:      -1,
				Translation: math3d.Vec3Zero(),
				Rotation:    math3d.QuatIdentity(),
				Scale:       math3d.Vec3One(),
			},
		},
		Animations: []gltf.Animation{
			{
				Name:     "InterpTest",
				Duration: 2.0,
				Channels: []gltf.AnimationChannel{
					// LINEAR translation starting at t=0.5..1.5 to also test range clamping.
					{
						TargetNode:    0,
						Path:          gltf.PathTranslation,
						Interpolation: gltf.InterpolationLinear,
						Times:         []float32{0.5, 1.5},
						Values:        []float32{1, 2, 3, 5, 6, 7},
						Stride:        3,
					},
					// LINEAR rotation with opposite hemisphere (-q90).
					{
						TargetNode:    0,
						Path:          gltf.PathRotation,
						Interpolation: gltf.InterpolationLinear,
						Times:         []float32{0.0, 1.0},
						Values:        []float32{q0.X, q0.Y, q0.Z, q0.W, q90Neg.X, q90Neg.Y, q90Neg.Z, q90Neg.W},
						Stride:        4,
					},
					// LINEAR morph target weights.
					{
						TargetNode:    0,
						Path:          gltf.PathWeights,
						Interpolation: gltf.InterpolationLinear,
						Times:         []float32{0.0, 1.0},
						Values:        []float32{0.0, 1.0, 1.0, 0.2},
						Stride:        2,
					},
					// STEP translation.
					{
						TargetNode:    1,
						Path:          gltf.PathTranslation,
						Interpolation: gltf.InterpolationStep,
						Times:         []float32{0.0, 1.0, 2.0},
						Values:        []float32{10, 0, 0, 20, 0, 0, 30, 0, 0},
						Stride:        3,
					},
					// CUBICSPLINE translation on [0, 2] with v0=(0,0,0), b0=(3,0,0), a1=(0,0,0), v1=(2,4,0).
					{
						TargetNode:    2,
						Path:          gltf.PathTranslation,
						Interpolation: gltf.InterpolationCubicSpline,
						Times:         []float32{0.0, 2.0},
						Values: []float32{
							0, 0, 0, 0, 0, 0, 3, 0, 0, // key 0: a0, v0, b0
							0, 0, 0, 2, 4, 0, 0, 0, 0, // key 1: a1, v1, b1
						},
						Stride: 3,
					},
					// CUBICSPLINE rotation: must stay unit-length.
					{
						TargetNode:    2,
						Path:          gltf.PathRotation,
						Interpolation: gltf.InterpolationCubicSpline,
						Times:         []float32{0.0, 2.0},
						Values: []float32{
							0, 0, 0, 0, q0.X, q0.Y, q0.Z, q0.W, 0, 0.2, 0, 0,
							0, -0.2, 0, 0, q90.X, q90.Y, q90.Z, q90.W, 0, 0, 0, 0,
						},
						Stride: 4,
					},
				},
			},
		},
	}

	p, err := anim3d.NewPlayer(doc)
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}
	p.SetLoop(false)
	_ = p.Play(0)

	// At t=0.25 (before translation channel's first keyframe at 0.5): clamped to (1,2,3).
	p.Seek(250 * time.Millisecond)
	if got := p.LocalTransform(0).Pos; !got.ApproxEqual(math3d.Vec3{X: 1, Y: 2, Z: 3}, eps) {
		t.Fatalf("pre-range clamp pos = %v, want (1,2,3)", got)
	}

	// At t=0.5: LINEAR rotation halfway to -q90 must take the +45 deg short path!
	p.Seek(500 * time.Millisecond)
	wantQ45 := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/4)
	if got := p.LocalTransform(0).Rot; !got.ApproxEqual(wantQ45, eps) {
		t.Fatalf("shortest-path Slerp at 0.5s = %v, want +45 deg (%v)", got, wantQ45)
	}
	// Weights at 0.5s: [0.5, 0.6].
	w := p.MorphWeights(0)
	if len(w) != 2 || !math3d.ApproxEqual(w[0], 0.5, eps) || !math3d.ApproxEqual(w[1], 0.6, eps) {
		t.Fatalf("weights at 0.5s = %v, want [0.5 0.6]", w)
	}

	// STEP at t=0.999s must still hold keyframe 0 (X=10); at t=1.0s jumps to X=20.
	p.Seek(999 * time.Millisecond)
	if got := p.LocalTransform(1).Pos.X; !math3d.ApproxEqual(got, 10, eps) {
		t.Fatalf("STEP at 0.999s X=%v, want 10", got)
	}
	p.Seek(1000 * time.Millisecond)
	if got := p.LocalTransform(1).Pos.X; !math3d.ApproxEqual(got, 20, eps) {
		t.Fatalf("STEP at 1.0s X=%v, want 20", got)
	}

	// CUBICSPLINE at t=1.0s (s=0.5, dt=2.0):
	// h00 = 0.5, h10 = 0.125, h01 = 0.5, h11 = -0.125
	// X = 0.5*0 + 0.125*2*3 + 0.5*2 + (-0.125)*2*0 = 0.75 + 1.0 = 1.75
	// Y = 0.5*0 + 0 + 0.5*4 + 0 = 2.0
	if got := p.LocalTransform(2).Pos; !got.ApproxEqual(math3d.Vec3{X: 1.75, Y: 2.0, Z: 0}, eps) {
		t.Fatalf("CUBICSPLINE at 1.0s = %v, want (1.75, 2, 0)", got)
	}
	if rotLen := p.LocalTransform(2).Rot.Len(); !math3d.ApproxEqual(rotLen, 1, eps) {
		t.Fatalf("CUBICSPLINE rotation not unit length: len=%v", rotLen)
	}

	// Clamping past last keyframe (t=1.9s > 1.5s) holds final value (5,6,7).
	p.Seek(1900 * time.Millisecond)
	if got := p.LocalTransform(0).Pos; !got.ApproxEqual(math3d.Vec3{X: 5, Y: 6, Z: 7}, eps) {
		t.Fatalf("post-range clamp pos = %v, want (5,6,7)", got)
	}
}

func TestPlayerControlsPauseStopSpeedSeekAndNonLooping(t *testing.T) {
	doc := buildCesiumManFixture(t)
	p, err := anim3d.NewPlayer(doc)
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}

	// Non-looping forward playback stops at Duration and holds final frame.
	p.SetLoop(false)
	if p.Loop() {
		t.Fatal("Loop() should be false")
	}
	if err := p.Play(0); err != nil {
		t.Fatalf("Play(0): %v", err)
	}
	p.SetSpeed(2.0)
	if p.Speed() != 2.0 {
		t.Fatalf("Speed = %v, want 2.0", p.Speed())
	}
	// Non-finite speeds are ignored.
	p.SetSpeed(float32(math.NaN()))
	p.SetSpeed(float32(math.Inf(1)))
	if p.Speed() != 2.0 {
		t.Fatalf("non-finite SetSpeed corrupted speed: %v", p.Speed())
	}

	// 250ms at 2x speed = 500ms clip time.
	p.Step(250 * time.Millisecond)
	if p.Time() != 500*time.Millisecond {
		t.Fatalf("Time = %v, want 500ms", p.Time())
	}

	// Pause freezes time and pose across Step calls.
	p.Pause()
	if p.Playing() {
		t.Fatal("Playing() should be false after Pause")
	}
	pausedPose := p.LocalTransform(5)
	p.Step(500 * time.Millisecond)
	if p.Time() != 500*time.Millisecond || p.LocalTransform(5) != pausedPose {
		t.Fatal("paused player advanced during Step")
	}

	// Seek while paused updates pose immediately without unpausing.
	p.Seek(1500 * time.Millisecond)
	if p.Playing() || p.Time() != 1500*time.Millisecond {
		t.Fatalf("after paused Seek: playing=%v time=%v", p.Playing(), p.Time())
	}
	if p.LocalTransform(5).Rot.ApproxEqual(pausedPose.Rot, 1e-2) {
		t.Fatal("paused Seek did not update node transforms")
	}

	// Play(0) resumes from 1500ms and stops cleanly upon hitting Duration (2000ms).
	_ = p.Play(0)
	p.Step(500 * time.Millisecond) // +1000ms at 2x -> clamped to 2000ms, stopped
	if p.Time() != 2*time.Second || p.Playing() {
		t.Fatalf("after reaching end: time=%v playing=%v, want 2s false", p.Time(), p.Playing())
	}

	// Calling Play(0) after a non-looping clip finished replays from 0.
	_ = p.Play(0)
	if !p.Playing() || p.Time() != 0 {
		t.Fatalf("replay after finish: playing=%v time=%v, want true 0", p.Playing(), p.Time())
	}

	// Reverse playback with looping wraps backwards cleanly.
	p.SetLoop(true)
	p.SetSpeed(-1.0)
	p.Step(250 * time.Millisecond)
	if p.Time() != 1750*time.Millisecond {
		t.Fatalf("reverse loop wrap Time = %v, want 1.75s", p.Time())
	}

	// Stop halts playback, resets time to 0, and restores bind pose.
	p.Stop()
	if p.Playing() || p.Time() != 0 {
		t.Fatalf("after Stop: playing=%v time=%v", p.Playing(), p.Time())
	}
	mats, err := p.SkinMatrices(0)
	if err != nil {
		t.Fatalf("SkinMatrices after Stop: %v", err)
	}
	for i, m := range mats {
		if !m.ApproxEqual(math3d.Mat4Identity(), eps) {
			t.Fatalf("after Stop SkinMatrices[%d] not identity: %v", i, m)
		}
	}
}

func TestPlayerCrossfadeAndInterruption(t *testing.T) {
	doc := buildCesiumManFixture(t)
	p, err := anim3d.NewPlayer(doc)
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}

	_ = p.Play(0)
	p.Seek(500 * time.Millisecond) // L thigh at +0.6 rad
	before := p.LocalTransform(5).Rot

	// Start 200ms crossfade into Idle (L thigh at 0 rad).
	if err := p.CrossfadeByName("Idle", 200*time.Millisecond); err != nil {
		t.Fatalf("CrossfadeByName: %v", err)
	}
	if !p.IsCrossfading() || p.CrossfadeProgress() != 0 {
		t.Fatalf("crossfade start: blending=%v progress=%v", p.IsCrossfading(), p.CrossfadeProgress())
	}
	// At alpha=0 there must be zero pop from the outgoing pose.
	if !p.LocalTransform(5).Rot.ApproxEqual(before, eps) {
		t.Fatalf("crossfade popped at alpha=0: got %v, want %v", p.LocalTransform(5).Rot, before)
	}

	// Halfway through crossfade (100ms): progress = 0.5, thigh rotation is intermediate.
	p.Step(100 * time.Millisecond)
	if !math3d.ApproxEqual(p.CrossfadeProgress(), 0.5, eps) {
		t.Fatalf("crossfade progress = %v, want 0.5", p.CrossfadeProgress())
	}
	midRot := p.LocalTransform(5).Rot
	// Interrupt the active crossfade back into Walk over 200ms: must snapshot midRot with zero pop.
	if err := p.Crossfade(0, 200*time.Millisecond); err != nil {
		t.Fatalf("interrupt Crossfade: %v", err)
	}
	if !p.LocalTransform(5).Rot.ApproxEqual(midRot, eps) {
		t.Fatalf("interrupted crossfade popped: got %v, want %v", p.LocalTransform(5).Rot, midRot)
	}

	// Finish crossfade.
	p.Step(200 * time.Millisecond)
	if p.IsCrossfading() || p.CrossfadeProgress() != 1 {
		t.Fatalf("crossfade did not complete: blending=%v progress=%v", p.IsCrossfading(), p.CrossfadeProgress())
	}
	if p.AnimationIndex() != 0 {
		t.Fatalf("AnimationIndex = %d, want 0", p.AnimationIndex())
	}
}

func TestPlayerApplyToSceneAndNodes(t *testing.T) {
	doc := buildCesiumManFixture(t)
	p, err := anim3d.NewPlayer(doc)
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}
	_ = p.Play(0)
	p.Seek(500 * time.Millisecond)

	// Build a scene3d hierarchy matching doc.Nodes.
	sc := scene3d.NewScene()
	sceneNodes := make([]*scene3d.Node, len(doc.Nodes))
	for i, gn := range doc.Nodes {
		sceneNodes[i] = scene3d.NewNode(gn.Name)
	}
	for i, gn := range doc.Nodes {
		if gn.Parent < 0 {
			if err := sc.Add(sceneNodes[i]); err != nil {
				t.Fatalf("scene.Add(%d): %v", i, err)
			}
		} else {
			if err := sceneNodes[gn.Parent].AddChild(sceneNodes[i]); err != nil {
				t.Fatalf("AddChild(%d -> %d): %v", gn.Parent, i, err)
			}
		}
	}

	updated := p.ApplyToScene(sc)
	if updated != len(doc.Nodes) {
		t.Fatalf("ApplyToScene updated %d nodes, want %d", updated, len(doc.Nodes))
	}

	// World matrices in scene3d must match Player.WorldTransforms() for every node.
	worlds := p.WorldTransforms()
	for i, sn := range sceneNodes {
		got := sn.WorldMatrix()
		if !got.ApproxEqual(worlds[i], 1e-3) {
			t.Fatalf("node %d (%s) scene3d WorldMatrix = %v, want %v", i, sn.Name, got, worlds[i])
		}
	}

	// Also verify ApplyToNodes by index.
	p.Seek(1500 * time.Millisecond)
	if n := p.ApplyToNodes(sceneNodes); n != len(doc.Nodes) {
		t.Fatalf("ApplyToNodes updated %d, want %d", n, len(doc.Nodes))
	}
	sc.UpdateTransforms()
	for i, sn := range sceneNodes {
		if !sn.WorldMatrix().ApproxEqual(p.WorldTransform(i), 1e-3) {
			t.Fatalf("after ApplyToNodes node %d world matrix mismatch", i)
		}
	}
}

func TestClipFromGLTFAndStateMachineIntegration(t *testing.T) {
	doc := buildCesiumManFixture(t)
	clips, err := anim3d.ClipsFromGLTF(doc)
	if err != nil {
		t.Fatalf("ClipsFromGLTF: %v", err)
	}
	if len(clips) != 2 {
		t.Fatalf("len(clips) = %d, want 2", len(clips))
	}

	walk := clips[0]
	walk.Loop = true
	if walk.Name != "Walk" || walk.Duration != 2*time.Second || walk.JointCount != len(doc.Nodes) {
		t.Fatalf("walk clip metadata = (%q, %v, %d)", walk.Name, walk.Duration, walk.JointCount)
	}
	// Walk animates 9 nodes (2..10); untargeted nodes 0 and 1 must be omitted from Tracks.
	if len(walk.Tracks) != 9 {
		t.Fatalf("walk track count = %d, want 9", len(walk.Tracks))
	}
	// Timed events from glTF extras must be loaded onto Clip.Events.
	if len(walk.Events) != 2 || walk.Events[0].Name != "footstep" || walk.Events[0].Payload != "left" {
		t.Fatalf("walk events = %+v, want 2 footstep events", walk.Events)
	}

	// Rotation-only joint (node 5 = Skeleton_leg_L_thigh) must preserve its rest
	// translation (-0.15, 0, -0.1) when sampled via Clip.Sample.
	pose := walk.Sample(500 * time.Millisecond)
	if !pose.Joints[5].Pos.ApproxEqual(math3d.Vec3{X: -0.15, Y: 0, Z: -0.1}, eps) {
		t.Fatalf("ClipFromGLTF lost rest offset on node 5: got %v", pose.Joints[5].Pos)
	}
	if !pose.Joints[5].Rot.ApproxEqual(math3d.QuatFromAxisAngle(math3d.Vec3{X: 1}, 0.6), eps) {
		t.Fatalf("ClipFromGLTF rotation on node 5 = %v", pose.Joints[5].Rot)
	}

	// Plug converted clip into StateMachine and verify events fire.
	sm := anim3d.NewStateMachine("walk")
	sm.AddState(anim3d.State{Name: "walk", Clip: walk})
	var payloads []string
	sm.OnEvent(func(fe anim3d.FiredEvent) {
		payloads = append(payloads, fe.Event.Payload)
	})
	for range 20 {
		sm.Update(100 * time.Millisecond)
	}
	if len(payloads) != 2 || payloads[0] != "left" || payloads[1] != "right" {
		t.Fatalf("StateMachine fired events = %v, want [left right]", payloads)
	}
}

func TestPlayerWithKhronosTestAssets(t *testing.T) {
	assets := []struct {
		dir      string
		file     string
		wantSkin bool
	}{
		{dir: "../gltf/testdata/khronos/RiggedSimple/glTF", file: "RiggedSimple.gltf", wantSkin: true},
		{dir: "../gltf/testdata/khronos/CesiumMilkTruck/glTF", file: "CesiumMilkTruck.gltf", wantSkin: false},
	}

	for _, tc := range assets {
		t.Run(tc.file, func(t *testing.T) {
			fsys := os.DirFS(filepath.FromSlash(tc.dir))
			doc, err := gltf.LoadFS(fsys, tc.file)
			if err != nil {
				t.Fatalf("gltf.LoadFS(%s): %v", tc.file, err)
			}
			p, err := anim3d.NewPlayer(doc)
			if err != nil {
				t.Fatalf("NewPlayer: %v", err)
			}
			if err := p.Play(0); err != nil {
				t.Fatalf("Play(0): %v", err)
			}

			startWorlds := p.WorldTransforms()
			moved := false
			for range 60 {
				p.Step(time.Second / 60)
				cur := p.WorldTransforms()
				for i, m := range cur {
					assertMat4Finite(t, tc.file, m)
					if !m.ApproxEqual(startWorlds[i], 1e-3) {
						moved = true
					}
				}
				if tc.wantSkin {
					mats, err := p.SkinMatrices(0)
					if err != nil {
						t.Fatalf("SkinMatrices(0): %v", err)
					}
					if len(mats) == 0 {
						t.Fatal("expected non-empty skin matrices")
					}
					for _, jm := range mats {
						assertMat4Finite(t, tc.file+"/skin", jm)
					}
				}
			}
			if !moved {
				t.Fatalf("%s: no node moved after 60 fixed steps", tc.file)
			}
		})
	}
}

func TestPlayerErrorHandling(t *testing.T) {
	if _, err := anim3d.NewPlayer(nil); !errors.Is(err, anim3d.ErrNilDocument) {
		t.Fatalf("NewPlayer(nil) err = %v, want ErrNilDocument", err)
	}
	if _, err := anim3d.ClipFromGLTF(nil, 0); !errors.Is(err, anim3d.ErrNilDocument) {
		t.Fatalf("ClipFromGLTF(nil) err = %v, want ErrNilDocument", err)
	}
	if _, err := anim3d.ClipsFromGLTF(nil); !errors.Is(err, anim3d.ErrNilDocument) {
		t.Fatalf("ClipsFromGLTF(nil) err = %v, want ErrNilDocument", err)
	}

	// Cyclic hierarchy is rejected.
	cyclic := &gltf.Document{
		Nodes: []gltf.Node{
			{Name: "a", Children: []int{1}, Scale: math3d.Vec3One(), Rotation: math3d.QuatIdentity()},
			{Name: "b", Children: []int{0}, Scale: math3d.Vec3One(), Rotation: math3d.QuatIdentity()},
		},
	}
	if _, err := anim3d.NewPlayer(cyclic); err == nil {
		t.Fatal("NewPlayer accepted cyclic hierarchy")
	}

	doc := buildCesiumManFixture(t)
	p, err := anim3d.NewPlayer(doc)
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}
	if err := p.Play(99); !errors.Is(err, anim3d.ErrInvalidAnimation) {
		t.Fatalf("Play(99) err = %v, want ErrInvalidAnimation", err)
	}
	if err := p.PlayByName("Missing"); !errors.Is(err, anim3d.ErrAnimationNotFound) {
		t.Fatalf("PlayByName(Missing) err = %v, want ErrAnimationNotFound", err)
	}
	if err := p.Crossfade(-1, time.Second); !errors.Is(err, anim3d.ErrInvalidAnimation) {
		t.Fatalf("Crossfade(-1) err = %v, want ErrInvalidAnimation", err)
	}
	if _, err := p.SkinMatrices(5); !errors.Is(err, anim3d.ErrInvalidSkin) {
		t.Fatalf("SkinMatrices(5) err = %v, want ErrInvalidSkin", err)
	}
	if _, err := p.SkinMatricesForNode(0); !errors.Is(err, anim3d.ErrInvalidSkin) {
		t.Fatalf("SkinMatricesForNode(0) err = %v, want ErrInvalidSkin (node 0 has no skin)", err)
	}
}

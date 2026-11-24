package samplegltf_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/samplegltf"
)

var update = flag.Bool("update", false, "regenerate the sample glTF/GLB models committed in the placeholder pack's models folder")

func sampleDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "placeholder", "pack", "models")
}

func TestCrateGLBRoundTrip(t *testing.T) {
	raw, err := samplegltf.GenerateCrateGLB()
	if err != nil {
		t.Fatalf("GenerateCrateGLB: %v", err)
	}
	doc, bin, err := samplegltf.UnpackGLB(raw)
	if err != nil {
		t.Fatalf("UnpackGLB: %v", err)
	}
	if err := samplegltf.ValidateDocument(doc, bin); err != nil {
		t.Fatalf("ValidateDocument: %v", err)
	}

	if len(doc.Meshes) != 1 || len(doc.Meshes[0].Primitives) != 1 {
		t.Fatalf("unexpected mesh count: %d", len(doc.Meshes))
	}
	prim := doc.Meshes[0].Primitives[0]
	for _, attr := range []string{"POSITION", "NORMAL", "TEXCOORD_0"} {
		if _, ok := prim.Attributes[attr]; !ok {
			t.Fatalf("crate primitive missing %s", attr)
		}
	}
	posAcc := doc.Accessors[prim.Attributes["POSITION"]]
	if posAcc.Count != 24 {
		t.Fatalf("crate vertex count = %d, want 24", posAcc.Count)
	}
	wantMin := []float64{-0.5, 0, -0.5}
	wantMax := []float64{0.5, 1, 0.5}
	for i := 0; i < 3; i++ {
		if posAcc.Min[i] != wantMin[i] || posAcc.Max[i] != wantMax[i] {
			t.Fatalf("crate bounds axis %d = [%v, %v], want [%v, %v]", i, posAcc.Min[i], posAcc.Max[i], wantMin[i], wantMax[i])
		}
	}
	if prim.Indices == nil || doc.Accessors[*prim.Indices].Count != 36 {
		t.Fatalf("crate index count mismatch")
	}
	if len(doc.Materials) != 1 || doc.Materials[0].PBRMetallicRoughness == nil {
		t.Fatalf("crate missing PBR metallicRoughness material")
	}

	// Deterministic regeneration check.
	again, err := samplegltf.GenerateCrateGLB()
	if err != nil {
		t.Fatalf("second GenerateCrateGLB: %v", err)
	}
	if !bytes.Equal(raw, again) {
		t.Fatalf("GenerateCrateGLB is not deterministic")
	}
}

func TestRobotArmGLTFRoundTrip(t *testing.T) {
	raw, err := samplegltf.GenerateRobotArmGLTF()
	if err != nil {
		t.Fatalf("GenerateRobotArmGLTF: %v", err)
	}
	doc, bin, err := samplegltf.ParseEmbeddedGLTF(raw)
	if err != nil {
		t.Fatalf("ParseEmbeddedGLTF: %v", err)
	}
	if err := samplegltf.ValidateDocument(doc, bin); err != nil {
		t.Fatalf("ValidateDocument: %v", err)
	}

	if len(doc.Skins) != 1 || len(doc.Skins[0].Joints) != 2 {
		t.Fatalf("expected 1 skin with 2 joints, got %+v", doc.Skins)
	}
	prim := doc.Meshes[0].Primitives[0]
	for _, attr := range []string{"POSITION", "NORMAL", "TEXCOORD_0", "JOINTS_0", "WEIGHTS_0"} {
		if _, ok := prim.Attributes[attr]; !ok {
			t.Fatalf("robot arm primitive missing %s", attr)
		}
	}
	posAcc := doc.Accessors[prim.Attributes["POSITION"]]
	wantMin := []float64{-0.125, 0, -0.125}
	wantMax := []float64{0.125, 1, 0.125}
	for i := 0; i < 3; i++ {
		if posAcc.Min[i] != wantMin[i] || posAcc.Max[i] != wantMax[i] {
			t.Fatalf("robot arm bounds axis %d = [%v, %v], want [%v, %v]", i, posAcc.Min[i], posAcc.Max[i], wantMin[i], wantMax[i])
		}
	}
	if len(doc.Animations) != 1 {
		t.Fatalf("expected 1 animation, got %d", len(doc.Animations))
	}
	anim := doc.Animations[0]
	if anim.Name != "Wave" || len(anim.Channels) != 2 || len(anim.Samplers) != 2 {
		t.Fatalf("unexpected animation layout: %+v", anim)
	}
	if anim.Extras == nil || len(anim.Extras.Events) != 3 {
		t.Fatalf("expected 3 extras.events markers, got %+v", anim.Extras)
	}
	wantEvents := []samplegltf.ClipEvent{
		{Time: 0, Name: "wave_start"},
		{Time: 0.5, Name: "wave_apex"},
		{Time: 1, Name: "wave_end"},
	}
	for i, want := range wantEvents {
		got := anim.Extras.Events[i]
		if got != want {
			t.Fatalf("event[%d] = %+v, want %+v", i, got, want)
		}
	}

	// Deterministic regeneration check.
	again, err := samplegltf.GenerateRobotArmGLTF()
	if err != nil {
		t.Fatalf("second GenerateRobotArmGLTF: %v", err)
	}
	if !bytes.Equal(raw, again) {
		t.Fatalf("GenerateRobotArmGLTF is not deterministic")
	}
}

func TestCommittedSamplesMatchGenerator(t *testing.T) {
	dir := sampleDir(t)
	if *update {
		if err := samplegltf.WriteSamples(dir); err != nil {
			t.Fatalf("WriteSamples: %v", err)
		}
	}

	crateWant, err := samplegltf.GenerateCrateGLB()
	if err != nil {
		t.Fatalf("GenerateCrateGLB: %v", err)
	}
	crateGot, err := os.ReadFile(filepath.Join(dir, "crate.glb"))
	if err != nil {
		t.Fatalf("read committed crate.glb (run `go test ./internal/samplegltf -update` to generate): %v", err)
	}
	if !bytes.Equal(crateGot, crateWant) {
		t.Fatalf("committed crate.glb differs from generator output (%d vs %d bytes); run `go test ./internal/samplegltf -update`", len(crateGot), len(crateWant))
	}
	if doc, bin, err := samplegltf.UnpackGLB(crateGot); err != nil {
		t.Fatalf("UnpackGLB committed crate.glb: %v", err)
	} else if err := samplegltf.ValidateDocument(doc, bin); err != nil {
		t.Fatalf("ValidateDocument committed crate.glb: %v", err)
	}

	armWant, err := samplegltf.GenerateRobotArmGLTF()
	if err != nil {
		t.Fatalf("GenerateRobotArmGLTF: %v", err)
	}
	armGot, err := os.ReadFile(filepath.Join(dir, "robot_arm.gltf"))
	if err != nil {
		t.Fatalf("read committed robot_arm.gltf (run `go test ./internal/samplegltf -update` to generate): %v", err)
	}
	if !bytes.Equal(armGot, armWant) {
		t.Fatalf("committed robot_arm.gltf differs from generator output (%d vs %d bytes); run `go test ./internal/samplegltf -update`", len(armGot), len(armWant))
	}
	if doc, bin, err := samplegltf.ParseEmbeddedGLTF(armGot); err != nil {
		t.Fatalf("ParseEmbeddedGLTF committed robot_arm.gltf: %v", err)
	} else if err := samplegltf.ValidateDocument(doc, bin); err != nil {
		t.Fatalf("ValidateDocument committed robot_arm.gltf: %v", err)
	}

	gemWant, err := samplegltf.GenerateGemGLB()
	if err != nil {
		t.Fatalf("GenerateGemGLB: %v", err)
	}
	gemGot, err := os.ReadFile(filepath.Join(dir, "gem.glb"))
	if err != nil {
		t.Fatalf("read committed gem.glb (run `go test ./internal/samplegltf -update` to generate): %v", err)
	}
	if !bytes.Equal(gemGot, gemWant) {
		t.Fatalf("committed gem.glb differs from generator output (%d vs %d bytes); run `go test ./internal/samplegltf -update`", len(gemGot), len(gemWant))
	}
}

func TestGemGLB(t *testing.T) {
	raw, err := samplegltf.GenerateGemGLB()
	if err != nil {
		t.Fatalf("GenerateGemGLB: %v", err)
	}
	doc, bin, err := samplegltf.UnpackGLB(raw)
	if err != nil {
		t.Fatalf("UnpackGLB: %v", err)
	}
	if err := samplegltf.ValidateDocument(doc, bin); err != nil {
		t.Fatalf("ValidateDocument: %v", err)
	}
	prim := doc.Meshes[0].Primitives[0]
	if n := doc.Accessors[prim.Attributes["POSITION"]].Count; n != 36 {
		t.Fatalf("gem vertex count = %d, want 36 (12 flat-shaded faces)", n)
	}
	pos := doc.Accessors[prim.Attributes["POSITION"]]
	if pos.Min[1] != 0 || pos.Max[1] != 1 {
		t.Fatalf("gem height = [%v, %v], want [0, 1]", pos.Min[1], pos.Max[1])
	}
	again, err := samplegltf.GenerateGemGLB()
	if err != nil {
		t.Fatalf("second GenerateGemGLB: %v", err)
	}
	if !bytes.Equal(raw, again) {
		t.Fatalf("GenerateGemGLB is not deterministic")
	}
}

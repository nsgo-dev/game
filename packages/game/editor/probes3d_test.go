package editor_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

func TestAddProbesAndInspectorUndoRedo(t *testing.T) {
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	v3YAML := []byte("version: 3\nname: Probe Edit\nlayers:\n  - name: world\n    objects: []\n")
	if err := ed.OpenYAML(v3YAML); err != nil {
		t.Fatalf("OpenYAML: %v", err)
	}
	if ed.HasProbes3D() {
		t.Fatal("HasProbes3D = true before adding probes")
	}

	rpID, err := ed.AddReflectionProbe3D()
	if err != nil {
		t.Fatalf("AddReflectionProbe3D: %v", err)
	}
	if ed.Doc.Version != sceneformat.CurrentVersion {
		t.Fatalf("Version after AddReflectionProbe3D = %d, want %d", ed.Doc.Version, sceneformat.CurrentVersion)
	}
	if !ed.HasProbes3D() {
		t.Fatal("HasProbes3D = false after AddReflectionProbe3D")
	}
	if ed.SelectedID != rpID {
		t.Fatalf("SelectedID = %q, want %q", ed.SelectedID, rpID)
	}

	lpID, err := ed.AddLightProbeGrid3D()
	if err != nil {
		t.Fatalf("AddLightProbeGrid3D: %v", err)
	}

	// Edit reflection probe intensity undoably.
	rpObj, _, _ := ed.FindObject(rpID)
	if rpObj == nil || rpObj.ReflectionProbe == nil {
		t.Fatalf("reflection probe %q missing", rpID)
	}
	editedRP := rpObj.ReflectionProbe.Clone()
	editedRP.Intensity = sceneformat.Float32(1.75)
	editedRP.BlendDistance = 1.25
	if err := ed.SetObjectReflectionProbe(rpID, editedRP); err != nil {
		t.Fatalf("SetObjectReflectionProbe: %v", err)
	}
	if rpObj.ReflectionProbe.EffectiveIntensity() != 1.75 {
		t.Fatalf("intensity = %v, want 1.75", rpObj.ReflectionProbe.EffectiveIntensity())
	}
	if !ed.Undo() {
		t.Fatal("Undo SetObjectReflectionProbe returned false")
	}
	if rpObj.ReflectionProbe.EffectiveIntensity() != 1 {
		t.Fatalf("after Undo intensity = %v, want 1", rpObj.ReflectionProbe.EffectiveIntensity())
	}
	if !ed.Redo() {
		t.Fatal("Redo SetObjectReflectionProbe returned false")
	}
	if rpObj.ReflectionProbe.EffectiveIntensity() != 1.75 {
		t.Fatalf("after Redo intensity = %v, want 1.75", rpObj.ReflectionProbe.EffectiveIntensity())
	}

	// Edit light probe grid counts undoably.
	lpObj, _, _ := ed.FindObject(lpID)
	if lpObj == nil || lpObj.LightProbeGrid == nil {
		t.Fatalf("light probe grid %q missing", lpID)
	}
	editedLP := lpObj.LightProbeGrid.Clone()
	cnt := [3]int{4, 3, 4}
	editedLP.Counts = &cnt
	if err := ed.SetObjectLightProbeGrid(lpID, editedLP); err != nil {
		t.Fatalf("SetObjectLightProbeGrid: %v", err)
	}
	if lpObj.LightProbeGrid.EffectiveCounts() != cnt {
		t.Fatalf("counts = %v, want %v", lpObj.LightProbeGrid.EffectiveCounts(), cnt)
	}
	if !ed.Undo() {
		t.Fatal("Undo SetObjectLightProbeGrid returned false")
	}
	if lpObj.LightProbeGrid.EffectiveCounts() != ([3]int{3, 2, 3}) {
		t.Fatalf("after Undo counts = %v, want [3 2 3]", lpObj.LightProbeGrid.EffectiveCounts())
	}

	// Command palette commands work.
	if err := ed.ExecuteCommand(editor.CmdAddReflectionProbe); err != nil {
		t.Fatalf("ExecuteCommand(CmdAddReflectionProbe): %v", err)
	}
	if err := ed.ExecuteCommand(editor.CmdAddLightProbeGrid); err != nil {
		t.Fatalf("ExecuteCommand(CmdAddLightProbeGrid): %v", err)
	}
}

func TestProbeBoundsHandlesUndoRedoAndCancel(t *testing.T) {
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sz := [3]float32{6, 4, 6}
	cnt := [3]int{2, 2, 2}
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Probe Handles",
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{
					ID: "rp",
					Y:  2,
					ReflectionProbe: &sceneformat.ReflectionProbeDoc{
						Size:          &sz,
						BlendDistance: 0.5,
					},
				},
				{
					ID: "lp",
					X:  10,
					Y:  2,
					LightProbeGrid: &sceneformat.LightProbeGridDoc{
						Size:   &sz,
						Counts: &cnt,
					},
				},
			},
		}},
	}
	data, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := ed.OpenYAML(data); err != nil {
		t.Fatalf("OpenYAML: %v", err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 2, 0},
		Yaw:      0,
		Pitch:    0.2,
		Distance: 14,
		FOV:      float32(math.Pi / 3),
		Near:     0.1,
		Far:      100,
	}

	const vpW, vpH float32 = 800, 600

	// 1. Reflection probe +X face handle drag, commit, undo, redo.
	ed.SelectObject("rp")
	posX, ok := ed.HandlePosition3D(editor.Handle3DProbeBoundsPosX)
	if !ok {
		t.Fatal("HandlePosition3D(Handle3DProbeBoundsPosX) returned false")
	}
	if math.Abs(float64(posX[0]-3)) > 1e-4 || math.Abs(float64(posX[1]-2)) > 1e-4 {
		t.Fatalf("+X face handle = %v, want [3 2 0]", posX)
	}

	sx, sy, _, vis := ed.Camera3D.Project(posX, vpW, vpH)
	if !vis {
		t.Fatal("+X handle not visible")
	}
	if picked := ed.PickHandle3D(sx, sy, vpW, vpH); picked != editor.Handle3DProbeBoundsPosX {
		t.Fatalf("PickHandle3D = %v, want Handle3DProbeBoundsPosX", picked)
	}

	histBefore := ed.History.Len()
	if got := ed.BeginHandleDrag3D(sx, sy, vpW, vpH); got != editor.Handle3DProbeBoundsPosX {
		t.Fatalf("BeginHandleDrag3D = %v, want Handle3DProbeBoundsPosX", got)
	}
	// Drag +X handle to the right (+80px) to expand +X face while -X face stays anchored at -3.
	ed.UpdateHandleDrag3D(sx+80, sy)
	if !ed.EndHandleDrag3D() {
		t.Fatal("EndHandleDrag3D returned false")
	}
	if ed.History.Len() != histBefore+1 {
		t.Fatalf("History.Len() = %d, want %d (single undoable step)", ed.History.Len(), histBefore+1)
	}

	rpObj, _, _ := ed.FindObject("rp")
	afterSize := rpObj.ReflectionProbe.EffectiveSize()
	afterCenter := rpObj.ReflectionProbe.EffectiveCenter()
	if afterSize[0] <= 6.1 {
		t.Fatalf("after +X drag size.X = %v, want > 6.1", afterSize[0])
	}
	// Opposite (-X) face must stay anchored at -3.
	negXFace := afterCenter[0] - afterSize[0]*0.5
	if math.Abs(float64(negXFace-(-3))) > 0.05 {
		t.Fatalf("opposite -X face moved to %v, want -3", negXFace)
	}

	// Undo restores original bounds.
	if !ed.Undo() {
		t.Fatal("Undo after bounds handle drag returned false")
	}
	if got := rpObj.ReflectionProbe.EffectiveSize(); got != sz {
		t.Fatalf("after Undo size = %v, want %v", got, sz)
	}
	if got := rpObj.ReflectionProbe.EffectiveCenter(); got != ([3]float32{}) {
		t.Fatalf("after Undo center = %v, want [0 0 0]", got)
	}

	// Redo reapplies dragged bounds.
	if !ed.Redo() {
		t.Fatal("Redo after bounds handle drag returned false")
	}
	if got := rpObj.ReflectionProbe.EffectiveSize(); got != afterSize {
		t.Fatalf("after Redo size = %v, want %v", got, afterSize)
	}

	// 2. Escape cancel on LightProbeGrid handle restores pre-drag bounds with 0 history entries.
	ed.Camera3D.Target = [3]float32{10, 2, 0}
	ed.SelectObject("lp")
	posY, ok := ed.HandlePosition3D(editor.Handle3DProbeBoundsPosY)
	if !ok {
		t.Fatal("HandlePosition3D(Handle3DProbeBoundsPosY) on light probe grid returned false")
	}
	ysx, ysy, _, yvis := ed.Camera3D.Project(posY, vpW, vpH)
	if !yvis {
		t.Fatal("+Y handle on light probe grid not visible")
	}
	histBeforeCancel := ed.History.Len()
	if !ed.BeginHandleDragKind3D(editor.Handle3DProbeBoundsPosY, ysx, ysy, vpW, vpH) {
		t.Fatal("BeginHandleDragKind3D(+Y) failed")
	}
	ed.UpdateHandleDrag3D(ysx, ysy-60)
	lpObj, _, _ := ed.FindObject("lp")
	if lpObj.LightProbeGrid.EffectiveSize()[1] == sz[1] {
		t.Fatal("UpdateHandleDrag3D did not update light probe grid Y size live")
	}
	ed.CancelHandleDrag3D()
	if lpObj.LightProbeGrid.EffectiveSize() != sz {
		t.Fatalf("CancelHandleDrag3D left size = %v, want %v", lpObj.LightProbeGrid.EffectiveSize(), sz)
	}
	if ed.History.Len() != histBeforeCancel {
		t.Fatalf("CancelHandleDrag3D pushed entry to History: got %d, want %d", ed.History.Len(), histBeforeCancel)
	}

	// 3. DrawViewport3D renders probe icons, wireframes, grid dots, and handles without panic.
	var d core.Drawing
	ed.DrawViewport3D(&d, core.Size{Width: core.Points(vpW), Height: core.Points(vpH)})

	// 4. Icon picking selects the probe object.
	iconX, iconY, _, ivis := ed.Camera3D.Project([3]float32{10, 2, 0}, vpW, vpH)
	if !ivis {
		t.Fatal("probe icon not visible")
	}
	if id, hit := ed.Pick3DObject(iconX, iconY, vpW, vpH); !hit || id != "lp" {
		t.Fatalf("Pick3DObject on probe icon = (%q, %v), want (\"lp\", true)", id, hit)
	}
}

func TestBakeProbesRejectsUnsafePaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte("version: 1\nname: ProbeSafety\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectPath = filepath.Join(dir, "game.yaml")
	sz := [3]float32{4, 4, 4}
	cnt := [3]int{2, 2, 2}
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Safety",
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{ID: "rp_bad", ReflectionProbe: &sceneformat.ReflectionProbeDoc{Size: &sz, Cubemap: "probes/not_hdr.txt"}},
				{ID: "lp_bad", LightProbeGrid: &sceneformat.LightProbeGridDoc{Size: &sz, Counts: &cnt, Data: "game.yaml"}},
			},
		}},
	}
	ed.Doc = doc
	if _, err := ed.BakeReflectionProbe3D("rp_bad"); err == nil {
		t.Fatal("BakeReflectionProbe3D accepted non-HDR extension")
	}
	if _, err := ed.BakeLightProbeGrid3D("lp_bad"); err == nil {
		t.Fatal("BakeLightProbeGrid3D accepted reserved game.yaml path")
	}
}

// Regressions for Blockers #1, #2, and #3:
// 1. Rebaking a fallback-lit scene (no 3D lights, no environment) stays lit instead of rendering pitch black.
// 2. Pre-set unbaked/deleted cubemap & data paths do not block baking.
// 3. Baking in a standalone saved scene (FilePath set, ProjectPath empty) loads cleanly in Scene3D().
func TestBakeProbesRebakeFallbackLitMissingTargetAndStandaloneScene(t *testing.T) {
	// 1 & 2: Project scene with NO explicit 3D lights and NO environment, plus pre-set missing output paths.
	projDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, "game.yaml"), []byte("version: 1\nname: ProbeRegressions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sz := [3]float32{8, 6, 8}
	cnt := [3]int{2, 2, 2}
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "NoLights",
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{ID: "wall", Mesh: "box", X: 2.5, Y: 0, PosZ: 0, ScaleX: sceneformat.Float32(0.5), ScaleY: sceneformat.Float32(4), ScaleZ: sceneformat.Float32(4)},
				{ID: "rp", X: 0, Y: 0, PosZ: 0, ReflectionProbe: &sceneformat.ReflectionProbeDoc{Size: &sz, Resolution: 16, Cubemap: "probes/custom_missing.hdr"}},
				{ID: "lp", X: 0, Y: 0, PosZ: 0, LightProbeGrid: &sceneformat.LightProbeGridDoc{Size: &sz, Counts: &cnt, Data: "probes/custom_missing.probes.yaml"}},
			},
		}},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectPath = filepath.Join(projDir, "game.yaml")

	// Blocker #2: Baking must succeed even though custom_missing.hdr and custom_missing.probes.yaml do not exist yet.
	n, err := ed.BakeAllProbes3D()
	if err != nil {
		t.Fatalf("BakeAllProbes3D with missing target files: %v", err)
	}
	if n != 2 {
		t.Fatalf("BakeAllProbes3D baked %d probes, want 2", n)
	}
	built1, err := ed.Scene3D()
	if err != nil {
		t.Fatalf("Scene3D after first bake: %v", err)
	}
	faceSum := func(cm [][3]float32) float64 {
		var s float64
		for _, p := range cm {
			s += float64(p[0]) + float64(p[1]) + float64(p[2])
		}
		return s
	}
	sum1 := faceSum(built1.ReflectionProbes[0].Cubemap.Faces[0])
	if sum1 <= 0.1 {
		t.Fatalf("first fallback-lit bake +X face sum = %v, want > 0.1", sum1)
	}

	// Blocker #1: Rebaking the already-baked fallback-lit scene must NOT render pitch black!
	if _, err := ed.BakeReflectionProbe3D("rp"); err != nil {
		t.Fatalf("rebake BakeReflectionProbe3D: %v", err)
	}
	built2, err := ed.Scene3D()
	if err != nil {
		t.Fatalf("Scene3D after rebake: %v", err)
	}
	sum2 := faceSum(built2.ReflectionProbes[0].Cubemap.Faces[0])
	if sum2 <= 0.1 {
		t.Fatalf("rebaked fallback-lit +X face rendered black (sum=%v, first bake was %v)", sum2, sum1)
	}

	// Deleting a baked file on disk and rebaking must also succeed.
	if err := os.Remove(filepath.Join(projDir, "probes", "custom_missing.hdr")); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.BakeReflectionProbe3D("rp"); err != nil {
		t.Fatalf("BakeReflectionProbe3D after deleting target file: %v", err)
	}

	// Blocker #3: Standalone saved scene (FilePath set, ProjectPath empty) must bake AND render in Scene3D().
	standaloneDir := t.TempDir()
	edStandalone, err := editor.New(&sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Standalone",
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{ID: "wall", Mesh: "box", X: 2.5, Y: 0, PosZ: 0, ScaleX: sceneformat.Float32(0.5), ScaleY: sceneformat.Float32(4), ScaleZ: sceneformat.Float32(4)},
				{ID: "rp", X: 0, Y: 0, PosZ: 0, ReflectionProbe: &sceneformat.ReflectionProbeDoc{Size: &sz, Resolution: 16}},
				{ID: "lp", X: 0, Y: 0, PosZ: 0, LightProbeGrid: &sceneformat.LightProbeGridDoc{Size: &sz, Counts: &cnt}},
			},
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	edStandalone.FilePath = filepath.Join(standaloneDir, "room.scene.yaml")
	if _, err := edStandalone.BakeAllProbes3D(); err != nil {
		t.Fatalf("standalone BakeAllProbes3D: %v", err)
	}
	builtStandalone, err := edStandalone.Scene3D()
	if err != nil {
		t.Fatalf("standalone Scene3D() failed after bake: %v", err)
	}
	if len(builtStandalone.ReflectionProbes) != 1 || builtStandalone.ReflectionProbes[0].Cubemap == nil {
		t.Fatalf("standalone Scene3D() did not load baked reflection probe cubemap")
	}
	if len(builtStandalone.LightProbeGrids) != 1 || builtStandalone.LightProbeGrids[0].Grid == nil {
		t.Fatalf("standalone Scene3D() did not load baked light probe grid")
	}
}

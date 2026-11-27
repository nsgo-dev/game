package editor_test

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/skeletal"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tooling/refs"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func testRunnerAtlas() *sprite.Atlas {
	frame := func(x, y, w, h float32) sprite.Frame {
		return sprite.Frame{X: x, Y: y, W: w, H: h, Width: w, Height: h}
	}
	return &sprite.Atlas{
		Image: core.ImageSource{URI: "runner.png"},
		Frames: map[string]sprite.Frame{
			"torso":       frame(0, 0, 12, 18),
			"torso_armor": frame(12, 0, 12, 18),
			"head":        frame(24, 0, 12, 12),
			"head_blink":  frame(36, 0, 12, 12),
			"helmet":      frame(48, 0, 12, 12),
			"arm_front":   frame(0, 18, 6, 12),
			"arm_back":    frame(6, 18, 6, 12),
			"leg_front":   frame(12, 18, 6, 18),
			"leg_back":    frame(18, 18, 6, 18),
		},
	}
}

func assertBoneClose(t *testing.T, label string, got, want skeletal.BoneTransform) {
	t.Helper()
	const eps = 1e-3
	if math.Abs(float64(got.X-want.X)) > eps ||
		math.Abs(float64(got.Y-want.Y)) > eps ||
		math.Abs(float64(got.Rotation-want.Rotation)) > eps ||
		math.Abs(float64(got.ScaleX-want.ScaleX)) > eps ||
		math.Abs(float64(got.ScaleY-want.ScaleY)) > eps {
		t.Fatalf("%s: got %+v, want %+v", label, got, want)
	}
}

// Acceptance #1: A rig built in the editor loads with skeletal.Load and plays the same animation
// (including crossfade blending) in the game.
func TestRigBuiltInEditorMatchesRuntimeSkeletalPlaybackAndCrossfade(t *testing.T) {
	atlas := testRunnerAtlas()
	ed, err := editor.New(nil, sceneformat.AtlasMap{"runner": atlas})
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}

	if err := ed.NewRig("fighter", "runner"); err != nil {
		t.Fatalf("NewRig: %v", err)
	}
	ed.SetRigCanvasSize(400, 300)

	// Add bones, reparent with world preservation, and adjust transform + length with handles.
	if err := ed.AddRigBone("torso", "root", 0, -20, 0, 16); err != nil {
		t.Fatalf("AddRigBone torso: %v", err)
	}
	if err := ed.AddRigBone("arm", "root", 6, -34, 0, 10); err != nil {
		t.Fatalf("AddRigBone arm: %v", err)
	}
	// Reparent arm under torso keeping its world pose (local becomes (6, -14)).
	if err := ed.ReparentRigBone("arm", "torso", true); err != nil {
		t.Fatalf("ReparentRigBone: %v", err)
	}
	// Cycle reparenting must be refused.
	if err := ed.ReparentRigBone("torso", "arm", true); err == nil {
		t.Fatal("ReparentRigBone allowed cycle")
	}

	// Interactive bone handles: move, rotate, and set length of "arm".
	ed.SelectRigBone("arm")
	origin, tip, ring, ok := ed.RigBoneHandlePos("arm")
	if !ok {
		t.Fatal("RigBoneHandlePos(arm) = false")
	}
	// Drag move handle by (+4, +2) canvas points (zoom = 2 -> (+2, +1) world units).
	if h := ed.BeginRigDrag(origin.X, origin.Y); h != editor.RigHandleBoneMove {
		t.Fatalf("BeginRigDrag(origin) = %v, want RigHandleBoneMove", h)
	}
	ed.UpdateRigDrag(origin.X+4, origin.Y+2)
	if !ed.EndRigDrag() {
		t.Fatal("EndRigDrag move returned false")
	}

	// Drag length tip handle to lengthen bone from 10 to 14 world units (28 canvas points).
	origin, tip, ring, _ = ed.RigBoneHandlePos("arm")
	if h := ed.BeginRigDrag(tip.X, tip.Y); h != editor.RigHandleBoneLength {
		t.Fatalf("BeginRigDrag(tip) = %v, want RigHandleBoneLength", h)
	}
	ed.UpdateRigDrag(origin.X+28, origin.Y)
	if !ed.EndRigDrag() {
		t.Fatal("EndRigDrag length returned false")
	}

	// Drag rotate ring handle by +45 degrees.
	origin, _, ring, _ = ed.RigBoneHandlePos("arm")
	if h := ed.BeginRigDrag(origin.X+ring, origin.Y); h != editor.RigHandleBoneRotate {
		t.Fatalf("BeginRigDrag(ring) = %v, want RigHandleBoneRotate", h)
	}
	rotPt := float32(math.Sqrt2 / 2)
	ed.UpdateRigDrag(origin.X+ring*rotPt, origin.Y+ring*rotPt)
	if !ed.EndRigDrag() {
		t.Fatal("EndRigDrag rotate returned false")
	}

	// Add slots, skins, and attach atlas frames with origin handle drag.
	if err := ed.AddRigSlot("torso", "torso", "", 1); err != nil {
		t.Fatalf("AddRigSlot torso: %v", err)
	}
	if err := ed.AddRigSlot("arm", "arm", "", 1); err != nil {
		t.Fatalf("AddRigSlot arm: %v", err)
	}
	if err := ed.AddRigSkin("default"); err != nil {
		t.Fatalf("AddRigSkin default: %v", err)
	}
	if err := ed.AttachRigFrame("default", "torso", "torso", 6, 16); err != nil {
		t.Fatalf("AttachRigFrame torso: %v", err)
	}
	if err := ed.AttachRigFrame("default", "arm", "arm_front", 3, 2); err != nil {
		t.Fatalf("AttachRigFrame arm: %v", err)
	}
	if err := ed.AddRigSkin("armored"); err != nil {
		t.Fatalf("AddRigSkin armored: %v", err)
	}
	if err := ed.AttachRigFrame("armored", "torso", "torso_armor", 6, 16); err != nil {
		t.Fatalf("AttachRigFrame armored torso: %v", err)
	}
	if err := ed.SelectRigSkin("default"); err != nil {
		t.Fatalf("SelectRigSkin: %v", err)
	}

	// Adjust torso attachment origin with interactive RigHandleAttachmentOrigin drag.
	ed.SelectRigSlot("torso")
	origPos, ok := ed.RigAttachmentOriginCanvasPos("torso")
	if !ok {
		t.Fatal("RigAttachmentOriginCanvasPos(torso) = false")
	}
	if h := ed.BeginRigHandleDrag(editor.RigHandleAttachmentOrigin, origPos.X, origPos.Y); h != editor.RigHandleAttachmentOrigin {
		t.Fatalf("BeginRigHandleDrag origin = %v", h)
	}
	ed.UpdateRigDrag(origPos.X+4, origPos.Y-2)
	if !ed.EndRigDrag() {
		t.Fatal("EndRigDrag origin returned false")
	}

	// Key translate, rotate, scale and slot attachment on two animations: "idle" and "attack".
	if err := ed.AddRigAnimation("idle", 800*time.Millisecond, true); err != nil {
		t.Fatalf("AddRigAnimation idle: %v", err)
	}
	if err := ed.KeyRigBoneTranslate("idle", "torso", 0, 0, 0, skeletal.CurveBezier, [4]float32{0.25, 0.1, 0.25, 1}); err != nil {
		t.Fatal(err)
	}
	if err := ed.KeyRigBoneTranslate("idle", "torso", 400*time.Millisecond, 0, -4, skeletal.CurveLinear, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if err := ed.KeyRigBoneTranslate("idle", "torso", 800*time.Millisecond, 0, 0, skeletal.CurveLinear, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if err := ed.KeyRigBoneRotate("idle", "arm", 0, -0.3, skeletal.CurveLinear, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if err := ed.KeyRigBoneRotate("idle", "arm", 400*time.Millisecond, 0.5, skeletal.CurveLinear, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if err := ed.KeyRigBoneScale("idle", "torso", 0, 1, 1, skeletal.CurveLinear, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if err := ed.KeyRigBoneScale("idle", "torso", 400*time.Millisecond, 1.1, 0.9, skeletal.CurveLinear, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if err := ed.KeyRigSlotAttachment("idle", "arm", 0, "arm_front"); err != nil {
		t.Fatal(err)
	}
	if err := ed.KeyRigSlotAttachment("idle", "arm", 400*time.Millisecond, "arm_back"); err != nil {
		t.Fatal(err)
	}

	if err := ed.AddRigAnimation("attack", 400*time.Millisecond, false); err != nil {
		t.Fatalf("AddRigAnimation attack: %v", err)
	}
	if err := ed.KeyRigBoneRotate("attack", "arm", 0, -1.0, skeletal.CurveLinear, [4]float32{}); err != nil {
		t.Fatal(err)
	}
	if err := ed.KeyRigBoneRotate("attack", "arm", 200*time.Millisecond, 1.2, skeletal.CurveLinear, [4]float32{}); err != nil {
		t.Fatal(err)
	}

	// Save rig YAML and verify it loads cleanly with skeletal.Load.
	savedYAML, err := ed.SaveRigYAML()
	if err != nil {
		t.Fatalf("SaveRigYAML: %v", err)
	}
	gameData, err := skeletal.Load(savedYAML)
	if err != nil {
		t.Fatalf("skeletal.Load(savedYAML): %v\n%s", err, savedYAML)
	}

	// Compare editor preview playback step-for-step with runtime game skeletal.Skeleton.
	if err := ed.SelectRigAnimation("idle"); err != nil {
		t.Fatal(err)
	}
	if err := ed.PlayRig(); err != nil {
		t.Fatal(err)
	}
	gameSkel := skeletal.NewSkeleton(gameData, atlas)
	if err := gameSkel.Play("idle", true); err != nil {
		t.Fatal(err)
	}

	for step := range 12 {
		dt := 50 * time.Millisecond
		ed.StepRig(dt)
		gameSkel.Update(dt)
		for _, boneName := range []string{"root", "torso", "arm"} {
			got, _ := ed.RigSkeleton().WorldBone(boneName)
			want, _ := gameSkel.WorldBone(boneName)
			assertBoneClose(t, "idle step "+string(rune('0'+step))+" "+boneName, got, want)
		}
	}

	// Crossfade preview must also match runtime skeletal.Skeleton.Crossfade identically!
	if err := ed.CrossfadeRig("attack", 200*time.Millisecond); err != nil {
		t.Fatalf("CrossfadeRig: %v", err)
	}
	if !ed.RigBlending() {
		t.Fatal("RigBlending = false immediately after CrossfadeRig")
	}
	if err := gameSkel.Crossfade("attack", 200*time.Millisecond, false); err != nil {
		t.Fatalf("gameSkel.Crossfade: %v", err)
	}
	for range 6 {
		dt := 50 * time.Millisecond
		ed.StepRig(dt)
		gameSkel.Update(dt)
		for _, boneName := range []string{"root", "torso", "arm"} {
			got, _ := ed.RigSkeleton().WorldBone(boneName)
			want, _ := gameSkel.WorldBone(boneName)
			assertBoneClose(t, "crossfade "+boneName, got, want)
		}
	}

	// Verify DrawRigViewport renders attachments, bones and handles cleanly.
	var d core.Drawing
	ed.DrawRigViewport(&d, core.Size{Width: 400, Height: 300})
	if len(d.Ops) == 0 {
		t.Fatal("DrawRigViewport produced no drawing ops")
	}
}

// Acceptance #2: Opening and saving an existing rig changes nothing byte-for-byte
// (both canonical runner.skel.yaml and a hand-authored file with comments, plus undo-back-to-clean).
func TestRigOpenAndSaveUnchangedByteForByte(t *testing.T) {
	runnerPath := filepath.Join("..", "skeletal", "testdata", "runner.skel.yaml")
	origCanonical, err := os.ReadFile(runnerPath)
	if err != nil {
		t.Fatalf("ReadFile runner.skel.yaml: %v", err)
	}

	ed, err := editor.New(nil, sceneformat.AtlasMap{"runner": testRunnerAtlas()})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Canonical runner.skel.yaml opened from disk and saved without edits.
	tmp := t.TempDir()
	copyPath := filepath.Join(tmp, "runner.skel.yaml")
	if err := os.WriteFile(copyPath, origCanonical, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenRigFile(copyPath, "runner"); err != nil {
		t.Fatalf("OpenRigFile: %v", err)
	}
	if ed.RigDirty() {
		t.Fatal("RigDirty = true right after open")
	}
	if err := ed.SaveRigFile(copyPath); err != nil {
		t.Fatalf("SaveRigFile: %v", err)
	}
	afterSave, err := os.ReadFile(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterSave, origCanonical) {
		t.Fatalf("open+save modified canonical runner.skel.yaml:\ngot:\n%s\nwant:\n%s", afterSave, origCanonical)
	}

	// Even canonical re-encode of RigData() must match runner.skel.yaml byte-for-byte.
	reencoded, err := skeletal.Encode(ed.RigData())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded, origCanonical) {
		t.Fatalf("skeletal.Encode(RigData()) differs from runner.skel.yaml")
	}

	// 2. Edit, then Undo back to initial state -> SaveRigYAML returns exact original bytes!
	if err := ed.MoveRigBone("torso", 10, -25); err != nil {
		t.Fatal(err)
	}
	if !ed.RigDirty() {
		t.Fatal("RigDirty = false after MoveRigBone")
	}
	if !ed.Undo() {
		t.Fatal("Undo returned false")
	}
	if ed.RigDirty() {
		t.Fatal("RigDirty = true after undoing edit")
	}
	afterUndo, err := ed.SaveRigYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterUndo, origCanonical) {
		t.Fatal("SaveRigYAML after Undo differs from original bytes")
	}

	// Redo restores the move deterministically.
	if !ed.Redo() {
		t.Fatal("Redo returned false")
	}
	if ed.RigData().Bones[1].X != 10 || ed.RigData().Bones[1].Y != -25 {
		t.Fatalf("after Redo torso = (%v, %v), want (10, -25)", ed.RigData().Bones[1].X, ed.RigData().Bones[1].Y)
	}

	// 3. Hand-written YAML with comments and custom formatting also round-trips byte-for-byte unchanged on open+save.
	commented := []byte("# Custom comment kept on untouched save\nversion: 1\nname: custom\nbones:\n  - {name: root}\n")
	if err := ed.OpenRigYAML("custom.skel.yaml", commented, ""); err != nil {
		t.Fatal(err)
	}
	got, err := ed.SaveRigYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, commented) {
		t.Fatalf("open+save modified commented rig:\ngot:\n%q\nwant:\n%q", got, commented)
	}
}

func TestRigDragCancelAndAnimateModeKeying(t *testing.T) {
	ed, err := editor.New(nil, sceneformat.AtlasMap{"runner": testRunnerAtlas()})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.NewRig("canceltest", "runner"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddRigBone("arm", "root", 0, -10, 0, 12); err != nil {
		t.Fatal(err)
	}
	ed.SetRigCanvasSize(400, 300)
	ed.SelectRigBone("arm")

	// CancelRigDrag restores bone position and leaves History untouched.
	histIdx := ed.History.Index()
	origin, _, _, ok := ed.RigBoneHandlePos("arm")
	if !ok {
		t.Fatal("missing arm handle")
	}
	if h := ed.BeginRigDrag(origin.X, origin.Y); h != editor.RigHandleBoneMove {
		t.Fatalf("got handle %v", h)
	}
	ed.UpdateRigDrag(origin.X+20, origin.Y+10)
	if !ed.CancelRigDrag() {
		t.Fatal("CancelRigDrag returned false")
	}
	if ed.History.Index() != histIdx {
		t.Fatalf("history index changed after cancel: got %d, want %d", ed.History.Index(), histIdx)
	}
	if got := ed.RigData().Bones[1]; got.X != 0 || got.Y != -10 {
		t.Fatalf("bone after cancel = (%v,%v), want (0,-10)", got.X, got.Y)
	}

	// Dragging bone move handle while an animation is selected keys Translate at current playhead!
	if err := ed.AddRigAnimation("bob", time.Second, true); err != nil {
		t.Fatal(err)
	}
	ed.SeekRig(500 * time.Millisecond)
	origin, _, _, _ = ed.RigBoneHandlePos("arm")
	if h := ed.BeginRigDrag(origin.X, origin.Y); h != editor.RigHandleBoneMove {
		t.Fatalf("animate mode handle = %v", h)
	}
	ed.UpdateRigDrag(origin.X+10, origin.Y-6) // zoom=2 -> (+5, -3)
	if !ed.EndRigDrag() {
		t.Fatal("EndRigDrag in animate mode returned false")
	}
	tl := ed.RigData().Animations["bob"].Bones["arm"]
	if len(tl.Translate) != 1 || tl.Translate[0].Time != 500*time.Millisecond {
		t.Fatalf("animate drag translate keys = %+v", tl.Translate)
	}
	if math.Abs(float64(tl.Translate[0].X-5)) > 1e-3 || math.Abs(float64(tl.Translate[0].Y+3)) > 1e-3 {
		t.Fatalf("animate drag key delta = (%v,%v), want (5,-3)", tl.Translate[0].X, tl.Translate[0].Y)
	}
}

func TestPlaceRigInSceneAndMoveFileRefRewrite(t *testing.T) {
	runnerPath := filepath.Join("..", "skeletal", "testdata", "runner.skel.yaml")
	runnerBytes, err := os.ReadFile(runnerPath)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rigs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	rigAbs := filepath.Join(root, "rigs", "runner.skel.yaml")
	if err := os.WriteFile(rigAbs, runnerBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	gameYAML := "version: 1\nname: RigDemo\nid: dev.example.rigdemo\nappVersion: 0.1.0\nwindow:\n  title: RigDemo\n  width: 640\n  height: 360\n"
	if err := os.WriteFile(filepath.Join(root, "game.yaml"), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	fsAssets := sceneformat.FSAssets{
		FS:      os.DirFS(root),
		Atlases: sceneformat.AtlasMap{"runner": testRunnerAtlas()},
	}
	ed, err := editor.New(nil, fsAssets)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(root); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenRigProjectFile("rigs/runner.skel.yaml", "runner"); err != nil {
		t.Fatalf("OpenRigProjectFile: %v", err)
	}
	if err := ed.SelectRigSkin("armored"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SelectRigAnimation("walk"); err != nil {
		t.Fatal(err)
	}

	// Place the open rig into the scene with chosen skin ("armored") and animation ("walk").
	id, err := ed.PlaceOpenRig("", 100, 120)
	if err != nil {
		t.Fatalf("PlaceOpenRig: %v", err)
	}
	if ed.BuildErr != nil {
		t.Fatalf("BuildErr after PlaceOpenRig: %v", ed.BuildErr)
	}
	bo := ed.Built.Object(id)
	if bo == nil || bo.Skeleton == nil {
		t.Fatal("placed rig has no built Skeleton")
	}
	if bo.Skeleton.Skin() != "armored" || bo.Skeleton.Animation() != "walk" {
		t.Fatalf("placed rig skin=%q anim=%q, want armored/walk", bo.Skeleton.Skin(), bo.Skeleton.Animation())
	}

	// Save scene to disk and verify refs.Scan detects FieldSkeletalRig and MoveFile rewrites it.
	sceneAbs := filepath.Join(root, "scenes", "level.scene.yaml")
	if err := ed.SaveFile(sceneAbs); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	scanned, err := refs.Scan(os.DirFS(root))
	if err != nil {
		t.Fatalf("refs.Scan: %v", err)
	}
	foundRef := false
	for _, r := range scanned.Refs {
		if r.Field == refs.FieldSkeletalRig && r.Target == "rigs/runner.skel.yaml" {
			foundRef = true
		}
	}
	if !foundRef {
		t.Fatalf("refs.Scan did not find skeletal rig ref: %+v", scanned.Refs)
	}

	if err := ed.MoveFile("rigs/runner.skel.yaml", "rigs/hero.skel.yaml"); err != nil {
		t.Fatalf("MoveFile: %v", err)
	}
	if ed.RigPath() != filepath.Join(root, "rigs", "hero.skel.yaml") {
		t.Fatalf("RigPath after MoveFile = %q", ed.RigPath())
	}
	obj, _, _ := ed.FindObject(id)
	if got := obj.Components[sceneformat.SkeletalComponentName].(map[string]any)["rig"]; got != "rigs/hero.skel.yaml" {
		t.Fatalf("scene object skeletal.rig after MoveFile = %v", got)
	}

	// Viewport draws placed skeletal object without error.
	var d core.Drawing
	ed.DrawViewport(&d, core.Size{Width: 640, Height: 360})
	if len(d.Ops) == 0 {
		t.Fatal("DrawViewport produced no ops for placed rig")
	}
}

func TestRigPanelUIWorkflow(t *testing.T) {
	runnerPath := filepath.Join("..", "skeletal", "testdata", "runner.skel.yaml")
	runnerBytes, err := os.ReadFile(runnerPath)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rigs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "game.yaml"), []byte("version: 1\nname: UIRig\nid: dev.example.uirig\nappVersion: 0.1.0\nwindow:\n  title: UIRig\n  width: 640\n  height: 360\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rigs", "runner.skel.yaml"), runnerBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	ed, err := editor.New(nil, sceneformat.FSAssets{
		FS:      fstest.MapFS{"rigs/runner.skel.yaml": &fstest.MapFile{Data: runnerBytes}},
		Atlases: sceneformat.AtlasMap{"runner": testRunnerAtlas()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(root); err != nil {
		t.Fatal(err)
	}

	var saved []byte
	ed.OnSaveRig = func(data []byte) error {
		saved = append([]byte(nil), data...)
		return nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))

	// Select runner.skel.yaml in FilesView and press Open Rig.
	openFileBrowserInUI(app, ed)
	app.Press("id:files-row-rigs/runner.skel.yaml")
	app.Press("id:files-open-rig")
	if !ed.RigOpen() {
		t.Fatal("rig editor did not open from FilesView")
	}

	// Save via UI without edits must return exact original bytes.
	app.Press("id:rig-save")
	if !bytes.Equal(saved, runnerBytes) {
		t.Fatal("UI Save Rig on untouched rig changed bytes")
	}

	// Switch tabs and add a bone through the UI.
	app.Press("id:rig-add-bone")
	if len(ed.RigData().Bones) != 8 {
		t.Fatalf("bone count after UI Add Bone = %d, want 8", len(ed.RigData().Bones))
	}
	app.Press("id:rig-tab-slots")
	app.Press("id:rig-tab-timeline")
	app.Press("id:rig-close")
	if ed.RigOpen() {
		t.Fatal("rig editor did not close")
	}
}

func TestRigNilEditorAndPlayModeGuards(t *testing.T) {
	var nilEd *editor.Editor
	for name, err := range map[string]error{
		"NewRig":             nilEd.NewRig("hero", ""),
		"OpenRigFile":        nilEd.OpenRigFile("hero.skel.yaml", ""),
		"OpenRigProjectFile": nilEd.OpenRigProjectFile("rigs/hero.skel.yaml", ""),
		"OpenRigYAML":        nilEd.OpenRigYAML("hero.skel.yaml", nil, ""),
		"CloseRig":           nilEd.CloseRig(),
		"SaveRigFile":        nilEd.SaveRigFile("hero.skel.yaml"),
		"PlayRig":            nilEd.PlayRig(),
		"CrossfadeRig":       nilEd.CrossfadeRig("walk", 100*time.Millisecond),
		"AddRigBone":         nilEd.AddRigBone("b", "", 0, 0, 0, 10),
	} {
		if err == nil {
			t.Errorf("nil Editor.%s = nil, want error", name)
		}
	}
	if _, err := nilEd.PlaceRig("", "rigs/hero.skel.yaml", "", "", "", 0, 0); err == nil {
		t.Error("nil Editor.PlaceRig = nil, want error")
	}

	ed, err := editor.New(nil, sceneformat.AtlasMap{"runner": testRunnerAtlas()})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.NewRig("hero", "runner"); err != nil {
		t.Fatal(err)
	}
	_ = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))
	if err := ed.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	defer func() { _ = ed.Stop() }()

	outPath := filepath.Join(t.TempDir(), "hero.skel.yaml")
	if err := ed.SaveRigFile(outPath); !errors.Is(err, editor.ErrPlaying) {
		t.Fatalf("SaveRigFile while playing = %v, want ErrPlaying", err)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("SaveRigFile wrote %s while playing", outPath)
	}
}

func TestRigProjectSwitchUnsavedAndSymlinkSafety(t *testing.T) {
	runnerPath := filepath.Join("..", "skeletal", "testdata", "runner.skel.yaml")
	runnerBytes, err := os.ReadFile(runnerPath)
	if err != nil {
		t.Fatal(err)
	}

	makeProject := func(name string) string {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "rigs"), 0o755); err != nil {
			t.Fatal(err)
		}
		yaml := "version: 1\nname: " + name + "\nid: dev.example." + strings.ToLower(name) + "\nappVersion: 0.1.0\nwindow:\n  title: " + name + "\n  width: 640\n  height: 360\n"
		if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "rigs", "runner.skel.yaml"), runnerBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	projA := makeProject("ProjA")
	projB := makeProject("ProjB")

	ed, err := editor.New(nil, sceneformat.AtlasMap{"runner": testRunnerAtlas()})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(projA); err != nil {
		t.Fatal(err)
	}

	// Symlink escaping projA must be refused by OpenRigProjectFile.
	if runtime.GOOS != "windows" {
		outside := filepath.Join(t.TempDir(), "escape.skel.yaml")
		if err := os.WriteFile(outside, runnerBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(projA, "rigs", "escape.skel.yaml")
		if err := os.Symlink(outside, link); err == nil {
			if err := ed.OpenRigProjectFile("rigs/escape.skel.yaml", ""); err == nil {
				t.Fatal("OpenRigProjectFile accepted symlink escaping project root")
			}
		}
	}

	if err := ed.OpenRigProjectFile("rigs/runner.skel.yaml", "runner"); err != nil {
		t.Fatal(err)
	}
	if ed.RigDirty() || ed.HasUnsavedChanges() {
		t.Fatalf("clean rig reported dirty=%v unsaved=%v", ed.RigDirty(), ed.HasUnsavedChanges())
	}

	// Editing the rig marks both RigDirty and HasUnsavedChanges; SaveAll writes it and clears both.
	if err := ed.AddRigBone("tail", "root", 0, 8, 0, 10); err != nil {
		t.Fatal(err)
	}
	if !ed.RigDirty() || !ed.HasUnsavedChanges() {
		t.Fatalf("edited rig dirty=%v unsaved=%v, want true/true", ed.RigDirty(), ed.HasUnsavedChanges())
	}
	if err := ed.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}
	if ed.RigDirty() || ed.HasUnsavedChanges() {
		t.Fatalf("after SaveAll dirty=%v unsaved=%v, want false/false", ed.RigDirty(), ed.HasUnsavedChanges())
	}
	savedOnDisk, err := os.ReadFile(filepath.Join(projA, "rigs", "runner.skel.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(savedOnDisk, []byte("name: tail")) {
		t.Fatal("SaveAll did not persist edited rig to disk")
	}

	// Start rig preview playback, then switch projects: OpenProject must stop loop and close rig.
	if err := ed.SelectRigAnimation("walk"); err != nil {
		t.Fatal(err)
	}
	if err := ed.PlayRig(); err != nil {
		t.Fatal(err)
	}
	if !ed.RigPlaying() {
		t.Fatal("expected RigPlaying() = true before OpenProject")
	}
	if err := ed.OpenProject(projB); err != nil {
		t.Fatalf("OpenProject(projB): %v", err)
	}
	if ed.RigOpen() || ed.RigPlaying() {
		t.Fatalf("after OpenProject: RigOpen=%v RigPlaying=%v, want false/false", ed.RigOpen(), ed.RigPlaying())
	}

	// OpenDemo must also stop and clear an open rig.
	if err := ed.OpenRigProjectFile("rigs/runner.skel.yaml", "runner"); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if ed.RigOpen() {
		t.Fatal("OpenDemo left previous project rig open")
	}

	// Standalone rig opened from an absolute OS path outside any project must be refused by PlaceOpenRig.
	standalone := filepath.Join(t.TempDir(), "outside.skel.yaml")
	if err := os.WriteFile(standalone, runnerBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenRigFile(standalone, "runner"); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.PlaceOpenRig("", 10, 20); err == nil {
		t.Fatal("PlaceOpenRig accepted standalone absolute OS path outside project")
	}
}

func TestRigHandlePriorityAndCurvePreservation(t *testing.T) {
	ed, err := editor.New(nil, sceneformat.AtlasMap{"runner": testRunnerAtlas()})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.NewRig("hero", "runner"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddRigBone("arm", "root", 10, 0, 0, 14); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddRigSlot("arm", "arm", "", 1); err != nil {
		t.Fatal(err)
	}
	if err := ed.AttachRigFrame("default", "arm", "arm_front", 3, 2); err != nil {
		t.Fatal(err)
	}
	ed.SetRigCanvasSize(400, 300)

	// Attachment sits at (0, 0) on top of the bone joint: selecting slot prioritizes RigHandleAttachmentOrigin;
	// selecting bone prioritizes RigHandleBoneMove.
	ed.SelectRigBone("arm")
	ed.SelectRigSlot("arm")
	origin, _, _, ok := ed.RigBoneHandlePos("arm")
	if !ok {
		t.Fatal("RigBoneHandlePos(arm) = false")
	}
	if h := ed.PickRigHandle(origin.X, origin.Y); h != editor.RigHandleAttachmentOrigin {
		t.Fatalf("PickRigHandle after SelectRigSlot = %v, want RigHandleAttachmentOrigin", h)
	}
	ed.SelectRigBone("arm")
	if h := ed.PickRigHandle(origin.X, origin.Y); h != editor.RigHandleBoneMove {
		t.Fatalf("PickRigHandle after SelectRigBone = %v, want RigHandleBoneMove", h)
	}

	// Animate-mode handle drag at an existing CurveBezier keyframe preserves CurveBezier + control points.
	wantBz := [4]float32{0.2, 0.0, 0.4, 1.0}
	if err := ed.AddRigAnimation("wave", time.Second, true); err != nil {
		t.Fatal(err)
	}
	if err := ed.KeyRigBoneTranslate("wave", "arm", 500*time.Millisecond, 1, 2, skeletal.CurveBezier, wantBz); err != nil {
		t.Fatal(err)
	}
	if err := ed.KeyRigBoneRotate("wave", "arm", 500*time.Millisecond, 0.25, skeletal.CurveBezier, wantBz); err != nil {
		t.Fatal(err)
	}
	ed.SeekRig(500 * time.Millisecond)

	origin, _, ring, _ := ed.RigBoneHandlePos("arm")
	if h := ed.BeginRigDrag(origin.X, origin.Y); h != editor.RigHandleBoneMove {
		t.Fatalf("BeginRigDrag translate = %v", h)
	}
	ed.UpdateRigDrag(origin.X+6, origin.Y-4)
	if !ed.EndRigDrag() {
		t.Fatal("EndRigDrag translate = false")
	}
	origin, _, ring, _ = ed.RigBoneHandlePos("arm")
	if h := ed.BeginRigDrag(origin.X+ring, origin.Y); h != editor.RigHandleBoneRotate {
		t.Fatalf("BeginRigDrag rotate = %v", h)
	}
	ed.UpdateRigDrag(origin.X, origin.Y+ring)
	if !ed.EndRigDrag() {
		t.Fatal("EndRigDrag rotate = false")
	}

	tl := ed.RigData().Animations["wave"].Bones["arm"]
	if len(tl.Translate) != 1 || tl.Translate[0].Curve != skeletal.CurveBezier || tl.Translate[0].Bezier != wantBz {
		t.Fatalf("translate key lost curve/bezier: %+v", tl.Translate)
	}
	if len(tl.Rotate) != 1 || tl.Rotate[0].Curve != skeletal.CurveBezier || tl.Rotate[0].Bezier != wantBz {
		t.Fatalf("rotate key lost curve/bezier: %+v", tl.Rotate)
	}
}

func TestRigPanelUIDoesNotRemountOnPlaybackAndEscapeCancelsDrag(t *testing.T) {
	runnerPath := filepath.Join("..", "skeletal", "testdata", "runner.skel.yaml")
	runnerBytes, err := os.ReadFile(runnerPath)
	if err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(nil, sceneformat.AtlasMap{"runner": testRunnerAtlas()})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenRigYAML("rigs/runner.skel.yaml", runnerBytes, "runner"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SelectRigAnimation("walk"); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 820))

	// Record mounted NodeIDs in Bones tab, run multiple playback ticks and a bone value edit,
	// and verify neither remounts the controls.
	boneInputBefore := app.Find("id:rig-new-bone-name").ID
	boneXBefore := app.Find("id:rig-bone-x").ID
	if err := ed.PlayRig(); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		ed.StepRig(16 * time.Millisecond)
		app.Settle()
	}
	if got := app.Find("id:rig-new-bone-name").ID; got != boneInputBefore {
		t.Fatalf("rig-new-bone-name remounted during PlayRig ticks: %v -> %v", boneInputBefore, got)
	}
	if got := app.Find("id:rig-bone-x").ID; got != boneXBefore {
		t.Fatalf("rig-bone-x remounted during PlayRig ticks: %v -> %v", boneXBefore, got)
	}
	ed.PauseRig()
	if err := ed.MoveRigBone(ed.SelectedRigBone(), 7, -3); err != nil {
		t.Fatal(err)
	}
	app.Settle()
	if got := app.Find("id:rig-bone-x").ID; got != boneXBefore {
		t.Fatalf("rig-bone-x remounted after bone value edit: %v -> %v", boneXBefore, got)
	}

	// Switch to Slots & Skins tab and verify rig-attach-frame tracks each slot's live frame (no stale signal)
	// and does not remount during playback ticks.
	viewTreeText := func(v *nsgotest.View) string {
		var out []string
		var walk func(*nsgotest.View)
		walk = func(n *nsgotest.View) {
			if n == nil {
				return
			}
			if n.Text != "" {
				out = append(out, n.Text)
			}
			for _, c := range n.Children {
				walk(c)
			}
		}
		walk(v)
		return strings.Join(out, " ")
	}
	app.Press("id:rig-tab-slots")
	ed.SelectRigSlot("torso")
	app.Settle()
	attachBefore := app.Find("id:rig-attach-frame").ID
	if got := viewTreeText(app.Find("id:rig-attach-frame")); !strings.Contains(got, "torso") {
		t.Fatalf("rig-attach-frame for torso = %q, want torso", got)
	}
	ed.SelectRigSlot("head")
	app.Settle()
	if got := viewTreeText(app.Find("id:rig-attach-frame")); !strings.Contains(got, "head") {
		t.Fatalf("rig-attach-frame for head = %q, want head", got)
	}
	if got := app.Find("id:rig-attach-frame").ID; got != attachBefore {
		t.Fatalf("rig-attach-frame remounted on slot switch: %v -> %v", attachBefore, got)
	}

	// Switch to Timeline tab and verify scrubber and inputs stay mounted across 10 PlayRig ticks.
	app.Press("id:rig-tab-timeline")
	scrubBefore := app.Find("id:rig-scrub").ID
	animInputBefore := app.Find("id:rig-new-anim-name").ID
	if err := ed.PlayRig(); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		ed.StepRig(16 * time.Millisecond)
		app.Settle()
	}
	if got := app.Find("id:rig-scrub").ID; got != scrubBefore {
		t.Fatalf("rig-scrub remounted during PlayRig ticks: %v -> %v", scrubBefore, got)
	}
	if got := app.Find("id:rig-new-anim-name").ID; got != animInputBefore {
		t.Fatalf("rig-new-anim-name remounted during PlayRig ticks: %v -> %v", animInputBefore, got)
	}
	ed.PauseRig()

	// Pressing Escape during an active rig handle drag cancels it cleanly.
	if err := ed.SelectRigAnimation(""); err != nil {
		t.Fatal(err)
	}
	ed.SelectRigBone("root")
	ed.SetRigCanvasSize(340, 260)
	origin, _, _, ok := ed.RigBoneHandlePos("root")
	if !ok {
		t.Fatal("RigBoneHandlePos(root) = false")
	}
	if h := ed.BeginRigDrag(origin.X, origin.Y); h == editor.RigHandleNone {
		t.Fatal("BeginRigDrag returned None")
	}
	app.Settle()
	app.Key("escape")
	if got := ed.DraggingRig(); got != editor.RigHandleNone {
		t.Fatalf("DraggingRig after Escape = %v, want RigHandleNone", got)
	}

	// Entering Play mode disables rig-save.
	if err := ed.Play(); err != nil {
		t.Fatal(err)
	}
	app.Settle()
	if app.Enabled("id:rig-save") {
		t.Fatal("rig-save should be disabled while Play mode is active")
	}
	if err := ed.Stop(); err != nil {
		t.Fatal(err)
	}
}

package editor_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/cutscene"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const floor1IntroBeforeYAML = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/cutscene.v2.json
version: 2
kind: timeline
scene: scenes/level_01.scene.yaml
length: 12s
skippable: true
skipAfter: 1s
tracks:
  - id: camera
    name: Camera
    kind: camera
    clips:
      - id: cam1
        label: Hold on door
        start: 0s
        duration: 1s
        easing: hold
        action: hold
        target: door
      - id: cam2
        label: Pan to knight
        start: 1s
        duration: 2.4s
        easing: easeInOut
        action: pan
        target: knight
      - id: cam3
        label: Follow knight
        start: 3.4s
        duration: 8.6s
        easing: linear
        action: follow
        target: knight
  - id: knight
    name: Knight
    kind: actor
    target: knight
    clips:
      - id: kn1
        label: Walk in
        start: 1s
        duration: 2.4s
        easing: easeOut
        action: walk
        fromX: 60
        toX: 220
        state: run
      - id: kn2
        label: Idle
        start: 3.4s
        duration: 4.6s
        action: state
        state: idle
      - id: kn3
        label: Run right
        start: 8s
        duration: 4s
        easing: easeIn
        action: walk
        fromX: 220
        toX: 600
        state: run
  - id: dialogue
    name: Dialogue
    kind: dialogue
    clips:
      - id: dl1
        start: 3.6s
        duration: 2.4s
        line: intro.knight.01
  - id: sfx
    name: Sound
    kind: sound
    group: sfx
    clips:
      - id: sx1
        label: door_slam
        start: 200ms
        duration: 600ms
        source: audio/door_slam.wav
  - id: music
    name: Music
    kind: music
    group: music
    clips:
      - id: mu1
        label: cellar_theme
        start: 0s
        duration: 12s
        source: audio/cellar_theme.ogg
        loop: true
`

const floor1IntroAfterYAML = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/cutscene.v2.json
version: 2
kind: timeline
scene: scenes/level_01.scene.yaml
length: 12s
skippable: true
skipAfter: 1s
tracks:
  - id: camera
    name: Camera
    kind: camera
    clips:
      - id: cam1
        label: Hold on door
        start: 0s
        duration: 1.6s
        easing: hold
        action: hold
        target: door
      - id: cam2
        label: Pan to knight
        start: 1.6s
        duration: 2.4s
        easing: easeInOut
        action: pan
        target: knight
      - id: cam3
        label: Follow knight
        start: 4s
        duration: 8s
        easing: linear
        action: follow
        target: knight
  - id: knight
    name: Knight
    kind: actor
    target: knight
    clips:
      - id: kn1
        label: Walk in
        start: 1s
        duration: 2.4s
        easing: easeOut
        action: walk
        fromX: 60
        toX: 220
        state: run
      - id: kn2
        label: Idle
        start: 3.4s
        duration: 4.6s
        action: state
        state: idle
      - id: kn3
        label: Run right
        start: 8s
        duration: 4s
        easing: easeIn
        action: walk
        fromX: 220
        toX: 600
        state: run
  - id: slime
    name: slime_01
    kind: actor
    target: slime_01
    clips:
      - id: sl1
        label: Drop
        start: 6s
        duration: 800ms
        easing: easeIn
        action: drop
        fromY: -40
        toY: 276
      - id: sl2
        label: Patrol
        start: 6.8s
        duration: 5.2s
        action: patrol
        fromX: 420
        toX: 520
        speed: 40
  - id: dialogue
    name: Dialogue
    kind: dialogue
    clips:
      - id: dl1
        start: 3.6s
        duration: 2.4s
        line: intro.knight.01
      - id: dl2
        start: 8.2s
        duration: 2.2s
        line: intro.knight.02
  - id: sfx
    name: Sound
    kind: sound
    group: sfx
    clips:
      - id: sx1
        label: door_slam
        start: 200ms
        duration: 600ms
        source: audio/door_slam.wav
      - id: sx2
        label: slime_drop
        start: 6s
        duration: 500ms
        source: audio/slime_drop.wav
  - id: music
    name: Music
    kind: music
    group: music
    clips:
      - id: mu1
        label: cellar_theme
        start: 2s
        duration: 10s
        source: audio/cellar_theme.ogg
        fadeIn: 2s
        loop: true
`

const leverRevealYAML = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/cutscene.v2.json
version: 2
kind: timeline
scene: scenes/level_01.scene.yaml
length: 6.5s
tracks:
  - id: camera
    name: Camera
    kind: camera
    clips:
      - id: lc1
        label: Pan to lever
        start: 0s
        duration: 6.5s
        action: hold
        target: lever
`

const floor3BossVideoYAML = `version: 1
source: asset:cutscenes/floor3_boss.mp4
group: cutscenes
skippable: true
skipAfter: 1.5s
fit: cover
`

const locEnYAML = `version: 1
locale: en
strings:
  intro.knight.01: "Another floor. Another locked door."
  intro.knight.02: "The lever has to be down here somewhere."
`

const locZhYAML = `version: 1
locale: zh
strings:
  intro.knight.01: "又一层。又一扇锁着的门。"
  intro.knight.02: "拉杆一定就在这下面。"
`

func cutsceneAtlas(t *testing.T) *sprite.Atlas {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 96, 32))
	fill := func(x0, y0, w, h int, c color.RGBA) {
		for y := y0; y < y0+h; y++ {
			for x := x0; x < x0+w; x++ {
				img.SetRGBA(x, y, c)
			}
		}
	}
	// knight frame (0..32)
	fill(8, 2, 16, 28, color.RGBA{R: 0xD7, G: 0xDE, B: 0xEA, A: 0xFF})
	fill(10, 0, 8, 4, color.RGBA{R: 0xE9, G: 0xA8, B: 0x4A, A: 0xFF})
	// slime frame (32..64)
	fill(36, 12, 24, 18, color.RGBA{R: 0x4F, G: 0xD3, B: 0x8A, A: 0xFF})
	// door frame (64..96)
	fill(68, 2, 24, 30, color.RGBA{R: 0x6B, G: 0x5A, B: 0x45, A: 0xFF})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return &sprite.Atlas{
		Image: core.ImageSource{Data: buf.Bytes(), Size: core.Size{Width: 96, Height: 32}},
		Frames: map[string]sprite.Frame{
			"knight": {Name: "knight", X: 0, Y: 0, W: 32, H: 32},
			"slime":  {Name: "slime", X: 32, Y: 0, W: 32, H: 32},
			"door":   {Name: "door", X: 64, Y: 0, W: 32, H: 32},
		},
	}
}

func dungeonCutsceneSceneDoc() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Floor 1",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#15181d",
		Camera:          &sceneformat.CameraDoc{X: 320, Y: 180},
		Layers: []sceneformat.LayerDoc{{
			Name:  "actors",
			Space: sceneformat.SpaceWorld,
			Order: 0,
			Objects: []sceneformat.ObjectDoc{
				{ID: "door", Name: "Door", X: 56, Y: 260, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "door"}},
				{ID: "knight", Name: "Knight", X: 60, Y: 260, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "knight"}},
				{ID: "slime_01", Name: "Slime", X: 420, Y: -40, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"}},
				{ID: "lever", Name: "Lever", X: 560, Y: 260},
			},
		}},
	}
}

func dungeonCutsceneCatalog() *l10n.Catalog {
	cat := l10n.NewCatalog("en")
	cat.Set("en", "intro.knight.01", "Another floor. Another locked door.")
	cat.Set("en", "intro.knight.02", "The lever has to be down here somewhere.")
	cat.Set("zh", "intro.knight.01", "又一层。又一扇锁着的门。")
	cat.Set("zh", "intro.knight.02", "拉杆一定就在这下面。")
	return cat
}

func writeDungeonCutsceneProject(t *testing.T) (dir string, ed *editor.Editor) {
	t.Helper()
	dir = t.TempDir()
	writeFile := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("game.yaml", "version: 1\nname: dungeon-run\nid: dev.nsgo.dungeon\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: level_01\n  scenes:\n    - name: level_01\n      path: scenes/level_01.scene.yaml\n")
	sceneBytes, err := sceneformat.Encode(dungeonCutsceneSceneDoc())
	if err != nil {
		t.Fatal(err)
	}
	writeFile("scenes/level_01.scene.yaml", string(sceneBytes))
	writeFile("loc/en.yaml", locEnYAML)
	writeFile("loc/zh.yaml", locZhYAML)
	writeFile("cutscenes/floor1_intro.cutscene.yaml", floor1IntroBeforeYAML)
	writeFile("cutscenes/lever_reveal.cutscene.yaml", leverRevealYAML)
	writeFile("cutscenes/floor3_boss.cutscene.yaml", floor3BossVideoYAML)
	writeFile("audio/door_slam.wav", "RIFF")
	writeFile("audio/slime_drop.wav", "RIFF")
	writeFile("audio/cellar_theme.ogg", "OggS")

	ed, err = editor.New(nil, sceneformat.AtlasMap{"actors": cutsceneAtlas(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.Resolver = sceneformat.AtlasMap{"actors": cutsceneAtlas(t)}
	ed.Rebuild()
	return dir, ed
}

// #1056 acceptance 1: Scrubbing to a time shows the scene posed as the player would show it at that time.
func TestCutsceneScrubMatchesTimelinePlayer(t *testing.T) {
	atlas := cutsceneAtlas(t)
	ed, err := editor.New(dungeonCutsceneSceneDoc(), sceneformat.AtlasMap{"actors": atlas})
	if err != nil {
		t.Fatal(err)
	}
	cat := dungeonCutsceneCatalog()
	ed.CutsceneCatalog = cat

	if err := ed.OpenCutsceneYAML([]byte(floor1IntroAfterYAML)); err != nil {
		t.Fatalf("OpenCutsceneYAML: %v", err)
	}

	// Build an independent cutscene.TimelinePlayer on a separate BuiltScene.
	parsedCut, err := cutscene.Parse([]byte(floor1IntroAfterYAML))
	if err != nil {
		t.Fatal(err)
	}
	refBuilt, err := sceneformat.Build(dungeonCutsceneSceneDoc(), sceneformat.AtlasMap{"actors": atlas})
	if err != nil {
		t.Fatal(err)
	}
	refPlayer, err := cutscene.NewTimelinePlayer(parsedCut, cutscene.TimelineTarget{
		Scene:   refBuilt,
		Catalog: cat,
		Locale:  "en",
	})
	if err != nil {
		t.Fatalf("NewTimelinePlayer: %v", err)
	}

	times := []time.Duration{
		0,
		1200 * time.Millisecond,
		2500 * time.Millisecond,
		4500 * time.Millisecond,
		6400 * time.Millisecond,
		8500 * time.Millisecond,
		10000 * time.Millisecond,
		12000 * time.Millisecond,
	}
	for _, at := range times {
		ed.SeekCutscene(at)
		refPlayer.Seek(at)

		preview := ed.CutscenePreviewScene()
		if preview == nil {
			t.Fatalf("at %v: CutscenePreviewScene is nil", at)
		}
		for _, actorID := range []string{"knight", "slime_01"} {
			gotObj := preview.Object(actorID)
			wantObj := refBuilt.Object(actorID)
			if gotObj == nil || wantObj == nil {
				t.Fatalf("at %v: missing object %q", at, actorID)
			}
			if math.Abs(float64(gotObj.Doc.X-wantObj.Doc.X)) > 1e-4 || math.Abs(float64(gotObj.Doc.Y-wantObj.Doc.Y)) > 1e-4 {
				t.Fatalf("at %v actor %q pos = (%.3f, %.3f), want (%.3f, %.3f)", at, actorID, gotObj.Doc.X, gotObj.Doc.Y, wantObj.Doc.X, wantObj.Doc.Y)
			}
			if ed.CutscenePreviewPlayer().ActorState(actorID) != refPlayer.ActorState(actorID) {
				t.Fatalf("at %v actor %q state = %q, want %q", at, actorID, ed.CutscenePreviewPlayer().ActorState(actorID), refPlayer.ActorState(actorID))
			}
		}
		if preview.Camera == nil || refBuilt.Camera == nil {
			t.Fatalf("at %v: missing camera", at)
		}
		if math.Abs(float64(preview.Camera.X-refBuilt.Camera.X)) > 1e-4 || math.Abs(float64(preview.Camera.Y-refBuilt.Camera.Y)) > 1e-4 {
			t.Fatalf("at %v camera = (%.3f, %.3f), want (%.3f, %.3f)", at, preview.Camera.X, preview.Camera.Y, refBuilt.Camera.X, refBuilt.Camera.Y)
		}
		if gotDL, wantDL := ed.CutsceneActiveDialogue(), refPlayer.ActiveDialogue(); gotDL != wantDL {
			t.Fatalf("at %v dialogue = %+v, want %+v", at, gotDL, wantDL)
		}
	}
}

// #1056 acceptance 2: Reverting one of five changes and keeping the review writes the other four in one undo step.
func TestCutsceneReviewRevertOneOfFiveKeepInOneUndoStep(t *testing.T) {
	dir, ed := writeDungeonCutsceneProject(t)
	cutFile := filepath.Join(dir, "cutscenes", "floor1_intro.cutscene.yaml")
	beforeDiskBytes, err := os.ReadFile(cutFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenCutsceneProjectFile("cutscenes/floor1_intro.cutscene.yaml"); err != nil {
		t.Fatalf("OpenCutsceneProjectFile: %v", err)
	}
	proposed, err := cutscene.Parse([]byte(floor1IntroAfterYAML))
	if err != nil {
		t.Fatal(err)
	}

	histBefore := ed.History.Len()
	if err := ed.OpenCutsceneReview(editor.CutsceneReviewRequest{
		Prompt:   "make the floor 1 intro reveal a slime and hint at the lever",
		Path:     cutFile,
		Proposed: &proposed,
	}); err != nil {
		t.Fatalf("OpenCutsceneReview: %v", err)
	}

	changes := ed.CutsceneReviewChanges()
	if len(changes) != 5 {
		t.Fatalf("len(changes) = %d, want 5: %+v", len(changes), changes)
	}
	if kept, total := ed.CutsceneReviewKeptCount(); kept != 5 || total != 5 {
		t.Fatalf("kept=%d total=%d, want 5 of 5", kept, total)
	}
	// Disk must remain untouched while the review is open.
	if raw, _ := os.ReadFile(cutFile); !bytes.Equal(raw, beforeDiskBytes) {
		t.Fatal("opening a review modified the cutscene file on disk before Keep")
	}

	// Revert 1 of 5 changes (find the slime_drop SFX change).
	sfxIdx := -1
	for i, ch := range changes {
		if strings.Contains(strings.ToLower(ch.Title), "slime_drop") {
			sfxIdx = i
			break
		}
	}
	if sfxIdx < 0 {
		t.Fatalf("could not find slime_drop change in %+v", changes)
	}
	if err := ed.ToggleCutsceneReviewChange(sfxIdx); err != nil {
		t.Fatalf("ToggleCutsceneReviewChange: %v", err)
	}
	if kept, total := ed.CutsceneReviewKeptCount(); kept != 4 || total != 5 {
		t.Fatalf("after reverting 1: kept=%d total=%d, want 4 of 5", kept, total)
	}

	// Keep the remaining 4 of 5 changes.
	if err := ed.KeepCutsceneReview(); err != nil {
		t.Fatalf("KeepCutsceneReview: %v", err)
	}
	if ed.CutsceneReviewOpen() {
		t.Fatal("review should be closed after KeepCutsceneReview")
	}
	if ed.History.Len() != histBefore+1 {
		t.Fatalf("History.Len() = %d, want %d (exactly one undo step)", ed.History.Len(), histBefore+1)
	}

	// Verify the file on disk has the 4 kept changes and does NOT have the reverted slime_drop clip.
	writtenBytes, err := os.ReadFile(cutFile)
	if err != nil {
		t.Fatal(err)
	}
	written, err := cutscene.Parse(writtenBytes)
	if err != nil {
		t.Fatalf("Parse kept cutscene on disk: %v", err)
	}
	var hasSlimeTrack, hasLine2, hasSlimeDrop, hasCam16, hasMusicFade bool
	for _, tr := range written.Tracks {
		if tr.Kind == cutscene.TrackActor && tr.Target == "slime_01" {
			hasSlimeTrack = true
		}
		for _, cl := range tr.Clips {
			if cl.ID == "cam1" && time.Duration(cl.Duration) == 1600*time.Millisecond {
				hasCam16 = true
			}
			if cl.Line == "intro.knight.02" {
				hasLine2 = true
			}
			if cl.ID == "sx2" || cl.Source == "audio/slime_drop.wav" {
				hasSlimeDrop = true
			}
			if cl.ID == "mu1" && time.Duration(cl.Start) == 2*time.Second && time.Duration(cl.FadeIn) == 2*time.Second {
				hasMusicFade = true
			}
		}
	}
	if !hasCam16 || !hasSlimeTrack || !hasLine2 || !hasMusicFade {
		t.Fatalf("missing kept changes on disk: cam16=%v slime=%v line2=%v musicFade=%v", hasCam16, hasSlimeTrack, hasLine2, hasMusicFade)
	}
	if hasSlimeDrop {
		t.Fatal("reverted slime_drop clip was written to disk")
	}

	// ONE Undo reverts all four kept changes in memory and on disk.
	if !ed.Undo() {
		t.Fatal("ed.Undo() returned false")
	}
	afterUndoDisk, err := os.ReadFile(cutFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterUndoDisk, beforeDiskBytes) {
		t.Fatalf("disk after single Undo does not match original before bytes:\ngot:\n%s\nwant:\n%s", afterUndoDisk, beforeDiskBytes)
	}
	undoneDoc := ed.Cutscene()
	if len(undoneDoc.Tracks) != 5 {
		t.Fatalf("in-memory tracks after Undo = %d, want 5 (original before)", len(undoneDoc.Tracks))
	}

	// ONE Redo re-applies all four kept changes in memory and on disk.
	if !ed.Redo() {
		t.Fatal("ed.Redo() returned false")
	}
	afterRedoDisk, err := os.ReadFile(cutFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterRedoDisk, writtenBytes) {
		t.Fatal("disk after single Redo does not match kept 4-of-5 bytes")
	}
}

func TestCutsceneReviewBeforeAfterHoldBManualEditUndoAndPlayGuard(t *testing.T) {
	_, ed := writeDungeonCutsceneProject(t)
	if err := ed.OpenCutsceneProjectFile("cutscenes/floor1_intro.cutscene.yaml"); err != nil {
		t.Fatal(err)
	}
	proposed, err := cutscene.Parse([]byte(floor1IntroAfterYAML))
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenCutsceneReview(editor.CutsceneReviewRequest{
		Prompt:   "make the floor 1 intro reveal a slime and hint at the lever",
		Proposed: &proposed,
	}); err != nil {
		t.Fatal(err)
	}

	// At t = 6.8 s in After, slime_01 has dropped to y = 276; in Before (via hold B), slime_01 stays at initial y = -40.
	ed.SeekCutscene(6800 * time.Millisecond)
	if y := ed.CutscenePreviewScene().Object("slime_01").Doc.Y; math.Abs(float64(y-276)) > 1e-3 {
		t.Fatalf("After slime_01 Y at 6.8s = %v, want 276", y)
	}
	if !ed.CutsceneHandleKeyEvent(core.KeyEvent{Key: "b"}, true) {
		t.Fatal("expected B down to be handled while review is open")
	}
	if !ed.CutsceneReviewShowingBefore() || !ed.CutsceneReviewPeekingBefore() {
		t.Fatal("expected hold B to peek Before")
	}
	if y := ed.CutscenePreviewScene().Object("slime_01").Doc.Y; math.Abs(float64(y-(-40))) > 1e-3 {
		t.Fatalf("Before peek slime_01 Y at 6.8s = %v, want -40", y)
	}
	ed.CutsceneHandleKeyEvent(core.KeyEvent{Key: "b"}, false)
	if ed.CutsceneReviewShowingBefore() {
		t.Fatal("releasing B did not return to After")
	}

	// Modifier chords must NOT be swallowed.
	if ed.CutsceneHandleKeyEvent(core.KeyEvent{Key: "z", Modifiers: core.Modifiers{Platform: true}}, true) {
		t.Fatal("Cmd+Z must not be swallowed by CutsceneHandleKeyEvent")
	}
	if ed.CutsceneHandleKeyEvent(core.KeyEvent{Key: "b", Modifiers: core.Modifiers{Control: true}}, true) {
		t.Fatal("Ctrl+B must not be swallowed by CutsceneHandleKeyEvent")
	}

	// Manual edit joins the review, and Undo (Cmd+Z) undoes it without closing the review.
	if !ed.SelectCutsceneClipByID("dl2") {
		t.Fatal("SelectCutsceneClipByID(dl2) returned false")
	}
	if err := ed.AdjustCutsceneClipStart(100 * time.Millisecond); err != nil {
		t.Fatalf("AdjustCutsceneClipStart: %v", err)
	}
	if !ed.CutsceneReviewHasManualEdits() {
		t.Fatal("expected CutsceneReviewHasManualEdits() == true")
	}
	_, _, cl, _, _ := ed.SelectedCutsceneClip()
	if time.Duration(cl.Start) != 8300*time.Millisecond {
		t.Fatalf("dl2 start after +0.1s = %v, want 8.3s", time.Duration(cl.Start))
	}
	if !ed.Undo() {
		t.Fatal("Undo() of review-joined edit returned false")
	}
	if !ed.CutsceneReviewOpen() {
		t.Fatal("Undo() of review-joined edit should keep the review open")
	}
	if ed.CutsceneReviewHasManualEdits() {
		t.Fatal("expected CutsceneReviewHasManualEdits() == false after Undo")
	}
	_, _, cl, _, _ = ed.SelectedCutsceneClip()
	if time.Duration(cl.Start) != 8200*time.Millisecond {
		t.Fatalf("dl2 start after Undo = %v, want 8.2s", time.Duration(cl.Start))
	}

	// Revert all closes the review and restores base.
	if err := ed.RevertAllCutsceneReview(); err != nil {
		t.Fatalf("RevertAllCutsceneReview: %v", err)
	}
	if ed.CutsceneReviewOpen() {
		t.Fatal("review still open after RevertAllCutsceneReview")
	}
	if len(ed.Cutscene().Tracks) != 5 {
		t.Fatalf("tracks after RevertAll = %d, want 5", len(ed.Cutscene().Tracks))
	}

	// Verify Play mode guards.
	var rootScope *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		rootScope = cx
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))
	var playKey bool
	var editErr error
	app.Post(func() {
		if err := ed.PlayIn(rootScope); err != nil {
			t.Errorf("PlayIn: %v", err)
			return
		}
		playKey = ed.CutsceneHandleKeyEvent(core.KeyEvent{Key: " "}, true)
		editErr = ed.AdjustCutsceneClipDuration(100 * time.Millisecond)
		_ = ed.Stop()
	})
	app.Settle()
	if playKey {
		t.Fatal("CutsceneHandleKeyEvent must return false while playing")
	}
	if !errors.Is(editErr, editor.ErrPlaying) {
		t.Fatalf("AdjustCutsceneClipDuration while playing = %v, want ErrPlaying", editErr)
	}
}

// #1056 acceptance 3: Full UI verification + real nsgotest screenshots on macOS beside the artboard, using only Spec tokens.
func TestCutsceneTimelineUIAndScreenshot(t *testing.T) {
	dir, ed := writeDungeonCutsceneProject(t)
	cutFile := filepath.Join(dir, "cutscenes", "floor1_intro.cutscene.yaml")
	if err := ed.OpenCutsceneProjectFile("cutscenes/floor1_intro.cutscene.yaml"); err != nil {
		t.Fatalf("OpenCutsceneProjectFile: %v", err)
	}
	proposed, err := cutscene.Parse([]byte(floor1IntroAfterYAML))
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenCutsceneReview(editor.CutsceneReviewRequest{
		Prompt:   "make the floor 1 intro reveal a slime and hint at the lever",
		Path:     cutFile,
		Proposed: &proposed,
	}); err != nil {
		t.Fatalf("OpenCutsceneReview: %v", err)
	}
	ed.SelectCutsceneClipByID("cam2")
	ed.SeekCutscene(4500 * time.Millisecond)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// Left rail: CUTSCENES (including video kind marked) and LINES from loc (en & zh).
	for _, id := range []string{
		"id:cutscene-panel",
		"id:cutscene-list-floor1_intro",
		"id:cutscene-list-lever_reveal",
		"id:cutscene-list-floor3_boss",
		"id:cutscene-rail-line-intro.knight.01",
		"id:cutscene-rail-line-intro.knight.02",
	} {
		if !app.Exists(id) {
			t.Fatalf("missing left rail node %s", id)
		}
	}
	// Verify video kind is marked on floor3_boss.
	foundVideo := false
	for _, entry := range ed.ProjectCutscenes() {
		if entry.Name == "floor3_boss" && entry.Kind == cutscene.KindVideo {
			foundVideo = true
		}
	}
	if !foundVideo {
		t.Fatal("ProjectCutscenes did not mark floor3_boss as video kind")
	}

	// Preview stage: camera label, canvas, subtitle at 4.5 s, review legend.
	if got := app.Find("id:cutscene-camera-label").Text; !strings.Contains(got, "follow knight") {
		t.Fatalf("camera label at 4.5s = %q, want to contain 'follow knight'", got)
	}
	if got := app.Find("id:cutscene-subtitle").Text; got != "Another floor. Another locked door." {
		t.Fatalf("subtitle at 4.5s = %q, want English line 01", got)
	}
	if !app.Exists("id:cutscene-review-legend") {
		t.Fatal("missing review line-style legend")
	}

	// Timeline drawer: transport, 24 thumbnails, 6 tracks, clips.
	if got := app.Find("id:cutscene-time").Text; got != "4.5 s" {
		t.Fatalf("time label = %q, want '4.5 s'", got)
	}
	for _, id := range []string{
		"id:cutscene-play",
		"id:cutscene-thumb-0",
		"id:cutscene-thumb-23",
		"id:cutscene-track-0",
		"id:cutscene-track-5",
		"id:cutscene-clip-cam2",
		"id:cutscene-clip-sl1",
		"id:cutscene-clip-dl2",
		"id:cutscene-clip-mu1",
	} {
		if !app.Exists(id) {
			t.Fatalf("missing timeline node %s", id)
		}
	}

	// Right rail: 5 CHANGES + selected clip inspector (Start, Duration, Easing).
	if got := app.Find("id:cutscene-review-header").Text; got != "5 CHANGES · floor1_intro.cutscene.yaml" {
		t.Fatalf("review header = %q", got)
	}
	if got := app.Find("id:cutscene-review-keep-text").Text; got != "Keep 5 of 5" {
		t.Fatalf("keep button text = %q, want 'Keep 5 of 5'", got)
	}
	for i := range 5 {
		if !app.Exists("id:cutscene-change-" + string(rune('0'+i))) {
			t.Fatalf("missing change card %d", i)
		}
	}

	// Capture real macOS screenshot of Screen 2 After state!
	app.Screenshot("cutscene-timeline-review")

	// Click dialogue clip dl2 -> inspector shows line id, English, Chinese, and loc note.
	app.Press("id:cutscene-clip-dl2")
	if !app.Exists("id:cutscene-line-card") {
		t.Fatal("dialogue line card did not render for dl2")
	}
	if got := app.Find("id:cutscene-line-id").Text; got != "intro.knight.02" {
		t.Fatalf("line id = %q, want intro.knight.02", got)
	}
	if got := app.Find("id:cutscene-line-en").Text; got != "The lever has to be down here somewhere." {
		t.Fatalf("line en = %q", got)
	}
	if got := app.Find("id:cutscene-line-zh").Text; got != "拉杆一定就在这下面。" {
		t.Fatalf("line zh = %q", got)
	}
	if got := app.Find("id:cutscene-line-note").Text; got != "Text lives in loc/en.yaml and loc/zh.yaml; the cutscene stores only the id." {
		t.Fatalf("line note = %q", got)
	}

	// Click +0.1 s on Start -> manual edit joins review & notice appears; toolbar Undo removes it.
	app.Press("id:cutscene-start-up")
	if !app.Exists("id:cutscene-review-edited-hint") {
		t.Fatal("missing 'Your edit joins this review · ⌘Z undoes it' hint after clip edit")
	}
	if got := app.Find("id:cutscene-start-value").Text; got != "8.3 s" {
		t.Fatalf("start value after +0.1s = %q, want '8.3 s'", got)
	}
	undoInUI(app, ed)
	if app.Exists("id:cutscene-review-edited-hint") {
		t.Fatal("edited hint should clear after Undo")
	}

	// Click Before toggle -> Before banner appears; capture screenshot.
	app.Press("id:cutscene-review-before")
	if !app.Exists("id:cutscene-before-banner") {
		t.Fatal("Before banner did not appear when clicking Before")
	}
	app.Screenshot("cutscene-timeline-before")

	// Return to After, scrub via thumbnail strip, revert 1 of 5, and Keep 4 of 5.
	app.Press("id:cutscene-review-after")
	app.Press("id:cutscene-thumb-18") // 18 * 0.5s = 9.0s -> intro.knight.02 active!
	if got := app.Find("id:cutscene-subtitle").Text; got != "The lever has to be down here somewhere." {
		t.Fatalf("subtitle after scrubbing to 9.0s = %q, want intro.knight.02", got)
	}

	app.Press("id:cutscene-change-toggle-2")
	if got := app.Find("id:cutscene-review-keep-text").Text; got != "Keep 4 of 5" {
		t.Fatalf("keep button after reverting 1 = %q, want 'Keep 4 of 5'", got)
	}
	app.Press("id:cutscene-review-keep")
	if ed.CutsceneReviewOpen() {
		t.Fatal("review should close after pressing Keep 4 of 5")
	}
}

func TestCutsceneNilReceiverGuards(t *testing.T) {
	var ed *editor.Editor
	if err := ed.OpenCutsceneYAML([]byte(floor1IntroBeforeYAML)); err == nil {
		t.Fatal("nil OpenCutsceneYAML should return error")
	}
	if err := ed.OpenCutsceneFile("cutscenes/x.cutscene.yaml"); err == nil {
		t.Fatal("nil OpenCutsceneFile should return error")
	}
	if err := ed.OpenCutsceneProjectFile("cutscenes/x.cutscene.yaml"); !errors.Is(err, editor.ErrNoProject) {
		t.Fatalf("nil OpenCutsceneProjectFile = %v, want ErrNoProject", err)
	}
	if err := ed.CloseCutscene(); err == nil {
		t.Fatal("nil CloseCutscene should return error")
	}
	if err := ed.SaveCutsceneFile("cutscenes/x.cutscene.yaml"); err == nil {
		t.Fatal("nil SaveCutsceneFile should return error")
	}
	if err := ed.PlayCutscene(); err == nil {
		t.Fatal("nil PlayCutscene should return error")
	}
	ed.PauseCutscene()
	if err := ed.SetCutsceneClipTiming(0, 0, 0, time.Second); err == nil {
		t.Fatal("nil SetCutsceneClipTiming should return error")
	}
	if err := ed.SetCutsceneClipEasing(0, 0, cutscene.EaseLinear); err == nil {
		t.Fatal("nil SetCutsceneClipEasing should return error")
	}
	if err := ed.AdjustCutsceneClipStart(100 * time.Millisecond); err == nil {
		t.Fatal("nil AdjustCutsceneClipStart should return error")
	}
	if err := ed.AdjustCutsceneClipDuration(100 * time.Millisecond); err == nil {
		t.Fatal("nil AdjustCutsceneClipDuration should return error")
	}
	if err := ed.OpenCutsceneReview(editor.CutsceneReviewRequest{}); err == nil {
		t.Fatal("nil OpenCutsceneReview should return error")
	}
	if err := ed.ToggleCutsceneReviewChange(0); err == nil {
		t.Fatal("nil ToggleCutsceneReviewChange should return error")
	}
	if err := ed.RevertAllCutsceneReview(); err == nil {
		t.Fatal("nil RevertAllCutsceneReview should return error")
	}
	if err := ed.KeepCutsceneReview(); err == nil {
		t.Fatal("nil KeepCutsceneReview should return error")
	}
}

func TestCutsceneCrossFileUndoAndNewSubdirKeep(t *testing.T) {
	dir, ed := writeDungeonCutsceneProject(t)
	cutA := filepath.Join(dir, "cutscenes", "floor1_intro.cutscene.yaml")
	cutB := filepath.Join(dir, "cutscenes", "boss", "new_boss.cutscene.yaml")

	if err := ed.OpenCutsceneProjectFile("cutscenes/floor1_intro.cutscene.yaml"); err != nil {
		t.Fatal(err)
	}
	// Edit cutscene A (pushes edit cutsceneCmd).
	if !ed.SelectCutsceneClipByID("dl1") {
		t.Fatal("SelectCutsceneClipByID(dl1) failed")
	}
	if err := ed.AdjustCutsceneClipStart(100 * time.Millisecond); err != nil {
		t.Fatalf("AdjustCutsceneClipStart on A: %v", err)
	}

	// Open a review on a brand-new cutscene B in a new subdirectory while A is open.
	// Base must NOT fall back to A!
	proposedB, err := cutscene.Parse([]byte(`version: 2
kind: timeline
scene: scenes/dungeon.scene.yaml
length: 4s
tracks:
  - id: cam
    kind: camera
    clips:
      - id: bcam1
        start: 0s
        duration: 4s
        action: hold
        target: door
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenCutsceneReview(editor.CutsceneReviewRequest{
		Prompt:   "create boss intro",
		Path:     cutB,
		Proposed: &proposedB,
	}); err != nil {
		t.Fatalf("OpenCutsceneReview on new B: %v", err)
	}
	// Must report only the 1 added camera track, NOT removals of A's tracks!
	changes := ed.CutsceneReviewChanges()
	if len(changes) != 1 || changes[0].Kind != editor.CutsceneChangeAdded {
		t.Fatalf("new cutscene review diffed against wrong file: changes = %+v", changes)
	}
	// CutsceneDirty & HasUnsavedChanges must be true while an uncommitted review is open.
	if !ed.CutsceneDirty() || !ed.HasUnsavedChanges() {
		t.Fatalf("CutsceneDirty=%v HasUnsavedChanges=%v while review open, want true", ed.CutsceneDirty(), ed.HasUnsavedChanges())
	}

	// Revert all on B must restore A (including A's path and edited doc), so Undo does not corrupt B.
	if err := ed.RevertAllCutsceneReview(); err != nil {
		t.Fatalf("RevertAllCutsceneReview: %v", err)
	}
	if ed.CutscenePath() != cutA {
		t.Fatalf("CutscenePath after RevertAll = %q, want %q", ed.CutscenePath(), cutA)
	}
	if !ed.Undo() {
		t.Fatal("Undo of A's edit failed")
	}
	if ed.CutscenePath() != cutA {
		t.Fatalf("CutscenePath after Undo = %q, want %q", ed.CutscenePath(), cutA)
	}

	// Now open review on B again and Keep -> must create parent dir cutscenes/boss/ and write B.
	if err := ed.OpenCutsceneReview(editor.CutsceneReviewRequest{
		Prompt:   "create boss intro",
		Path:     cutB,
		Proposed: &proposedB,
	}); err != nil {
		t.Fatalf("OpenCutsceneReview on new B (2nd): %v", err)
	}
	if err := ed.KeepCutsceneReview(); err != nil {
		t.Fatalf("KeepCutsceneReview in new subdir: %v", err)
	}
	if _, err := os.Stat(cutB); err != nil {
		t.Fatalf("expected %s to exist after Keep: %v", cutB, err)
	}
	if ed.CutscenePath() != cutB {
		t.Fatalf("CutscenePath after Keep B = %q, want %q", ed.CutscenePath(), cutB)
	}

	// Single Undo must delete newly created B on disk AND restore A in memory!
	if !ed.Undo() {
		t.Fatal("Undo of Keep B failed")
	}
	if _, err := os.Stat(cutB); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %s to be deleted after Undo, got err=%v", cutB, err)
	}
	if ed.CutscenePath() != cutA {
		t.Fatalf("CutscenePath after Undo Keep B = %q, want %q", ed.CutscenePath(), cutA)
	}
	if got := ed.Cutscene(); got == nil || len(got.Tracks) != 5 {
		t.Fatalf("expected A (5 tracks) restored after Undo Keep B, got %+v", got)
	}

	// In-memory review (no path) stays dirty after Keep because nothing was written to disk.
	edMem, err := editor.New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := edMem.OpenCutsceneYAML([]byte(floor1IntroBeforeYAML)); err != nil {
		t.Fatal(err)
	}
	afterDoc, err := cutscene.Parse([]byte(floor1IntroAfterYAML))
	if err != nil {
		t.Fatal(err)
	}
	if err := edMem.OpenCutsceneReview(editor.CutsceneReviewRequest{Proposed: &afterDoc}); err != nil {
		t.Fatal(err)
	}
	if err := edMem.KeepCutsceneReview(); err != nil {
		t.Fatal(err)
	}
	if !edMem.CutsceneDirty() {
		t.Fatal("in-memory cutscene should remain CutsceneDirty() == true after Keep with no file path")
	}
}

func TestCutsceneProjectPathTraversalAndSymlinkEscape(t *testing.T) {
	dir, ed := writeDungeonCutsceneProject(t)
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.cutscene.yaml")
	if err := os.WriteFile(outsideFile, []byte(floor1IntroBeforeYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ed.OpenCutsceneProjectFile("../outside.cutscene.yaml"); err == nil {
		t.Fatal("OpenCutsceneProjectFile must reject .. path traversal")
	}

	escapeLink := filepath.Join(dir, "cutscenes", "escape.cutscene.yaml")
	if err := os.Symlink(outsideFile, escapeLink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := ed.OpenCutsceneProjectFile("cutscenes/escape.cutscene.yaml"); err == nil {
		t.Fatal("OpenCutsceneProjectFile must reject symlink escaping project root")
	}
	if err := ed.OpenCutsceneProjectFile("cutscenes/floor1_intro.cutscene.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.SaveCutsceneFile(escapeLink); err == nil {
		t.Fatal("SaveCutsceneFile must reject symlink escaping project root")
	}
}

func TestCutsceneBeforeViewClipEditResolvesByIdentityAndSoundSort(t *testing.T) {
	_, ed := writeDungeonCutsceneProject(t)
	if err := ed.OpenCutsceneProjectFile("cutscenes/floor1_intro.cutscene.yaml"); err != nil {
		t.Fatal(err)
	}
	// Sound track clip reordering in non-review mode should succeed and sort clips by Start.
	if err := ed.OpenCutsceneYAML([]byte(`version: 2
kind: timeline
scene: scenes/dungeon.scene.yaml
length: 6s
tracks:
  - id: sfx
    kind: sound
    clips:
      - id: s1
        start: 1s
        duration: 500ms
        source: audio/a.wav
      - id: s2
        start: 3s
        duration: 500ms
        source: audio/b.wav
`)); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetCutsceneClipTiming(0, 1, 500*time.Millisecond, 500*time.Millisecond); err != nil {
		t.Fatalf("reordering sound clip earlier failed: %v", err)
	}
	if got := ed.Cutscene(); got.Tracks[0].Clips[0].ID != "s2" {
		t.Fatalf("expected s2 sorted first, got %+v", got.Tracks[0].Clips)
	}

	// Re-open floor1_intro and start review (After inserts slime_01 track before dialogue/sound/music!).
	if err := ed.OpenCutsceneProjectFile("cutscenes/floor1_intro.cutscene.yaml"); err != nil {
		t.Fatal(err)
	}
	proposed, err := cutscene.Parse([]byte(floor1IntroAfterYAML))
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenCutsceneReview(editor.CutsceneReviewRequest{Proposed: &proposed}); err != nil {
		t.Fatal(err)
	}
	// Switch to Before view: in Before, track 2 is dialogue (dl1), whereas in After track 2 is slime_01!
	ed.SetCutsceneReviewViewMode(editor.CutsceneReviewBefore)
	// Invalid duration must fail WITHOUT switching view away from Before.
	if err := ed.SetCutsceneClipTiming(2, 0, 0, -time.Second); err == nil {
		t.Fatal("expected error for negative duration")
	}
	if !ed.CutsceneReviewShowingBefore() {
		t.Fatal("failed SetCutsceneClipTiming must not mutate review view away from Before")
	}
	// Valid edit on Before's track 2 clip 0 (dialogue dl1) must edit dl1 in After (track 3), NOT slime_01 (track 2)!
	if err := ed.SetCutsceneClipTiming(2, 0, 2900*time.Millisecond, 2600*time.Millisecond); err != nil {
		t.Fatalf("Before-view SetCutsceneClipTiming on dl1: %v", err)
	}
	eff := ed.Cutscene()
	if eff.Tracks[2].Kind != cutscene.TrackActor || eff.Tracks[2].Target != "slime_01" {
		t.Fatalf("unexpected track 2: %+v", eff.Tracks[2])
	}
	if time.Duration(eff.Tracks[2].Clips[0].Start) == 2900*time.Millisecond {
		t.Fatal("Before-view edit mutated wrong track (slime_01 instead of dialogue dl1)")
	}
	var dl1Start time.Duration
	for _, tr := range eff.Tracks {
		for _, cl := range tr.Clips {
			if cl.ID == "dl1" {
				dl1Start = time.Duration(cl.Start)
			}
		}
	}
	if dl1Start != 2900*time.Millisecond {
		t.Fatalf("dl1 start = %v, want 2.9s", dl1Start)
	}
}

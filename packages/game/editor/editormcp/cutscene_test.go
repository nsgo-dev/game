package editormcp_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor/editormcp"
)

const mcpCutsceneBefore = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/cutscene.v2.json
version: 2
kind: timeline
scene: scenes/room.scene.yaml
length: 6s
tracks:
  - id: camera
    name: Camera
    kind: camera
    clips:
      - id: cam1
        label: Hold on hero
        start: 0s
        duration: 6s
        action: hold
        target: hero
  - id: hero
    name: Hero
    kind: actor
    target: hero
    clips:
      - id: h1
        label: Walk
        start: 0s
        duration: 4s
        action: walk
        fromX: 40
        toX: 200
`

const mcpCutsceneAfter = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/cutscene.v2.json
version: 2
kind: timeline
scene: scenes/room.scene.yaml
length: 6s
tracks:
  - id: camera
    name: Camera
    kind: camera
    clips:
      - id: cam1
        label: Hold on hero
        start: 0s
        duration: 2s
        action: hold
        target: hero
      - id: cam2
        label: Follow hero
        start: 2s
        duration: 4s
        action: follow
        target: hero
  - id: hero
    name: Hero
    kind: actor
    target: hero
    clips:
      - id: h1
        label: Walk
        start: 0s
        duration: 4s
        action: walk
        fromX: 40
        toX: 200
  - id: dialogue
    name: Dialogue
    kind: dialogue
    clips:
      - id: dl1
        start: 1s
        duration: 3s
        line: intro.hero.01
`

func TestCutsceneWriteReviewAndScrub(t *testing.T) {
	f := openFixture(t)
	// Seed localization and an existing cutscene on disk.
	locDir := filepath.Join(f.dir, "loc")
	if err := os.MkdirAll(locDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locDir, "en.yaml"), []byte("version: 1\nlocale: en\nstrings:\n  intro.hero.01: \"Let's go.\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cutDir := filepath.Join(f.dir, "cutscenes")
	if err := os.MkdirAll(cutDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cutPath := filepath.Join(cutDir, "intro.cutscene.yaml")
	if err := os.WriteFile(cutPath, []byte(mcpCutsceneBefore), 0o644); err != nil {
		t.Fatal(err)
	}

	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	// Invalid YAML is refused without modifying disk or editor.
	badRes := call(t, cs, editormcp.ToolCutsceneWrite, map[string]any{
		"file": "cutscenes/intro.cutscene.yaml",
		"yaml": "version: 2\nkind: timeline\nunknownKey: true\n",
	})
	if !badRes.IsError {
		t.Fatal("expected invalid cutscene YAML to fail")
	}
	if raw, _ := os.ReadFile(cutPath); !bytes.Equal(raw, []byte(mcpCutsceneBefore)) {
		t.Fatal("invalid cutscene_write modified file on disk")
	}

	// Propose updated cutscene -> opens review without overwriting disk yet.
	res := call(t, cs, editormcp.ToolCutsceneWrite, map[string]any{
		"file":   "cutscenes/intro.cutscene.yaml",
		"yaml":   mcpCutsceneAfter,
		"prompt": "follow hero and add a line",
	})
	out := mustOK(t, res)
	if !strings.Contains(out, "Opened cutscene review") {
		t.Fatalf("cutscene_write output = %q", out)
	}
	var writeRes editormcp.CutsceneWriteResult
	structured(t, res, &writeRes)
	if writeRes.Status != "pending_review" || len(writeRes.Changes) != 2 {
		t.Fatalf("writeRes = %+v", writeRes)
	}
	if !f.ed.CutsceneReviewOpen() {
		t.Fatal("editor did not open cutscene review")
	}
	if raw, _ := os.ReadFile(cutPath); !bytes.Equal(raw, []byte(mcpCutsceneBefore)) {
		t.Fatal("pending review overwrote file on disk before Keep")
	}

	// Scrub to t = 2.0 s in After -> hero is at x = 120, dialogue active.
	scrubRes := call(t, cs, editormcp.ToolCutsceneScrub, map[string]any{
		"timeSeconds": 2.0,
		"view":        "after",
	})
	mustOK(t, scrubRes)
	var scrub editormcp.CutsceneScrubResult
	structured(t, scrubRes, &scrub)
	if !scrub.ReviewOpen || scrub.ShowingBefore || scrub.TotalChanges != 2 {
		t.Fatalf("scrub review state = %+v", scrub)
	}
	if !scrub.Dialogue.Active || scrub.Dialogue.ID != "intro.hero.01" || scrub.Dialogue.Text != "Let's go." {
		t.Fatalf("scrub dialogue = %+v", scrub.Dialogue)
	}
	if len(scrub.Actors) != 1 || scrub.Actors[0].ID != "hero" || scrub.Actors[0].X != 120 {
		t.Fatalf("scrub actors = %+v, want hero at x=120", scrub.Actors)
	}

	// Scrub to Before -> dialogue is NOT active.
	beforeRes := call(t, cs, editormcp.ToolCutsceneScrub, map[string]any{
		"timeSeconds": 2.0,
		"view":        "before",
	})
	mustOK(t, beforeRes)
	var beforeScrub editormcp.CutsceneScrubResult
	structured(t, beforeRes, &beforeScrub)
	if !beforeScrub.ShowingBefore || beforeScrub.Dialogue.Active {
		t.Fatalf("beforeScrub = %+v", beforeScrub)
	}

	// Keep the review -> writes file to disk in one undo step.
	if err := f.ed.KeepCutsceneReview(); err != nil {
		t.Fatalf("KeepCutsceneReview: %v", err)
	}
	keptDisk, err := os.ReadFile(cutPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(keptDisk), "intro.hero.01") {
		t.Fatalf("kept disk missing dialogue line:\n%s", keptDisk)
	}

	// Direct write with review=false writes canonical YAML immediately.
	directRes := call(t, cs, editormcp.ToolCutsceneWrite, map[string]any{
		"file":   "cutscenes/direct.cutscene.yaml",
		"yaml":   mcpCutsceneBefore,
		"review": false,
	})
	mustOK(t, directRes)
	var direct editormcp.CutsceneWriteResult
	structured(t, directRes, &direct)
	if direct.Status != "written" || !direct.Opened {
		t.Fatalf("direct write = %+v", direct)
	}
	if _, err := os.Stat(filepath.Join(cutDir, "direct.cutscene.yaml")); err != nil {
		t.Fatalf("direct cutscene not written: %v", err)
	}

	// Huge finite timeSeconds must clamp to Length instead of overflowing negative to 0s.
	hugeRes := call(t, cs, editormcp.ToolCutsceneScrub, map[string]any{
		"timeSeconds": 1e12,
	})
	mustOK(t, hugeRes)
	var hugeScrub editormcp.CutsceneScrubResult
	structured(t, hugeRes, &hugeScrub)
	if hugeScrub.TimeSeconds != hugeScrub.LengthSeconds || hugeScrub.TimeSeconds <= 0 {
		t.Fatalf("huge timeSeconds = %v, want clamped to Length %v", hugeScrub.TimeSeconds, hugeScrub.LengthSeconds)
	}

	// Explicit review=true for a video cutscene must return an error without touching disk.
	vidPath := filepath.Join(cutDir, "trailer.cutscene.yaml")
	vidRes := call(t, cs, editormcp.ToolCutsceneWrite, map[string]any{
		"file":   "cutscenes/trailer.cutscene.yaml",
		"yaml":   "version: 2\nkind: video\nsource: media/trailer.mp4\n",
		"review": true,
	})
	if !vidRes.IsError {
		t.Fatalf("expected error for review=true on video cutscene, got %+v", vidRes)
	}
	if _, err := os.Stat(vidPath); !os.IsNotExist(err) {
		t.Fatalf("video cutscene should not be written when review=true is rejected, err=%v", err)
	}

	// Proposing a brand-new cutscene while direct.cutscene.yaml is open must diff against an empty baseline, not direct.cutscene.yaml.
	brandNewRes := call(t, cs, editormcp.ToolCutsceneWrite, map[string]any{
		"file":   "cutscenes/brand_new.cutscene.yaml",
		"yaml":   "version: 2\nkind: timeline\nscene: scenes/main.scene.yaml\nlength: 2s\ntracks:\n  - id: cam\n    kind: camera\n    clips:\n      - id: c1\n        start: 0s\n        duration: 2s\n        action: hold\n        target: hero\n",
		"prompt": "new intro",
		"review": true,
	})
	mustOK(t, brandNewRes)
	var brandNew editormcp.CutsceneWriteResult
	structured(t, brandNewRes, &brandNew)
	if brandNew.Status != "pending_review" || len(brandNew.Changes) != 1 || brandNew.Changes[0].Sign != "+" {
		t.Fatalf("brand-new cutscene review diffed against wrong file: %+v", brandNew)
	}

	// Symlink escaping project root must be refused by cutscene_write.
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.cutscene.yaml")
	if err := os.WriteFile(outsideFile, []byte(mcpCutsceneBefore), 0o644); err != nil {
		t.Fatal(err)
	}
	escapeLink := filepath.Join(cutDir, "escape.cutscene.yaml")
	if err := os.Symlink(outsideFile, escapeLink); err == nil {
		escRes := call(t, cs, editormcp.ToolCutsceneWrite, map[string]any{
			"file":   "cutscenes/escape.cutscene.yaml",
			"yaml":   mcpCutsceneAfter,
			"review": false,
		})
		if !escRes.IsError {
			t.Fatalf("expected symlink escape error, got %+v", escRes)
		}
	}
}

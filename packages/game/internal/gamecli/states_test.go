package gamecli

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const knightStatesFixtureYAML = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/states.v1.json
version: 1
name: Knight
entry: idle
parameters:
  - name: move.x
    type: float
  - name: velocity.y
    type: float
  - name: on_floor
    type: bool
    default: true
  - name: jump
    type: trigger
  - name: attack
    type: trigger
  - name: hurt
    type: trigger
states:
  - name: idle
    clip: anim/knight_idle.animation.yaml
    x: 40
    "y": 230
  - name: run
    clip: anim/knight_run.animation.yaml
    x: 330
    "y": 230
  - name: jump
    clip: anim/knight_jump.animation.yaml
    x: 190
    "y": 70
  - name: fall
    clip: anim/knight_fall.animation.yaml
    x: 430
    "y": 70
  - name: attack
    clip: anim/knight_attack.animation.yaml
    x: 190
    "y": 400
  - name: hurt
    clip: anim/knight_hurt.animation.yaml
    x: 430
    "y": 400
transitions:
  - from: idle
    to: run
    label: moving
    when: '|move.x| > 0.1'
  - from: run
    to: idle
    label: stopped
    when: '|move.x| <= 0.1'
  - from: idle
    to: jump
    label: jump pressed, on floor
    when: jump && on_floor
  - from: run
    to: jump
    label: jump pressed, on floor
    when: jump && on_floor
  - from: jump
    to: fall
    label: falling
    when: velocity.y > 0
  - from: fall
    to: run
    label: lands
    when: on_floor
  - from: idle
    to: attack
    label: attack pressed
    when: attack
  - from: run
    to: attack
    label: attack pressed
    when: attack
  - from: attack
    to: idle
    label: clip ends
    onEnd: true
  - from: '*'
    to: hurt
    label: took damage
    when: hurt
  - from: hurt
    to: idle
    label: clip ends
    onEnd: true
`

// TestStatesGraphScreenshot opens the knight sprite state machine in the hosted editor window,
// turns on Drive with keys, selects run, and captures a real platform screenshot under `nsgo test -target macos`.
func TestStatesGraphScreenshot(t *testing.T) {
	names := []string{
		"idle_0", "idle_1", "idle_2", "idle_3",
		"run_0", "run_1", "run_2", "run_3", "run_4", "run_5",
		"jump_0", "jump_1",
		"fall_0", "fall_1",
		"atk_0", "atk_1", "atk_2", "atk_3",
		"hurt_0", "hurt_1",
	}
	img := image.NewRGBA(image.Rect(0, 0, len(names)*16, 16))
	palette := []color.RGBA{
		{R: 0xD7, G: 0xDE, B: 0xEA, A: 0xFF},
		{R: 0x3B, G: 0x7B, B: 0xFF, A: 0xFF},
		{R: 0x4F, G: 0xD3, B: 0x8A, A: 0xFF},
		{R: 0xE9, G: 0xA8, B: 0x4A, A: 0xFF},
		{R: 0xF2, G: 0xC1, B: 0x4E, A: 0xFF},
		{R: 0xFF, G: 0x6B, B: 0x6B, A: 0xFF},
	}
	frames := make(map[string]sprite.Frame, len(names))
	for i, name := range names {
		col := palette[0]
		switch {
		case strings.HasPrefix(name, "run_"):
			col = palette[1]
		case strings.HasPrefix(name, "jump_"):
			col = palette[2]
		case strings.HasPrefix(name, "fall_"):
			col = palette[3]
		case strings.HasPrefix(name, "atk_"):
			col = palette[4]
		case strings.HasPrefix(name, "hurt_"):
			col = palette[5]
		}
		for y := 2; y < 14; y++ {
			for x := i*16 + 3; x < i*16+13; x++ {
				img.SetRGBA(x, y, col)
			}
		}
		frames[name] = sprite.Frame{Name: name, X: float32(i * 16), Y: 0, W: 16, H: 16}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	atlas := &sprite.Atlas{
		Image:  core.ImageSource{Data: buf.Bytes(), Size: core.Size{Width: core.Points(len(names) * 16), Height: 16}},
		Frames: frames,
	}
	mk := func(name string, f []string, ms int, loop bool) *resource.Animation {
		c, err := animclip.FromFrames(name, f, 1000.0/float64(ms), loop)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	clips := animclip.ClipMap{
		"anim/knight_idle.animation.yaml":   mk("idle", []string{"idle_0", "idle_1", "idle_2", "idle_3"}, 150, true),
		"anim/knight_run.animation.yaml":    mk("run", []string{"run_0", "run_1", "run_2", "run_3", "run_4", "run_5"}, 100, true),
		"anim/knight_jump.animation.yaml":   mk("jump", []string{"jump_0", "jump_1"}, 120, false),
		"anim/knight_fall.animation.yaml":   mk("fall", []string{"fall_0", "fall_1"}, 120, true),
		"anim/knight_attack.animation.yaml": mk("attack", []string{"atk_0", "atk_1", "atk_2", "atk_3"}, 80, false),
		"anim/knight_hurt.animation.yaml":   mk("hurt", []string{"hurt_0", "hurt_1"}, 90, false),
	}
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Dungeon Level 01",
		VirtualWidth:    320,
		VirtualHeight:   180,
		BackgroundColor: "#15181d",
		Layers: []sceneformat.LayerDoc{{
			Name:  "actors",
			Space: sceneformat.SpaceWorld,
			Order: 0,
			Objects: []sceneformat.ObjectDoc{{
				ID:     "knight",
				Name:   "Knight",
				X:      120,
				Y:      90,
				Sprite: &sceneformat.SpriteRef{Atlas: "knight", Frame: "idle_0"},
				Components: map[string]any{
					sceneformat.AnimatorComponentName: map[string]any{
						"states":    "anim/knight.states.yaml",
						"startClip": "idle",
					},
				},
			}},
		}},
	}
	ed, err := editor.New(doc, sceneformat.AtlasMap{"knight": atlas})
	if err != nil {
		t.Fatal(err)
	}
	ed.Clips = clips
	ed.SelectObject("knight")
	if err := ed.OpenStatesYAML([]byte(knightStatesFixtureYAML), "knight"); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	app.Press("id:states-node-run")
	app.Press("id:states-drive-toggle")
	app.Settle()

	if got := app.Find("id:states-leaving-header").Text; got != "LEAVING RUN" {
		t.Fatalf("LEAVING header = %q, want LEAVING RUN", got)
	}
	app.Screenshot("states/graph-editor")
}

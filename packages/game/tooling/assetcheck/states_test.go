package assetcheck_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
)

const validClipYAML = `type: animation
version: 1
name: idle
length: 0.4
loop: true
tracks:
  - property: sprite.frame
    frames:
      - time: 0
        frame: hero
`

func TestCheckSpriteStateMachines(t *testing.T) {
	t.Run("valid states file and animator component pass", func(t *testing.T) {
		fsys := fstest.MapFS{
			"atlases/actors.json":      &fstest.MapFile{Data: []byte(actorsAtlas)},
			"atlases/actors.png":       &fstest.MapFile{Data: pngBytes(t, 16, 16)},
			"anim/idle.animation.yaml": &fstest.MapFile{Data: []byte(validClipYAML)},
			"anim/run.animation.yaml":  &fstest.MapFile{Data: []byte(validClipYAML)},
			"anim/knight.states.yaml": &fstest.MapFile{Data: []byte(`version: 1
entry: idle
parameters:
  - name: move.x
    type: float
states:
  - name: idle
    clip: anim/idle.animation.yaml
  - name: run
    clip: anim/run.animation.yaml
transitions:
  - from: idle
    to: run
    when: '|move.x| > 0.1'
  - from: run
    to: idle
    when: '|move.x| <= 0.1'
`)},
			"scenes/room.scene.yaml": &fstest.MapFile{Data: []byte(`version: 1
name: Room
layers:
  - name: actors
    objects:
      - id: knight
        sprite:
          atlas: actors
          frame: hero
        components:
          animator:
            states: anim/knight.states.yaml
            startClip: idle
`)},
		}
		report, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if !report.OK() {
			t.Fatalf("unexpected problems: %v", report.Problems)
		}
		if report.States != 1 {
			t.Fatalf("report.States = %d, want 1", report.States)
		}
	})

	// #1054 acceptance: a transition to a missing state is reported by game check with file and line.
	t.Run("transition to missing state reports file and line", func(t *testing.T) {
		statesYAML := strings.Join([]string{
			"version: 1",
			"entry: idle",
			"parameters:",
			"  - name: move.x",
			"    type: float",
			"states:",
			"  - name: idle",
			"    clip: anim/idle.animation.yaml",
			"transitions:",
			"  - from: idle",
			"    to: missing_run",
			"    when: '|move.x| > 0.1'",
			"",
		}, "\n")
		fsys := fstest.MapFS{
			"anim/idle.animation.yaml": &fstest.MapFile{Data: []byte(validClipYAML)},
			"anim/knight.states.yaml":  &fstest.MapFile{Data: []byte(statesYAML)},
		}
		report, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		var found bool
		for _, p := range report.Problems {
			if p.File == "anim/knight.states.yaml" && p.Line == 11 && strings.Contains(p.Message, "missing_run") {
				found = true
			}
		}
		if !found {
			t.Fatalf("problems = %v, want anim/knight.states.yaml:11 mentioning missing_run", report.Problems)
		}
	})

	t.Run("unknown clip and unread parameter report file and line", func(t *testing.T) {
		statesYAML := strings.Join([]string{
			"version: 1",
			"entry: idle",
			"parameters:",
			"  - name: move.x",
			"    type: float",
			"  - name: never_used",
			"    type: bool",
			"states:",
			"  - name: idle",
			"    clip: anim/idle.animation.yaml",
			"  - name: run",
			"    clip: anim/no_such_clip.animation.yaml",
			"transitions:",
			"  - from: idle",
			"    to: run",
			"    when: '|move.x| > 0.1'",
			"",
		}, "\n")
		fsys := fstest.MapFS{
			"anim/idle.animation.yaml": &fstest.MapFile{Data: []byte(validClipYAML)},
			"anim/knight.states.yaml":  &fstest.MapFile{Data: []byte(statesYAML)},
		}
		report, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		var badClip, badParam bool
		for _, p := range report.Problems {
			if p.File == "anim/knight.states.yaml" && p.Line == 12 && strings.Contains(p.Message, "no_such_clip") {
				badClip = true
			}
			if p.File == "anim/knight.states.yaml" && p.Line == 6 && strings.Contains(p.Message, "never_used") {
				badParam = true
			}
		}
		if !badClip || !badParam {
			t.Fatalf("problems = %v, want clip problem at line 12 (%t) and unused param at line 6 (%t)", report.Problems, badClip, badParam)
		}
	})

	t.Run("unknown animator startClip reports file and line", func(t *testing.T) {
		sceneYAML := strings.Join([]string{
			"version: 1",
			"name: Room",
			"layers:",
			"  - name: actors",
			"    objects:",
			"      - id: knight",
			"        components:",
			"          animator:",
			"            states: anim/knight.states.yaml",
			"            startClip: bogus",
			"",
		}, "\n")
		fsys := fstest.MapFS{
			"anim/idle.animation.yaml": &fstest.MapFile{Data: []byte(validClipYAML)},
			"anim/knight.states.yaml": &fstest.MapFile{Data: []byte(`version: 1
entry: idle
states:
  - name: idle
    clip: anim/idle.animation.yaml
`)},
			"scenes/room.scene.yaml": &fstest.MapFile{Data: []byte(sceneYAML)},
		}
		report, err := assetcheck.Check(fsys)
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		var found bool
		for _, p := range report.Problems {
			if p.File == "scenes/room.scene.yaml" && p.Line == 10 && strings.Contains(p.Message, "bogus") {
				found = true
			}
		}
		if !found {
			t.Fatalf("problems = %v, want scenes/room.scene.yaml:10 mentioning bogus", report.Problems)
		}
	})
}

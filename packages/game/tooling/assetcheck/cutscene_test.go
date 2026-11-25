package assetcheck_test

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
)

const testCutsceneScene = `version: 2
layers:
  - name: world
    objects:
      - id: door
      - id: knight
      - id: slime_01
`

const testCutsceneL10nEn = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/l10n.v1.json
version: 1
locale: en
strings:
  intro.knight.01: Another floor. Another locked door.
  intro.knight.02: The lever has to be down here somewhere.
`

const testCutsceneValid = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/cutscene.v2.json
version: 2
kind: timeline
scene: scenes/level_01.scene.yaml
length: 12s
tracks:
  - kind: camera
    clips:
      - start: 0s
        duration: 1.6s
        action: hold
        target: door
  - kind: actor
    target: knight
    clips:
      - start: 1s
        duration: 2.4s
        action: walk
        fromX: 60
        toX: 220
  - kind: dialogue
    clips:
      - start: 3.6s
        duration: 2.4s
        line: intro.knight.01
  - kind: sound
    clips:
      - start: 200ms
        duration: 600ms
        source: audio/door_slam.wav
  - kind: music
    clips:
      - start: 2s
        duration: 10s
        source: audio/cellar_theme.ogg
        fadeIn: 2s
`

func TestCheckCutsceneValid(t *testing.T) {
	fsys := fstest.MapFS{
		"nsgo.yaml":                            {Data: []byte("name: test\n")},
		"scenes/level_01.scene.yaml":           {Data: []byte(testCutsceneScene)},
		"loc/en.yaml":                          {Data: []byte(testCutsceneL10nEn)},
		"audio/door_slam.wav":                  {Data: []byte("wav")},
		"audio/cellar_theme.ogg":               {Data: []byte("ogg")},
		"cutscenes/floor1_intro.cutscene.yaml": {Data: []byte(testCutsceneValid)},
	}
	rep, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("unexpected problems: %v", rep.Problems)
	}
	if rep.Cutscenes != 1 {
		t.Fatalf("Cutscenes = %d, want 1", rep.Cutscenes)
	}
}

func TestCheckCutsceneReportsMissingObjectsLinesAndAudioWithFileAndLine(t *testing.T) {
	const broken = `version: 2
kind: timeline
scene: scenes/level_01.scene.yaml
length: 12s
tracks:
  - kind: camera
    clips:
      - start: 0s
        duration: 1.6s
        action: hold
        target: missing_door
  - kind: actor
    target: ghost_knight
    clips:
      - start: 1s
        duration: 2s
        action: walk
        toX: 100
  - kind: dialogue
    clips:
      - start: 3s
        duration: 2s
        line: intro.knight.99
  - kind: sound
    clips:
      - start: 200ms
        duration: 500ms
        source: audio/missing_sfx.wav
  - kind: music
    clips:
      - start: 2s
        duration: 5s
        source: audio/missing_bgm.ogg
`
	fsys := fstest.MapFS{
		"nsgo.yaml":                      {Data: []byte("name: test\n")},
		"scenes/level_01.scene.yaml":     {Data: []byte(testCutsceneScene)},
		"loc/en.yaml":                    {Data: []byte(testCutsceneL10nEn)},
		"cutscenes/broken.cutscene.yaml": {Data: []byte(broken)},
	}
	rep, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range rep.Problems {
		got = append(got, p.String())
	}
	want := []string{
		`cutscenes/broken.cutscene.yaml:11: cutscene camera target "missing_door": scene "scenes/level_01.scene.yaml" has no object "missing_door"`,
		`cutscenes/broken.cutscene.yaml:13: cutscene actor target "ghost_knight": scene "scenes/level_01.scene.yaml" has no object "ghost_knight"`,
		`cutscenes/broken.cutscene.yaml:23: dialogue line "intro.knight.99": not found in localization tables`,
		`cutscenes/broken.cutscene.yaml:28: cutscene audio "audio/missing_sfx.wav": not found`,
		`cutscenes/broken.cutscene.yaml:33: cutscene audio "audio/missing_bgm.ogg": not found`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("problems:\n%v\nwant:\n%v", got, want)
	}
}

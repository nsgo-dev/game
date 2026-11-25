package refs_test

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/refs"
)

func TestCutsceneRefsAndMove(t *testing.T) {
	const cut = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/cutscene.v2.json
version: 2
kind: timeline
scene: scenes/level_01.scene.yaml
length: 12s
tracks:
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
	fsys := fstest.MapFS{
		"game.yaml":                            {Data: []byte(gameHead)},
		"scenes/level_01.scene.yaml":           {Data: []byte("version: 2\nuid: lvl0000000001\n")},
		"cutscenes/floor1_intro.cutscene.yaml": {Data: []byte(cut)},
		"audio/door_slam.wav":                  {Data: []byte("wav")},
		"audio/cellar_theme.ogg":               {Data: []byte("ogg")},
	}
	p := scan(t, fsys)
	want := []string{
		`cutscenes/floor1_intro.cutscene.yaml:4 scene "scenes/level_01.scene.yaml" -> scenes/level_01.scene.yaml`,
		`cutscenes/floor1_intro.cutscene.yaml:11 cutscene audio "audio/door_slam.wav" -> audio/door_slam.wav`,
		`cutscenes/floor1_intro.cutscene.yaml:16 cutscene audio "audio/cellar_theme.ogg" -> audio/cellar_theme.ogg`,
	}
	if got := refList(p); !slices.Equal(got, want) {
		t.Fatalf("refs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := checkList(p); len(got) != 0 {
		t.Fatalf("problems: %v", got)
	}

	dir := dirFS(t, fsys)
	move(t, dir, "audio/door_slam.wav", "audio/sfx/door_slam.wav")
	if got := read(t, dir, "cutscenes/floor1_intro.cutscene.yaml"); !strings.Contains(got, "source: audio/sfx/door_slam.wav\n") {
		t.Fatalf("cutscene not rewritten:\n%s", got)
	}
	move(t, dir, "scenes/level_01.scene.yaml", "scenes/floor1/level_01.scene.yaml")
	if got := read(t, dir, "cutscenes/floor1_intro.cutscene.yaml"); !strings.Contains(got, "scene: scenes/floor1/level_01.scene.yaml\n") {
		t.Fatalf("cutscene scene ref not rewritten:\n%s", got)
	}
	clean(t, dir)

	// Missing audio path is reported by Check with file and line.
	delete(fsys, "audio/cellar_theme.ogg")
	probs := checkList(scan(t, fsys))
	if wantProb := []string{`cutscenes/floor1_intro.cutscene.yaml:16: cutscene audio "audio/cellar_theme.ogg": not found`}; !slices.Equal(probs, wantProb) {
		t.Fatalf("problems = %v, want %v", probs, wantProb)
	}
	// A URI video source is ignored by refs.Check.
	fsys["cutscenes/boss.cutscene.yaml"] = &fstest.MapFile{Data: []byte("version: 1\nsource: asset:cutscenes/boss.mp4\n")}
	p2 := scan(t, fsys)
	for _, prob := range p2.Check() {
		if prob.File == "cutscenes/boss.cutscene.yaml" {
			t.Fatalf("unexpected problem for asset: URI video source: %v", prob)
		}
	}
	_ = refs.KindCutscene
}

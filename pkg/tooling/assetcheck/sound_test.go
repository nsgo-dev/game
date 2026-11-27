package assetcheck_test

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
)

func TestCheckSoundConcurrencyGroups(t *testing.T) {
	const baseGame = `version: 1
name: SoundCheck
id: dev.nsgo.soundcheck
appVersion: 0.1.0
window: {width: 640, height: 360}
audio:
  concurrency:
    - name: impacts
      max: 4
      resolve: quietest
`
	fsys := fstest.MapFS{
		"game.yaml": {Data: []byte(baseGame)},
		"sounds/hit.sound.yaml": {Data: []byte(`type: sound
version: 1
bus: sfx
concurrency: impacts
priority: 10
`)},
		"sounds/bad.sound.yaml": {Data: []byte(`type: sound
version: 1
bus: sfx
concurrency: explosions
priority: 5
`)},
	}

	report, err := assetcheck.Check(fsys)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if report.Sounds != 2 {
		t.Fatalf("report.Sounds = %d, want 2", report.Sounds)
	}
	got := make([]string, len(report.Problems))
	for i, p := range report.Problems {
		got[i] = p.String()
	}
	want := []string{
		`sounds/bad.sound.yaml:4: sound concurrency group "explosions" is not declared in game.yaml audio.concurrency`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("problems = %v, want %v", got, want)
	}
}

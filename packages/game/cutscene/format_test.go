package cutscene_test

import (
	"bytes"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/cutscene"
)

const intro = `version: 1
source: asset:cutscenes/intro.mp4
group: cutscenes
skippable: true
skipAfter: 1.5s
fit: cover
transparent: true
`

func TestParseReadsEveryField(t *testing.T) {
	c, err := cutscene.Parse([]byte(intro))
	if err != nil {
		t.Fatal(err)
	}
	want := cutscene.Cutscene{Version: 1, Source: "asset:cutscenes/intro.mp4", Group: "cutscenes", Skippable: true,
		SkipAfter: cutscene.Duration(1500 * time.Millisecond), Fit: cutscene.FitCover, Transparent: true}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("parsed %+v, want %+v", c, want)
	}
}

func TestParseReadsTimelineFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/floor1_intro.cutscene.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c, err := cutscene.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != cutscene.CurrentVersion || c.EffectiveKind() != cutscene.KindTimeline {
		t.Fatalf("version %d kind %q", c.Version, c.EffectiveKind())
	}
	if c.Scene != "scenes/level_01.scene.yaml" || time.Duration(c.Length) != 12*time.Second {
		t.Fatalf("scene %q length %v", c.Scene, time.Duration(c.Length))
	}
	if len(c.Tracks) != 6 {
		t.Fatalf("tracks = %d, want 6", len(c.Tracks))
	}
	out, err := cutscene.Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, data) {
		t.Fatalf("round-trip mismatch:\n%s\nwant:\n%s", out, data)
	}
}

func TestEncodeRoundTripsUnchanged(t *testing.T) {
	for _, text := range []string{
		intro,
		"version: 1\nsource: asset:a.mp4\n",
		"version: 1\nsource: https://example.com/a b.webm\nskipAfter: 250ms\nfit: stretch\n",
		"version: 1\nsource: 'asset:yes'\nfit: contain\n",
		"version: 1\nsource: asset:long.mp4\nskippable: true\nskipAfter: 1h2m3.5s\n",
		"version: 2\nkind: video\nsource: asset:boss.mp4\nskippable: true\n",
	} {
		c, err := cutscene.Parse([]byte(text))
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		out, err := cutscene.Encode(c)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		back, err := cutscene.Parse(out)
		if err != nil {
			t.Fatalf("%q encoded as %q: %v", text, out, err)
		}
		if !reflect.DeepEqual(back, c) {
			t.Fatalf("%q: read back %+v, want %+v", out, back, c)
		}
		again, err := cutscene.Encode(back)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("encoded %q then %q (%v)", out, again, err)
		}
	}
	// A version 1 file in the canonical form writes back byte for byte without a schema comment.
	c, _ := cutscene.Parse([]byte(intro))
	if out, _ := cutscene.Encode(c); string(out) != intro {
		t.Fatalf("encoded\n%s\nwant\n%s", out, intro)
	}
}

func TestEncodeLeavesOutZeroValues(t *testing.T) {
	out, err := cutscene.Encode(cutscene.Cutscene{Version: cutscene.MinVersion, Source: "asset:a.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "version: 1\nsource: asset:a.mp4\n" {
		t.Fatalf("encoded %q", out)
	}
}

func TestParseRefusesBadFiles(t *testing.T) {
	for name, text := range map[string]string{
		"empty":                  "  \n",
		"unknown key":            "version: 1\nsource: asset:a.mp4\nloop: true\n",
		"no version":             "source: asset:a.mp4\n",
		"future version":         "version: 3\nsource: asset:a.mp4\n",
		"float version":          "version: 1.5\nsource: asset:a.mp4\n",
		"text version":           "version: '1'\nsource: asset:a.mp4\n",
		"no source":              "version: 1\n",
		"blank source":           "version: 1\nsource: '  '\n",
		"unknown fit":            "version: 1\nsource: asset:a.mp4\nfit: zoom\n",
		"negative skip":          "version: 1\nsource: asset:a.mp4\nskipAfter: -1s\n",
		"number skip":            "version: 1\nsource: asset:a.mp4\nskipAfter: 2\n",
		"unitless skip":          "version: 1\nsource: asset:a.mp4\nskipAfter: '2'\n",
		"list skip":              "version: 1\nsource: asset:a.mp4\nskipAfter: [1s]\n",
		"two documents":          "version: 1\nsource: asset:a.mp4\n---\nversion: 1\nsource: asset:b.mp4\n",
		"not a mapping":          "- version: 1\n",
		"bad yaml":               "version: [1\n",
		"v1 with kind":           "version: 1\nkind: video\nsource: asset:a.mp4\n",
		"v1 with timeline":       "version: 1\nlength: 5s\n",
		"v2 bad kind":            "version: 2\nkind: audio\nlength: 5s\n",
		"v2 video with length":   "version: 2\nkind: video\nsource: asset:a.mp4\nlength: 5s\n",
		"v2 timeline no length":  "version: 2\nkind: timeline\n",
		"v2 timeline neg length": "version: 2\nkind: timeline\nlength: -1s\n",
		"v2 timeline with video": "version: 2\nkind: timeline\nlength: 5s\nsource: asset:a.mp4\n",
		"unknown track kind":     "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: lights\n",
		"actor no target":        "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: actor\n",
		"camera with target":     "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: camera\n    target: hero\n",
		"camera bad action":      "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: camera\n    clips:\n      - start: 0s\n        duration: 1s\n        action: walk\n",
		"camera hold no target":  "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: camera\n    clips:\n      - start: 0s\n        duration: 1s\n        action: hold\n",
		"camera follow coords":   "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: camera\n    clips:\n      - start: 0s\n        duration: 1s\n        action: follow\n        target: hero\n        toX: 10\n",
		"actor bad action":       "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: actor\n    target: hero\n    clips:\n      - start: 0s\n        duration: 1s\n        action: pan\n",
		"actor state no state":   "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: actor\n    target: hero\n    clips:\n      - start: 0s\n        duration: 1s\n        action: state\n",
		"actor drop no toY":      "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: actor\n    target: hero\n    clips:\n      - start: 0s\n        duration: 1s\n        action: drop\n        fromY: 0\n",
		"actor patrol bad speed": "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: actor\n    target: hero\n    clips:\n      - start: 0s\n        duration: 1s\n        action: patrol\n        toX: 100\n        speed: 0\n",
		"dialogue no line":       "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: dialogue\n    clips:\n      - start: 0s\n        duration: 1s\n",
		"dialogue inline text":   "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: dialogue\n    clips:\n      - start: 0s\n        duration: 1s\n        line: intro.01\n        text: Hello\n",
		"sound no source":        "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: sound\n    clips:\n      - start: 0s\n        duration: 500ms\n",
		"sound bad volume":       "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: sound\n    clips:\n      - start: 0s\n        duration: 500ms\n        source: audio/hit.wav\n        volume: 1.5\n",
		"music fade too long":    "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: music\n    clips:\n      - start: 0s\n        duration: 2s\n        source: audio/bgm.ogg\n        fadeIn: 3s\n",
		"clip past length":       "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: dialogue\n    clips:\n      - start: 4s\n        duration: 2s\n        line: a.b\n",
		"overlapping clips":      "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: dialogue\n    clips:\n      - start: 0s\n        duration: 2s\n        line: a.1\n      - start: 1s\n        duration: 2s\n        line: a.2\n",
		"bad easing":             "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: actor\n    target: hero\n    clips:\n      - start: 0s\n        duration: 1s\n        easing: bounce\n        action: walk\n        toX: 100\n",
		"duplicate clip id":      "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - kind: dialogue\n    clips:\n      - id: c1\n        start: 0s\n        duration: 1s\n        line: a.1\n      - id: c1\n        start: 1s\n        duration: 1s\n        line: a.2\n",
		"duplicate track id":     "version: 2\nkind: timeline\nlength: 5s\ntracks:\n  - id: t1\n    kind: dialogue\n  - id: t1\n    kind: sound\n",
	} {
		if c, err := cutscene.Parse([]byte(text)); err == nil {
			t.Errorf("%s: parsed %+v, want an error", name, c)
		} else if !strings.HasPrefix(err.Error(), "cutscene: ") {
			t.Errorf("%s: error %q does not name the package", name, err)
		}
	}
}

func TestEncodeRefusesWhatParseWould(t *testing.T) {
	for _, c := range []cutscene.Cutscene{
		{Source: "asset:a.mp4"},
		{Version: 1},
		{Version: 1, Source: "asset:a.mp4", Fit: "zoom"},
		{Version: 1, Source: "asset:a.mp4", SkipAfter: -1},
		{Version: 2, Kind: cutscene.KindTimeline},
	} {
		if out, err := cutscene.Encode(c); err == nil {
			t.Errorf("%+v encoded as %q, want an error", c, out)
		}
	}
}

func TestValidateRejectsDuplicateActorTargetAndFadeOverflow(t *testing.T) {
	dupYAML := `version: 2
kind: timeline
length: 4s
tracks:
  - kind: actor
    target: knight
    clips:
      - start: 0s
        duration: 1s
        action: state
        state: idle
  - kind: actor
    target: knight
    clips:
      - start: 1s
        duration: 1s
        action: state
        state: run
`
	if _, err := cutscene.Parse([]byte(dupYAML)); err == nil {
		t.Fatal("Parse accepted duplicate actor track target")
	}

	huge := cutscene.Duration(time.Duration(math.MaxInt64 - 10))
	overflowCutscene := cutscene.Cutscene{
		Version: cutscene.CurrentVersion,
		Kind:    cutscene.KindTimeline,
		Length:  cutscene.Duration(5 * time.Second),
		Tracks: []cutscene.Track{
			{
				Kind: cutscene.TrackMusic,
				Clips: []cutscene.Clip{
					{
						Start:    0,
						Duration: cutscene.Duration(5 * time.Second),
						Source:   "audio/theme.ogg",
						FadeIn:   huge,
						FadeOut:  huge,
					},
				},
			},
		},
	}
	if err := cutscene.Validate(overflowCutscene); err == nil {
		t.Fatal("Validate accepted overflowing fadeIn + fadeOut")
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(intro))
	f.Add([]byte("version: 1\nsource: asset:a.mp4\n"))
	f.Add([]byte("version: 1\nsource: asset:a.mp4\nskipAfter: 9999999999h\n"))
	f.Add([]byte("version: 1\nsource: &a asset:a.mp4\ngroup: *a\n"))
	f.Add([]byte("version: 1\nsource: !!binary aGVsbG8=\n"))
	f.Add([]byte("{version: 1, source: x, fit: cover}"))
	if data, err := os.ReadFile("testdata/floor1_intro.cutscene.yaml"); err == nil {
		f.Add(data)
	}
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := cutscene.Parse(data)
		if err != nil {
			return
		}
		// Whatever Parse accepts writes out, and reads back as the same cutscene, written the same way.
		out, err := cutscene.Encode(c)
		if err != nil {
			t.Fatalf("parsed %+v from %q but could not encode it: %v", c, data, err)
		}
		back, err := cutscene.Parse(out)
		if err != nil {
			t.Fatalf("encoded %+v as %q, which does not parse: %v", c, out, err)
		}
		if !reflect.DeepEqual(back, c) {
			t.Fatalf("encoded %+v as %q, read back as %+v", c, out, back)
		}
		again, err := cutscene.Encode(back)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("encoded %q then %q (%v)", out, again, err)
		}
	})
}

func BenchmarkParse(b *testing.B) {
	data := []byte(intro)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := cutscene.Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}

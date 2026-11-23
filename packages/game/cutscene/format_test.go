package cutscene_test

import (
	"bytes"
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
	if c != want {
		t.Fatalf("parsed %+v, want %+v", c, want)
	}
}

func TestEncodeRoundTripsUnchanged(t *testing.T) {
	for _, text := range []string{
		intro,
		"version: 1\nsource: asset:a.mp4\n",
		"version: 1\nsource: https://example.com/a b.webm\nskipAfter: 250ms\nfit: stretch\n",
		"version: 1\nsource: 'asset:yes'\nfit: contain\n",
		"version: 1\nsource: asset:long.mp4\nskippable: true\nskipAfter: 1h2m3.5s\n",
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
		if back != c {
			t.Fatalf("%q: read back %+v, want %+v", out, back, c)
		}
		again, err := cutscene.Encode(back)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("encoded %q then %q (%v)", out, again, err)
		}
	}
	// A file in the canonical form writes back byte for byte.
	c, _ := cutscene.Parse([]byte(intro))
	if out, _ := cutscene.Encode(c); string(out) != intro {
		t.Fatalf("encoded\n%s\nwant\n%s", out, intro)
	}
}

func TestEncodeLeavesOutZeroValues(t *testing.T) {
	out, err := cutscene.Encode(cutscene.Cutscene{Version: cutscene.CurrentVersion, Source: "asset:a.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "version: 1\nsource: asset:a.mp4\n" {
		t.Fatalf("encoded %q", out)
	}
}

func TestParseRefusesBadFiles(t *testing.T) {
	for name, text := range map[string]string{
		"empty":          "  \n",
		"unknown key":    "version: 1\nsource: asset:a.mp4\nloop: true\n",
		"no version":     "source: asset:a.mp4\n",
		"future version": "version: 2\nsource: asset:a.mp4\n",
		"no source":      "version: 1\n",
		"blank source":   "version: 1\nsource: '  '\n",
		"unknown fit":    "version: 1\nsource: asset:a.mp4\nfit: zoom\n",
		"negative skip":  "version: 1\nsource: asset:a.mp4\nskipAfter: -1s\n",
		"number skip":    "version: 1\nsource: asset:a.mp4\nskipAfter: 2\n",
		"unitless skip":  "version: 1\nsource: asset:a.mp4\nskipAfter: '2'\n",
		"list skip":      "version: 1\nsource: asset:a.mp4\nskipAfter: [1s]\n",
		"two documents":  "version: 1\nsource: asset:a.mp4\n---\nversion: 1\nsource: asset:b.mp4\n",
		"not a mapping":  "- version: 1\n",
		"bad yaml":       "version: [1\n",
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
	} {
		if out, err := cutscene.Encode(c); err == nil {
			t.Errorf("%+v encoded as %q, want an error", c, out)
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(intro))
	f.Add([]byte("version: 1\nsource: asset:a.mp4\n"))
	f.Add([]byte("version: 1\nsource: asset:a.mp4\nskipAfter: 9999999999h\n"))
	f.Add([]byte("version: 1\nsource: &a asset:a.mp4\ngroup: *a\n"))
	f.Add([]byte("version: 1\nsource: !!binary aGVsbG8=\n"))
	f.Add([]byte("{version: 1, source: x, fit: cover}"))
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
		if back != c {
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

package assetbuild_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/assetbuild"
)

// fullProject uses every field, in the order and form Encode writes it.
const fullProject = `version: 1
assets:
  cache: build/assets-cache.yaml
  atlases:
    - name: ui
      in: art/ui
      out: assets/atlases
      padding: 0
      extrude: 1
      maxWidth: 1024
      maxHeight: 512
      scale: 2
      trim: false
    - name: hero
      in: art/hero
      out: assets/atlases
  textures:
    - in: art/albedo.png
      out: assets/albedo.ktx2
      format: bc1
      mipmaps: true
      srgb: true
  audio:
    - in: sfx/jump.wav
      out: assets/jump.wav
      format: wav
      rate: 22050
      channels: 1
      peakDB: -1
    - in: music/theme.wav
      out: assets/theme.m4a
      format: aac
      rmsDB: -14.5
      bitrate: 192
  manifest:
    in: assets
    out: assets/manifest_gen.go
    package: gameassets
    prefix: 'res:'
    embed: true
    embedDir: assets
`

func TestParseEncodeRoundTrip(t *testing.T) {
	p, err := assetbuild.Parse([]byte(fullProject))
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Assets.Atlases[0]; got.Padding == nil || *got.Padding != 0 || got.Trim == nil || *got.Trim {
		t.Fatalf("explicit padding 0 and trim false were lost: %+v", got)
	}
	if got := p.Assets.Audio[0].PeakDB; got == nil || *got != -1 {
		t.Fatalf("peakDB = %v, want -1", got)
	}
	out, err := assetbuild.Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != fullProject {
		t.Fatalf("Encode changed the file:\n%s\nwant:\n%s", out, fullProject)
	}
	again, err := assetbuild.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, again) {
		t.Fatalf("round trip changed the project:\n%+v\n%+v", p, again)
	}
}

func TestParseMinimal(t *testing.T) {
	p, err := assetbuild.Parse([]byte("version: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Assets.CachePath(); got != assetbuild.DefaultCachePath {
		t.Fatalf("CachePath = %q, want %q", got, assetbuild.DefaultCachePath)
	}
	out, err := assetbuild.Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "version: 1\n" {
		t.Fatalf("Encode = %q", out)
	}
}

func TestParseRefuses(t *testing.T) {
	atlas := "version: 1\nassets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: assets\n"
	for _, tc := range []struct {
		name, src, want string
	}{
		{"empty", "  \n", "empty game.yaml"},
		{"version", "version: 2\n", "unsupported version 2"},
		{"no version", "assets: {}\n", "unsupported version 0"},
		{"unknown field", "version: 1\nrelease: {}\n", "field release not found"},
		{"unknown nested field", atlas + "      colour: red\n", "field colour not found"},
		{"trailing document", "version: 1\n---\nversion: 1\n", "trailing YAML document"},
		{"atlas name missing", "version: 1\nassets:\n  atlases:\n    - in: a\n      out: b\n", "name is required"},
		{"atlas name with slash", "version: 1\nassets:\n  atlases:\n    - name: a/b\n      in: a\n      out: b\n", "plain file name"},
		{"atlas name with dot", "version: 1\nassets:\n  atlases:\n    - name: .ui\n      in: a\n      out: b\n", "plain file name"},
		{"absolute path", "version: 1\nassets:\n  atlases:\n    - name: ui\n      in: /art\n      out: b\n", "inside the project"},
		{"escaping path", "version: 1\nassets:\n  atlases:\n    - name: ui\n      in: ../art\n      out: b\n", "inside the project"},
		{"unclean path", "version: 1\nassets:\n  atlases:\n    - name: ui\n      in: art//ui\n      out: b\n", "inside the project"},
		{"backslash", "version: 1\nassets:\n  atlases:\n    - name: ui\n      in: 'art\\ui'\n      out: b\n", "forward slashes"},
		{"missing out", "version: 1\nassets:\n  textures:\n    - in: a.png\n", "out: path is required"},
		{"out is the project", "version: 1\nassets:\n  textures:\n    - in: a.png\n      out: .\n", "names the project directory"},
		{"negative padding", atlas + "      padding: -1\n", "padding -1"},
		{"negative scale", atlas + "      scale: -2\n", "scale -2"},
		{"duplicate atlas", atlas + "    - name: ui\n      in: art/other\n      out: assets\n", "also writes"},
		{"texture format", "version: 1\nassets:\n  textures:\n    - in: a.png\n      out: a.ktx2\n      format: dxt1\n", "must be rgba8"},
		{"audio format", "version: 1\nassets:\n  audio:\n    - in: a.wav\n      out: a.ogg\n      format: ogg\n", "must be wav"},
		{"audio channels", "version: 1\nassets:\n  audio:\n    - in: a.wav\n      out: b.wav\n      channels: 3\n", "channels 3"},
		{"audio rate", "version: 1\nassets:\n  audio:\n    - in: a.wav\n      out: b.wav\n      rate: -1\n", "rate -1"},
		{"audio both levels", "version: 1\nassets:\n  audio:\n    - in: a.wav\n      out: b.wav\n      peakDB: -1\n      rmsDB: -14\n", "may not both be set"},
		{"audio infinite level", "version: 1\nassets:\n  audio:\n    - in: a.wav\n      out: b.wav\n      peakDB: .inf\n", "finite"},
		{"shared output", "version: 1\nassets:\n  textures:\n    - in: a.png\n      out: x\n  audio:\n    - in: a.wav\n      out: x\n", "also writes"},
		{"cache output", "version: 1\nassets:\n  cache: x\n  textures:\n    - in: a.png\n      out: x\n", "also writes"},
		{"atlas out is its in", "version: 1\nassets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: art/ui\n", "inside atlases[0]'s images"},
		{"atlas out under its in", "version: 1\nassets:\n  atlases:\n    - name: ui\n      in: art\n      out: art/pages\n", "inside atlases[0]'s images"},
		{"atlas in the project root", "version: 1\nassets:\n  atlases:\n    - name: ui\n      in: .\n      out: pages\n", "inside atlases[0]'s images"},
		{"atlas out under another's in", atlas + "    - name: hero\n      in: assets\n      out: hero\n", "atlases[0] writes its pages to assets, inside atlases[1]'s images"},
		{"atlas page names clash", atlas + "    - name: ui-2\n      in: art/other\n      out: assets\n", "atlases[1] writes assets/ui-2.png, which atlases[0] also writes"},
		{"texture over atlas page", atlas + "  textures:\n    - in: a.png\n      out: assets/ui.png\n", "textures[0] writes assets/ui.png, which atlases[0] also writes"},
		{"audio over atlas page", atlas + "  audio:\n    - in: a.wav\n      out: assets/ui-1.json\n", "audio[0] writes assets/ui-1.json, which atlases[0] also writes"},
		{"cache over atlas page", "version: 1\nassets:\n  cache: assets/ui.json\n  atlases:\n    - name: ui\n      in: art/ui\n      out: assets\n", "the cache writes assets/ui.json, which atlases[0] also writes"},
		{"cache in manifest folder", "version: 1\nassets:\n  cache: assets/cache.yaml\n  manifest:\n    in: assets\n    out: gen/assets.go\n", "inside the manifest's folder"},
		{"manifest not go", "version: 1\nassets:\n  manifest:\n    in: assets\n    out: assets/manifest.txt\n", ".go file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := assetbuild.Parse([]byte(tc.src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestParseAccepts(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"atlas pages in a dot folder of its images", "version: 1\nassets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: art/ui/.pages\n"},
		{"atlases sharing a folder", "version: 1\nassets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: assets\n    - name: ui-x\n      in: art/x\n      out: assets\n  textures:\n    - in: a.png\n      out: assets/ui.ktx2\n"},
		{"cache in a dot folder of the manifest's", "version: 1\nassets:\n  cache: assets/.cache/assets.yaml\n  manifest:\n    in: assets\n    out: gen/assets.go\n"},
		{"default cache with the manifest of the project", "version: 1\nassets:\n  manifest:\n    in: .\n    out: gen/assets.go\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := assetbuild.Parse([]byte(tc.src)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEncodeValidates(t *testing.T) {
	if _, err := assetbuild.Encode(&assetbuild.Project{}); err == nil {
		t.Fatal("Encode accepted version 0")
	}
	if _, err := assetbuild.Encode(nil); err == nil {
		t.Fatal("Encode accepted nil")
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(fullProject))
	f.Add([]byte("version: 1\n"))
	f.Add([]byte("version: 1\nassets:\n  audio:\n    - in: a.wav\n      out: b.wav\n      peakDB: -0\n"))
	f.Add([]byte("version: 1\nassets: {atlases: [{name: ui, in: a, out: .}]}\n"))
	f.Add([]byte("version: 1\nassets:\n  atlases: []\n  manifest: null\n"))
	f.Add([]byte("a: &a [*a]"))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := assetbuild.Parse(data)
		if err != nil {
			return
		}
		out, err := assetbuild.Encode(p)
		if err != nil {
			t.Fatalf("Encode of a parsed project failed: %v", err)
		}
		again, err := assetbuild.Parse(out)
		if err != nil {
			t.Fatalf("Parse of an encoded project failed: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(p, again) {
			t.Fatalf("round trip changed the project:\n%s", out)
		}
		out2, err := assetbuild.Encode(again)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("Encode is not stable: %v\n%s\n%s", err, out, out2)
		}
	})
}

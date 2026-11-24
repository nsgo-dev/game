package project_test

import (
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/project"
)

// assetsHead is the smallest valid project file, which the assets cases extend.
const assetsHead = "version: 1\nname: G\nid: dev.example.g\nappVersion: '1'\nwindow: {width: 1, height: 1}\n"

func TestAssetsDefaultCache(t *testing.T) {
	f := project.MustParse([]byte(assetsHead + "assets: {}\n"))
	if got := f.Assets.CachePath(); got != project.DefaultAssetsCache {
		t.Fatalf("CachePath = %q, want %q", got, project.DefaultAssetsCache)
	}
	f = project.MustParse([]byte(assetsHead + "assets:\n  cache: build/c.yaml\n  atlases: []\n"))
	if got := f.Assets.CachePath(); got != "build/c.yaml" || f.Assets.Atlases != nil {
		t.Fatalf("CachePath = %q, atlases = %#v", got, f.Assets.Atlases)
	}
	if err := (*project.Assets)(nil).Validate(); err != nil {
		t.Fatalf("Validate of no assets section: %v", err)
	}
}

func TestAssetsRefused(t *testing.T) {
	atlas := assetsHead + "assets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: assets\n"
	for _, tc := range []struct {
		name, src, want string
	}{
		{"unknown nested field", atlas + "      colour: red\n", "field colour not found"},
		{"atlas name missing", assetsHead + "assets:\n  atlases:\n    - in: a\n      out: b\n", "name is required"},
		{"atlas name with slash", assetsHead + "assets:\n  atlases:\n    - name: a/b\n      in: a\n      out: b\n", "plain file name"},
		{"atlas name with dot", assetsHead + "assets:\n  atlases:\n    - name: .ui\n      in: a\n      out: b\n", "plain file name"},
		{"absolute path", assetsHead + "assets:\n  atlases:\n    - name: ui\n      in: /art\n      out: b\n", "inside the project"},
		{"escaping path", assetsHead + "assets:\n  atlases:\n    - name: ui\n      in: ../art\n      out: b\n", "inside the project"},
		{"unclean path", assetsHead + "assets:\n  atlases:\n    - name: ui\n      in: art//ui\n      out: b\n", "inside the project"},
		{"backslash", assetsHead + "assets:\n  atlases:\n    - name: ui\n      in: 'art\\ui'\n      out: b\n", "forward slashes"},
		{"missing out", assetsHead + "assets:\n  textures:\n    - in: a.png\n", "out: path is required"},
		{"out is the project", assetsHead + "assets:\n  textures:\n    - in: a.png\n      out: .\n", "names the project directory"},
		{"negative padding", atlas + "      padding: -1\n", "padding -1"},
		{"negative scale", atlas + "      scale: -2\n", "scale -2"},
		{"duplicate atlas", atlas + "    - name: ui\n      in: art/other\n      out: assets\n", "also writes"},
		{"texture format", assetsHead + "assets:\n  textures:\n    - in: a.png\n      out: a.ktx2\n      format: dxt1\n", "must be rgba8"},
		{"audio format", assetsHead + "assets:\n  audio:\n    - in: a.wav\n      out: a.ogg\n      format: ogg\n", "must be wav"},
		{"audio channels", assetsHead + "assets:\n  audio:\n    - in: a.wav\n      out: b.wav\n      channels: 3\n", "channels 3"},
		{"audio rate", assetsHead + "assets:\n  audio:\n    - in: a.wav\n      out: b.wav\n      rate: -1\n", "rate -1"},
		{"audio both levels", assetsHead + "assets:\n  audio:\n    - in: a.wav\n      out: b.wav\n      peakDB: -1\n      rmsDB: -14\n", "may not both be set"},
		{"audio infinite level", assetsHead + "assets:\n  audio:\n    - in: a.wav\n      out: b.wav\n      peakDB: .inf\n", "finite"},
		{"shared output", assetsHead + "assets:\n  textures:\n    - in: a.png\n      out: x\n  audio:\n    - in: a.wav\n      out: x\n", "also writes"},
		{"cache output", assetsHead + "assets:\n  cache: x\n  textures:\n    - in: a.png\n      out: x\n", "also writes"},
		{"atlas out is its in", assetsHead + "assets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: art/ui\n", "inside atlases[0]'s images"},
		{"atlas out under its in", assetsHead + "assets:\n  atlases:\n    - name: ui\n      in: art\n      out: art/pages\n", "inside atlases[0]'s images"},
		{"atlas in the project root", assetsHead + "assets:\n  atlases:\n    - name: ui\n      in: .\n      out: pages\n", "inside atlases[0]'s images"},
		{"atlas out under another's in", atlas + "    - name: hero\n      in: assets\n      out: hero\n", "atlases[0] writes its pages to assets, inside atlases[1]'s images"},
		{"atlas page names clash", atlas + "    - name: ui-2\n      in: art/other\n      out: assets\n", "atlases[1] writes assets/ui-2.png, which atlases[0] also writes"},
		{"texture over atlas page", atlas + "  textures:\n    - in: a.png\n      out: assets/ui.png\n", "textures[0] writes assets/ui.png, which atlases[0] also writes"},
		{"audio over atlas page", atlas + "  audio:\n    - in: a.wav\n      out: assets/ui-1.json\n", "audio[0] writes assets/ui-1.json, which atlases[0] also writes"},
		{"cache over atlas page", assetsHead + "assets:\n  cache: assets/ui.json\n  atlases:\n    - name: ui\n      in: art/ui\n      out: assets\n", "the cache writes assets/ui.json, which atlases[0] also writes"},
		{"cache in manifest folder", assetsHead + "assets:\n  cache: assets/cache.yaml\n  manifest:\n    in: assets\n    out: gen/assets.go\n", "inside the manifest's folder"},
		{"manifest not go", assetsHead + "assets:\n  manifest:\n    in: assets\n    out: assets/manifest.txt\n", ".go file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := project.Parse([]byte(tc.src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestAssetsAccepted(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"atlas pages in a dot folder of its images", assetsHead + "assets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: art/ui/.pages\n"},
		{"atlases sharing a folder", assetsHead + "assets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: assets\n    - name: ui-x\n      in: art/x\n      out: assets\n  textures:\n    - in: a.png\n      out: assets/ui.ktx2\n"},
		{"cache in a dot folder of the manifest's", assetsHead + "assets:\n  cache: assets/.cache/assets.yaml\n  manifest:\n    in: assets\n    out: gen/assets.go\n"},
		{"default cache with the manifest of the project", assetsHead + "assets:\n  manifest:\n    in: .\n    out: gen/assets.go\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := project.Parse([]byte(tc.src)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

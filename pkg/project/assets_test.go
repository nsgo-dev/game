package project_test

import (
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/project"
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
		{"texture out is its in", assetsHead + "assets:\n  textures:\n    - in: art/a.png\n      out: art/a.png\n", "textures[0] writes art/a.png, which textures[0] reads"},
		{"audio out is its in", assetsHead + "assets:\n  audio:\n    - in: sfx/j.wav\n      out: sfx/j.wav\n", "audio[0] writes sfx/j.wav, which audio[0] reads"},
		{"audio out is a texture's in", assetsHead + "assets:\n  textures:\n    - in: a.png\n      out: a.ktx2\n  audio:\n    - in: a.wav\n      out: a.png\n", "audio[0] writes a.png, which textures[0] reads"},
		{"cache is an input", assetsHead + "assets:\n  cache: sfx/j.wav\n  audio:\n    - in: sfx/j.wav\n      out: j.wav\n", "the cache writes sfx/j.wav, which audio[0] reads"},
		{"cache is game.yaml", assetsHead + "assets:\n  cache: game.yaml\n", "the cache writes game.yaml, the project file"},
		{"texture out is game.yaml", assetsHead + "assets:\n  textures:\n    - in: a.png\n      out: game.yaml\n", "textures[0] writes game.yaml, the project file"},
		{"audio out is game.yaml", assetsHead + "assets:\n  audio:\n    - in: a.wav\n      out: game.yaml\n", "audio[0] writes game.yaml, the project file"},
		{"texture png in atlas images", atlas + "  textures:\n    - in: a.png\n      out: art/ui/a.PNG\n", "textures[0] writes art/ui/a.PNG, inside atlases[0]'s images in art/ui"},
		{"audio png in atlas images", atlas + "  audio:\n    - in: a.wav\n      out: art/ui/sub/j.png\n", "audio[0] writes art/ui/sub/j.png, inside atlases[0]'s images in art/ui"},
		{"cache png in atlas images", atlas + "  cache: art/ui/cache.png\n", "the cache writes art/ui/cache.png, inside atlases[0]'s images in art/ui"},
		{"manifest not go", assetsHead + "assets:\n  manifest:\n    in: assets\n    out: assets/manifest.txt\n", ".go file"},
		{"font in not a font", assetsHead + "assets:\n  fonts:\n    - in: a.woff\n      out: a.fnt\n", ".ttf or .otf"},
		{"font out not fnt", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: a.txt\n", ".fnt file"},
		{"font out without a name", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: fonts/.fnt\n", "does not start with a dot"},
		{"font out with a quote", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: 'fonts/a\"b.fnt'\n", "double quote"},
		{"font size", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: a.fnt\n      size: -4\n", "size -4"},
		{"font padding", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: a.fnt\n      padding: -1\n", "padding -1"},
		{"font chars", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: a.fnt\n      chars: \"a\\tb\"\n", "control character"},
		{"font pages clash with an atlas", atlas + "  fonts:\n    - in: a.ttf\n      out: assets/ui.fnt\n", "fonts[0] writes assets/ui.png, which atlases[0] also writes"},
		{"font pages in atlas images", atlas + "  fonts:\n    - in: a.ttf\n      out: art/ui/hud.fnt\n", "fonts[0] writes its pages to art/ui, inside atlases[0]'s images"},
		{"texture over font page", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: assets/hud.fnt\n  textures:\n    - in: a.png\n      out: assets/hud-1.png\n", "textures[0] writes assets/hud-1.png, which fonts[0] also writes"},
		{"texture reads a font page", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: assets/hud.fnt\n  textures:\n    - in: assets/hud.png\n      out: assets/hud.ktx2\n", "textures[0] reads assets/hud.png, a page fonts[0] writes"},
		{"texture reads a page of a font with several", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: assets/hud.fnt\n  textures:\n    - in: assets/hud-2.png\n      out: assets/hud.ktx2\n", "textures[0] reads assets/hud-2.png"},
		{"font size too large", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: a.fnt\n      size: 100000\n", "size 100000"},
		{"two fonts write one fnt", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: hud.fnt\n    - in: b.otf\n      out: hud.fnt\n", "also writes"},
		{"model in not a model", assetsHead + "assets:\n  models:\n    - in: a.obj\n      out: a.glb\n", ".gltf or .glb"},
		{"model out not glb", assetsHead + "assets:\n  models:\n    - in: a.gltf\n      out: a.gltf\n", ".glb file"},
		{"model scale", assetsHead + "assets:\n  models:\n    - in: a.gltf\n      out: a.glb\n      scale: -1\n", "scale -1"},
		{"model out is its in", assetsHead + "assets:\n  models:\n    - in: a.glb\n      out: a.glb\n", "models[0] writes a.glb, which models[0] reads"},
		{"model out is a font's out", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: a.fnt\n  models:\n    - in: a.gltf\n      out: a.fnt\n", "out \"a.fnt\" must be a .glb file"},
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
		{"texture of an atlas page", assetsHead + "assets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: assets\n  textures:\n    - in: assets/ui.png\n      out: assets/ui.ktx2\n"},
		{"non-PNG output beside atlas images", assetsHead + "assets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: assets\n  textures:\n    - in: a.png\n      out: art/ui/a.ktx2\n"},
		{"default cache with the manifest of the project", assetsHead + "assets:\n  manifest:\n    in: .\n    out: gen/assets.go\n"},
		{"font and model", assetsHead + "assets:\n  fonts:\n    - in: fonts/Inter.TTF\n      out: assets/hud.fnt\n      size: 24\n      chars: \" 0123456789\"\n      padding: 0\n  models:\n    - in: models/ship.gltf\n      out: assets/ship.glb\n      scale: 0.01\n      animations: false\n"},
		{"texture of a JSON-named neighbor of a font page", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: assets/hud.fnt\n  textures:\n    - in: assets/hud-x.png\n      out: assets/hud.ktx2\n"},
		{"largest font size", assetsHead + "assets:\n  fonts:\n    - in: a.ttf\n      out: a.fnt\n      size: 2048\n"},
		{"font beside an atlas", assetsHead + "assets:\n  atlases:\n    - name: ui\n      in: art/ui\n      out: assets\n  fonts:\n    - in: a.otf\n      out: assets/ui-x.fnt\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := project.Parse([]byte(tc.src)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAssetsIsOutput(t *testing.T) {
	f := project.MustParse([]byte(assetsHead + `assets:
  atlases:
    - name: ui
      in: art/ui
      out: assets
  textures:
    - in: art/sky.png
      out: assets/sky.ktx2
  audio:
    - in: sfx/jump.wav
      out: assets/jump.wav
  fonts:
    - in: fonts/hud.ttf
      out: assets/fonts/hud.fnt
  models:
    - in: models/ship.gltf
      out: assets/ship.glb
  manifest:
    in: assets
    out: internal/assets/assets.go
`))
	for p, want := range map[string]bool{
		".game/assets-cache.yaml":   true,
		"assets/ui.png":             true,
		"assets/ui.json":            true,
		"assets/ui-2.png":           true,
		"assets/ui-x.png":           false,
		"assets/sub/ui.png":         false,
		"assets/sky.ktx2":           true,
		"assets/jump.wav":           true,
		"internal/assets/assets.go": true,
		"art/sky.png":               false,
		"sfx/jump.wav":              false,
		"assets/font.ttf":           false,
		"assets/fonts/hud.fnt":      true,
		"assets/fonts/hud.png":      true,
		"assets/fonts/hud-3.png":    true,
		"assets/fonts/hud.json":     false,
		"assets/fonts/hud-x.png":    false,
		"fonts/hud.ttf":             false,
		"assets/ship.glb":           true,
		"models/ship.gltf":          false,
	} {
		if got := f.Assets.IsOutput(p); got != want {
			t.Errorf("IsOutput(%q) = %v, want %v", p, got, want)
		}
	}
	if (*project.Assets)(nil).IsOutput("assets/ui.png") {
		t.Error("a nil assets section writes a file")
	}
}

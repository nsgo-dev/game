package project_test

import (
	"bytes"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func readGolden(t testing.TB) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/game.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRoundTripKeepsTheFileByteForByte(t *testing.T) {
	golden := readGolden(t)
	f, err := project.Parse(golden)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := project.Encode(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(out, golden) {
		t.Fatalf("Encode(Parse(golden)) differs:\n%s\nwant:\n%s", out, golden)
	}
	again, err := project.Parse(out)
	if err != nil {
		t.Fatalf("Parse(Encode): %v", err)
	}
	if !reflect.DeepEqual(again, f) {
		t.Fatalf("Parse(Encode(f)) = %+v, want %+v", again, f)
	}
}

func TestParseReadsEveryField(t *testing.T) {
	f, err := project.Parse(readGolden(t))
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "Cave Run" || f.ID != "dev.nsgo.caverun" || f.AppVersion != "0.1.0" || f.Title() != "Cave Run" {
		t.Fatalf("identity = %q %q %q %q", f.Name, f.ID, f.AppVersion, f.Title())
	}
	if w, h := f.WindowSize(); w != 960 || h != 540 {
		t.Fatalf("WindowSize = %v, %v", w, h)
	}
	if c := f.WindowConstraints(); c != (core.WindowConstraints{MinWidth: 480, MinHeight: 270}) {
		t.Fatalf("WindowConstraints = %+v", c)
	}
	if f.Appearance() != core.AppearanceDark {
		t.Fatalf("Appearance = %v", f.Appearance())
	}
	vp, ok := f.Viewport()
	want := screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFit, IntegerScale: true, PixelSnap: true, BarColor: core.RGB(0x0b0d17)}
	if !ok || vp != want {
		t.Fatalf("Viewport = %+v, %v; want %+v", vp, ok, want)
	}
	if d := f.Audio.Buses[1].Effects[1].Delay; d == nil || d.Time != 250*time.Millisecond {
		t.Fatalf("sfx delay = %+v", d)
	}
	if got := f.BuildTargets(); !reflect.DeepEqual(got, []string{"macos", "windows_x64", "web"}) {
		t.Fatalf("BuildTargets = %v", got)
	}
	if p, ok := f.Preset("windows"); !ok || !reflect.DeepEqual(p.Targets, []string{"windows_x64"}) {
		t.Fatalf("Preset(windows) = %+v, %v", p, ok)
	}
	if _, ok := f.Preset("ios"); ok {
		t.Fatal("Preset(ios) found a preset the file has not")
	}
	if f.Editor == nil || !reflect.DeepEqual(f.Editor.Plugins, []string{"health", "waypoints"}) {
		t.Fatalf("Editor = %+v, want plugins health and waypoints", f.Editor)
	}
	if f.Assets == nil || f.Assets.CachePath() != "build/assets-cache.yaml" {
		t.Fatalf("Assets = %+v", f.Assets)
	}
	if got := f.Assets.Atlases[0]; got.Name != "ui" || got.Padding == nil || *got.Padding != 0 || got.Trim == nil || *got.Trim {
		t.Fatalf("Assets.Atlases[0] = %+v", got)
	}
	if got := f.Assets.Textures[0]; got.Format != "bc1" || !got.Mipmaps || !got.SRGB {
		t.Fatalf("Assets.Textures[0] = %+v", got)
	}
	if got := f.Assets.Audio[0]; got.PeakDB == nil || *got.PeakDB != -1 {
		t.Fatalf("Assets.Audio[0] = %+v", got)
	}
	if got := f.Assets.Manifest; got == nil || got.Package != "gameassets" || got.Prefix != "res:" || !got.Embed {
		t.Fatalf("Assets.Manifest = %+v", got)
	}
	if n := f.Navigation; n == nil || n.CellSize != 0.2 || n.MaxVertsPerPoly != 6 || len(n.Agents) != 2 || n.Agents[0] != (project.NavAgent{Name: "humanoid", Radius: 0.4, Height: 1.8, MaxSlope: 40, MaxClimb: 0.4}) || n.Agents[1] != (project.NavAgent{Name: "rat"}) {
		t.Fatalf("Navigation = %+v", f.Navigation)
	}
	tiles := f.FindPack("cave-tiles")
	if tiles == nil || tiles.Kind != project.PackAsset || tiles.PackVersion != "1.2.0" || !reflect.DeepEqual(tiles.Resources, []string{"cave-tiles"}) {
		t.Fatalf("FindPack(cave-tiles) = %+v", tiles)
	}
	if got, want := tiles.Paths(), []string{"packs/cave-tiles/pack.yaml", "packs/cave-tiles/tiles.json", "packs/cave-tiles/tiles.png"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Paths = %v, want %v", got, want)
	}
	if p := f.FindPack("waypoint-tools"); p == nil || !reflect.DeepEqual(p.Plugins, []string{"waypoints"}) {
		t.Fatalf("FindPack(waypoint-tools) = %+v", p)
	}
	if f.FindPack("other") != nil {
		t.Fatal("FindPack found a pack the file has not")
	}
}

func TestDefaultsOfAMinimalFile(t *testing.T) {
	f, err := project.Parse([]byte("version: 1\nname: Starter\nid: dev.example.starter\nappVersion: 1.0.0\nwindow: {width: 480, height: 640}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Viewport(); ok {
		t.Fatal("Viewport reported a display the file has not")
	}
	if f.Appearance() != core.AppearanceSystem || f.WindowConstraints() != (core.WindowConstraints{}) {
		t.Fatalf("appearance %v, constraints %+v", f.Appearance(), f.WindowConstraints())
	}
	if f.BuildTargets() != nil {
		t.Fatalf("BuildTargets = %v, want nil", f.BuildTargets())
	}
	l, err := f.LoadAutoloads(fstest.MapFS{})
	if err != nil || l.Main() != nil || len(l.Scenes) != 0 || len(l.Resources) != 0 {
		t.Fatalf("LoadAutoloads = %+v, %v", l, err)
	}
	a := input.NewActions()
	f.BindInput(a)
	if a.Bindings("left") != nil {
		t.Fatal("BindInput bound actions the file has not")
	}
	out, err := project.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	want := "version: 1\nname: Starter\nid: dev.example.starter\nappVersion: 1.0.0\nwindow:\n  width: 480\n  height: 640\n"
	if string(out) != want {
		t.Fatalf("Encode = %q, want %q", out, want)
	}
}

func TestValidateRefusesInvalidFiles(t *testing.T) {
	const head = "version: 1\nname: Game\nid: dev.example.game\nappVersion: 1.0.0\nwindow: {width: 640, height: 360}\n"
	const sum = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	cases := map[string]string{
		"empty":                        "",
		"version":                      strings.Replace(head, "version: 1", "version: 2", 1),
		"unknown field":                head + "colour: red\n",
		"unknown nested field":         head + "display: {width: 1, height: 1, zoom: 2}\n",
		"trailing document":            head + "---\nversion: 1\n",
		"no name":                      strings.Replace(head, "name: Game", "name: ''", 1),
		"name spaces":                  strings.Replace(head, "name: Game", "name: ' Game'", 1),
		"bad id":                       strings.Replace(head, "dev.example.game", "game", 1),
		"id digit":                     strings.Replace(head, "dev.example.game", "dev.1game", 1),
		"id char":                      strings.Replace(head, "dev.example.game", "dev.ex ample", 1),
		"no app version":               strings.Replace(head, "appVersion: 1.0.0", "appVersion: ''", 1),
		"window size":                  strings.Replace(head, "width: 640", "width: 0", 1),
		"window below minimum":         strings.Replace(head, "height: 360}", "height: 360, minHeight: 400}", 1),
		"window max below min":         strings.Replace(head, "height: 360}", "height: 360, minWidth: 100, maxWidth: 50}", 1),
		"window appearance":            strings.Replace(head, "height: 360}", "height: 360, appearance: dim}", 1),
		"window nan":                   strings.Replace(head, "width: 640", "width: .nan", 1),
		"display size":                 head + "display: {width: 320, height: -1}\n",
		"navigation cell size":         head + "navigation: {cellSize: -1}\n",
		"navigation nan":               head + "navigation: {cellHeight: .nan}\n",
		"navigation detail spacing":    head + "navigation: {cellSize: 0.3, detailSampleDistance: 0.001}\n",
		"navigation verts":             head + "navigation: {maxVertsPerPoly: 2}\n",
		"navigation unknown field":     head + "navigation: {agentRadius: 1}\n",
		"navigation agent name":        head + "navigation: {agents: [{name: ''}]}\n",
		"navigation agent spaces":      head + "navigation: {agents: [{name: big rat}]}\n",
		"navigation agent twice":       head + "navigation: {agents: [{name: rat}, {name: rat}]}\n",
		"navigation agent radius":      head + "navigation: {agents: [{name: rat, radius: -0.1}]}\n",
		"navigation agent slope":       head + "navigation: {agents: [{name: rat, maxSlope: 90}]}\n",
		"display mode":                 head + "display: {width: 320, height: 180, mode: zoom}\n",
		"display bar color":            head + "display: {width: 320, height: 180, barColor: 'not a color'}\n",
		"action name":                  head + "input: {actions: [{name: ''}]}\n",
		"action spaces":                head + "input: {actions: [{name: 'go left'}]}\n",
		"duplicate action":             head + "input: {actions: [{name: a}, {name: a}]}\n",
		"binding kind":                 head + "input: {actions: [{name: a, bindings: [{kind: voice}]}]}\n",
		"key without key":              head + "input: {actions: [{name: a, bindings: [{kind: key}]}]}\n",
		"key with spaces":              head + "input: {actions: [{name: a, bindings: [{kind: key, key: 'space bar'}]}]}\n",
		"key with button":              head + "input: {actions: [{name: a, bindings: [{kind: key, key: a, button: left}]}]}\n",
		"key with code":                head + "input: {actions: [{name: a, bindings: [{kind: key, key: a, code: KeyA}]}]}\n",
		"key with region":              head + "input: {actions: [{name: a, bindings: [{kind: key, key: a, region: {width: 1, height: 1}}]}]}\n",
		"keyCode without code":         head + "input: {actions: [{name: a, bindings: [{kind: keyCode}]}]}\n",
		"keyCode with spaces":          head + "input: {actions: [{name: a, bindings: [{kind: keyCode, code: 'Key A'}]}]}\n",
		"keyCode with key":             head + "input: {actions: [{name: a, bindings: [{kind: keyCode, code: KeyA, key: a}]}]}\n",
		"keyCode with region":          head + "input: {actions: [{name: a, bindings: [{kind: keyCode, code: KeyA, region: {width: 1, height: 1}}]}]}\n",
		"gamepadButton without button": head + "input: {actions: [{name: a, bindings: [{kind: gamepadButton}]}]}\n",
		"gamepadButton unknown":        head + "input: {actions: [{name: a, bindings: [{kind: gamepadButton, button: turbo}]}]}\n",
		"gamepadButton with key":       head + "input: {actions: [{name: a, bindings: [{kind: gamepadButton, button: south, key: a}]}]}\n",
		"gamepadButton with region":    head + "input: {actions: [{name: a, bindings: [{kind: gamepadButton, button: south, region: {width: 1, height: 1}}]}]}\n",
		"touch without region":         head + "input: {actions: [{name: a, bindings: [{kind: touchRegion}]}]}\n",
		"touch with button":            head + "input: {actions: [{name: a, bindings: [{kind: touchRegion, button: left, region: {width: 1, height: 1}}]}]}\n",
		"touch with key":               head + "input: {actions: [{name: a, bindings: [{kind: touchRegion, key: a, region: {width: 1, height: 1}}]}]}\n",
		"mouse with region":            head + "input: {actions: [{name: a, bindings: [{kind: mouseButton, button: left, region: {width: 1, height: 1}}]}]}\n",
		"mouse with code":              head + "input: {actions: [{name: a, bindings: [{kind: mouseButton, button: left, code: KeyA}]}]}\n",
		"mouse button":                 head + "input: {actions: [{name: a, bindings: [{kind: mouseButton, button: thumb}]}]}\n",
		"mouseRegion with key":         head + "input: {actions: [{name: a, bindings: [{kind: mouseRegion, button: left, key: a, region: {width: 1, height: 1}}]}]}\n",
		"region size":                  head + "input: {actions: [{name: a, bindings: [{kind: mouseRegion, button: left, region: {width: 0, height: 1}}]}]}\n",
		"axis without actions":         head + "input: {axes: [{name: x}]}\n",
		"axis undeclared action":       head + "input: {actions: [{name: a}], axes: [{name: x, negative: a, positive: b}]}\n",
		"duplicate axis":               head + "input: {actions: [{name: a}], axes: [{name: x, negative: a}, {name: x, positive: a}]}\n",
		"duplicate motion axis":        head + "input: {motionAxes: [{name: m, axis: tiltX}, {name: m, axis: tiltY}]}\n",
		"motion axis":                  head + "input: {motionAxes: [{name: m, axis: tilt}]}\n",
		"motion dead zone":             head + "input: {motionAxes: [{name: m, axis: tiltX, deadZone: 1}]}\n",
		"motion sensitivity":           head + "input: {motionAxes: [{name: m, axis: gyroZ, sensitivity: -1}]}\n",
		"duplicate gamepad axis":       head + "input: {gamepadAxes: [{name: g, axis: leftX}, {name: g, axis: rightX}]}\n",
		"gamepad axis unknown":         head + "input: {gamepadAxes: [{name: g, axis: triggerL}]}\n",
		"gamepad axis deadZone 1":      head + "input: {gamepadAxes: [{name: g, axis: leftX, deadZone: 1}]}\n",
		"gamepad axis deadZone nan":    head + "input: {gamepadAxes: [{name: g, axis: leftX, deadZone: .nan}]}\n",
		"gamepad axis sensitivity neg": head + "input: {gamepadAxes: [{name: g, axis: leftX, sensitivity: -1}]}\n",
		"unknown binding field":        head + "input: {actions: [{name: a, bindings: [{kind: key, key: a, extra: 1}]}]}\n",
		"master":                       head + "audio: {master: 1.5}\n",
		"bus name":                     head + "audio: {buses: [{name: ''}]}\n",
		"duplicate bus":                head + "audio: {buses: [{name: music}, {name: music}]}\n",
		"bus volume":                   head + "audio: {buses: [{name: music, volume: -0.1}]}\n",
		"no effect":                    head + "audio: {buses: [{name: music, effects: [{}]}]}\n",
		"two effects":                  head + "audio: {buses: [{name: music, effects: [{gain: {factor: 1}, bitcrush: {bits: 4}}]}]}\n",
		"cutoff":                       head + "audio: {buses: [{name: music, effects: [{lowPass: {cutoffHz: 0}}]}]}\n",
		"q":                            head + "audio: {buses: [{name: music, effects: [{highPass: {cutoffHz: 10, q: -1}}]}]}\n",
		"delay time":                   head + "audio: {buses: [{name: music, effects: [{delay: {time: 0s}}]}]}\n",
		"delay feedback":               head + "audio: {buses: [{name: music, effects: [{delay: {time: 1s, feedback: 1}}]}]}\n",
		"reverb wet":                   head + "audio: {buses: [{name: music, effects: [{reverb: {wet: 2}}]}]}\n",
		"gain":                         head + "audio: {buses: [{name: music, effects: [{gain: {factor: -1}}]}]}\n",
		"bits":                         head + "audio: {buses: [{name: music, effects: [{bitcrush: {bits: 16}}]}]}\n",
		"downsample":                   head + "audio: {buses: [{name: music, effects: [{bitcrush: {bits: 4, downsample: -1}}]}]}\n",
		"scene name":                   head + "autoload: {scenes: [{name: '', path: a.yaml}]}\n",
		"duplicate scene":              head + "autoload: {scenes: [{name: a, path: a.yaml}, {name: a, path: b.yaml}]}\n",
		"scene extension":              head + "autoload: {scenes: [{name: a, path: a.json}]}\n",
		"absolute path":                head + "autoload: {resources: [{name: a, path: /etc/passwd}]}\n",
		"escaping path":                head + "autoload: {resources: [{name: a, path: ../secret.txt}]}\n",
		"backslash path":               head + "autoload: {resources: [{name: a, path: 'data\\a.csv'}]}\n",
		"dot path":                     head + "autoload: {resources: [{name: a, path: .}]}\n",
		"main scene":                   head + "autoload: {mainScene: b, scenes: [{name: a, path: a.yaml}]}\n",
		"output":                       head + "export: {output: ../dist}\n",
		"platform":                     head + "export: {presets: [{platform: amiga}]}\n",
		"duplicate platform":           head + "export: {presets: [{platform: web}, {platform: web}]}\n",
		"foreign target":               head + "export: {presets: [{platform: macos, targets: [linux_x64]}]}\n",
		"unknown target":               head + "export: {presets: [{platform: macos, targets: [macos_ppc]}]}\n",
		"duplicate target":             head + "export: {presets: [{platform: macos, targets: [macos_arm64, macos_arm64]}]}\n",
		"assets atlas name":            head + "assets: {atlases: [{name: a/b, in: art, out: assets}]}\n",
		"assets atlas self scan":       head + "assets: {atlases: [{name: ui, in: art, out: art/pages}]}\n",
		"assets atlas page clash":      head + "assets: {atlases: [{name: ui, in: a, out: assets}, {name: ui-1, in: b, out: assets}]}\n",
		"assets texture format":        head + "assets: {textures: [{in: a.png, out: a.ktx2, format: dxt1}]}\n",
		"assets audio both levels":     head + "assets: {audio: [{in: a.wav, out: b.wav, peakDB: -1, rmsDB: -14}]}\n",
		"assets shared output":         head + "assets: {textures: [{in: a.png, out: x}], audio: [{in: a.wav, out: x}]}\n",
		"assets cache in manifest":     head + "assets: {cache: assets/cache.yaml, manifest: {in: assets, out: gen/assets.go}}\n",
		"assets manifest not go":       head + "assets: {manifest: {in: assets, out: gen/assets.txt}}\n",
		"plugin name":                  head + "editor: {plugins: ['']}\n",
		"plugin spaces":                head + "editor: {plugins: ['level tools']}\n",
		"duplicate plugin":             head + "editor: {plugins: [health, health]}\n",
		"unknown editor field":         head + "editor: {plugins: [health], theme: dark}\n",
		"pack name":                    head + "packs: [{name: Tiles, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/t}]\n",
		"pack kind":                    head + "packs: [{name: t, kind: music, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/t}]\n",
		"pack version":                 head + "packs: [{name: t, kind: asset, packVersion: '1 0', sha256: " + sum + ", dir: packs/t}]\n",
		"pack version partial":         head + "packs: [{name: t, kind: asset, packVersion: '1.2', sha256: " + sum + ", dir: packs/t}]\n",
		"pack version non-semver":      head + "packs: [{name: t, kind: asset, packVersion: latest, sha256: " + sum + ", dir: packs/t}]\n",
		"pack version leading v":       head + "packs: [{name: t, kind: asset, packVersion: v1.0.0, sha256: " + sum + ", dir: packs/t}]\n",
		"pack sha256":                  head + "packs: [{name: t, kind: asset, packVersion: 1.0.0, sha256: ABC, dir: packs/t}]\n",
		"pack dir":                     head + "packs: [{name: t, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: ../t}]\n",
		"pack root dir":                head + "packs: [{name: t, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: .}]\n",
		"pack file":                    head + "packs: [{name: t, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/t, files: [../x]}]\n",
		"pack files unsorted":          head + "packs: [{name: t, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/t, files: [b, a]}]\n",
		"pack files twice":             head + "packs: [{name: t, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/t, files: [a, a]}]\n",
		"duplicate pack":               head + "packs: [{name: t, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/t}, {name: t, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/u}]\n",
		"shared pack file":             head + "packs: [{name: t, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: packs, files: [t/a]}, {name: u, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/t, files: [a]}]\n",
		"pack missing resource":        head + "packs: [{name: t, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/t, resources: [tiles]}]\n",
		"pack missing scene":           head + "autoload: {resources: [{name: room, path: a.yaml}]}\npacks: [{name: t, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/t, scenes: [room]}]\n",
		"pack missing plugin":          head + "packs: [{name: t, kind: plugin, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/t, plugins: [paths]}]\n",
		"shared pack plugin":           head + "editor: {plugins: [paths]}\npacks: [{name: t, kind: plugin, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/t, plugins: [paths]}, {name: u, kind: plugin, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/u, plugins: [paths]}]\n",
		"unknown pack field":           head + "packs: [{name: t, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/t, url: x}]\n",
		"pack file case":               head + "packs: [{name: t, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: packs, files: [t/A.png]}, {name: u, kind: asset, packVersion: 1.0.0, sha256: " + sum + ", dir: packs/t, files: [a.png]}]\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if f, err := project.Parse([]byte(src)); err == nil {
				t.Fatalf("Parse accepted %q as %+v", src, f)
			}
		})
	}
	if _, err := project.Parse([]byte(head)); err != nil {
		t.Fatalf("the valid head fails: %v", err)
	}
	if err := project.Validate(nil); err == nil {
		t.Fatal("Validate(nil) succeeded")
	}
	if _, err := project.Encode(&project.File{}); err == nil {
		t.Fatal("Encode of an empty File succeeded")
	}
}

func TestValidatePackVersion(t *testing.T) {
	for _, good := range []string{"0.0.0", "1.0.0", "1.2.3-beta.1", "2.0.0+build.1"} {
		if err := project.ValidatePackVersion(good); err != nil {
			t.Errorf("ValidatePackVersion(%q) unexpected error: %v", good, err)
		}
	}
	for _, bad := range []string{"", "1", "1.2", "latest", "v1.0.0", "V1.0.0", "1 0", " 1.0.0", "1.0.0 ", "01.0.0", "1.0.0-"} {
		if err := project.ValidatePackVersion(bad); err == nil {
			t.Errorf("ValidatePackVersion(%q) succeeded, want error", bad)
		}
	}
}

func TestMustParsePanicsOnAnInvalidFile(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustParse did not panic")
		}
	}()
	project.MustParse([]byte("version: 9\n"))
}

func TestReadFS(t *testing.T) {
	fsys := fstest.MapFS{project.FileName: {Data: readGolden(t)}}
	f, err := project.ReadFS(fsys)
	if err != nil || f.Name != "Cave Run" {
		t.Fatalf("ReadFS = %+v, %v", f, err)
	}
	if _, err := project.ReadFS(fstest.MapFS{}); err == nil {
		t.Fatal("ReadFS without game.yaml succeeded")
	}
}

func TestCloneIsDeep(t *testing.T) {
	f := project.MustParse(readGolden(t))
	c := f.Clone()
	if !reflect.DeepEqual(c, f) {
		t.Fatal("Clone differs from the original")
	}
	*c.Window.Resizable = false
	c.Display.Width = 1
	c.Input.Actions[0].Bindings[0].Region.X = 99
	c.Input.Axes[0].Name = "other"
	*c.Audio.Master = 0
	*c.Audio.Buses[0].Volume = 0
	c.Audio.Buses[0].Effects[0].LowPass.CutoffHz = 1
	c.Autoload.Scenes[0].Path = "x.yaml"
	c.Export.Presets[1].Targets[0] = "windows_arm64"
	c.Editor.Plugins[0] = "other"
	*c.Assets.Atlases[0].Padding = 3
	*c.Assets.Atlases[0].Trim = true
	c.Assets.Textures[0].Format = "etc2"
	*c.Assets.Audio[0].PeakDB = -3
	*c.Assets.Audio[1].RMSDB = -20
	*c.Assets.Fonts[0].Padding = 4
	*c.Assets.Models[0].Animations = true
	c.Assets.Manifest.Package = "other"
	c.Navigation.CellSize = 9
	c.Navigation.Agents[0].Radius = 9
	c.Packs[0].Files[0] = "other"
	c.Packs[0].Resources[0] = "other"
	c.Packs[1].Plugins[0] = "other"
	if !reflect.DeepEqual(f, project.MustParse(readGolden(t))) {
		t.Fatal("changing the clone changed the original")
	}
	if (*project.File)(nil).Clone() != nil {
		t.Fatal("Clone of nil is not nil")
	}
}

func TestBindInput(t *testing.T) {
	f := project.MustParse(readGolden(t))
	a := input.NewActions()
	a.Bind("code", input.MouseButton(core.MouseButtonMiddle))
	f.BindInput(a)

	region := core.Bounds{Origin: core.Point{Y: 116}, Size: core.Size{Width: 72, Height: 64}}
	if got, want := a.Bindings("left"), []input.Input{input.TouchRegion(region), input.MouseRegion(region, core.MouseButtonLeft)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("left = %+v, want %+v", got, want)
	}
	if got, want := a.Bindings("fire"), []input.Input{input.MouseButton(core.MouseButtonRight)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fire = %+v, want %+v", got, want)
	}
	if a.Bindings("menu") != nil {
		t.Fatal("an action without bindings got some")
	}
	if a.Bindings("code") == nil {
		t.Fatal("BindInput dropped a binding the game made in code")
	}
	if neg, pos, ok := a.AxisBinding("move"); !ok || neg != "left" || pos != "right" {
		t.Fatalf("move axis = %q, %q, %v", neg, pos, ok)
	}
	if got, ok := f.Region("right"); !ok || got != (core.Bounds{Origin: core.Point{X: 72, Y: 116}, Size: core.Size{Width: 72, Height: 64}}) {
		t.Fatalf("Region(right) = %+v, %v", got, ok)
	}
	for _, name := range []string{"fire", "menu", "missing"} {
		if _, ok := f.Region(name); ok {
			t.Fatalf("Region(%s) found a region", name)
		}
	}
	if (project.Binding{Kind: "key"}).Input() != (input.Input{}) {
		t.Fatal("an invalid binding is not the zero Input")
	}
	if (project.Binding{Kind: "keyCode"}).Input() != (input.Input{}) {
		t.Fatal("a keyCode binding without code is not the zero Input")
	}
	if (project.Binding{Kind: "gamepadButton", Button: "nope"}).Input() != (input.Input{}) {
		t.Fatal("a gamepadButton binding with unknown button is not the zero Input")
	}
}

func TestKeyAndGamepadBindingsRoundTripAndFireInGame(t *testing.T) {
	raw, err := os.ReadFile("testdata/input_bindings.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f, err := project.Parse(raw)
	if err != nil {
		t.Fatalf("Parse(input_bindings.yaml): %v", err)
	}
	encoded, err := project.Encode(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(encoded, raw) {
		t.Fatalf("round-trip differs:\n%s\nwant:\n%s", encoded, raw)
	}

	// Verify Clone deep-copies GamepadAxes.
	cloned := f.Clone()
	cloned.Input.GamepadAxes[0].Name = "changed"
	if f.Input.GamepadAxes[0].Name != "move" {
		t.Fatal("mutating cloned GamepadAxes mutated original")
	}

	// Verify every gamepad button name and stick axis maps cleanly through BindInput.
	allButtons := make([]project.Binding, len(project.GamepadButtons))
	for i, btn := range project.GamepadButtons {
		allButtons[i] = project.Binding{Kind: project.KindGamepadButton, Button: btn}
	}
	allDoc := &project.File{
		Version:    project.CurrentVersion,
		Name:       "All",
		ID:         "dev.nsgo.all",
		AppVersion: "1.0.0",
		Window:     project.Window{Width: 320, Height: 180},
		Input: &project.Input{
			Actions: []project.Action{{Name: "any", Bindings: allButtons}},
			GamepadAxes: []project.GamepadAxis{
				{Name: "lx", Axis: "leftX", DeadZone: -1},
				{Name: "ly", Axis: "leftY"},
				{Name: "rx", Axis: "rightX"},
				{Name: "ry", Axis: "rightY"},
			},
		},
	}
	allYAML, err := project.Encode(allDoc)
	if err != nil {
		t.Fatalf("Encode all gamepad bindings: %v", err)
	}
	allParsed, err := project.Parse(allYAML)
	if err != nil {
		t.Fatalf("Parse all gamepad bindings: %v", err)
	}
	allActions := input.NewActions()
	allParsed.BindInput(allActions)
	if got := len(allActions.Bindings("any")); got != len(project.GamepadButtons) {
		t.Fatalf("bound gamepad buttons = %d, want %d", got, len(project.GamepadButtons))
	}
	for i, in := range allActions.Bindings("any") {
		if in.Kind != input.InputGamepadButton || in.PadButton != gamepad.Button(i) {
			t.Fatalf("button %d = %+v, want Button(%d)", i, in, i)
		}
	}
	if ax, opts, ok := allActions.GamepadAxisBinding("lx"); !ok || ax != gamepad.AxisLeftX || opts.DeadZone != -1 {
		t.Fatalf("lx = %v, %+v, %v", ax, opts, ok)
	}

	// Live game test: bind game.yaml into input.Actions in a running game.Loop,
	// press the bound key, keyCode, gamepad button and stick, and see the actions and axes fire.
	actions := input.NewActions()
	f.BindInput(actions)

	var (
		steps       int
		jumpPressed bool
		jumpJust    bool
		leftPressed bool
		firePressed bool
		moveAxis    float64
		aimYAxis    float64
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		actions.WatchKeys(cx)
		actions.WatchGamepads(cx)
		loop := &game.Loop{
			Rate: 60,
			Update: func(time.Duration) {
				actions.Step()
				steps++
				jumpPressed = actions.Pressed("jump")
				jumpJust = actions.JustPressed("jump")
				leftPressed = actions.Pressed("left")
				firePressed = actions.Pressed("fire")
				moveAxis = actions.Axis("move")
				aimYAxis = actions.Axis("aimY")
			},
		}
		loop.Run(cx)
		return core.View{}
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop, Gamepads: true}))

	step := func() {
		before := steps
		for steps == before {
			app.Advance(time.Second / 60)
		}
	}

	step()
	if jumpPressed || leftPressed || firePressed || moveAxis != 0 {
		t.Fatalf("idle state unexpected: jump=%v left=%v fire=%v move=%v", jumpPressed, leftPressed, firePressed, moveAxis)
	}

	// Bound key ("space") fires "jump".
	app.KeyDown("space")
	step()
	if !jumpPressed || !jumpJust {
		t.Fatalf("after pressing space: jumpPressed=%v jumpJust=%v, want true, true", jumpPressed, jumpJust)
	}
	app.KeyUp("space")
	step()
	if jumpPressed {
		t.Fatal("jump stayed pressed after releasing space")
	}

	// Bound keyCode ("KeyA") fires "left" and drives axis "move" to -1.
	app.KeyCodeDown("KeyA")
	step()
	if !leftPressed || moveAxis != -1 {
		t.Fatalf("after pressing KeyA: left=%v move=%v, want true, -1", leftPressed, moveAxis)
	}
	app.KeyCodeUp("KeyA")
	step()

	// Bound gamepad button and stick axes fire through WatchGamepads.
	app.ConnectGamepad(1, "Standard Pad")
	app.SetGamepadButton(1, gamepad.ButtonRT, 0.8)
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 0.575)
	app.SetGamepadAxis(1, gamepad.AxisRightY, 0.6)
	step()
	if !firePressed {
		t.Fatal("gamepad rightTrigger did not fire action fire")
	}
	if math.Abs(moveAxis-0.5) > 1e-3 {
		t.Fatalf("gamepad leftX moveAxis = %v, want ~0.5", moveAxis)
	}
	// RightY: axial deadZone 0.2 -> (0.6 - 0.2) / 0.8 = 0.5, sensitivity 1.5 -> 0.75, invert -> -0.75.
	if math.Abs(aimYAxis-(-0.75)) > 1e-3 {
		t.Fatalf("gamepad rightY aimYAxis = %v, want ~-0.75", aimYAxis)
	}
}

func TestBindInputBindsMotionAxes(t *testing.T) {
	f := project.MustParse([]byte("version: 1\nname: G\nid: dev.example.g\nappVersion: '1'\nwindow: {width: 1, height: 1}\ninput:\n  motionAxes:\n    - {name: a, axis: tiltX, deadZone: 0.1, sensitivity: 2, maxRange: 0.5, smoothing: 0.3, invert: true}\n    - {name: b, axis: tiltY}\n    - {name: c, axis: tiltZ}\n    - {name: d, axis: gyroX}\n    - {name: e, axis: gyroY}\n    - {name: f, axis: gyroZ}\n"))
	a := input.NewActions()
	f.BindInput(a)
	want := map[string]input.MotionAxis{"a": input.TiltX, "b": input.TiltY, "c": input.TiltZ, "d": input.GyroX, "e": input.GyroY, "f": input.GyroZ}
	for name, axis := range want {
		got, opts, ok := a.MotionAxisBinding(name)
		if !ok || got != axis {
			t.Fatalf("motion axis %s = %v, %v; want %v", name, got, ok, axis)
		}
		if name == "a" && opts != (input.MotionAxisOptions{DeadZone: 0.1, Sensitivity: 2, MaxRange: 0.5, Smoothing: 0.3, Invert: true}) {
			t.Fatalf("motion axis a options = %+v", opts)
		}
	}
}

func TestAnAxisAndAMotionAxisMayShareAName(t *testing.T) {
	f, err := project.Parse([]byte("version: 1\nname: G\nid: dev.example.g\nappVersion: '1'\nwindow: {width: 1, height: 1}\ninput:\n  actions:\n    - name: left\n      bindings:\n        - kind: touchRegion\n          region: {width: 100, height: 100}\n    - name: right\n      bindings:\n        - kind: touchRegion\n          region: {x: 100, width: 100, height: 100}\n  axes:\n    - {name: steer, negative: left, positive: right}\n  motionAxes:\n    - {name: steer, axis: tiltX, maxRange: 1}\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	a := input.NewActions()
	f.BindInput(a)
	// Tilting left (-0.4) while holding right (+1) steers +0.6: Actions.Axis sums both.
	const g = 9.80665
	a.OnMotion(core.MotionSample{Sensor: core.SensorAccelerometer, Vector: core.SensorVector{X: -0.4 * g}})
	a.OnTouch(core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{{ID: 1, Position: core.Point{X: 150, Y: 50}, Changed: true}}})
	a.Step()
	if got := a.Axis("steer"); got < 0.6-1e-6 || got > 0.6+1e-6 {
		t.Fatalf("steer = %v, want 0.6", got)
	}
	for _, dup := range []string{
		"  axes:\n    - {name: steer, negative: left}\n    - {name: steer, positive: right}\n",
		"  motionAxes:\n    - {name: steer, axis: tiltX}\n    - {name: steer, axis: tiltY}\n",
	} {
		src := "version: 1\nname: G\nid: dev.example.g\nappVersion: '1'\nwindow: {width: 1, height: 1}\ninput:\n  actions:\n    - name: left\n    - name: right\n" + dup
		if _, err := project.Parse([]byte(src)); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("Parse of a duplicate name = %v", err)
		}
	}
}

func TestConfigureMixer(t *testing.T) {
	f := project.MustParse(readGolden(t))
	var m *sound.Mixer
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		m = sound.NewMixer(cx, "sound")
		m.SetGroupEffects("ui", sound.Gain(2))
		f.ConfigureMixer(m)
		return core.View{}
	})
	if m.Master() != 0.9 || m.Volume("music") != 0.8 || m.Volume("sfx") != 1 {
		t.Fatalf("master %v, music %v, sfx %v", m.Master(), m.Volume("music"), m.Volume("sfx"))
	}
	if n := len(m.GroupEffects("music")); n != 2 {
		t.Fatalf("music has %d effects, want 2", n)
	}
	if n := len(m.GroupEffects("sfx")); n != 4 {
		t.Fatalf("sfx has %d effects, want 4", n)
	}
	if n := len(m.GroupEffects("ui")); n != 1 {
		t.Fatalf("a bus without effects left %d effects of the chain set in code, want 1", n)
	}

	// The configured chain is the sound package's: a gain of 0.5 halves a sample.
	samples := []float64{0.5, -0.5}
	project.Bus{Effects: []project.Effect{{Gain: &project.Gain{Factor: 0.5}}}}.Chain().ProcessSamples(samples, 44100, 1)
	if samples[0] != 0.25 || samples[1] != -0.25 {
		t.Fatalf("samples = %v, want [0.25 -0.25]", samples)
	}
	if (project.Effect{}).BusEffect() != nil || (project.Bus{}).Chain() != nil {
		t.Fatal("an empty effect or bus made a chain")
	}
}

func TestLoadAutoloads(t *testing.T) {
	f := project.MustParse(readGolden(t))
	scene := []byte("version: 1\nname: Title\nlayers:\n  - name: main\n")
	fsys := fstest.MapFS{
		"scenes/title.yaml": {Data: scene},
		"data/enemies.csv":  {Data: []byte("name,hp\nbat,3\n")},
		// The resource the installed cave-tiles pack added.
		"packs/cave-tiles/tiles.json": {Data: []byte("{}")},
	}
	l, err := f.LoadAutoloads(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if l.Main() == nil || l.Main().Name != "Title" || string(l.Resources["enemies"]) != "name,hp\nbat,3\n" {
		t.Fatalf("Loaded = %+v", l)
	}

	missing := fstest.MapFS{"scenes/title.yaml": {Data: scene}}
	if _, err := f.LoadAutoloads(missing); err == nil || !strings.Contains(err.Error(), "enemies") {
		t.Fatalf("a missing resource: %v", err)
	}
	invalid := fstest.MapFS{"scenes/title.yaml": {Data: []byte("version: 7\n")}, "data/enemies.csv": {}}
	if _, err := f.LoadAutoloads(invalid); err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("an invalid scene: %v", err)
	}
	if _, err := f.LoadAutoloads(fstest.MapFS{}); err == nil {
		t.Fatal("a missing scene loaded")
	}
	if (*project.Loaded)(nil).Main() != nil {
		t.Fatal("Main of nil is not nil")
	}
}

func TestTargetsOf(t *testing.T) {
	if got := project.TargetsOf("macos"); !reflect.DeepEqual(got, []string{"macos_arm64", "macos_x64"}) {
		t.Fatalf("TargetsOf(macos) = %v", got)
	}
	if got := project.TargetsOf("ios"); !reflect.DeepEqual(got, []string{"ios", "ios_simulator"}) {
		t.Fatalf("TargetsOf(ios) = %v", got)
	}
	for _, p := range project.Platforms {
		if len(project.TargetsOf(p)) == 0 {
			t.Fatalf("platform %s has no targets", p)
		}
	}
}

func BenchmarkParse(b *testing.B) {
	data := readGolden(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := project.Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	f := project.MustParse(readGolden(b))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := project.Encode(f); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCheckReportsEveryProblemOfAProject(t *testing.T) {
	golden := readGolden(t)
	f := project.MustParse(golden)
	nsgo, err := f.SyncNSGo(nil)
	if err != nil {
		t.Fatal(err)
	}
	good := fstest.MapFS{
		project.FileName:                 {Data: golden},
		project.NSGoFileName:             {Data: nsgo},
		"scenes/title.yaml":              {Data: []byte("version: 1\nname: Title\nlayers:\n  - name: main\n")},
		"data/enemies.csv":               {Data: []byte("name,hp\n")},
		"packs/cave-tiles/pack.yaml":     {Data: []byte("version: 1\n")},
		"packs/cave-tiles/tiles.json":    {Data: []byte("{}")},
		"packs/cave-tiles/tiles.png":     {Data: []byte("png")},
		"packs/waypoint-tools/pack.yaml": {Data: []byte("version: 1\n")},
	}
	if got, err := project.Check(good); err != nil || got.Name != "Cave Run" {
		t.Fatalf("Check of a good project = %+v, %v", got, err)
	}

	// Without nsgo.yaml there is nothing to disagree with.
	noNSGo := fstest.MapFS{}
	for k, v := range good {
		if k != project.NSGoFileName {
			noNSGo[k] = v
		}
	}
	if _, err := project.Check(noNSGo); err != nil {
		t.Fatalf("Check without nsgo.yaml: %v", err)
	}

	// A stale nsgo.yaml, a missing resource and a missing pack file are all reported, and the file still comes back.
	bad := fstest.MapFS{
		project.FileName:     {Data: golden},
		project.NSGoFileName: {Data: []byte("name: Old Name\n")},
		"scenes/title.yaml":  good["scenes/title.yaml"],
	}
	for name, file := range good {
		if strings.HasPrefix(name, "packs/") && name != "packs/cave-tiles/tiles.png" {
			bad[name] = file
		}
	}
	got, err := project.Check(bad)
	if got == nil || err == nil || !strings.Contains(err.Error(), "name is \"Old Name\"") || !strings.Contains(err.Error(), "enemies") || !strings.Contains(err.Error(), "packs/cave-tiles/tiles.png") {
		t.Fatalf("Check of a bad project = %v, %v", got, err)
	}

	if _, err := project.Check(fstest.MapFS{project.FileName: {Data: []byte("version: 2\n")}}); err == nil {
		t.Fatal("Check accepted an invalid game.yaml")
	}
	if _, err := project.Check(fstest.MapFS{}); err == nil {
		t.Fatal("Check accepted a project without game.yaml")
	}
}

func TestEmptyEditorSectionRoundTrips(t *testing.T) {
	const head = "version: 1\nname: Game\nid: dev.example.game\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\n"
	for _, section := range []string{"editor: {}\n", "editor: {plugins: []}\n", "editor:\n  plugins: []\n"} {
		f, err := project.Parse([]byte(head + section))
		if err != nil {
			t.Fatalf("%q: Parse: %v", section, err)
		}
		if f.Editor == nil || len(f.Editor.Plugins) != 0 {
			t.Fatalf("%q: Editor = %+v, want an empty section", section, f.Editor)
		}
		out, err := project.Encode(f)
		if err != nil {
			t.Fatalf("%q: Encode: %v", section, err)
		}
		// An empty plugin list is the same as none, so every spelling saves as editor: {} and keeps the section.
		if want := head + "editor: {}\n"; string(out) != want {
			t.Fatalf("%q: Encode = %q, want %q", section, out, want)
		}
		again, err := project.Parse(out)
		if err != nil {
			t.Fatalf("%q: Parse(Encode): %v", section, err)
		}
		if again2, _ := project.Encode(again); !bytes.Equal(again2, out) {
			t.Fatalf("%q: second round trip = %q, want %q", section, again2, out)
		}
	}
}

func TestExportBudgetsRoundTripAndValidation(t *testing.T) {
	const raw = `version: 1
name: Game
id: dev.example.game
appVersion: 1.0.0
window:
  width: 640
  height: 360
export:
  output: dist
  budgets:
    qualityProfile: low
    downloadSizeMB: 80
    frameTimeP95Ms: 16.7
  presets:
    - platform: web
`
	f, err := project.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse valid export.budgets: %v", err)
	}
	if f.Export == nil || f.Export.Budgets == nil {
		t.Fatal("expected Export.Budgets to be parsed")
	}
	if f.Export.Budgets.QualityProfile != "low" || f.Export.Budgets.DownloadSizeMB != 80 || f.Export.Budgets.FrameTimeP95Ms != 16.7 {
		t.Fatalf("Budgets = %+v", f.Export.Budgets)
	}
	encoded, err := project.Encode(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(encoded) != raw {
		t.Fatalf("round trip mismatch:\ngot:\n%s\nwant:\n%s", encoded, raw)
	}
	cloned := f.Clone()
	cloned.Export.Budgets.DownloadSizeMB = 99
	if f.Export.Budgets.DownloadSizeMB != 80 {
		t.Fatal("Clone shared Budgets pointer with original")
	}

	const head = "version: 1\nname: Game\nid: dev.example.game\nappVersion: 1.0.0\nwindow:\n  width: 640\n  height: 360\n"
	badCases := []struct {
		name string
		yaml string
		want string
	}{
		{"unknown field", head + "export:\n  budgets:\n    extra: 1\n", "extra"},
		{"space in qualityProfile", head + "export:\n  budgets:\n    qualityProfile: low spec\n", "qualityProfile"},
		{"negative downloadSizeMB", head + "export:\n  budgets:\n    downloadSizeMB: -1\n", "downloadSizeMB"},
		{"negative frameTimeP95Ms", head + "export:\n  budgets:\n    frameTimeP95Ms: -0.5\n", "frameTimeP95Ms"},
		{"nan downloadSizeMB", head + "export:\n  budgets:\n    downloadSizeMB: .nan\n", "downloadSizeMB"},
		{"inf frameTimeP95Ms", head + "export:\n  budgets:\n    frameTimeP95Ms: .inf\n", "frameTimeP95Ms"},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := project.Parse([]byte(tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse(%s) = %v, want error containing %q", tc.name, err, tc.want)
			}
		})
	}
	fBad := f.Clone()
	fBad.Export.Budgets.FrameTimeP95Ms = math.NaN()
	if err := project.Validate(fBad); err == nil {
		t.Fatal("Validate accepted NaN FrameTimeP95Ms")
	}
}

func TestNegativeZeroRoundTrip(t *testing.T) {
	const raw = `version: 1
name: Game
id: dev.example.game
appVersion: 1.0.0
window:
  width: 640
  height: 360
  minWidth: -0.0
input:
  actions:
    - name: tap
      bindings:
        - kind: touchRegion
          region: {x: -0.0, y: -0.0, width: 100, height: 100}
audio:
  master: -0.0
  buses:
    - name: sfx
      volume: -0.0
      effects:
        - gain: {factor: -0.0}
        - delay: {time: 200ms, feedback: -0.0, wet: -0.0}
        - reverb: {roomSize: -0.0, damping: -0.0, wet: -0.0}
        - lowPass: {cutoffHz: 1000, q: -0.0}
physics:
  gravity2D: {x: -0.0, y: -0.0}
  gravity3D: {x: -0.0, y: -0.0, z: -0.0}
export:
  budgets:
    downloadSizeMB: -0.0
    frameTimeP95Ms: -0.0
assets:
  audio:
    - in: raw/a.wav
      out: built/a.wav
      peakDB: -0.0
navigation:
  cellSize: -0.0
`
	parsed, err := project.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if math.Signbit(float64(*parsed.Audio.Master)) {
		t.Fatal("parsed Audio.Master kept negative zero")
	}
	if math.Signbit(float64(*parsed.Audio.Buses[0].Volume)) {
		t.Fatal("parsed Bus.Volume kept negative zero")
	}
	if math.Signbit(parsed.Audio.Buses[0].Effects[0].Gain.Factor) {
		t.Fatal("parsed Gain.Factor kept negative zero")
	}
	if math.Signbit(*parsed.Assets.Audio[0].PeakDB) {
		t.Fatal("parsed Assets.Audio[0].PeakDB kept negative zero")
	}

	first, err := project.Encode(parsed)
	if err != nil {
		t.Fatalf("Encode 1: %v", err)
	}
	if strings.Contains(string(first), "-0") {
		t.Fatalf("Encode wrote negative zero:\n%s", first)
	}
	again, err := project.Parse(first)
	if err != nil {
		t.Fatalf("Parse 2: %v\n%s", err, first)
	}
	second, err := project.Encode(again)
	if err != nil {
		t.Fatalf("Encode 2: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("round trip changed bytes:\n--- first\n%s\n--- second\n%s", first, second)
	}

	// Also test encoding a Go-constructed File carrying -0.0 directly without mutating caller.
	nz32 := float32(math.Copysign(0, -1))
	nz64 := math.Copysign(0, -1)
	inMem := &project.File{
		Version:    project.CurrentVersion,
		Name:       "Game",
		ID:         "dev.example.game",
		AppVersion: "1.0.0",
		Window:     project.Window{Width: 640, Height: 360},
		Audio: &project.Audio{
			Master: project.Float32(nz32),
			Buses: []project.Bus{{
				Name:    "sfx",
				Volume:  project.Float32(nz32),
				Effects: []project.Effect{{Gain: &project.Gain{Factor: nz64}}},
			}},
		},
	}
	mFirst, err := project.Encode(inMem)
	if err != nil {
		t.Fatalf("Encode in-memory: %v", err)
	}
	if strings.Contains(string(mFirst), "-0") {
		t.Fatalf("Encode in-memory wrote negative zero:\n%s", mFirst)
	}
	if !math.Signbit(float64(*inMem.Audio.Master)) || !math.Signbit(inMem.Audio.Buses[0].Effects[0].Gain.Factor) {
		t.Fatal("Encode mutated caller File")
	}
	mAgain, err := project.Parse(mFirst)
	if err != nil {
		t.Fatalf("Parse in-memory: %v", err)
	}
	mSecond, err := project.Encode(mAgain)
	if err != nil {
		t.Fatalf("Encode in-memory 2: %v", err)
	}
	if !bytes.Equal(mFirst, mSecond) {
		t.Fatalf("in-memory round trip changed bytes:\n--- first\n%s\n--- second\n%s", mFirst, mSecond)
	}
}

func TestActionAssistParseEncodeValidateAndBindInput(t *testing.T) {
	src := `version: 1
name: Assist Game
id: dev.example.assist
appVersion: 0.1.0
window:
  width: 960
  height: 540
input:
  actions:
    - name: sprint
      bindings:
        - kind: key
          key: shift
      assist: {toggle: true}
    - name: struggle
      bindings:
        - kind: key
          key: e
      assist: {toggle: true, mash: true, mashRate: 12}
    - name: pause
      bindings:
        - kind: key
          key: escape
`
	f, err := project.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse valid assist config: %v", err)
	}
	encoded, err := project.Encode(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(encoded) != src {
		t.Fatalf("Encode round-trip mismatch:\n--- got\n%s\n--- want\n%s", encoded, src)
	}

	// Clone preserves Assist independently.
	cp := f.Clone()
	cp.Input.Actions[0].Assist.Toggle = false
	if !f.Input.Actions[0].Assist.Toggle {
		t.Fatal("Clone aliased Action.Assist")
	}

	// BindInput registers declared ActionAssist and prevents undeclared menu actions from latching.
	a := input.NewActions()
	f.BindInput(a)
	if got := a.ActionAssist("sprint"); !got.Toggle || got.Mash {
		t.Fatalf("ActionAssist(sprint) = %+v, want Toggle=true Mash=false", got)
	}
	if got := a.ActionAssist("struggle"); !got.Toggle || !got.Mash || got.MashRate != 12 {
		t.Fatalf("ActionAssist(struggle) = %+v, want Toggle=true Mash=true MashRate=12", got)
	}
	if got := a.ActionAssist("pause"); got.Toggle || got.Mash {
		t.Fatalf("ActionAssist(pause) = %+v, want zero", got)
	}
	if !slices.Equal(a.ToggleableActions(), []string{"sprint", "struggle"}) {
		t.Fatalf("ToggleableActions = %v, want [sprint struggle]", a.ToggleableActions())
	}
	if !slices.Equal(a.MashableActions(), []string{"struggle"}) {
		t.Fatalf("MashableActions = %v, want [struggle]", a.MashableActions())
	}

	// Validation refuses mashRate without mash: true and negative mashRate.
	badNoMash := `version: 1
name: Bad
id: dev.example.bad
appVersion: 0.1.0
window: {width: 640, height: 360}
input:
  actions:
    - name: struggle
      assist: {mashRate: 12}
`
	if _, err := project.Parse([]byte(badNoMash)); err == nil || !strings.Contains(err.Error(), "requires mash: true") {
		t.Fatalf("expected error for mashRate without mash: true, got %v", err)
	}

	badNegRate := `version: 1
name: Bad
id: dev.example.bad
appVersion: 0.1.0
window: {width: 640, height: 360}
input:
  actions:
    - name: struggle
      assist: {mash: true, mashRate: -5}
`
	if _, err := project.Parse([]byte(badNegRate)); err == nil || !strings.Contains(err.Error(), "mashRate") {
		t.Fatalf("expected error for negative mashRate, got %v", err)
	}
}

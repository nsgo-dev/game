package project_test

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/core"
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
	cases := map[string]string{
		"empty":                  "",
		"version":                strings.Replace(head, "version: 1", "version: 2", 1),
		"unknown field":          head + "colour: red\n",
		"unknown nested field":   head + "display: {width: 1, height: 1, zoom: 2}\n",
		"trailing document":      head + "---\nversion: 1\n",
		"no name":                strings.Replace(head, "name: Game", "name: ''", 1),
		"name spaces":            strings.Replace(head, "name: Game", "name: ' Game'", 1),
		"bad id":                 strings.Replace(head, "dev.example.game", "game", 1),
		"id digit":               strings.Replace(head, "dev.example.game", "dev.1game", 1),
		"id char":                strings.Replace(head, "dev.example.game", "dev.ex ample", 1),
		"no app version":         strings.Replace(head, "appVersion: 1.0.0", "appVersion: ''", 1),
		"window size":            strings.Replace(head, "width: 640", "width: 0", 1),
		"window below minimum":   strings.Replace(head, "height: 360}", "height: 360, minHeight: 400}", 1),
		"window max below min":   strings.Replace(head, "height: 360}", "height: 360, minWidth: 100, maxWidth: 50}", 1),
		"window appearance":      strings.Replace(head, "height: 360}", "height: 360, appearance: dim}", 1),
		"window nan":             strings.Replace(head, "width: 640", "width: .nan", 1),
		"display size":           head + "display: {width: 320, height: -1}\n",
		"display mode":           head + "display: {width: 320, height: 180, mode: zoom}\n",
		"display bar color":      head + "display: {width: 320, height: 180, barColor: 'not a color'}\n",
		"action name":            head + "input: {actions: [{name: ''}]}\n",
		"action spaces":          head + "input: {actions: [{name: 'go left'}]}\n",
		"duplicate action":       head + "input: {actions: [{name: a}, {name: a}]}\n",
		"binding kind":           head + "input: {actions: [{name: a, bindings: [{kind: key}]}]}\n",
		"touch without region":   head + "input: {actions: [{name: a, bindings: [{kind: touchRegion}]}]}\n",
		"touch with button":      head + "input: {actions: [{name: a, bindings: [{kind: touchRegion, button: left, region: {width: 1, height: 1}}]}]}\n",
		"mouse with region":      head + "input: {actions: [{name: a, bindings: [{kind: mouseButton, button: left, region: {width: 1, height: 1}}]}]}\n",
		"mouse button":           head + "input: {actions: [{name: a, bindings: [{kind: mouseButton, button: thumb}]}]}\n",
		"region size":            head + "input: {actions: [{name: a, bindings: [{kind: mouseRegion, button: left, region: {width: 0, height: 1}}]}]}\n",
		"axis without actions":   head + "input: {axes: [{name: x}]}\n",
		"axis undeclared action": head + "input: {actions: [{name: a}], axes: [{name: x, negative: a, positive: b}]}\n",
		"duplicate axis":         head + "input: {actions: [{name: a}], axes: [{name: x, negative: a}, {name: x, positive: a}]}\n",
		"duplicate motion axis":  head + "input: {motionAxes: [{name: m, axis: tiltX}, {name: m, axis: tiltY}]}\n",
		"motion axis":            head + "input: {motionAxes: [{name: m, axis: tilt}]}\n",
		"motion dead zone":       head + "input: {motionAxes: [{name: m, axis: tiltX, deadZone: 1}]}\n",
		"motion sensitivity":     head + "input: {motionAxes: [{name: m, axis: gyroZ, sensitivity: -1}]}\n",
		"master":                 head + "audio: {master: 1.5}\n",
		"bus name":               head + "audio: {buses: [{name: ''}]}\n",
		"duplicate bus":          head + "audio: {buses: [{name: music}, {name: music}]}\n",
		"bus volume":             head + "audio: {buses: [{name: music, volume: -0.1}]}\n",
		"no effect":              head + "audio: {buses: [{name: music, effects: [{}]}]}\n",
		"two effects":            head + "audio: {buses: [{name: music, effects: [{gain: {factor: 1}, bitcrush: {bits: 4}}]}]}\n",
		"cutoff":                 head + "audio: {buses: [{name: music, effects: [{lowPass: {cutoffHz: 0}}]}]}\n",
		"q":                      head + "audio: {buses: [{name: music, effects: [{highPass: {cutoffHz: 10, q: -1}}]}]}\n",
		"delay time":             head + "audio: {buses: [{name: music, effects: [{delay: {time: 0s}}]}]}\n",
		"delay feedback":         head + "audio: {buses: [{name: music, effects: [{delay: {time: 1s, feedback: 1}}]}]}\n",
		"reverb wet":             head + "audio: {buses: [{name: music, effects: [{reverb: {wet: 2}}]}]}\n",
		"gain":                   head + "audio: {buses: [{name: music, effects: [{gain: {factor: -1}}]}]}\n",
		"bits":                   head + "audio: {buses: [{name: music, effects: [{bitcrush: {bits: 16}}]}]}\n",
		"downsample":             head + "audio: {buses: [{name: music, effects: [{bitcrush: {bits: 4, downsample: -1}}]}]}\n",
		"scene name":             head + "autoload: {scenes: [{name: '', path: a.yaml}]}\n",
		"duplicate scene":        head + "autoload: {scenes: [{name: a, path: a.yaml}, {name: a, path: b.yaml}]}\n",
		"scene extension":        head + "autoload: {scenes: [{name: a, path: a.json}]}\n",
		"absolute path":          head + "autoload: {resources: [{name: a, path: /etc/passwd}]}\n",
		"escaping path":          head + "autoload: {resources: [{name: a, path: ../secret.txt}]}\n",
		"backslash path":         head + "autoload: {resources: [{name: a, path: 'data\\a.csv'}]}\n",
		"dot path":               head + "autoload: {resources: [{name: a, path: .}]}\n",
		"main scene":             head + "autoload: {mainScene: b, scenes: [{name: a, path: a.yaml}]}\n",
		"output":                 head + "export: {output: ../dist}\n",
		"platform":               head + "export: {presets: [{platform: amiga}]}\n",
		"duplicate platform":     head + "export: {presets: [{platform: web}, {platform: web}]}\n",
		"foreign target":         head + "export: {presets: [{platform: macos, targets: [linux_x64]}]}\n",
		"unknown target":         head + "export: {presets: [{platform: macos, targets: [macos_ppc]}]}\n",
		"duplicate target":       head + "export: {presets: [{platform: macos, targets: [macos_arm64, macos_arm64]}]}\n",
		"plugin name":            head + "editor: {plugins: ['']}\n",
		"plugin spaces":          head + "editor: {plugins: ['level tools']}\n",
		"duplicate plugin":       head + "editor: {plugins: [health, health]}\n",
		"unknown editor field":   head + "editor: {plugins: [health], theme: dark}\n",
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
		project.FileName:     {Data: golden},
		project.NSGoFileName: {Data: nsgo},
		"scenes/title.yaml":  {Data: []byte("version: 1\nname: Title\nlayers:\n  - name: main\n")},
		"data/enemies.csv":   {Data: []byte("name,hp\n")},
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

	// A stale nsgo.yaml and a missing resource are both reported, and the file still comes back.
	bad := fstest.MapFS{
		project.FileName:     {Data: golden},
		project.NSGoFileName: {Data: []byte("name: Old Name\n")},
		"scenes/title.yaml":  good["scenes/title.yaml"],
	}
	got, err := project.Check(bad)
	if got == nil || err == nil || !strings.Contains(err.Error(), "name is \"Old Name\"") || !strings.Contains(err.Error(), "enemies") {
		t.Fatalf("Check of a bad project = %v, %v", got, err)
	}

	if _, err := project.Check(fstest.MapFS{project.FileName: {Data: []byte("version: 2\n")}}); err == nil {
		t.Fatal("Check accepted an invalid game.yaml")
	}
	if _, err := project.Check(fstest.MapFS{}); err == nil {
		t.Fatal("Check accepted a project without game.yaml")
	}
}

package quality_test

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/quality"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestLevelNamesRoundTrip(t *testing.T) {
	for _, l := range quality.Levels() {
		text, err := l.MarshalText()
		if err != nil {
			t.Fatalf("%v: MarshalText: %v", l, err)
		}
		var back quality.Level
		if err := back.UnmarshalText(text); err != nil || back != l {
			t.Fatalf("%q read back as %v, %v", text, back, err)
		}
	}
	if l, err := quality.ParseLevel("  High "); err != nil || l != quality.High {
		t.Fatalf("ParseLevel(\"  High \") = %v, %v", l, err)
	}
	for _, bad := range []string{"", "ultra", "1"} {
		if _, err := quality.ParseLevel(bad); err == nil {
			t.Fatalf("ParseLevel(%q) did not fail", bad)
		}
	}
	if _, err := quality.Level(7).MarshalText(); err == nil {
		t.Fatal("MarshalText of an invalid level did not fail")
	}
}

func TestNormalizeSetting(t *testing.T) {
	for in, want := range map[string]string{"": "auto", " AUTO ": "auto", "low": "low", "Medium": "medium", "high\n": "high"} {
		got, err := quality.NormalizeSetting(in)
		if err != nil || got != want {
			t.Fatalf("NormalizeSetting(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	if _, err := quality.NormalizeSetting("ultra"); err == nil {
		t.Fatal("NormalizeSetting(\"ultra\") did not fail")
	}
}

func TestPresetScalesCounts(t *testing.T) {
	low := quality.DefaultPreset(quality.Low)
	if got := low.Particles(512); got != 128 {
		t.Fatalf("low Particles(512) = %d, want 128", got)
	}
	if got := low.Particles(1); got != 1 {
		t.Fatalf("low Particles(1) = %d, want at least 1", got)
	}
	if got := low.Particles(0); got != 0 {
		t.Fatalf("low Particles(0) = %d, want 0", got)
	}
	if got := low.Scale(256); got != 128 {
		t.Fatalf("low Scale(256) = %d, want 128", got)
	}
	if got := low.ScaleSize(core.Size{Width: 640, Height: 360}); got != (core.Size{Width: 320, Height: 180}) {
		t.Fatalf("low ScaleSize = %v", got)
	}
	high := quality.DefaultPreset(quality.High)
	if high.Particles(512) != 512 || high.Scale(256) != 256 {
		t.Fatal("high preset scaled counts")
	}
	var zero quality.Preset
	if zero.Particles(40) != 40 || zero.Scale(40) != 40 {
		t.Fatal("an invalid preset scaled counts instead of leaving them")
	}
}

func TestDefaultPresetsGrowFromLowToHigh(t *testing.T) {
	low, med, high := quality.DefaultPreset(quality.Low), quality.DefaultPreset(quality.Medium), quality.DefaultPreset(quality.High)
	if !(low.ResolutionScale < med.ResolutionScale && med.ResolutionScale < high.ResolutionScale) {
		t.Fatal("resolution scales do not grow")
	}
	if !(low.ParticleScale < med.ParticleScale && med.ParticleScale < high.ParticleScale) {
		t.Fatal("particle scales do not grow")
	}
	if low.Effects || low.Shadows || !med.Effects || med.Shadows || !high.Effects || !high.Shadows {
		t.Fatalf("effects and shadows: low %+v, medium %+v, high %+v", low, med, high)
	}
	if quality.DefaultPreset(quality.Level(9)) != med {
		t.Fatal("an invalid level does not fall back to medium")
	}
}

const defaultYAML = `version: 1
presets:
  low:
    resolution_scale: 0.5
    particle_scale: 0.25
    effects: false
    shadows: false
  medium:
    resolution_scale: 0.75
    particle_scale: 0.6
    effects: true
    shadows: false
  high:
    resolution_scale: 1
    particle_scale: 1
    effects: true
    shadows: true
`

func TestDefaultConfigEncodesDeterministically(t *testing.T) {
	for range 3 {
		data, err := quality.EncodeConfig(quality.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != defaultYAML {
			t.Fatalf("EncodeConfig(DefaultConfig()) =\n%s\nwant\n%s", data, defaultYAML)
		}
	}
	c, err := quality.ParseConfig([]byte(defaultYAML))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, quality.DefaultConfig()) {
		t.Fatalf("parsed %+v, want DefaultConfig", c)
	}
}

func TestConfigFileRoundTripsUnchanged(t *testing.T) {
	data, err := os.ReadFile("testdata/quality.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c, err := quality.ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Devices) != 2 || c.Devices[0].Level != quality.High || c.Devices[1].OS != core.OSAndroid || c.Adapt.WindowFrames != 120 {
		t.Fatalf("parsed %+v", c)
	}
	out, err := quality.EncodeConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(data) {
		t.Fatalf("round trip changed the file:\n%s\nwant\n%s", out, data)
	}
}

func TestParseConfigRefusesBadDocuments(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"no version":      strings.Replace(defaultYAML, "version: 1\n", "", 1),
		"future version":  strings.Replace(defaultYAML, "version: 1", "version: 2", 1),
		"unknown field":   defaultYAML + "bloom: true\n",
		"unknown preset":  defaultYAML + "  ultra:\n    resolution_scale: 1\n",
		"missing preset":  strings.SplitAfter(defaultYAML, "shadows: false\n")[0] + "",
		"zero scale":      strings.Replace(defaultYAML, "resolution_scale: 0.5", "resolution_scale: 0", 1),
		"scale above one": strings.Replace(defaultYAML, "particle_scale: 1", "particle_scale: 1.5", 1),
		"nan scale":       strings.Replace(defaultYAML, "particle_scale: 1", "particle_scale: .nan", 1),
		"trailing doc":    defaultYAML + "---\nversion: 1\n",
		"numeric level":   defaultYAML + "devices:\n  - model: X\n    level: 2\n",
		"unknown level":   defaultYAML + "devices:\n  - model: X\n    level: ultra\n",
		"empty model":     defaultYAML + "devices:\n  - model: \" \"\n    level: low\n",
		"unknown os":      defaultYAML + "devices:\n  - model: X\n    os: amiga\n    level: low\n",
		"duplicate model": defaultYAML + "devices:\n  - model: Pixel\n    level: low\n  - model: pixel\n    level: high\n",
		"target fps":      defaultYAML + "adapt:\n  target_fps: 500\n",
		"window":          defaultYAML + "adapt:\n  window_frames: 3\n",
		"upgrades":        defaultYAML + "adapt:\n  upgrade_windows: -1\n",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := quality.ParseConfig([]byte(doc)); err == nil {
				t.Fatalf("ParseConfig accepted:\n%s", doc)
			}
		})
	}
	bad := quality.DefaultConfig()
	bad.Presets.High.ResolutionScale = 2
	if _, err := quality.EncodeConfig(bad); err == nil {
		t.Fatal("EncodeConfig accepted an invalid config")
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		d    quality.Device
		want quality.Level
	}{
		{"low-end Android phone", quality.Device{OS: core.OSAndroid, Idiom: core.IdiomPhone, Model: "Samsung SM-A045F", CPUs: 8, OSVersion: core.OSVersion{Major: 33}}, quality.Low},
		{"Android flagship", quality.Device{OS: core.OSAndroid, Idiom: core.IdiomPhone, Model: "Google Pixel 8", CPUs: 9, OSVersion: core.OSVersion{Major: 35}}, quality.Low},
		{"Android tablet", quality.Device{OS: core.OSAndroid, Idiom: core.IdiomTablet, CPUs: 8}, quality.Low},
		{"iPhone 14 Pro", quality.Device{OS: core.OSIOS, Model: "iPhone15,2"}, quality.High},
		{"iPhone 13", quality.Device{OS: core.OSIOS, Model: "iPhone14,5"}, quality.High},
		{"iPhone 11", quality.Device{OS: core.OSIOS, Model: "iPhone12,1"}, quality.Medium},
		{"iPhone X", quality.Device{OS: core.OSIOS, Model: "iPhone10,3"}, quality.Low},
		{"iPad Pro M1", quality.Device{OS: core.OSIOS, Idiom: core.IdiomTablet, Model: "iPad13,8"}, quality.High},
		{"iPad 8th gen", quality.Device{OS: core.OSIOS, Idiom: core.IdiomTablet, Model: "iPad11,6"}, quality.Medium},
		{"old iPad", quality.Device{OS: core.OSIOS, Idiom: core.IdiomTablet, Model: "iPad7,5"}, quality.Low},
		{"simulator", quality.Device{OS: core.OSIOS, Model: "arm64"}, quality.Medium},
		{"malformed model", quality.Device{OS: core.OSIOS, Model: "iPhoneX,1"}, quality.Medium},
		{"Apple TV", quality.Device{OS: core.OSIOS, Idiom: core.IdiomTV, Model: "AppleTV14,1"}, quality.Low},
		{"workstation", quality.Device{OS: core.OSLinux, Idiom: core.IdiomDesktop, CPUs: 16}, quality.High},
		{"laptop", quality.Device{OS: core.OSWindows, Idiom: core.IdiomDesktop, CPUs: 4}, quality.Medium},
		{"netbook", quality.Device{OS: core.OSWindows, Idiom: core.IdiomDesktop, CPUs: 2}, quality.Low},
		{"Mac", quality.Device{OS: core.OSMacOS, Idiom: core.IdiomMac, Model: "Mac15,7", CPUs: 12}, quality.High},
		{"desktop without CPU count", quality.Device{OS: core.OSLinux, Idiom: core.IdiomDesktop}, quality.Medium},
		{"web", quality.Device{OS: core.OSWeb, Idiom: core.IdiomDesktop, CPUs: 16}, quality.Medium},
		{"unknown", quality.Device{}, quality.Medium},
	}
	for _, tc := range cases {
		if got := quality.Classify(tc.d); got != tc.want {
			t.Errorf("%s: Classify = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDeviceRulesComeBeforeTheHeuristic(t *testing.T) {
	c := quality.DefaultConfig()
	c.Devices = []quality.DeviceRule{
		{Model: "google pixel 8", OS: core.OSAndroid, Level: quality.High},
		{Model: "Google Pixel", Level: quality.Medium},
	}
	pixel8 := quality.Device{OS: core.OSAndroid, Model: "Google Pixel 8 Pro"}
	if got := c.StartLevel(pixel8); got != quality.High {
		t.Fatalf("Pixel 8 Pro starts at %v, want high from the first rule", got)
	}
	if got := c.StartLevel(quality.Device{OS: core.OSAndroid, Model: "Google Pixel 6a"}); got != quality.Medium {
		t.Fatalf("Pixel 6a starts at %v, want medium from the second rule", got)
	}
	if got := c.StartLevel(quality.Device{OS: core.OSIOS, Model: "Google Pixel 8"}); got != quality.Medium {
		t.Fatalf("an iOS device named like a Pixel 8 starts at %v, want medium from the rule without an OS", got)
	}
	if got := c.StartLevel(quality.Device{OS: core.OSAndroid, Model: "Google"}); got != quality.Low {
		t.Fatalf("a shorter model starts at %v, want the heuristic's low", got)
	}
}

func TestDeviceFromEnvReadsALowEndAndroidPhone(t *testing.T) {
	var d quality.Device
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		d = quality.DeviceFromEnv(cx.Env())
		return core.View{}
	}, nsgotest.Environment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone, DeviceModel: "Samsung SM-A045F", OSVersion: core.OSVersion{Major: 33}}))
	if d.OS != core.OSAndroid || d.Model != "Samsung SM-A045F" || d.OSVersion.Major != 33 || d.CPUs < 1 {
		t.Fatalf("DeviceFromEnv = %+v", d)
	}
	sel, err := quality.NewSelector(quality.DefaultConfig(), d)
	if err != nil {
		t.Fatal(err)
	}
	if sel.Level() != quality.Low || sel.Preset() != quality.DefaultPreset(quality.Low) || !sel.Auto() {
		t.Fatalf("a low-end Android phone starts at %v (auto %v), want low", sel.Level(), sel.Auto())
	}
}

// feed observes n frames of length dt and returns how many changed the level.
func feed(s *quality.Selector, n int, dt time.Duration) int {
	changes := 0
	for range n {
		if s.Observe(dt) {
			changes++
		}
	}
	return changes
}

const (
	at60 = time.Second / 60
	at30 = time.Second / 30
	at20 = time.Second / 20
)

func newSelector(t *testing.T, start quality.Level) *quality.Selector {
	t.Helper()
	c := quality.DefaultConfig()
	c.Devices = []quality.DeviceRule{{Model: "Test", Level: start}}
	s, err := quality.NewSelector(c, quality.Device{Model: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Level() != start {
		t.Fatalf("started at %v, want %v", s.Level(), start)
	}
	return s
}

func TestSelectorStepsDownWhenFramesRunSlow(t *testing.T) {
	s := newSelector(t, quality.High)
	var changes []quality.Level
	s.OnChange = func(l quality.Level, p quality.Preset) {
		if p != quality.DefaultPreset(l) {
			t.Errorf("OnChange(%v) got preset %+v", l, p)
		}
		changes = append(changes, l)
	}
	// 30 warm-up frames, then a 90-frame window at 20 fps.
	if n := feed(s, 30+89, at20); n != 0 {
		t.Fatalf("changed %d times before the window ended", n)
	}
	if !s.Observe(at20) || s.Level() != quality.Medium {
		t.Fatalf("level %v after a slow window, want medium", s.Level())
	}
	feed(s, 120, at20)
	if s.Level() != quality.Low {
		t.Fatalf("level %v after another slow window, want low", s.Level())
	}
	if n := feed(s, 1000, at20); n != 0 || s.Level() != quality.Low {
		t.Fatalf("stepped %d times below low", n)
	}
	if !reflect.DeepEqual(changes, []quality.Level{quality.Medium, quality.Low}) {
		t.Fatalf("OnChange saw %v", changes)
	}
}

func TestSelectorHoldsAtTheTargetFrameRate(t *testing.T) {
	s := newSelector(t, quality.Medium)
	// A display paced at exactly 30 fps, jittering a little either way, is neither slow nor fast.
	for i := range 3000 {
		dt := at30 + time.Duration(i%3-1)*time.Millisecond
		if s.Observe(dt) {
			t.Fatalf("frame %d changed the level to %v", i, s.Level())
		}
	}
	// A few slow frames in each window are not enough.
	for i := range 3000 {
		dt := at30
		if i%10 == 0 {
			dt = at20
		}
		if s.Observe(dt) {
			t.Fatalf("frame %d changed the level to %v", i, s.Level())
		}
	}
}

func TestSelectorStepsUpWithHeadroomAndSettles(t *testing.T) {
	s := newSelector(t, quality.Low)
	// Warm-up and four fast windows.
	feed(s, 30+4*90-1, at60)
	if s.Level() != quality.Low {
		t.Fatalf("stepped up early to %v", s.Level())
	}
	if !s.Observe(at60) || s.Level() != quality.Medium {
		t.Fatalf("level %v after four fast windows, want medium", s.Level())
	}
	// Medium runs fast too, so it reaches high.
	feed(s, 30+4*90, at60)
	if s.Level() != quality.High {
		t.Fatalf("level %v, want high", s.Level())
	}
	// High is too slow: back to medium, and never up to high again.
	feed(s, 30+90, at20)
	if s.Level() != quality.Medium {
		t.Fatalf("level %v after a slow window at high, want medium", s.Level())
	}
	if n := feed(s, 100*90, at60); n != 0 || s.Level() != quality.Medium {
		t.Fatalf("went back up %d times to %v", n, s.Level())
	}
}

func TestSelectorIgnoresHitches(t *testing.T) {
	s := newSelector(t, quality.High)
	feed(s, 30, at60)
	for range 1000 {
		s.Observe(2 * time.Second)
		s.Observe(0)
		s.Observe(-time.Millisecond)
	}
	if s.Level() != quality.High {
		t.Fatalf("hitches moved the level to %v", s.Level())
	}
}

func TestPlayerSettingOverridesAuto(t *testing.T) {
	s := newSelector(t, quality.Medium)
	var changes []quality.Level
	s.OnChange = func(l quality.Level, _ quality.Preset) { changes = append(changes, l) }
	if err := s.SetSetting("High"); err != nil {
		t.Fatal(err)
	}
	if s.Level() != quality.High || s.Auto() || s.Setting() != "high" {
		t.Fatalf("after setting high: level %v, auto %v, setting %q", s.Level(), s.Auto(), s.Setting())
	}
	if n := feed(s, 5000, at20); n != 0 || s.Level() != quality.High {
		t.Fatalf("frame times moved a fixed level %d times", n)
	}
	if err := s.SetSetting("ultra"); err == nil || s.Level() != quality.High || s.Auto() {
		t.Fatalf("an unknown setting: err %v, level %v, auto %v", err, s.Level(), s.Auto())
	}
	if err := s.SetSetting("auto"); err != nil {
		t.Fatal(err)
	}
	if s.Level() != quality.Medium || !s.Auto() || s.Setting() != "auto" || s.StartLevel() != quality.Medium {
		t.Fatalf("after auto: level %v, auto %v, setting %q", s.Level(), s.Auto(), s.Setting())
	}
	feed(s, 30+90, at20)
	if s.Level() != quality.Low {
		t.Fatalf("auto did not adapt again: %v", s.Level())
	}
	if !reflect.DeepEqual(changes, []quality.Level{quality.High, quality.Medium, quality.Low}) {
		t.Fatalf("OnChange saw %v", changes)
	}
}

func TestAdaptOffKeepsTheStartingLevel(t *testing.T) {
	c := quality.DefaultConfig()
	c.Adapt.Off = true
	s, err := quality.NewSelector(c, quality.Device{OS: core.OSLinux, CPUs: 16})
	if err != nil {
		t.Fatal(err)
	}
	if n := feed(s, 5000, at20); n != 0 || s.Level() != quality.High {
		t.Fatalf("Adapt.Off moved the level %d times to %v", n, s.Level())
	}
}

func TestCustomAdaptTarget(t *testing.T) {
	c := quality.DefaultConfig()
	c.Adapt = quality.Adapt{TargetFPS: 60, WindowFrames: 10, UpgradeWindows: 1}
	c.Devices = []quality.DeviceRule{{Model: "T", Level: quality.High}}
	s, err := quality.NewSelector(c, quality.Device{Model: "T"})
	if err != nil {
		t.Fatal(err)
	}
	// 40 fps meets the default 30 but not a 60 fps target.
	feed(s, 30+10, time.Second/40)
	if s.Level() != quality.Medium {
		t.Fatalf("level %v at 40 fps with a 60 fps target, want medium", s.Level())
	}
}

func TestNewSelectorRefusesAnInvalidConfig(t *testing.T) {
	if _, err := quality.NewSelector(quality.Config{}, quality.Device{}); err == nil {
		t.Fatal("NewSelector accepted a zero Config")
	}
}

func BenchmarkSelectorObserve(b *testing.B) {
	s, err := quality.NewSelector(quality.DefaultConfig(), quality.Device{OS: core.OSAndroid})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		s.Observe(at30 + time.Duration(i%5)*time.Millisecond)
	}
}

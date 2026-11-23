package resource_test

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/gameui"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

var testdataKinds = map[string]resource.Kind{
	"stone.material.yaml":          resource.KindMaterial,
	"jump.curve.yaml":              resource.KindCurve,
	"sunset.gradient.yaml":         resource.KindGradient,
	"torch_flicker.animation.yaml": resource.KindAnimation,
	"menu.theme.yaml":              resource.KindTheme,
	"default.buses.yaml":           resource.KindBusLayout,
	"ice.physics.yaml":             resource.KindPhysicsMaterial,
}

func readTestdata(tb testing.TB, name string) []byte {
	tb.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		tb.Fatalf("read testdata %q: %v", name, err)
	}
	return data
}

func TestTestdataRoundTripsUnchanged(t *testing.T) {
	if len(testdataKinds) != len(resource.Kinds()) {
		t.Fatalf("testdata covers %d kinds, want all %d", len(testdataKinds), len(resource.Kinds()))
	}
	for name, kind := range testdataKinds {
		t.Run(name, func(t *testing.T) {
			raw := readTestdata(t, name)
			r, err := resource.Parse(raw)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if r.Kind() != kind {
				t.Fatalf("Kind = %q, want %q", r.Kind(), kind)
			}
			out, err := resource.Encode(r)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if !bytes.Equal(out, raw) {
				t.Fatalf("round trip changed the file:\n--- want\n%s\n--- got\n%s", raw, out)
			}
		})
	}
}

func TestParseRejectsInvalidDocuments(t *testing.T) {
	cases := []struct {
		name, doc, want string
	}{
		{"empty", "  \n", "empty document"},
		{"missing type", "version: 1\n", "missing type"},
		{"unknown type", "type: shader\nversion: 1\n", "unknown type"},
		{"future version", "type: material\nversion: 2\n", "unsupported version 2"},
		{"missing version", "type: material\n", "unsupported version 0"},
		{"unknown field", "type: material\nversion: 1\nshininess: 3\n", "field shininess not found"},
		{"field of another type", "type: material\nversion: 1\nkeys: []\n", "field keys not found"},
		{"trailing document", "type: material\nversion: 1\n---\ntype: material\nversion: 1\n", "trailing"},
		{"bad base color", "type: material\nversion: 1\nbaseColor: notacolor\n", "baseColor"},
		{"opacity out of range", "type: material\nversion: 1\nopacity: 1.5\n", "opacity"},
		{"unknown blend", "type: material\nversion: 1\nblend: glow\n", "blend"},
		{"absolute texture", "type: material\nversion: 1\ntexture: /etc/stone.png\n", "texture"},
		{"dot texture", "type: material\nversion: 1\ntexture: .\n", "texture"},
		{"curve without keys", "type: curve\nversion: 1\n", "at least one key"},
		{"curve keys out of order", "type: curve\nversion: 1\nkeys:\n  - {time: 1, value: 0}\n  - {time: 1, value: 1}\n", "after the previous"},
		{"curve unknown interp", "type: curve\nversion: 1\nkeys:\n  - {time: 0, value: 0, interp: cubic}\n", "interp"},
		{"gradient out of order", "type: gradient\nversion: 1\nstops:\n  - {offset: 0.5, color: red}\n  - {offset: 0.2, color: blue}\n", "before the previous"},
		{"gradient bad color", "type: gradient\nversion: 1\nstops:\n  - {offset: 0, color: nope}\n", "color"},
		{"animation without length", "type: animation\nversion: 1\n", "length"},
		{"animation key past length", "type: animation\nversion: 1\nlength: 1\ntracks:\n  - property: x\n    keys:\n      - {time: 2, value: 0}\n", "must be in [0, 1]"},
		{"animation duplicate track", "type: animation\nversion: 1\nlength: 1\ntracks:\n  - {property: x, keys: [{time: 0, value: 0}]}\n  - {property: x, keys: [{time: 0, value: 1}]}\n", "two tracks"},
		{"animation empty property", "type: animation\nversion: 1\nlength: 1\ntracks:\n  - {keys: [{time: 0, value: 0}]}\n", "empty property"},
		{"theme bad color", "type: theme\nversion: 1\ncolors:\n  accent: nope\n", "accent"},
		{"theme bad weight", "type: theme\nversion: 1\nfont:\n  weight: heavy\n", "weight"},
		{"theme negative padding", "type: theme\nversion: 1\npadding: -1\n", "padding"},
		{"bus duplicate", "type: busLayout\nversion: 1\nbuses:\n  - name: music\n  - name: music\n", "duplicate bus"},
		{"bus empty name", "type: busLayout\nversion: 1\nbuses:\n  - effects: []\n", "empty name"},
		{"bus effect with two kinds", "type: busLayout\nversion: 1\nbuses:\n  - name: music\n    effects:\n      - {gain: {factor: 1}, lowPass: {cutoffHz: 100}}\n", "exactly one"},
		{"bus effect with none", "type: busLayout\nversion: 1\nbuses:\n  - name: music\n    effects:\n      - {}\n", "exactly one"},
		{"bus bitcrush bits", "type: busLayout\nversion: 1\nbuses:\n  - name: music\n    effects:\n      - {bitcrush: {bits: 16}}\n", "bits"},
		{"bus filter cutoff", "type: busLayout\nversion: 1\nbuses:\n  - name: music\n    effects:\n      - {highPass: {cutoffHz: 0}}\n", "cutoffHz"},
		{"physics restitution", "type: physicsMaterial\nversion: 1\nrestitution: 1.5\n", "restitution"},
		{"physics negative friction", "type: physicsMaterial\nversion: 1\nfriction: -0.1\n", "friction"},
		{"not a mapping", "- type: material\n", "decode header"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resource.Parse([]byte(tc.doc))
			if err == nil {
				t.Fatalf("Parse accepted %q", tc.doc)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestParseAsRefusesAnotherType(t *testing.T) {
	if _, err := resource.ParseAs[*resource.Curve](readTestdata(t, "stone.material.yaml")); err == nil || !strings.Contains(err.Error(), "is a material, want curve") {
		t.Fatalf("ParseAs error = %v", err)
	}
	m, err := resource.ParseAs[*resource.Material](readTestdata(t, "stone.material.yaml"))
	if err != nil || m.Name != "Stone" {
		t.Fatalf("ParseAs = %+v, %v", m, err)
	}
}

func TestEncodeWritesHeaderFirstAndIsDeterministic(t *testing.T) {
	op := float32(0.5)
	m := &resource.Material{Header: resource.NewHeader(resource.KindMaterial), BaseColor: "red", Opacity: &op, Blend: "screen"}
	first, err := resource.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	want := "type: material\nversion: 1\nbaseColor: red\nopacity: 0.5\nblend: screen\n"
	if string(first) != want {
		t.Fatalf("Encode =\n%s\nwant\n%s", first, want)
	}
	for range 5 {
		again, err := resource.Encode(m)
		if err != nil || !bytes.Equal(again, first) {
			t.Fatalf("Encode not deterministic: %q, %v", again, err)
		}
	}
	if _, err := resource.Encode(&resource.Material{}); err == nil {
		t.Fatal("Encode accepted a material without a header")
	}
	if _, err := resource.Encode(&resource.Curve{Header: resource.NewHeader(resource.KindMaterial), Keys: []resource.Key{{}}}); err == nil {
		t.Fatal("Encode accepted a curve with a material header")
	}
	if _, err := resource.Encode(nil); err == nil {
		t.Fatal("Encode accepted nil")
	}
}

func TestNewCoversEveryKind(t *testing.T) {
	for _, k := range resource.Kinds() {
		r, err := resource.New(k)
		if err != nil {
			t.Fatalf("New(%q): %v", k, err)
		}
		if r.Kind() != k {
			t.Fatalf("New(%q).Kind() = %q", k, r.Kind())
		}
	}
	if _, err := resource.New("shader"); err == nil {
		t.Fatal("New accepted an unknown kind")
	}
}

func TestMaterialAccessors(t *testing.T) {
	m, err := resource.ParseAs[*resource.Material](readTestdata(t, "stone.material.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.EffectiveOpacity(); got != 0.75 {
		t.Fatalf("EffectiveOpacity = %v", got)
	}
	if got := m.BlendOp(); got != core.CompositeMultiply {
		t.Fatalf("BlendOp = %v", got)
	}
	if got := m.Color(); got != core.RGB(0x8a8f99) {
		t.Fatalf("Color = %#x", uint32(got))
	}
	if got := m.EffectiveRoughness(); got != 0.9 {
		t.Fatalf("EffectiveRoughness = %v", got)
	}
	if _, ok := m.EmissiveColor(); ok {
		t.Fatal("EmissiveColor reported a color for a material without one")
	}
	var zero resource.Material
	if zero.EffectiveOpacity() != 1 || zero.BlendOp() != core.CompositeSourceOver || zero.Color() != core.RGB(0xffffff) || zero.EffectiveRoughness() != 1 {
		t.Fatal("zero material defaults are wrong")
	}
	names := resource.BlendModes()
	if len(names) != 26 || names[0] != "color" {
		t.Fatalf("BlendModes = %v", names)
	}
}

func TestCurveEval(t *testing.T) {
	c, err := resource.ParseAs[*resource.Curve](readTestdata(t, "jump.curve.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ t, want float32 }{
		{-1, 0},
		{0, 0},
		{0.25, 0.5}, // smoothstep(0.5) = 0.5
		{0.125, 0.15625},
		{0.5, 1},
		{0.75, 0.5},
		{1, 0},
		{2, 0},
		{float32(math.NaN()), 0},
	}
	for _, tc := range cases {
		if got := c.Eval(tc.t); math.Abs(float64(got-tc.want)) > 1e-6 {
			t.Fatalf("Eval(%v) = %v, want %v", tc.t, got, tc.want)
		}
	}
	step := &resource.Curve{Header: resource.NewHeader(resource.KindCurve), Keys: []resource.Key{{Time: 0, Value: 2, Interp: resource.InterpStep}, {Time: 1, Value: 4}}}
	if got := step.Eval(0.99); got != 2 {
		t.Fatalf("step Eval = %v, want 2", got)
	}
}

func TestAnimationSample(t *testing.T) {
	a, err := resource.ParseAs[*resource.Animation](readTestdata(t, "torch_flicker.animation.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := a.ClipTime(2); math.Abs(float64(got-0.5)) > 1e-6 {
		t.Fatalf("ClipTime(2) = %v, want 0.5", got)
	}
	if got := a.ClipTime(-0.5); math.Abs(float64(got-1)) > 1e-6 {
		t.Fatalf("ClipTime(-0.5) = %v, want 1", got)
	}
	type sample struct {
		target, property string
		value            float32
	}
	var got []sample
	a.Sample(0.75+1.5, func(target, property string, v float32) { got = append(got, sample{target, property, v}) })
	if len(got) != 2 || got[0].target != "flame" || got[0].property != "alpha" || math.Abs(float64(got[0].value-0.6)) > 1e-6 || got[1].target != "" || got[1].property != "rotation" {
		t.Fatalf("Sample = %+v", got)
	}
	once := *a
	once.Loop = false
	if got := once.ClipTime(5); got != 1.5 {
		t.Fatalf("non-looping ClipTime(5) = %v, want 1.5", got)
	}
}

func TestGradientAt(t *testing.T) {
	g, err := resource.ParseAs[*resource.Gradient](readTestdata(t, "sunset.gradient.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := g.At(-1); got != core.RGB(0xffcc66) {
		t.Fatalf("At(-1) = %#x", uint32(got))
	}
	if got := g.At(0.5); got != core.RGB(0xff6633) {
		t.Fatalf("At(0.5) = %#x", uint32(got))
	}
	if got := g.At(2); got != core.RGB(0x331a4d) {
		t.Fatalf("At(2) = %#x", uint32(got))
	}
	two := &resource.Gradient{Header: resource.NewHeader(resource.KindGradient), Stops: []resource.GradientStop{{Offset: 0, Color: "#000000"}, {Offset: 1, Color: "rgba(255, 255, 255, 0)"}}}
	if got := two.At(0.5); got != core.Color(0x80808080) {
		t.Fatalf("At(0.5) = %#x, want 0x80808080", uint32(got))
	}
	stops := g.CoreStops()
	if len(stops) != 3 || stops[1].Offset != 0.5 || stops[1].Color != core.RGB(0xff6633) {
		t.Fatalf("CoreStops = %+v", stops)
	}
}

func TestThemeApplyOverridesOnlySetFields(t *testing.T) {
	th, err := resource.ParseAs[*resource.Theme](readTestdata(t, "menu.theme.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	base := gameui.DefaultTheme()
	base.Font.Italic = true
	got := th.Apply(base)
	if got.PanelBackground != core.RGB(0x10141c) || got.Accent != core.RGB(0xf97316) || got.Text != core.RGB(0xfafafa) {
		t.Fatal("Apply did not set the theme's colors")
	}
	if got.Hover != base.Hover || got.BorderWidth != base.BorderWidth {
		t.Fatal("Apply changed fields the theme leaves out")
	}
	if got.Padding != 10 || got.CornerRadius != 4 {
		t.Fatalf("Apply sizes = %v, %v", got.Padding, got.CornerRadius)
	}
	if got.Font.Family != "Inter" || got.Font.Size != 18 || got.Font.Weight != core.FontWeightSemibold || got.Font.Italic {
		t.Fatalf("Apply font = %+v", got.Font)
	}
}

func TestBusLayoutApplySetsEffectChains(t *testing.T) {
	bl, err := resource.ParseAs[*resource.BusLayout](readTestdata(t, "default.buses.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var mixer *sound.Mixer
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mixer = sound.NewMixer(cx, "sound")
		return core.View{}
	})
	var music, effects, ui int
	app.Post(func() {
		mixer.SetGroupEffects("ui", sound.Gain(2))
		bl.Apply(mixer)
		music = len(mixer.GroupEffects("music"))
		effects = len(mixer.GroupEffects("effects"))
		ui = len(mixer.GroupEffects("ui"))
	})
	app.Settle()
	if music != 1 || effects != 4 || ui != 0 {
		t.Fatalf("chains music=%d effects=%d ui=%d, want 1, 4, 0", music, effects, ui)
	}

	// A gain of 0.5 on the bus halves a sample exactly.
	half := &resource.Bus{Name: "x", Effects: []resource.BusEffect{{Gain: &resource.GainEffect{Factor: 0.5}}}}
	samples := []float64{0.8, -0.4}
	sound.ProcessSamples(samples, 44100, 1, half.Chain()...)
	if samples[0] != 0.4 || samples[1] != -0.2 {
		t.Fatalf("gain chain produced %v", samples)
	}
}

func TestPhysicsMaterialApplyTo(t *testing.T) {
	pm, err := resource.ParseAs[*resource.PhysicsMaterial](readTestdata(t, "ice.physics.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	body := physics.NewRigidBox(0, 0, 10, 10, 1)
	body.AngularDamping = 0.3
	pm.ApplyTo(body)
	if body.Friction != 0.02 || body.Restitution != 0.1 || body.LinearDamping != 0.05 || body.AngularDamping != 0.3 {
		t.Fatalf("ApplyTo body = friction %v restitution %v linear %v angular %v", body.Friction, body.Restitution, body.LinearDamping, body.AngularDamping)
	}
}

func libraryFS() fstest.MapFS {
	fsys := fstest.MapFS{}
	for name := range testdataKinds {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			panic(err)
		}
		fsys["res/"+name] = &fstest.MapFile{Data: data}
	}
	fsys["res/broken.material.yaml"] = &fstest.MapFile{Data: []byte("type: material\nversion: 1\nopacity: 3\n")}
	return fsys
}

func TestLibrarySharesOneValuePerPath(t *testing.T) {
	lib := resource.NewLibrary(libraryFS())
	a, err := lib.Material("res/stone.material.yaml")
	if err != nil {
		t.Fatal(err)
	}
	b, err := resource.LoadAs[*resource.Material](lib, "res/stone.material.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("two loads of one path returned different values")
	}
	if _, err := lib.Material("res/jump.curve.yaml"); err == nil || !strings.Contains(err.Error(), "is a curve, want material") {
		t.Fatalf("Material of a curve: %v", err)
	}
	for _, bad := range []string{"", ".", "/res/stone.material.yaml", "res/../res/stone.material.yaml", "res/missing.yaml"} {
		if _, err := lib.Load(bad); err == nil {
			t.Fatalf("Load(%q) succeeded", bad)
		}
	}
	if _, err := lib.Load("res/broken.material.yaml"); err == nil || !strings.Contains(err.Error(), "res/broken.material.yaml") {
		t.Fatalf("Load of an invalid file: %v", err)
	}
	if got := lib.Paths(); len(got) != 2 || got[0] != "res/jump.curve.yaml" || got[1] != "res/stone.material.yaml" {
		t.Fatalf("Paths = %v", got)
	}
}

func TestLibraryReloadChangesSharedValueInPlace(t *testing.T) {
	lib := resource.NewLibrary(libraryFS())
	m, err := lib.Material("res/stone.material.yaml")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := lib.Reload("res/stone.material.yaml", []byte("type: material\nversion: 1\nopacity: 0.25\nblend: screen\n"))
	if !loaded || err != nil {
		t.Fatalf("Reload = %v, %v", loaded, err)
	}
	if m.EffectiveOpacity() != 0.25 || m.BlendOp() != core.CompositeScreen || m.Name != "" {
		t.Fatalf("held material after reload = %+v", m)
	}
	if lib.Revision() != 1 {
		t.Fatalf("Revision = %d, want 1", lib.Revision())
	}

	// A broken edit and a type change keep the last good value.
	if loaded, err := lib.Reload("res/stone.material.yaml", []byte("type: material\nversion: 1\nopacity: 7\n")); !loaded || err == nil {
		t.Fatalf("Reload of a broken file = %v, %v", loaded, err)
	}
	if loaded, err := lib.Reload("res/stone.material.yaml", readTestdata(t, "jump.curve.yaml")); !loaded || err == nil || !strings.Contains(err.Error(), "type changed") {
		t.Fatalf("Reload changing type = %v, %v", loaded, err)
	}
	if m.EffectiveOpacity() != 0.25 || lib.Revision() != 1 {
		t.Fatal("a failed reload changed the held material")
	}

	if loaded, err := lib.Reload("res/never-loaded.yaml", []byte("junk")); loaded || err != nil {
		t.Fatalf("Reload of an unloaded path = %v, %v", loaded, err)
	}
	if lib.Loaded("res/never-loaded.yaml") || !lib.Loaded("res/stone.material.yaml") {
		t.Fatal("Loaded is wrong")
	}
}

func TestLibraryReloadsEveryKind(t *testing.T) {
	lib := resource.NewLibrary(libraryFS())
	for name := range testdataKinds {
		path := "res/" + name
		r, err := lib.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := resource.Encode(r)
		edited := bytes.Clone(before)
		edited = bytes.Replace(edited, []byte("name: "), []byte("name: Edited "), 1)
		if loaded, err := lib.Reload(path, edited); !loaded || err != nil {
			t.Fatalf("Reload %s = %v, %v", path, loaded, err)
		}
		after, _ := resource.Encode(r)
		if !bytes.Equal(after, edited) {
			t.Fatalf("held %s after reload:\n%s\nwant\n%s", path, after, edited)
		}
	}
}

func FuzzParseRoundTrip(f *testing.F) {
	for name := range testdataKinds {
		f.Add(readTestdata(f, name))
	}
	f.Add([]byte("type: curve\nversion: 1\nkeys: [{time: 0, value: 1}]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := resource.Parse(data)
		if err != nil {
			return
		}
		first, err := resource.Encode(r)
		if err != nil {
			t.Fatalf("Encode of a parsed resource: %v", err)
		}
		again, err := resource.Parse(first)
		if err != nil {
			t.Fatalf("Parse of encoded output: %v\n%s", err, first)
		}
		second, err := resource.Encode(again)
		if err != nil {
			t.Fatalf("second Encode: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("encoding is not stable:\n%s\n---\n%s", first, second)
		}
	})
}

func BenchmarkParseMaterial(b *testing.B) {
	data := readTestdata(b, "stone.material.yaml")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := resource.Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCurveEval(b *testing.B) {
	c, err := resource.ParseAs[*resource.Curve](readTestdata(b, "jump.curve.yaml"))
	if err != nil {
		b.Fatal(err)
	}
	var sink float32
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		sink += c.Eval(float32(i%100) / 100)
	}
	_ = sink
}

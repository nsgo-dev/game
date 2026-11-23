package animclip_test

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func bodyRegistry(t testing.TB) *sceneformat.ComponentRegistry {
	t.Helper()
	reg := sceneformat.NewComponentRegistry()
	if err := reg.Register(sceneformat.ComponentType{
		Name: "body",
		Fields: []sceneformat.Field{
			{Name: "solid", Kind: sceneformat.FieldBool, Default: true},
			{Name: "speed", Kind: sceneformat.FieldFloat, Default: 32.0, Min: sceneformat.Float64(0), Max: sceneformat.Float64(100), Doc: "Top speed."},
			{Name: "layer", Kind: sceneformat.FieldInt, Default: 1, Min: sceneformat.Float64(0), Max: sceneformat.Float64(31)},
			{Name: "tag", Kind: sceneformat.FieldString},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestLookup(t *testing.T) {
	reg := bodyRegistry(t)
	for _, name := range []string{"x", "alpha", "flipX", "scaleZ"} {
		p, err := animclip.Lookup(reg, name)
		if err != nil || p.Name != name || p.IsComponent() {
			t.Fatalf("Lookup(%q) = %+v, %v", name, p, err)
		}
	}
	p, err := animclip.Lookup(reg, "body.speed")
	if err != nil || p.Component != "body" || p.Field != "speed" || p.Kind != animclip.KindFloat || *p.Min != 0 || *p.Max != 100 || p.Doc != "Top speed." {
		t.Fatalf("Lookup(body.speed) = %+v, %v", p, err)
	}
	if p, err := animclip.Lookup(reg, "body.layer"); err != nil || p.Kind != animclip.KindInt {
		t.Fatalf("Lookup(body.layer) = %+v, %v", p, err)
	}
	if p, err := animclip.Lookup(reg, "body.solid"); err != nil || p.Kind != animclip.KindBool {
		t.Fatalf("Lookup(body.solid) = %+v, %v", p, err)
	}
	for name, want := range map[string]string{
		"z":           "not an object property",
		"body.tag":    "string field",
		"body.sped":   "no field",
		"wheel.speed": "not registered",
	} {
		if _, err := animclip.Lookup(reg, name); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Lookup(%q) err = %v, want %q", name, err, want)
		}
	}
	if _, err := animclip.Lookup(nil, "body.speed"); err == nil || !strings.Contains(err.Error(), "registry") {
		t.Fatalf("Lookup without a registry err = %v", err)
	}
}

func TestPropertiesListsObjectFieldsThenKeyableComponentFields(t *testing.T) {
	var names []string
	for _, p := range animclip.Properties(bodyRegistry(t)) {
		names = append(names, p.Name)
	}
	objects := len(animclip.ObjectProperties())
	if names[0] != "x" || len(names) != objects+3 {
		t.Fatalf("Properties = %v", names)
	}
	if got := names[objects:]; !slices.Equal(got, []string{"body.solid", "body.speed", "body.layer"}) {
		t.Fatalf("component properties = %v", got)
	}
	if got := len(animclip.Properties(nil)); got != objects {
		t.Fatalf("Properties(nil) has %d, want %d", got, objects)
	}
}

func TestConvertAndCheckValue(t *testing.T) {
	reg := bodyRegistry(t)
	speed, _ := animclip.Lookup(reg, "body.speed")
	layer, _ := animclip.Lookup(reg, "body.layer")
	solid, _ := animclip.Lookup(reg, "body.solid")
	cases := []struct {
		p    animclip.Property
		in   float64
		want float64
	}{
		{speed, 50.5, 50.5}, {speed, -3, 0}, {speed, 120, 100},
		{layer, 2.5, 3}, {layer, 2.49, 2}, {layer, 40, 31},
		{solid, 0.5, 1}, {solid, 0.49, 0},
	}
	for _, c := range cases {
		if got := c.p.Convert(c.in); got != c.want {
			t.Fatalf("%s.Convert(%v) = %v, want %v", c.p.Name, c.in, got, c.want)
		}
	}
	for _, bad := range []struct {
		p    animclip.Property
		v    float64
		want string
	}{
		{solid, 0.5, "0 or 1"}, {layer, 1.5, "whole"}, {speed, 101, "maximum"}, {speed, -1, "minimum"}, {speed, math.Inf(1), "finite"},
	} {
		if err := bad.p.CheckValue(bad.v); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Fatalf("%s.CheckValue(%v) = %v, want %q", bad.p.Name, bad.v, err, bad.want)
		}
	}
}

func TestGetAndSetEveryProperty(t *testing.T) {
	reg := bodyRegistry(t)
	o := &sceneformat.ObjectDoc{ID: "hero"}
	for _, p := range animclip.Properties(reg) {
		want := 0.75
		if p.Kind != animclip.KindFloat {
			want = 1
		}
		p.Set(o, want)
		got, err := p.Get(o, reg)
		if err != nil || got != want {
			t.Fatalf("%s: Get after Set(%v) = %v, %v", p.Name, want, got, err)
		}
	}
	body := o.Components["body"].(map[string]any)
	if body["solid"] != true || body["layer"] != 1 || body["speed"] != 0.75 {
		t.Fatalf("body = %#v, want Go bool, int and float64 values", body)
	}
	fresh := &sceneformat.ObjectDoc{ID: "slime"}
	speed, _ := animclip.Lookup(reg, "body.speed")
	if v, err := speed.Get(fresh, reg); err != nil || v != 32 {
		t.Fatalf("unset speed = %v, %v, want the default 32", v, err)
	}
	scale, _ := animclip.Lookup(reg, "scaleX")
	if v, _ := scale.Get(fresh, reg); v != 1 {
		t.Fatalf("unset scaleX = %v, want 1", v)
	}
}

func TestCheckRefusesKeysTheirPropertyCannotTake(t *testing.T) {
	reg := bodyRegistry(t)
	clip := func(property string, value float32) *resource.Animation {
		return &resource.Animation{Header: resource.NewHeader(resource.KindAnimation), Length: 1, Tracks: []resource.Track{{Property: property, Keys: []resource.Key{{Time: 0, Value: value}}}}}
	}
	if err := animclip.Check(clip("body.layer", 3), reg); err != nil {
		t.Fatalf("Check of a valid clip: %v", err)
	}
	for _, c := range []struct {
		property string
		value    float32
		want     string
	}{
		{"body.solid", 0.5, "0 or 1"},
		{"body.layer", 0.5, "whole"},
		{"alpha", 2, "maximum"},
		{"body.tag", 0, "cannot be keyed"},
		{"bogus", 0, "not an object property"},
	} {
		err := animclip.Check(clip(c.property, c.value), reg)
		if err == nil || !strings.Contains(err.Error(), c.want) || strings.Count(err.Error(), "animclip:") != 1 {
			t.Fatalf("Check(%s = %v) = %v, want %q once prefixed", c.property, c.value, err, c.want)
		}
	}
}

const walkClip = `type: animation
version: 1
name: Walk
length: 2
tracks:
  - property: x
    keys:
      - time: 0
        value: 10
      - time: 0.5
        value: 30
        interp: smooth
      - time: 1.5
        value: 20
        interp: step
      - time: 1.8
        value: 0
  - target: lamp
    property: alpha
    keys:
      - time: 1
        value: 0.25
  - property: body.layer
    keys:
      - time: 0
        value: 0
      - time: 2
        value: 5
events:
  - time: 0.5
    name: step
  - time: 1.5
    name: step
  - time: 2
    name: end
`

func parseClip(t testing.TB, src string) *resource.Animation {
	t.Helper()
	clip, err := resource.ParseAs[*resource.Animation]([]byte(src))
	if err != nil {
		t.Fatalf("parse clip: %v", err)
	}
	return clip
}

func recorder(out map[string]float64) animclip.Binder {
	return func(target string, p animclip.Property) (func(float64), error) {
		key := target + "/" + p.Name
		return func(v float64) { out[key] = v }, nil
	}
}

func TestPlayerPlaysWhatSampleComputes(t *testing.T) {
	reg := bodyRegistry(t)
	clip := parseClip(t, walkClip)
	got := map[string]float64{}
	p, err := animclip.NewPlayer(clip, reg, recorder(got))
	if err != nil {
		t.Fatal(err)
	}
	if p.Length() != 2*time.Second {
		t.Fatalf("Length = %v", p.Length())
	}
	for ms := 0; ms <= 2000; ms += 25 {
		at := time.Duration(ms) * time.Millisecond
		p.Seek(at)
		want := map[string]float64{}
		clip.Sample(float32(at.Seconds()), func(target, property string, v float32) {
			want[target+"/"+property] = float64(v)
		})
		for key, w := range want {
			if key == "/body.layer" {
				// Sample keeps the unrounded number, and float32 time can put it on the other side of a half.
				continue
			}
			if g := got[key]; math.Abs(g-w) > 1e-5 {
				t.Fatalf("at %v %s = %v, Sample gives %v", at, key, got[key], w)
			}
		}
	}
	// The step key holds 20 until the next key, which then takes over.
	p.Seek(1799 * time.Millisecond)
	if got["/x"] != 20 {
		t.Fatalf("x just before the key after a step = %v, want 20", got["/x"])
	}
	p.Seek(1800 * time.Millisecond)
	if got["/x"] != 0 {
		t.Fatalf("x on the key after a step = %v, want 0", got["/x"])
	}
	p.Seek(time.Second)
	if got["/body.layer"] != 3 {
		t.Fatalf("body.layer at 1s = %v, want 2.5 rounded to 3", got["/body.layer"])
	}
	// A track holds its only key's value before and after it.
	p.Seek(0)
	if got["lamp/alpha"] != 0.25 {
		t.Fatalf("lamp alpha before its key = %v, want 0.25", got["lamp/alpha"])
	}
}

func TestPlayerUpdateMatchesSeekAndFiresEvents(t *testing.T) {
	reg := bodyRegistry(t)
	clip := parseClip(t, walkClip)
	played := map[string]float64{}
	p, err := animclip.NewPlayer(clip, reg, recorder(played))
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	p.OnEvent = func(name string) { events = append(events, name+"@"+p.Time().String()) }
	sought := map[string]float64{}
	ref, err := animclip.NewPlayer(clip, reg, recorder(sought))
	if err != nil {
		t.Fatal(err)
	}
	ref.OnEvent = func(name string) { t.Fatalf("Seek fired event %q", name) }

	p.Play()
	step := time.Second / 60
	for !p.Done() {
		p.Update(step)
		ref.Seek(p.Time())
		for k, v := range sought {
			if played[k] != v {
				t.Fatalf("at %v %s played %v, sought %v", p.Time(), k, played[k], v)
			}
		}
	}
	if p.Playing() || p.Time() != 2*time.Second {
		t.Fatalf("after the end: playing %t at %v", p.Playing(), p.Time())
	}
	// An event fires on the first step that reaches its time.
	crossing := func(at time.Duration) string { return min((at+step-1)/step*step, 2*time.Second).String() }
	if want := []string{"step@" + crossing(500*time.Millisecond), "step@" + crossing(1500*time.Millisecond), "end@2s"}; !slices.Equal(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestPlayerLoops(t *testing.T) {
	clip := parseClip(t, walkClip)
	clip.Loop = true
	got := map[string]float64{}
	p, err := animclip.NewPlayer(clip, bodyRegistry(t), recorder(got))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Loop() {
		t.Fatal("Loop = false for a looping clip")
	}
	var steps int
	p.OnEvent = func(name string) {
		if name == "step" {
			steps++
		}
	}
	p.Play()
	for range 5 {
		p.Update(500 * time.Millisecond)
	}
	if p.Done() || !p.Playing() || p.Time() != 500*time.Millisecond {
		t.Fatalf("looping clip after 2.5s: done %t playing %t at %v", p.Done(), p.Playing(), p.Time())
	}
	if steps != 3 {
		t.Fatalf("step events = %d, want 3 over 2.5s", steps)
	}
	p.Seek(2500 * time.Millisecond)
	if p.Time() != 500*time.Millisecond || got["/x"] != 30 {
		t.Fatalf("Seek(2.5s) on a loop: at %v x %v, want 0.5s and 30", p.Time(), got["/x"])
	}
	p.Seek(-500 * time.Millisecond)
	if p.Time() != 1500*time.Millisecond {
		t.Fatalf("Seek(-0.5s) on a loop: at %v, want 1.5s", p.Time())
	}
	p.SetLoop(false)
	p.Seek(5 * time.Second)
	if p.Time() != 2*time.Second || p.Loop() {
		t.Fatalf("Seek past the end without loop: at %v loop %t", p.Time(), p.Loop())
	}
}

func TestNewPlayerRefusesWhatItCannotBind(t *testing.T) {
	reg := bodyRegistry(t)
	clip := parseClip(t, walkClip)
	doc := &sceneformat.SceneDoc{Version: 1, Layers: []sceneformat.LayerDoc{{Name: "main", Objects: []sceneformat.ObjectDoc{{ID: "hero"}}}}}
	if _, err := animclip.NewPlayer(clip, reg, animclip.BindDoc(doc, "hero")); err == nil || !strings.Contains(err.Error(), `no object "lamp"`) {
		t.Fatalf("NewPlayer without the lamp: %v", err)
	}
	if _, err := animclip.NewPlayer(clip, nil, recorder(map[string]float64{})); err == nil || !strings.Contains(err.Error(), "registry") {
		t.Fatalf("NewPlayer of a component track without a registry: %v", err)
	}
	if _, err := animclip.NewPlayer(clip, reg, nil); err == nil {
		t.Fatal("NewPlayer with a nil binder succeeded")
	}
	nilSetter := func(string, animclip.Property) (func(float64), error) { return nil, nil }
	if _, err := animclip.NewPlayer(clip, reg, nilSetter); err == nil || !strings.Contains(err.Error(), "no setter") {
		t.Fatalf("NewPlayer with a binder that returns no setter: %v", err)
	}
}

func TestBindSceneKeepsSpritesInStep(t *testing.T) {
	reg := bodyRegistry(t)
	half := float32(0.5)
	doc := &sceneformat.SceneDoc{Version: 1, Layers: []sceneformat.LayerDoc{{Name: "main", Objects: []sceneformat.ObjectDoc{
		{ID: "hero", Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"}},
		{ID: "lamp", Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"}, Material: "glass.material.yaml"},
	}}}}
	built, err := sceneformat.Build(doc, resolver{
		atlas: &sprite.Atlas{Image: core.ImageSource{URI: "asset:actors.png"}, Frames: map[string]sprite.Frame{"hero": {Name: "hero", W: 16, H: 16, Width: 16, Height: 16}}},
		mat:   &resource.Material{Header: resource.NewHeader(resource.KindMaterial), Opacity: &half},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := animclip.NewPlayer(parseClip(t, walkClip), reg, animclip.BindScene(built, "hero"))
	if err != nil {
		t.Fatal(err)
	}
	p.Seek(time.Second)
	hero, lamp := built.Object("hero"), built.Object("lamp")
	if hero.Doc.X != 25 || hero.Sprite.X != 25 {
		t.Fatalf("hero x = %v, sprite %v, want 25", hero.Doc.X, hero.Sprite.X)
	}
	if lamp.Doc.EffectiveAlpha() != 0.25 || lamp.Sprite.Alpha != 0.125 {
		t.Fatalf("lamp alpha = %v, sprite %v, want 0.25 and 0.125 through the material", lamp.Doc.EffectiveAlpha(), lamp.Sprite.Alpha)
	}
	if got := hero.Doc.Components["body"].(map[string]any)["layer"]; got != 3 {
		t.Fatalf("hero body.layer = %#v, want int 3 (2.5 rounded)", got)
	}
	built.SyncMaterials()
	if lamp.Sprite.Alpha != 0.125 {
		t.Fatalf("SyncMaterials changed the lamp's alpha to %v", lamp.Sprite.Alpha)
	}
}

type resolver struct {
	atlas *sprite.Atlas
	mat   *resource.Material
}

func (r resolver) Atlas(string) (*sprite.Atlas, error)         { return r.atlas, nil }
func (r resolver) Material(string) (*resource.Material, error) { return r.mat, nil }

func TestSecondsUsesTheShortestDecimal(t *testing.T) {
	if got := animclip.Seconds(0.1); got != 100*time.Millisecond {
		t.Fatalf("Seconds(0.1) = %v", got)
	}
	if got := animclip.KeyValue(9.81); got != 9.81 {
		t.Fatalf("KeyValue(9.81) = %v", got)
	}
	if got := animclip.Seconds(float32(math.Inf(1))); got != math.MaxInt64 {
		t.Fatalf("Seconds(+Inf) = %v", got)
	}
}

func FuzzPlayerSeek(f *testing.F) {
	f.Add([]byte(walkClip), int64(time.Second))
	f.Add([]byte("type: animation\nversion: 1\nlength: 0.5\nloop: true\ntracks:\n  - {property: rotation, keys: [{time: 0.25, value: 3}]}\n"), int64(-7))
	reg := bodyRegistry(f)
	f.Fuzz(func(t *testing.T, data []byte, at int64) {
		clip, err := resource.ParseAs[*resource.Animation](data)
		if err != nil || animclip.Check(clip, reg) != nil {
			return
		}
		got := map[string]float64{}
		p, err := animclip.NewPlayer(clip, reg, recorder(got))
		if err != nil {
			return
		}
		p.Seek(time.Duration(at))
		if p.Time() < 0 || p.Time() > p.Length() {
			t.Fatalf("Seek(%v) put the player at %v, outside [0, %v]", time.Duration(at), p.Time(), p.Length())
		}
		for i := range clip.Tracks {
			tr := &clip.Tracks[i]
			lo, hi := math.Inf(1), math.Inf(-1)
			for _, k := range tr.Keys {
				v := animclip.KeyValue(k.Value)
				lo, hi = min(lo, v), max(hi, v)
			}
			prop, _ := animclip.Lookup(reg, tr.Property)
			lo, hi = prop.Convert(lo), prop.Convert(hi)
			tol := 1e-9 * (math.Abs(lo) + math.Abs(hi) + 1)
			v, ok := got[tr.Target+"/"+tr.Property]
			if !ok || math.IsNaN(v) || v < lo-tol || v > hi+tol {
				t.Fatalf("track %d at %v = %v (written %t), outside its keys' [%v, %v]", i, p.Time(), v, ok, lo, hi)
			}
		}
	})
}

func BenchmarkPlayerUpdate(b *testing.B) {
	clip := parseClip(b, walkClip)
	clip.Loop = true
	p, err := animclip.NewPlayer(clip, bodyRegistry(b), func(string, animclip.Property) (func(float64), error) {
		return func(float64) {}, nil
	})
	if err != nil {
		b.Fatal(err)
	}
	p.Play()
	b.ReportAllocs()
	for b.Loop() {
		p.Update(time.Second / 60)
	}
}

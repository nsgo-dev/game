package particles_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/nsgo/core"
)

const explosionEffect = `version: 1
emitters:
  - config:
      space: world
      shape:
        kind: circle
        radius: 6
      rate: 45
      lifetimeMin: 300ms
      lifetimeMax: 700ms
      colorCurve:
        - t: 0
          color: "#ffd166ff"
    duration: 200ms
  - file: fx/sparks.particles.yaml
    offsetX: 4
    offsetY: -2
    delay: 50ms
    minQuality: medium
params:
  count:
    type: int
    int: 12
  scale:
    type: vec2
    vec2: [1, 0.5]
  speed:
    type: float
    float: 1.5
  tint:
    type: color
    color: "#ff8800ff"
`

func TestEffectRoundTripIsByteForByte(t *testing.T) {
	e, err := particles.UnmarshalEffectYAML([]byte(explosionEffect))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Emitters) != 2 {
		t.Fatalf("emitters = %d, want 2", len(e.Emitters))
	}
	inline, file := e.Emitters[0], e.Emitters[1]
	if inline.Config == nil || inline.Config.Shape != particles.Circle(6) || inline.Config.LifetimeMax != 700*time.Millisecond || inline.Duration != 200*time.Millisecond {
		t.Fatalf("inline emitter = %+v, config %+v", inline, inline.Config)
	}
	if file.File != "fx/sparks.particles.yaml" || file.Config != nil || file.OffsetX != 4 || file.OffsetY != -2 || file.Delay != 50*time.Millisecond || file.MinQuality != "medium" {
		t.Fatalf("file emitter = %+v", file)
	}
	want := map[string]particles.EffectParam{
		"count": {Type: particles.EffectParamInt, Int: 12},
		"scale": {Type: particles.EffectParamVec2, Vec2: [2]float32{1, 0.5}},
		"speed": {Type: particles.EffectParamFloat, Float: 1.5},
		"tint":  {Type: particles.EffectParamColor, Color: core.Color(0xff8800ff)},
	}
	for name, p := range want {
		if e.Params[name] != p {
			t.Errorf("param %s = %+v, want %+v", name, e.Params[name], p)
		}
	}
	out, err := particles.MarshalEffectYAML(e)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != explosionEffect {
		t.Fatalf("round trip changed the file:\n%s", out)
	}
}

func TestEffectRefusesInvalidFiles(t *testing.T) {
	const config = "    config:\n      rate: 1\n"
	for name, tc := range map[string]struct{ data, field string }{
		"version":             {"version: 2\nemitters:\n  -\n" + config, "version"},
		"no emitters":         {"version: 1\nemitters: []\n", "emitters"},
		"unknown field":       {"version: 1\nemitters:\n  - colour: 1\n", ""},
		"file and config":     {"version: 1\nemitters:\n  - file: a.yaml\n" + config, "emitters[0]"},
		"neither":             {"version: 1\nemitters:\n  - delay: 1s\n", "emitters[0]"},
		"escaping path":       {"version: 1\nemitters:\n  - file: ../a.yaml\n", "emitters[0].file"},
		"negative delay":      {"version: 1\nemitters:\n  - file: a.yaml\n    delay: -1s\n", "emitters[0]"},
		"bare duration":       {"version: 1\nemitters:\n  - file: a.yaml\n    duration: 2\n", ""},
		"unknown quality":     {"version: 1\nemitters:\n  - file: a.yaml\n    minQuality: ultra\n", "emitters[0].minQuality"},
		"inline version":      {"version: 1\nemitters:\n  - config:\n      version: 1\n", "emitters[0].config.version"},
		"inline bad shape":    {"version: 1\nemitters:\n  - config:\n      shape:\n        kind: point\n        radius: 2\n", "emitters[0].config.shape"},
		"unknown param type":  {"version: 1\nemitters:\n  - file: a.yaml\nparams:\n  p: {type: bool}\n", "params.p"},
		"value of other type": {"version: 1\nemitters:\n  - file: a.yaml\nparams:\n  p: {type: float, int: 1}\n", "params.p"},
		"missing value":       {"version: 1\nemitters:\n  - file: a.yaml\nparams:\n  p: {type: int}\n", "params.p"},
		"fractional int":      {"version: 1\nemitters:\n  - file: a.yaml\nparams:\n  p: {type: int, int: 1.5}\n", ""},
		"short vec2":          {"version: 1\nemitters:\n  - file: a.yaml\nparams:\n  p: {type: vec2, vec2: [1]}\n", "params.p"},
		"unquoted color":      {"version: 1\nemitters:\n  - file: a.yaml\nparams:\n  p: {type: color, color: \"ff8800\"}\n", ""},
		"infinite float":      {"version: 1\nemitters:\n  - file: a.yaml\nparams:\n  p: {type: float, float: .inf}\n", "params.p"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := particles.UnmarshalEffectYAML([]byte(tc.data))
			if err == nil {
				t.Fatal("loaded an invalid effect")
			}
			if tc.field == "" {
				return
			}
			le, ok := loaderr.As(err)
			if !ok || le.Field != tc.field {
				t.Fatalf("error %v at field %q, want field %q", err, fieldOf(le), tc.field)
			}
		})
	}
}

func fieldOf(e *loaderr.Error) string {
	if e == nil {
		return ""
	}
	return e.Field
}

func TestEffectSetParam(t *testing.T) {
	e, err := particles.UnmarshalEffectYAML([]byte(explosionEffect))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{"speed": 2.0, "count": 3, "tint": core.Color(0x112233ff), "scale": [2]float32{2, 3}} {
		if err := e.SetParam(name, value); err != nil {
			t.Fatalf("SetParam(%s, %v): %v", name, value, err)
		}
	}
	if err := e.SetParam("speed", float32(0.25)); err != nil || e.Params["speed"].Float != 0.25 {
		t.Fatalf("float32 speed: %v, %+v", err, e.Params["speed"])
	}
	if e.Params["count"].Int != 3 || e.Params["tint"].Color != core.Color(0x112233ff) || e.Params["scale"].Vec2 != [2]float32{2, 3} {
		t.Fatalf("params after SetParam = %+v", e.Params)
	}
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"missing", 1.0, "unknown param"},
		{"speed", 1, "needs a float64"},
		{"count", 1.0, "needs an int"},
		{"tint", "#ff0000", "needs a core.Color"},
		{"scale", []float32{1, 2}, "needs a [2]float32"},
		{"speed", float32(math.NaN()), "not finite"},
	} {
		before := e.Params[tc.name]
		err := e.SetParam(tc.name, tc.value)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("SetParam(%s, %v) = %v, want %q", tc.name, tc.value, err, tc.want)
		}
		if e.Params[tc.name] != before {
			t.Errorf("SetParam(%s, %v) changed the param to %+v", tc.name, tc.value, e.Params[tc.name])
		}
	}
	out, err := particles.MarshalEffectYAML(e)
	if err != nil {
		t.Fatal(err)
	}
	back, err := particles.UnmarshalEffectYAML(out)
	if err != nil {
		t.Fatal(err)
	}
	if back.Params["tint"] != e.Params["tint"] || back.Params["scale"] != e.Params["scale"] {
		t.Fatalf("set params did not survive a save: %+v", back.Params)
	}
}

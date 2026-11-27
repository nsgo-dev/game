package vehicle3d_test

import (
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/vehicle3d"
)

func TestReferenceConfigFile(t *testing.T) {
	data, err := os.ReadFile("testdata/reference_car.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := vehicle3d.ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, vehicle3d.ReferenceConfig()) {
		t.Fatalf("testdata/reference_car.yaml differs from ReferenceConfig:\n%+v", cfg)
	}
	out, err := vehicle3d.EncodeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(data) {
		t.Fatalf("EncodeConfig changed the file:\n%s", out)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	cfg := vehicle3d.ReferenceConfig()
	cfg.Name = ""
	cfg.Wheels[0].MaxForce = 0
	cfg.ChassisOffset = [3]float32{0.1, 0.123456789, -1e-7}
	cfg.Drag = 1e6
	a, err := vehicle3d.EncodeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := vehicle3d.ParseConfig(a)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Fatalf("round trip changed the config:\n got %+v\nwant %+v", got, cfg)
	}
	b, err := vehicle3d.EncodeConfig(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("EncodeConfig is not deterministic")
	}
}

func TestParseConfigRejects(t *testing.T) {
	base, err := os.ReadFile("testdata/reference_car.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]string{
		"empty":         "  \n",
		"unknown field": string(base) + "turbo: true\n",
		"unknown wheel": strings.Replace(string(base), "radius: 0.35", "radius: 0.35\n    camber: 2", 1),
		"version":       strings.Replace(string(base), "version: 1", "version: 2", 1),
		"no version":    strings.Replace(string(base), "version: 1\n", "", 1),
		"trailing doc":  string(base) + "---\nversion: 1\n",
		"short vector":  strings.Replace(string(base), "chassisOffset: [0, 0.2, 0]", "chassisOffset: [0, 0.2]", 1),
		"not yaml":      "{",
	} {
		if _, err := vehicle3d.ParseConfig([]byte(doc)); err == nil {
			t.Errorf("%s: ParseConfig accepted it", name)
		}
	}
}

func TestConfigValidate(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	for name, mutate := range map[string]func(*vehicle3d.Config){
		"mass":            func(c *vehicle3d.Config) { c.Mass = 0 },
		"nan mass":        func(c *vehicle3d.Config) { c.Mass = nan },
		"extent":          func(c *vehicle3d.Config) { c.ChassisHalfExtents[1] = 0 },
		"offset":          func(c *vehicle3d.Config) { c.ChassisOffset[0] = inf },
		"engine":          func(c *vehicle3d.Config) { c.EngineForce = -1 },
		"reverse":         func(c *vehicle3d.Config) { c.ReverseForce = nan },
		"brake":           func(c *vehicle3d.Config) { c.BrakeForce = -1 },
		"drag":            func(c *vehicle3d.Config) { c.Drag = -1 },
		"rolling":         func(c *vehicle3d.Config) { c.RollingResistance = -1 },
		"angular damping": func(c *vehicle3d.Config) { c.AngularDamping = -1 },
		"max speed":       func(c *vehicle3d.Config) { c.MaxSpeed = 0 },
		"reverse speed":   func(c *vehicle3d.Config) { c.MaxReverseSpeed = 0 },
		"steer rate":      func(c *vehicle3d.Config) { c.SteerRate = 0 },
		"handbrake grip":  func(c *vehicle3d.Config) { c.HandbrakeGrip = 1.5 },
		"roll influence":  func(c *vehicle3d.Config) { c.RollInfluence = -0.1 },
		"steer angle":     func(c *vehicle3d.Config) { c.MaxSteerAngle = 90 },
		"no wheels":       func(c *vehicle3d.Config) { c.Wheels = nil },
		"too many wheels": func(c *vehicle3d.Config) { c.Wheels = make([]vehicle3d.WheelConfig, 33) },
		"wheel name":      func(c *vehicle3d.Config) { c.Wheels[1].Name = "" },
		"duplicate name":  func(c *vehicle3d.Config) { c.Wheels[1].Name = c.Wheels[0].Name },
		"anchor":          func(c *vehicle3d.Config) { c.Wheels[0].Anchor[2] = nan },
		"radius":          func(c *vehicle3d.Config) { c.Wheels[0].Radius = 0 },
		"stiffness":       func(c *vehicle3d.Config) { c.Wheels[0].Stiffness = 0 },
		"rest length":     func(c *vehicle3d.Config) { c.Wheels[0].RestLength = -1 },
		"damping":         func(c *vehicle3d.Config) { c.Wheels[0].Damping = -1 },
		"max force":       func(c *vehicle3d.Config) { c.Wheels[0].MaxForce = inf },
		"friction":        func(c *vehicle3d.Config) { c.Wheels[0].Friction = -1 },
	} {
		cfg := vehicle3d.ReferenceConfig()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: Validate accepted it", name)
		}
		if _, err := vehicle3d.EncodeConfig(cfg); err == nil {
			t.Errorf("%s: EncodeConfig accepted it", name)
		}
	}
	var nilCfg *vehicle3d.Config
	if nilCfg.Validate() == nil {
		t.Error("nil config validated")
	}
}

func FuzzParseConfig(f *testing.F) {
	if data, err := os.ReadFile("testdata/reference_car.yaml"); err == nil {
		f.Add(data)
	}
	f.Add([]byte("version: 1\nmass: 1\n"))
	f.Add([]byte("version: 1\nwheels: [{name: a}]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := vehicle3d.ParseConfig(data)
		if err != nil {
			return
		}
		out, err := vehicle3d.EncodeConfig(cfg)
		if err != nil {
			t.Fatalf("parsed config does not encode: %v", err)
		}
		again, err := vehicle3d.ParseConfig(out)
		if err != nil {
			t.Fatalf("encoded config does not parse: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(again, cfg) {
			t.Fatalf("round trip changed the config:\n%+v\n%+v", again, cfg)
		}
	})
}

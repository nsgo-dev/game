package vehicle_test

import (
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/vehicle"
)

func TestReferenceConfigFile(t *testing.T) {
	data, err := os.ReadFile("testdata/reference_car.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := vehicle.ParseConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, vehicle.ReferenceConfig()) {
		t.Fatalf("testdata/reference_car.yaml differs from ReferenceConfig:\n%+v", cfg)
	}
	out, err := vehicle.EncodeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(data) {
		t.Fatalf("EncodeConfig changed the file:\n%s", out)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	cfg := vehicle.ReferenceConfig()
	cfg.Name = ""
	cfg.Wheels[0].MaxForce = 0
	cfg.ChassisOffset = [3]float32{0.1, 0.123456789, -1e-7}
	cfg.Drag = 1e6
	a, err := vehicle.EncodeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := vehicle.ParseConfig(a)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Fatalf("round trip changed the config:\n got %+v\nwant %+v", got, cfg)
	}
	b, err := vehicle.EncodeConfig(got)
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
		if _, err := vehicle.ParseConfig([]byte(doc)); err == nil {
			t.Errorf("%s: ParseConfig accepted it", name)
		}
	}
}

func TestConfigValidate(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	for name, mutate := range map[string]func(*vehicle.Config){
		"mass":            func(c *vehicle.Config) { c.Mass = 0 },
		"nan mass":        func(c *vehicle.Config) { c.Mass = nan },
		"extent":          func(c *vehicle.Config) { c.ChassisHalfExtents[1] = 0 },
		"offset":          func(c *vehicle.Config) { c.ChassisOffset[0] = inf },
		"engine":          func(c *vehicle.Config) { c.EngineForce = -1 },
		"reverse":         func(c *vehicle.Config) { c.ReverseForce = nan },
		"brake":           func(c *vehicle.Config) { c.BrakeForce = -1 },
		"drag":            func(c *vehicle.Config) { c.Drag = -1 },
		"rolling":         func(c *vehicle.Config) { c.RollingResistance = -1 },
		"angular damping": func(c *vehicle.Config) { c.AngularDamping = -1 },
		"max speed":       func(c *vehicle.Config) { c.MaxSpeed = 0 },
		"reverse speed":   func(c *vehicle.Config) { c.MaxReverseSpeed = 0 },
		"steer rate":      func(c *vehicle.Config) { c.SteerRate = 0 },
		"handbrake grip":  func(c *vehicle.Config) { c.HandbrakeGrip = 1.5 },
		"roll influence":  func(c *vehicle.Config) { c.RollInfluence = -0.1 },
		"steer angle":     func(c *vehicle.Config) { c.MaxSteerAngle = 90 },
		"no wheels":       func(c *vehicle.Config) { c.Wheels = nil },
		"too many wheels": func(c *vehicle.Config) { c.Wheels = make([]vehicle.WheelConfig, 33) },
		"wheel name":      func(c *vehicle.Config) { c.Wheels[1].Name = "" },
		"duplicate name":  func(c *vehicle.Config) { c.Wheels[1].Name = c.Wheels[0].Name },
		"anchor":          func(c *vehicle.Config) { c.Wheels[0].Anchor[2] = nan },
		"radius":          func(c *vehicle.Config) { c.Wheels[0].Radius = 0 },
		"stiffness":       func(c *vehicle.Config) { c.Wheels[0].Stiffness = 0 },
		"rest length":     func(c *vehicle.Config) { c.Wheels[0].RestLength = -1 },
		"damping":         func(c *vehicle.Config) { c.Wheels[0].Damping = -1 },
		"max force":       func(c *vehicle.Config) { c.Wheels[0].MaxForce = inf },
		"friction":        func(c *vehicle.Config) { c.Wheels[0].Friction = -1 },
	} {
		cfg := vehicle.ReferenceConfig()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: Validate accepted it", name)
		}
		if _, err := vehicle.EncodeConfig(cfg); err == nil {
			t.Errorf("%s: EncodeConfig accepted it", name)
		}
	}
	var nilCfg *vehicle.Config
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
		cfg, err := vehicle.ParseConfig(data)
		if err != nil {
			return
		}
		out, err := vehicle.EncodeConfig(cfg)
		if err != nil {
			t.Fatalf("parsed config does not encode: %v", err)
		}
		again, err := vehicle.ParseConfig(out)
		if err != nil {
			t.Fatalf("encoded config does not parse: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(again, cfg) {
			t.Fatalf("round trip changed the config:\n%+v\n%+v", again, cfg)
		}
	})
}

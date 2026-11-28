package sceneformat

import (
	"reflect"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestBehaviorConfigValidation(t *testing.T) {
	valid := []BehaviorConfig{
		{Version: 1, Kind: "platform"},
		{Version: 1, Kind: "eight_direction", Directions: 4},
		{Version: 1, Kind: "eight_direction", Directions: 8},
		{Version: 1, Kind: "tile_movement", TileSize: 16},
		{Version: 1, Kind: "car", MaxSpeed: 200, Acceleration: 300},
		{Version: 1, Kind: "drag_drop"},
		{Version: 1, Kind: "turret", Range: 160, RotateSpeed: 180, FireRate: 2, Targets: []string{"enemies"}},
		{Version: 1, Kind: "line_of_sight", Range: 200},
		{Version: 1, Kind: "path_agent", MaxSpeed: 80},
	}
	for _, c := range valid {
		if err := c.Validate(); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	invalid := map[string]BehaviorConfig{
		"missing version":   {Kind: "platform"},
		"future version":    {Version: 2, Kind: "platform"},
		"unknown kind":      {Version: 1, Kind: "unknown"},
		"six directions":    {Version: 1, Kind: "eight_direction", Directions: 6},
		"no directions":     {Version: 1, Kind: "eight_direction"},
		"zero tile size":    {Version: 1, Kind: "tile_movement"},
		"negative speed":    {Version: 1, Kind: "car", MaxSpeed: -1},
		"negative range":    {Version: 1, Kind: "line_of_sight", Range: -5},
		"negative fireRate": {Version: 1, Kind: "turret", FireRate: -1},
	}
	for name, c := range invalid {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: accepted %+v", name, c)
		}
	}
}

func TestParseBehaviorConfig(t *testing.T) {
	c, err := ParseBehaviorConfig([]byte("version: 1\nkind: turret\nrange: 160\nfireRate: 2\ntargets: [enemies]\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := BehaviorConfig{Version: 1, Kind: "turret", Range: 160, FireRate: 2, Targets: []string{"enemies"}}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("got %+v, want %+v", c, want)
	}
	for name, src := range map[string]string{
		"unknown field":    "version: 1\nkind: platform\nspeed: 3\n",
		"trailing doc":     "version: 1\nkind: platform\n---\nversion: 1\nkind: car\n",
		"invalid settings": "version: 1\nkind: tile_movement\n",
	} {
		if _, err := ParseBehaviorConfig([]byte(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestBehaviorConfigRoundTrip(t *testing.T) {
	src := "version: 1\nkind: eight_direction\naxis: move\nmaxSpeed: 120\nacceleration: 800\ndeceleration: 1000\ndirections: 8\n"
	c, err := ParseBehaviorConfig([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != src {
		t.Fatalf("round trip changed the document:\n%s", out)
	}
}

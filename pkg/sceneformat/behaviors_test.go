package sceneformat

import "testing"

func TestBehaviorConfigValidation(t *testing.T) {
	valid := []BehaviorConfig{{Version: 1, Kind: "platform"}, {Version: 1, Kind: "eight_direction", Directions: 8}, {Version: 1, Kind: "tile_movement", TileSize: 16}, {Version: 1, Kind: "turret", FireRate: 2}}
	for _, c := range valid {
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if err := (BehaviorConfig{Version: 1, Kind: "eight_direction", Directions: 6}).Validate(); err == nil {
		t.Fatal("invalid directions accepted")
	}
	if err := (BehaviorConfig{Version: 1, Kind: "unknown"}).Validate(); err == nil {
		t.Fatal("unknown behavior accepted")
	}
}

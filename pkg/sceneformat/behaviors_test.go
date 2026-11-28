package sceneformat_test

import (
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"testing"
)

func TestBehaviorComponentSchemas(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	if err := sceneformat.RegisterBehaviorComponents(reg); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"platform", "eight_direction", "tile_movement", "car", "drag_drop", "turret", "line_of_sight", "path_agent"} {
		if _, ok := reg.Lookup(n); !ok {
			t.Fatalf("missing %s", n)
		}
	}
	_, ok := sceneformat.BehaviorComponent("unknown")
	if ok {
		t.Fatal("unknown behavior accepted")
	}
}

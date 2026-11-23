package skeletal_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game/skeletal"
)

// rig is a two-bone arm with a "raise" animation that lifts the upper arm over half a second.
const rig = `{
	"version": 1,
	"name": "arm",
	"bones": [
		{"name": "shoulder"},
		{"name": "upper", "parent": "shoulder", "length": 10},
		{"name": "hand", "parent": "upper", "x": 10, "length": 4}
	],
	"slots": [{"name": "upper", "bone": "upper"}],
	"skins": {"default": {"upper": {"frame": "sleeve"}}},
	"animations": {
		"raise": {
			"duration": 0.5,
			"bones": {"upper": {"rotate": [{"time": 0, "angle": 0}, {"time": 0.5, "angle": -1.5707964}]}}
		}
	}
}`

// Load reads and validates a rig, refusing unknown versions, cycles and references to missing bones.
func ExampleLoad() {
	data, err := skeletal.Load([]byte(rig))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(data.Name, len(data.Bones), len(data.Animations))
	_, err = skeletal.Load([]byte(`{"version": 2, "name": "x"}`))
	fmt.Println(err != nil)
	// Output:
	// arm 3 1
	// true
}

// A Skeleton plays a rig's animations; WorldBone gives a bone's place after the bones above it have moved, here the hand after the arm is raised.
func ExampleSkeleton() {
	data, _ := skeletal.Load([]byte(rig))
	s := skeletal.NewSkeleton(data, nil)
	s.X, s.Y = 100, 100
	s.UpdateWorldTransforms()
	hand, _ := s.WorldBone("hand")
	fmt.Printf("hand at (%.1f, %.1f)\n", hand.X, hand.Y)

	if err := s.Play("raise", false); err != nil {
		fmt.Println(err)
		return
	}
	for range 40 {
		s.Update(time.Second / 60)
	}
	s.UpdateWorldTransforms()
	hand, _ = s.WorldBone("hand")
	fmt.Printf("hand at (%.1f, %.1f), done %v\n", hand.X, hand.Y, s.Done())
	// Output:
	// hand at (110.0, 100.0)
	// hand at (100.0, 90.0), done true
}

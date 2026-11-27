package rooms_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/rooms"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

const exampleMap = `version: 1
transition:
  kind: slide
  duration: 300ms
rooms:
  - id: entrance
    x: 0
    "y": 0
    width: 320
    height: 180
  - id: hall
    x: 320
    "y": 0
    width: 640
    height: 180
    transition:
      kind: fade
      color: '#102030'
`

// Example reads a room map, finds the room holding a point and writes the map back unchanged.
func Example() {
	m, err := rooms.Parse([]byte(exampleMap))
	if err != nil {
		panic(err)
	}
	r := m.RoomAt(core.Point{X: 400, Y: 90})
	fmt.Println(r.ID, r.Bounds())
	fmt.Println(m.TransitionInto(m.Room("entrance")).Kind, m.TransitionInto(r).Length())
	out, err := rooms.Encode(m)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(out) == exampleMap)
	// Output:
	// hall {{320 0} {640 180}}
	// slide 600ms
	// true
}

// ExampleDirector moves a camera from the entrance into the hall, which fades: the view is covered over the first half, the camera cuts at the midpoint and the view is uncovered over the second half.
func ExampleDirector() {
	m, err := rooms.Parse([]byte(exampleMap))
	if err != nil {
		panic(err)
	}
	cam := &tilemap.Camera{Width: 320, Height: 180}
	dir := rooms.NewDirector(m, cam)
	dir.Start(core.Point{X: 100, Y: 90})
	fmt.Println(dir.Room().ID, cam.X, cam.Y)

	player := core.Point{X: 400, Y: 90}
	fmt.Println(dir.Update(player, 150*time.Millisecond), dir.Room().ID, dir.FadeAlpha(), cam.X)
	dir.Update(player, 150*time.Millisecond)
	fmt.Println(dir.FadeAlpha(), cam.X)
	dir.Update(player, 300*time.Millisecond)
	fmt.Println(dir.Transitioning(), dir.FadeAlpha(), cam.X, cam.Y)
	// Output:
	// entrance 160 90
	// true hall 0.5 160
	// 1 480
	// false 0 480 90
}

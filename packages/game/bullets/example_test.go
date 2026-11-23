package bullets_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game/bullets"
	"github.com/nsgo-dev/game/packages/game/math2d"
)

func Example() {
	file, err := bullets.Parse([]byte(`version: 1
patterns:
  - name: bloom
    kind: ring
    frame: orb
    count: 12
    speed: 90
    spin: 15
    shots: 3
    interval: 200ms
    lifetime: 2s
    radius: 4
`))
	if err != nil {
		panic(err)
	}
	field := bullets.NewField(testAtlas(), 64)
	field.Bounds = math2d.Rect{W: 320, H: 240}
	bloom, _ := file.Pattern("bloom")
	boss, err := bullets.NewEmitter(field, bloom, 1)
	if err != nil {
		panic(err)
	}
	boss.X, boss.Y = 160, 60

	// Each fixed step: move the field, then fire; draw with field.Draw(d) in the canvas.
	for range 30 {
		field.Update(time.Second / 60)
		boss.Update(time.Second / 60)
	}
	fmt.Println(boss.Shots(), field.Len(), boss.Done())
	// Output: 3 36 true
}

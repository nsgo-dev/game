package impacts_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/impacts"
)

// Draw what goes behind a character and what goes in front of it separately, so the character can stand between them.
func ExampleGroundSlam() {
	e := impacts.GroundSlam
	p := pixelfx.NewPlayer(e, pixelfx.Options{X: 144, Y: 164})
	back := pixelfx.NewFrame(288, 208, 0, 0)
	front := pixelfx.NewFrame(288, 208, 0, 0)
	p.Update(0.3)
	p.Draw(back, pixelfx.LayerBack)
	p.Draw(front, pixelfx.LayerFront)
	drawn := func(f *pixelfx.Frame) bool {
		for _, c := range f.Color {
			if c != 0 {
				return true
			}
		}
		return false
	}
	fmt.Println(drawn(back), drawn(front))
	// Output: true true
}

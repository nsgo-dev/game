package weather_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/weather"
)

// Importing the package registers its effects; render rain frames for a sprite sheet.
func ExampleRain() {
	e, _ := pixelfx.Lookup("weather/rain")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == weather.Rain, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true true 240 320 180
}

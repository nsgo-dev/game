package rhythm_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game/rhythm"
)

// Example reads a beat map with a tempo change and converts between track time, beats and bars.
func Example() {
	m, err := rhythm.ParseBeatMap([]byte(`version: 1
offsetMs: 100
tempos:
  - beat: 0
    bpm: 120
  - beat: 8
    bpm: 60
`))
	if err != nil {
		panic(err)
	}
	timing, err := m.Timing()
	if err != nil {
		panic(err)
	}
	fmt.Println(timing.Time(8), timing.Time(10))
	fmt.Println(timing.Beat(5100 * time.Millisecond))
	bar, inBar := timing.Bar(10)
	fmt.Println(bar, inBar)
	// Output:
	// 4.1s 6.1s
	// 9
	// 2 2
}

// ExampleClock starts a clock when the track starts playing and judges a tap on a device whose audio offset is 80ms.
func ExampleClock() {
	m, _ := rhythm.ParseBeatMap([]byte("version: 1\ntempos: [{beat: 0, bpm: 120}]\n"))
	timing, _ := m.Timing()
	clock := rhythm.NewClock(timing)
	clock.SetOffsets(rhythm.Offsets{Audio: 80 * time.Millisecond})
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock.Start(start, 0)

	fmt.Println(clock.BeatTime(4).Sub(start))
	tap := start.Add(2100 * time.Millisecond)
	fmt.Println(clock.Judge(tap, 4))
	// Output:
	// 2s
	// 20ms
}

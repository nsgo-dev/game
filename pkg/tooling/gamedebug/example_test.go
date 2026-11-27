//go:build !release

package gamedebug_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
)

// A Console runs commands typed by the developer and tunes registered variables within their ranges.
func ExampleConsole() {
	c := gamedebug.NewConsole()
	gravity := 9.8
	c.RegisterFloat("gravity", 0, 50, &gravity)
	c.RegisterCommand("spawn", "spawn <kind>: add an enemy", func(args []string) (string, error) {
		return "spawned " + args[0], nil
	})

	out, err := c.Exec("set gravity 24.5")
	fmt.Println(out, err, gravity)
	out, err = c.Exec("spawn slime")
	fmt.Println(out, err)
	_, err = c.Exec("set gravity 99")
	fmt.Println(err != nil, gravity)
	fmt.Println(c.Complete("sp"))
	// Output:
	// gravity = 24.5 <nil> 24.5
	// spawned slime <nil>
	// true 24.5
	// [spawn]
}

// A Profiler times named scopes per frame and reports the costliest ones; SetNow gives it a clock, here one that advances by hand.
func ExampleProfiler() {
	now := time.Unix(0, 0)
	p := gamedebug.NewProfiler(0)
	p.SetNow(func() time.Time { return now })

	p.BeginFrame()
	physics := p.Begin("physics")
	now = now.Add(3 * time.Millisecond)
	p.End(physics)
	for range 2 {
		ai := p.Begin("ai")
		now = now.Add(time.Millisecond)
		p.End(ai)
	}
	p.EndFrame()

	for _, s := range p.TopScopes(2) {
		fmt.Println(s.Name, s.Total, s.Calls)
	}
	// Output:
	// physics 3ms 1
	// ai 2ms 2
}

// A MemoryTracker adds up decoded asset sizes per group and checks them against budgets.
func ExampleMemoryTracker() {
	m := gamedebug.NewMemoryTracker()
	m.SetBudget("level-1", 8<<20)
	m.Track("level-1", "tiles.png", gamedebug.AssetImage, gamedebug.EstimateImageBytes(1024, 1024))
	m.Track("level-1", "boss.png", gamedebug.AssetImage, gamedebug.EstimateImageBytes(1024, 1024))
	fmt.Println(m.GroupBytes("level-1"), m.CheckBudget("level-1") == nil)

	m.Track("level-1", "music.pcm", gamedebug.AssetAudio, gamedebug.EstimatePCMBytes(48000*60, 2, 2))
	fmt.Println(m.CheckBudget("level-1") == nil)
	m.ReleaseGroup("level-1")
	fmt.Println(m.TotalBytes())
	// Output:
	// 8388608 true
	// false
	// 0
}

// An Overlay's FrameStats keeps recent frame timings for its graph; RecordFrame adds one, as WrapLoop does for each frame of a game.Loop.
func ExampleOverlay() {
	o := gamedebug.New(gamedebug.Options{ToggleKey: "F3"})
	for _, ms := range []time.Duration{16, 17, 18} {
		o.RecordFrame(ms*time.Millisecond, 4*time.Millisecond, 8*time.Millisecond)
	}
	stats := o.Stats()
	fmt.Println(stats.Len(), stats.Average().Total, stats.MaxTotal())
	fmt.Println(o.Visible())
	o.Toggle()
	fmt.Println(o.Visible())
	// Output:
	// 3 17ms 18ms
	// false
	// true
}

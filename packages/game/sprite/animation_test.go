package sprite_test

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const ms = time.Millisecond

// frames returns an animation's frames, named by their index, lasting durations.
func frames(durations ...time.Duration) []sprite.AnimationFrame {
	f := make([]sprite.AnimationFrame, len(durations))
	for i, d := range durations {
		f[i] = sprite.AnimationFrame{Frame: sprite.Frame{Name: string(rune('a' + i)), W: 1, H: 1}, Duration: d}
	}
	return f
}

// steps updates a by dt n times and returns the frame index after each.
func steps(a *sprite.Animation, dt time.Duration, n int) []int {
	var at []int
	for range n {
		a.Update(dt)
		at = append(at, a.Index())
	}
	return at
}

func sameInts(t *testing.T, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("frames %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("frames %v, want %v", got, want)
		}
	}
}

func TestALoopingAnimationStartsOverAfterItsLastFrame(t *testing.T) {
	a := sprite.Animation{Frames: frames(30*ms, 20*ms, 50*ms), Loop: true}
	if a.Index() != 0 || a.Frame().Name != "a" {
		t.Fatalf("starts on frame %d %q, want 0 a", a.Index(), a.Frame().Name)
	}
	sameInts(t, steps(&a, 10*ms, 12), []int{0, 0, 1, 1, 2, 2, 2, 2, 2, 0, 0, 0})
	if a.Done() {
		t.Fatal("a looping animation is done")
	}
	if a.Frame().Name != "a" {
		t.Fatalf("frame %q, want a", a.Frame().Name)
	}
}

func TestAnAnimationOnceStopsOnItsLastFrame(t *testing.T) {
	a := sprite.Animation{Frames: frames(30*ms, 20*ms, 50*ms)}
	sameInts(t, steps(&a, 10*ms, 12), []int{0, 0, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2})
	if !a.Done() {
		t.Fatal("not done after showing its last frame for its duration")
	}

	a.Reset()
	sameInts(t, steps(&a, 10*ms, 9), []int{0, 0, 1, 1, 2, 2, 2, 2, 2})
	if a.Done() {
		t.Fatal("done before its last frame showed for its duration")
	}
	a.Update(10 * ms)
	if !a.Done() || a.Index() != 2 {
		t.Fatalf("done %v on frame %d, want done on 2", a.Done(), a.Index())
	}
}

func TestALongStepSkipsWholeRounds(t *testing.T) {
	a := sprite.Animation{Frames: frames(30*ms, 20*ms, 50*ms), Loop: true}
	// Ten rounds and 40 ms.
	a.Update(1040 * ms)
	if a.Index() != 1 {
		t.Fatalf("frame %d, want 1", a.Index())
	}
	b := sprite.Animation{Frames: frames(30*ms, 20*ms, 50*ms)}
	b.Update(time.Hour)
	if !b.Done() || b.Index() != 2 {
		t.Fatalf("done %v on frame %d, want done on 2", b.Done(), b.Index())
	}
}

func TestAnAnimationOfNoTimeOrNoFramesStaysPut(t *testing.T) {
	a := sprite.Animation{Frames: frames(0, 0), Loop: true}
	a.Update(10 * ms)
	if a.Index() != 0 {
		t.Fatalf("frame %d, want 0", a.Index())
	}
	var none sprite.Animation
	none.Update(10 * ms)
	if none.Frame() != (sprite.Frame{}) || none.Done() {
		t.Fatalf("an animation of no frames shows %+v, done %v", none.Frame(), none.Done())
	}
}

func TestATaggedAnimationPlaysOnTheLoopsSteps(t *testing.T) {
	atlas, err := sprite.LoadAtlas(heroImage, asepriteHash)
	if err != nil {
		t.Fatal(err)
	}
	// Each sprite plays its own copy.
	walk, fall := atlas.Animations["walk"], atlas.Animations["fall"]
	hero := sprite.NewSprite(walk.Frame())
	l := &game.Loop{Rate: 100, Update: func(dt time.Duration) {
		walk.Update(dt)
		fall.Update(dt)
		hero.Frame = walk.Frame()
	}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})
	// walk's frames last 100, 100, 150 and 200 ms, 550 ms a round: 1 s is 450 ms into the second round, on its last frame.
	app.Advance(time.Second)
	if walk.Index() != 3 || hero.Frame.Name != "hero 3.aseprite" {
		t.Fatalf("after 1 s: walk on frame %d, the hero on %q, want 3 and hero 3.aseprite", walk.Index(), hero.Frame.Name)
	}
	// fall shows frames 2, 1 and 0 for 150, 100 and 100 ms, once.
	if !fall.Done() || fall.Frame().Name != "hero 0.aseprite" {
		t.Fatalf("fall done %v on %q, want done on hero 0.aseprite", fall.Done(), fall.Frame().Name)
	}
	if atlas.Animations["walk"].Index() != 0 {
		t.Fatal("playing a copy moved the atlas's animation")
	}
}

func TestAnimationSpeedScalesAndReversesPlayback(t *testing.T) {
	// 2x speed: 30ms, 20ms, 50ms take 15ms, 10ms, 25ms (50ms round).
	fast := sprite.Animation{Frames: frames(30*ms, 20*ms, 50*ms), Loop: true, Speed: 2}
	sameInts(t, steps(&fast, 5*ms, 12), []int{0, 0, 1, 1, 2, 2, 2, 2, 2, 0, 0, 0})

	// 0.5x speed: 30ms, 20ms, 50ms take 60ms, 40ms, 100ms.
	slow := sprite.Animation{Frames: frames(30*ms, 20*ms, 50*ms), Loop: true, Speed: 0.5}
	sameInts(t, steps(&slow, 20*ms, 12), []int{0, 0, 1, 1, 2, 2, 2, 2, 2, 0, 0, 0})

	// Reverse looping (-1x): starts on frame 2 (50ms), then 1 (20ms), then 0 (30ms), wraps to 2.
	rev := sprite.Animation{Frames: frames(30*ms, 20*ms, 50*ms), Loop: true, Speed: -1}
	if rev.Index() != 2 || rev.Frame().Name != "c" {
		t.Fatalf("reverse starts on frame %d %q, want 2 c", rev.Index(), rev.Frame().Name)
	}
	sameInts(t, steps(&rev, 10*ms, 12), []int{2, 2, 2, 2, 1, 1, 0, 0, 0, 2, 2, 2})

	// Reverse one-shot stops on frame 0 and resets back to frame 2.
	revOnce := sprite.Animation{Frames: frames(30*ms, 20*ms, 50*ms), Speed: -1}
	sameInts(t, steps(&revOnce, 10*ms, 12), []int{2, 2, 2, 2, 1, 1, 0, 0, 0, 0, 0, 0})
	if !revOnce.Done() || revOnce.Index() != 0 {
		t.Fatalf("reverse once done %v on frame %d, want done on 0", revOnce.Done(), revOnce.Index())
	}
	revOnce.Reset()
	if revOnce.Done() || revOnce.Index() != 2 {
		t.Fatalf("after Reset: done %v on frame %d, want not done on 2", revOnce.Done(), revOnce.Index())
	}

	// Long step in reverse skips whole rounds cleanly: 10 rounds + 60ms = 50ms of frame 2 + 10ms into frame 1.
	revLong := sprite.Animation{Frames: frames(30*ms, 20*ms, 50*ms), Loop: true, Speed: -1}
	revLong.Update(1060 * ms)
	if revLong.Index() != 1 {
		t.Fatalf("reverse long step on frame %d, want 1", revLong.Index())
	}
}

func TestAnimationEventsFireDeterministicallyOnLoopSteps(t *testing.T) {
	type fired struct {
		step  int
		event string
		index int
	}
	var got []fired
	step := 0

	fs := frames(30*ms, 20*ms, 50*ms)
	fs[0].Event = "step_left"
	fs[1].Event = "hit"
	fs[2].Event = "spawn_vfx"

	a := sprite.Animation{
		Frames: fs,
		Loop:   true,
		OnEvent: func(event string, frameIndex int) {
			got = append(got, fired{step: step, event: event, index: frameIndex})
		},
	}

	l := &game.Loop{Rate: 100, Update: func(dt time.Duration) {
		step++
		a.Update(dt)
	}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})
	// 15 steps of 10ms = 150ms: enters 0 at step 1, 1 at step 3 (30ms), 2 at step 5 (50ms),
	// wraps to 0 at step 10 (100ms), 1 at step 13 (130ms), 2 at step 15 (150ms).
	app.Advance(150 * ms)

	want := []fired{
		{step: 1, event: "step_left", index: 0},
		{step: 3, event: "hit", index: 1},
		{step: 5, event: "spawn_vfx", index: 2},
		{step: 10, event: "step_left", index: 0},
		{step: 13, event: "hit", index: 1},
		{step: 15, event: "spawn_vfx", index: 2},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("events %+v, want %+v", got, want)
	}
}

func TestAnimationUpdateEventsCrossesMultipleFramesAndWrapsInOrder(t *testing.T) {
	fs := frames(30*ms, 20*ms, 50*ms)
	fs[0].Event = "footstep"
	fs[1].Event = "hit"
	fs[2].Event = "spawn_vfx"

	// Forward multi-frame crossing and wraparound in one step (140ms crosses 0 -> 1 -> 2 -> 0 -> 1).
	a := sprite.Animation{Frames: fs, Loop: true}
	if got, want := a.UpdateEvents(140*ms), []string{"footstep", "hit", "spawn_vfx", "footstep", "hit"}; !slices.Equal(got, want) {
		t.Fatalf("forward events %v, want %v", got, want)
	}
	if a.Index() != 1 {
		t.Fatalf("index %d, want 1", a.Index())
	}

	// Reverse multi-frame crossing and wraparound (140ms crosses 2 -> 1 -> 0 -> 2).
	rev := sprite.Animation{Frames: fs, Loop: true, Speed: -1}
	if got, want := rev.UpdateEvents(140*ms), []string{"spawn_vfx", "hit", "footstep", "spawn_vfx"}; !slices.Equal(got, want) {
		t.Fatalf("reverse events %v, want %v", got, want)
	}
	if rev.Index() != 2 {
		t.Fatalf("reverse index %d, want 2", rev.Index())
	}

	// One-shot animation fires each event once and fires nothing more after Done.
	once := sprite.Animation{Frames: fs}
	if got, want := once.UpdateEvents(200*ms), []string{"footstep", "hit", "spawn_vfx"}; !slices.Equal(got, want) {
		t.Fatalf("one-shot events %v, want %v", got, want)
	}
	if !once.Done() {
		t.Fatal("expected one-shot animation to be done")
	}
	if got := once.UpdateEvents(50 * ms); len(got) != 0 {
		t.Fatalf("events after done %v, want none", got)
	}
}

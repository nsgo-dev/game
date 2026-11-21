package sprite_test

import (
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

package main

import (
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/nsgo/core"
)

var (
	barColor    = core.RGB(0x020617)
	skyColor    = core.RGB(0x0b1026)
	groundColor = core.RGB(0x1e293b)
	laneColor   = core.RGBA(0x94a3b8, 0.08)
	basketColor = core.RGB(0xf59e0b)
	starColor   = core.RGB(0xfde047)
)

// viewport scales the virtual playfield to fit any window, with bars on the sides that do not match its shape.
var viewport = screen.Viewport{
	Width:    virtualWidth,
	Height:   virtualHeight,
	Mode:     screen.ModeFit,
	BarColor: barColor,
}

// The left and right halves of the playfield, in virtual points: a touch or a click in one moves the basket that way.
var (
	leftHalf  = core.Bounds{Size: core.Size{Width: virtualWidth / 2, Height: virtualHeight}}
	rightHalf = core.Bounds{Origin: core.Point{X: virtualWidth / 2}, Size: core.Size{Width: virtualWidth / 2, Height: virtualHeight}}
)

// starCatcher ties a world to the screen: a game.Loop steps it with the player's input, and a Canvas draws it.
type starCatcher struct {
	world   *world
	actions *input.Actions
	// size is the Canvas's size when it last drew, which input maps into virtual points.
	size core.Size
	// alpha is how far the current display frame falls between the last two steps.
	alpha float64
	// frame changes on every Draw of the loop, so the Canvas that reads it draws again.
	frame *core.Signal[uint64]
}

// Root is the game's root component.
func Root(cx *core.Scope) core.Node { return newStarCatcher(cx).view() }

func newStarCatcher(cx *core.Scope) *starCatcher {
	s := &starCatcher{
		world:   newWorld(),
		actions: input.NewActions(),
		frame:   core.NewSignal(cx, uint64(0)),
	}

	// Touches and clicks arrive in the view's points; the regions below are in the playfield's.
	s.actions.MapPoint = func(p core.Point) (core.Point, bool) { return viewport.ViewToGame(s.size, p) }
	s.actions.Bind("left", input.TouchRegion(leftHalf), input.MouseRegion(leftHalf, core.MouseButtonLeft))
	s.actions.Bind("right", input.TouchRegion(rightHalf), input.MouseRegion(rightHalf, core.MouseButtonLeft))

	// A key latches its action for the next step; held, it repeats.
	repeat := core.KeyOptions{Repeat: true}
	for _, key := range []string{"left", "a"} {
		cx.OnKeyWith(key, repeat, func() { s.actions.Trigger("left") })
	}
	for _, key := range []string{"right", "d"} {
		cx.OnKeyWith(key, repeat, func() { s.actions.Trigger("right") })
	}

	loop := &game.Loop{
		Rate:   60,
		Update: s.update,
		Draw: func(alpha float64) {
			s.alpha = alpha
			s.frame.Update(func(n uint64) uint64 { return n + 1 })
		},
	}
	loop.Run(cx)
	return s
}

// update samples input once and steps the world: each new press of a side moves the basket one lane.
func (s *starCatcher) update(dt time.Duration) {
	s.actions.Step()
	move := 0
	if s.actions.JustPressed("left") {
		move--
	}
	if s.actions.JustPressed("right") {
		move++
	}
	s.world.step(dt, move)
}

func (s *starCatcher) view() core.Node {
	v := core.View{
		TestID: "game-view",
		Style:  core.Style{FlexGrow: 1, FlexShrink: 1, Background: barColor},
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "Star Catcher playfield",
				Style: core.Style{
					Position: core.Absolute,
					Top:      core.Pt(0),
					Right:    core.Pt(0),
					Bottom:   core.Pt(0),
					Left:     core.Pt(0),
				},
				Draw: s.draw,
			},
		},
	}
	s.actions.Attach(&v)
	return v
}

func (s *starCatcher) draw(d *core.Drawing, size core.Size) {
	_ = s.frame.Get()
	s.size = size
	viewport.Draw(d, size, s.drawWorld)
}

// drawWorld draws the world in virtual points, placing what moves between its last two steps by alpha.
func (s *starCatcher) drawWorld(d *core.Drawing, _ core.Size) {
	w := s.world
	a := float32(s.alpha)
	d.SetFillColor(skyColor).FillRect(0, 0, virtualWidth, virtualHeight)
	d.SetFillColor(laneColor)
	for lane := 1; lane < laneCount; lane += 2 {
		d.FillRect(laneWidth*float32(lane), 0, laneWidth, virtualHeight-groundHeight)
	}
	d.SetFillColor(groundColor).FillRect(0, virtualHeight-groundHeight, virtualWidth, groundHeight)

	d.SetFillColor(basketColor).FillRect(w.basketAt(a)-basketWidth/2, basketY, basketWidth, basketHeight)

	d.SetFillColor(starColor).BeginPath().Ellipse(laneX(w.starLane), w.starAt(a), itemSize/2, itemSize/2).Fill()
}

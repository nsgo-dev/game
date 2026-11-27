package main

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

var (
	barColor    = core.RGB(0x020617)
	skyColor    = core.RGB(0x0b1026)
	groundColor = core.RGB(0x1e293b)
	laneColor   = core.RGBA(0x94a3b8, 0.08)
	textColor   = core.RGB(0xf8fafc)
	dimColor    = core.RGB(0x94a3b8)
	panelColor  = core.RGBA(0x0f172a, 0.85)
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

// starCatcher ties a world to the screen: a game.Loop steps it with the player's input, a Canvas draws it with sprites, and native views show the score.
type starCatcher struct {
	world   *world
	actions *input.Actions

	twinkle sprite.Animation
	rock    sprite.Frame
	basket  sprite.Sprite
	// items are the sprites of the falling items, reused from frame to frame.
	items []sprite.Sprite
	batch sprite.Batch

	// size is the Canvas's size when it last drew, which input maps into virtual points.
	size core.Size
	// alpha is how far the current display frame falls between the last two steps.
	alpha float64
	// frame changes on every Draw of the loop, so the Canvas that reads it draws again.
	frame *core.Signal[uint64]

	// score, lives and over mirror the world for the native views.
	score, lives *core.Signal[int]
	over         *core.Signal[bool]
}

// Root is the game's root component. Each launch seeds the game from the clock, so each plays differently.
func Root(cx *core.Scope) core.Node {
	return newStarCatcher(cx, uint64(cx.WallTime().UnixNano())).view()
}

func newStarCatcher(cx *core.Scope, seed uint64) *starCatcher {
	w := newWorld(seed)
	s := &starCatcher{
		world:   w,
		actions: input.NewActions(),
		frame:   core.NewSignal(cx, uint64(0)),
		score:   core.NewSignal(cx, w.score),
		lives:   core.NewSignal(cx, w.lives),
		over:    core.NewSignal(cx, w.over),
	}
	atlas := makeAtlas()
	s.twinkle = atlas.Animations["twinkle"]
	s.rock = atlas.Frames["rock"]
	s.basket = sprite.NewSprite(atlas.Frames["basket"])
	s.basket.OriginY = 0
	s.batch.Atlas = atlas

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
	for _, key := range []string{"enter", "space"} {
		cx.OnKey(key, func() { s.actions.Trigger("restart") })
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

// update samples input once and steps the world. While the game is over, any press starts the next one.
func (s *starCatcher) update(dt time.Duration) {
	s.actions.Step()
	left, right := s.actions.JustPressed("left"), s.actions.JustPressed("right")
	if s.world.over && (left || right || s.actions.JustPressed("restart")) {
		s.world.reset()
		s.sync()
		return
	}
	move := 0
	if left {
		move--
	}
	if right {
		move++
	}
	s.world.step(dt, move)
	s.twinkle.Update(dt)
	if len(s.world.events) > 0 {
		s.sync()
	}
}

// sync copies what the native views show from the world.
func (s *starCatcher) sync() {
	s.score.Set(s.world.score)
	s.lives.Set(s.world.lives)
	s.over.Set(s.world.over)
}

func (s *starCatcher) view() core.Node {
	field := core.View{
		TestID: "game-view",
		Style:  fill(),
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "Star Catcher playfield",
				Style:  fill(),
				Draw:   s.draw,
			},
		},
	}
	s.actions.Attach(&field)
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: barColor},
		Children: []core.Node{
			field,
			s.hud(),
			core.Show{
				When: s.over.Get,
				Then: func(*core.Scope) core.Node { return s.gameOver() },
			},
		},
	}
}

// hud is the score and the lives along the top, which touches pass through to the playfield.
func (s *starCatcher) hud() core.Node {
	label := core.Style{Color: textColor, FontSize: 16, FontWeight: core.FontWeightBold}
	return core.View{
		PointerEvents: core.PointerEventsNone,
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Left:           core.Pt(0),
			Right:          core.Pt(0),
			Direction:      core.Row,
			JustifyContent: core.JustifySpaceBetween,
			Padding:        core.All(12),
			PaddingEnv:     core.EnvAll(core.EnvSafeArea),
		},
		Children: []core.Node{
			core.Text{TestID: "score", ContentFunc: func() string { return fmt.Sprintf("Score: %d", s.score.Get()) }, Style: label},
			core.Text{TestID: "lives", ContentFunc: func() string { return fmt.Sprintf("Lives: %d", s.lives.Get()) }, Style: label},
		},
	}
}

// gameOver is the panel shown once the last life goes.
func (s *starCatcher) gameOver() core.Node {
	return core.View{
		TestID:        "game-over",
		PointerEvents: core.PointerEventsNone,
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Left:           core.Pt(0),
			Right:          core.Pt(0),
			Bottom:         core.Pt(0),
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Background: panelColor, CornerRadius: 12, Padding: core.Symmetric(16, 24), AlignItems: core.AlignCenter, Gap: 6},
				Children: []core.Node{
					core.Text{Content: "Game over", Style: core.Style{Color: textColor, FontSize: 24, FontWeight: core.FontWeightBold}},
					core.Text{Content: "Tap or press Enter to play again", Style: core.Style{Color: dimColor, FontSize: 13}},
				},
			},
		},
	}
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
		d.FillRect(laneWidth*float32(lane), 0, laneWidth, groundY)
	}
	d.SetFillColor(groundColor).FillRect(0, groundY, virtualWidth, groundHeight)

	if cap(s.items) < len(w.items) {
		s.items = make([]sprite.Sprite, len(w.items), 2*len(w.items))
	}
	s.items = s.items[:len(w.items)]
	s.batch.Sprites = append(s.batch.Sprites[:0], &s.basket)
	for i, it := range w.items {
		f := s.twinkle.Frame()
		if it.kind == kindRock {
			f = s.rock
		}
		sp := &s.items[i]
		*sp = sprite.NewSprite(f)
		sp.X, sp.Y = laneX(it.lane), it.at(a)
		// Items turn a little as they fall.
		sp.Rotation = sp.Y / 60
		s.batch.Sprites = append(s.batch.Sprites, sp)
	}
	s.basket.X, s.basket.Y = w.basketAt(a), basketY
	s.batch.Draw(d)
}

// fill is the style of a child that covers its whole parent.
func fill() core.Style {
	return core.Style{
		Position: core.Absolute,
		Top:      core.Pt(0),
		Right:    core.Pt(0),
		Bottom:   core.Pt(0),
		Left:     core.Pt(0),
	}
}

package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

const (
	spriteSize  = 48
	gridSpacing = 48
	initialVX   = 180
	initialVY   = 140
	spinSpeed   = 1.2
)

// Root is the game's root NSGo component: a full-screen Canvas driven by a
// fixed-step game.Loop, with a native HUD layered on top.
func Root(cx *core.Scope) core.Node {
	desktop.SetTitle(cx, Project.Title())
	score := core.NewSignal(cx, 0)
	frame := core.NewSignal(cx, uint64(0))
	g := newStarterGame(score)

	loop := &game.Loop{
		Rate:   60,
		Update: g.update,
		Draw: func(alpha float64) {
			g.alpha = alpha
			frame.Update(func(n uint64) uint64 { return n + 1 })
		},
	}
	loop.Run(cx)

	return core.View{
		Style: core.Style{
			FlexGrow:   1,
			FlexShrink: 1,
			Background: core.RGB(0x0f172a),
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "Game arena",
				Style: core.Style{
					Position: core.Absolute,
					Top:      core.Pt(0),
					Right:    core.Pt(0),
					Bottom:   core.Pt(0),
					Left:     core.Pt(0),
				},
				OnPress: func(core.PressEvent) { g.tap() },
				Draw: func(d *core.Drawing, size core.Size) {
					_ = frame.Get()
					g.draw(d, size)
				},
			},
			hudOverlay(score, g.reset),
		},
	}
}

// hudOverlay renders native NSGo controls above the Canvas while letting taps
// outside the controls pass through to the arena.
func hudOverlay(score *core.Signal[int], onReset func()) core.Node {
	return core.View{
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			Position:       core.Absolute,
			Top:            core.Pt(0),
			Left:           core.Pt(0),
			Right:          core.Pt(0),
			Direction:      core.Row,
			JustifyContent: core.JustifySpaceBetween,
			AlignItems:     core.AlignCenter,
			Padding:        core.All(16),
			PaddingEnv:     core.EnvAll(core.EnvSafeArea),
		},
		Children: []core.Node{
			core.View{
				PointerEvents: core.PointerEventsNone,
				Style: core.Style{
					Background:   core.RGBA(0x1e293b, 0.85),
					CornerRadius: 10,
					Padding:      core.Symmetric(8, 14),
					Gap:          2,
				},
				Children: []core.Node{
					core.Text{
						TestID:      "score",
						ContentFunc: func() string { return fmt.Sprintf("Score: %d", score.Get()) },
						Style: core.Style{
							Color:      core.RGB(0xf8fafc),
							FontSize:   18,
							FontWeight: core.FontWeightBold,
						},
					},
					core.Text{
						Content: "Tap anywhere to reverse",
						Style: core.Style{
							Color:    core.RGB(0x94a3b8),
							FontSize: 12,
						},
					},
				},
			},
			base.Button{
				OnPress: func(core.PressEvent) { onReset() },
				Label:   "Reset",
				Style: core.Style{
					Background:   core.RGBA(0x0284c7, 0.9),
					CornerRadius: 10,
					Padding:      core.Symmetric(10, 16),
					Active:       &core.Style{Opacity: core.Alpha(0.8)},
				},
				Children: []core.Node{
					core.Text{
						Content: "Reset",
						Style: core.Style{
							Color:      core.RGB(0xffffff),
							FontSize:   14,
							FontWeight: core.FontWeightBold,
						},
					},
				},
			},
		},
	}
}

type starterGame struct {
	width, height float32
	placed        bool

	prevX, prevY float32
	x, y         float32
	vx, vy       float32
	prevRot, rot float32
	flipX        bool
	alpha        float64

	anim  sprite.Animation
	hero  sprite.Sprite
	batch *sprite.Batch
	score *core.Signal[int]
}

func newStarterGame(score *core.Signal[int]) *starterGame {
	atlas := makeAtlas()
	anim := atlas.Animations["pulse"]
	hero := sprite.NewSprite(anim.Frame())
	hero.OriginX = spriteSize / 2
	hero.OriginY = spriteSize / 2

	g := &starterGame{
		vx:    initialVX,
		vy:    initialVY,
		anim:  anim,
		hero:  hero,
		score: score,
	}
	g.batch = &sprite.Batch{
		Atlas:   atlas,
		Sprites: []*sprite.Sprite{&g.hero},
	}
	return g
}

func (g *starterGame) resize(w, h float32) {
	if w <= 0 || h <= 0 {
		return
	}
	g.width, g.height = w, h
	if !g.placed {
		g.center()
		g.placed = true
		return
	}
	g.clamp()
}

func (g *starterGame) center() {
	g.x, g.y = g.width/2, g.height/2
	g.prevX, g.prevY = g.x, g.y
}

func (g *starterGame) clamp() {
	half := float32(spriteSize) / 2
	if g.width > spriteSize {
		g.x = clampf(g.x, half, g.width-half)
	}
	if g.height > spriteSize {
		g.y = clampf(g.y, half, g.height-half)
	}
}

func (g *starterGame) reset() {
	g.center()
	g.vx = initialVX
	g.vy = initialVY
	g.prevRot, g.rot = 0, 0
	g.flipX = false
	g.anim.Reset()
	g.hero.Frame = g.anim.Frame()
	if g.score != nil {
		g.score.Set(0)
	}
}

func (g *starterGame) tap() {
	g.vx = -g.vx
	g.flipX = g.vx < 0
	if g.score != nil {
		g.score.Update(func(n int) int { return n + 1 })
	}
}

func (g *starterGame) update(dt time.Duration) {
	secs := float32(dt.Seconds())
	g.prevX, g.prevY = g.x, g.y
	g.prevRot = g.rot

	g.x += g.vx * secs
	g.y += g.vy * secs
	g.rot += spinSpeed * secs

	half := float32(spriteSize) / 2
	if g.width > spriteSize {
		if g.x < half {
			g.x = half
			g.vx = absf(g.vx)
		} else if g.x > g.width-half {
			g.x = g.width - half
			g.vx = -absf(g.vx)
		}
	}
	if g.height > spriteSize {
		if g.y < half {
			g.y = half
			g.vy = absf(g.vy)
		} else if g.y > g.height-half {
			g.y = g.height - half
			g.vy = -absf(g.vy)
		}
	}

	g.flipX = g.vx < 0
	g.anim.Update(dt)
	g.hero.Frame = g.anim.Frame()
}

func (g *starterGame) draw(d *core.Drawing, size core.Size) {
	w, h := float32(size.Width), float32(size.Height)
	g.resize(w, h)

	// Arena background and subtle grid.
	d.SetFillColor(core.RGB(0x0f172a)).BeginPath().Rect(0, 0, w, h).Fill()
	d.SetStrokeColor(core.RGBA(0x94a3b8, 0.12)).SetLineWidth(1).BeginPath()
	for x := float32(gridSpacing); x < w; x += gridSpacing {
		d.MoveTo(x, 0).LineTo(x, h)
	}
	for y := float32(gridSpacing); y < h; y += gridSpacing {
		d.MoveTo(0, y).LineTo(w, y)
	}
	d.Stroke()

	// Interpolate between the last two fixed steps by alpha for smooth motion.
	a := float32(g.alpha)
	g.hero.X = lerpf(g.prevX, g.x, a)
	g.hero.Y = lerpf(g.prevY, g.y, a)
	g.hero.Rotation = lerpf(g.prevRot, g.rot, a)
	g.hero.FlipX = g.flipX
	g.batch.Draw(d)
}

// makeAtlas builds a two-frame procedural sprite sheet so the starter runs with
// no external image files. Replace it with sprite.LoadAtlas when you add art.
func makeAtlas() *sprite.Atlas {
	sheet := image.NewRGBA(image.Rect(0, 0, spriteSize*2, spriteSize))
	drawOrbFrame(sheet, 0, 0.38)
	drawOrbFrame(sheet, spriteSize, 0.50)

	f0 := sprite.Frame{Name: "orb_0", X: 0, Y: 0, W: spriteSize, H: spriteSize, Width: spriteSize, Height: spriteSize}
	f1 := sprite.Frame{Name: "orb_1", X: spriteSize, Y: 0, W: spriteSize, H: spriteSize, Width: spriteSize, Height: spriteSize}

	return &sprite.Atlas{
		Image: core.ImageSource{Pixels: sheet},
		Frames: map[string]sprite.Frame{
			f0.Name: f0,
			f1.Name: f1,
		},
		Animations: map[string]sprite.Animation{
			"pulse": {
				Loop: true,
				Frames: []sprite.AnimationFrame{
					{Frame: f0, Duration: 280 * time.Millisecond},
					{Frame: f1, Duration: 280 * time.Millisecond},
				},
			},
		},
	}
}

func drawOrbFrame(dst *image.RGBA, offsetX int, coreRatio float64) {
	center := float64(spriteSize-1) / 2
	radius := float64(spriteSize)/2 - 2
	for y := range spriteSize {
		for x := range spriteSize {
			dx := float64(x) - center
			dy := float64(y) - center
			dist := math.Hypot(dx, dy)
			if dist > radius {
				continue
			}
			// Soft outer glow, cyan body, bright pulsing core, and directional eye dot so FlipX is visible.
			t := dist / radius
			var c color.NRGBA
			switch {
			case math.Hypot(dx-radius*0.28, dy+radius*0.18) < radius*0.16:
				c = color.NRGBA{R: 0x0f, G: 0x17, B: 0x2a, A: 0xff}
			case t < coreRatio:
				c = color.NRGBA{R: 0xe0, G: 0xf2, B: 0xfe, A: 0xff}
			case t < 0.85:
				c = color.NRGBA{R: 0x38, G: 0xbd, B: 0xf8, A: 0xff}
			default:
				alpha := uint8(255 * (1 - (t-0.85)/0.15))
				c = color.NRGBA{R: 0x02, G: 0x84, B: 0xc7, A: alpha}
			}
			dst.Set(offsetX+x, y, c)
		}
	}
}

func lerpf(a, b, t float32) float32 { return a + (b-a)*t }

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

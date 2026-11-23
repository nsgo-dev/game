package main

import "github.com/nsgo-dev/nsgo/core"

// groundHeight is how tall the strip of ground at the bottom of the playfield is, in points.
const groundHeight = 24

var (
	skyColor    = core.RGB(0x0b1026)
	groundColor = core.RGB(0x1e293b)
	textColor   = core.RGB(0xf8fafc)
)

// Root is the game's root component: a Canvas that fills the window, with the game's title in a native Text above it.
func Root(cx *core.Scope) core.Node {
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: skyColor},
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "Star Catcher playfield",
				Style:  fill(),
				Draw:   drawPlayfield,
			},
			core.View{
				PointerEvents: core.PointerEventsNone,
				Style: core.Style{
					Position:   core.Absolute,
					Top:        core.Pt(0),
					Left:       core.Pt(0),
					Right:      core.Pt(0),
					AlignItems: core.AlignCenter,
					Padding:    core.All(24),
					PaddingEnv: core.EnvAll(core.EnvSafeArea),
				},
				Children: []core.Node{
					core.Text{
						TestID:  "title",
						Content: "Star Catcher",
						Style:   core.Style{Color: textColor, FontSize: 28, FontWeight: core.FontWeightBold},
					},
				},
			},
		},
	}
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

// drawPlayfield paints the night sky and the ground, in the canvas's own points.
func drawPlayfield(d *core.Drawing, size core.Size) {
	w, h := float32(size.Width), float32(size.Height)
	d.SetFillColor(skyColor).FillRect(0, 0, w, h)
	d.SetFillColor(groundColor).FillRect(0, h-groundHeight, w, groundHeight)
}

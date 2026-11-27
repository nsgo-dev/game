package main

import (
	"fmt"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/nsgo/core"
)

// Controller connects a Scene to NSGo's view tree through a fixed-step game.Loop.
type Controller struct {
	// Scene is the particle scene the loop steps and the canvas draws.
	Scene *Scene
	// Loop steps Scene at StepRate and redraws the canvas every display frame.
	Loop *game.Loop

	frame *core.Signal[uint64]
	count *core.Signal[int]
}

// Root is Fountain's NSGo root view.
func Root(cx *core.Scope) core.Node {
	_, node := NewView(cx, DefaultSeed)
	return node
}

// NewView builds the scene for seed, starts its loop and returns the controller with the view that shows it.
func NewView(cx *core.Scope, seed uint64) (*Controller, core.Node) {
	c := &Controller{
		Scene: NewScene(seed),
		frame: core.NewSignal(cx, uint64(0)),
		count: core.NewSignal(cx, 0),
	}
	c.Loop = &game.Loop{
		Rate:   StepRate,
		Update: c.Scene.Step,
		Draw: func(float64) {
			c.frame.Set(c.frame.GetUntracked() + 1)
			if n := c.Scene.Count(); n != c.count.GetUntracked() {
				c.count.Set(n)
			}
		},
	}
	c.Loop.Run(cx)

	keys := core.KeyOptions{When: core.KeyAlways, Repeat: true}
	cx.OnKeyWith("up", keys, func() { c.Scene.SetTarget(c.Scene.Target + TargetStep) })
	cx.OnKeyWith("down", keys, func() { c.Scene.SetTarget(c.Scene.Target - TargetStep) })
	cx.OnKeyWith("r", core.KeyOptions{When: core.KeyAlways}, c.Scene.Reset)

	return c, c.view(cx)
}

func (c *Controller) view(cx *core.Scope) core.Node {
	return core.View{
		TestID: "fountain-root",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: core.RGB(0x05070D),
			Overflow:   core.Hidden,
		},
		Children: []core.Node{
			core.Canvas{
				TestID: "fountain-canvas",
				Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = c.frame.Get()
					c.Scene.Draw(d, size)
				},
			},
			core.View{
				PointerEvents: core.PointerEventsNone,
				StyleFunc: func() core.Style {
					safe := cx.Env().SafeArea()
					return core.Style{
						Position:     core.Absolute,
						Top:          core.Pt(max(core.Points(12), safe.Top+8)),
						Left:         core.Pt(max(core.Points(12), safe.Left+8)),
						Padding:      core.Edges{Top: 6, Right: 10, Bottom: 6, Left: 10},
						Background:   core.RGBA(0x0B1221, 0.84),
						CornerRadius: 6,
					}
				},
				Children: []core.Node{
					core.Text{
						TestID:      "hud-count",
						ContentFunc: func() string { return fmt.Sprintf("%d particles", c.count.Get()) },
						Style: core.Style{
							FontFamily:  core.FontMonospace,
							FontSize:    12,
							FontWeight:  core.FontWeightSemibold,
							FontVariant: core.FontTabularNums,
							Color:       core.RGB(0xE2E8F0),
						},
					},
				},
			},
		},
	}
}

package main

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/ui"
)

// Controller connects a Ward to NSGo's view tree through a fixed-step game.Loop.
type Controller struct {
	// Ward is the game's state, which the loop steps and the canvas draws.
	Ward *Ward
	// Loop steps Ward at StepRate and redraws the canvas every display frame.
	Loop *game.Loop

	frame *core.Signal[uint64]
	size  core.Size
}

// Root is Field Medic's NSGo root view, on the ward game.yaml's main scene holds.
func Root(cx *core.Scope) core.Node {
	desktop.SetTitle(cx, gameProject.Title())
	w, err := loadMainWard()
	if err != nil {
		return core.View{
			Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Background: core.RGB(0x111827)},
			Children: []core.Node{
				core.Text{TestID: "load-error", Content: fmt.Sprintf("Field Medic failed to load its ward: %v", err), Style: core.Style{Color: core.RGB(0xF87171), FontSize: 14}},
			},
		}
	}
	_, node := NewView(cx, w)
	return node
}

// NewView starts w's loop on cx and returns the controller with the view that shows it. Pressing a patient heals it; R, or Restart once every patient is lost, starts the ward again.
func NewView(cx *core.Scope, w *Ward) (*Controller, core.Node) {
	c := &Controller{Ward: w, frame: core.NewSignal(cx, uint64(0))}
	c.Loop = &game.Loop{
		Rate:   StepRate,
		Update: w.Step,
		Draw:   func(float64) { c.frame.Set(c.frame.GetUntracked() + 1) },
	}
	c.Loop.Run(cx)
	cx.OnKeyWith("r", core.KeyOptions{When: core.KeyAlways}, c.Restart)
	return c, c.view()
}

// Press heals the patient at a point of the canvas, as a press on the canvas does.
func (c *Controller) Press(at core.Point) {
	x, y := c.Ward.ToScene(c.size, at)
	if c.Ward.HealAt(x, y) {
		c.redraw()
	}
}

// Restart resets the ward.
func (c *Controller) Restart() {
	c.Ward.Reset()
	c.redraw()
}

func (c *Controller) redraw() { c.frame.Set(c.frame.GetUntracked() + 1) }

func (c *Controller) view() core.Node {
	hud := core.Style{FontFamily: core.FontMonospace, FontSize: 13, FontWeight: core.FontWeightSemibold, FontVariant: core.FontTabularNums, Color: core.RGB(0xE2E8F0)}
	return core.View{
		TestID: "fieldmedic-root",
		Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100), Background: core.RGB(0x05070D), Overflow: core.Hidden},
		Children: []core.Node{
			core.Canvas{
				TestID:   "ward-canvas",
				Style:    core.Style{Width: core.Pct(100), Height: core.Pct(100)},
				Label:    "Ward",
				OnLayout: func(b core.Bounds) { c.size = b.Size },
				OnPress:  func(e core.PressEvent) { c.Press(e.Position) },
				Draw: func(d *core.Drawing, size core.Size) {
					_ = c.frame.Get()
					c.size = size
					c.Ward.Draw(d, size)
				},
			},
			core.View{
				PointerEvents: core.PointerEventsNone,
				Style:         core.Style{Position: core.Absolute, Top: core.Pt(12), Left: core.Pt(12), Padding: core.Edges{Top: 6, Right: 10, Bottom: 6, Left: 10}, Background: core.RGBA(0x0B1221, 0.84), CornerRadius: 6},
				Children: []core.Node{
					core.Text{
						TestID: "hud",
						Style:  hud,
						ContentFunc: func() string {
							_ = c.frame.Get()
							return fmt.Sprintf("%d s  lost %d of %d", int(c.Ward.Survived().Seconds()), c.Ward.LostCount(), len(c.Ward.Patients))
						},
					},
				},
			},
			core.Show{
				When: func() bool {
					_ = c.frame.Get()
					return c.Ward.Over()
				},
				Then: func(*core.Scope) core.Node {
					return core.View{
						TestID: "game-over",
						Style:  core.Style{Position: core.Absolute, Top: core.Pt(0), Left: core.Pt(0), Width: core.Pct(100), Height: core.Pct(100), AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 12, Background: core.RGBA(0x05070D, 0.7)},
						Children: []core.Node{
							core.Text{
								Style: core.Style{FontSize: 22, FontWeight: core.FontWeightBold, Color: core.RGB(0xF8FAFC)},
								ContentFunc: func() string {
									_ = c.frame.Get()
									return fmt.Sprintf("Every patient lost after %d s", int(c.Ward.Survived().Seconds()))
								},
							},
							ui.Button{TestID: "restart", Text: "Restart", OnPress: c.Restart},
						},
					}
				},
			},
		},
	}
}

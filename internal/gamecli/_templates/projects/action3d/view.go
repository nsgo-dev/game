package main

import (
	"fmt"
	"time"

	gamelib "github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/gpu"
)

// The input map's action and axis names, which game.yaml binds to keys and game controller buttons and sticks, and the on-screen controls bind to touch.
const (
	actJump    = "jump"
	actAttack  = "attack"
	actRestart = "restart"
	axisMoveX  = "moveX"
	axisMoveY  = "moveY"
	axisTurn   = "turn"
	axisStickX = "stickX"
	axisStickY = "stickY"
	axisLookX  = "lookX"
)

// dragTurn is how far dragging the view with the mouse turns the camera, in radians per point.
const dragTurn = 0.008

// Root is the game's root NSGo component: the 3D view, the HUD and the on-screen touch controls.
func Root(cx *core.Scope) core.Node {
	_, node := newPlay(cx)
	return node
}

// play is the running game on screen: the simulation, the fixed-step loop that drives it, the input it reads and the renderer that draws it.
type play struct {
	g       *game
	actions *input.Actions
	pad     *input.VirtualPad
	move    *input.VirtualStick
	look    *input.VirtualStick
	buttons []*input.VirtualButton
	render  renderer
	loop    *gamelib.Loop
	alpha   float32

	hud       *core.Signal[hudState]
	touched   *core.Signal[bool]
	padTick   *core.Signal[int]
	gpuStatus *core.Signal[string]

	dragging  bool
	lastMouse core.Point
}

// newPlay loads the game's content and builds its view, returning the play so tests can watch it.
func newPlay(cx *core.Scope) (*play, core.Node) {
	desktop.SetTitle(cx, Project.Title())
	c, err := loadContent()
	if err != nil {
		return nil, errorView(err)
	}
	g, err := newGame(c.actors, c.level, c.reg)
	if err != nil {
		return nil, errorView(err)
	}
	p := &play{
		g:         g,
		actions:   input.NewActions(),
		hud:       core.NewSignal(cx, g.hud()),
		touched:   core.NewSignal(cx, false),
		padTick:   core.NewSignal(cx, 0),
		gpuStatus: core.NewSignal(cx, ""),
	}
	g.onChange = func() { p.hud.Set(g.hud()) }
	Project.BindInput(p.actions)
	p.actions.WatchKeys(cx)
	p.actions.WatchGamepads(cx)
	p.pad = input.NewVirtualPad()
	p.move = p.pad.AddStick(&input.VirtualStick{AxisXName: axisMoveX, AxisYName: axisMoveY, Radius: 56, DeadZone: 0.12, Floating: true})
	p.look = p.pad.AddStick(&input.VirtualStick{AxisXName: axisTurn, Radius: 56, DeadZone: 0.12, Floating: true})
	p.buttons = []*input.VirtualButton{
		p.pad.AddButton(&input.VirtualButton{Action: actAttack, Label: "Attack", Radius: 42}),
		p.pad.AddButton(&input.VirtualButton{Action: actJump, Label: "Jump", Radius: 34}),
	}
	p.pad.BindActions(p.actions)

	p.loop = &gamelib.Loop{Rate: stepRate, Update: p.update, Draw: func(alpha float64) { p.alpha = float32(alpha) }}
	p.loop.Run(cx)
	return p, p.view(cx)
}

// update runs one fixed step: it reads the devices into controls and steps the game.
func (p *play) update(dt time.Duration) {
	a := p.actions
	a.Step()
	if a.JustPressed(actRestart) {
		p.restart()
		return
	}
	p.g.step(controls{
		moveX:  float32(clamp1(a.Axis(axisMoveX) + a.Axis(axisStickX))),
		moveY:  float32(clamp1(a.Axis(axisMoveY) + a.Axis(axisStickY))),
		turn:   float32(clamp1(a.Axis(axisTurn) + a.Axis(axisLookX))),
		jump:   a.JustPressed(actJump),
		attack: a.JustPressed(actAttack),
	}, dt)
}

// restart starts the run over.
func (p *play) restart() {
	// The level has already been built once, so reset cannot fail.
	_ = p.g.reset()
	p.hud.Set(p.g.hud())
}

// layoutPad places the on-screen controls for a view of size: the movement stick anywhere on the left half, the camera stick anywhere on the right half, and the buttons at the bottom right.
func (p *play) layoutPad(size core.Size) {
	w, h := size.Width, size.Height
	p.move.Zone = core.Bounds{Size: core.Size{Width: w / 2, Height: h}}
	p.move.Center = core.Point{X: 96, Y: h - 96}
	p.look.Zone = core.Bounds{Origin: core.Point{X: w / 2}, Size: core.Size{Width: w / 2, Height: h}}
	p.look.Center = core.Point{X: w * 3 / 4, Y: h / 2}
	p.buttons[0].Center = core.Point{X: w - 76, Y: h - 84}
	p.buttons[1].Center = core.Point{X: w - 170, Y: h - 56}
	if !p.move.Active() {
		p.move.Reset()
	}
}

func (p *play) onTouch(ev core.TouchEvent) {
	p.pad.OnTouch(ev)
	if !p.touched.GetUntracked() {
		p.touched.Set(true)
	}
	p.padTick.Update(func(n int) int { return n + 1 })
}

func (p *play) onMouseDown(ev core.MouseButtonEvent) {
	if ev.Button == core.MouseButtonLeft {
		p.dragging, p.lastMouse = true, ev.Position
	}
}

func (p *play) onMouseMove(ev core.MouseEvent) {
	// A button released outside the window sends this view no mouse up, so a move without the left button held ends the drag.
	if p.dragging && ev.PressedButton != core.MouseButtonLeft {
		p.dragging = false
	}
	if p.dragging {
		p.g.orbit(-float32(ev.Position.X-p.lastMouse.X) * dragTurn)
		p.lastMouse = ev.Position
	}
}

func (p *play) onMouseUp(ev core.MouseButtonEvent) {
	if ev.Button == core.MouseButtonLeft {
		p.dragging = false
	}
}

func (p *play) view(cx *core.Scope) core.Node {
	root := core.View{
		TestID:      "game-view",
		Style:       core.Style{FlexGrow: 1, FlexShrink: 1, Background: skyColor, Overflow: core.Hidden},
		OnTouch:     p.onTouch,
		OnMouseDown: p.onMouseDown,
		OnMouseMove: p.onMouseMove,
		OnMouseUp:   p.onMouseUp,
		OnLayout:    func(b core.Bounds) { p.layoutPad(b.Size) },
		Children: []core.Node{
			gpu.View(cx, gpu.ViewProps{
				TestID:          "game-gpu",
				DepthFormat:     gpu.FormatDepth24Plus,
				RetainResources: true,
				Style:           fill(),
				OnReady: func(dev *gpu.Device) {
					if err := p.render.setup(cx, dev, p.g); err != nil {
						p.gpuStatus.Set(fmt.Sprintf("The 3D view could not start: %v", err))
						return
					}
					// A device that came back after a failure or a lost device draws again, so the panel saying it could not goes away.
					p.gpuStatus.Set("")
				},
				OnFrame: func(frame *gpu.Frame) {
					if err := p.render.draw(frame, p.g, p.alpha); err != nil {
						p.gpuStatus.Set(fmt.Sprintf("The 3D view stopped drawing: %v", err))
					}
				},
				OnError: func(err gpu.Error) {
					if err.Code == gpu.ErrUnsupported {
						p.gpuStatus.Set("This device has no GPU the 3D view can draw with.")
						return
					}
					p.gpuStatus.Set(fmt.Sprintf("GPU error: %v", err))
				},
			}),
			core.View{PointerEvents: core.PointerEventsNone, Style: fill(), Children: []core.Node{core.Canvas{
				TestID: "touch-controls",
				Label:  "Touch controls",
				Style:  fill(),
				Draw: func(d *core.Drawing, _ core.Size) {
					_ = p.padTick.Get()
					if p.touched.Get() {
						p.move.Draw(d, input.VirtualStyle{})
						if p.look.Active() {
							p.look.Draw(d, input.VirtualStyle{})
						}
						for _, b := range p.buttons {
							b.Draw(d, input.VirtualStyle{})
						}
					}
				},
			}}},
			p.hudView(),
		},
	}
	p.actions.Attach(&root)
	return root
}

// hudView is the health, key and enemy count at the top, what to do next, and the banner and restart button at the end of a run.
func (p *play) hudView() core.Node {
	label := func(id string, content func() string, size core.Points) core.Node {
		return core.Text{TestID: id, ContentFunc: content, Style: core.Style{Color: core.RGB(0xf8fafc), FontSize: size, FontWeight: core.FontWeightSemibold}}
	}
	panel := core.Style{Background: core.RGBA(0x0f172a, 0.72), CornerRadius: 10, Padding: core.Symmetric(8, 12), Gap: 4}
	return core.View{
		PointerEvents: core.PointerEventsBoxNone,
		Style:         core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0), Padding: core.All(12), PaddingEnv: core.EnvAll(core.EnvSafeArea), Gap: 10},
		Children: []core.Node{
			core.View{
				PointerEvents: core.PointerEventsNone,
				Style:         core.Style{Direction: core.Row, Gap: 10, AlignItems: core.AlignStart},
				Children: []core.Node{
					core.View{Style: panel, Children: []core.Node{
						label("health", func() string {
							return fmt.Sprintf("Health %d/%d", p.hud.Get().health, p.g.actors.Hero.Health)
						}, 16),
						label("key", func() string {
							if p.hud.Get().key {
								return "Key found"
							}
							return "No key"
						}, 13),
						label("enemies", func() string { return fmt.Sprintf("Enemies %d", p.hud.Get().enemies) }, 13),
					}},
					core.View{Style: panel, Children: []core.Node{
						label("objective", func() string {
							_ = p.hud.Get()
							return p.g.objective()
						}, 14),
					}},
				},
			},
			core.Dynamic(func(*core.Scope) core.Node {
				if msg := p.gpuStatus.Get(); msg != "" {
					return core.View{PointerEvents: core.PointerEventsNone, Style: panel, Children: []core.Node{label("gpu-status", func() string { return msg }, 13)}}
				}
				return nil
			}),
			core.Dynamic(func(*core.Scope) core.Node {
				ph := p.hud.Get().phase
				if ph == phasePlaying {
					return nil
				}
				title := "Victory"
				if ph == phaseLost {
					title = "Defeated"
				}
				return core.View{
					PointerEvents: core.PointerEventsBoxNone,
					Style:         core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
					Children: []core.Node{core.View{
						TestID: "banner",
						Style:  core.Style{Background: core.RGBA(0x0f172a, 0.85), CornerRadius: 14, Padding: core.Symmetric(18, 28), Gap: 12, AlignItems: core.AlignCenter},
						Children: []core.Node{
							label("banner-title", func() string { return title }, 28),
							base.Button{
								OnPress:  func(core.PressEvent) { p.restart() },
								Label:    "Play again",
								Style:    core.Style{Background: core.RGB(0x0284c7), CornerRadius: 10, Padding: core.Symmetric(10, 18), Active: &core.Style{Opacity: core.Alpha(0.8)}},
								Children: []core.Node{core.Text{Content: "Play again", Style: core.Style{Color: core.RGB(0xffffff), FontSize: 15, FontWeight: core.FontWeightBold}}},
							},
						},
					}},
				}
			}),
		},
	}
}

// errorView reports content that failed to load, which the tests keep from happening in a shipped game.
func errorView(err error) core.Node {
	return core.View{
		Style: core.Style{FlexGrow: 1, Background: skyColor, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Padding: core.All(24)},
		Children: []core.Node{core.Text{
			TestID:  "load-error",
			Content: err.Error(),
			Style:   core.Style{Color: core.RGB(0xfca5a5), FontSize: 14},
		}},
	}
}

func fill() core.Style {
	return core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0)}
}

func clamp1(v float64) float64 { return max(-1, min(1, v)) }

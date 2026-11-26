package main

import (
	"fmt"
	"runtime"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/vehicle"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/gpu"
)

// ghostKey is the storage key the best lap's ghost is saved under, as the YAML vehicle.EncodeGhost writes.
const ghostKey = "racing.bestLap"

// racingGame is the running game: the race, the input that drives it, the chase camera and the signals the HUD reads.
type racingGame struct {
	track    *Track
	cfg      vehicle.Config
	race     *Race
	renderer *renderer
	store    core.Storage

	actions  *input.Actions
	controls vehicle.Controls
	pad      *input.VirtualPad
	stick    *input.VirtualStick
	buttons  []*input.VirtualButton

	// cam is the chase camera after the last step and prevCam before it; drawing interpolates between them.
	cam, prevCam scene3d.Camera
	alpha        float32

	lap, place, lapTime, best, countdown *core.Signal[string]
	over                                 *core.Signal[bool]
	errText                              *core.Signal[string]
	padShown                             *core.Signal[bool]
	padTick                              *core.Signal[int]
}

// Root is the game's root NSGo component: the race on a GPU view, the HUD, the results screen and the touch controls over it, run by a fixed-step game.Loop.
func Root(cx *core.Scope) core.Node { return root(cx, nil) }

// root builds Root, handing the game to started first when it is set, as the tests do to watch the race.
func root(cx *core.Scope, started func(*racingGame)) core.Node {
	desktop.SetTitle(cx, Project.Title())
	g, err := newRacingGame(cx)
	if err != nil {
		return core.Text{Content: fmt.Sprintf("Could not start the race: %v", err), Style: core.Style{Color: core.RGB(0xffffff), Padding: core.All(24)}}
	}
	if started != nil {
		started(g)
	}
	loop := &game.Loop{
		Rate:      stepRate,
		Immersive: true,
		Update:    g.update,
		Draw:      func(alpha float64) { g.alpha = float32(alpha) },
	}
	loop.Run(cx)

	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: core.RGB(skyColor)},
		Children: []core.Node{
			gpu.View(cx, gpu.ViewProps{
				TestID:           "race-view",
				Label:            "Race track",
				DepthFormat:      gpu.FormatDepth24Plus,
				RetainResources:  true,
				Style:            core.Style{FlexGrow: 1, FlexShrink: 1},
				OnReady:          func(dev *gpu.Device) { g.setupGPU(cx, dev) },
				OnDeviceRestored: func(dev *gpu.Device) { g.setupGPU(cx, dev) },
				OnFrame: func(frame *gpu.Frame) {
					cam := g.prevCam.Lerp(&g.cam, g.alpha)
					g.renderer.draw(frame, g.race, cam, g.alpha)
				},
				Children: []core.Node{g.touchLayer(), g.hudView(), g.countdownView(), g.resultsView()},
			}),
		},
	}
}

// newRacingGame loads the track, the car and the saved ghost, builds the scene's meshes and binds the input map of game.yaml.
func newRacingGame(cx *core.Scope) (*racingGame, error) {
	track, cfg, err := LoadRace()
	if err != nil {
		return nil, err
	}
	g := &racingGame{
		track:     track,
		cfg:       cfg,
		store:     cx.Storage(),
		actions:   input.NewActions(),
		controls:  vehicle.DefaultControls(),
		lap:       core.NewSignal(cx, ""),
		place:     core.NewSignal(cx, ""),
		lapTime:   core.NewSignal(cx, ""),
		best:      core.NewSignal(cx, ""),
		countdown: core.NewSignal(cx, ""),
		over:      core.NewSignal(cx, false),
		errText:   core.NewSignal(cx, ""),
		padShown:  core.NewSignal(cx, runtime.GOOS == "android" || runtime.GOOS == "ios"),
		padTick:   core.NewSignal(cx, 0),
	}
	if err := g.restart(); err != nil {
		return nil, err
	}
	meshes, static, err := sceneMeshes(g.race.course)
	if err != nil {
		return nil, err
	}
	g.renderer = newRenderer(meshes, static)

	Project.BindInput(g.actions)
	g.actions.WatchKeys(cx)
	g.actions.WatchGamepads(cx)
	g.pad = input.NewVirtualPad()
	g.stick = g.pad.AddStick(&input.VirtualStick{AxisXName: "steer", Radius: 56, DeadZone: 0.1, Floating: true})
	g.buttons = []*input.VirtualButton{
		g.pad.AddButton(&input.VirtualButton{Radius: 48, Label: "GAS", Action: "throttle"}),
		g.pad.AddButton(&input.VirtualButton{Radius: 38, Label: "BRAKE", Action: "brake"}),
		g.pad.AddButton(&input.VirtualButton{Radius: 26, Label: "R", Action: "respawn"}),
	}
	g.pad.BindActions(g.actions)
	g.layoutPad(core.Bounds{Size: core.Size{Width: core.Points(Project.Window.Width), Height: core.Points(Project.Window.Height)}})
	return g, nil
}

// loadGhost returns the best lap saved for this exact track at this step rate, or nil when there is none, it does not parse, or it was recorded on a different layout or rate.
func (g *racingGame) loadGhost() *vehicle.Ghost {
	data, ok := g.store.GetItem(ghostKey)
	if !ok {
		return nil
	}
	ghost, err := vehicle.ParseGhost([]byte(data))
	if err != nil || ghost.Name != trackID(g.track) || ghost.Rate != stepRate {
		return nil
	}
	return ghost
}

// saveGhost keeps a new best lap for the next race and the next launch.
func (g *racingGame) saveGhost(ghost *vehicle.Ghost) {
	data, err := vehicle.EncodeGhost(ghost)
	if err == nil {
		err = g.store.SetItem(ghostKey, string(data))
	}
	if err != nil {
		g.errText.Set(fmt.Sprintf("Could not save the best lap: %v", err))
	}
}

// restart lines the cars up on the grid for a new race, keeping the best lap's ghost.
func (g *racingGame) restart() error {
	ghost := g.loadGhost()
	if g.race != nil && g.race.Ghost() != nil {
		ghost = g.race.Ghost()
	}
	race, err := newRace(g.track, g.cfg, ghost)
	if err != nil {
		return err
	}
	race.onBestLap = g.saveGhost
	g.race = race
	g.snapCamera()
	g.over.Set(false)
	g.syncHUD()
	return nil
}

// setupGPU creates the renderer's GPU resources on dev, reporting a failure on screen.
func (g *racingGame) setupGPU(cx *core.Scope, dev *gpu.Device) {
	if err := g.renderer.setup(cx, dev, len(g.race.cars)); err != nil {
		g.errText.Set(fmt.Sprintf("Could not set up 3D drawing: %v", err))
	}
}

// update runs one fixed step: the input, the race, the camera and the HUD.
func (g *racingGame) update(time.Duration) {
	g.actions.Step()
	if g.race.phase == phaseFinished && g.actions.JustPressed("confirm") {
		g.again()
		return
	}
	g.race.Step(g.controls.Read(g.actions), g.actions.JustPressed("respawn"))
	g.followCamera(0.12)
	g.syncHUD()
}

// again starts the next race from the results screen.
func (g *racingGame) again() {
	if err := g.restart(); err != nil {
		g.errText.Set(fmt.Sprintf("Could not restart the race: %v", err))
	}
}

// followCamera moves the chase camera smooth of the way toward its place behind and above the player's car.
func (g *racingGame) followCamera(smooth float32) {
	g.prevCam = g.cam
	pos := fromPhys(g.race.player.v.Body.Pos)
	fwd := flat(fromPhys(g.race.player.v.Forward())).Norm()
	want := pos.Sub(fwd.Scale(8)).Add(math3d.Vec3{Y: 3.4})
	g.cam.Pos = g.cam.Pos.Lerp(want, smooth)
	g.cam.LookAt(pos.Add(fwd.Scale(4)).Add(math3d.Vec3{Y: 1}), math3d.Vec3Up())
}

// snapCamera puts the chase camera straight in its place, as at the start of a race.
func (g *racingGame) snapCamera() {
	g.cam = *scene3d.NewPerspectiveCamera(math3d.Vec3{}, math3d.DegToRad(60), 16.0/9.0, 0.1, 500)
	g.followCamera(1)
	g.prevCam = g.cam
}

// syncHUD copies the race's state into the HUD's signals, which only redraw what changed.
func (g *racingGame) syncHUD() {
	r := g.race
	g.lap.Set(fmt.Sprintf("Lap %d/%d", r.Lap(), r.track.Laps))
	g.place.Set(fmt.Sprintf("Position %d/%d", r.Place(), len(r.cars)))
	// The lap clock shows tenths, so its text changes ten times a second rather than every step.
	g.lapTime.Set(formatSteps(r.LapSteps() / (stepRate / 10) * (stepRate / 10)))
	if gh := r.Ghost(); gh != nil {
		g.best.Set("Best " + formatSteps(gh.LapSteps))
	} else {
		g.best.Set("Best --:--.--")
	}
	switch n := r.Countdown(); {
	case n > 0:
		g.countdown.Set(fmt.Sprint(n))
	case r.phase == phaseRacing && r.race.Step() < stepRate:
		g.countdown.Set("GO!")
	default:
		g.countdown.Set("")
	}
	g.over.Set(r.phase == phaseFinished)
}

// hudView is the panel of lap, position and times at the top left.
func (g *racingGame) hudView() core.Node {
	text := func(id string, s *core.Signal[string], size core.Points, color uint32) core.Node {
		return core.Text{TestID: id, ContentFunc: s.Get, Style: core.Style{Color: core.RGB(color), FontSize: size, FontWeight: core.FontWeightBold}}
	}
	return core.View{
		PointerEvents: core.PointerEventsNone,
		Style: core.Style{
			Position:   core.Absolute,
			Top:        core.Pt(0),
			Left:       core.Pt(0),
			Padding:    core.All(16),
			PaddingEnv: core.EnvAll(core.EnvSafeArea),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{Background: core.RGBA(0x0f172a, 0.75), CornerRadius: 10, Padding: core.Symmetric(8, 14), Gap: 2},
				Children: []core.Node{
					text("lap", g.lap, 20, 0xf8fafc),
					text("place", g.place, 16, 0xfacc15),
					text("lap-time", g.lapTime, 16, 0xe2e8f0),
					text("best-lap", g.best, 13, 0x7dd3fc),
					core.Text{TestID: "error", ContentFunc: g.errText.Get, Style: core.Style{Color: core.RGB(0xfca5a5), FontSize: 12}},
				},
			},
		},
	}
}

// countdownView is the big countdown in the middle of the screen.
func (g *racingGame) countdownView() core.Node {
	return core.View{
		PointerEvents: core.PointerEventsNone,
		Style:         fill(core.Style{AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter}),
		Children: []core.Node{
			core.Text{TestID: "countdown", ContentFunc: g.countdown.Get, Style: core.Style{Color: core.RGB(0xffffff), FontSize: 72, FontWeight: core.FontWeightBold}},
		},
	}
}

// resultsView is the results screen, shown once the race is over: the outcome, every car's place and times, and a button to race again.
func (g *racingGame) resultsView() core.Node {
	return core.Show{
		When: g.over.Get,
		Then: func(*core.Scope) core.Node {
			r := g.race
			title, color := "You win!", uint32(0x4ade80)
			if !r.Won() {
				title, color = "You lose", uint32(0xf87171)
			}
			rows := []core.Node{
				core.Text{TestID: "result-title", Content: title, Style: core.Style{Color: core.RGB(color), FontSize: 34, FontWeight: core.FontWeightBold}},
				core.Text{Content: r.track.Name, Style: core.Style{Color: core.RGB(0x94a3b8), FontSize: 14}},
			}
			for _, res := range r.Results() {
				time := "did not finish"
				if res.Finished {
					time = formatSteps(res.Steps)
				}
				best := "--:--.--"
				if res.BestLap > 0 {
					best = formatSteps(res.BestLap)
				}
				line := fmt.Sprintf("%d. %s   %s   best lap %s", res.Place, res.Name, time, best)
				weight := core.FontWeightNormal
				if res.Player {
					weight = core.FontWeightBold
				}
				rows = append(rows, core.Text{TestID: fmt.Sprintf("result-%d", res.Place), Content: line, Style: core.Style{Color: core.RGB(0xf8fafc), FontSize: 16, FontWeight: weight}})
			}
			rows = append(rows, base.Button{
				Label:   "Race again",
				OnPress: func(core.PressEvent) { g.again() },
				Style:   core.Style{Background: core.RGB(0x0284c7), CornerRadius: 10, Padding: core.Symmetric(10, 18), Margin: core.Edges{Top: 10}, Active: &core.Style{Opacity: core.Alpha(0.8)}},
				Children: []core.Node{
					core.Text{Content: "Race again", Style: core.Style{Color: core.RGB(0xffffff), FontSize: 16, FontWeight: core.FontWeightBold}},
				},
			})
			return core.View{
				Style: fill(core.Style{AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Background: core.RGBA(0x020617, 0.45)}),
				Children: []core.Node{
					core.View{
						TestID:   "results",
						Style:    core.Style{Background: core.RGBA(0x0f172a, 0.92), CornerRadius: 14, Padding: core.All(24), Gap: 6, AlignItems: core.AlignCenter},
						Children: rows,
					},
				},
			}
		},
	}
}

// touchLayer is the full-screen layer under the HUD that takes touches for the on-screen controls and draws them once the player has touched the screen, or from the start on a phone.
func (g *racingGame) touchLayer() core.Node {
	return core.View{
		TestID: "touch-controls",
		Style:  fill(core.Style{}),
		OnTouch: func(ev core.TouchEvent) {
			g.pad.OnTouch(ev)
			g.padShown.Set(true)
			g.padTick.Update(func(n int) int { return n + 1 })
		},
		OnLayout: g.layoutPad,
		Children: []core.Node{
			core.Canvas{
				Style: fill(core.Style{}),
				Draw: func(d *core.Drawing, _ core.Size) {
					_ = g.padTick.Get()
					if g.padShown.Get() {
						g.pad.Draw(d, input.VirtualStyle{})
					}
				},
			},
		},
	}
}

// layoutPad places the on-screen controls for a screen of bounds' size: the steering stick anywhere on the lower left, the pedals at the bottom right and the respawn button above them.
func (g *racingGame) layoutPad(bounds core.Bounds) {
	w, h := bounds.Size.Width, bounds.Size.Height
	if w <= 0 || h <= 0 {
		return
	}
	g.stick.Zone = core.Bounds{Origin: core.Point{Y: h / 3}, Size: core.Size{Width: w / 2, Height: h * 2 / 3}}
	g.stick.Center = core.Point{X: 110, Y: h - 110}
	g.buttons[0].Center = core.Point{X: w - 90, Y: h - 100}
	g.buttons[1].Center = core.Point{X: w - 210, Y: h - 80}
	g.buttons[2].Center = core.Point{X: w - 60, Y: h - 230}
	g.padTick.Update(func(n int) int { return n + 1 })
}

// fill is style s stretched over its parent.
func fill(s core.Style) core.Style {
	s.Position = core.Absolute
	s.Top, s.Right, s.Bottom, s.Left = core.Pt(0), core.Pt(0), core.Pt(0), core.Pt(0)
	return s
}

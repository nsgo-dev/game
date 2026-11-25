package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/vehicle"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// startGame starts the app at the window size game.yaml sets, with opts, and returns it with its game. The playthroughs pass nsgotest.NoGPU: nsgotest draws a GPU view with a software rasterizer that runs the shaders a pixel at a time, far too slow for minutes of racing, so TestRaceViewDraws checks the drawing on its own at a small size.
func startGame(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *racingGame) {
	t.Helper()
	var g *racingGame
	opts = append([]nsgotest.Option{nsgotest.Size(core.Points(Project.Window.Width), core.Points(Project.Window.Height))}, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return root(cx, func(rg *racingGame) { g = rg })
	}, opts...)
	if g == nil {
		t.Fatal("the game did not start")
	}
	return app, g
}

// playRace advances app a step at a time until the race is over, calling press before each step with what the racing line asks of the player's car, which press turns into the keys, buttons or touches a player would use.
func playRace(t *testing.T, app *nsgotest.App, g *racingGame, pace float32, press func(vehicle.Input)) {
	t.Helper()
	pilot := &driver{course: g.race.course, pace: pace}
	for i := 0; g.race.Results() == nil; i++ {
		if i >= maxRaceSteps {
			t.Fatalf("the race did not end in %d steps: lap %d, place %d", maxRaceSteps, g.race.Lap(), g.race.Place())
		}
		press(pilot.drive(g.race.player.v))
		app.Advance(stepDur)
	}
}

// TestCountdownStartsTheRace checks the lights count down from 3 with the cars held on the grid, then say GO! and let them go.
func TestCountdownStartsTheRace(t *testing.T) {
	app, g := startGame(t, nsgotest.NoGPU())
	wantText(t, app, "countdown", "3")
	app.Find("text:Lap 1/2")
	app.Find("text:Position 3/3")
	app.KeyDown("up")
	app.Advance(time.Second + stepDur)
	wantText(t, app, "countdown", "2")
	if s := g.race.player.v.Speed(); s > 0.5 {
		t.Fatalf("the player's car moved during the countdown at %v m/s", s)
	}
	app.Advance(2 * time.Second)
	wantText(t, app, "countdown", "GO!")
	app.Advance(2 * time.Second)
	if s := g.race.player.v.Speed(); s < 5 {
		t.Fatalf("speed %v m/s two seconds after the start with the throttle held", s)
	}
	app.KeyUp("up")
}

// TestRaceViewDraws checks the race view draws the whole scene, every car included, with the sky behind the road and the ground.
func TestRaceViewDraws(t *testing.T) {
	app, g := startGame(t, nsgotest.Size(96, 54))
	app.Advance(2 * stepDur)
	frames := app.GPUFrames()
	if len(frames) == 0 {
		t.Fatal("the race view drew no GPU frames")
	}
	if last := frames[len(frames)-1]; last.DrawsCount < len(g.renderer.static)+carParts*len(g.race.cars) {
		t.Fatalf("the last frame drew %d meshes, want at least %d, the scene's and every car's", last.DrawsCount, len(g.renderer.static)+carParts*len(g.race.cars))
	}
	img, err := app.GPUReadPixels("race-view")
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if top, bottom := img.RGBAAt(b.Dx()/2, 0), img.RGBAAt(b.Dx()/2, b.Dy()-1); top == bottom {
		t.Fatalf("the top and the bottom of the frame are both %v, want the sky above the road", top)
	}

	// After a lost device comes back, the race draws again with the resources NSGo restored.
	pipe := g.renderer.pipe
	app.TriggerGPUDeviceLost("race-view", nsgotest.GPULostUnknown, "test")
	app.Advance(stepDur)
	app.RestoreGPUDevice("race-view")
	app.Advance(2 * stepDur)
	if after := app.GPUFrames(); len(after) <= len(frames) || after[len(after)-1].DrawsCount < len(g.renderer.static) {
		t.Fatal("the race view did not draw after the device was restored")
	}
	if msg := g.errText.GetUntracked(); msg != "" {
		t.Fatalf("after the restore the game reports %q", msg)
	}
	if g.renderer.pipe != pipe {
		t.Fatal("the renderer rebuilt its resources, which the view retains across a device loss")
	}
}

// TestGamepadPlaythroughWins drives the race with a game controller, the stick steering and the triggers on the pedals, at the racing line's full pace: the player comes from the back of the grid to win, the best lap's ghost is saved and races alongside the next time.
func TestGamepadPlaythroughWins(t *testing.T) {
	app, g := startGame(t, nsgotest.NoGPU())
	app.ConnectGamepad(0, "Xbox Wireless Controller")
	button := func(b gamepad.Button, on bool) {
		v := float32(0)
		if on {
			v = 1
		}
		app.SetGamepadButton(0, b, v)
	}
	playRace(t, app, g, 1, func(in vehicle.Input) {
		app.SetGamepadAxis(0, gamepad.AxisLeftX, stickFor(in.Steer))
		button(gamepad.ButtonRT, in.Throttle > 0.3)
		button(gamepad.ButtonLT, in.Brake > 0.3)
	})
	button(gamepad.ButtonRT, false)
	button(gamepad.ButtonLT, false)
	app.SetGamepadAxis(0, gamepad.AxisLeftX, 0)
	app.Advance(stepDur)
	app.Find("text:You win!")
	app.Find("results")
	if res := g.race.Results(); !res[0].Player || !res[0].Finished {
		t.Fatalf("results %+v, want the player first", res)
	}

	// The best flying lap is saved as a ghost for this track, carried into the next race and kept off its standing first lap; race_test.go checks it races from lap 2.
	saved, ok := app.Storage().GetItem(ghostKey)
	if !ok {
		t.Fatal("no ghost saved")
	}
	ghost, err := vehicle.ParseGhost([]byte(saved))
	if err != nil {
		t.Fatal(err)
	}
	if ghost.Name != trackID(g.track) || ghost.Rate != stepRate {
		t.Fatalf("ghost saved as %q at %d steps a second, want %q at %d", ghost.Name, ghost.Rate, trackID(g.track), stepRate)
	}
	app.Find("text:Best " + formatSteps(ghost.LapSteps))
	button(gamepad.ButtonStart, true)
	app.Advance(stepDur)
	button(gamepad.ButtonStart, false)
	app.Advance(countdownSteps*stepDur + time.Second)
	if g.race.Results() != nil || g.race.phase != phaseRacing {
		t.Fatal("Start on the results screen did not begin a new race")
	}
	if g.race.Ghost() == nil {
		t.Fatal("the next race lost the ghost")
	}
	if _, _, shown := g.race.ghostPose(0); shown {
		t.Fatal("the ghost races on the next race's first lap, which starts on the grid")
	}
}

// TestKeyboardPlaythroughLoses drives the race with the arrow keys at a slow pace: both rivals finish first and the race ends with the player beaten. Enter starts the race again.
func TestKeyboardPlaythroughLoses(t *testing.T) {
	app, g := startGame(t, nsgotest.NoGPU())
	held := map[string]bool{}
	hold := func(key string, on bool) {
		switch {
		case on && !held[key]:
			app.KeyDown(key)
		case !on && held[key]:
			app.KeyUp(key)
		}
		held[key] = on
	}
	playRace(t, app, g, 0.6, func(in vehicle.Input) {
		hold("left", in.Steer < -0.25)
		hold("right", in.Steer > 0.25)
		hold("up", in.Throttle > 0.3)
		hold("down", in.Brake > 0.3)
	})
	for key := range held {
		hold(key, false)
	}
	app.Advance(stepDur)
	app.Find("text:You lose")
	res := g.race.Results()
	if g.race.Won() || res[len(res)-1].Name != playerName {
		t.Fatalf("results %+v, want the player last", res)
	}
	if g.race.player.racer.Laps == 0 {
		t.Fatal("the keyboard did not get the player's car round a lap")
	}
	app.Key("enter")
	app.Advance(stepDur)
	wantText(t, app, "countdown", "3")
	if g.race.Results() != nil {
		t.Fatal("Enter on the results screen did not begin a new race")
	}
}

// TestTouchControlsDrive drives with the on-screen controls: the gas pedal speeds the car up, the stick steers it, the brake slows it and the respawn button puts it back on the road. A player holds the gas and steers at once; nsgotest gives every finger App.Touch puts down the same touch ID (nsgo-dev/nsgo nsgotest/pointer.go), so the test lifts each finger before the next goes down.
func TestTouchControlsDrive(t *testing.T) {
	app, g := startGame(t, nsgotest.NoGPU())
	app.Advance(countdownSteps*stepDur + stepDur)
	gas := app.Touch("touch-controls", g.buttons[0].Center)
	app.Advance(2 * time.Second)
	if s := g.race.player.v.Speed(); s < 5 {
		t.Fatalf("speed %v m/s after two seconds on the gas pedal", s)
	}
	if !g.padShown.GetUntracked() {
		t.Fatal("the touch controls did not show after a touch")
	}
	gas.Lift()

	at := g.stick.Center
	steer := app.Touch("touch-controls", at)
	steer.Move(core.Point{X: at.X + 30, Y: at.Y})
	steer.Move(core.Point{X: at.X + 60, Y: at.Y})
	app.Advance(stepDur * 2)
	if s := g.race.player.v.Input().Steer; s < 0.5 {
		t.Fatalf("steer %v with the stick pushed right", s)
	}
	steer.Lift()
	app.Advance(stepDur * 2)
	if s := g.race.player.v.Input().Steer; s != 0 {
		t.Fatalf("steer %v after the stick was let go", s)
	}

	before := g.race.player.v.Speed()
	brake := app.Touch("touch-controls", g.buttons[1].Center)
	app.Advance(time.Second)
	if s := g.race.player.v.Speed(); s > before-3 {
		t.Fatalf("speed %v m/s after a second on the brake from %v", s, before)
	}
	brake.Lift()

	respawn := app.Touch("touch-controls", g.buttons[2].Center)
	app.Advance(stepDur * 2)
	respawn.Lift()
	if s := g.race.player.v.Speed(); s > 0.5 {
		t.Fatalf("speed %v m/s right after a respawn", s)
	}
	if _, side := g.race.course.nearest(fromPhys(g.race.player.v.Body.Pos)); abs(side) > 0.5 {
		t.Fatalf("respawned %v m off the centerline", side)
	}
}

// TestRaceAgainButton presses the results screen's button with the pointer.
func TestRaceAgainButton(t *testing.T) {
	app, g := startGame(t, nsgotest.NoGPU())
	pilot := &driver{course: g.race.course, pace: 1}
	// Skip the drive itself, which the playthroughs cover: the race runs headless to the end, then the app shows its results.
	for g.race.Results() == nil {
		g.race.Step(pilot.drive(g.race.player.v), false)
	}
	app.Advance(stepDur)
	app.Find("text:You win!")
	app.Press("role:button Race again")
	wantText(t, app, "countdown", "3")
	if g.race.Results() != nil {
		t.Fatal("the button did not begin a new race")
	}
}

// wantText fails t unless the view with test ID id shows text.
func wantText(t *testing.T, app *nsgotest.App, id, text string) {
	t.Helper()
	if got := app.Find(id).Text; got != text {
		t.Fatalf("%s shows %q, want %q", id, got, text)
	}
}

// stickFor returns the stick position that reads as steer once game.yaml's dead zone of 0.1 is taken off.
func stickFor(steer float32) float32 {
	const deadZone = 0.1
	switch {
	case steer > 0:
		return deadZone + steer*(1-deadZone)
	case steer < 0:
		return -deadZone + steer*(1-deadZone)
	}
	return 0
}

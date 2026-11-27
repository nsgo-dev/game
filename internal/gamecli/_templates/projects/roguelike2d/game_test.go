package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

const frame = time.Second / 60

// The seeds the playthroughs use: the clearing bot wins winSeed in the fewest turns of the seeds TestBotsWinAndLose plays, and the diving bot dies on the first floor of loseSeed.
const (
	winSeed  = 2
	loseSeed = 21
)

// The storage keys of the save slots, as save.StorageBackend names them.
const (
	runKey     = savePrefix + ".slot." + runSlot
	recordsKey = savePrefix + ".slot." + recordsSlot
)

// startGame starts the app in a 960 by 540 window, three times the virtual screen, with every new run seeded from seeds in turn, and returns it with its game.
func startGame(t *testing.T, seeds []uint64, opts ...nsgotest.Option) (*nsgotest.App, *roguelikeGame) {
	t.Helper()
	var g *roguelikeGame
	next := 0
	o := options{
		seed: func() uint64 {
			s := seeds[min(next, len(seeds)-1)]
			next++
			return s
		},
		hook: func(rg *roguelikeGame) { g = rg },
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return newRoot(cx, o) },
		append([]nsgotest.Option{nsgotest.Size(960, 540), nsgotest.ManualFrames()}, opts...)...)
	app.Advance(3 * frame)
	if g == nil {
		t.Fatal("the game did not start")
	}
	return app, g
}

// device is one way to play: it performs an action through the app's input.
type device interface {
	perform(a action)
}

// press holds an input for a frame and lets it go for another, so the game hears one press.
func press(app *nsgotest.App, down, up func()) {
	down()
	app.Advance(frame)
	up()
	app.Advance(frame)
}

type keyboard struct{ app *nsgotest.App }

func (k keyboard) perform(a action) {
	key := map[string]string{abilityQuaff: "q", abilityCleave: "e"}[a.ability]
	switch a.kind {
	case actWait:
		key = "space"
	case actMove:
		key = map[cell]string{{Y: -1}: "up", {Y: 1}: "down", {X: -1}: "left", {X: 1}: "right"}[a.dir]
	}
	press(k.app, func() { k.app.KeyDown(key) }, func() { k.app.KeyUp(key) })
}

// controller moves with the left stick and acts with the face buttons.
type controller struct{ app *nsgotest.App }

func (c controller) perform(a action) {
	if a.kind == actMove {
		axis, v := gamepad.AxisLeftX, float32(a.dir.X)
		if a.dir.X == 0 {
			axis, v = gamepad.AxisLeftY, float32(a.dir.Y)
		}
		press(c.app, func() { c.app.SetGamepadAxis(1, axis, v) }, func() { c.app.SetGamepadAxis(1, axis, 0) })
		return
	}
	btn := map[string]gamepad.Button{abilityQuaff: gamepad.ButtonWest, abilityCleave: gamepad.ButtonNorth}[a.ability]
	if a.kind == actWait {
		btn = gamepad.ButtonSouth
	}
	press(c.app, func() { c.app.SetGamepadButton(1, btn, 1) }, func() { c.app.SetGamepadButton(1, btn, 0) })
}

// touchscreen taps the on-screen controls.
type touchscreen struct {
	t   *testing.T
	app *nsgotest.App
	g   *roguelikeGame
}

func controlFrame(t *testing.T, g *roguelikeGame, id string) core.Bounds {
	t.Helper()
	for _, c := range g.touch.Controls() {
		if c.ID == id {
			return c.Frame
		}
	}
	t.Fatalf("no touch control %q", id)
	return core.Bounds{}
}

func centerOf(f core.Bounds) core.Point {
	return core.Point{X: f.Origin.X + f.Size.Width/2, Y: f.Origin.Y + f.Size.Height/2}
}

func dpadPoint(t *testing.T, g *roguelikeGame, dir cell) core.Point {
	t.Helper()
	f := controlFrame(t, g, "move")
	c := centerOf(f)
	r := min(f.Size.Width, f.Size.Height) / 2
	return core.Point{X: c.X + core.Points(dir.X)*r*0.65, Y: c.Y + core.Points(dir.Y)*r*0.65}
}

func (s touchscreen) perform(a action) {
	var at core.Point
	switch a.kind {
	case actWait:
		at = centerOf(controlFrame(s.t, s.g, "wait"))
	case actMove:
		at = dpadPoint(s.t, s.g, a.dir)
	case actAbility:
		id := map[string]string{abilityQuaff: "quaff", abilityCleave: "cleave"}[a.ability]
		at = centerOf(controlFrame(s.t, s.g, id))
	}
	var finger *nsgotest.Finger
	press(s.app, func() { finger = s.app.Touch("game-view", at) }, func() { finger.Lift() })
}

// play performs the bot's actions with dev until the run ends, failing when an action takes no turn or limit turns pass.
func play(t *testing.T, g *roguelikeGame, dev device, style, limit int) {
	t.Helper()
	r := g.run
	for r.state == playing && r.turns < limit {
		a := nextAction(r, style)
		before := r.turns
		dev.perform(a)
		if r.turns == before {
			t.Fatalf("performing %+v at %v on floor %d took no turn; the log says %q", a, r.pos, r.floor.index+1, r.lastMessage())
		}
	}
}

// nextAction is the bot's next action, waiting where it has nowhere to step.
func nextAction(r *run, style int) action {
	a := botAction(r, style)
	if a.kind == actMove && a.dir == (cell{}) {
		return action{kind: actWait}
	}
	return a
}

// checkWin checks that the run was won and the end panel and records say so.
func checkWin(t *testing.T, app *nsgotest.App, g *roguelikeGame) {
	t.Helper()
	if g.run.state != won {
		t.Fatalf("the run ended %v on floor %d, slain by %q, want won", g.run.state, g.run.floor.index+1, g.run.killer)
	}
	app.Find("text:The Heart Ember is yours")
	app.Find(fmt.Sprintf("text:You climbed out of all %d floors.", len(g.data.dungeon.Floors)))
	app.Find("text:Runs 1 · Wins 1 · Deepest floor 5 · Most shards " + fmt.Sprint(g.run.shards))
	if _, ok := app.Storage().GetItem(runKey); ok {
		t.Error("the run slot is still there after the run was won")
	}
}

func TestKeyboardPlaythroughWins(t *testing.T) {
	app, g := startGame(t, []uint64{winSeed})
	app.Find("text:Floor 1/5")
	play(t, g, keyboard{app}, botClear, 5000)
	checkWin(t, app, g)
}

func TestGamepadPlaythroughWins(t *testing.T) {
	app, g := startGame(t, []uint64{winSeed})
	app.ConnectGamepad(1, "Test Controller")
	app.Advance(2 * frame)
	play(t, g, controller{app}, botClear, 5000)
	checkWin(t, app, g)
}

func TestTouchPlaythroughWins(t *testing.T) {
	app, g := startGame(t, []uint64{winSeed}, nsgotest.Environment(core.Environment{Idiom: core.IdiomPhone, SafeArea: core.Edges{Left: 47, Right: 47, Bottom: 21}}))
	for _, id := range []string{"touch-move", "touch-wait", "touch-quaff", "touch-cleave"} {
		if !app.Exists("id:" + id) {
			t.Fatalf("%s is not visible on a phone", id)
		}
	}
	play(t, g, touchscreen{t, app, g}, botClear, 5000)
	checkWin(t, app, g)
	if !g.touch.Visible() {
		t.Error("the on-screen controls are hidden after the screen was touched")
	}
}

func TestTouchControlsHideOnDesktopUntilTouched(t *testing.T) {
	app, g := startGame(t, []uint64{winSeed}, nsgotest.Idiom(core.IdiomDesktop))
	if g.touch.Visible() || app.Exists("id:touch-move") {
		t.Fatal("the touch controls show on a desktop before any touch")
	}

	// Clicking the mouse where the d-pad or wait button sits neither shows the controls nor takes a turn.
	for _, at := range []core.Point{dpadPoint(t, g, openStep(g.run)), centerOf(controlFrame(t, g, "wait"))} {
		press(app, func() { app.PressDown("game-view"); app.HoverAt("game-view", at) }, func() { app.PressUp("game-view") })
	}
	if g.touch.Visible() || app.Exists("id:touch-move") {
		t.Fatal("a mouse click showed the touch controls")
	}
	if g.run.turns != 0 {
		t.Fatalf("mouse clicks took %d turns, want 0", g.run.turns)
	}

	// Touching the wait button shows the controls and takes a turn.
	touchscreen{t, app, g}.perform(action{kind: actWait})
	if !g.touch.Visible() || !app.Exists("id:touch-move") || !app.Exists("id:touch-wait") {
		t.Fatal("a touch did not show the touch controls")
	}
	if g.run.turns != 1 {
		t.Fatalf("touching wait took %d turns, want 1", g.run.turns)
	}

	// Using the keyboard hides them again.
	keyboard{app}.perform(action{kind: actWait})
	if g.touch.Visible() || app.Exists("id:touch-move") {
		t.Fatal("the keyboard did not hide the touch controls")
	}
}

func TestTouchControlsStayInsideSafeAreaAndOutsideLetterbox(t *testing.T) {
	overlaps := func(a, b core.Bounds) bool {
		return a.Origin.X < b.Origin.X+b.Size.Width && b.Origin.X < a.Origin.X+a.Size.Width &&
			a.Origin.Y < b.Origin.Y+b.Size.Height && b.Origin.Y < a.Origin.Y+a.Size.Height
	}
	checkLayout := func(t *testing.T, size core.Size, safe core.Edges, checkLetterbox bool) {
		t.Helper()
		_, g := startGame(t, []uint64{winSeed}, nsgotest.Size(size.Width, size.Height), nsgotest.Environment(core.Environment{Idiom: core.IdiomPhone, SafeArea: safe}))
		controls := g.touch.Controls()
		playfieldBottom := g.viewport.GameToView(size, core.Point{X: core.Points(g.viewport.Width), Y: core.Points(g.viewport.Height)}).Y
		for i, c := range controls {
			if c.Frame.Size.Width < 44 || c.Frame.Size.Height < 44 {
				t.Errorf("%s is smaller than 44pt: %+v", c.ID, c.Frame.Size)
			}
			if c.Frame.Origin.X < safe.Left || c.Frame.Origin.X+c.Frame.Size.Width > size.Width-safe.Right ||
				c.Frame.Origin.Y < safe.Top || c.Frame.Origin.Y+c.Frame.Size.Height > size.Height-safe.Bottom {
				t.Errorf("%s is outside the safe area: %+v", c.ID, c.Frame)
			}
			if checkLetterbox && c.Frame.Origin.Y < playfieldBottom {
				t.Errorf("%s (%+v) overlaps the letterboxed playfield (bottom %.1f)", c.ID, c.Frame, playfieldBottom)
			}
			for j := i + 1; j < len(controls); j++ {
				if overlaps(c.Frame, controls[j].Frame) {
					t.Errorf("%s (%+v) overlaps %s (%+v)", c.ID, c.Frame, controls[j].ID, controls[j].Frame)
				}
			}
		}
	}
	t.Run("landscape phone", func(t *testing.T) {
		checkLayout(t, core.Size{Width: 844, Height: 390}, core.Edges{Left: 47, Right: 47, Bottom: 21}, false)
	})
	t.Run("portrait phone", func(t *testing.T) {
		checkLayout(t, core.Size{Width: 390, Height: 844}, core.Edges{Top: 59, Bottom: 34}, true)
	})
}

// TestDivingIsFatalAndPermanent loses a run, checks the summary and the records, checks that permadeath deleted the run slot, and starts another run with Play again.
func TestDivingIsFatalAndPermanent(t *testing.T) {
	app, g := startGame(t, []uint64{loseSeed, winSeed})
	if _, ok := app.Storage().GetItem(runKey); !ok {
		t.Fatal("arriving on the first floor did not save the run")
	}
	play(t, g, keyboard{app}, botDive, 5000)
	if g.run.state != lost {
		t.Fatalf("the run ended %v, want lost", g.run.state)
	}
	app.Find("text:Slain on floor 1")
	app.Find(fmt.Sprintf("text:A %s ended the run.", g.run.killer))
	app.Find("text:" + g.run.summarize().tally())
	app.Find(fmt.Sprintf("text:HP 0/%d", g.run.maxHealth()))
	if _, ok := app.Storage().GetItem(runKey); ok {
		t.Fatal("the run slot survived the player's death")
	}
	raw, _ := app.Storage().GetItem(recordsKey)
	var env struct {
		Version int     `json:"version"`
		Data    records `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil || env.Version != saveVersion || env.Data.Runs != 1 || env.Data.Wins != 0 || env.Data.Last == nil || env.Data.Last.Killer != g.run.killer {
		t.Fatalf("records slot = %s (%v)", raw, err)
	}

	dead := g.run
	app.Press("play-again")
	app.Advance(2 * frame)
	if g.run == dead || g.run.state != playing || g.run.seed != winSeed || app.Exists("end-panel") {
		t.Fatalf("Play again did not start a new run with the next seed")
	}
	app.Find("text:HP 24/24")
}

// TestRestartWaitsForTheEnd checks that R does nothing during a run, and that Start on a controller starts a new one once the run is over.
func TestRestartWaitsForTheEnd(t *testing.T) {
	app, g := startGame(t, []uint64{loseSeed})
	first := g.run
	app.Key("r")
	app.Advance(2 * frame)
	if g.run != first {
		t.Fatal("R restarted a run that was still being played")
	}
	play(t, g, keyboard{app}, botDive, 5000)
	app.ConnectGamepad(1, "Test Controller")
	app.Advance(2 * frame)
	press(app, func() { app.SetGamepadButton(1, gamepad.ButtonStart, 1) }, func() { app.SetGamepadButton(1, gamepad.ButtonStart, 0) })
	if g.run == first || g.run.state != playing {
		t.Fatal("Start after the run was lost did not start a new one")
	}
}

// TestQuittingResumesWhereTheRunStood plays to the second floor and some turns into it, starts the app again on the storage the first one wrote, and checks that the run comes back exactly as it stood, and then plays out exactly as the first one does: quitting neither undoes a turn nor changes the rolls to come.
func TestQuittingResumesWhereTheRunStood(t *testing.T) {
	app, g := startGame(t, []uint64{winSeed})
	r := g.run
	for (r.floor.index == 0 || r.turns < 40) && r.state == playing {
		keyboard{app}.perform(nextAction(r, botClear))
	}
	if r.floor.index != 1 || r.state != playing {
		t.Fatalf("the bot is on floor %d with the run %v, want floor 2 in play", r.floor.index+1, r.state)
	}
	raw, ok := app.Storage().GetItem(runKey)
	if !ok {
		t.Fatal("playing did not save the run")
	}

	again, g2 := startGame(t, []uint64{99}, nsgotest.Storage(map[string]string{runKey: raw}))
	r2 := g2.run
	want, _ := json.Marshal(r.save())
	if got, _ := json.Marshal(r2.save()); string(got) != string(want) {
		t.Fatalf("resumed run = %s, want %s", got, want)
	}
	again.Find("text:Floor 2/5")
	again.Find(fmt.Sprintf("text:Lv %d", r.level))

	playRun(r, botClear, 20000)
	playRun(r2, botClear, 20000)
	if r.summarize() != r2.summarize() {
		t.Fatalf("the resumed run ended %+v, the first %+v", r2.summarize(), r.summarize())
	}
}

// TestUnreadableSlotsStartAfresh starts the app over a run slot that is not valid and one from a newer version of the game: both start a new run, and only the newer one is left alone.
func TestUnreadableSlotsStartAfresh(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		kept      bool
	}{
		{"corrupt", "{not json", false},
		{"future", `{"version":99,"data":{"seed":1}}`, true},
		{"mismatched", `{"version":1,"data":{"seed":1,"floor":42,"health":5,"level":1}}`, false},
		{"huge-level", `{"version":1,"data":{"seed":1,"floor":0,"health":5,"level":1000000000000000000,"weapon":"rusty-pick"}}`, false},
		{"negative-draughts", `{"version":1,"data":{"seed":1,"floor":0,"health":5,"level":1,"draughts":-3,"weapon":"rusty-pick"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, g := startGame(t, []uint64{winSeed}, nsgotest.Storage(map[string]string{runKey: tc.raw}))
			if g.run.seed != winSeed || g.run.floor.index != 0 {
				t.Fatalf("started seed %d on floor %d, want a new run", g.run.seed, g.run.floor.index+1)
			}
			raw, _ := app.Storage().GetItem(runKey)
			if (raw == tc.raw) != tc.kept {
				t.Fatalf("run slot = %q, kept %v, want kept %v", raw, raw == tc.raw, tc.kept)
			}
		})
	}
}

// TestEachBindingActs checks every binding of the movement and wait actions takes a turn: keys, physical keys, the d-pad, the stick and touch over the on-screen controls.
func TestEachBindingActs(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(app *nsgotest.App, g *roguelikeGame)
	}{
		{"arrow", func(app *nsgotest.App, g *roguelikeGame) {
			keyboard{app}.perform(action{kind: actMove, dir: openStep(g.run)})
		}},
		{"WASD", func(app *nsgotest.App, g *roguelikeGame) {
			code := map[cell]string{{Y: -1}: "KeyW", {Y: 1}: "KeyS", {X: -1}: "KeyA", {X: 1}: "KeyD"}[openStep(g.run)]
			press(app, func() { app.KeyCodeDown(code) }, func() { app.KeyCodeUp(code) })
		}},
		{"d-pad", func(app *nsgotest.App, g *roguelikeGame) {
			btn := map[cell]gamepad.Button{{Y: -1}: gamepad.ButtonDPadUp, {Y: 1}: gamepad.ButtonDPadDown, {X: -1}: gamepad.ButtonDPadLeft, {X: 1}: gamepad.ButtonDPadRight}[openStep(g.run)]
			app.ConnectGamepad(1, "Test Controller")
			app.Advance(frame)
			press(app, func() { app.SetGamepadButton(1, btn, 1) }, func() { app.SetGamepadButton(1, btn, 0) })
		}},
		{"stick", func(app *nsgotest.App, g *roguelikeGame) {
			app.ConnectGamepad(1, "Test Controller")
			app.Advance(frame)
			controller{app}.perform(action{kind: actMove, dir: openStep(g.run)})
		}},
		{"touch", func(app *nsgotest.App, g *roguelikeGame) {
			touchscreen{t, app, g}.perform(action{kind: actMove, dir: openStep(g.run)})
		}},
		{"period", func(app *nsgotest.App, _ *roguelikeGame) {
			press(app, func() { app.KeyCodeDown("Period") }, func() { app.KeyCodeUp("Period") })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, g := startGame(t, []uint64{winSeed})
			tc.do(app, g)
			if g.run.turns != 1 {
				t.Fatalf("%s took %d turns, want 1", tc.name, g.run.turns)
			}
		})
	}
}

// TestHoldingADirectionRepeats holds a direction for a second and checks that it steps once at once and then repeats, as holding a key does.
func TestHoldingADirectionRepeats(t *testing.T) {
	app, g := startGame(t, []uint64{winSeed})
	dir := openStep(g.run)
	key := map[cell]string{{Y: -1}: "up", {Y: 1}: "down", {X: -1}: "left", {X: 1}: "right"}[dir]
	app.KeyDown(key)
	app.Advance(frame)
	if g.run.turns != 1 {
		t.Fatalf("pressing %s took %d turns, want 1", key, g.run.turns)
	}
	app.Advance(time.Second)
	app.KeyUp(key)
	// One step, then one after repeatDelay frames and one every repeatEvery frames, for as long as the floor lets the player walk.
	if want := 1 + 1 + (60-repeatDelay)/repeatEvery; g.run.turns < 2 || g.run.turns > want {
		t.Fatalf("holding %s for a second took %d turns, want 2 to %d", key, g.run.turns, want)
	}
}

func TestRequestsOrientation(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(960, 540), nsgotest.ManualFrames())
	if app.Orientation != core.OrientationLandscape {
		t.Fatalf("orientation = %v, want landscape (%v)", app.Orientation, core.OrientationLandscape)
	}
}

// openStep returns a direction the player can walk from where they stand.
func openStep(r *run) cell {
	for _, st := range steps {
		to := cell{X: r.pos.X + st.X, Y: r.pos.Y + st.Y}
		if _, taken := r.floor.foeAt(to); r.floor.walkable(to) && !taken {
			return st
		}
	}
	panic("the player is walled in")
}

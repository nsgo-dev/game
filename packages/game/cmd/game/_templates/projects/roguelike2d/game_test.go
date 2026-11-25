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
	app *nsgotest.App
	g   *roguelikeGame
}

func (s touchscreen) perform(a action) {
	name := map[string]string{abilityQuaff: actionQuaff, abilityCleave: actionCleave}[a.ability]
	switch a.kind {
	case actWait:
		name = actionWait
	case actMove:
		name = map[cell]string{{Y: -1}: "up", {Y: 1}: "down", {X: -1}: "left", {X: 1}: "right"}[a.dir]
	}
	var finger *nsgotest.Finger
	press(s.app, func() { finger = s.app.Touch("game-view", regionCenter(s.g, name)) }, func() { finger.Lift() })
}

// regionCenter returns the middle of the region game.yaml binds to action, in the game view's points.
func regionCenter(g *roguelikeGame, action string) core.Point {
	r, ok := Project.Region(action)
	if !ok {
		panic("game.yaml binds no region to " + action)
	}
	return g.viewport.GameToView(g.currentViewSize(), core.Point{X: r.Origin.X + r.Size.Width/2, Y: r.Origin.Y + r.Size.Height/2})
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
	app, g := startGame(t, []uint64{winSeed})
	play(t, g, touchscreen{app, g}, botClear, 5000)
	checkWin(t, app, g)
	if !g.showTouchControls() {
		t.Error("the on-screen controls are hidden after the screen was touched")
	}
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

// TestEachBindingActs checks every binding of the movement and wait actions takes a turn: keys, physical keys, the d-pad, the stick, touch and the mouse over the on-screen controls.
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
			touchscreen{app, g}.perform(action{kind: actMove, dir: openStep(g.run)})
		}},
		{"click", func(app *nsgotest.App, g *roguelikeGame) {
			name := map[cell]string{{Y: -1}: "up", {Y: 1}: "down", {X: -1}: "left", {X: 1}: "right"}[openStep(g.run)]
			at := regionCenter(g, name)
			press(app, func() { app.PressDown("game-view"); app.HoverAt("game-view", at) }, func() { app.PressUp("game-view") })
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

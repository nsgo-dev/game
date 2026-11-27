package main

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/fighting"
	"github.com/nsgo-dev/game/pkg/placeholder"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// frame is one fixed step of the game loop.
const frame = time.Second / stepRate

// matchLimit is the longest a whole match can take: every round to its timer, with the announcements between them.
const matchLimit = 5 * 70 * time.Second

func startGame(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *fightingGame) {
	t.Helper()
	var g *fightingGame
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return rootWithHook(cx, func(x *fightingGame) { g = x })
	}, append([]nsgotest.Option{nsgotest.Size(960, 540)}, opts...)...)
	app.Advance(3 * frame)
	if g == nil || !app.Exists("id:title-menu") {
		t.Fatal("the game did not start on its title screen")
	}
	return app, g
}

// withSound gives the headless platform audio and has it decode the sounds the game plays, which it does for those it is told the lengths of.
func withSound(t *testing.T) []nsgotest.Option {
	return []nsgotest.Option{nsgotest.Environment(core.Environment{Audio: true}), func(app *nsgotest.App) {
		app.AudioDurations = map[string]time.Duration{}
		for _, name := range sfx {
			src, err := placeholder.Sound(name)
			if err != nil {
				t.Fatal(err)
			}
			app.AudioDurations[string(src.Data)] = 300 * time.Millisecond
		}
	}}
}

// tap holds key for two steps, so the fixed-step loop sees it go down and come up.
func tap(app *nsgotest.App, key string) {
	app.KeyDown(key)
	app.Advance(2 * frame)
	app.KeyUp(key)
	app.Advance(2 * frame)
}

// tapPad holds button b of controller id for two steps.
func tapPad(app *nsgotest.App, id int, b gamepad.Button) {
	app.SetGamepadButton(id, b, 1)
	app.Advance(2 * frame)
	app.SetGamepadButton(id, b, 0)
	app.Advance(2 * frame)
}

// untilFight advances to the end of the round announcement.
func untilFight(t *testing.T, app *nsgotest.App, g *fightingGame) {
	t.Helper()
	for range 4 * stepRate {
		if g.m != nil && g.m.phase == phaseFight {
			return
		}
		app.Advance(frame)
	}
	t.Fatal("the round never started")
}

// untilResult advances, calling each step before every step when it is not nil, until the result screen shows, and returns its text.
func untilResult(t *testing.T, app *nsgotest.App, g *fightingGame, each func()) string {
	t.Helper()
	for elapsed := time.Duration(0); elapsed < matchLimit; elapsed += frame {
		if g.scr == screenResult {
			return app.Find("id:result").Text
		}
		if each != nil {
			each()
		}
		app.Advance(frame)
	}
	t.Fatalf("the match did not end within %v", matchLimit)
	return ""
}

// TestLossAgainstTheCPU starts a match against the CPU from the keyboard and stands still until the CPU wins it.
func TestLossAgainstTheCPU(t *testing.T) {
	app, g := startGame(t, withSound(t)...)
	tap(app, "enter")
	if g.scr != screenFight || g.mode != modeCPU {
		t.Fatalf("Enter on the title screen: screen %v mode %v, want a fight against the CPU", g.scr, g.mode)
	}
	app.Find("id:hud")
	if got := app.Find("id:banner").Text; got != "Round 1" {
		t.Fatalf("banner %q, want Round 1", got)
	}
	untilFight(t, app, g)
	if got := untilResult(t, app, g, nil); got != "You lose" {
		t.Fatalf("result %q, want You lose", got)
	}
	if got, want := g.m.players[1].wins, g.roster.file.Rules.RoundsToWin; got != want {
		t.Fatalf("the CPU won %d rounds, want %d", got, want)
	}
	if plays := soundPlays(t, app, "hit"); plays == 0 {
		t.Error("the CPU's hits played no sound")
	}
	// Enter on the result screen asks for a rematch, Escape goes back to the title screen.
	tap(app, "escape")
	if g.scr != screenTitle || !app.Exists("id:title-menu") {
		t.Fatalf("Escape on the result screen: screen %v, want the title screen", g.scr)
	}
}

// soundPlays returns how often the placeholder pack's sound name played.
func soundPlays(t *testing.T, app *nsgotest.App, name string) int {
	t.Helper()
	src, err := placeholder.Sound(name)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range app.Sounds {
		if bytes.Equal(s.Source.Data, src.Data) {
			return len(s.Plays)
		}
	}
	t.Fatalf("the sound %q was never loaded", name)
	return 0
}

// keyBot plays the first player from the keyboard against the CPU: it guards what it sees coming, throws its projectile from afar, closes in and strikes up close.
type keyBot struct {
	app  *nsgotest.App
	g    *fightingGame
	held []string
	// plan is a script of key sets, one per step.
	plan [][]string
	// seen are the moves the bot's fighter played.
	seen map[string]bool
}

// keys returns the keyboard keys for a direction relative to the first player's facing and its buttons, as game.yaml binds them.
func (b *keyBot) keys(d fighting.Direction, buttons ...string) []string {
	p := b.g.m.players[0]
	abs := p.facing().Relative(d)
	var keys []string
	switch abs.X() {
	case -1:
		keys = append(keys, "a")
	case 1:
		keys = append(keys, "d")
	}
	switch abs.Y() {
	case -1:
		keys = append(keys, "s")
	case 1:
		keys = append(keys, "w")
	}
	for _, btn := range buttons {
		keys = append(keys, map[string]string{"light": "j", "heavy": "k"}[btn])
	}
	return keys
}

func (b *keyBot) add(steps int, keys []string) {
	for range steps {
		b.plan = append(b.plan, keys)
	}
}

// step presses and releases keys for the next step.
func (b *keyBot) step() {
	m := b.g.m
	if m != nil {
		b.seen[m.players[0].moveName()] = true
	}
	if m == nil || m.phase != phaseFight {
		b.press(nil)
		b.plan = nil
		return
	}
	if len(b.plan) == 0 {
		b.decide(m)
	}
	var want []string
	if len(b.plan) > 0 {
		want, b.plan = b.plan[0], b.plan[1:]
	}
	b.press(want)
}

func (b *keyBot) decide(m *match) {
	me, o := m.players[0], m.players[1]
	d := abs32(o.f.X - me.f.X)
	switch {
	case m.threatened(me):
		guard := fighting.Back
		if attackLevel(o) == levelLow {
			guard = fighting.DownBack
		}
		b.add(6, b.keys(guard))
	case d >= 150 && me.shot == nil:
		b.add(2, b.keys(fighting.Down))
		b.add(2, b.keys(fighting.DownForward))
		b.add(2, b.keys(fighting.Forward, "light"))
		b.add(12, nil)
	case d < 62:
		b.add(2, b.keys(fighting.Neutral, "light"))
		b.add(6, nil)
		b.add(2, b.keys(fighting.Neutral, "heavy"))
		b.add(10, nil)
	default:
		b.add(6, b.keys(fighting.Forward))
	}
}

func (b *keyBot) press(want []string) {
	for _, k := range b.held {
		if !slices.Contains(want, k) {
			b.app.KeyUp(k)
		}
	}
	for _, k := range want {
		if !slices.Contains(b.held, k) {
			b.app.KeyDown(k)
		}
	}
	b.held = slices.Clone(want)
}

// TestWinAgainstTheCPU plays a match against the CPU from the keyboard, with special moves from motion inputs, guards and combos, and wins it.
func TestWinAgainstTheCPU(t *testing.T) {
	app, g := startGame(t)
	tap(app, "enter")
	untilFight(t, app, g)
	bot := &keyBot{app: app, g: g, seen: map[string]bool{}}
	if got := untilResult(t, app, g, bot.step); got != "You win" {
		t.Fatalf("result %q after %d rounds (%d to %d), want You win", got, g.m.round, g.m.players[0].wins, g.m.players[1].wins)
	}
	for _, move := range []string{"gale", "light", "heavy", "guard", "walk"} {
		if !bot.seen[move] {
			t.Errorf("the winning player never played %s; it played %v", move, bot.seen)
		}
	}
	bot.press(nil)
	// Enter asks for a rematch.
	tap(app, "enter")
	if g.scr != screenFight || g.m.round != 1 || g.m.players[0].wins != 0 {
		t.Fatal("Enter on the result screen did not start a rematch")
	}
}

// TestTwoGamepadsPlayVersus has the second controller join from the title screen and each controller play its own fighter, through a pause, to a win.
func TestTwoGamepadsPlayVersus(t *testing.T) {
	app, g := startGame(t)
	app.ConnectGamepad(1, "Pad one")
	app.ConnectGamepad(2, "Pad two")
	app.Advance(2 * frame)
	// The second controller's D-pad picks its fighter and its Start starts a versus match.
	tapPad(app, 2, gamepad.ButtonDPadRight)
	if picks := g.picks.GetUntracked(); picks[1] != 0 {
		t.Fatalf("the second pad's D-pad right picked fighter %d for the second player, want 0", picks[1])
	}
	tapPad(app, 2, gamepad.ButtonDPadRight)
	tapPad(app, 2, gamepad.ButtonStart)
	if g.scr != screenFight || g.mode != modeVersus {
		t.Fatalf("the second pad's Start: screen %v mode %v, want a versus fight", g.scr, g.mode)
	}
	untilFight(t, app, g)
	a, b := g.m.players[0], g.m.players[1]

	// Each pad walks its own fighter.
	ax, bx := a.f.X, b.f.X
	app.SetGamepadButton(1, gamepad.ButtonDPadRight, 1)
	app.Advance(20 * frame)
	app.SetGamepadButton(1, gamepad.ButtonDPadRight, 0)
	if a.f.X <= ax || b.f.X != bx {
		t.Fatalf("pad one's D-pad moved the fighters %.1f -> %.1f and %.1f -> %.1f, want only the first forward", ax, a.f.X, bx, b.f.X)
	}
	app.SetGamepadAxis(2, gamepad.AxisLeftX, -1)
	app.Advance(20 * frame)
	app.SetGamepadAxis(2, gamepad.AxisLeftX, 0)
	if b.f.X >= bx {
		t.Fatalf("pad two's stick did not walk the second fighter forward: %.1f -> %.1f", bx, b.f.X)
	}

	// Start pauses and resumes.
	tapPad(app, 1, gamepad.ButtonStart)
	if g.scr != screenPaused || !app.Exists("id:pause-menu") {
		t.Fatal("Start did not pause the fight")
	}
	steps := g.m.steps
	app.Advance(30 * frame)
	if g.m.steps != steps {
		t.Fatal("the match went on while paused")
	}
	tapPad(app, 2, gamepad.ButtonStart)
	if g.scr != screenFight {
		t.Fatal("the second pad's Start did not resume the fight")
	}

	// The first pad walks in and strikes until the match is won; the second stands still.
	got := untilResult(t, app, g, func() {
		m := g.m
		if m.phase != phaseFight {
			app.SetGamepadButton(1, gamepad.ButtonDPadRight, 0)
			app.SetGamepadButton(1, gamepad.ButtonWest, 0)
			return
		}
		me, o := m.players[0], m.players[1]
		near := abs32(o.f.X-me.f.X) < 50
		forward := gamepad.ButtonDPadRight
		if me.f.FacingLeft {
			forward = gamepad.ButtonDPadLeft
		}
		app.SetGamepadButton(1, gamepad.ButtonDPadRight, 0)
		app.SetGamepadButton(1, gamepad.ButtonDPadLeft, 0)
		if !near {
			app.SetGamepadButton(1, forward, 1)
		}
		strike := float32(0)
		if near && m.steps%8 < 2 {
			strike = 1
		}
		app.SetGamepadButton(1, gamepad.ButtonWest, strike)
	})
	if got != "Player 1 wins" {
		t.Fatalf("result %q, want Player 1 wins", got)
	}
}

// TestScreenChangeKeepsEachPadToItsPlayer presses the second controller on the very first step after a screen change, when the actions have just been reset and remember no controllers, and checks that only the second player sees it.
func TestScreenChangeKeepsEachPadToItsPlayer(t *testing.T) {
	app, g := startGame(t)
	app.ConnectGamepad(1, "Pad one")
	app.ConnectGamepad(2, "Pad two")
	app.Advance(2 * frame)
	g.picks.Set([2]int{0, 1})
	g.startMatch(modeVersus)
	untilFight(t, app, g)
	g.setScreen(screenFight)
	app.SetGamepadButton(2, gamepad.ButtonWest, 1)
	g.controls.step(true)
	app.SetGamepadButton(2, gamepad.ButtonWest, 0)
	if g.controls.actions[0].Pressed("p1_light") || !g.controls.actions[1].Pressed("p2_light") {
		t.Fatalf("pad two's light after a screen change: player 1 %v, player 2 %v, want only player 2", g.controls.actions[0].Pressed("p1_light"), g.controls.actions[1].Pressed("p2_light"))
	}
}

// TestTouchPlays starts a match from the title screen's button and plays it with the on-screen stick and attack buttons.
func TestTouchPlays(t *testing.T) {
	app, g := startGame(t)
	app.Press("id:vs-cpu")
	if g.scr != screenFight || g.mode != modeCPU {
		t.Fatal("the Versus CPU button did not start a fight against the CPU")
	}
	untilFight(t, app, g)
	p := g.m.players[0]
	view := func(x, y float32) core.Point {
		return g.viewport.GameToView(g.viewSize, core.Point{X: core.Points(x), Y: core.Points(y)})
	}

	// Pushing the stick toward the opponent walks forward.
	x := p.f.X
	finger := app.Touch("id:arena", view(60, 210))
	finger.Move(view(100, 210))
	app.Advance(20 * frame)
	finger.Lift()
	if !g.controls.touched {
		t.Fatal("the touch was not seen")
	}
	if p.f.X <= x {
		t.Fatalf("the stick pushed forward moved the fighter %.1f -> %.1f", x, p.f.X)
	}

	// The light button strikes.
	r, _ := Project.Region("p1_light")
	app.Advance(10 * frame)
	finger = app.Touch("id:arena", view(float32(r.Origin.X+r.Size.Width/2), float32(r.Origin.Y+r.Size.Height/2)))
	app.Advance(2 * frame)
	if p.moveName() != "light" {
		t.Fatalf("the light button started %q, want light", p.moveName())
	}
	finger.Lift()

	// The pause button pauses, and the menu's buttons resume and quit.
	app.Press("id:pause-btn")
	if g.scr != screenPaused {
		t.Fatal("the pause button did not pause")
	}
	app.Press("id:resume-btn")
	if g.scr != screenFight {
		t.Fatal("Resume did not resume")
	}
	app.Press("id:pause-btn")
	app.Press("id:quit-btn")
	if g.scr != screenTitle || g.m != nil {
		t.Fatal("Quit did not go back to the title screen")
	}
}

// TestTitlePicksFighters cycles the fighters from the title screen's buttons and keys.
func TestTitlePicksFighters(t *testing.T) {
	app, g := startGame(t)
	app.Find("text:P1  Kestrel  ▸")
	app.Press("id:pick-p1")
	if picks := g.picks.GetUntracked(); picks != [2]int{1, 1} {
		t.Fatalf("picks %v after pressing the first player's pick, want [1 1]", picks)
	}
	tap(app, "left")
	if picks := g.picks.GetUntracked(); picks != [2]int{1, 0} {
		t.Fatalf("picks %v after the left arrow, the second player's left, want [1 0]", picks)
	}
	tap(app, "enter")
	if g.m.players[0].fighter.def.ID != "basalt" || g.m.players[1].fighter.def.ID != "kestrel" {
		t.Fatalf("the match is %s against %s, want basalt against kestrel", g.m.players[0].fighter.def.ID, g.m.players[1].fighter.def.ID)
	}
}

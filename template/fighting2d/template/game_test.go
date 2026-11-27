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
	g.controls.step(frame, false)
	app.SetGamepadButton(2, gamepad.ButtonWest, 0)
	if g.controls.pressed(0, "p1_light") || !g.controls.pressed(1, "p2_light") {
		t.Fatalf("pad two's light after a screen change: player 1 %v, player 2 %v, want only player 2", g.controls.pressed(0, "p1_light"), g.controls.pressed(1, "p2_light"))
	}
}

// TestVersusDisconnectPausesAndReconnectRestoresPlayer checks that disconnecting player 1's controller in a versus match releases held input, keeps pad 2 on player 2, pauses with the reconnect panel, and resumes with pad 1 back on player 1 once it reconnects.
func TestVersusDisconnectPausesAndReconnectRestoresPlayer(t *testing.T) {
	app, g := startGame(t)
	app.ConnectGamepad(1, "Pad one")
	app.ConnectGamepad(2, "Pad two")
	app.Advance(2 * frame)
	tapPad(app, 2, gamepad.ButtonStart)
	untilFight(t, app, g)
	a, b := g.m.players[0], g.m.players[1]

	// Hold D-pad right on pad 1, then drop pad 1 mid-round.
	app.SetGamepadButton(1, gamepad.ButtonDPadRight, 1)
	app.Advance(4 * frame)
	app.DisconnectGamepad(1)
	app.Advance(2 * frame)
	if g.scr != screenReconnect || !app.Exists("id:reconnect-menu") {
		t.Fatalf("after disconnecting pad 1: screen %v reconnect-menu=%v, want screenReconnect with panel", g.scr, app.Exists("id:reconnect-menu"))
	}
	if got := app.Find("id:reconnect-title").Text; got != "Player 1: reconnect your controller" {
		t.Fatalf("reconnect title %q, want %q", got, "Player 1: reconnect your controller")
	}
	if g.controls.pressed(0, "p1_right") {
		t.Fatal("player 1 kept p1_right held after pad 1 disconnected")
	}

	// While paused, pad 2 stays on player 2 and does not move player 1 or advance the match.
	ax, bx, steps := a.f.X, b.f.X, g.m.steps
	app.SetGamepadButton(2, gamepad.ButtonDPadRight, 1)
	app.Advance(20 * frame)
	app.SetGamepadButton(2, gamepad.ButtonDPadRight, 0)
	if a.f.X != ax || b.f.X != bx || g.m.steps != steps || g.scr != screenReconnect {
		t.Fatalf("pad 2 during pad 1 disconnect: p1 %.1f->%.1f, p2 %.1f->%.1f, steps %d->%d, screen %v", ax, a.f.X, bx, b.f.X, steps, g.m.steps, g.scr)
	}

	// Reconnecting pad 1 closes the panel and puts pad 1 back on player 1.
	app.ConnectGamepad(1, "Pad one")
	app.Advance(2 * frame)
	if g.scr != screenFight || app.Exists("id:reconnect-menu") {
		t.Fatalf("after reconnecting pad 1: screen %v reconnect-menu=%v, want screenFight without panel", g.scr, app.Exists("id:reconnect-menu"))
	}
	app.SetGamepadButton(1, gamepad.ButtonDPadRight, 1)
	app.Advance(20 * frame)
	app.SetGamepadButton(1, gamepad.ButtonDPadRight, 0)
	if a.f.X <= ax || b.f.X != bx {
		t.Fatalf("reconnected pad 1 moved fighters %.1f->%.1f and %.1f->%.1f, want only player 1 forward", ax, a.f.X, bx, b.f.X)
	}
	app.SetGamepadAxis(2, gamepad.AxisLeftX, -1)
	app.Advance(20 * frame)
	app.SetGamepadAxis(2, gamepad.AxisLeftX, 0)
	if b.f.X >= bx {
		t.Fatalf("pad 2 no longer moved player 2 after pad 1 reconnected: %.1f->%.1f", bx, b.f.X)
	}
}

// TestVersusDisconnectReassignsFreePadAndContinuesOnKeyboard checks recovery from a versus controller disconnect by pressing a button on an unassigned controller, continuing on the keyboard via the panel button or Enter, and recovering from a simultaneous two-pad disconnect.
func TestVersusDisconnectReassignsFreePadAndContinuesOnKeyboard(t *testing.T) {
	app, g := startGame(t)
	app.ConnectGamepad(1, "Pad one")
	app.ConnectGamepad(2, "Pad two")
	app.ConnectGamepad(3, "Pad three")
	app.SetGamepadButton(3, gamepad.ButtonWest, 1)
	app.Advance(2 * frame)
	tapPad(app, 2, gamepad.ButtonStart)
	untilFight(t, app, g)
	a, b := g.m.players[0], g.m.players[1]

	// Disconnect pad 1; pad 3's already-held button must not steal player 1's slot.
	app.DisconnectGamepad(1)
	app.Advance(5 * frame)
	if g.scr != screenReconnect {
		t.Fatalf("already-held button on pad 3 stole player 1's slot: screen %v", g.scr)
	}

	// Newly pressing Start on free pad 3 claims player 1 and resumes without immediately opening the pause menu.
	app.SetGamepadButton(3, gamepad.ButtonStart, 1)
	app.Advance(2 * frame)
	app.SetGamepadButton(3, gamepad.ButtonStart, 0)
	app.SetGamepadButton(3, gamepad.ButtonWest, 0)
	app.Advance(2 * frame)
	if g.scr != screenFight {
		t.Fatalf("pressing Start on free pad 3: screen %v, want screenFight", g.scr)
	}
	ax, bx := a.f.X, b.f.X
	app.SetGamepadButton(3, gamepad.ButtonDPadRight, 1)
	app.Advance(20 * frame)
	app.SetGamepadButton(3, gamepad.ButtonDPadRight, 0)
	if a.f.X <= ax || b.f.X != bx {
		t.Fatalf("reassigned pad 3 moved fighters %.1f->%.1f and %.1f->%.1f, want only player 1", ax, a.f.X, bx, b.f.X)
	}

	// Disconnect pad 2: player 1's pad cannot dismiss player 2's prompt, and Continue on keyboard resumes with player 2 on arrow keys.
	app.DisconnectGamepad(2)
	app.Advance(2 * frame)
	if g.scr != screenReconnect || app.Find("id:reconnect-title").Text != "Player 2: reconnect your controller" {
		t.Fatalf("after disconnecting pad 2: screen %v title %q", g.scr, app.Find("id:reconnect-title").Text)
	}
	tapPad(app, 3, gamepad.ButtonStart)
	if g.scr != screenReconnect {
		t.Fatalf("player 1's Start dismissed player 2's reconnect panel: screen %v", g.scr)
	}
	app.Press("id:continue-keyboard-btn")
	app.Advance(2 * frame)
	if g.scr != screenFight {
		t.Fatalf("Continue on keyboard did not resume: screen %v", g.scr)
	}
	bx = b.f.X
	app.KeyDown("left")
	app.Advance(20 * frame)
	app.KeyUp("left")
	if b.f.X >= bx {
		t.Fatalf("player 2 could not move on keyboard after continuing on keyboard: %.1f->%.1f", bx, b.f.X)
	}

	// Simultaneous disconnect: start fresh with pads 1 and 2, drop both, recover pad 2 first, then let player 1 continue on keyboard with Enter.
	app.Press("id:pause-btn")
	app.Press("id:quit-btn")
	app.DisconnectGamepad(3)
	app.ConnectGamepad(1, "Pad one")
	app.ConnectGamepad(2, "Pad two")
	app.Advance(2 * frame)
	tapPad(app, 2, gamepad.ButtonStart)
	untilFight(t, app, g)
	a, b = g.m.players[0], g.m.players[1]

	app.DisconnectGamepad(1)
	app.DisconnectGamepad(2)
	app.Advance(2 * frame)
	if g.scr != screenReconnect || app.Find("id:reconnect-title").Text != "Players 1 & 2: reconnect your controllers" {
		t.Fatalf("both pads disconnected: screen %v title %q", g.scr, app.Find("id:reconnect-title").Text)
	}
	app.ConnectGamepad(2, "Pad two")
	app.Advance(2 * frame)
	if g.scr != screenReconnect || app.Find("id:reconnect-title").Text != "Player 1: reconnect your controller" {
		t.Fatalf("after pad 2 returned while pad 1 still out: screen %v title %q", g.scr, app.Find("id:reconnect-title").Text)
	}
	tap(app, "enter")
	if g.scr != screenFight {
		t.Fatalf("Enter on player 1 reconnect panel did not continue on keyboard: screen %v", g.scr)
	}
	ax, bx = a.f.X, b.f.X
	app.KeyDown("d")
	app.Advance(20 * frame)
	app.KeyUp("d")
	if a.f.X <= ax || b.f.X != bx {
		t.Fatalf("player 1 keyboard after Enter: p1 %.1f->%.1f p2 %.1f->%.1f", ax, a.f.X, bx, b.f.X)
	}
}

// TestMidMatchControllerConnectDoesNotStealSlot checks that a controller connecting mid-match, even with a lower ID than an assigned controller, is never handed to a fighter that already has one.
func TestMidMatchControllerConnectDoesNotStealSlot(t *testing.T) {
	t.Run("two_pads_lower_id_connects", func(t *testing.T) {
		app, g := startGame(t)
		app.ConnectGamepad(2, "Pad two")
		app.ConnectGamepad(3, "Pad three")
		app.Advance(2 * frame)
		tapPad(app, 3, gamepad.ButtonStart)
		untilFight(t, app, g)
		a, b := g.m.players[0], g.m.players[1]

		// Connect pad 1 (lower ID than both 2 and 3) mid-match.
		app.ConnectGamepad(1, "Pad one")
		app.Advance(2 * frame)
		ax, bx := a.f.X, b.f.X
		app.SetGamepadButton(1, gamepad.ButtonDPadRight, 1)
		app.Advance(20 * frame)
		app.SetGamepadButton(1, gamepad.ButtonDPadRight, 0)
		if a.f.X != ax || b.f.X != bx {
			t.Fatalf("mid-match pad 1 moved fighters: p1 %.1f->%.1f p2 %.1f->%.1f", ax, a.f.X, bx, b.f.X)
		}

		// Pads 2 and 3 still drive players 1 and 2 respectively.
		app.SetGamepadButton(2, gamepad.ButtonDPadRight, 1)
		app.Advance(20 * frame)
		app.SetGamepadButton(2, gamepad.ButtonDPadRight, 0)
		if a.f.X <= ax || b.f.X != bx {
			t.Fatalf("pad 2 after mid-match pad 1 connect: p1 %.1f->%.1f p2 %.1f->%.1f", ax, a.f.X, bx, b.f.X)
		}
		ax = a.f.X
		app.SetGamepadButton(3, gamepad.ButtonDPadLeft, 1)
		app.Advance(20 * frame)
		app.SetGamepadButton(3, gamepad.ButtonDPadLeft, 0)
		if b.f.X >= bx || a.f.X != ax {
			t.Fatalf("pad 3 after mid-match pad 1 connect: p1 %.1f->%.1f p2 %.1f->%.1f", ax, a.f.X, bx, b.f.X)
		}
	})

	t.Run("one_pad_lower_id_connects", func(t *testing.T) {
		app, g := startGame(t)
		app.ConnectGamepad(2, "Pad two")
		app.Advance(2 * frame)
		app.Press("id:vs-player")
		untilFight(t, app, g)
		a, b := g.m.players[0], g.m.players[1]

		// Connect pad 1 (lower ID than player 1's pad 2) mid-match.
		app.ConnectGamepad(1, "Pad one")
		app.Advance(2 * frame)
		ax, bx := a.f.X, b.f.X
		app.SetGamepadButton(1, gamepad.ButtonDPadRight, 1)
		app.Advance(20 * frame)
		app.SetGamepadButton(1, gamepad.ButtonDPadRight, 0)
		if a.f.X != ax || b.f.X != bx {
			t.Fatalf("mid-match pad 1 took over a fighter: p1 %.1f->%.1f p2 %.1f->%.1f", ax, a.f.X, bx, b.f.X)
		}

		// Player 1 still uses pad 2.
		app.SetGamepadButton(2, gamepad.ButtonDPadRight, 1)
		app.Advance(20 * frame)
		app.SetGamepadButton(2, gamepad.ButtonDPadRight, 0)
		if a.f.X <= ax || b.f.X != bx {
			t.Fatalf("pad 2 lost player 1 after mid-match pad 1 connect: p1 %.1f->%.1f p2 %.1f->%.1f", ax, a.f.X, bx, b.f.X)
		}
	})
}

// TestManualPauseDuringDisconnectDoesNotAutoUnpause checks that reconnecting while manually paused stays on the pause menu, and resuming while a controller is still disconnected opens the reconnect panel.
func TestManualPauseDuringDisconnectDoesNotAutoUnpause(t *testing.T) {
	app, g := startGame(t)
	app.ConnectGamepad(1, "Pad one")
	app.ConnectGamepad(2, "Pad two")
	app.Advance(2 * frame)
	tapPad(app, 2, gamepad.ButtonStart)
	untilFight(t, app, g)

	tapPad(app, 1, gamepad.ButtonStart)
	if g.scr != screenPaused {
		t.Fatalf("Start did not open pause menu: screen %v", g.scr)
	}
	app.DisconnectGamepad(1)
	app.Advance(2 * frame)
	if g.scr != screenPaused {
		t.Fatalf("disconnect while manually paused changed screen to %v", g.scr)
	}
	app.ConnectGamepad(1, "Pad one")
	app.Advance(2 * frame)
	if g.scr != screenPaused {
		t.Fatalf("reconnecting while manually paused auto-unpaused to %v", g.scr)
	}

	app.DisconnectGamepad(1)
	app.Advance(2 * frame)
	app.Press("id:resume-btn")
	if g.scr != screenReconnect {
		t.Fatalf("Resume while pad 1 still disconnected: screen %v, want screenReconnect", g.scr)
	}
	app.ConnectGamepad(1, "Pad one")
	app.Advance(2 * frame)
	if g.scr != screenFight {
		t.Fatalf("reconnecting pad 1 from reconnect panel: screen %v, want screenFight", g.scr)
	}
}

// TestReconnectPanelRendersOnTarget triggers a versus controller disconnect through Players.OnGamepads so the reconnect panel can be verified and screenshotted on real targets as well as headless.
func TestReconnectPanelRendersOnTarget(t *testing.T) {
	app, g := startGame(t)
	app.Press("id:vs-player")
	app.Post(func() {
		for range g.roster.file.Rules.IntroSteps + 45 {
			g.fightStep()
		}
		g.frame.Update(func(n uint64) uint64 { return n + 1 })
	})
	app.Settle()
	if g.scr != screenFight || g.mode != modeVersus {
		t.Fatalf("Versus player button: screen %v mode %v, want versus screenFight", g.scr, g.mode)
	}
	app.Screenshot("fighting2d/before-disconnect")

	app.Post(func() {
		g.controls.players.AssignGamepad(0, 1)
		g.controls.players.OnGamepads([]gamepad.Pad{{ID: 1, Name: "Pad one"}})
		g.controls.players.OnGamepads(nil)
	})
	app.Settle()
	if g.scr != screenReconnect || !app.Exists("id:reconnect-menu") {
		t.Fatalf("after simulated disconnect: screen %v reconnect-menu=%v, want screenReconnect", g.scr, app.Exists("id:reconnect-menu"))
	}
	if got := app.Find("id:reconnect-title").Text; got != "Player 1: reconnect your controller" {
		t.Fatalf("reconnect title %q, want %q", got, "Player 1: reconnect your controller")
	}
	app.Screenshot("fighting2d/after-reconnect-panel")

	app.Press("id:continue-keyboard-btn")
	app.Settle()
	if g.scr != screenFight || app.Exists("id:reconnect-menu") {
		t.Fatalf("Continue on keyboard did not dismiss panel: screen %v", g.scr)
	}
}

func controlFrame(t *testing.T, tc *fightingGame, player int, id string) core.Bounds {
	t.Helper()
	for _, c := range tc.controls.touch[player].Controls() {
		if c.ID == id {
			return c.Frame
		}
	}
	t.Fatalf("player %d touch control %q not found", player+1, id)
	return core.Bounds{}
}

func centerOf(b core.Bounds) core.Point {
	return core.Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height/2}
}

func overlaps(a, b core.Bounds) bool {
	return a.Origin.X < b.Origin.X+b.Size.Width && b.Origin.X < a.Origin.X+a.Size.Width &&
		a.Origin.Y < b.Origin.Y+b.Size.Height && b.Origin.Y < a.Origin.Y+a.Size.Height
}

// TestTouchPlays starts a match from the title screen's button and plays it with the on-screen stick and attack buttons.
func TestTouchPlays(t *testing.T) {
	app, g := startGame(t, nsgotest.Idiom(core.IdiomDesktop))
	app.Press("id:vs-cpu")
	if g.scr != screenFight || g.mode != modeCPU {
		t.Fatal("the Versus CPU button did not start a fight against the CPU")
	}
	untilFight(t, app, g)
	if g.controls.touch[0].Visible() || app.Exists("id:touch-light") {
		t.Fatal("the touch controls show on a desktop before any touch")
	}
	p := g.m.players[0]

	// Pushing the stick toward the opponent walks forward.
	x := p.f.X
	moveAt := centerOf(controlFrame(t, g, 0, "move"))
	finger := app.Touch("id:arena", moveAt)
	finger.Move(core.Point{X: moveAt.X + 45, Y: moveAt.Y})
	app.Advance(20 * frame)
	finger.Lift()
	if !g.controls.touch[0].Visible() || !app.Exists("id:touch-light") {
		t.Fatal("the touch controls did not show after a touch")
	}
	if p.f.X <= x {
		t.Fatalf("the stick pushed forward moved the fighter %.1f -> %.1f", x, p.f.X)
	}

	// The light and heavy buttons strike.
	app.Advance(10 * frame)
	finger = app.Touch("id:arena", centerOf(controlFrame(t, g, 0, "light")))
	app.Advance(2 * frame)
	if p.moveName() != "light" {
		t.Fatalf("the light button started %q, want light", p.moveName())
	}
	finger.Lift()
	app.Advance(20 * frame)

	finger = app.Touch("id:arena", centerOf(controlFrame(t, g, 0, "heavy")))
	app.Advance(2 * frame)
	if p.moveName() != "heavy" {
		t.Fatalf("the heavy button started %q, want heavy", p.moveName())
	}
	finger.Lift()
	app.Advance(30 * frame)

	// Pushing the stick up jumps (up-positive p1_move_y).
	finger = app.Touch("id:arena", moveAt)
	finger.Move(core.Point{X: moveAt.X, Y: moveAt.Y - 45})
	app.Advance(4 * frame)
	finger.Lift()
	if !p.airborne {
		t.Fatalf("pushing the stick up did not jump: move %q, airborne=%v", p.moveName(), p.airborne)
	}

	// Using the keyboard hides the touch controls again.
	tap(app, "d")
	if g.controls.touch[0].Visible() || app.Exists("id:touch-light") {
		t.Fatal("the keyboard did not hide the touch controls")
	}

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

// TestTouchVersusPlaysTwoPlayersOnOnePhone checks that on a phone (native and browser) local versus shows P1's compacted controls on the left half and P2's mirrored controls on the right half from the first frame, inside the safe area, at least 44pt, without overlap, and lets both players move and strike independently and at the same time.
func TestTouchVersusPlaysTwoPlayersOnOnePhone(t *testing.T) {
	safe := core.Edges{Left: 47, Right: 47, Bottom: 21}
	for _, tc := range []struct {
		name string
		os   core.OS
	}{
		{"ios", core.OSIOS},
		{"android", core.OSAndroid},
		{"phone web", core.OSWeb},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, g := startGame(t, nsgotest.Size(844, 390), nsgotest.Environment(core.Environment{OS: tc.os, Idiom: core.IdiomPhone, SafeArea: safe}))
			app.Press("id:vs-player")
			if g.scr != screenFight || g.mode != modeVersus {
				t.Fatal("the Versus player button did not start a versus fight")
			}
			app.Advance(2 * frame)
			if !g.controls.touch[0].Visible() || !g.controls.touch[1].Visible() {
				t.Fatalf("versus touch controls hidden on first frame: p1=%v p2=%v", g.controls.touch[0].Visible(), g.controls.touch[1].Visible())
			}

			const mid = core.Points(844) / 2
			p1IDs := []string{"p1-move", "p1-light", "p1-heavy"}
			p2IDs := []string{"p2-move", "p2-light", "p2-heavy"}
			var p1Frames, p2Frames []core.Bounds
			for _, id := range p1IDs {
				if !app.Exists("id:touch-" + id) {
					t.Fatalf("P1 control %q not drawn on the first frame", id)
				}
				f := controlFrame(t, g, 0, id)
				if f.Origin.X < safe.Left || f.Origin.X+f.Size.Width > mid || f.Origin.Y < safe.Top || f.Origin.Y+f.Size.Height > 390-safe.Bottom {
					t.Errorf("P1 control %q frame %+v outside left-half safe area", id, f)
				}
				if f.Size.Width < 44 || f.Size.Height < 44 {
					t.Errorf("P1 control %q size %+v smaller than 44pt", id, f.Size)
				}
				p1Frames = append(p1Frames, f)
			}
			for _, id := range p2IDs {
				if !app.Exists("id:touch-" + id) {
					t.Fatalf("P2 control %q not drawn on the first frame", id)
				}
				f := controlFrame(t, g, 1, id)
				if f.Origin.X < mid || f.Origin.X+f.Size.Width > 844-safe.Right || f.Origin.Y < safe.Top || f.Origin.Y+f.Size.Height > 390-safe.Bottom {
					t.Errorf("P2 control %q frame %+v outside right-half safe area", id, f)
				}
				if f.Size.Width < 44 || f.Size.Height < 44 {
					t.Errorf("P2 control %q size %+v smaller than 44pt", id, f.Size)
				}
				p2Frames = append(p2Frames, f)
			}
			for i, a := range p1Frames {
				for j, b := range p2Frames {
					if overlaps(a, b) {
						t.Errorf("P1 control %s %+v overlaps P2 control %s %+v", p1IDs[i], a, p2IDs[j], b)
					}
				}
			}

			untilFight(t, app, g)
			p1, p2 := g.m.players[0], g.m.players[1]

			// P1 walks forward with the left-half stick without moving P2.
			p1x, p2x := p1.f.X, p2.f.X
			p1Stick := centerOf(controlFrame(t, g, 0, "p1-move"))
			f := app.Touch("id:arena", p1Stick)
			f.Move(core.Point{X: p1Stick.X + 40, Y: p1Stick.Y})
			app.Advance(15 * frame)
			f.Lift()
			if p1.f.X <= p1x || p2.f.X != p2x {
				t.Fatalf("P1 stick moved fighters %.1f -> %.1f and %.1f -> %.1f", p1x, p1.f.X, p2x, p2.f.X)
			}

			// P2 walks forward (left toward P1) with the mirrored right-half stick without moving P1.
			p1x, p2x = p1.f.X, p2.f.X
			p2Stick := centerOf(controlFrame(t, g, 1, "p2-move"))
			f = app.Touch("id:arena", p2Stick)
			f.Move(core.Point{X: p2Stick.X - 40, Y: p2Stick.Y})
			app.Advance(15 * frame)
			f.Lift()
			if p2.f.X >= p2x || p1.f.X != p1x {
				t.Fatalf("P2 stick moved fighters %.1f -> %.1f and %.1f -> %.1f", p1x, p1.f.X, p2x, p2.f.X)
			}

			// P2's light button strikes only for P2.
			f = app.Touch("id:arena", centerOf(controlFrame(t, g, 1, "p2-light")))
			app.Advance(2 * frame)
			if p2.moveName() != "light" || p1.moveName() == "light" {
				t.Fatalf("P2 light started p1=%q p2=%q, want only p2 light", p1.moveName(), p2.moveName())
			}
			f.Lift()
			app.Advance(30 * frame)

			// Both players strike simultaneously with two distinct fingers on the same touch event.
			p1Heavy := centerOf(controlFrame(t, g, 0, "p1-heavy"))
			p2Heavy := centerOf(controlFrame(t, g, 1, "p2-heavy"))
			down := core.TouchEvent{
				Phase: core.TouchStart,
				Touches: []core.Touch{
					{ID: 1, Position: p1Heavy, Changed: true},
					{ID: 2, Position: p2Heavy, Changed: true},
				},
			}
			g.controls.players.OnTouch(down)
			app.Advance(2 * frame)
			if p1.moveName() != "heavy" || p2.moveName() != "heavy" {
				t.Fatalf("simultaneous heavy touches started p1=%q p2=%q, want both heavy", p1.moveName(), p2.moveName())
			}
			up := core.TouchEvent{
				Phase: core.TouchEnd,
				Touches: []core.Touch{
					{ID: 1, Position: p1Heavy, Changed: true},
					{ID: 2, Position: p2Heavy, Changed: true},
				},
			}
			g.controls.players.OnTouch(up)
		})
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

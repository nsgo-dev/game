package main

import (
	"strconv"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/dialogue"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// These tests play the game through the app as a player would, with the keyboard, a gamepad or the touch pad: each walks the maps with the input map's bindings, talks to people and fights, and reaches the victory screen or the defeat screen.

// controls is one input device.
type controls interface {
	// hold holds a direction down, releasing the one held before; dirNone releases it.
	hold(d dir)
	// down and up press and release confirm, cancel or menu.
	down(action string)
	up(action string)
}

// keyboard plays with the keys game.yaml binds: the arrows, Enter, Escape and Tab.
type keyboard struct {
	app  *nsgotest.App
	held string
}

func (k *keyboard) hold(d dir) {
	key := map[dir]string{dirUp: "up", dirDown: "down", dirLeft: "left", dirRight: "right"}[d]
	if key == k.held {
		return
	}
	if k.held != "" {
		k.app.KeyUp(k.held)
	}
	if key != "" {
		k.app.KeyDown(key)
	}
	k.held = key
}

var actionKeys = map[string]string{actionConfirm: "enter", actionCancel: "escape", actionMenu: "tab"}

func (k *keyboard) down(action string) { k.app.KeyDown(actionKeys[action]) }
func (k *keyboard) up(action string)   { k.app.KeyUp(actionKeys[action]) }

// pad plays with game controller 1: the left stick to walk, South to confirm, East to cancel and Start for the menu.
type pad struct{ app *nsgotest.App }

func (p *pad) hold(d dir) {
	dx, dy := d.delta()
	p.app.SetGamepadAxis(1, gamepad.AxisLeftX, float32(dx))
	p.app.SetGamepadAxis(1, gamepad.AxisLeftY, float32(dy))
}

var actionButtons = map[string]gamepad.Button{actionConfirm: gamepad.ButtonSouth, actionCancel: gamepad.ButtonEast, actionMenu: gamepad.ButtonStart}

func (p *pad) down(action string) { p.app.SetGamepadButton(1, actionButtons[action], 1) }
func (p *pad) up(action string)   { p.app.SetGamepadButton(1, actionButtons[action], 0) }

// touch plays with fingers on the on-screen pad, at the regions game.yaml gives its actions.
type touch struct {
	app    *nsgotest.App
	a      *app
	held   dir
	finger *nsgotest.Finger
	press  *nsgotest.Finger
}

// at returns the middle of action's touch region in the stage's coordinates.
func (tc *touch) at(action string) core.Point {
	r, ok := Project.Region(action)
	if !ok {
		panic("no touch region for " + action)
	}
	mid := core.Point{X: r.Origin.X + r.Size.Width/2, Y: r.Origin.Y + r.Size.Height/2}
	return tc.a.viewport.GameToView(tc.a.size, mid)
}

func (tc *touch) hold(d dir) {
	if d == tc.held {
		return
	}
	if tc.finger != nil {
		tc.finger.Lift()
		tc.finger = nil
	}
	if name := map[dir]string{dirUp: actionUp, dirDown: actionDown, dirLeft: actionLeft, dirRight: actionRight}[d]; name != "" {
		tc.finger = tc.app.Touch("id:stage", tc.at(name))
	}
	tc.held = d
}

func (tc *touch) down(action string) { tc.press = tc.app.Touch("id:stage", tc.at(action)) }
func (tc *touch) up(string)          { tc.press.Lift() }

// player drives the app through one input device and reads the game to decide what to do next.
type player struct {
	t   *testing.T
	app *nsgotest.App
	a   *app
	g   *Game
	ctl controls
}

// startApp starts the game as Root does and returns the player for the device ctl makes.
func startApp(t *testing.T, ctl func(*nsgotest.App, *app) controls, opts ...nsgotest.Option) *player {
	t.Helper()
	var a *app
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var err error
		if a, err = start(cx); err != nil {
			t.Fatal(err)
		}
		return a.node()
	}, opts...)
	p := &player{t: t, app: app, a: a, g: a.g}
	p.ctl = ctl(app, a)
	p.step()
	return p
}

// step runs the app until the game has taken one more fixed step.
func (p *player) step() {
	p.t.Helper()
	for n := p.a.steps; p.a.steps == n; {
		p.app.Advance(time.Second / 60)
	}
}

// steps runs n fixed steps.
func (p *player) steps(n int) {
	for range n {
		p.step()
	}
}

// press presses and releases action, a step each.
func (p *player) press(action string) {
	p.t.Helper()
	p.ctl.down(action)
	p.step()
	p.ctl.up(action)
	p.step()
}

// tapMove holds d for one step and lets go, to move a menu's selection or turn to face a person.
func (p *player) tapMove(d dir) {
	p.ctl.hold(d)
	p.step()
	p.ctl.hold(dirNone)
	p.step()
}

// newGame picks New game on the title screen.
func (p *player) newGame() {
	p.t.Helper()
	p.app.Find("text:" + Project.Title())
	p.choose("new")
	if p.g.mode != modeField {
		p.t.Fatalf("mode %v after New game", p.g.mode)
	}
}

// choose selects the menu entry id with up and down and picks it with confirm.
func (p *player) choose(id string) {
	p.t.Helper()
	p.step()
	for range 20 {
		v, ok := p.g.menuView()
		if !ok {
			p.t.Fatalf("no menu shows an entry %q", id)
		}
		if v.items[p.g.sel].id == id {
			p.press(actionConfirm)
			return
		}
		p.tapMove(dirDown)
	}
	p.t.Fatalf("could not select menu entry %q", id)
}

// walkTo walks to c on the current map along the shortest path, fighting any slime that gets in the way, and stops when c is an exit that takes the player to another map.
func (p *player) walkTo(c Cell) {
	p.t.Helper()
	start := p.g.mapID
	for range 4000 {
		if p.g.mapID != start {
			p.ctl.hold(dirNone)
			p.step()
			return
		}
		switch p.g.mode {
		case modeBattle:
			p.ctl.hold(dirNone)
			p.winBattle()
			continue
		case modeField:
		default:
			p.t.Fatalf("mode %v while walking to %v", p.g.mode, c)
		}
		// Steer from where the current step ends, since the game turns there in the same fixed step.
		if p.g.pos == c {
			p.ctl.hold(dirNone)
			if p.g.walk == 0 {
				p.step()
				return
			}
		} else {
			p.ctl.hold(p.nextDir(c))
		}
		p.step()
	}
	p.t.Fatalf("never reached %v on %s; at %v", c, p.g.mapID, p.g.pos)
}

// nextDir returns the first step of the shortest walkable path from the player's tile to goal, which may be a foe's tile.
func (p *player) nextDir(goal Cell) dir {
	p.t.Helper()
	g := p.g
	from := map[Cell]dir{g.pos: dirNone}
	queue := []Cell{g.pos}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == goal {
			for {
				d := from[c]
				prev := c.step(opposite(d))
				if prev == g.pos {
					return d
				}
				c = prev
			}
		}
		for _, d := range []dir{dirUp, dirDown, dirLeft, dirRight} {
			n := c.step(d)
			if _, seen := from[n]; seen || n != goal && (g.blocked(n) || g.foeAt(n) != nil || g.exitAt(n)) {
				continue
			}
			from[n] = d
			queue = append(queue, n)
		}
	}
	p.t.Fatalf("no path from %v to %v on %s", g.pos, goal, g.mapID)
	return dirNone
}

// exitAt reports whether an exit of the current map is at c.
func (g *Game) exitAt(c Cell) bool {
	for _, e := range g.level.Exits {
		if e.At == c {
			return true
		}
	}
	return false
}

func opposite(d dir) dir {
	return map[dir]dir{dirUp: dirDown, dirDown: dirUp, dirLeft: dirRight, dirRight: dirLeft}[d]
}

// travel takes the exit at c to the map called to.
func (p *player) travel(c Cell, to string) {
	p.t.Helper()
	p.walkTo(c)
	if p.g.mapID != to {
		p.t.Fatalf("on %s after the exit at %v, want %s", p.g.mapID, c, to)
	}
}

// fight walks into the foe called id and fights it until the battle ends.
func (p *player) fight(id string) {
	p.t.Helper()
	for range 4000 {
		if p.g.mode == modeBattle && p.g.battle.foe.ID == id {
			p.ctl.hold(dirNone)
			p.attackUntilOver()
			return
		}
		if p.g.mode == modeBattle {
			p.ctl.hold(dirNone)
			p.winBattle()
			continue
		}
		var target *foeState
		for _, f := range p.g.foes {
			if f.ID == id {
				target = f
			}
		}
		if target == nil {
			p.t.Fatalf("no foe %s on %s", id, p.g.mapID)
		}
		p.ctl.hold(p.nextDir(target.at))
		p.step()
	}
	p.t.Fatalf("never reached %s", id)
}

// winBattle attacks a foe that got in the way and fails the test if it is not beaten.
func (p *player) winBattle() {
	p.t.Helper()
	p.attackUntilOver()
	if p.g.mode != modeField {
		p.t.Fatalf("lost to a foe in the way: mode %v", p.g.mode)
	}
}

// attackUntilOver picks Attack until the battle ends.
func (p *player) attackUntilOver() {
	p.t.Helper()
	for range 50 {
		if p.g.mode != modeBattle {
			return
		}
		if tc, ok := p.ctl.(*touch); ok {
			// On a touch screen the battle's moves are buttons.
			tc.app.Press("id:menu-attack")
			p.step()
			continue
		}
		p.choose("attack")
	}
	p.t.Fatal("the battle did not end")
}

// talkTo faces the person standing at c from the tile below it and talks, picking the options given, in order, as they come, and the last option of any others.
func (p *player) talkTo(c Cell, picks ...string) {
	p.t.Helper()
	p.walkTo(Cell{c.X(), c.Y() + 1})
	p.tapMove(dirUp)
	p.press(actionConfirm)
	if p.g.mode != modeTalk {
		p.t.Fatalf("mode %v after confirm facing %v", p.g.mode, c)
	}
	for range 200 {
		if p.g.mode != modeTalk {
			if len(picks) > 0 {
				p.t.Fatalf("the conversation ended before offering %q", picks)
			}
			return
		}
		choices := p.g.talk.Choices()
		if len(choices) == 0 {
			p.press(actionConfirm)
			continue
		}
		// With nothing left to pick, the last option, goodbye, ends the conversation.
		want := len(choices) - 1
		if len(picks) > 0 {
			want = -1
			for i, c := range choices {
				if c.Text == picks[0] {
					want = i
				}
			}
			if want < 0 {
				p.t.Fatalf("no option %q in %+v", picks[0], choices)
			}
			picks = picks[1:]
		}
		for p.g.choice != want {
			p.tapMove(dirDown)
		}
		p.press(actionConfirm)
	}
	p.t.Fatal("the conversation did not end")
}

// openPack opens the pack with the menu button.
func (p *player) openPack() {
	p.t.Helper()
	p.press(actionMenu)
	if p.g.mode != modeMenu {
		p.t.Fatalf("mode %v after the menu button", p.g.mode)
	}
}

// TestPlayToVictoryWithKeyboard plays the whole game with the keyboard: it takes Mira's quest, gathers the glowcaps, beats a slime for the coins to buy the iron sword, equips it with the vest from the meadow, beats Thornhide and brings the glowcaps back.
func TestPlayToVictoryWithKeyboard(t *testing.T) {
	p := startApp(t, func(app *nsgotest.App, _ *app) controls { return &keyboard{app: app} }, onDesktop)
	p.newGame()
	p.app.Find("id:hud-place")

	p.talkTo(Cell{5, 5}, "I'll bring them.")
	p.app.Find("text:Gather glowcaps 0/3")
	p.talkTo(Cell{31, 5})
	if p.g.bag.Count(itemPotion) != 1 {
		t.Fatalf("Pell gave %d potions", p.g.bag.Count(itemPotion))
	}

	p.travel(Cell{33, 6}, "field")
	if got := p.app.Find("id:hud-place").Text; got != "Whisper Meadow" {
		t.Fatalf("the HUD names %q", got)
	}
	p.walkTo(Cell{20, 3})
	p.fight("slime-road")
	if p.g.bag.Count(itemCoin) < 20 {
		t.Fatalf("%d coins after a slime", p.g.bag.Count(itemCoin))
	}
	p.walkTo(Cell{34, 9})
	p.walkTo(Cell{8, 20})
	p.app.Find("text:Drive off Thornhide")
	p.walkTo(Cell{41, 2})
	p.travel(Cell{0, 6}, "town")

	p.talkTo(Cell{18, 5}, "Buy it for 20 coins.")
	p.openPack()
	p.choose("equip-iron-sword")
	p.choose("equip-leather-vest")
	p.app.Find("text:HP " + strconv.Itoa(p.g.hp) + "/30 · Attack 9 · Defense 3 · Coins " + strconv.Itoa(p.g.bag.Count(itemCoin)))
	p.press(actionCancel)
	p.talkTo(Cell{5, 5}, "Could you tend my wounds?")
	if p.g.hp != 30 {
		t.Fatalf("HP %d after Mira's care", p.g.hp)
	}

	p.travel(Cell{33, 6}, "field")
	p.fight("thornhide")
	if p.g.mode != modeField || !p.g.defeated["thornhide"] {
		t.Fatalf("mode %v after fighting Thornhide", p.g.mode)
	}
	p.travel(Cell{0, 6}, "town")
	p.talkTo(Cell{5, 5})
	if p.g.mode != modeWon {
		t.Fatalf("mode %v after bringing the glowcaps back", p.g.mode)
	}
	p.app.Find("text:The shrine burns again")
	if p.g.bag.Count("shrine-charm") != 1 || p.g.bag.Count("glowcap") != 0 {
		t.Fatalf("rewards: %+v", p.g.bag.Slots())
	}

	p.choose("continue")
	if p.g.mode != modeField {
		t.Fatalf("mode %v after Keep exploring", p.g.mode)
	}
}

// TestLoseToThornhideWithGamepad walks straight to Thornhide with the starting sword, fighting the slimes in the way, and loses.
func TestLoseToThornhideWithGamepad(t *testing.T) {
	p := startApp(t, func(app *nsgotest.App, _ *app) controls {
		app.ConnectGamepad(1, "Standard Pad")
		return &pad{app: app}
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop, Gamepads: true}))
	p.newGame()
	p.travel(Cell{33, 6}, "field")
	p.fight("thornhide")
	if p.g.mode != modeLost {
		t.Fatalf("mode %v after fighting Thornhide with the wooden sword", p.g.mode)
	}
	p.app.Find("text:You fall in Whisper Meadow")
	p.choose("new")
	if p.g.mode != modeField || p.g.mapID != "town" || p.g.hp != 30 {
		t.Fatalf("after New game: mode %v on %s with %d HP", p.g.mode, p.g.mapID, p.g.hp)
	}
}

// TestPlayWithTouch walks with the touch pad, talks with the A button and taps, saves and loads through the pack's buttons and loses to Thornhide pressing Attack.
func TestPlayWithTouch(t *testing.T) {
	p := startApp(t, func(app *nsgotest.App, a *app) controls { return &touch{app: app, a: a} },
		nsgotest.Environment(core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone}))
	p.app.Press("id:menu-new")
	p.step()
	if p.g.mode != modeField {
		t.Fatalf("mode %v after pressing New game", p.g.mode)
	}

	// Talk to Pell: the A pad starts the conversation and taps anywhere move it on.
	p.walkTo(Cell{31, 6})
	p.tapMove(dirUp)
	p.press(actionConfirm)
	for range 50 {
		if p.g.mode != modeTalk {
			break
		}
		f := p.app.Touch("id:stage", p.a.viewport.GameToView(p.a.size, core.Point{X: 240, Y: 60}))
		p.step()
		f.Lift()
		p.step()
	}
	if p.g.mode != modeField || p.g.bag.Count(itemPotion) != 1 {
		t.Fatalf("mode %v, %d potions after talking to Pell", p.g.mode, p.g.bag.Count(itemPotion))
	}

	p.app.Press("id:hud-pack")
	p.step()
	p.app.Press("id:menu-save")
	p.step()
	p.app.Press("id:menu-slot1")
	p.step()
	p.app.Find("text:Saved to slot 1.")
	saved := p.g.pos

	p.travel(Cell{33, 6}, "field")
	p.fight("thornhide")
	if p.g.mode != modeLost {
		t.Fatalf("mode %v after fighting Thornhide", p.g.mode)
	}
	p.app.Press("id:menu-load")
	p.step()
	p.app.Press("id:menu-slot1")
	p.step()
	if p.g.mode != modeField || p.g.mapID != "town" || p.g.pos != saved || p.g.hp != 30 {
		t.Fatalf("after loading slot 1: mode %v on %s at %v with %d HP", p.g.mode, p.g.mapID, p.g.pos, p.g.hp)
	}
}

func TestTitleShowsTheGameAndPadsOnlyOnTouchScreens(t *testing.T) {
	desk := startApp(t, func(app *nsgotest.App, _ *app) controls { return &keyboard{app: app} }, onDesktop)
	desk.app.Find("text:" + Project.Title())
	if len(desk.app.Find("id:game-canvas").Ops) == 0 {
		t.Fatal("the title screen draws nothing")
	}
	if desk.a.touch {
		t.Fatal("a desktop shows the touch pad")
	}
	phone := startApp(t, func(app *nsgotest.App, a *app) controls { return &touch{app: app, a: a} },
		nsgotest.Environment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}))
	if !phone.a.touch {
		t.Fatal("a phone hides the touch pad")
	}
}

func TestRequestsOrientation(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(960, 540))
	if app.Orientation != core.OrientationLandscape {
		t.Fatalf("orientation = %v, want landscape (%v)", app.Orientation, core.OrientationLandscape)
	}
}

// onDesktop runs a test on a desktop, where nsgotest would otherwise use a phone.
var onDesktop = nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop})

// dialogueEvent is a dialogue script command, as the script runs it.
func dialogueEvent(command string, args ...string) dialogue.Event {
	return dialogue.Event{Command: command, Args: args}
}

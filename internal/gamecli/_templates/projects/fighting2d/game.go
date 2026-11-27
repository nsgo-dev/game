package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/placeholder"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// screenKind is what the game shows.
type screenKind int

const (
	screenTitle screenKind = iota
	screenFight
	screenPaused
	screenReconnect
	screenResult
)

// mode is who the second fighter is.
type mode int

const (
	modeCPU mode = iota
	modeVersus
)

// sfx are the placeholder pack's sounds the game plays, by what it plays them for.
var sfx = map[eventKind]string{
	evRound: "select",
	evFight: "powerup",
	evHit:   "hit",
	evBlock: "click",
	evCombo: "coin",
	evKO:    "explosion",
}

// hudState is what the native HUD shows, kept in one signal that changes only when something on it does.
type hudState struct {
	health  [2]float32
	wins    [2]int
	seconds int
	banner  string
	// hits are each side's combo counter and combo its named combo, while they show.
	hits  [2]int
	combo [2]string
}

// fightingGame is the game: the roster, the input devices, the match being played and the CPU opponent, driven by a fixed-step game.Loop and drawn into a Canvas under a native HUD.
type fightingGame struct {
	cx       *core.Scope
	roster   *roster
	controls *controls
	viewport screen.Viewport
	viewSize core.Size
	loop     *game.Loop
	mixer    *sound.Mixer
	sounds   map[eventKind]*sound.Effect

	m    *match
	cpu  *cpu
	mode mode
	// scr mirrors screen for the loop, which reads it every step.
	scr      screenKind
	hitboxes bool
	// banner is the HUD's banner and bannerLeft how long it stays; comboName and comboLeft are each side's named combo caption.
	banner     string
	bannerLeft int
	comboName  [2]string
	comboLeft  [2]int

	screen *core.Signal[screenKind]
	// disconnected tracks which players' controllers are awaiting reconnect in versus.
	disconnected *core.Signal[[2]bool]
	// picks are the roster indexes of the two fighters.
	picks  *core.Signal[[2]int]
	hud    *core.Signal[hudState]
	result *core.Signal[string]
	frame  *core.Signal[uint64]
}

// Root is the game's root NSGo component.
func Root(cx *core.Scope) core.Node { return rootWithHook(cx, nil) }

// rootWithHook is Root, handing the game to hook first, for tests.
func rootWithHook(cx *core.Scope, hook func(*fightingGame)) core.Node {
	desktop.SetTitle(cx, Project.Title())
	g := newFightingGame(cx)
	if hook != nil {
		hook(g)
	}
	return g.render()
}

func newFightingGame(cx *core.Scope) *fightingGame {
	r, err := loadRoster(dataFS)
	if err != nil {
		// The data is embedded and its tests load it, so this is a programming error.
		panic(fmt.Errorf("fighting2d: %w", err))
	}
	g := &fightingGame{
		cx:           cx,
		roster:       r,
		picks:        core.NewSignal(cx, [2]int{0, 1 % len(r.fighters)}),
		screen:       core.NewSignal(cx, screenTitle),
		disconnected: core.NewSignal(cx, [2]bool{}),
		hud:          core.NewSignal(cx, hudState{}),
		result:       core.NewSignal(cx, ""),
		frame:        core.NewSignal(cx, uint64(0)),
		mixer:        sound.NewMixer(cx, "fighting2d.sound"),
		sounds:       map[eventKind]*sound.Effect{},
	}
	g.viewport, _ = Project.Viewport()
	g.controls = newControls(cx, func(p core.Point) (core.Point, bool) { return g.viewport.ViewToGame(g.viewSize, p) }, g.disconnectPolicy(false))
	Project.ConfigureMixer(g.mixer)
	for kind, name := range sfx {
		src, err := placeholder.Sound(name)
		if err != nil {
			panic(err)
		}
		audio.LoadSound(cx, src, func(s *audio.Sound, err error) {
			// A target without audio plays the game silently.
			if err == nil {
				g.sounds[kind] = g.mixer.Effect(s, "sfx", 4)
			}
		})
	}
	g.loop = &game.Loop{
		Rate:   stepRate,
		Update: func(time.Duration) { g.update() },
		Draw:   func(float64) { g.frame.Update(func(n uint64) uint64 { return n + 1 }) },
		// The loop pauses in the background; the sound stays paused on return while a pause or reconnect menu is open.
		OnPause: func(paused bool) {
			g.mixer.SetPaused(paused || g.scr == screenPaused || g.scr == screenReconnect)
		},
	}
	g.loop.Run(cx)
	return g
}

// disconnectPolicy returns the controller disconnect rules: in versus, losing an assigned controller pauses the fight until it returns, a free controller presses a button, or the player continues on the keyboard.
func (g *fightingGame) disconnectPolicy(versus bool) input.DisconnectPolicy {
	return input.DisconnectPolicy{
		PauseOnDisconnect:    versus,
		AllowReassignOnPress: true,
		OnPause:              g.onDisconnectPause,
		OnDisconnect:         func(input.DisconnectEvent) { g.syncDisconnected() },
		OnReconnect:          func(input.ReconnectEvent) { g.syncDisconnected() },
	}
}

func (g *fightingGame) onDisconnectPause(paused bool) {
	g.syncDisconnected()
	if paused && g.scr == screenFight {
		g.setScreen(screenReconnect)
		g.mixer.SetPaused(true)
	} else if !paused && g.scr == screenReconnect {
		g.setScreen(screenFight)
		g.mixer.SetPaused(false)
	}
}

func (g *fightingGame) syncDisconnected() {
	d := g.controls.disconnected()
	if d != g.disconnected.GetUntracked() {
		g.disconnected.Set(d)
	}
}

// continueOnKeyboard unassigns every disconnected controller so those players keep fighting on the keyboard.
func (g *fightingGame) continueOnKeyboard() {
	g.controls.unassignDisconnected()
	g.syncDisconnected()
}

// continuePlayerOnKeyboard unassigns player i's disconnected controller so player i keeps fighting on the keyboard.
func (g *fightingGame) continuePlayerOnKeyboard(i int) {
	g.controls.unassignGamepad(i)
	g.syncDisconnected()
}

// setScreen switches what the game shows and releases the input held into it.
func (g *fightingGame) setScreen(s screenKind) {
	g.scr = s
	g.screen.Set(s)
	g.controls.reset()
}

// startMatch starts a match in mode with the picked fighters.
func (g *fightingGame) startMatch(md mode) {
	g.controls.setDisconnectPolicy(g.disconnectPolicy(false))
	g.controls.assignFreePads(true)
	g.mode = md
	picks := g.picks.GetUntracked()
	g.m = newMatch(g.roster, g.roster.fighters[picks[0]], g.roster.fighters[picks[1]])
	g.cpu = nil
	if md == modeCPU {
		g.cpu = newCPU(&g.roster.file.CPU)
	}
	g.bannerLeft, g.comboLeft = 0, [2]int{}
	g.result.Set("")
	g.setScreen(screenFight)
	g.controls.setDisconnectPolicy(g.disconnectPolicy(md == modeVersus))
	g.syncDisconnected()
	g.handle(g.m.events)
	g.syncHUD()
}

// cycle moves player i's pick to the next fighter of the roster.
func (g *fightingGame) cycle(i, by int) {
	n := len(g.roster.fighters)
	picks := g.picks.GetUntracked()
	picks[i] = (picks[i] + by + n) % n
	g.picks.Set(picks)
}

// pause pauses or resumes the fight.
func (g *fightingGame) pause(paused bool) {
	if paused && g.scr == screenFight {
		g.setScreen(screenPaused)
		g.mixer.SetPaused(true)
	} else if !paused && g.scr == screenPaused {
		if g.controls.pausedByDisconnect() {
			g.syncDisconnected()
			g.setScreen(screenReconnect)
			return
		}
		g.setScreen(screenFight)
		g.mixer.SetPaused(false)
	}
}

// quit leaves the match for the title screen.
func (g *fightingGame) quit() {
	g.mixer.SetPaused(false)
	g.m, g.cpu = nil, nil
	g.setScreen(screenTitle)
	g.controls.setDisconnectPolicy(g.disconnectPolicy(false))
	g.controls.assignFreePads(true)
	g.syncDisconnected()
	g.syncHUD()
}

// update is one fixed step.
func (g *fightingGame) update() {
	c := g.controls
	was := g.scr
	c.step(was == screenTitle)
	if c.justPressed("hitboxes") {
		g.hitboxes = !g.hitboxes
	}
	if was == screenReconnect && g.scr == screenFight {
		return
	}
	switch g.scr {
	case screenTitle:
		g.titleStep()
	case screenFight:
		if c.justPressed("pause") || c.playerJustPressed(0, "start") || c.playerJustPressed(1, "start") {
			g.pause(true)
			return
		}
		g.fightStep()
	case screenPaused:
		if c.justPressed("pause") || c.playerJustPressed(0, "start") || c.playerJustPressed(1, "start") {
			g.pause(false)
		}
	case screenReconnect:
		for i := range 2 {
			if c.playerDisconnected(i) && c.playerJustPressed(i, "start") {
				g.continuePlayerOnKeyboard(i)
			}
		}
	case screenResult:
		switch {
		case c.playerJustPressed(0, "start"):
			g.startMatch(g.mode)
		case c.justPressed("pause"):
			g.quit()
		}
	}
}

// titleStep lets each player pick a fighter with left and right and start: the first player's start fights the CPU, the second's starts a versus match.
func (g *fightingGame) titleStep() {
	c := g.controls
	for i := range 2 {
		switch {
		case c.playerJustPressed(i, "left"):
			g.cycle(i, -1)
		case c.playerJustPressed(i, "right"):
			g.cycle(i, 1)
		}
	}
	switch {
	case c.playerJustPressed(0, "start"):
		g.startMatch(modeCPU)
	case c.playerJustPressed(1, "start"):
		g.startMatch(modeVersus)
	}
}

// fightStep feeds the players' input to the match and reacts to what happened.
func (g *fightingGame) fightStep() {
	m := g.m
	split := g.mode == modeVersus
	var in [2]control
	in[0] = read(m.players[0], g.controls.source(0, split))
	if g.cpu != nil {
		in[1] = g.cpu.control(m, m.players[1])
	} else {
		in[1] = read(m.players[1], g.controls.source(1, split))
	}
	g.handle(m.step(in))
	if g.bannerLeft > 0 {
		g.bannerLeft--
	}
	for i := range g.comboLeft {
		if g.comboLeft[i] > 0 {
			g.comboLeft[i]--
		}
	}
	g.syncHUD()
}

// handle plays the sounds of a step's events and puts their banners up.
func (g *fightingGame) handle(events []event) {
	for _, e := range events {
		if s := g.sounds[e.kind]; s != nil {
			s.Play(audio.Voice{Volume: 0.8})
		}
		switch e.kind {
		case evRound:
			g.showBanner(fmt.Sprintf("Round %d", g.m.round), g.roster.file.Rules.IntroSteps)
		case evFight:
			g.showBanner("Fight!", 40)
		case evCombo:
			g.comboName[e.player] = comboTitle(e.name)
			g.comboLeft[e.player] = 90
		case evKO:
			g.showBanner("K.O.", g.roster.file.Rules.OutroSteps)
		case evTimeUp:
			g.showBanner("Time", g.roster.file.Rules.OutroSteps)
		case evMatchOver:
			g.result.Set(g.resultText(e.player))
			g.setScreen(screenResult)
			g.controls.setDisconnectPolicy(g.disconnectPolicy(false))
			g.syncDisconnected()
		}
	}
}

// showBanner shows text across the middle of the screen for steps.
func (g *fightingGame) showBanner(text string, steps int) {
	g.banner, g.bannerLeft = text, steps
}

// comboTitle turns a move set combo's name, such as "squall", into the caption the HUD shows.
func comboTitle(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + strings.ReplaceAll(name[1:], "_", " ")
}

// resultText says who won the match.
func (g *fightingGame) resultText(winner int) string {
	switch {
	case winner < 0:
		return "Draw"
	case g.mode == modeCPU && winner == 0:
		return "You win"
	case g.mode == modeCPU:
		return "You lose"
	}
	return fmt.Sprintf("Player %d wins", winner+1)
}

// syncHUD sets the HUD's signal when what it shows changed.
func (g *fightingGame) syncHUD() {
	var h hudState
	if m := g.m; m != nil {
		for i, p := range m.players {
			h.health[i] = float32(p.health) / float32(p.fighter.def.Health)
			h.wins[i] = p.wins
			if p.combo >= 2 {
				h.hits[i] = p.combo
			}
			if g.comboLeft[i] > 0 {
				h.combo[i] = g.comboName[i]
			}
		}
		h.seconds = m.seconds()
		if g.bannerLeft > 0 {
			h.banner = g.banner
		}
	}
	if h != g.hud.GetUntracked() {
		g.hud.Set(h)
	}
}

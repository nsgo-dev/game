package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/idle"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/ui"
)

// Action names, which game.yaml's input map binds to keys and controller buttons.
const (
	actionStoke    = "stoke"
	actionUp       = "up"
	actionDown     = "down"
	actionLeft     = "left"
	actionRight    = "right"
	actionConfirm  = "confirm"
	actionBack     = "back"
	actionNotation = "notation"
)

// autosaveEvery is how much play passes between saves; the game also saves when it goes to the background or quits, and after a prestige, a win or a lost run.
const autosaveEvery = 10 * time.Second

// overlay is the card shown over the game, if any.
type overlay uint8

const (
	// overlayNone shows the game.
	overlayNone overlay = iota
	// overlayAway reports what the hearth made while the player was away.
	overlayAway
	// overlayCold is the loss: the fire went out and the run must be rekindled.
	overlayCold
	// overlayWon is the win: the goal is bought, and play goes on after it.
	overlayWon
)

// hearthGame ties the hearth to NSGo: its loop, input, save slot, offline credit and views.
type hearthGame struct {
	cx       *core.Scope
	h        *hearth
	store    *save.Store
	actions  *input.Actions
	viewport screen.Viewport
	shop     *shop
	format   idle.Format

	// seen is the wall-clock time the player was last seen, which the next absence counts from.
	seen time.Time
	// background says the app is in the background, where the loop runs no steps.
	background bool
	// sinceSave is the play since the last save.
	sinceSave time.Duration
	// steps counts fixed steps, which animate the flame.
	steps int64
	// canvasSize is the canvas's size at its last draw, which maps presses into the virtual resolution.
	canvasSize core.Size

	// away is the last absence, which overlayAway and overlayCold describe.
	away awayReport
	// tick changes every drawn frame, so the HUD and the canvas redraw with the game.
	tick *core.Signal[uint64]
	// shown is the overlay on screen.
	shown *core.Signal[overlay]
	// problem is a save error to show, empty when saving works.
	problem *core.Signal[string]
}

// Root is the game's root NSGo component: a native HUD over a canvas that draws the hearth and its shop, driven by a fixed-step game.Loop.
func Root(cx *core.Scope) core.Node { return rootWith(cx, nil) }

// rootWith is Root, handing the game to onReady when it is not nil, as the tests do to look inside it.
func rootWith(cx *core.Scope, onReady func(*hearthGame)) core.Node {
	desktop.SetTitle(cx, Project.Title())
	// The 360x600 hearth and shop stack vertically: a phone stays upright while the game runs.
	cx.RequestOrientation(core.OrientationPortrait)
	g := newHearthGame(cx, mustParseData(hearthYAML))
	if onReady != nil {
		onReady(g)
	}
	return g.render()
}

// newHearthGame loads the save from cx's storage, credits the time since it was written and starts the loop.
func newHearthGame(cx *core.Scope, data *hearthData) *hearthGame {
	viewport, _ := Project.Viewport()
	g := &hearthGame{
		cx:       cx,
		store:    newSaveStore(cx.Storage()),
		actions:  input.NewActions(),
		viewport: viewport,
		tick:     core.NewSignal(cx, uint64(0)),
		shown:    core.NewSignal(cx, overlayNone),
		problem:  core.NewSignal(cx, ""),
	}
	g.load(data)
	g.shop = newShop(g)

	Project.BindInput(g.actions)
	g.actions.WatchKeys(cx)
	g.actions.WatchGamepads(cx)

	loop := &game.Loop{Rate: 60, Update: g.update, Draw: func(float64) { g.tick.Update(func(n uint64) uint64 { return n + 1 }) }}
	loop.Run(cx)

	// Time in the background earns offline progress when the app comes back; the loop itself pauses there.
	cx.Effect(func() {
		background := cx.Env().Lifecycle() == core.LifecycleBackground
		cx.Untrack(func() { g.setBackground(background) })
	})
	cx.OnQuit(func(context.Context) { g.save() })
	return g
}

// load restores the save, or starts a new game when there is none, and credits the time since the save was written. A save that cannot be read leaves saving off, so it is not overwritten.
func (g *hearthGame) load(data *hearthData) {
	now := g.cx.WallTime()
	var s savedGame
	err := g.store.LoadChecked(saveSlot, &s, func() error { return s.check(data) })
	switch {
	case err == nil:
		var n idle.Notation
		var last time.Time
		g.h, n, last = s.restore(data)
		g.format.Notation = n
		g.seen = last
		g.returnFrom(last, now)
		return
	case errors.Is(err, save.ErrNotFound):
	default:
		g.store = nil
		g.problem.Set(fmt.Sprintf("The save could not be read, so this game is not saved: %v", err))
	}
	g.h = newHearth(data)
	g.seen = now
}

// returnFrom credits the absence from last to now and shows what it made.
func (g *hearthGame) returnFrom(last, now time.Time) {
	r := g.h.creditAway(last, now)
	if !r.Away.Seen.IsZero() {
		g.seen = r.Away.Seen
	}
	g.away = r
	switch {
	case r.WentCold || g.h.cold:
		g.shown.Set(overlayCold)
	case !r.Earned.IsZero():
		g.shown.Set(overlayAway)
	}
	g.save()
}

// setBackground saves on the way to the background and credits the time away on the way back.
func (g *hearthGame) setBackground(background bool) {
	if background == g.background {
		return
	}
	g.background = background
	if background {
		g.save()
		return
	}
	g.returnFrom(g.seen, g.cx.WallTime())
}

// save writes the game to its slot with now as the last-seen time, unless saving is off.
func (g *hearthGame) save() {
	now := g.cx.WallTime()
	if now.After(g.seen) {
		g.seen = now
	}
	g.sinceSave = 0
	if g.store == nil {
		return
	}
	if err := g.store.Save(saveSlot, snapshot(g.h, g.format.Notation, g.seen)); err != nil {
		g.problem.Set(fmt.Sprintf("The game could not be saved: %v", err))
		return
	}
	g.problem.Set("")
}

// update runs one fixed step: input, then the hearth, then the autosave. The hearth waits while a card shows, since the player cannot stoke under it.
func (g *hearthGame) update(dt time.Duration) {
	g.steps++
	g.actions.Step()
	g.handleActions()
	if g.shown.Get() == overlayNone && g.h.step(dt, 1) {
		g.away = awayReport{}
		g.shown.Set(overlayCold)
		g.save()
	}
	g.sinceSave += dt
	if g.sinceSave >= autosaveEvery {
		g.save()
	}
}

// handleActions acts on the step's keyboard and controller input: the overlay's button while one shows, the shop's focus and stoking otherwise. Stoking does not close a card, so a player still stoking when the fire goes out sees the loss before rekindling.
func (g *hearthGame) handleActions() {
	a := g.actions
	if a.JustPressed(actionNotation) {
		g.cycleNotation()
	}
	if g.shown.Get() != overlayNone {
		if a.JustPressed(actionConfirm) || a.JustPressed(actionBack) {
			g.dismiss()
		}
		return
	}
	if a.JustPressed(actionStoke) {
		g.stoke()
	}
	g.shop.stepActions(a)
}

// stoke feeds the fire.
func (g *hearthGame) stoke() { g.h.stoke() }

// buyGenerator buys generator i.
func (g *hearthGame) buyGenerator(i int) { g.h.buyGenerator(i) }

// buyUpgrade buys upgrade j.
func (g *hearthGame) buyUpgrade(j int) { g.h.buyUpgrade(j) }

// prestige scatters the ashes and saves the new run.
func (g *hearthGame) prestige() {
	if g.h.prestige() {
		g.shop.focusFlame()
		g.save()
	}
}

// buyGoal buys the goal, which wins the game.
func (g *hearthGame) buyGoal() {
	if g.h.buyGoal() {
		g.shown.Set(overlayWon)
		g.save()
	}
}

// dismiss acts on the overlay's button: rekindling after a loss, closing it otherwise.
func (g *hearthGame) dismiss() {
	if g.shown.Get() == overlayCold {
		g.h.rekindle()
		g.shop.focusFlame()
		g.save()
	}
	g.shown.Set(overlayNone)
}

// cycleNotation switches the way numbers are written, which the save keeps.
func (g *hearthGame) cycleNotation() {
	g.format.Notation = nextNotation(g.format.Notation)
	g.tick.Update(func(n uint64) uint64 { return n + 1 })
	g.save()
}

// num writes n in the player's notation.
func (g *hearthGame) num(n idle.Number) string { return g.format.Format(n) }

// render builds the views: the HUD, the canvas and the overlay.
func (g *hearthGame) render() core.Node {
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: colorBackground, PaddingEnv: core.EnvAll(core.EnvSafeArea)},
		Children: []core.Node{
			g.renderHUD(),
			core.Canvas{
				TestID: "hearth",
				Label:  "Hearth and shop",
				Style:  core.Style{FlexGrow: 1, FlexShrink: 1},
				OnPress: func(ev core.PressEvent) {
					// Keys reach the shop through the input map, and an overlay takes every press while it shows.
					if ev.Keyboard || g.shown.Get() != overlayNone {
						return
					}
					if p, ok := g.viewport.ViewToGame(g.canvasSize, ev.Position); ok {
						g.shop.press(p)
					}
				},
				Draw: func(d *core.Drawing, size core.Size) {
					_ = g.tick.Get()
					g.canvasSize = size
					g.viewport.Draw(d, size, func(d *core.Drawing, vsize core.Size) {
						g.shop.draw(d, vsize)
					})
				},
			},
			g.renderOverlay(),
		},
	}
}

// renderHUD is the native header: embers, production, ash and the notation switch.
func (g *hearthGame) renderHUD() core.Node {
	text := func(id string, size core.Points, color core.Color, content func() string) core.Node {
		return core.Text{
			TestID:      id,
			ContentFunc: func() string { _ = g.tick.Get(); return content() },
			Style:       core.Style{Color: color, FontSize: size, FontWeight: core.FontWeightBold},
		}
	}
	return core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Padding: core.Symmetric(10, 16), Gap: 12},
		Children: []core.Node{
			core.View{
				Style: core.Style{FlexGrow: 1, Gap: 2},
				Children: []core.Node{
					text("embers", 26, colorText, func() string { return g.num(g.h.embers) + " embers" }),
					text("rate", 14, colorEmber, func() string { return "+" + g.num(g.h.rate()) + "/s" }),
					core.Show{
						When: func() bool { _ = g.tick.Get(); return !g.h.ash.IsZero() },
						Then: func(*core.Scope) core.Node {
							return text("ash", 13, colorMuted, func() string {
								bonus := g.h.ash.Scale(g.h.data.Prestige.Bonus * 100)
								return fmt.Sprintf("%s ash · +%s%%", g.num(g.h.ash), g.num(bonus))
							})
						},
					},
					core.Show{
						When: func() bool { return g.problem.Get() != "" },
						Then: func(*core.Scope) core.Node {
							return core.Text{TestID: "problem", ContentFunc: g.problem.Get, Style: core.Style{Color: colorDanger, FontSize: 12}}
						},
					},
				},
			},
			ui.Button{
				TestID:   "notation",
				Variant:  ui.ButtonSecondary,
				TextFunc: func() string { _ = g.tick.Get(); return g.format.Notation.String() },
				Label:    "Number notation",
				OnPress:  g.cycleNotation,
			},
		},
	}
}

// renderOverlay is the card over the game: the away report, the loss and the win. Its text is read every frame, because the card can change while it shows, as when the fire goes out on a return with the win card up.
func (g *hearthGame) renderOverlay() core.Node {
	field := func(pick func(title, detail, button string) string) func() string {
		return func() string { _ = g.tick.Get(); return pick(g.overlayText()) }
	}
	return core.Show{
		When: func() bool { return g.shown.Get() != overlayNone },
		Then: func(*core.Scope) core.Node {
			return core.View{
				TestID: "overlay",
				Style: core.Style{
					Position: core.Absolute, Top: core.Pt(0), Left: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0),
					AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Padding: core.All(24),
					Background: core.RGBA(0x0c0a09, 0.78),
				},
				Children: []core.Node{
					core.View{
						Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(320), Padding: core.All(20), Gap: 12, CornerRadius: 14, Background: colorPanel},
						Children: []core.Node{
							core.Text{TestID: "overlay-title", ContentFunc: field(func(title, _, _ string) string { return title }), Style: core.Style{Color: colorText, FontSize: 20, FontWeight: core.FontWeightBold}},
							core.Text{TestID: "overlay-detail", ContentFunc: field(func(_, detail, _ string) string { return detail }), Style: core.Style{Color: colorMuted, FontSize: 14}},
							ui.Button{TestID: "overlay-button", TextFunc: field(func(_, _, button string) string { return button }), Style: core.Style{Width: core.Pct(100)}, OnPress: g.dismiss},
						},
					},
				},
			}
		},
	}
}

// overlayText returns the shown overlay's title, detail and button.
func (g *hearthGame) overlayText() (title, detail, button string) {
	away := ""
	if g.away.Away.Credited > 0 {
		away = fmt.Sprintf("Away %s: +%s embers.", formatDuration(g.away.Away.Elapsed), g.num(g.away.Earned))
		if g.away.Away.Capped {
			away += fmt.Sprintf(" The hearth counts up to %s away.", formatDuration(g.away.Away.Credited))
		}
	}
	switch g.shown.Get() {
	case overlayCold:
		detail = "The fire ran out of fuel and this run is lost. Ash is kept."
		if g.away.WentCold {
			detail = away + " The fire ran out of fuel while you were away. Ash is kept."
		}
		return "The hearth went cold", detail, "Rekindle"
	case overlayWon:
		return "The " + g.h.data.Goal.Name + " burns", fmt.Sprintf("Lit after %s and %d runs. The hearth keeps burning.", formatDuration(g.h.played), g.h.runs+1), "Play on"
	default:
		return "Welcome back", away, "Continue"
	}
}

// formatDuration writes d in hours and minutes, or seconds under a minute.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int64(d/time.Second))
	}
	h, m := int64(d/time.Hour), int64(d%time.Hour/time.Minute)
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

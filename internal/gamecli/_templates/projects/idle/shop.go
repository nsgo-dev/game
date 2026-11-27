package main

import (
	"math"
	"strconv"

	"github.com/nsgo-dev/game/pkg/gameui"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/core"
)

// The palette, shared by the HUD and the canvas.
var (
	colorBackground = core.RGB(0x1c1412)
	colorPanel      = core.RGB(0x2a201b)
	colorText       = core.RGB(0xfaf5ef)
	colorMuted      = core.RGB(0xa8a29e)
	colorEmber      = core.RGB(0xfb923c)
	colorFlame      = core.RGB(0xfde68a)
	colorDanger     = core.RGB(0xf87171)
	colorAsh        = core.RGB(0x57534e)
)

// upgradeSlots is how many upgrades the shop offers at once: the next ones in hearth.yaml's order that are not bought yet.
const upgradeSlots = 3

// shop is the canvas's gameui tree: the hearth to stoke, the fuel gauge, a row per generator, the next upgrades, prestige and the goal. Pointer and touch presses reach it through press, and keyboard and controller focus through stepActions, so every control works with every input.
type shop struct {
	g    *hearthGame
	root *gameui.Root
	nav  *gameui.SpatialNavigator

	flame    *gameui.Button
	fuel     *gameui.ProgressBar
	gens     []shopRow
	upgrades [upgradeSlots]shopRow
	// offered is the index in hearth.yaml's upgrades of what each upgrade slot sells, or -1.
	offered  [upgradeSlots]int
	prestige *gameui.Button
	goal     *gameui.Button
}

// shopRow is a line of the shop: what it sells, and the button that buys it.
type shopRow struct {
	row   *gameui.HBox
	label *gameui.Label
	buy   *gameui.Button
	// affordable says the player has the embers for it, which the button's frame shows.
	affordable bool
}

// newShop builds the shop for g's hearth.
func newShop(g *hearthGame) *shop {
	s := &shop{g: g}
	s.flame = &gameui.Button{Text: "Stoke", Height: 140, FrameDrawer: s.drawFlame, TextDrawer: drawBottomText, OnClick: g.stoke}
	s.fuel = &gameui.ProgressBar{Height: 20, Max: float32(g.h.data.Fuel.Capacity), TrackColor: colorPanel}
	children := []gameui.Widget{s.flame, s.fuel}
	for i := range g.h.data.Generators {
		s.gens = append(s.gens, newShopRow(func() { g.buyGenerator(i) }))
		children = append(children, s.gens[i].row)
	}
	for k := range s.upgrades {
		s.upgrades[k] = newShopRow(func() {
			if j := s.offered[k]; j >= 0 {
				g.buyUpgrade(j)
			}
		})
		children = append(children, s.upgrades[k].row)
	}
	s.prestige = &gameui.Button{Flex: 1, OnClick: g.prestige}
	s.goal = &gameui.Button{Flex: 1, OnClick: g.buyGoal}
	children = append(children, &gameui.HBox{Height: 38, Spacing: 8, Children: []gameui.Widget{s.prestige, s.goal}})

	theme := gameui.DefaultTheme()
	theme.Font = core.Font{Size: 13}
	theme.Accent = colorEmber
	theme.FocusedBorder = colorFlame
	s.root = gameui.NewRoot(&gameui.VBox{Padding: core.All(12), Spacing: 6, Children: children})
	s.root.Theme = &theme
	s.nav = gameui.NewSpatialNavigator(s.root)
	s.refresh()
	s.layout()
	s.focusFlame()
	return s
}

// newShopRow returns a row whose button calls buy.
func newShopRow(buy func()) shopRow {
	r := shopRow{label: &gameui.Label{Flex: 1, Align: gameui.AlignCenter, TextAlign: core.TextAlignLeft}}
	r.buy = &gameui.Button{Width: 108, OnClick: buy}
	r.row = &gameui.HBox{Height: 34, Spacing: 8, Children: []gameui.Widget{r.label, r.buy}}
	return r
}

// virtualBounds is the shop's rectangle: the whole virtual resolution game.yaml sets.
func (s *shop) virtualBounds() core.Bounds {
	return core.Bounds{Size: core.Size{Width: core.Points(s.g.viewport.Width), Height: core.Points(s.g.viewport.Height)}}
}

// layout places the widgets, as drawing does, so presses and focus moves between frames see where they are.
func (s *shop) layout() { s.root.Layout(s.virtualBounds()) }

// focusFlame gives the hearth the keyboard and controller focus, where play starts.
func (s *shop) focusFlame() { s.nav.SetFocus(s.flame) }

// refresh writes the hearth's state into the widgets: labels, prices, what is offered and what is affordable.
func (s *shop) refresh() {
	g, h := s.g, s.g.h
	s.fuel.Value = float32(h.fuel)
	s.fuel.Label = "Fuel " + formatFuel(h.fuel) + "  " + formatFuelRate(h.netFuel())
	s.fuel.FillColor = colorEmber
	if h.fuel < h.data.Fuel.Capacity/4 && h.netFuel() < 0 {
		s.fuel.FillColor = colorDanger
	}

	for i, gen := range h.data.Generators {
		r := &s.gens[i]
		// A generator shows once the one before it is owned, so the shop grows as the run does.
		r.row.Hidden = i > 0 && h.owned[i] == 0 && h.owned[i-1] == 0
		r.label.Text = gen.Name + " ×" + formatCount(h.owned[i]) + "   +" + g.num(h.generatorRate(i)) + "/s"
		cost := h.generatorCost(i)
		r.buy.Text = g.num(cost)
		r.affordable = !h.cold && h.embers.Cmp(cost) >= 0
	}

	k := 0
	for j, u := range h.data.Upgrades {
		if k == upgradeSlots {
			break
		}
		if h.bought[j] {
			continue
		}
		s.offered[k] = j
		r := &s.upgrades[k]
		r.row.Hidden = false
		r.label.Text = u.Name + "   " + describeUpgrade(h.data, u)
		r.buy.Text = g.num(u.Cost)
		r.affordable = !h.cold && h.embers.Cmp(u.Cost) >= 0
		k++
	}
	for ; k < upgradeSlots; k++ {
		s.offered[k] = -1
		s.upgrades[k].row.Hidden = true
	}

	if gain := h.ashGain(); gain.IsZero() {
		s.prestige.Text = "Scatter ashes at " + g.num(h.data.Prestige.Threshold)
		s.prestige.Disabled = true
	} else {
		s.prestige.Text = "Scatter ashes +" + g.num(gain)
		s.prestige.Disabled = h.cold
	}
	if h.won {
		s.goal.Text = h.data.Goal.Name + " lit"
		s.goal.Disabled = true
	} else {
		s.goal.Text = h.data.Goal.Name + "  " + g.num(h.data.Goal.Cost)
		s.goal.Disabled = h.cold || h.embers.Cmp(h.data.Goal.Cost) < 0
	}
	for _, rows := range [][]shopRow{s.gens, s.upgrades[:]} {
		for i := range rows {
			rows[i].buy.FrameDrawer = frameDim
			if rows[i].affordable {
				rows[i].buy.FrameDrawer = frameAffordable
			}
		}
	}
}

// press handles a pointer or touch press at p, in virtual coordinates.
func (s *shop) press(p core.Point) {
	s.refresh()
	s.layout()
	s.root.OnPress(core.PressEvent{Position: p})
}

// stepActions moves the focus and presses the focused control from the step's actions.
func (s *shop) stepActions(a *input.Actions) {
	s.refresh()
	s.layout()
	s.nav.StepActions(func(n gameui.NavAction) bool {
		switch n {
		case gameui.NavUp:
			return a.JustPressed(actionUp)
		case gameui.NavDown:
			return a.JustPressed(actionDown)
		case gameui.NavLeft:
			return a.JustPressed(actionLeft)
		case gameui.NavRight:
			return a.JustPressed(actionRight)
		case gameui.NavConfirm:
			return a.JustPressed(actionConfirm)
		}
		return false
	})
}

// draw draws the shop at the virtual size.
func (s *shop) draw(d *core.Drawing, size core.Size) {
	s.refresh()
	d.SetFillColor(colorBackground).FillRect(0, 0, float32(size.Width), float32(size.Height))
	s.root.Draw(d, s.virtualBounds())
}

// drawFlame draws the hearth: a brick hearth and a flame that grows with production and flickers with the steps, or a heap of grey ash once the fire is out.
func (s *shop) drawFlame(d *core.Drawing, b core.Bounds, state gameui.WidgetState) {
	h := s.g.h
	x, y := float32(b.Origin.X), float32(b.Origin.Y)
	w, ht := float32(b.Size.Width), float32(b.Size.Height)
	fill := colorPanel
	if state.Pressed {
		fill = core.RGB(0x3a2c24)
	}
	d.SetFillColor(fill).BeginPath().RoundRect(x, y, w, ht, 12).Fill()
	cx, base := x+w/2, y+ht-30
	d.SetFillColor(core.RGB(0x44342b)).BeginPath().RoundRect(cx-70, base, 140, 10, 3).Fill()
	if h.cold {
		d.SetFillColor(colorAsh).BeginPath().MoveTo(cx-44, base).QuadTo(cx, base-26, cx+44, base).ClosePath().Fill()
		return
	}
	// The flame's height follows the order of magnitude of production, which is for display only, so Log10 may differ in its last bits between platforms.
	// The denominator is kept at 1 or more, so a goal cost of 100 or less in hearth.yaml still draws a flame.
	grow := float32(math.Max(0, math.Min(1, (h.rate().Log10()+1)/math.Max(1, h.data.Goal.Cost.Log10()-2))))
	if h.rate().IsZero() {
		grow = 0
	}
	flicker := float32(math.Sin(float64(s.g.steps)/7)) * 3
	fuel := float32(h.fuel / h.data.Fuel.Capacity)
	height := (36 + 60*grow) * (0.55 + 0.45*fuel)
	for i, c := range []core.Color{colorEmber, core.RGB(0xfdba74), colorFlame} {
		scale := 1 - float32(i)*0.3
		fw, fh := 30*scale+20*grow*scale, height*scale+flicker*scale
		d.SetFillColor(c).BeginPath().
			MoveTo(cx-fw, base).
			QuadTo(cx-fw, base-fh*0.6, cx, base-fh).
			QuadTo(cx+fw, base-fh*0.6, cx+fw, base).
			ClosePath().Fill()
	}
}

// drawBottomText writes a widget's text along its bottom edge, under the flame.
func drawBottomText(d *core.Drawing, p gameui.TextDrawParams) {
	b := p.Bounds
	d.SetFillColor(colorMuted).SetTextAlign(core.TextAlignCenter).SetTextBaseline(core.BaselineMiddle).
		FillText(p.Text, float32(b.Origin.X+b.Size.Width/2), float32(b.Origin.Y+b.Size.Height-12), p.Font)
}

// frameAffordable and frameDim frame the shop's buy buttons, so a button shows whether the player can afford what it buys.
var (
	frameAffordable = rowFrame(true)
	frameDim        = rowFrame(false)
)

// rowFrame returns a button frame that is bright while the player can afford what it buys and dim otherwise.
func rowFrame(affordable bool) gameui.FrameDrawer {
	return func(d *core.Drawing, b core.Bounds, state gameui.WidgetState) {
		fill := core.RGB(0x3b2a20)
		if affordable {
			fill = core.RGB(0x9a3412)
			if state.Pressed {
				fill = core.RGB(0xc2410c)
			}
		}
		d.SetFillColor(fill).BeginPath().RoundRect(float32(b.Origin.X), float32(b.Origin.Y), float32(b.Size.Width), float32(b.Size.Height), 8).Fill()
	}
}

// formatFuel writes a fuel level as a whole number.
func formatFuel(f float64) string { return strconv.FormatFloat(math.Floor(f), 'f', 0, 64) }

// formatFuelRate writes how fast the gauge changes, with its sign.
func formatFuelRate(r float64) string {
	r = math.Round(r*100) / 100
	sign := ""
	if r > 0 {
		sign = "+"
	} else if r == 0 {
		r = 0 // no "-0"
	}
	return sign + strconv.FormatFloat(r, 'f', -1, 64) + "/s"
}

// formatCount writes how many of a generator are owned.
func formatCount(n int64) string { return strconv.FormatInt(n, 10) }

// describeUpgrade says what upgrade u does.
func describeUpgrade(d *hearthData, u upgradeData) string {
	value := strconv.FormatFloat(u.Value, 'f', -1, 64)
	switch u.Effect {
	case effectGenerator:
		return d.Generators[d.generator(u.Target)].Name + " ×" + value
	case effectAll:
		return "all ×" + value
	case effectStoke:
		return "stoke ×" + value
	default:
		return "fuel +" + value + "/s"
	}
}

package main

import (
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
)

// Colors of the interface.
var (
	colorBackdrop = core.RGB(0x0b1026)
	colorPanel    = core.RGBA(0x0b1026, 0.78)
	colorButton   = core.RGBA(0x1f2a4d, 0.92)
	colorSelected = core.RGB(0xe8a25c)
	colorOn       = core.RGB(0x5fb8a0)
	colorText     = core.RGB(0xf8fafc)
	colorMuted    = core.RGB(0xaab4c8)
)

// menuItem is one entry of a menu: a button to tap, or to reach with the arrow keys or the D-pad and pick with the advance action.
type menuItem struct {
	// id names it in tests, as the TestID menu-<id>.
	id      string
	label   string
	enabled bool
	run     func()
}

// menuOpen reports whether a menu covers the stage: the title screen, an ending, or the pause, save and load panels.
func (n *novel) menuOpen() bool {
	switch n.panel.Get() {
	case panelPause, panelSave, panelLoad:
		return true
	case panelNone:
		return n.phase.Get() != phasePlaying
	}
	return false
}

// menuItems returns the entries of the menu shown. Read in a binding, it follows the phase, the panel, the language and the slots.
func (n *novel) menuItems() []menuItem {
	ph, pn := n.phase.Get(), n.panel.Get()
	switch {
	case pn == panelSave || pn == panelLoad:
		_ = n.slots.Get()
		items := make([]menuItem, 0, slotCount+1)
		for i := 1; i <= slotCount; i++ {
			used := n.saves.Exists(slotName(i))
			label := n.t("slot.empty", l10n.Param("slot", i))
			if used {
				label = n.t("slot.used", l10n.Param("slot", i), l10n.Param("chapter", n.slotChapter(i)))
			}
			item := menuItem{id: slotName(i), label: label, enabled: used, run: func() { n.loadFrom(i) }}
			if pn == panelSave {
				item.enabled, item.run = true, func() { n.saveTo(i) }
				// A used slot asks first: the first press shows the question, and a second press on it overwrites the save.
				if used && n.overwrite.Get() != i {
					item.run = func() { n.overwrite.Set(i) }
				} else if used {
					item.label = n.t("slot.overwrite", l10n.Param("slot", i))
				}
			}
			items = append(items, item)
		}
		return append(items, menuItem{id: "back", label: n.t("menu.back"), enabled: true, run: n.back})
	case pn == panelPause:
		return []menuItem{
			{id: "resume", label: n.t("menu.resume"), enabled: true, run: func() { n.openPanel(panelNone) }},
			{id: "save", label: n.t("menu.save"), enabled: true, run: func() { n.openPanel(panelSave) }},
			{id: "load", label: n.t("menu.load"), enabled: n.anySave(), run: func() { n.openPanel(panelLoad) }},
			{id: "title", label: n.t("menu.title"), enabled: true, run: n.toTitle},
		}
	case ph == phaseTitle:
		return []menuItem{
			{id: "new", label: n.t("menu.new"), enabled: true, run: n.newGame},
			{id: "load", label: n.t("menu.load"), enabled: n.anySave(), run: func() { n.openPanel(panelLoad) }},
			{id: "language", label: n.t("menu.language"), enabled: true, run: func() {
				n.setLocale(nextLanguage(n.locale.GetUntracked()))
				n.saveSettings()
			}},
		}
	case ph == phaseEnded:
		return []menuItem{{id: "title", label: n.t("menu.title"), enabled: true, run: n.toTitle}}
	}
	return nil
}

// anySave reports whether any save slot holds a save. Read in a binding, it follows saves.
func (n *novel) anySave() bool {
	_ = n.slots.Get()
	for i := 1; i <= slotCount; i++ {
		if n.saves.Exists(slotName(i)) {
			return true
		}
	}
	return false
}

// slotChapter returns the chapter name of the line slot i holds.
func (n *novel) slotChapter(i int) string {
	var data saveData
	if err := n.saves.LoadChecked(slotName(i), &data, data.check); err != nil {
		return "?"
	}
	return n.t("chapter." + n.story.chapterOf(data.Node))
}

// activate runs the menu item at position i, if it is enabled.
func (n *novel) activate(i int) {
	items := n.menuItems()
	if i >= 0 && i < len(items) && items[i].enabled {
		items[i].run()
	}
}

// step moves the highlight from position from by dir to the next enabled item, staying put at either end.
func step(items []menuItem, from, dir int) int {
	for i := from + dir; i >= 0 && i < len(items); i += dir {
		if items[i].enabled {
			return i
		}
	}
	return from
}

// view is the game's view tree: the stage canvas, the buttons over the story, the menus and the status message.
func (n *novel) view() core.Node {
	full := core.Style{Position: core.Absolute, Top: core.Pt(0), Left: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0)}
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: colorBackdrop},
		Children: []core.Node{
			core.View{
				TestID:  "stage",
				Style:   full,
				OnPress: func(ev core.PressEvent) { n.press(ev.Position) },
				Children: []core.Node{
					core.Canvas{
						TestID: "canvas",
						Label:  "Story",
						Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
						Draw:   n.draw,
					},
				},
			},
			core.Show{
				When: func() bool { return n.phase.Get() == phasePlaying && n.panel.Get() == panelNone },
				Then: func(*core.Scope) core.Node { return n.hud() },
			},
			core.Show{
				When: func() bool { return n.phase.Get() == phasePlaying && n.panel.Get() == panelLog },
				Then: func(*core.Scope) core.Node { return n.logBar() },
			},
			core.Show{
				When: n.menuOpen,
				Then: func(*core.Scope) core.Node { return core.Dynamic(func(*core.Scope) core.Node { return n.menu() }) },
			},
			core.Show{
				When: func() bool { return n.status.Get() != "" },
				Then: func(*core.Scope) core.Node {
					return core.View{
						PointerEvents: core.PointerEventsNone,
						Style: core.Style{
							Position: core.Absolute, Top: core.Pt(12), Left: core.Pt(12),
							PaddingEnv: core.EnvAll(core.EnvSafeArea),
						},
						Children: []core.Node{core.Text{
							TestID:      "status",
							ContentFunc: n.status.Get,
							Style: core.Style{
								Color: colorText, FontSize: 14, Background: colorPanel,
								CornerRadius: 8, Padding: core.Symmetric(6, 12),
							},
						}},
					}
				},
			},
		},
	}
}

// hud is the row of buttons over the story: auto and skip modes, the backlog and the menu.
func (n *novel) hud() core.Node {
	toggle := func(id, key string, on *core.Signal[bool], press func()) core.Node {
		return n.button(id, func() string { return n.t(key) }, func() bool { return on != nil && on.Get() }, press)
	}
	return core.View{
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0),
			Direction: core.Row, Gap: 8, Padding: core.All(12),
			PaddingEnv: core.EnvAll(core.EnvSafeArea),
		},
		Children: []core.Node{
			toggle("auto", "hud.auto", n.auto, func() { n.run.p.SetAuto(!n.run.p.Auto()); n.auto.Set(n.run.p.Auto()) }),
			toggle("skip", "hud.skip", n.skip, func() { n.run.p.SetSkip(!n.run.p.Skipping()); n.skip.Set(n.run.p.Skipping()) }),
			toggle("log", "hud.log", nil, func() { n.openPanel(panelLog) }),
			toggle("menu", "hud.menu", nil, func() { n.openPanel(panelPause) }),
		},
	}
}

// logBar is the row of buttons over the backlog, which scroll it for touch and the mouse and close it. The keyboard and controllers scroll it with the up and down actions.
func (n *novel) logBar() core.Node {
	return core.View{
		PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{
			Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0),
			Direction: core.Row, Gap: 8, Padding: core.All(12),
			PaddingEnv: core.EnvAll(core.EnvSafeArea),
		},
		Children: []core.Node{
			n.logButton("log-older", "log.older", func() bool { return n.logScroll.Get() >= n.logScrollMax() }, func() { n.scrollLog(1) }),
			n.logButton("log-newer", "log.newer", func() bool { return n.logScroll.Get() == 0 }, func() { n.scrollLog(-1) }),
			n.logButton("log-close", "log.close", func() bool { return false }, func() { n.openPanel(panelNone) }),
		},
	}
}

// logButton is a button of the backlog's row, dimmed and inert while disabled reports true.
func (n *novel) logButton(id, key string, disabled func() bool, press func()) core.Node {
	label := func() string { return n.t(key) }
	return core.View{
		TestID:    id,
		Role:      core.RoleButton,
		LabelFunc: label,
		NoFocus:   true,
		Disabled:  disabled,
		OnPress:   func(core.PressEvent) { press() },
		Style:     core.Style{Background: colorButton, CornerRadius: 8, Padding: core.Symmetric(6, 12)},
		Children: []core.Node{core.Text{
			ContentFunc: label,
			StyleFunc: func() core.Style {
				color := colorText
				if disabled() {
					color = colorMuted
				}
				return core.Style{Color: color, FontSize: 14, FontWeight: core.FontWeightBold}
			},
		}},
	}
}

// button is a small button of the HUD, filled while on reports true. It is no Tab stop: the keyboard and controllers reach the same actions through game.yaml's input map, so Space and Return never press it as well as go on.
func (n *novel) button(id string, label func() string, on func() bool, press func()) core.Node {
	return core.View{
		TestID:    id,
		Role:      core.RoleButton,
		LabelFunc: label,
		Pressed:   on,
		NoFocus:   true,
		OnPress:   func(core.PressEvent) { press() },
		StyleFunc: func() core.Style {
			bg := colorButton
			if on() {
				bg = colorOn
			}
			return core.Style{Background: bg, CornerRadius: 8, Padding: core.Symmetric(6, 12)}
		},
		Children: []core.Node{core.Text{
			ContentFunc: label,
			Style:       core.Style{Color: colorText, FontSize: 14, FontWeight: core.FontWeightBold},
		}},
	}
}

// menu is the menu shown: its heading and its items, the highlighted one outlined.
func (n *novel) menu() core.Node {
	var heading []core.Node
	text := func(id, s string, size core.Points, color core.Color) core.Node {
		return core.Text{TestID: id, Content: s, Style: core.Style{Color: color, FontSize: size, TextAlign: core.TextAlignCenter}}
	}
	backdrop := colorPanel
	switch {
	case n.panel.Get() == panelSave:
		heading = append(heading, text("heading", n.t("panel.save"), 22, colorText))
	case n.panel.Get() == panelLoad:
		heading = append(heading, text("heading", n.t("panel.load"), 22, colorText))
	case n.panel.Get() == panelPause:
	case n.phase.Get() == phaseTitle:
		backdrop = core.RGBA(0x0b1026, 0.35)
		heading = append(heading, text("heading", Project.Title(), 40, colorText), text("tagline", n.t("title.tagline"), 16, colorMuted))
		if len(n.found) > 0 {
			heading = append(heading, text("endings-found", n.endingsFound(), 14, colorMuted))
		}
	case n.phase.Get() == phaseEnded:
		ending := n.run.ending
		heading = append(heading,
			text("heading", n.t("ending."+ending), 34, colorText),
			text("ending-text", n.t("ending."+ending+".text"), 16, colorMuted),
			text("endings-found", n.endingsFound(), 14, colorMuted),
		)
	}
	children := heading
	for i, item := range n.menuItems() {
		children = append(children, n.menuButton(i, item))
	}
	return core.View{
		TestID: "menu-panel",
		Style: core.Style{
			Position: core.Absolute, Top: core.Pt(0), Left: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0),
			Background: backdrop, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
			Gap: 12, Padding: core.All(24), PaddingEnv: core.EnvAll(core.EnvSafeArea),
		},
		Children: children,
	}
}

// endingsFound is the line that counts the endings found.
func (n *novel) endingsFound() string {
	return n.t("endings.found", l10n.Param("found", len(n.found)), l10n.Param("total", len(endings)))
}

// menuButton is the button of a menu's item at position i.
func (n *novel) menuButton(i int, item menuItem) core.Node {
	textColor := colorText
	if !item.enabled {
		textColor = colorMuted
	}
	return core.View{
		TestID:   "menu-" + item.id,
		Role:     core.RoleButton,
		Label:    item.label,
		NoFocus:  true,
		Disabled: func() bool { return !item.enabled },
		OnPress: func(core.PressEvent) {
			n.selected.Set(i)
			n.activate(i)
		},
		StyleFunc: func() core.Style {
			border := core.RGBA(0xffffff, 0.12)
			if n.selected.Get() == i {
				border = colorSelected
			}
			return core.Style{
				Width: core.Pt(300), Padding: core.Symmetric(10, 16), CornerRadius: 10,
				Background: colorButton, BorderWidth: 2, BorderColor: border, AlignItems: core.AlignCenter,
			}
		},
		Children: []core.Node{core.Text{Content: item.label, Style: core.Style{Color: textColor, FontSize: 17}}},
	}
}

// draw draws the stage: the background, then the portraits, the text box and the choices, or the backlog.
func (n *novel) draw(d *core.Drawing, size core.Size) {
	_ = n.frame.Get()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	n.size = size
	w, h, t := float32(size.Width), float32(size.Height), float32(n.clock.Seconds())
	r := n.run
	if n.phase.GetUntracked() == phaseTitle {
		drawBackground(d, titleBackground, w, h, t)
		return
	}
	if r.fade > 0 && r.previous != "" {
		drawBackground(d, r.previous, w, h, t)
		d.Save().SetAlpha(1 - float32(r.fade)/float32(fadeDuration))
		drawBackground(d, r.background, w, h, t)
		d.Restore()
	} else {
		drawBackground(d, r.background, w, h, t)
	}
	if r.p == nil || n.phase.GetUntracked() != phasePlaying {
		return
	}
	st := n.styleFor(size)
	if n.panel.GetUntracked() == panelLog {
		// The backlog starts below the band its buttons stand in.
		top := logBarHeight + float32(n.insets().Top)
		d.Save().SetFillColor(core.RGBA(0x0b1020, 0.94)).FillRect(0, 0, w, top).Translate(0, top)
		r.p.DrawBacklog(d, core.Size{Width: size.Width, Height: core.Points(max(h-top, 0))}, st, n.logScroll.GetUntracked())
		d.Restore()
		return
	}
	r.p.Draw(d, size, st)
	// Outline the choice the keyboard or controller would pick.
	if choices := r.p.Choices(); len(choices) > 0 {
		if sel := n.selected.GetUntracked(); sel >= 0 && sel < len(choices) {
			b := st.Layout(size, len(choices)).Choices[sel]
			d.SetStrokeColor(colorSelected).SetLineWidth(3).BeginPath().
				RoundRect(float32(b.Origin.X)-3, float32(b.Origin.Y)-3, float32(b.Size.Width)+6, float32(b.Size.Height)+6, 10).Stroke()
		}
	}
}

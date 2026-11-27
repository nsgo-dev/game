package main

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/quest"
)

// The menu pages. Every menu is a list of entries the player moves through with up and down and picks with confirm, or presses directly.
const (
	pageTitle  = "title"
	pagePack   = "pack"
	pageSave   = "save"
	pageLoad   = "load"
	pageBattle = "battle"
	pageLost   = "lost"
	pageWon    = "won"
)

// slots are the save slots.
var slots = []string{"slot1", "slot2", "slot3"}

// menuItem is one entry of a menu.
type menuItem struct {
	// id is unique within its menu; the UI's test IDs are "menu-" + id.
	id       string
	label    string
	disabled bool
	run      func()
}

// menuView is what a menu shows: a title, lines of information and the entries.
type menuView struct {
	title string
	lines []string
	items []menuItem
	// bottom places the menu along the bottom of the screen, as the battle's, rather than in the middle.
	bottom bool
}

// showMenu switches to mode m showing page, its first entry selected.
func (g *Game) showMenu(m mode, page string) {
	g.mode, g.page, g.back, g.sel = m, page, "", 0
	g.readSlots()
	g.rev++
}

// showPage shows page in the current mode, cancel returning to the page shown now.
func (g *Game) showPage(page string) {
	g.back, g.page, g.sel = g.page, page, 0
	g.readSlots()
	g.rev++
}

// menuView returns the menu the game shows, and false when it shows none.
func (g *Game) menuView() (menuView, bool) {
	switch g.mode {
	case modeField, modeTalk:
		return menuView{}, false
	}
	switch g.page {
	case pageTitle:
		return menuView{title: g.title, items: []menuItem{
			{id: "new", label: "New game", run: g.startNewGame},
			{id: "load", label: "Load game", disabled: !g.anySave(), run: func() { g.showPage(pageLoad) }},
		}}, true
	case pageSave, pageLoad:
		return g.slotMenu(g.page == pageSave), true
	case pagePack:
		return g.packMenu(), true
	case pageBattle:
		return g.battleMenu(), true
	case pageLost:
		return menuView{title: "You fall in " + g.level.Name, items: []menuItem{
			{id: "load", label: "Load game", disabled: !g.anySave(), run: func() { g.showPage(pageLoad) }},
			{id: "new", label: "New game", run: g.startNewGame},
		}}, true
	case pageWon:
		return menuView{title: "The shrine burns again", lines: []string{"Hollowbrook is safe, and Mira's charm is in your pack."}, items: []menuItem{
			{id: "continue", label: "Keep exploring", run: func() { g.won = false; g.mode = modeField }},
			{id: "new", label: "New game", run: g.startNewGame},
		}}, true
	}
	return menuView{}, false
}

// packMenu lists the player's stats, equipment and quest with what they can do with their items, save and load.
func (g *Game) packMenu() menuView {
	p := &g.progress
	v := menuView{title: "Pack"}
	v.lines = append(v.lines, fmt.Sprintf("HP %d/%d · Attack %d · Defense %d · Coins %d", g.hp, maxHP(p), attack(p), defense(p), g.bag.Count(itemCoin)))
	var worn []string
	for _, s := range g.gear.Slots() {
		name := "none"
		if item, ok := g.gear.Equipped(s.Name); ok {
			def, _ := g.c.items.Item(item)
			name = def.Name
		}
		worn = append(worn, strings.ToUpper(s.Name[:1])+s.Name[1:]+": "+name)
	}
	v.lines = append(v.lines, strings.Join(worn, " · "))
	var carried []string
	for _, s := range g.bag.Slots() {
		if s.Empty() || s.Item == itemCoin {
			continue
		}
		def, _ := g.c.items.Item(s.Item)
		carried = append(carried, amountOf(def.Name, s.Count))
	}
	if len(carried) > 0 {
		v.lines = append(v.lines, "Carrying: "+strings.Join(carried, ", "))
	}
	if q := g.c.quests.Quest(mainQuest); g.quests.State(mainQuest) == quest.Active {
		var objs []string
		for _, o := range g.quests.Objectives(mainQuest) {
			mark := "○"
			if o.State == quest.ObjectiveDone {
				mark = "●"
			}
			objs = append(objs, fmt.Sprintf("%s %s", mark, o.Text))
		}
		v.lines = append(v.lines, q.Title+": "+strings.Join(objs, "  "))
	}

	v.items = append(v.items, menuItem{id: "potion", label: fmt.Sprintf("Drink a potion (%d)", g.bag.Count(itemPotion)), disabled: g.bag.Count(itemPotion) == 0 || g.hp >= maxHP(p), run: g.drinkPotion})
	for i, s := range g.bag.Slots() {
		def, _ := g.c.items.Item(s.Item)
		if s.Empty() || def.Equip == "" {
			continue
		}
		v.items = append(v.items, menuItem{id: "equip-" + s.Item, label: "Equip the " + def.Name, run: func() { g.equip(i) }})
	}
	v.items = append(v.items,
		menuItem{id: "save", label: "Save game", run: func() { g.showPage(pageSave) }},
		menuItem{id: "load", label: "Load game", disabled: !g.anySave(), run: func() { g.showPage(pageLoad) }},
		menuItem{id: "close", label: "Close", run: g.closeMenu},
	)
	return v
}

// battleMenu shows the foe and the battle's log with the player's three moves.
func (g *Game) battleMenu() menuView {
	b := g.battle
	potions := g.bag.Count(itemPotion)
	return menuView{
		title:  fmt.Sprintf("%s · HP %d/%d", b.foe.kind.Name, b.hp, b.foe.kind.HP),
		lines:  b.log,
		bottom: true,
		items: []menuItem{
			{id: "attack", label: "Attack", run: g.attackFoe},
			{id: "potion", label: fmt.Sprintf("Potion (%d)", potions), disabled: potions == 0 || g.hp >= maxHP(&g.progress), run: g.drinkPotion},
			{id: "flee", label: "Flee", disabled: b.foe.kind.Boss, run: g.flee},
		},
	}
}

// slotMenu lists the save slots to save to or load from.
func (g *Game) slotMenu(saving bool) menuView {
	v := menuView{title: "Load game"}
	if saving {
		v.title = "Save game"
	}
	for i, slot := range slots {
		read := g.slotsRead[i]
		item := menuItem{id: slot, label: fmt.Sprintf("Slot %d · %s", i+1, read.summary)}
		if saving {
			item.run = func() { g.saveTo(slot) }
		} else {
			item.disabled = !read.ok
			item.run = func() { g.loadFrom(slot) }
		}
		v.items = append(v.items, item)
	}
	v.items = append(v.items, menuItem{id: "back", label: "Back", run: g.menuBack})
	return v
}

// updateMenu moves the selection with up and down, picks the selected entry with confirm and goes back with cancel. The menu button closes the pack.
func (g *Game) updateMenu(in intent) {
	v, ok := g.menuView()
	if !ok || len(v.items) == 0 {
		return
	}
	g.sel = min(g.sel, len(v.items)-1)
	if v.items[g.sel].disabled {
		g.moveSel(v, true)
	}
	switch {
	case in.turned && (in.dir == dirUp || in.dir == dirDown):
		g.moveSel(v, in.dir == dirDown)
	case in.confirm:
		g.pick(v.items[g.sel].id)
	case in.cancel || in.menu && g.mode == modeMenu:
		g.menuBack()
	}
}

// moveSel selects the next enabled entry of v after (or, with down false, before) the selected one, wrapping around.
func (g *Game) moveSel(v menuView, down bool) {
	step := len(v.items) - 1
	if down {
		step = 1
	}
	for range v.items {
		g.sel = (g.sel + step) % len(v.items)
		if !v.items[g.sel].disabled {
			break
		}
	}
	g.rev++
}

// pick runs the entry id of the menu shown, if it is there and enabled; a press on the entry's button calls it.
func (g *Game) pick(id string) {
	v, ok := g.menuView()
	if !ok {
		return
	}
	for i, item := range v.items {
		if item.id == id && !item.disabled {
			g.sel = i
			item.run()
			g.rev++
			return
		}
	}
}

// menuBack returns to the page the current one was opened from, or closes the pack.
func (g *Game) menuBack() {
	switch {
	case g.back != "":
		g.page, g.back, g.sel = g.back, "", 0
		g.readSlots()
		g.rev++
	case g.mode == modeMenu:
		g.closeMenu()
	}
}

// closeMenu returns from the pack to the map.
func (g *Game) closeMenu() {
	g.mode = modeField
	g.rev++
}

// amountOf names n of an item for a list: "Potion" or "Potion ×3".
func amountOf(name string, n int) string {
	if n == 1 {
		return name
	}
	return fmt.Sprintf("%s ×%d", name, n)
}

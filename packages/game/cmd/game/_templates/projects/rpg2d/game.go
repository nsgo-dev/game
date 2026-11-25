package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/flow"
	"github.com/nsgo-dev/game/packages/game/inventory"
	"github.com/nsgo-dev/game/packages/game/quest"
	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/game/packages/game/stats"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/game/packages/game/vn"
	"github.com/nsgo-dev/nsgo/core"
)

// Equipment slots, each named after the kind of item it takes (items.yaml's equip field).
const (
	slotWeapon = "weapon"
	slotArmor  = "armor"
	slotCharm  = "charm"
)

// Stats of stats.yaml the game reads.
const (
	statMaxHP   = "maxHP"
	statAttack  = "attack"
	statDefense = "defense"
)

// Items of items.yaml the game itself knows about.
const (
	itemCoin   = "coin"
	itemPotion = "potion"
)

// Tuning of the game's pace and its new game.
const (
	bagSize     = 12
	startCoins  = 15
	startWeapon = "wooden-sword"
	// stepTime is how long the player and foes take to walk one tile.
	stepTime = 160 * time.Millisecond
	// foeWait is how long a patrolling foe waits between steps.
	foeWait = 600 * time.Millisecond
	// fleeGrace is how long foes leave the player alone after a flight.
	fleeGrace   = 1500 * time.Millisecond
	messageTime = 2500 * time.Millisecond
	flashTime   = 250 * time.Millisecond
)

// dir is a direction of movement or facing.
type dir uint8

const (
	dirNone dir = iota
	dirUp
	dirDown
	dirLeft
	dirRight
)

// delta returns the change in column and row of one step in d.
func (d dir) delta() (dx, dy int) {
	switch d {
	case dirUp:
		return 0, -1
	case dirDown:
		return 0, 1
	case dirLeft:
		return -1, 0
	case dirRight:
		return 1, 0
	}
	return 0, 0
}

// step returns the cell one step from c in d.
func (c Cell) step(d dir) Cell {
	dx, dy := d.delta()
	return Cell{c.X() + dx, c.Y() + dy}
}

// mode is what the game is doing, which decides what input does and what is drawn.
type mode uint8

const (
	// modeTitle shows the title menu over the town.
	modeTitle mode = iota
	// modeField lets the player walk the current map.
	modeField
	// modeTalk plays a conversation over the map.
	modeTalk
	// modeMenu shows the pack: items, equipment, the quest and the save slots.
	modeMenu
	// modeBattle is a turn-based fight with one foe.
	modeBattle
	// modeLost is the screen after the player's HP reached 0.
	modeLost
	// modeWon is the screen after the main quest was completed.
	modeWon
)

// intent is what the player asked for during one fixed step, read from keyboard, gamepad and touch alike (input.go).
type intent struct {
	// dir is the direction held, dirNone for none.
	dir dir
	// turned reports that dir was pressed this step, for moving through menus one entry per press.
	turned bool
	// confirm, cancel and menu report that the button was pressed this step.
	confirm, cancel, menu bool
	// tap, when set, is where a tap or click on the stage landed, in game points.
	tap *core.Point
}

// progress is the state of one playthrough, which a save slot keeps.
type progress struct {
	mapID    string
	pos      Cell
	facing   dir
	hp       int
	bag      *inventory.Container
	gear     *inventory.Equipment
	sheet    *stats.Sheet
	quests   *quest.Log
	talk     *vn.Presenter
	defeated map[string]bool
	taken    map[string]bool
}

// foeState is a foe walking the current map.
type foeState struct {
	*Foe
	kind FoeDef
	at   Cell
	from Cell
	// walk is how much of a step is left, 0 standing; wait counts down to the next step.
	walk, wait time.Duration
	// stop is the index of the patrol stop it walks to: 0 is its start, i is Patrol[i-1].
	stop int
}

// battle is a fight in progress.
type battle struct {
	foe *foeState
	hp  int
	// log holds the last few things that happened, the newest last.
	log []string
	// hit and hurt count down the flashes of the foe and the player after a blow.
	hit, hurt time.Duration
}

// Game is the whole game's state and rules, with no drawing or input devices, so tests drive it directly as well as through the app.
type Game struct {
	c     *content
	store *save.Store
	title string

	mode mode
	// page and sel are the menu shown in the menu modes and its selected entry; back is the page cancel returns to.
	page, back string
	sel        int

	progress

	level *MapDef
	tiles map[string]*tilemap.Tilemap
	foes  []*foeState
	// from and walk are the step the player is taking: the tile left and the time left, 0 when standing.
	from Cell
	walk time.Duration
	// arriving is set when a foe started a battle while the player was stepping onto a tile, so the step finishes, picking up what lies there or taking the exit, once the battle is won or fled.
	arriving bool

	battle *battle
	grace  time.Duration
	// choice is the selected option of the conversation.
	choice int
	// won is set when the main quest completes, and shows modeWon once the conversation ends.
	won bool

	// slotsRead is what the save slots held when the menu page was last shown, read from storage once per page rather than on every step (readSlots).
	slotsRead []slotRead

	message     string
	messageLeft time.Duration
	// rev changes whenever what the menus show changes, for the UI to rebuild them.
	rev uint64
}

// newGame returns a game on its title screen with a new playthrough ready behind it.
func newGame(c *content, store *save.Store, title string) *Game {
	g := &Game{c: c, store: store, title: title, tiles: map[string]*tilemap.Tilemap{}}
	g.progress = g.newProgress()
	g.enter(c.world.Start)
	g.showMenu(modeTitle, pageTitle)
	return g
}

// newProgress returns the state a new game starts in: the starting coins and weapon, full HP and no quest.
func (g *Game) newProgress() progress {
	c := g.c
	p := progress{
		mapID:    c.world.Start.Map,
		pos:      c.world.Start.At,
		facing:   dirDown,
		bag:      must(inventory.NewContainer(c.items, bagSize)),
		gear:     must(inventory.NewEquipment(c.items, inventory.EquipSlot{Name: slotWeapon, Kind: slotWeapon}, inventory.EquipSlot{Name: slotArmor, Kind: slotArmor}, inventory.EquipSlot{Name: slotCharm, Kind: slotCharm})),
		sheet:    must(c.stats.NewSheet()),
		quests:   must(quest.NewLog(c.quests)),
		defeated: map[string]bool{},
		taken:    map[string]bool{},
	}
	p.quests.OnUpdate = g.onQuest
	p.talk = vn.New(c.dialogue, c.cast, vn.Options{Dialogue: flow.DialogueOptions{OnCommand: g.onCommand}, CharsPerSecond: 60})
	check(p.bag.AddAll(itemCoin, startCoins))
	check(p.bag.AddAll(startWeapon, 1))
	check(p.gear.Equip(slotWeapon, p.bag, slices.IndexFunc(p.bag.Slots(), func(s inventory.Stack) bool { return s.Item == startWeapon })))
	applyGear(c, &p)
	p.hp = maxHP(&p)
	return p
}

// startNewGame throws the current playthrough away and starts a new one in the town.
func (g *Game) startNewGame() {
	g.progress = g.newProgress()
	g.battle, g.won, g.grace, g.arriving = nil, false, 0, false
	g.enter(g.c.world.Start)
	g.mode = modeField
	g.say(g.level.Name)
}

// update moves the game on by one fixed step dt with what the player asked for.
func (g *Game) update(dt time.Duration, in intent) {
	if g.messageLeft > 0 {
		g.messageLeft -= dt
		if g.messageLeft <= 0 {
			g.message = ""
		}
	}
	if b := g.battle; b != nil {
		b.hit = max(b.hit-dt, 0)
		b.hurt = max(b.hurt-dt, 0)
	}
	switch g.mode {
	case modeField:
		g.updateField(dt, in)
	case modeTalk:
		g.updateTalk(dt, in)
	default:
		g.updateMenu(in)
	}
}

// updateField walks the player and the foes.
func (g *Game) updateField(dt time.Duration, in intent) {
	g.grace = max(g.grace-dt, 0)
	for _, f := range slices.Clone(g.foes) {
		g.updateFoe(f, dt)
		if g.mode != modeField {
			return
		}
	}
	if g.walk > 0 {
		g.walk -= dt
		if g.walk > 0 {
			return
		}
		g.walk = 0
		g.arrive()
		if g.mode != modeField {
			return
		}
	}
	switch {
	case in.menu || in.cancel:
		g.showMenu(modeMenu, pagePack)
	case in.confirm:
		g.interact()
	case in.dir != dirNone:
		g.facing = in.dir
		next := g.pos.step(in.dir)
		if f := g.foeAt(next); f != nil {
			g.startBattle(f)
			return
		}
		if !g.blocked(next) {
			g.from, g.pos, g.walk = g.pos, next, stepTime
		}
	}
}

// arrive picks up what lies on the tile the player stepped onto and takes the exit there.
func (g *Game) arrive() {
	for _, it := range g.level.Items {
		if it.At != g.pos || g.taken[it.ID] {
			continue
		}
		def, _ := g.c.items.Item(it.Item)
		if err := g.bag.AddAll(it.Item, it.Quantity()); err != nil {
			g.say("Your pack is too full for the " + def.Name + ".")
			continue
		}
		g.taken[it.ID] = true
		g.say("Found " + amount(def.Name, it.Quantity()) + ".")
		g.quests.Handle(quest.Collected{Item: it.Item, Amount: it.Quantity()}.Event())
	}
	for _, e := range g.level.Exits {
		if e.At == g.pos {
			g.enter(e.To)
			g.say(g.level.Name)
			return
		}
	}
}

// updateFoe walks a patrolling foe toward its next stop, starting a battle when it walks into the player.
func (g *Game) updateFoe(f *foeState, dt time.Duration) {
	if f.walk > 0 {
		f.walk = max(f.walk-dt, 0)
		return
	}
	if len(f.Patrol) == 0 {
		return
	}
	if f.wait -= dt; f.wait > 0 {
		return
	}
	f.wait = foeWait
	stops := append([]Cell{f.At}, f.Patrol...)
	if f.at == stops[f.stop] {
		f.stop = (f.stop + 1) % len(stops)
	}
	goal := stops[f.stop]
	d := dirNone
	switch {
	case goal.X() < f.at.X():
		d = dirLeft
	case goal.X() > f.at.X():
		d = dirRight
	case goal.Y() < f.at.Y():
		d = dirUp
	case goal.Y() > f.at.Y():
		d = dirDown
	}
	next := f.at.step(d)
	if next == g.pos {
		if g.grace == 0 {
			g.startBattle(f)
		}
		return
	}
	if g.blocked(next) || g.foeAt(next) != nil {
		return
	}
	f.from, f.at, f.walk = f.at, next, stepTime
}

// interact talks to the person the player faces.
func (g *Game) interact() {
	if p := g.personAt(g.pos.step(g.facing)); p != nil {
		g.startTalk(p)
	}
}

// enter puts the player on a map, with its foes back at their posts.
func (g *Game) enter(p Place) {
	g.level = g.c.world.Map(p.Map)
	g.mapID, g.pos, g.from, g.walk = p.Map, p.At, p.At, 0
	g.foes = nil
	for i := range g.level.Foes {
		f := &g.level.Foes[i]
		if g.defeated[f.ID] {
			continue
		}
		kind, _ := g.c.world.Foe(f.Kind)
		g.foes = append(g.foes, &foeState{Foe: f, kind: kind, at: f.At, from: f.At, wait: foeWait})
	}
	if g.tiles[p.Map] == nil {
		g.tiles[p.Map] = buildTilemap(g.c, g.level)
	}
}

// buildTilemap turns a map's rows into a tilemap.Tilemap drawn with the atlas, its solid tiles flagged tilemap.TileSolid.
func buildTilemap(c *content, m *MapDef) *tilemap.Tilemap {
	w := c.world
	names := make([]string, len(w.Tiles))
	gids := map[rune]uint32{}
	for i, t := range w.Tiles {
		names[i] = t.Art
		gids[[]rune(t.Key)[0]] = uint32(i + 1)
	}
	width, height := m.Size()
	size := float32(w.TileSize)
	tm := tilemap.New(width, height, size, size, tilemap.NewAtlasTileset(1, c.atlas, names...))
	data := make([]uint32, 0, width*height)
	for _, row := range m.Rows {
		for _, r := range row {
			data = append(data, gids[r])
		}
	}
	tm.Layers = append(tm.Layers, tilemap.NewTileLayer("ground", width, height, data))
	for i, t := range w.Tiles {
		if t.Solid {
			tm.SetFlags(uint32(i+1), tilemap.TileSolid)
		}
	}
	return tm
}

// blocked reports whether nobody may walk onto c: it is outside the map, solid or where a person stands. Foes are checked apart, since walking into one starts a battle.
func (g *Game) blocked(c Cell) bool {
	width, height := g.level.Size()
	if c.X() < 0 || c.Y() < 0 || c.X() >= width || c.Y() >= height {
		return true
	}
	if g.tiles[g.mapID].FlagsAt(c.X(), c.Y())&tilemap.TileSolid != 0 {
		return true
	}
	return g.personAt(c) != nil
}

// personAt returns the person standing on c, or nil.
func (g *Game) personAt(c Cell) *Person {
	for i := range g.level.People {
		if g.level.People[i].At == c {
			return &g.level.People[i]
		}
	}
	return nil
}

// foeAt returns the foe standing on c, or nil.
func (g *Game) foeAt(c Cell) *foeState {
	for _, f := range g.foes {
		if f.at == c {
			return f
		}
	}
	return nil
}

// --- Conversations ---

// startTalk starts the conversation with p, telling the script what it may ask about.
func (g *Game) startTalk(p *Person) {
	r := g.talk.Runner()
	r.SetVar("coins", g.bag.Count(itemCoin))
	r.SetVar("potions", g.bag.Count(itemPotion))
	r.SetVar("quest", g.quests.State(mainQuest).String())
	r.SetVar("ready", g.questReady(mainQuest))
	r.SetVar("hasSword", g.owns("iron-sword"))
	r.SetVar("hurt", g.hp < maxHP(&g.progress))
	g.mode, g.choice = modeTalk, 0
	if err := g.talk.Start(p.Node); err != nil {
		g.endTalk()
		g.say(err.Error())
		return
	}
	if g.talk.Ended() {
		g.endTalk()
	}
}

// updateTalk reveals the conversation's text and moves it on when the player presses confirm or taps, choosing the selected option where options wait.
func (g *Game) updateTalk(dt time.Duration, in intent) {
	if err := g.talk.Update(dt); err != nil {
		g.endTalk()
		g.say(err.Error())
		return
	}
	var err error
	if choices := g.talk.Choices(); len(choices) > 0 {
		g.choice = min(g.choice, len(choices)-1)
		if !choices[g.choice].Available {
			g.choice = nextChoice(choices, g.choice, true)
		}
		if in.turned && (in.dir == dirUp || in.dir == dirDown) {
			g.choice = nextChoice(choices, g.choice, in.dir == dirDown)
		}
		pick := -1
		if in.tap != nil {
			if i, ok := g.talk.ChoiceAt(stageSize, talkStyle(g.c), *in.tap); ok {
				pick = i
			}
		}
		if pick < 0 && in.confirm && choices[g.choice].Available {
			pick = choices[g.choice].Index
		}
		if pick >= 0 {
			g.choice = 0
			err = g.talk.Choose(pick)
		}
	} else if in.confirm || in.tap != nil {
		err = g.talk.Advance()
	}
	if err != nil {
		g.say(err.Error())
	}
	if err != nil || g.talk.Ended() {
		g.endTalk()
	}
}

// nextChoice returns the available option after (or, with down false, before) the one at i, wrapping around.
func nextChoice(choices []flow.DialogueChoice, i int, down bool) int {
	step := len(choices) - 1
	if down {
		step = 1
	}
	for range choices {
		i = (i + step) % len(choices)
		if choices[i].Available {
			break
		}
	}
	return i
}

// endTalk returns to the map, or to the victory screen when the conversation completed the main quest.
func (g *Game) endTalk() {
	g.mode = modeField
	if g.won {
		g.showMenu(modeWon, pageWon)
	}
}

// onCommand runs a command of the dialogue script; dialogue.yarn lists them.
func (g *Game) onCommand(ev flow.DialogueEvent) {
	arg := func(i int) string {
		if i < len(ev.Args) {
			return ev.Args[i]
		}
		return ""
	}
	count := func() int {
		n, err := strconv.Atoi(arg(1))
		if err != nil || n <= 0 {
			return 1
		}
		return n
	}
	switch ev.Command {
	case "give":
		def, _ := g.c.items.Item(arg(0))
		if err := g.bag.AddAll(arg(0), count()); err != nil {
			g.say(fmt.Sprintf("Could not take %s: %v", arg(0), err))
			return
		}
		g.say("Received " + amount(def.Name, count()) + ".")
	case "take":
		if err := g.bag.Remove(arg(0), count()); err != nil {
			g.say(fmt.Sprintf("Could not hand over %s: %v", arg(0), err))
		}
	case "start":
		if err := g.quests.Start(arg(0)); err != nil {
			g.say(err.Error())
			return
		}
		g.syncQuest(arg(0))
	case "talked":
		g.quests.Handle(quest.Talked{Character: arg(0)}.Event())
	case "heal":
		g.hp = maxHP(&g.progress)
		g.say("HP fully restored.")
	default:
		g.say(fmt.Sprintf("%s: unknown command <<%s>>", dialogueFile, ev.Command))
	}
	g.rev++
}

// --- Quests ---

// syncQuest counts what the player did before the quest started: the items already in the bag and the foes already defeated.
func (g *Game) syncQuest(id string) {
	for _, o := range g.quests.Objectives(id) {
		n := 0
		switch o.Kind {
		case quest.KindCollect:
			n = g.bag.Count(o.Target)
		case quest.KindDefeat:
			for _, m := range g.c.world.Maps {
				for _, f := range m.Foes {
					if f.Kind == o.Target && g.defeated[f.ID] {
						n++
					}
				}
			}
		}
		if n > 0 {
			check(g.quests.SetProgress(id, o.ID, min(n, o.Required)))
		}
	}
}

// questReady reports whether every objective of the quest but talking to someone is done, so the one who gave it can take it back.
func (g *Game) questReady(id string) bool {
	if g.quests.State(id) != quest.Active {
		return false
	}
	for _, o := range g.quests.Objectives(id) {
		if o.Kind != quest.KindTalk && !o.Optional && o.State != quest.ObjectiveDone {
			return false
		}
	}
	return true
}

// onQuest reports the quest log's changes and grants a completed quest's rewards.
func (g *Game) onQuest(u quest.Update) {
	q := g.c.quests.Quest(u.Quest)
	switch u.Kind {
	case quest.QuestStarted:
		g.say("New quest: " + q.Title)
	case quest.ObjectiveCompleted:
		if o, ok := g.quests.Objective(u.Quest, u.Objective); ok && o.Kind != quest.KindTalk {
			g.say(o.Text + ": done")
		}
	case quest.QuestCompleted:
		for _, r := range u.Rewards {
			if r.Kind == "item" {
				if err := g.bag.AddAll(r.ID, r.Quantity()); err != nil {
					g.say(fmt.Sprintf("Your pack has no room for %s.", r.ID))
				}
			}
		}
		g.say("Quest complete: " + q.Title)
		if u.Quest == mainQuest {
			g.won = true
		}
	}
	g.rev++
}

// questTracker returns the main quest's current objective and its progress, or "" when the quest is not active.
func (g *Game) questTracker() string {
	if g.quests.State(mainQuest) != quest.Active {
		return ""
	}
	for _, o := range g.quests.Objectives(mainQuest) {
		if o.State != quest.ObjectiveDone && !o.Optional {
			if o.Required > 1 {
				return fmt.Sprintf("%s %d/%d", o.Text, o.Progress, o.Required)
			}
			return o.Text
		}
	}
	return ""
}

// --- Items and equipment ---

// applyGear gives p's stats the effect of each item it has equipped, from stats.yaml's effect of the same name, and keeps its HP within the new maximum.
func applyGear(c *content, p *progress) {
	for _, slot := range p.gear.Slots() {
		p.sheet.RemoveSource(slot.Name)
		if item, ok := p.gear.Equipped(slot.Name); ok {
			e, _ := c.stats.Effect(item)
			must(p.sheet.Apply(slot.Name, e))
		}
	}
	p.hp = min(p.hp, maxHP(p))
}

// maxHP, attack and defense return p's stats with its equipment.
func maxHP(p *progress) int   { return int(p.sheet.Value(statMaxHP)) }
func attack(p *progress) int  { return int(p.sheet.Value(statAttack)) }
func defense(p *progress) int { return int(p.sheet.Value(statDefense)) }

// owns reports whether the player carries or wears item.
func (g *Game) owns(item string) bool {
	if g.bag.Count(item) > 0 {
		return true
	}
	for _, s := range g.gear.Slots() {
		if e, ok := g.gear.Equipped(s.Name); ok && e == item {
			return true
		}
	}
	return false
}

// equip puts on the item in bag slot i, returning what it replaces to the bag.
func (g *Game) equip(i int) {
	def, _ := g.c.items.Item(g.bag.Slot(i).Item)
	if err := g.gear.Equip(def.Equip, g.bag, i); err != nil {
		g.say(err.Error())
		return
	}
	applyGear(g.c, &g.progress)
	g.say("Equipped the " + def.Name + ".")
}

// drinkPotion restores HP from a potion, which in battle takes the player's turn.
func (g *Game) drinkPotion() {
	def, _ := g.c.items.Item(itemPotion)
	if err := g.bag.Remove(itemPotion, 1); err != nil {
		g.say("You have no potions.")
		return
	}
	heal := int(def.Stats["heal"])
	g.hp = min(g.hp+heal, maxHP(&g.progress))
	if b := g.battle; b != nil {
		b.say(fmt.Sprintf("You drink a potion and recover %d HP.", heal))
		g.foeTurn()
		return
	}
	g.say(fmt.Sprintf("Recovered %d HP.", heal))
}

// --- Battles ---

// startBattle starts a fight with f. A step the player is taking is cut short and finished after the battle (finishStep).
func (g *Game) startBattle(f *foeState) {
	if g.walk > 0 {
		g.walk, g.arriving = 0, true
	}
	g.battle = &battle{foe: f, hp: f.kind.HP}
	g.battle.say(fmt.Sprintf("%s blocks your way!", f.kind.Name))
	g.showMenu(modeBattle, pageBattle)
}

// say adds a line to the battle's log.
func (b *battle) say(line string) {
	b.log = append(b.log, line)
	if len(b.log) > 3 {
		b.log = b.log[len(b.log)-3:]
	}
}

// damage is what a blow deals: the attacker's attack less the defender's defense, at least 1.
func damage(attack, defense int) int {
	return max(1, attack-defense)
}

// attackFoe strikes the foe, which strikes back if it still stands.
func (g *Game) attackFoe() {
	b := g.battle
	kind := b.foe.kind
	n := damage(attack(&g.progress), kind.Defense)
	b.hp = max(b.hp-n, 0)
	b.hit = flashTime
	b.say(fmt.Sprintf("You hit %s for %d.", kind.Name, n))
	if b.hp > 0 {
		g.foeTurn()
		return
	}
	g.defeated[b.foe.ID] = true
	g.foes = slices.DeleteFunc(g.foes, func(f *foeState) bool { return f == b.foe })
	won := kind.Name + " is defeated!"
	if kind.Coins > 0 {
		check(g.bag.AddAll(itemCoin, kind.Coins))
		won += fmt.Sprintf(" You find %d coins.", kind.Coins)
	}
	g.battle = nil
	g.mode = modeField
	g.say(won)
	g.quests.Handle(quest.Defeated{Enemy: kind.ID, Amount: 1}.Event())
	g.finishStep()
}

// finishStep completes the step a battle cut short, once the player is back on the map.
func (g *Game) finishStep() {
	if g.arriving {
		g.arriving = false
		g.arrive()
	}
}

// foeTurn lets the foe strike, ending the game when the player's HP runs out.
func (g *Game) foeTurn() {
	b := g.battle
	n := damage(b.foe.kind.Attack, defense(&g.progress))
	g.hp = max(g.hp-n, 0)
	b.hurt = flashTime
	b.say(fmt.Sprintf("%s hits you for %d.", b.foe.kind.Name, n))
	if g.hp == 0 {
		g.battle, g.arriving = nil, false
		g.showMenu(modeLost, pageLost)
	}
}

// flee leaves a battle with any foe but a boss, which keeps the player from leaving.
func (g *Game) flee() {
	if g.battle.foe.kind.Boss {
		return
	}
	g.battle = nil
	g.mode = modeField
	g.grace = fleeGrace
	g.say("You got away.")
	g.finishStep()
}

// say shows a message over the game for a while.
func (g *Game) say(text string) {
	g.message, g.messageLeft = text, messageTime
}

// amount names n of an item: "a Potion", "an Iron Sword" or "3 Coin".
func amount(name string, n int) string {
	if n == 1 {
		if name != "" && strings.ContainsRune("AEIOUaeiou", rune(name[0])) {
			return "an " + name
		}
		return "a " + name
	}
	return fmt.Sprintf("%d %s", n, name)
}

// must returns v and panics on err: the data files were checked as the game loaded, so an error here is a bug in the game's code.
func must[T any](v T, err error) T {
	check(err)
	return v
}

// check panics on err, as must does.
func check(err error) {
	if err != nil {
		panic(err)
	}
}

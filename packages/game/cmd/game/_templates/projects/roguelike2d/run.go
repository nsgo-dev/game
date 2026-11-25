package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/ability"
	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/loot"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/game/packages/game/stats"
)

// outcome is how a run stands.
type outcome int

const (
	playing outcome = iota
	won
	lost
)

func (o outcome) String() string {
	switch o {
	case won:
		return "won"
	case lost:
		return "lost"
	}
	return "playing"
}

// sightRadius is how far, in cells, the player sees.
const sightRadius = 7

// logLength is how many messages the run keeps.
const logLength = 4

// The stats sources the run applies item effects from, one per equipment slot; a tonic's source is tonicSource plus its effect's name, so drinking another of the same tonic restarts it.
const (
	sourceWeapon = "weapon"
	sourceArmor  = "armor"
	tonicSource  = "tonic:"
)

// action is what the player does with one turn.
type action struct {
	kind actionKind
	// dir is the step a move takes.
	dir cell
	// ability is the id in data/abilities.yaml an actAbility uses.
	ability string
}

type actionKind int

const (
	actMove actionKind = iota
	actWait
	// actAbility uses the ability the action names, which takes a turn when the ability activates.
	actAbility
)

// run is one descent through the dungeon, from the first floor to a win or a death. It knows nothing about devices or drawing: the game turns input into actions, calls act once per action and draws what it holds. Each turn is the player's action and then the monsters', which the systems of monsterTurn play on the floor's world of actors, so the same seed and the same actions always play out the same way.
type run struct {
	data *gameData
	seed uint64

	floor *floor
	pos   cell

	sheet    *stats.Sheet
	health   int
	level    int
	xp       int
	draughts int
	shards   int
	kills    int
	turns    int
	weapon   *ItemDef
	armor    *ItemDef

	caster *ability.Caster
	roller *loot.Roller
	combat *random.Stream
	// monsterTurn runs the systems that play the monsters' part of each turn.
	monsterTurn *entities.Scheduler

	dist    []int
	visible []bool
	log     []string

	state outcome
	// killer names what ended a lost run.
	killer string
	// persist, when set, is called with the run's save as the player arrives on each floor and after every turn that leaves the run in play; the game writes it to the run slot, so quitting and coming back resumes the run exactly where it stood. An error it returns is shown in the log, once until a save succeeds again.
	persist   func(runSave) error
	saveError bool
}

// newRun starts a run with seed on the first floor.
func newRun(data *gameData, seed uint64, persist func(runSave) error) (*run, error) {
	r, err := newRunState(data, seed, persist)
	if err != nil {
		return nil, err
	}
	w, _ := data.dungeon.item(data.dungeon.Player.Weapon)
	r.equip(w)
	r.health = r.maxHealth()
	r.draughts = data.dungeon.Player.Draughts
	r.say(fmt.Sprintf("You enter %s.", data.dungeon.Name))
	if err := r.enterFloor(0); err != nil {
		return nil, err
	}
	return r, nil
}

// newRunState returns a run with seed at level 1 and no floor, with its sheet, caster, loot roller and monster systems made from data.
func newRunState(data *gameData, seed uint64, persist func(runSave) error) (*run, error) {
	sheet, err := data.stats.NewSheet()
	if err != nil {
		return nil, err
	}
	// The caster takes one step a turn, so the durations in data/abilities.yaml count turns.
	caster, err := ability.NewCaster(data.abilities, 1)
	if err != nil {
		return nil, err
	}
	r := &run{data: data, seed: seed, sheet: sheet, caster: caster, level: 1, persist: persist}
	r.monsterTurn = r.newMonsterTurn()
	r.roller = loot.NewRoller(data.loot, floorStream(seed, 0, "loot"))
	caster.Resources = draughtPool{r}
	caster.Condition = r.condition
	caster.OnEvent = r.abilityEvent
	return r, nil
}

// enterFloor puts the player on floor index, generated from the run's seed, and saves the run.
func (r *run) enterFloor(index int) error {
	pity := r.roller.State()
	r.roller = loot.NewRoller(r.data.loot, floorStream(r.seed, index, "loot"))
	if err := r.roller.SetState(pity); err != nil {
		return err
	}
	f, err := generateFloor(r.data.dungeon, index, r.seed, r.roller)
	if err != nil {
		return err
	}
	r.floor = f
	r.pos = f.start
	r.combat = floorStream(r.seed, index, "combat")
	r.visible = make([]bool, len(f.grid.Cells))
	r.look()
	r.say(fmt.Sprintf("Floor %d: %s.", index+1, f.def.Name))
	r.persistNow()
	return nil
}

// persistNow hands the run's save to persist.
func (r *run) persistNow() {
	if r.persist == nil {
		return
	}
	err := r.persist(r.save())
	if err != nil && !r.saveError {
		r.say("The run could not be saved: " + err.Error())
	}
	r.saveError = err != nil
}

// act plays one action and reports whether it took a turn. A step into a wall and an ability that cannot be used take none.
func (r *run) act(a action) bool {
	if r.state != playing {
		return false
	}
	switch a.kind {
	case actWait:
	case actAbility:
		if reason := r.caster.TryActivate(a.ability); reason != ability.ReasonOK {
			r.say(refusal(a.ability, reason))
			return false
		}
	default:
		to := cell{X: r.pos.X + a.dir.X, Y: r.pos.Y + a.dir.Y}
		if e, ok := r.floor.foeAt(to); ok {
			r.strike(e, r.attack())
			break
		}
		if !r.floor.walkable(to) {
			return false
		}
		r.pos = to
		r.pickUp()
		if to == r.floor.exit {
			if r.floor.relic {
				r.state = won
				r.say("You lift the Heart Ember. The deep lets you go.")
				r.turns++
				return true
			}
			r.turns++
			r.tick()
			if err := r.enterFloor(r.floor.index + 1); err != nil {
				// The tests generate every floor of many seeds, so this only happens after an edit they would have caught.
				panic(err)
			}
			return true
		}
	}
	r.turns++
	r.monstersAct()
	if r.state == playing {
		r.tick()
	}
	r.look()
	if r.state == playing {
		r.persistNow()
	}
	return true
}

// tick advances the timed stats effects and the ability cooldowns by one turn, and recovers 1 health every RegenTurns turns.
func (r *run) tick() {
	r.sheet.Step()
	r.caster.Update(nil)
	if n := r.data.dungeon.Player.RegenTurns; n > 0 && r.turns%n == 0 {
		r.health++
	}
	r.health = min(r.health, r.maxHealth())
}

// look recomputes what the player sees and the walking distances the monsters hunt by.
func (r *run) look() {
	visibleFrom(r.floor.grid, r.pos, sightRadius, r.visible)
	for i, v := range r.visible {
		if v {
			r.floor.seen[i] = true
		}
	}
	r.dist = distances(r.floor.grid, r.pos)
}

// monstersAct plays the monsters' part of a turn: the systems of monsterTurn, run on the floor's world with the distance map from where the player now stands.
func (r *run) monstersAct() {
	r.dist = distances(r.floor.grid, r.pos)
	if err := r.monsterTurn.Step(r.floor.actors, time.Second); err != nil {
		panic(err)
	}
}

// distAt returns the steps from the player to c, or -1.
func (r *run) distAt(c cell) int {
	if !r.floor.grid.In(c.X, c.Y) {
		return -1
	}
	return r.dist[c.Y*r.floor.grid.Width+c.X]
}

// damage rolls a blow of attack against defense: attack plus 0 to 2, less defense, and never below 1.
func (r *run) damage(attack, defense int) int {
	return max(1, attack+r.combat.IntRange(0, 2)-defense)
}

// strike hits the monster e with attack, and defeats it when its health runs out.
func (r *run) strike(e entities.Entity, attack int) {
	foe := r.floor.foe(e)
	def := r.foeDef(foe)
	n := r.damage(attack, def.Defense)
	foe.Health -= n
	if foe.Health > 0 {
		r.say(fmt.Sprintf("You hit the %s for %d.", def.Name, n))
		return
	}
	r.say(fmt.Sprintf("You defeat the %s.", def.Name))
	r.kills++
	at := r.floor.at(e)
	r.floor.actors.Destroy(e)
	if def.Drops != "" {
		drops, err := r.roller.Roll(def.Drops)
		if err != nil {
			panic(err)
		}
		for _, dr := range drops {
			r.floor.spawnLoot(dr.Item, dr.Count, at)
		}
	}
	r.gainXP(def.XP)
}

// hurt has the monster e strike the player, which ends the run when the player's health runs out.
func (r *run) hurt(e entities.Entity) {
	def := r.foeDef(r.floor.foe(e))
	n := r.damage(def.Attack, r.defense())
	r.health -= n
	if r.health > 0 {
		r.say(fmt.Sprintf("The %s hits you for %d.", def.Name, n))
		return
	}
	r.health = 0
	r.state = lost
	r.killer = def.Name
	r.say(fmt.Sprintf("The %s strikes you down.", def.Name))
}

// gainXP adds xp and raises the player's level for every LevelXP times the current level it reaches, raising the maxHealth and attack bases and healing by the level's health.
func (r *run) gainXP(xp int) {
	p := r.data.dungeon.Player
	r.xp += xp
	for r.xp >= r.level*p.LevelXP {
		r.xp -= r.level * p.LevelXP
		r.level++
		r.applyLevel()
		r.health += p.LevelHealth
		r.say(fmt.Sprintf("You reach level %d.", r.level))
	}
}

// applyLevel sets the maxHealth and attack bases for the player's level.
func (r *run) applyLevel() {
	p := r.data.dungeon.Player
	for _, s := range []struct {
		stat string
		per  int
	}{{statMaxHealth, p.LevelHealth}, {statAttack, p.LevelAttack}} {
		def := r.baseOf(s.stat)
		if err := r.sheet.SetBase(s.stat, def+float64((r.level-1)*s.per)); err != nil {
			panic(err)
		}
	}
}

// baseOf returns the base data/stats.yaml gives stat.
func (r *run) baseOf(stat string) float64 {
	for _, d := range r.data.stats.Stats {
		if d.Name == stat {
			return d.Base
		}
	}
	return 0
}

// pickUp takes every item on the player's cell, in the order floor.loot gives.
func (r *run) pickUp() {
	for _, e := range r.floor.loot() {
		if r.floor.at(e) != r.pos {
			continue
		}
		it := r.floor.lootOf(e)
		def, _ := r.data.dungeon.item(it.Item)
		r.take(def, it.Count)
		r.floor.actors.Destroy(e)
	}
}

// take applies count of an item the player picked up.
func (r *run) take(it *ItemDef, count int) {
	switch it.Kind {
	case kindWeapon, kindArmor:
		held := r.weapon
		if it.Kind == kindArmor {
			held = r.armor
		}
		if held != nil && held.Rank >= it.Rank {
			r.say(fmt.Sprintf("You leave the %s; your %s is as good.", it.Name, held.Name))
			return
		}
		r.equip(it)
		r.say(fmt.Sprintf("You take up the %s.", it.Name))
	case kindDraught:
		r.draughts += count
		r.say(fmt.Sprintf("You find %s.", plural(count, "draught")))
	case kindTonic:
		e, _ := r.data.stats.Effect(it.Effect)
		if _, err := r.sheet.Refresh(tonicSource+e.Name, e); err != nil {
			panic(err)
		}
		r.say(fmt.Sprintf("You drink the %s.", it.Name))
	case kindShards:
		r.shards += count
		r.say(fmt.Sprintf("You gather %s.", plural(count, "shard")))
	}
}

// equip puts a weapon or armor in its slot, replacing the bonus of the one it held.
func (r *run) equip(it *ItemDef) {
	source := sourceWeapon
	if it.Kind == kindArmor {
		source = sourceArmor
		r.armor = it
	} else {
		r.weapon = it
	}
	e, _ := r.data.stats.Effect(it.Effect)
	if _, err := r.sheet.Refresh(source, e); err != nil {
		panic(err)
	}
}

// condition answers the conditions data/abilities.yaml names.
func (r *run) condition(name string) bool {
	switch name {
	case conditionHurt:
		return r.health < r.maxHealth()
	case conditionFoes:
		return len(r.adjacent()) > 0
	}
	return false
}

// abilityEvent applies an ability's effect when it activates.
func (r *run) abilityEvent(ev ability.Event) {
	if ev.Kind != ability.EventActivated {
		return
	}
	switch ev.Ability {
	case abilityQuaff:
		heal := min(r.data.dungeon.Player.DraughtHeal, r.maxHealth()-r.health)
		r.health += heal
		r.say(fmt.Sprintf("You quaff a draught and recover %d.", heal))
	case abilityCleave:
		foes := r.adjacent()
		r.say(fmt.Sprintf("You cleave %s.", plural(len(foes), "foe")))
		for _, e := range foes {
			r.strike(e, r.attack())
		}
	}
}

// adjacent returns the monsters on the eight cells around the player, in the order floor.foes gives.
func (r *run) adjacent() []entities.Entity {
	var out []entities.Entity
	for _, e := range r.floor.foes() {
		if at := r.floor.at(e); abs(at.X-r.pos.X) <= 1 && abs(at.Y-r.pos.Y) <= 1 {
			out = append(out, e)
		}
	}
	return out
}

func (r *run) maxHealth() int { return int(r.sheet.Value(statMaxHealth)) }
func (r *run) attack() int    { return int(r.sheet.Value(statAttack)) }
func (r *run) defense() int   { return int(r.sheet.Value(statDefense)) }

// say adds a message to the log, dropping the oldest past logLength.
func (r *run) say(msg string) {
	r.log = append(r.log, msg)
	if len(r.log) > logLength {
		r.log = append(r.log[:0], r.log[len(r.log)-logLength:]...)
	}
}

// lastMessage returns the newest message.
func (r *run) lastMessage() string {
	if len(r.log) == 0 {
		return ""
	}
	return r.log[len(r.log)-1]
}

// draughtPool is the Resources the quaff ability spends: the run's draughts.
type draughtPool struct{ r *run }

func (p draughtPool) Amount(resource string) float64 {
	if resource == resourceDraughts {
		return float64(p.r.draughts)
	}
	return 0
}

func (p draughtPool) Spend(resource string, amount float64) {
	if resource == resourceDraughts {
		p.r.draughts -= int(amount)
	}
}

// refusal says why ability id could not be used.
func refusal(id string, reason ability.Reason) string {
	switch {
	case reason == ability.ReasonCooldown && id == abilityCleave:
		return "You are still recovering from your last cleave."
	case reason == ability.ReasonCooldown:
		return "You need a moment before another draught."
	case reason == ability.ReasonCost:
		return "You have no draughts left."
	case reason == ability.ReasonCondition && id == abilityCleave:
		return "There is nothing next to you to cleave."
	case reason == ability.ReasonCondition:
		return "You are not hurt."
	}
	return fmt.Sprintf("You cannot %s now (%s).", strings.ToLower(id), reason)
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

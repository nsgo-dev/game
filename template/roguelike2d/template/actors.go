package main

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/entities"
)

// The components of the actors on a floor. Every monster and every item lying on the floor is an entity in the floor's entities.World, and what it is comes from the components it carries: a monster has a Position and a Foe, a guardian a Guard as well, and an item a Position and a Loot. The player is not an entity, because the run holds everything about the player and the rules treat the player apart. Each component names its kind by the id data/dungeon.yaml gives it, so the run slot saves the world as readable JSON with entities.World.Snapshot.
type (
	// Position is the cell an actor stands on.
	Position struct {
		X int `json:"x"`
		Y int `json:"y"`
	}

	// Foe is a monster: its kind, the health it has left and whether it has noticed the player.
	Foe struct {
		// Kind is the monster's id in data/dungeon.yaml.
		Kind   string `json:"kind"`
		Health int    `json:"health"`
		// Hunting is set once the monster has noticed the player; it then chases the player for the rest of the floor.
		Hunting bool `json:"hunting,omitempty"`
	}

	// Guard keeps a guardian on its cell: it strikes whoever stands next to it but never leaves the exit it guards.
	Guard struct{}

	// Loot is an item lying on the floor, Count of it.
	Loot struct {
		// Item is the item's id in data/dungeon.yaml.
		Item  string `json:"item"`
		Count int    `json:"count"`
	}
)

// The names the run slot gives the components, as entities.RegisterCodec takes them.
const (
	codecPosition = "position"
	codecFoe      = "foe"
	codecGuard    = "guard"
	codecLoot     = "loot"
)

// The systems a monster turn runs, in the order the scheduler puts them in.
const (
	systemNotice = "notice"
	systemHunt   = "hunt"
)

func (p Position) cell() cell { return cell{X: p.X, Y: p.Y} }

func positionOf(c cell) Position { return Position{X: c.X, Y: c.Y} }

// newActors returns an empty world with a codec for every component, so it can be saved and restored.
func newActors() *entities.World {
	w := entities.NewWorld()
	for _, err := range []error{
		entities.RegisterCodec[Position](w, codecPosition),
		entities.RegisterCodec[Foe](w, codecFoe),
		entities.RegisterCodec[Guard](w, codecGuard),
		entities.RegisterCodec[Loot](w, codecLoot),
	} {
		if err != nil {
			// The names above are distinct and fixed, so this only happens after an edit the tests would have caught.
			panic(err)
		}
	}
	return w
}

// spawnFoe puts a monster of def on at, a guardian when guard is set.
func (f *floor) spawnFoe(def *MonsterDef, at cell, guard bool) entities.Entity {
	e := f.actors.Spawn()
	entities.Set(f.actors, e, positionOf(at))
	entities.Set(f.actors, e, Foe{Kind: def.ID, Health: def.Health})
	if guard {
		entities.Set(f.actors, e, Guard{})
	}
	return e
}

// spawnLoot lays count of item on at.
func (f *floor) spawnLoot(item string, count int, at cell) entities.Entity {
	e := f.actors.Spawn()
	entities.Set(f.actors, e, positionOf(at))
	entities.Set(f.actors, e, Loot{Item: item, Count: count})
	return e
}

// foes returns the monsters in the order they take their turns: the order they were spawned in, which is the order of their slots in the world. Monsters never spawn after a floor is generated, so a restored world gives the same order.
func (f *floor) foes() []entities.Entity {
	var out []entities.Entity
	f.actors.Each(func(e entities.Entity) bool {
		if entities.Has[Foe](f.actors, e) {
			out = append(out, e)
		}
		return true
	})
	return out
}

// loot returns the items on the floor, ordered by cell, then id, then count, so the order never depends on which slots the world reused for drops.
func (f *floor) loot() []entities.Entity {
	var out []entities.Entity
	entities.Each2(f.actors, func(e entities.Entity, _ *Position, _ *Loot) bool {
		out = append(out, e)
		return true
	})
	slices.SortStableFunc(out, func(a, b entities.Entity) int {
		pa, pb := f.at(a), f.at(b)
		la, lb := f.lootOf(a), f.lootOf(b)
		return cmp.Or(cmp.Compare(pa.Y, pb.Y), cmp.Compare(pa.X, pb.X), cmp.Compare(la.Item, lb.Item), cmp.Compare(la.Count, lb.Count))
	})
	return out
}

// foeAt returns the monster on c.
func (f *floor) foeAt(c cell) (entities.Entity, bool) {
	var found entities.Entity
	entities.Each2(f.actors, func(e entities.Entity, p *Position, _ *Foe) bool {
		if p.cell() == c {
			found = e
			return false
		}
		return true
	})
	return found, !found.IsZero()
}

// at returns the cell e stands on.
func (f *floor) at(e entities.Entity) cell {
	p, _ := entities.Get[Position](f.actors, e)
	return p.cell()
}

// foe returns e's Foe, which the caller knows it has.
func (f *floor) foe(e entities.Entity) *Foe {
	v, _ := entities.Get[Foe](f.actors, e)
	return v
}

// lootOf returns e's Loot, which the caller knows it has.
func (f *floor) lootOf(e entities.Entity) *Loot {
	v, _ := entities.Get[Loot](f.actors, e)
	return v
}

// guards reports whether e is a guardian.
func (f *floor) guards(e entities.Entity) bool { return entities.Has[Guard](f.actors, e) }

// checkActors reports the first actor of a restored world that the rules could not play: one off the floor's cells, of a kind data/dungeon.yaml does not define, with health or a count out of range, or with neither a Foe nor a Loot.
func (f *floor) checkActors(d *Dungeon) error {
	var err error
	f.actors.Each(func(e entities.Entity) bool {
		p, ok := entities.Get[Position](f.actors, e)
		if !ok || !f.walkable(p.cell()) {
			err = fmt.Errorf("actor %s is not on a floor cell", e)
			return false
		}
		foe, isFoe := entities.Get[Foe](f.actors, e)
		it, isLoot := entities.Get[Loot](f.actors, e)
		switch {
		case isFoe == isLoot:
			err = fmt.Errorf("actor %s is not one monster or one item", e)
		case isFoe:
			def, ok := d.monster(foe.Kind)
			if !ok || foe.Health < 1 || foe.Health > def.Health {
				err = fmt.Errorf("actor %s: monster %q with %d health", e, foe.Kind, foe.Health)
			}
		default:
			if _, ok := d.item(it.Item); !ok || it.Count < 1 || it.Count > maxSaved || f.guards(e) {
				err = fmt.Errorf("actor %s: item %q, %d of it", e, it.Item, it.Count)
			}
		}
		return err == nil
	})
	return err
}

// newMonsterTurn returns the scheduler that plays the monsters' turn on a floor's world: notice, where a monster within its sense of the player starts to hunt, and then hunt, where a hunting monster next to the player strikes and one further away steps down the distance map toward the player. A new system, such as one that lets monsters flee or heal, is one more AddSystem with Before or After.
func (r *run) newMonsterTurn() *entities.Scheduler {
	s := entities.NewScheduler()
	for _, err := range []error{
		s.AddSystem(systemNotice, func(w *entities.World, _ time.Duration) { r.notice(w) }),
		s.AddSystem(systemHunt, func(w *entities.World, _ time.Duration) { r.hunt(w) }, entities.After(systemNotice)),
		s.Build(),
	} {
		if err != nil {
			panic(err)
		}
	}
	return s
}

// notice sets every monster hunting that is within its sense of the player.
func (r *run) notice(w *entities.World) {
	entities.Each2(w, func(_ entities.Entity, p *Position, foe *Foe) bool {
		if d := r.distAt(p.cell()); d >= 0 && d <= r.foeDef(foe).Sense {
			foe.Hunting = true
		}
		return true
	})
}

// hunt gives every hunting monster its turn, in the order foes returns.
func (r *run) hunt(w *entities.World) {
	for _, e := range r.floor.foes() {
		if r.state != playing {
			return
		}
		foe := r.floor.foe(e)
		at := r.floor.at(e)
		d := r.distAt(at)
		if !foe.Hunting || d < 0 {
			continue
		}
		if d == 1 {
			r.hurt(e)
			continue
		}
		if r.floor.guards(e) {
			continue
		}
		best := at
		for _, st := range steps {
			n := cell{X: at.X + st.X, Y: at.Y + st.Y}
			nd := r.distAt(n)
			if _, taken := r.floor.foeAt(n); nd >= 0 && nd < r.distAt(best) && !taken && n != r.pos {
				best = n
			}
		}
		entities.Set(w, e, positionOf(best))
	}
}

// foeDef returns the definition of foe's kind, which the floor's generation and the run slot's check guarantee exists.
func (r *run) foeDef(foe *Foe) *MonsterDef {
	def, _ := r.data.dungeon.monster(foe.Kind)
	return def
}

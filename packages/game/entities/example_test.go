package entities_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game/entities"
)

type Pos struct{ X, Y float32 }

type Vel struct{ X, Y float32 }

type Hitpoints struct{ HP int }

// A World holds entities and their components; Each2 visits every entity that has both component types, in a fixed order.
func ExampleWorld() {
	w := entities.NewWorld()
	defer w.Dispose()

	player := w.Spawn()
	entities.Set(w, player, Pos{X: 0, Y: 0})
	entities.Set(w, player, Vel{X: 2, Y: 1})
	rock := w.Spawn()
	entities.Set(w, rock, Pos{X: 5, Y: 5})

	entities.Each2(w, func(e entities.Entity, p *Pos, v *Vel) bool {
		p.X += v.X
		p.Y += v.Y
		return true
	})
	p, _ := entities.Get[Pos](w, player)
	fmt.Println(player, *p)
	fmt.Println(entities.Has[Vel](w, rock), w.Count())

	w.Destroy(rock)
	again := w.Spawn()
	fmt.Println(w.Alive(rock), again.Index() == rock.Index(), again.Generation())
	// Output:
	// Entity(0:1) {2 1}
	// false 2
	// false true 2
}

// A Prefab spawns entities with a preset set of components.
func ExamplePrefab() {
	w := entities.NewWorld()
	defer w.Dispose()

	slime := entities.With(entities.NewPrefab("slime"), Hitpoints{HP: 3})
	entities.With(slime, Vel{X: -1})
	for _, e := range slime.SpawnN(w, 3) {
		h, _ := entities.Get[Hitpoints](w, e)
		fmt.Println(e, h.HP)
	}
	fmt.Println(entities.Component[Hitpoints](w).Len())
	// Output:
	// Entity(0:1) 3
	// Entity(1:1) 3
	// Entity(2:1) 3
	// 3
}

// A Scheduler runs systems in an order derived from their Before and After constraints.
func ExampleScheduler() {
	w := entities.NewWorld()
	defer w.Dispose()

	s := entities.NewScheduler()
	s.AddSystem("render", func(*entities.World, time.Duration) {})
	s.AddSystem("move", func(*entities.World, time.Duration) {}, entities.Before("collide"))
	s.AddSystem("collide", func(*entities.World, time.Duration) {}, entities.Before("render"))
	if err := s.Step(w, time.Second/60); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(s)
	// Output:
	// move -> collide -> render
}

// Hierarchy2D parents entities so a child's world transform follows its parent's.
func ExampleHierarchy2D() {
	w := entities.NewWorld()
	defer w.Dispose()
	h := w.Hierarchy2D()

	ship := w.Spawn()
	h.SetLocal(ship, entities.NewTransform2D(100, 50))
	turret := w.Spawn()
	h.SetLocal(turret, entities.NewTransform2D(10, 0))
	if err := h.SetParent(turret, ship, false); err != nil {
		fmt.Println(err)
		return
	}
	h.SetScale(ship, 2, 2)
	h.Sync()
	wt, _ := h.WorldTransform(turret)
	fmt.Println(wt.X, wt.Y, h.ChildCount(ship))

	h.SetVisible(ship, false)
	h.Sync()
	wt, _ = h.WorldTransform(turret)
	fmt.Println(wt.WorldVisible)
	// Output:
	// 120 50 1
	// false
}

// Snapshot captures the components that have a registered codec, and Restore rebuilds a World from it, as SaveWorld and LoadWorld do through a save.Store.
func ExampleWorld_Snapshot() {
	w := entities.NewWorld()
	defer w.Dispose()
	entities.RegisterCodec[Hitpoints](w, "health")
	e := w.Spawn()
	entities.Set(w, e, Hitpoints{HP: 7})

	snap, err := w.Snapshot()
	if err != nil {
		fmt.Println(err)
		return
	}
	loaded := entities.NewWorld()
	defer loaded.Dispose()
	entities.RegisterCodec[Hitpoints](loaded, "health")
	if err := loaded.Restore(snap); err != nil {
		fmt.Println(err)
		return
	}
	entities.Each1(loaded, func(e entities.Entity, h *Hitpoints) bool {
		fmt.Println(e, h.HP)
		return true
	})
	// Output:
	// Entity(0:1) 7
}

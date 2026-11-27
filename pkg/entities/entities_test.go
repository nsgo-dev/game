package entities_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Velocity struct {
	DX float64 `json:"dx"`
	DY float64 `json:"dy"`
}

type Health struct {
	Current int `json:"current"`
	Max     int `json:"max"`
}

type Tag struct {
	Label string `json:"label"`
}

type Inventory struct {
	Items []string       `json:"items"`
	Stats map[string]int `json:"stats,omitempty"`
}

type Parent struct {
	Target entities.Entity `json:"target"`
}

// transientRender is intentionally not registered as a codec.
type transientRender struct {
	Layer int
}

// --- #137: Entity model ---

func TestEntityLifecycleAndStaleHandleSafety(t *testing.T) {
	w := entities.NewWorld()

	if w.Count() != 0 {
		t.Fatalf("Count = %d, want 0", w.Count())
	}
	if w.Alive(entities.Zero) {
		t.Fatalf("Zero entity must never be alive")
	}
	if !entities.Zero.IsZero() || entities.Zero.String() != "Entity(0)" {
		t.Fatalf("unexpected Zero properties: IsZero=%v String=%q", entities.Zero.IsZero(), entities.Zero.String())
	}

	// Destroying Zero or an out-of-range entity is a no-op.
	w.Destroy(entities.Zero)
	w.Destroy(entities.Entity(1<<32 | 99))

	e1 := w.Spawn()
	e2 := w.Spawn()
	if e1.IsZero() || e2.IsZero() || e1 == e2 {
		t.Fatalf("invalid spawned handles: e1=%v e2=%v", e1, e2)
	}
	if e1.Index() != 0 || e1.Generation() != 1 || e1.String() != "Entity(0:1)" {
		t.Fatalf("unexpected e1: index=%d gen=%d str=%q", e1.Index(), e1.Generation(), e1.String())
	}
	if !w.Alive(e1) || !w.Alive(e2) || w.Count() != 2 {
		t.Fatalf("alive=(%v,%v) count=%d", w.Alive(e1), w.Alive(e2), w.Count())
	}

	entities.Set(w, e1, Position{X: 1, Y: 2})
	entities.Set(w, e2, Position{X: 3, Y: 4})

	// Destroy e1 and recycle its slot.
	w.Destroy(e1)
	if w.Alive(e1) || w.Count() != 1 {
		t.Fatalf("after Destroy(e1): alive=%v count=%d", w.Alive(e1), w.Count())
	}
	if entities.Has[Position](w, e1) {
		t.Fatalf("destroyed entity still has Position")
	}

	e3 := w.Spawn()
	if e3.Index() != e1.Index() {
		t.Fatalf("recycled slot index = %d, want %d", e3.Index(), e1.Index())
	}
	if e3.Generation() != e1.Generation()+1 {
		t.Fatalf("recycled slot generation = %d, want %d", e3.Generation(), e1.Generation()+1)
	}
	if e3 == e1 {
		t.Fatalf("recycled handle collided with stale handle %v", e1)
	}
	if w.Alive(e1) || !w.Alive(e3) {
		t.Fatalf("stale e1 alive=%v, fresh e3 alive=%v", w.Alive(e1), w.Alive(e3))
	}

	// Operations on stale e1 must not affect fresh e3 at the same slot.
	entities.Set(w, e3, Position{X: 10, Y: 20})
	entities.Set(w, e1, Position{X: 999, Y: 999})
	entities.Remove[Position](w, e1)
	w.Destroy(e1)

	if !w.Alive(e3) {
		t.Fatalf("Destroy on stale handle e1 killed recycled entity e3")
	}
	if _, ok := entities.Get[Position](w, e1); ok {
		t.Fatalf("Get on stale handle e1 returned recycled entity's component")
	}
	got, ok := entities.Get[Position](w, e3)
	if !ok || *got != (Position{X: 10, Y: 20}) {
		t.Fatalf("e3 Position = (%v, %v), want ({10 20}, true)", got, ok)
	}

	// Clear invalidates all existing handles and bumps generations.
	w.Clear()
	if w.Count() != 0 || w.Alive(e2) || w.Alive(e3) {
		t.Fatalf("after Clear: count=%d alive(e2)=%v alive(e3)=%v", w.Count(), w.Alive(e2), w.Alive(e3))
	}
	e4 := w.Spawn()
	if e4 == e1 || e4 == e3 || !w.Alive(e4) {
		t.Fatalf("after Clear Spawn returned colliding or dead handle %v", e4)
	}

	// Reset releases storage and leaves World usable.
	w.Reset()
	if w.Count() != 0 || w.Alive(e4) {
		t.Fatalf("after Reset: count=%d alive(e4)=%v", w.Count(), w.Alive(e4))
	}
	e5 := w.Spawn()
	if !w.Alive(e5) || w.Count() != 1 {
		t.Fatalf("World unusable after Reset: e5=%v count=%d", e5, w.Count())
	}
}

func TestWorldAttachDisposesWithScope(t *testing.T) {
	w := entities.NewWorld()
	var e entities.Entity
	var shown *core.Signal[bool]

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shown = core.NewSignal(cx, true)
		return core.Show{
			When: shown.Get,
			Then: func(cx *core.Scope) core.Node {
				w.Attach(cx)
				e = w.Spawn()
				entities.Set(w, e, Position{X: 5, Y: 6})
				return core.View{}
			},
		}
	})

	if w.Disposed() || !w.Alive(e) || w.Count() != 1 {
		t.Fatalf("before scope disposal: disposed=%v alive=%v count=%d", w.Disposed(), w.Alive(e), w.Count())
	}

	app.Post(func() { shown.Set(false) })
	app.Settle()

	if !w.Disposed() || w.Alive(e) || w.Count() != 0 {
		t.Fatalf("after scope disposal: disposed=%v alive=%v count=%d", w.Disposed(), w.Alive(e), w.Count())
	}
	if spawned := w.Spawn(); !spawned.IsZero() {
		t.Fatalf("Spawn on disposed world returned %v, want Zero", spawned)
	}
	if _, err := w.Snapshot(); !errors.Is(err, entities.ErrWorldDisposed) {
		t.Fatalf("Snapshot on disposed world error = %v, want ErrWorldDisposed", err)
	}
	// Dispose is idempotent.
	w.Dispose()
}

func TestSpawnDestroy100kSteadyStateZeroAllocs(t *testing.T) {
	const count = 100_000
	w := entities.NewWorld()
	buf := make([]entities.Entity, count)

	// Warm up entity slots and free list to 100k capacity.
	for i := 0; i < count; i++ {
		e := w.Spawn()
		entities.Set(w, e, Position{X: float64(i), Y: float64(i)})
		buf[i] = e
	}
	for i := 0; i < count; i++ {
		w.Destroy(buf[i])
	}
	if w.Count() != 0 {
		t.Fatalf("Count after warmup = %d, want 0", w.Count())
	}

	// Steady-state 100k spawn + destroy (without components): 0 allocs/op.
	allocs := testing.AllocsPerRun(5, func() {
		for i := 0; i < count; i++ {
			buf[i] = w.Spawn()
		}
		for i := 0; i < count; i++ {
			w.Destroy(buf[i])
		}
	})
	if allocs != 0 {
		t.Fatalf("100k steady-state Spawn+Destroy allocated %.1f times/run, want 0", allocs)
	}

	// Steady-state 100k spawn + Set + destroy: 0 allocs/op.
	posStore := entities.Component[Position](w)
	allocsWithComp := testing.AllocsPerRun(3, func() {
		for i := 0; i < count; i++ {
			e := w.Spawn()
			posStore.Set(e, Position{X: float64(i), Y: float64(i)})
			buf[i] = e
		}
		for i := 0; i < count; i++ {
			w.Destroy(buf[i])
		}
	})
	if allocsWithComp != 0 {
		t.Fatalf("100k steady-state Spawn+Set+Destroy allocated %.1f times/run, want 0", allocsWithComp)
	}
}

// --- #138: Components and queries ---

func TestComponentsAndQueriesExactDeterministicZeroAllocs(t *testing.T) {
	w := entities.NewWorld()

	// Spawn 6 entities with varied component combinations.
	e0 := w.Spawn() // Position only
	e1 := w.Spawn() // Position + Velocity
	e2 := w.Spawn() // Velocity only
	e3 := w.Spawn() // Position + Velocity + Health
	e4 := w.Spawn() // Position + Health
	e5 := w.Spawn() // Position + Velocity + Health (plus Inventory non-comparable)

	entities.Set(w, e0, Position{X: 0})
	entities.Set(w, e1, Position{X: 1})
	entities.Set(w, e1, Velocity{DX: 10})
	entities.Set(w, e2, Velocity{DX: 20})
	entities.Set(w, e3, Position{X: 3})
	entities.Set(w, e3, Velocity{DX: 30})
	entities.Set(w, e3, Health{Current: 30, Max: 100})
	entities.Set(w, e4, Position{X: 4})
	entities.Set(w, e4, Health{Current: 40, Max: 100})
	entities.Set(w, e5, Position{X: 5})
	entities.Set(w, e5, Velocity{DX: 50})
	entities.Set(w, e5, Health{Current: 50, Max: 100})
	entities.Set(w, e5, Inventory{Items: []string{"key"}, Stats: map[string]int{"atk": 5}})

	// Overwriting a component updates in place without duplicating.
	entities.Set(w, e1, Position{X: 11})
	if entities.Component[Position](w).Len() != 5 {
		t.Fatalf("Position Len = %d, want 5", entities.Component[Position](w).Len())
	}

	// Non-comparable component works cleanly without reflection.
	inv, ok := entities.Get[Inventory](w, e5)
	if !ok || len(inv.Items) != 1 || inv.Items[0] != "key" || inv.Stats["atk"] != 5 {
		t.Fatalf("unexpected Inventory: (%v, %v)", inv, ok)
	}

	// Each1 visits exact Position entities in deterministic order.
	var got1 []entities.Entity
	entities.Each1[Position](w, func(e entities.Entity, p *Position) bool {
		got1 = append(got1, e)
		return true
	})
	want1 := []entities.Entity{e0, e1, e3, e4, e5}
	if !slices.Equal(got1, want1) {
		t.Fatalf("Each1[Position] = %v, want %v", got1, want1)
	}

	// Each2[Position, Velocity]: |Position|=5 > |Velocity|=4, so drives from Velocity.
	var got2 []entities.Entity
	entities.Each2[Position, Velocity](w, func(e entities.Entity, p *Position, v *Velocity) bool {
		got2 = append(got2, e)
		p.X += v.DX
		return true
	})
	want2 := []entities.Entity{e1, e3, e5}
	if !slices.Equal(got2, want2) {
		t.Fatalf("Each2[Position, Velocity] = %v, want %v", got2, want2)
	}
	// Verify in-place mutation through query pointers.
	if p, _ := entities.Get[Position](w, e1); p.X != 21 {
		t.Fatalf("e1.X = %v, want 21", p.X)
	}
	if p, _ := entities.Get[Position](w, e0); p.X != 0 {
		t.Fatalf("e0.X was modified to %v, want 0", p.X)
	}

	// Each2[Velocity, Position]: |Velocity|=4 <= |Position|=5, drives from first store.
	var got2Rev []entities.Entity
	entities.Each2[Velocity, Position](w, func(e entities.Entity, _ *Velocity, _ *Position) bool {
		got2Rev = append(got2Rev, e)
		return true
	})
	if !slices.Equal(got2Rev, want2) {
		t.Fatalf("Each2[Velocity, Position] = %v, want %v", got2Rev, want2)
	}

	// Each3 visits exact intersection across all three smallest-set permutations.
	want3 := []entities.Entity{e3, e5}
	check3 := func(name string, got []entities.Entity) {
		t.Helper()
		if !slices.Equal(got, want3) {
			t.Fatalf("%s = %v, want %v", name, got, want3)
		}
	}
	var g3a, g3b, g3c []entities.Entity
	entities.Each3[Health, Velocity, Position](w, func(e entities.Entity, _ *Health, _ *Velocity, _ *Position) bool {
		g3a = append(g3a, e)
		return true
	})
	entities.Each3[Position, Health, Velocity](w, func(e entities.Entity, _ *Position, _ *Health, _ *Velocity) bool {
		g3b = append(g3b, e)
		return true
	})
	entities.Each3[Position, Velocity, Health](w, func(e entities.Entity, _ *Position, _ *Velocity, _ *Health) bool {
		g3c = append(g3c, e)
		return true
	})
	check3("Each3[H,V,P]", g3a)
	check3("Each3[P,H,V]", g3b)
	check3("Each3[P,V,H]", g3c)

	// Early termination works.
	stopped := 0
	entities.Each2[Position, Velocity](w, func(entities.Entity, *Position, *Velocity) bool {
		stopped++
		return false
	})
	if stopped != 1 {
		t.Fatalf("early stop visited %d entities, want 1", stopped)
	}

	// Zero-allocation verification for Get, Has, Each1, Each2, Each3.
	var sum float64
	if a := testing.AllocsPerRun(100, func() {
		if p, ok := entities.Get[Position](w, e3); ok {
			sum += p.X
		}
		if entities.Has[Velocity](w, e3) {
			sum++
		}
	}); a != 0 {
		t.Fatalf("Get+Has allocated %.1f times/run, want 0", a)
	}
	if a := testing.AllocsPerRun(100, func() {
		entities.Each1[Position](w, func(_ entities.Entity, p *Position) bool {
			sum += p.X
			return true
		})
	}); a != 0 {
		t.Fatalf("Each1 allocated %.1f times/run, want 0", a)
	}
	if a := testing.AllocsPerRun(100, func() {
		entities.Each2[Position, Velocity](w, func(_ entities.Entity, p *Position, v *Velocity) bool {
			sum += p.X + v.DX
			return true
		})
	}); a != 0 {
		t.Fatalf("Each2 allocated %.1f times/run, want 0", a)
	}
	if a := testing.AllocsPerRun(100, func() {
		entities.Each3[Position, Velocity, Health](w, func(_ entities.Entity, p *Position, v *Velocity, h *Health) bool {
			sum += p.X + v.DX + float64(h.Current)
			return true
		})
	}); a != 0 {
		t.Fatalf("Each3 allocated %.1f times/run, want 0", a)
	}
	_ = sum
}

func TestDestroyDuringQueryDoesNotSkipEntities(t *testing.T) {
	w := entities.NewWorld()
	for i := 0; i < 5; i++ {
		e := w.Spawn()
		entities.Set(w, e, Position{X: float64(i)})
		entities.Set(w, e, Health{Current: i % 2, Max: 10})
	}

	visited := 0
	entities.Each2[Position, Health](w, func(e entities.Entity, _ *Position, h *Health) bool {
		visited++
		if h.Current == 0 {
			w.Destroy(e)
		}
		return true
	})

	if visited != 5 {
		t.Fatalf("visited %d entities while destroying dead ones, want 5", visited)
	}
	if w.Count() != 2 {
		t.Fatalf("Count = %d after destroying 0-health entities, want 2", w.Count())
	}
}

// --- #139: Systems and update order ---

func TestSchedulerOrderingTieBreakingAndValidation(t *testing.T) {
	s := entities.NewScheduler()
	var ran []string
	record := func(name string) func(*entities.World, time.Duration) {
		return func(_ *entities.World, _ time.Duration) {
			ran = append(ran, name)
		}
	}

	// Register out of order with forward Before references and redundant After constraints.
	if err := s.AddSystem("render", record("render"), entities.After("physics")); err != nil {
		t.Fatalf("AddSystem render: %v", err)
	}
	if err := s.AddSystem("physics", record("physics"), entities.Before("render"), entities.After("input")); err != nil {
		t.Fatalf("AddSystem physics: %v", err)
	}
	if err := s.AddSystem("audio", record("audio")); err != nil {
		t.Fatalf("AddSystem audio: %v", err)
	}
	if err := s.AddSystem("input", record("input"), entities.Before("physics")); err != nil {
		t.Fatalf("AddSystem input: %v", err)
	}
	if err := s.AddSystem("ui", record("ui"), entities.After("render")); err != nil {
		t.Fatalf("AddSystem ui: %v", err)
	}

	// Ready at start: audio(idx 2) and input(idx 3). Tie-breaking picks audio first, then input -> physics -> render -> ui.
	wantOrder := []string{"audio", "input", "physics", "render", "ui"}
	if err := s.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	gotOrder := s.Order()
	if !slices.Equal(gotOrder, wantOrder) {
		t.Fatalf("Order = %v, want %v", gotOrder, wantOrder)
	}
	if gotStr, wantStr := s.String(), "audio -> input -> physics -> render -> ui"; gotStr != wantStr {
		t.Fatalf("String = %q, want %q", gotStr, wantStr)
	}
	// Mutating returned slice must not affect scheduler state.
	gotOrder[0] = "tampered"
	if s.Order()[0] != "audio" {
		t.Fatalf("Order returned aliased slice")
	}

	w := entities.NewWorld()
	if err := s.Step(w, 16*time.Millisecond); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if !slices.Equal(ran, wantOrder) {
		t.Fatalf("ran = %v, want %v", ran, wantOrder)
	}

	// Steady-state Step allocates zero times.
	noop := entities.NewScheduler()
	_ = noop.AddSystem("a", func(*entities.World, time.Duration) {})
	_ = noop.AddSystem("b", func(*entities.World, time.Duration) {}, entities.After("a"))
	if err := noop.Build(); err != nil {
		t.Fatalf("noop Build: %v", err)
	}
	if allocs := testing.AllocsPerRun(100, func() {
		_ = noop.Step(w, 16*time.Millisecond)
	}); allocs != 0 {
		t.Fatalf("Scheduler.Step allocated %.1f times/run, want 0", allocs)
	}
}

func TestSchedulerRejectsCyclesAndInvalidSystems(t *testing.T) {
	noop := func(*entities.World, time.Duration) {}

	t.Run("immediate validation", func(t *testing.T) {
		s := entities.NewScheduler()
		if err := s.AddSystem("", noop); !errors.Is(err, entities.ErrEmptySystemName) {
			t.Fatalf("empty name err = %v, want ErrEmptySystemName", err)
		}
		if err := s.AddSystem("a", nil); !errors.Is(err, entities.ErrNilSystem) {
			t.Fatalf("nil fn err = %v, want ErrNilSystem", err)
		}
		if err := s.AddSystem("a", noop, entities.Before("a")); !errors.Is(err, entities.ErrSystemCycle) {
			t.Fatalf("self Before err = %v, want ErrSystemCycle", err)
		}
		if err := s.AddSystem("a", noop, entities.After("a")); !errors.Is(err, entities.ErrSystemCycle) {
			t.Fatalf("self After err = %v, want ErrSystemCycle", err)
		}
		if err := s.AddSystem("a", noop, entities.Before("")); !errors.Is(err, entities.ErrEmptySystemName) {
			t.Fatalf("empty Before err = %v, want ErrEmptySystemName", err)
		}
		if err := s.AddSystem("a", noop); err != nil {
			t.Fatalf("AddSystem a: %v", err)
		}
		if err := s.AddSystem("a", noop); !errors.Is(err, entities.ErrDuplicateSystem) {
			t.Fatalf("duplicate err = %v, want ErrDuplicateSystem", err)
		}
	})

	t.Run("unknown target", func(t *testing.T) {
		s := entities.NewScheduler()
		_ = s.AddSystem("a", noop, entities.Before("missing"))
		if err := s.Build(); !errors.Is(err, entities.ErrUnknownSystem) {
			t.Fatalf("Build err = %v, want ErrUnknownSystem", err)
		}
		if s.Order() != nil {
			t.Fatalf("Order = %v on invalid schedule, want nil", s.Order())
		}
	})

	t.Run("cycle", func(t *testing.T) {
		ran := false
		s := entities.NewScheduler()
		_ = s.AddSystem("a", func(*entities.World, time.Duration) { ran = true }, entities.Before("b"))
		_ = s.AddSystem("b", func(*entities.World, time.Duration) { ran = true }, entities.Before("c"))
		_ = s.AddSystem("c", func(*entities.World, time.Duration) { ran = true }, entities.Before("a"))
		if err := s.Step(entities.NewWorld(), time.Millisecond); !errors.Is(err, entities.ErrSystemCycle) {
			t.Fatalf("Step err = %v, want ErrSystemCycle", err)
		}
		if ran {
			t.Fatalf("cyclic schedule executed a system")
		}
	})
}

func TestSchedulerDrivenByGameLoop(t *testing.T) {
	w := entities.NewWorld()
	hero := w.Spawn()
	entities.Set(w, hero, Position{X: 0, Y: 0})
	entities.Set(w, hero, Velocity{DX: 60, DY: 0})

	var stepLog []string
	sched := entities.NewScheduler()
	_ = sched.AddSystem("clamp", func(w *entities.World, _ time.Duration) {
		stepLog = append(stepLog, "clamp")
		entities.Each1[Position](w, func(_ entities.Entity, p *Position) bool {
			if p.X > 100 {
				p.X = 100
			}
			return true
		})
	}, entities.After("move"))
	_ = sched.AddSystem("move", func(w *entities.World, dt time.Duration) {
		stepLog = append(stepLog, "move")
		sec := dt.Seconds()
		entities.Each2[Position, Velocity](w, func(_ entities.Entity, p *Position, v *Velocity) bool {
			p.X += v.DX * sec
			p.Y += v.DY * sec
			return true
		})
	})

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			if err := sched.Step(w, dt); err != nil {
				t.Errorf("sched.Step: %v", err)
			}
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		w.Attach(cx)
		loop.Run(cx)
		return core.View{}
	})

	app.Advance(time.Second)

	pos, ok := entities.Get[Position](w, hero)
	if !ok {
		t.Fatalf("hero lost Position")
	}
	// 60 units/sec * 1 sec = 60.
	if pos.X < 59.9 || pos.X > 60.1 {
		t.Fatalf("hero.X = %v after 1s, want ~60", pos.X)
	}
	if len(stepLog) != 120 {
		t.Fatalf("stepLog len = %d, want 120 (60 steps * 2 systems)", len(stepLog))
	}
	for i := 0; i < len(stepLog); i += 2 {
		if stepLog[i] != "move" || stepLog[i+1] != "clamp" {
			t.Fatalf("step %d ran [%s, %s], want [move, clamp]", i/2, stepLog[i], stepLog[i+1])
		}
	}
}

// --- #140: Prefabs ---

func TestPrefabSpawnsIndependentEntities(t *testing.T) {
	goblin := entities.NewPrefab("goblin")
	entities.With(goblin, Position{X: 10, Y: 20})
	entities.With(goblin, Health{Current: 50, Max: 50})
	entities.WithFunc(goblin, func() Inventory {
		return Inventory{
			Items: []string{"dagger"},
			Stats: map[string]int{"luck": 3},
		}
	})

	if goblin.Name() != "goblin" {
		t.Fatalf("Name = %q, want %q", goblin.Name(), "goblin")
	}

	// Derived prefab overrides Health without modifying parent prefab.
	boss := goblin.Clone("goblin-boss")
	entities.With(boss, Health{Current: 200, Max: 200})

	w := entities.NewWorld()
	spawned := goblin.SpawnN(w, 3)
	if len(spawned) != 3 {
		t.Fatalf("SpawnN len = %d, want 3", len(spawned))
	}
	bossEntity := boss.Spawn(w)

	// Mutate entity 0's value and reference components.
	p0, _ := entities.Get[Position](w, spawned[0])
	p0.X = 99
	h0, _ := entities.Get[Health](w, spawned[0])
	h0.Current = 10
	inv0, _ := entities.Get[Inventory](w, spawned[0])
	inv0.Items[0] = "sword"
	inv0.Items = append(inv0.Items, "shield")
	inv0.Stats["luck"] = 99

	// Entities 1 and 2 must remain completely untouched.
	for _, e := range spawned[1:] {
		p, _ := entities.Get[Position](w, e)
		h, _ := entities.Get[Health](w, e)
		inv, _ := entities.Get[Inventory](w, e)
		if *p != (Position{X: 10, Y: 20}) {
			t.Fatalf("%v Position = %+v, want {10 20}", e, *p)
		}
		if *h != (Health{Current: 50, Max: 50}) {
			t.Fatalf("%v Health = %+v, want {50 50}", e, *h)
		}
		if !slices.Equal(inv.Items, []string{"dagger"}) || inv.Stats["luck"] != 3 {
			t.Fatalf("%v Inventory = %+v, mutated by sibling", e, *inv)
		}
	}

	// Boss got overridden health while goblin kept 50.
	if bh, _ := entities.Get[Health](w, bossEntity); *bh != (Health{Current: 200, Max: 200}) {
		t.Fatalf("boss Health = %+v, want {200 200}", *bh)
	}
	if goblin.SpawnN(w, 0) != nil {
		t.Fatalf("SpawnN(0) should return nil")
	}
}

// --- #141: Saving and loading a world ---

func registerGameCodecs(t *testing.T, w *entities.World) {
	t.Helper()
	for _, err := range []error{
		entities.RegisterCodec[Position](w, "position"),
		entities.RegisterCodec[Velocity](w, "velocity"),
		entities.RegisterCodec[Health](w, "health"),
		entities.RegisterCodec[Tag](w, "tag"),
		entities.RegisterCodec[Inventory](w, "inventory"),
		entities.RegisterCodec[Parent](w, "parent"),
	} {
		if err != nil {
			t.Fatalf("RegisterCodec: %v", err)
		}
	}
}

func assertWorldsEqual(t *testing.T, got, want *entities.World) {
	t.Helper()
	if got.Count() != want.Count() {
		t.Fatalf("world Count = %d, want %d", got.Count(), want.Count())
	}
	gotSnap, err := got.Snapshot()
	if err != nil {
		t.Fatalf("got.Snapshot: %v", err)
	}
	wantSnap, err := want.Snapshot()
	if err != nil {
		t.Fatalf("want.Snapshot: %v", err)
	}
	gotJSON, err := json.Marshal(gotSnap)
	if err != nil {
		t.Fatalf("marshal gotSnap: %v", err)
	}
	wantJSON, err := json.Marshal(wantSnap)
	if err != nil {
		t.Fatalf("marshal wantSnap: %v", err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("world snapshots differ:\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
}

func buildSampleWorld(t *testing.T) (*entities.World, entities.Entity, entities.Entity, entities.Entity) {
	t.Helper()
	w := entities.NewWorld()
	registerGameCodecs(t, w)

	// Create and destroy an entity first so we exercise non-contiguous slots and generation > 1.
	temp := w.Spawn()
	entities.Set(w, temp, Position{X: -1, Y: -1})
	w.Destroy(temp)

	hero := w.Spawn() // recycled slot 0, gen 2
	entities.Set(w, hero, Position{X: 12.5, Y: -4.25})
	entities.Set(w, hero, Velocity{DX: 1.5, DY: 0.5})
	entities.Set(w, hero, Health{Current: 85, Max: 100})
	entities.Set(w, hero, Tag{Label: "hero"})
	entities.Set(w, hero, Inventory{Items: []string{"potion", "key"}, Stats: map[string]int{"gold": 42}})
	entities.Set(w, hero, transientRender{Layer: 7}) // not registered -> omitted from snapshot

	gap := w.Spawn() // slot 1, gen 1 -> destroyed to create a hole
	w.Destroy(gap)

	pet := w.Spawn() // recycled slot 1, gen 2
	entities.Set(w, pet, Position{X: 14, Y: -4.25})
	entities.Set(w, pet, Tag{Label: "pet"})
	entities.Set(w, pet, Parent{Target: hero})

	// Leave slot 2 empty and put a third entity at slot 3 to test sparse holes in snapshot.
	hole := w.Spawn()  // slot 2
	chest := w.Spawn() // slot 3
	w.Destroy(hole)
	entities.Set(w, chest, Position{X: 100, Y: 200})
	entities.Set(w, chest, Tag{Label: "chest"})

	return w, hero, pet, chest
}

func TestWorldSaveAndLoadRoundTripViaStorageAndFileStores(t *testing.T) {
	src, hero, pet, chest := buildSampleWorld(t)

	// Snapshot output must be deterministic across repeated calls.
	snap1, err := src.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot 1: %v", err)
	}
	snap2, err := src.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot 2: %v", err)
	}
	b1, _ := json.Marshal(snap1)
	b2, _ := json.Marshal(snap2)
	if !bytes.Equal(b1, b2) {
		t.Fatalf("non-deterministic snapshots:\n%s\nvs\n%s", b1, b2)
	}

	// Verify transient unregistered components are excluded.
	if bytes.Contains(b1, []byte("Layer")) {
		t.Fatalf("unregistered transient component leaked into snapshot: %s", b1)
	}

	verifyRestored := func(t *testing.T, dst *entities.World) {
		t.Helper()
		assertWorldsEqual(t, dst, src)

		// Exact generational handles and cross-entity references are preserved.
		if !dst.Alive(hero) || !dst.Alive(pet) || !dst.Alive(chest) {
			t.Fatalf("live handles not preserved: hero=%v pet=%v chest=%v", dst.Alive(hero), dst.Alive(pet), dst.Alive(chest))
		}
		staleHero := entities.Entity(1<<32 | uint64(hero.Index())) // gen 1, hero is gen 2
		if dst.Alive(staleHero) {
			t.Fatalf("stale generation handle %v should not be alive", staleHero)
		}
		if p, ok := entities.Get[Parent](dst, pet); !ok || p.Target != hero || !dst.Alive(p.Target) {
			t.Fatalf("pet Parent = (%v, %v), want hero %v", p, ok, hero)
		}
		if inv, ok := entities.Get[Inventory](dst, hero); !ok || !slices.Equal(inv.Items, []string{"potion", "key"}) || inv.Stats["gold"] != 42 {
			t.Fatalf("hero Inventory = (%v, %v)", inv, ok)
		}
		if entities.Has[transientRender](dst, hero) {
			t.Fatalf("transientRender should not exist after restore")
		}
		// Spawning into the restored world recycles hole slot 2 cleanly without colliding with live entities.
		extra := dst.Spawn()
		if !dst.Alive(extra) || extra == hero || extra == pet || extra == chest {
			t.Fatalf("Spawn after restore returned invalid handle %v", extra)
		}
	}

	t.Run("StorageStore across restart", func(t *testing.T) {
		app1 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			store := save.NewStorageStore(cx.Storage(), "world")
			if err := entities.SaveWorld(store, "slot1", src); err != nil {
				t.Fatalf("SaveWorld: %v", err)
			}
			return core.View{}
		})

		// Copy storage entries into a fresh app to simulate a restart.
		kept := map[string]string{}
		st := app1.Storage()
		for _, k := range st.Keys() {
			kept[k], _ = st.GetItem(k)
		}

		dst := entities.NewWorld()
		registerGameCodecs(t, dst)
		// Also verify a pre-acquired *Store[Position] stays valid after LoadWorld.
		posStore := entities.Component[Position](dst)

		nsgotest.Start(t, func(cx *core.Scope) core.Node {
			store := save.NewStorageStore(cx.Storage(), "world")
			if err := entities.LoadWorld(store, "slot1", dst); err != nil {
				t.Fatalf("LoadWorld: %v", err)
			}
			return core.View{}
		}, nsgotest.Storage(kept))

		verifyRestored(t, dst)
		if p, ok := posStore.Get(hero); !ok || p.X != 12.5 {
			t.Fatalf("cached *Store[Position] not updated after LoadWorld: (%v, %v)", p, ok)
		}
	})

	t.Run("FileStore TempDir", func(t *testing.T) {
		store := save.NewFileStore(t.TempDir())
		if err := entities.SaveWorld(store, "autosave", src); err != nil {
			t.Fatalf("SaveWorld: %v", err)
		}
		dst := entities.NewWorld()
		registerGameCodecs(t, dst)
		if err := entities.LoadWorld(store, "autosave", dst); err != nil {
			t.Fatalf("LoadWorld: %v", err)
		}
		verifyRestored(t, dst)
	})

	t.Run("FileStore DataDir", func(t *testing.T) {
		var dataDir string
		nsgotest.Start(t, func(cx *core.Scope) core.Node {
			dataDir = cx.DataDir()
			return core.View{}
		})
		if dataDir == "" {
			t.Skip("DataDir is empty on this platform")
		}
		store := save.NewFileStore(dataDir)
		slot := fmt.Sprintf("entities-datadir-%d", time.Now().UnixNano())
		t.Cleanup(func() { _ = store.Delete(slot) })

		if err := entities.SaveWorld(store, slot, src); err != nil {
			t.Fatalf("SaveWorld: %v", err)
		}
		dst := entities.NewWorld()
		registerGameCodecs(t, dst)
		if err := entities.LoadWorld(store, slot, dst); err != nil {
			t.Fatalf("LoadWorld: %v", err)
		}
		verifyRestored(t, dst)
	})
}

func TestWorldRestoreAtomicOnErrorAndCodecValidation(t *testing.T) {
	w := entities.NewWorld()
	if err := entities.RegisterCodec[Position](w, ""); !errors.Is(err, entities.ErrEmptyCodecName) {
		t.Fatalf("empty codec name err = %v, want ErrEmptyCodecName", err)
	}
	if err := entities.RegisterCodec[Position](w, "pos"); err != nil {
		t.Fatalf("RegisterCodec pos: %v", err)
	}
	if err := entities.RegisterCodec[Velocity](w, "pos"); !errors.Is(err, entities.ErrDuplicateCodec) {
		t.Fatalf("duplicate name err = %v, want ErrDuplicateCodec", err)
	}
	if err := entities.RegisterCodec[Position](w, "position2"); !errors.Is(err, entities.ErrDuplicateCodec) {
		t.Fatalf("duplicate type err = %v, want ErrDuplicateCodec", err)
	}

	existing := w.Spawn()
	entities.Set(w, existing, Position{X: 7, Y: 8})

	// Unknown codec leaves w completely intact.
	badCodecSnap := entities.WorldSnapshot{
		Entities: []entities.EntitySnapshot{{
			Index:      0,
			Generation: 1,
			Components: []entities.ComponentSnapshot{{
				Type: "unknown",
				Data: json.RawMessage(`{}`),
			}},
		}},
	}
	if err := w.Restore(badCodecSnap); !errors.Is(err, entities.ErrUnknownCodec) {
		t.Fatalf("Restore unknown codec err = %v, want ErrUnknownCodec", err)
	}
	if !w.Alive(existing) {
		t.Fatalf("failed Restore clobbered existing world")
	}
	if p, ok := entities.Get[Position](w, existing); !ok || *p != (Position{X: 7, Y: 8}) {
		t.Fatalf("existing component mutated after failed Restore: (%v, %v)", p, ok)
	}

	// Zero generation and duplicate index are rejected cleanly.
	if err := w.Restore(entities.WorldSnapshot{
		Entities: []entities.EntitySnapshot{{Index: 0, Generation: 0}},
	}); !errors.Is(err, entities.ErrInvalidSnapshot) {
		t.Fatalf("zero gen err = %v, want ErrInvalidSnapshot", err)
	}
	if err := w.Restore(entities.WorldSnapshot{
		Entities: []entities.EntitySnapshot{
			{Index: 0, Generation: 1},
			{Index: 0, Generation: 2},
		},
	}); !errors.Is(err, entities.ErrInvalidSnapshot) {
		t.Fatalf("duplicate index err = %v, want ErrInvalidSnapshot", err)
	}
}

func TestWorldSaveMigrationWithSaveStore(t *testing.T) {
	dir := t.TempDir()

	// Save v1 world with "health" stored using old field name {"hp": 75}.
	type healthV1 struct {
		HP int `json:"hp"`
	}
	w1 := entities.NewWorld()
	_ = entities.RegisterCodec[healthV1](w1, "health")
	e := w1.Spawn()
	entities.Set(w1, e, healthV1{HP: 75})

	storeV1 := save.NewFileStore(dir, save.WithVersion(1))
	if err := entities.SaveWorld(storeV1, "slot1", w1); err != nil {
		t.Fatalf("SaveWorld v1: %v", err)
	}

	// Load into v2 world where "health" is Health{Current, Max}, migrated via save.WithMigration.
	storeV2 := save.NewFileStore(dir,
		save.WithVersion(2),
		save.WithMigration(1, func(raw json.RawMessage) (json.RawMessage, error) {
			var snap entities.WorldSnapshot
			if err := json.Unmarshal(raw, &snap); err != nil {
				return nil, err
			}
			for i := range snap.Entities {
				for j := range snap.Entities[i].Components {
					c := &snap.Entities[i].Components[j]
					if c.Type == "health" {
						var old healthV1
						if err := json.Unmarshal(c.Data, &old); err != nil {
							return nil, err
						}
						upgraded, err := json.Marshal(Health{Current: old.HP, Max: 100})
						if err != nil {
							return nil, err
						}
						c.Data = upgraded
					}
				}
			}
			return json.Marshal(snap)
		}),
	)

	w2 := entities.NewWorld()
	_ = entities.RegisterCodec[Health](w2, "health")
	if err := entities.LoadWorld(storeV2, "slot1", w2); err != nil {
		t.Fatalf("LoadWorld v2: %v", err)
	}
	h, ok := entities.Get[Health](w2, e)
	if !ok || *h != (Health{Current: 75, Max: 100}) {
		t.Fatalf("migrated Health = (%v, %v), want {75 100}", h, ok)
	}
}

func BenchmarkSpawnDestroy100k(b *testing.B) {
	const count = 100_000
	w := entities.NewWorld()
	buf := make([]entities.Entity, count)
	for i := 0; i < count; i++ {
		buf[i] = w.Spawn()
	}
	for i := 0; i < count; i++ {
		w.Destroy(buf[i])
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < count; j++ {
			buf[j] = w.Spawn()
		}
		for j := 0; j < count; j++ {
			w.Destroy(buf[j])
		}
	}
}

// A world snapshot must keep parent-child links, not restore every entity as a root.
func TestWorldSnapshotRestoresHierarchy(t *testing.T) {
	w := entities.NewWorld()
	registerGameCodecs(t, w)
	if err := entities.RegisterCodec[entities.Transform2D](w, "transform2d"); err != nil {
		t.Fatalf("RegisterCodec[Transform2D]: %v", err)
	}
	h := entities.NewHierarchy2D(w)
	grand := w.Spawn()
	parent := w.Spawn()
	child := w.Spawn()
	h.SetLocal(grand, entities.NewTransform2D(1, 0))
	h.SetLocal(parent, entities.NewTransform2D(10, 0))
	h.SetLocal(child, entities.NewTransform2D(2, 0))
	if err := h.SetParent(parent, grand, false); err != nil {
		t.Fatalf("SetParent(parent, grand): %v", err)
	}
	if err := h.SetParent(child, parent, false); err != nil {
		t.Fatalf("SetParent(child, parent): %v", err)
	}

	snap, err := w.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	restored := entities.NewWorld()
	registerGameCodecs(t, restored)
	if err := entities.RegisterCodec[entities.Transform2D](restored, "transform2d"); err != nil {
		t.Fatalf("RegisterCodec[Transform2D]: %v", err)
	}
	entities.NewHierarchy2D(restored)
	if err := restored.Restore(snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	h2 := restored.Hierarchy2D()
	if p, ok := h2.Parent(child); !ok || p != parent {
		t.Fatalf("restored parent of child = %v, %v, want %v", p, ok, parent)
	}
	if p, ok := h2.Parent(parent); !ok || p != grand {
		t.Fatalf("restored parent of parent = %v, %v, want %v", p, ok, grand)
	}
	wt, ok := entities.Get[entities.WorldTransform2D](restored, child)
	if !ok {
		t.Fatal("child has no world transform after restore")
	}
	if math.Abs(float64(wt.X-13)) > 1e-5 || math.Abs(float64(wt.Y)) > 1e-5 {
		t.Fatalf("restored world transform = (%v, %v), want (13, 0)", wt.X, wt.Y)
	}
}

func TestRestoreRejectsOversizedIndex(t *testing.T) {
	w := entities.NewWorld()
	hi := uint32(1 << 20)
	err := w.Restore(entities.WorldSnapshot{Entities: []entities.EntitySnapshot{{Index: hi, Generation: 1}}})
	if !errors.Is(err, entities.ErrInvalidSnapshot) {
		t.Fatalf("Restore(oversized index) = %v, want ErrInvalidSnapshot", err)
	}
}

func TestRestoreAdvancesDeadSlotGenerations(t *testing.T) {
	w := entities.NewWorld()
	registerGameCodecs(t, w)
	a := w.Spawn()
	w.Destroy(a)
	_ = w.Spawn() // keeps a live entity so the snapshot is non-empty
	snap, err := w.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := w.Restore(snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	c := w.Spawn() // reuses the freed slot
	if c == a {
		t.Fatalf("spawn after restore reused stale handle %v", a)
	}
	if w.Alive(a) {
		t.Fatalf("stale handle %v is alive after restore", a)
	}
}

func TestInspectionHelpers(t *testing.T) {
	w := entities.NewWorld()
	registerGameCodecs(t, w)

	names := w.CodecNames()
	wantNames := []string{"health", "inventory", "parent", "position", "tag", "velocity"}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("CodecNames = %v, want %v", names, wantNames)
	}
	if cname, ok := entities.CodecName[Position](w); !ok || cname != "position" {
		t.Fatalf("CodecName[Position] = (%q, %v), want (position, true)", cname, ok)
	}
	if _, ok := entities.CodecName[entities.Transform2D](w); ok {
		t.Fatal("CodecName[Transform2D] should be false when unregistered")
	}

	e := w.Spawn()
	entities.Set(w, e, Position{X: 10, Y: 20})
	entities.Set(w, e, Health{Current: 80, Max: 100})

	// ParentOf / ChildrenOf without a hierarchy must not create one.
	if p, ok := w.ParentOf(e); ok || p != entities.Zero {
		t.Fatalf("ParentOf without hierarchy = (%v, %v), want (Zero, false)", p, ok)
	}
	if ch := w.ChildrenOf(e); len(ch) != 0 {
		t.Fatalf("ChildrenOf without hierarchy = %v, want empty", ch)
	}
	if entities.Has[entities.Transform2D](w, e) {
		t.Fatal("ParentOf/ChildrenOf unexpectedly created Transform2D")
	}

	if got, ok := w.Resolve(e.Index(), e.Generation()); !ok || got != e {
		t.Fatalf("Resolve(%d, %d) = (%v, %v), want (%v, true)", e.Index(), e.Generation(), got, ok, e)
	}
	if _, ok := w.Resolve(e.Index(), 0); ok {
		t.Fatal("Resolve with generation 0 should fail")
	}
	if entities.MakeEntity(0, 0) != entities.Zero {
		t.Fatal("MakeEntity(0, 0) should be Zero")
	}

	if !w.HasCodec(e, "position") || w.HasCodec(e, "velocity") || w.HasCodec(e, "missing") {
		t.Fatal("HasCodec mismatch")
	}
	raw, ok, err := w.EncodeComponent(e, "position")
	if err != nil || !ok || string(raw) != `{"x":10,"y":20}` {
		t.Fatalf("EncodeComponent(position) = (%s, %v, %v)", raw, ok, err)
	}
	if _, ok, err := w.EncodeComponent(e, "velocity"); err != nil || ok {
		t.Fatalf("EncodeComponent(absent) = (%v, %v), want (false, nil)", ok, err)
	}
	comps, err := w.EntityComponents(e)
	if err != nil || len(comps) != 2 || comps[0].Type != "health" || comps[1].Type != "position" {
		t.Fatalf("EntityComponents = (%v, %v)", comps, err)
	}

	child := w.Spawn()
	h := w.Hierarchy2D()
	if err := h.SetParent(child, e, false); err != nil {
		t.Fatalf("SetParent: %v", err)
	}
	if p, ok := w.ParentOf(child); !ok || p != e {
		t.Fatalf("ParentOf(child) = (%v, %v), want (%v, true)", p, ok, e)
	}
	if ch := w.ChildrenOf(e); len(ch) != 1 || ch[0] != child {
		t.Fatalf("ChildrenOf(e) = %v, want [%v]", ch, child)
	}

	w.Destroy(e)
	if _, ok := w.Resolve(e.Index(), e.Generation()); ok {
		t.Fatal("Resolve should reject destroyed entity")
	}
}

func TestSchedulerHasSystem(t *testing.T) {
	var nilSched *entities.Scheduler
	if nilSched.HasSystem("a") {
		t.Fatal("nil Scheduler.HasSystem should be false")
	}
	s := entities.NewScheduler()
	if s.HasSystem("") || s.HasSystem("move") {
		t.Fatal("empty Scheduler should report false")
	}
	if err := s.AddSystem("move", func(*entities.World, time.Duration) {}); err != nil {
		t.Fatalf("AddSystem: %v", err)
	}
	if !s.HasSystem("move") || s.HasSystem("other") {
		t.Fatal("HasSystem mismatch after AddSystem")
	}
}

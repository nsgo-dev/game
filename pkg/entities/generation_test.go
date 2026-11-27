package entities_test

import (
	"errors"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/save"
)

func TestRestoreSmallerSnapshotKeepsOmittedHandlesDead(t *testing.T) {
	w := entities.NewWorld()
	registerGameCodecs(t, w)

	old := make([]entities.Entity, 5)
	for i := range old {
		old[i] = w.Spawn()
		entities.Set(w, old[i], Position{X: float64(i), Y: float64(i)})
	}

	// Restore a snapshot holding only index 0 (same handle as old[0]).
	snap := entities.WorldSnapshot{
		Entities: []entities.EntitySnapshot{{
			Index:      0,
			Generation: old[0].Generation(),
		}},
	}
	if err := w.Restore(snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !w.Alive(old[0]) {
		t.Fatalf("restored handle %v should be alive", old[0])
	}
	for i := 1; i < len(old); i++ {
		if w.Alive(old[i]) {
			t.Fatalf("omitted handle old[%d]=%v alive immediately after Restore", i, old[i])
		}
	}

	// Spawn 4 more entities and verify none alias old[1..4].
	fresh := make([]entities.Entity, 4)
	for i := range fresh {
		fresh[i] = w.Spawn()
		entities.Set(w, fresh[i], Position{X: 100 + float64(i)})
	}
	for i := 1; i < len(old); i++ {
		if w.Alive(old[i]) {
			t.Fatalf("stale handle old[%d]=%v aliased a new entity after Restore+Spawn (fresh=%v)", i, old[i], fresh)
		}
		if entities.Has[Position](w, old[i]) {
			t.Fatalf("stale handle old[%d]=%v reads component after Restore+Spawn", i, old[i])
		}
	}
	for i, e := range fresh {
		if !w.Alive(e) {
			t.Fatalf("fresh[%d]=%v not alive", i, e)
		}
		if e.Generation() <= 1 {
			t.Fatalf("fresh[%d]=%v reused generation %d, want > 1", i, e, e.Generation())
		}
	}

	// Also verify when index 0 is restored at a different generation: old[0] stays dead too.
	if err := w.Restore(entities.WorldSnapshot{
		Entities: []entities.EntitySnapshot{{Index: 0, Generation: 9}},
	}); err != nil {
		t.Fatalf("Restore gen 9: %v", err)
	}
	restored0 := entities.MakeEntity(0, 9)
	if !w.Alive(restored0) || w.Alive(old[0]) {
		t.Fatalf("after gen-9 restore: alive(0:9)=%v alive(old[0])=%v", w.Alive(restored0), w.Alive(old[0]))
	}
	for range 4 {
		e := w.Spawn()
		for j, prev := range fresh {
			if e == prev || w.Alive(prev) {
				t.Fatalf("spawn %v aliased prior handle fresh[%d]=%v", e, j, prev)
			}
		}
	}
}

func TestRestoreEmptySnapshotKeepsOldHandlesDead(t *testing.T) {
	w := entities.NewWorld()
	registerGameCodecs(t, w)

	e0 := w.Spawn()
	e1 := w.Spawn()
	entities.Set(w, e0, Position{X: 1, Y: 2})
	entities.Set(w, e1, Position{X: 3, Y: 4})

	if err := w.Restore(entities.WorldSnapshot{}); err != nil {
		t.Fatalf("Restore(empty): %v", err)
	}
	if w.Count() != 0 || w.Alive(e0) || w.Alive(e1) {
		t.Fatalf("after empty Restore: count=%d alive=(%v,%v)", w.Count(), w.Alive(e0), w.Alive(e1))
	}

	n0 := w.Spawn()
	n1 := w.Spawn()
	if w.Alive(e0) || w.Alive(e1) {
		t.Fatalf("stale handles (%v, %v) became alive after empty Restore+Spawn (%v, %v)", e0, e1, n0, n1)
	}
	if n0 == e0 || n1 == e1 || !w.Alive(n0) || !w.Alive(n1) {
		t.Fatalf("unexpected spawns after empty Restore: n0=%v n1=%v", n0, n1)
	}
	if entities.Has[Position](w, n0) || entities.Has[Position](w, e0) {
		t.Fatalf("component leaked across empty Restore")
	}

	// Repeated empty restores without intervening spawns keep generations valid and do not alias.
	if err := w.Restore(entities.WorldSnapshot{}); err != nil {
		t.Fatalf("second Restore(empty): %v", err)
	}
	if err := w.Restore(entities.WorldSnapshot{}); err != nil {
		t.Fatalf("third Restore(empty): %v", err)
	}
	n2 := w.Spawn()
	if w.Alive(e0) || w.Alive(n0) || !w.Alive(n2) || n2 == e0 || n2 == n0 {
		t.Fatalf("repeated empty Restore aliased handle: e0=%v n0=%v n2=%v", e0, n0, n2)
	}
}

func TestResetKeepsOldHandlesDead(t *testing.T) {
	w := entities.NewWorld()
	registerGameCodecs(t, w)

	old := make([]entities.Entity, 5)
	for i := range old {
		old[i] = w.Spawn()
		entities.Set(w, old[i], Position{X: float64(i)})
	}
	h := w.Hierarchy2D()
	if err := h.SetParent(old[1], old[0], false); err != nil {
		t.Fatalf("SetParent: %v", err)
	}

	w.Reset()
	if w.Count() != 0 {
		t.Fatalf("Count after Reset = %d, want 0", w.Count())
	}
	for i, e := range old {
		if w.Alive(e) {
			t.Fatalf("old[%d]=%v still alive after Reset", i, e)
		}
	}

	// Codecs must remain registered across Reset.
	if _, ok := entities.CodecName[Position](w); !ok {
		t.Fatal("Position codec lost across Reset")
	}

	// Spawning after Reset reuses indices from 0 upward with bumped generations.
	next := make([]entities.Entity, 6)
	for i := range next {
		next[i] = w.Spawn()
		entities.Set(w, next[i], Position{X: 50 + float64(i)})
	}
	for i, e := range old {
		if w.Alive(e) {
			t.Fatalf("stale handle old[%d]=%v aliased new entity after Reset+Spawn (next=%v)", i, e, next)
		}
		if entities.Has[Position](w, e) {
			t.Fatalf("stale handle old[%d]=%v reads component after Reset+Spawn", i, e)
		}
	}
	for i := 0; i < len(old); i++ {
		if next[i].Index() != uint32(i) || next[i].Generation() != old[i].Generation()+1 {
			t.Fatalf("next[%d] = %v, want index=%d gen=%d", i, next[i], i, old[i].Generation()+1)
		}
	}
	if next[5].Index() != 5 || next[5].Generation() != 1 {
		t.Fatalf("next[5] = %v, want Entity(5:1)", next[5])
	}

	// A second Reset (including an extra back-to-back Reset) keeps all prior handles dead.
	w.Reset()
	w.Reset()
	after := w.Spawn()
	if after.Index() != 0 || after.Generation() != next[0].Generation()+1 {
		t.Fatalf("spawn after repeated Reset = %v, want index=0 gen=%d", after, next[0].Generation()+1)
	}
	if w.Alive(old[0]) || w.Alive(next[0]) {
		t.Fatalf("prior handles aliased after repeated Reset: old=%v next=%v after=%v", old[0], next[0], after)
	}

	// Reset on a disposed world is a no-op and keeps it disposed.
	w.Dispose()
	w.Reset()
	if !w.Disposed() || !w.Spawn().IsZero() {
		t.Fatalf("Reset revived disposed world")
	}
}

func TestRestoreIntoLargerSmallerAndFreshWorlds(t *testing.T) {
	t.Run("into larger world with holes and tail", func(t *testing.T) {
		w := entities.NewWorld()
		registerGameCodecs(t, w)

		var stale []entities.Entity
		for range 8 {
			e := w.Spawn()
			stale = append(stale, e)
		}
		// Advance slot 1 and slot 6 to higher generations before Restore.
		w.Destroy(stale[1])
		e1b := w.Spawn() // slot 1 gen 2
		stale = append(stale, e1b)
		w.Destroy(stale[6])
		e6b := w.Spawn() // slot 6 gen 2
		stale = append(stale, e6b)

		snap := entities.WorldSnapshot{
			Entities: []entities.EntitySnapshot{
				{Index: 0, Generation: stale[0].Generation()},
				{Index: 2, Generation: stale[2].Generation()},
			},
		}
		if err := w.Restore(snap); err != nil {
			t.Fatalf("Restore: %v", err)
		}
		if w.Count() != 2 || !w.Alive(stale[0]) || !w.Alive(stale[2]) {
			t.Fatalf("restored entities mismatch: count=%d alive(0)=%v alive(2)=%v", w.Count(), w.Alive(stale[0]), w.Alive(stale[2]))
		}
		// Spawn 10 new entities covering hole 1, tail 3..7, and new slots 8..11.
		for range 10 {
			n := w.Spawn()
			if !w.Alive(n) || n.IsZero() {
				t.Fatalf("invalid spawned handle %v", n)
			}
			for _, old := range stale {
				if old == stale[0] || old == stale[2] {
					continue
				}
				if n == old || w.Alive(old) {
					t.Fatalf("spawn %v aliased omitted handle %v", n, old)
				}
			}
		}
	})

	t.Run("into smaller world omitting existing slot", func(t *testing.T) {
		w := entities.NewWorld()
		e0 := w.Spawn() // 0:1
		e1 := w.Spawn() // 1:1

		snap := entities.WorldSnapshot{
			Entities: []entities.EntitySnapshot{
				{Index: 0, Generation: 1},
				{Index: 4, Generation: 3},
			},
		}
		if err := w.Restore(snap); err != nil {
			t.Fatalf("Restore: %v", err)
		}
		if !w.Alive(e0) || !w.Alive(entities.MakeEntity(4, 3)) || w.Alive(e1) {
			t.Fatalf("unexpected liveness after restore into smaller world")
		}
		// Holes are 1, 2, 3. Slot 1 must have generation >= 2 so e1 never aliases.
		for range 3 {
			n := w.Spawn()
			if n == e1 || w.Alive(e1) {
				t.Fatalf("spawn %v aliased omitted slot handle %v", n, e1)
			}
		}
	})

	t.Run("into fresh world", func(t *testing.T) {
		w := entities.NewWorld()
		if err := w.Restore(entities.WorldSnapshot{}); err != nil {
			t.Fatalf("Restore(empty) on fresh world: %v", err)
		}
		if w.Count() != 0 {
			t.Fatalf("Count = %d, want 0", w.Count())
		}
		freshSnap := entities.WorldSnapshot{
			Entities: []entities.EntitySnapshot{
				{Index: 0, Generation: 2},
				{Index: 3, Generation: 5},
			},
		}
		if err := w.Restore(freshSnap); err != nil {
			t.Fatalf("Restore on fresh world: %v", err)
		}
		e0 := entities.MakeEntity(0, 2)
		e3 := entities.MakeEntity(3, 5)
		if w.Count() != 2 || !w.Alive(e0) || !w.Alive(e3) {
			t.Fatalf("fresh restore liveness mismatch: count=%d e0=%v e3=%v", w.Count(), w.Alive(e0), w.Alive(e3))
		}
		// Holes 1 and 2 spawn cleanly without colliding with e0 or e3.
		h1 := w.Spawn()
		h2 := w.Spawn()
		if h1.Index() != 1 || h2.Index() != 2 || !w.Alive(h1) || !w.Alive(h2) {
			t.Fatalf("unexpected hole spawns on fresh world: h1=%v h2=%v", h1, h2)
		}
	})
}

func TestRestoreRollbackNeverAliasesPreRollbackHandles(t *testing.T) {
	setupRollback := func(t *testing.T) (*entities.World, entities.WorldSnapshot, entities.Entity, []entities.Entity) {
		t.Helper()
		w := entities.NewWorld()
		registerGameCodecs(t, w)

		e0 := w.Spawn() // 0:1
		entities.Set(w, e0, Position{X: 10, Y: 20})
		checkpoint, err := w.Snapshot()
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}

		// Progress slot 0 through generations 2, 3, 4.
		var later []entities.Entity
		prev := e0
		for range 3 {
			w.Destroy(prev)
			prev = w.Spawn()
			later = append(later, prev)
		}
		// Roll back to checkpoint (slot 0 returns to 0:1).
		if err := w.Restore(checkpoint); err != nil {
			t.Fatalf("Restore(checkpoint): %v", err)
		}
		if !w.Alive(e0) {
			t.Fatalf("checkpoint handle %v not alive after rollback", e0)
		}
		for _, old := range later {
			if w.Alive(old) {
				t.Fatalf("post-checkpoint handle %v alive immediately after rollback", old)
			}
		}
		return w, checkpoint, e0, later
	}

	assertNoAlias := func(t *testing.T, w *entities.World, spawned entities.Entity, forbidden ...entities.Entity) {
		t.Helper()
		if !w.Alive(spawned) || spawned.IsZero() {
			t.Fatalf("spawned handle %v is not alive", spawned)
		}
		for _, old := range forbidden {
			if spawned == old || w.Alive(old) {
				t.Fatalf("spawned %v aliased pre-rollback handle %v", spawned, old)
			}
		}
	}

	t.Run("destroy after rollback", func(t *testing.T) {
		w, _, e0, later := setupRollback(t)
		w.Destroy(e0)
		next := w.Spawn()
		assertNoAlias(t, w, next, append([]entities.Entity{e0}, later...)...)
		if next.Generation() <= later[len(later)-1].Generation() {
			t.Fatalf("next generation = %d, want > %d", next.Generation(), later[len(later)-1].Generation())
		}
	})

	t.Run("clear after rollback", func(t *testing.T) {
		w, _, e0, later := setupRollback(t)
		w.Clear()
		next := w.Spawn()
		assertNoAlias(t, w, next, append([]entities.Entity{e0}, later...)...)
	})

	t.Run("reset after rollback", func(t *testing.T) {
		w, _, e0, later := setupRollback(t)
		w.Reset()
		next := w.Spawn()
		assertNoAlias(t, w, next, append([]entities.Entity{e0}, later...)...)
	})

	t.Run("empty restore after rollback", func(t *testing.T) {
		w, _, e0, later := setupRollback(t)
		if err := w.Restore(entities.WorldSnapshot{}); err != nil {
			t.Fatalf("Restore(empty): %v", err)
		}
		next := w.Spawn()
		assertNoAlias(t, w, next, append([]entities.Entity{e0}, later...)...)
	})

	t.Run("double rollback preserves highest floor", func(t *testing.T) {
		w, _, e0, later := setupRollback(t)
		// Roll back a second time to generation 2 (still below generation 4).
		snap2 := entities.WorldSnapshot{
			Entities: []entities.EntitySnapshot{{Index: 0, Generation: 2}},
		}
		if err := w.Restore(snap2); err != nil {
			t.Fatalf("second rollback Restore: %v", err)
		}
		restored2 := entities.MakeEntity(0, 2)
		if !w.Alive(restored2) || w.Alive(e0) || w.Alive(later[1]) || w.Alive(later[2]) {
			t.Fatalf("unexpected liveness after second rollback")
		}
		w.Destroy(restored2)
		next := w.Spawn()
		assertNoAlias(t, w, next, append([]entities.Entity{e0}, later...)...)
	})

	t.Run("save and load checkpoint into progressed world", func(t *testing.T) {
		store := save.NewFileStore(t.TempDir())
		w := entities.NewWorld()
		registerGameCodecs(t, w)

		hero := w.Spawn()
		pet := w.Spawn()
		entities.Set(w, hero, Position{X: 1, Y: 2})
		entities.Set(w, pet, Parent{Target: hero})
		if err := entities.SaveWorld(store, "checkpoint", w); err != nil {
			t.Fatalf("SaveWorld: %v", err)
		}

		// Progress world with extra enemies and recycled slots.
		enemies := []entities.Entity{w.Spawn(), w.Spawn(), w.Spawn()}
		w.Destroy(pet)
		pet2 := w.Spawn() // same slot as pet, higher generation
		enemies = append(enemies, pet2)

		if err := entities.LoadWorld(store, "checkpoint", w); err != nil {
			t.Fatalf("LoadWorld: %v", err)
		}
		if !w.Alive(hero) || !w.Alive(pet) {
			t.Fatalf("restored checkpoint handles not alive: hero=%v pet=%v", w.Alive(hero), w.Alive(pet))
		}
		if p, ok := entities.Get[Parent](w, pet); !ok || p.Target != hero {
			t.Fatalf("restored cross-entity ref = (%v, %v), want %v", p, ok, hero)
		}
		// Destroy pet and spawn replacements; neither pet2 nor extra enemies may alias.
		w.Destroy(pet)
		for range 5 {
			n := w.Spawn()
			assertNoAlias(t, w, n, append([]entities.Entity{pet}, enemies...)...)
		}
	})
}

func TestGenerationWraparoundNeverProducesZero(t *testing.T) {
	t.Run("destroy wraps MaxUint32 to 1", func(t *testing.T) {
		w := entities.NewWorld()
		if err := w.Restore(entities.WorldSnapshot{
			Entities: []entities.EntitySnapshot{{Index: 0, Generation: math.MaxUint32}},
		}); err != nil {
			t.Fatalf("Restore: %v", err)
		}
		eMax := entities.MakeEntity(0, math.MaxUint32)
		if !w.Alive(eMax) {
			t.Fatalf("eMax=%v not alive", eMax)
		}
		w.Destroy(eMax)
		next := w.Spawn()
		if next.IsZero() || next.Index() != 0 || next.Generation() != 1 || !w.Alive(next) {
			t.Fatalf("after Destroy wrap: next=%v alive=%v, want Entity(0:1)", next, w.Alive(next))
		}
		if w.Alive(eMax) {
			t.Fatalf("eMax still alive after wrap")
		}
	})

	t.Run("reset wraps MaxUint32 to 1", func(t *testing.T) {
		w := entities.NewWorld()
		if err := w.Restore(entities.WorldSnapshot{
			Entities: []entities.EntitySnapshot{{Index: 0, Generation: math.MaxUint32}},
		}); err != nil {
			t.Fatalf("Restore: %v", err)
		}
		eMax := entities.MakeEntity(0, math.MaxUint32)
		w.Reset()
		next := w.Spawn()
		if next.IsZero() || next.Index() != 0 || next.Generation() != 1 || !w.Alive(next) {
			t.Fatalf("after Reset wrap: next=%v alive=%v, want Entity(0:1)", next, w.Alive(next))
		}
		if w.Alive(eMax) {
			t.Fatalf("eMax alive after Reset wrap")
		}
	})

	t.Run("restore empty and partial wrap MaxUint32 to 1", func(t *testing.T) {
		w := entities.NewWorld()
		if err := w.Restore(entities.WorldSnapshot{
			Entities: []entities.EntitySnapshot{
				{Index: 0, Generation: math.MaxUint32},
				{Index: 1, Generation: math.MaxUint32},
			},
		}); err != nil {
			t.Fatalf("Restore: %v", err)
		}
		e0Max := entities.MakeEntity(0, math.MaxUint32)
		e1Max := entities.MakeEntity(1, math.MaxUint32)

		// Omit slot 1 while keeping slot 0 at gen 5.
		if err := w.Restore(entities.WorldSnapshot{
			Entities: []entities.EntitySnapshot{{Index: 0, Generation: 5}},
		}); err != nil {
			t.Fatalf("Restore partial: %v", err)
		}
		n1 := w.Spawn()
		if n1.IsZero() || n1.Index() != 1 || n1.Generation() != 1 || !w.Alive(n1) {
			t.Fatalf("after partial Restore wrap: n1=%v alive=%v, want Entity(1:1)", n1, w.Alive(n1))
		}
		if w.Alive(e0Max) || w.Alive(e1Max) {
			t.Fatalf("MaxUint32 handles alive after partial Restore")
		}

		// Now restore MaxUint32 on slot 0 and follow with an empty Restore.
		if err := w.Restore(entities.WorldSnapshot{
			Entities: []entities.EntitySnapshot{{Index: 0, Generation: math.MaxUint32}},
		}); err != nil {
			t.Fatalf("Restore MaxUint32: %v", err)
		}
		if err := w.Restore(entities.WorldSnapshot{}); err != nil {
			t.Fatalf("Restore empty: %v", err)
		}
		n0 := w.Spawn()
		if n0.IsZero() || n0.Index() != 0 || n0.Generation() != 1 || !w.Alive(n0) {
			t.Fatalf("after empty Restore wrap: n0=%v alive=%v, want Entity(0:1)", n0, w.Alive(n0))
		}
	})

	t.Run("rollback floor near MaxUint32", func(t *testing.T) {
		w := entities.NewWorld()
		if err := w.Restore(entities.WorldSnapshot{
			Entities: []entities.EntitySnapshot{{Index: 0, Generation: math.MaxUint32 - 1}},
		}); err != nil {
			t.Fatalf("Restore MaxUint32-1: %v", err)
		}
		// Roll back to gen 1; floor must be MaxUint32.
		if err := w.Restore(entities.WorldSnapshot{
			Entities: []entities.EntitySnapshot{{Index: 0, Generation: 1}},
		}); err != nil {
			t.Fatalf("Restore rollback: %v", err)
		}
		w.Destroy(entities.MakeEntity(0, 1))
		eMax := w.Spawn()
		if eMax.Index() != 0 || eMax.Generation() != math.MaxUint32 {
			t.Fatalf("after floor destroy: got %v, want Entity(0:%d)", eMax, uint32(math.MaxUint32))
		}
		w.Destroy(eMax)
		wrapped := w.Spawn()
		if wrapped.IsZero() || wrapped.Index() != 0 || wrapped.Generation() != 1 {
			t.Fatalf("after MaxUint32 destroy: got %v, want Entity(0:1)", wrapped)
		}
	})
}

func TestFailedRestoreLeavesGenerationsUntouched(t *testing.T) {
	w := entities.NewWorld()
	e0 := w.Spawn()
	e1 := w.Spawn()

	err := w.Restore(entities.WorldSnapshot{
		Entities: []entities.EntitySnapshot{{
			Index:      0,
			Generation: 1,
			Components: []entities.ComponentSnapshot{{
				Type: "unregistered",
				Data: []byte(`{}`),
			}},
		}},
	})
	if !errors.Is(err, entities.ErrUnknownCodec) {
		t.Fatalf("Restore err = %v, want ErrUnknownCodec", err)
	}
	if !w.Alive(e0) || !w.Alive(e1) || w.Count() != 2 {
		t.Fatalf("failed Restore altered live state: alive=(%v,%v) count=%d", w.Alive(e0), w.Alive(e1), w.Count())
	}
	w.Destroy(e1)
	e1Next := w.Spawn()
	if e1Next.Index() != 1 || e1Next.Generation() != 2 {
		t.Fatalf("e1Next = %v, want Entity(1:2)", e1Next)
	}
}

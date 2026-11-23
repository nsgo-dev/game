package inventory_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/inventory"
)

type S = inventory.Stack

func newBag(t testing.TB, cat *inventory.Catalog, size int) *inventory.Container {
	t.Helper()
	c, err := inventory.NewContainer(cat, size)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func wantSlots(t *testing.T, c *inventory.Container, want ...S) {
	t.Helper()
	if got := c.Slots(); !reflect.DeepEqual(got, want) {
		t.Fatalf("slots = %v, want %v", got, want)
	}
}

func fill(t *testing.T, c *inventory.Container, stacks ...S) {
	t.Helper()
	st := inventory.ContainerState{}
	for i, s := range stacks {
		if !s.Empty() {
			st.Slots = append(st.Slots, inventory.SlotState{Slot: i, Item: s.Item, Count: s.Count})
		}
	}
	if err := c.Restore(st); err != nil {
		t.Fatal(err)
	}
}

func TestAddTopsUpStacksThenFillsEmptySlots(t *testing.T) {
	cat := testCatalog(t)
	bag := newBag(t, cat, 4)
	fill(t, bag, S{}, S{"potion", 15}, S{"arrow", 3}, S{"potion", 19})

	rem, err := bag.Add("potion", 12)
	if err != nil || rem != 0 {
		t.Fatalf("Add = %d, %v", rem, err)
	}
	// 5 top up slot 1, 1 tops up slot 3, the remaining 6 go to the first empty slot.
	wantSlots(t, bag, S{"potion", 6}, S{"potion", 20}, S{"arrow", 3}, S{"potion", 20})

	rem, err = bag.Add("potion", 30)
	if err != nil || rem != 16 {
		t.Fatalf("Add overflow = %d, %v; want 16 remaining", rem, err)
	}
	wantSlots(t, bag, S{"potion", 20}, S{"potion", 20}, S{"arrow", 3}, S{"potion", 20})
	if bag.Count("potion") != 60 || bag.Room("potion") != 0 || bag.Room("arrow") != 96 {
		t.Fatalf("Count/Room = %d/%d/%d", bag.Count("potion"), bag.Room("potion"), bag.Room("arrow"))
	}
	if bag.Room("sword") != 0 {
		t.Fatal("Room of unknown item is not 0")
	}
}

func TestAddSplitsLargeCountsAcrossEmptySlots(t *testing.T) {
	bag := newBag(t, testCatalog(t), 3)
	if rem, err := bag.Add("iron-helm", 2); err != nil || rem != 0 {
		t.Fatalf("Add helm = %d, %v", rem, err)
	}
	wantSlots(t, bag, S{"iron-helm", 1}, S{"iron-helm", 1}, S{})
	if rem, err := bag.Add("arrow", 150); err != nil || rem != 51 {
		t.Fatalf("Add arrows = %d, %v; want 51 remaining", rem, err)
	}
	wantSlots(t, bag, S{"iron-helm", 1}, S{"iron-helm", 1}, S{"arrow", 99})
}

func TestAddAllIsAllOrNothing(t *testing.T) {
	bag := newBag(t, testCatalog(t), 2)
	fill(t, bag, S{"potion", 10}, S{"old-key", 1})
	if err := bag.AddAll("potion", 11); !errors.Is(err, inventory.ErrFull) {
		t.Fatalf("AddAll over room = %v, want ErrFull", err)
	}
	wantSlots(t, bag, S{"potion", 10}, S{"old-key", 1})
	if err := bag.AddAll("potion", 10); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{"potion", 20}, S{"old-key", 1})
	if err := bag.AddAll("sword", 1); !errors.Is(err, inventory.ErrUnknownItem) {
		t.Fatalf("AddAll unknown = %v", err)
	}
}

func TestRemoveTakesFromLastSlotBackwards(t *testing.T) {
	bag := newBag(t, testCatalog(t), 4)
	fill(t, bag, S{"arrow", 50}, S{"potion", 2}, S{"arrow", 10}, S{"arrow", 5})
	if err := bag.Remove("arrow", 20); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{"arrow", 45}, S{"potion", 2}, S{}, S{})
	if err := bag.Remove("arrow", 46); !errors.Is(err, inventory.ErrNotEnough) {
		t.Fatalf("Remove too many = %v", err)
	}
	wantSlots(t, bag, S{"arrow", 45}, S{"potion", 2}, S{}, S{})
	if err := bag.Remove("arrow", -1); !errors.Is(err, inventory.ErrInvalidCount) {
		t.Fatalf("Remove -1 = %v", err)
	}
}

func TestRemoveAt(t *testing.T) {
	bag := newBag(t, testCatalog(t), 2)
	fill(t, bag, S{"arrow", 10})
	got, err := bag.RemoveAt(0, 4)
	if err != nil || got != (S{"arrow", 4}) {
		t.Fatalf("RemoveAt = %v, %v", got, err)
	}
	if _, err := bag.RemoveAt(0, 7); !errors.Is(err, inventory.ErrInvalidCount) {
		t.Fatalf("RemoveAt too many = %v", err)
	}
	if _, err := bag.RemoveAt(1, 1); !errors.Is(err, inventory.ErrEmptySlot) {
		t.Fatalf("RemoveAt empty = %v", err)
	}
	if _, err := bag.RemoveAt(0, 6); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{}, S{})
	if !bag.IsEmpty() {
		t.Fatal("IsEmpty = false")
	}
}

func TestMoveToEmptyMergeSwap(t *testing.T) {
	bag := newBag(t, testCatalog(t), 4)
	fill(t, bag, S{"potion", 15}, S{}, S{"potion", 12}, S{"arrow", 7})

	if err := bag.Move(0, 1); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{}, S{"potion", 15}, S{"potion", 12}, S{"arrow", 7})

	// Merge: slot 1 has room for 5 of the 12, the other 7 stay.
	if err := bag.Move(2, 1); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{}, S{"potion", 20}, S{"potion", 7}, S{"arrow", 7})

	// Onto a full stack of the same item: nothing changes.
	if err := bag.Move(2, 1); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{}, S{"potion", 20}, S{"potion", 7}, S{"arrow", 7})

	// Different item: swap.
	if err := bag.Move(3, 1); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{}, S{"arrow", 7}, S{"potion", 7}, S{"potion", 20})

	// Onto a full stack nothing moves; the other way round fills the partial stack.
	if err := bag.Move(2, 3); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{}, S{"arrow", 7}, S{"potion", 7}, S{"potion", 20})
	if err := bag.Move(3, 2); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{}, S{"arrow", 7}, S{"potion", 20}, S{"potion", 7})

	if err := bag.Move(1, 1); err != nil {
		t.Fatal(err)
	}
	if err := bag.Move(0, 1); !errors.Is(err, inventory.ErrEmptySlot) {
		t.Fatalf("Move from empty = %v", err)
	}
	if err := bag.Move(-1, 1); !errors.Is(err, inventory.ErrSlotOutOfRange) {
		t.Fatalf("Move from -1 = %v", err)
	}
}

func TestMoveAcrossContainers(t *testing.T) {
	cat := testCatalog(t)
	bag := newBag(t, cat, 2)
	chest := newBag(t, cat, 2)
	fill(t, bag, S{"arrow", 90}, S{"old-key", 1})
	fill(t, chest, S{"arrow", 20})
	if err := bag.MoveTo(0, chest, 0); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{"arrow", 11}, S{"old-key", 1})
	wantSlots(t, chest, S{"arrow", 99}, S{})
	if err := bag.MoveTo(1, chest, 0); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{"arrow", 11}, S{"arrow", 99})
	wantSlots(t, chest, S{"old-key", 1}, S{})

	other := newBag(t, testCatalog(t), 2)
	if err := bag.MoveTo(0, other, 0); !errors.Is(err, inventory.ErrCatalogMismatch) {
		t.Fatalf("MoveTo other catalog = %v", err)
	}
}

func TestSplit(t *testing.T) {
	bag := newBag(t, testCatalog(t), 4)
	fill(t, bag, S{"arrow", 40}, S{}, S{"arrow", 90}, S{"potion", 1})
	if err := bag.Split(0, 1, 15); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{"arrow", 25}, S{"arrow", 15}, S{"arrow", 90}, S{"potion", 1})
	if err := bag.Split(0, 2, 10); !errors.Is(err, inventory.ErrSlotOccupied) {
		t.Fatalf("Split over max = %v", err)
	}
	if err := bag.Split(0, 3, 1); !errors.Is(err, inventory.ErrSlotOccupied) {
		t.Fatalf("Split onto other item = %v", err)
	}
	if err := bag.Split(0, 2, 9); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{"arrow", 16}, S{"arrow", 15}, S{"arrow", 99}, S{"potion", 1})
	if err := bag.Split(0, 1, 17); !errors.Is(err, inventory.ErrInvalidCount) {
		t.Fatalf("Split too many = %v", err)
	}
	if err := bag.Split(0, 1, 0); !errors.Is(err, inventory.ErrInvalidCount) {
		t.Fatalf("Split 0 = %v", err)
	}
	if err := bag.Split(0, 1, 16); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{}, S{"arrow", 31}, S{"arrow", 99}, S{"potion", 1})
}

func TestTransferQuickMovesWhatFits(t *testing.T) {
	cat := testCatalog(t)
	bag := newBag(t, cat, 1)
	chest := newBag(t, cat, 2)
	fill(t, bag, S{"arrow", 80})
	fill(t, chest, S{"potion", 1}, S{"arrow", 50})
	moved, err := bag.Transfer(0, chest)
	if err != nil || moved != 49 {
		t.Fatalf("Transfer = %d, %v; want 49", moved, err)
	}
	wantSlots(t, bag, S{"arrow", 31})
	wantSlots(t, chest, S{"potion", 1}, S{"arrow", 99})
	if _, err := bag.Transfer(0, bag); err == nil {
		t.Fatal("Transfer into itself accepted")
	}
	if _, err := chest.Transfer(0, bag); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, chest, S{"potion", 1}, S{"arrow", 99})
}

func TestContainerRestoreRejectsBadStateUnchanged(t *testing.T) {
	bag := newBag(t, testCatalog(t), 3)
	fill(t, bag, S{"arrow", 5})
	bad := []inventory.ContainerState{
		{Slots: []inventory.SlotState{{Slot: 3, Item: "arrow", Count: 1}}},
		{Slots: []inventory.SlotState{{Slot: -1, Item: "arrow", Count: 1}}},
		{Slots: []inventory.SlotState{{Slot: 0, Item: "sword", Count: 1}}},
		{Slots: []inventory.SlotState{{Slot: 0, Item: "arrow", Count: 0}}},
		{Slots: []inventory.SlotState{{Slot: 0, Item: "arrow", Count: 100}}},
		{Slots: []inventory.SlotState{{Slot: 1, Item: "arrow", Count: 1}, {Slot: 1, Item: "potion", Count: 1}}},
	}
	for i, st := range bad {
		if err := bag.Restore(st); err == nil {
			t.Fatalf("Restore %d accepted %+v", i, st)
		}
		wantSlots(t, bag, S{"arrow", 5}, S{}, S{})
	}
	if err := bag.Restore(inventory.ContainerState{}); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{}, S{}, S{})
}

// TestRandomOperationsKeepInvariants runs a seeded sequence of every operation over two containers and checks after each step that no stack exceeds its maximum, empty slots are zero values, and items are only created by Add and destroyed by Remove.
func TestRandomOperationsKeepInvariants(t *testing.T) {
	cat := testCatalog(t)
	items := []string{"potion", "arrow", "iron-helm", "gold-ring", "silver-ring", "old-key"}
	a := newBag(t, cat, 6)
	b := newBag(t, cat, 4)
	gear := newGear(t, cat)
	rng := rand.New(rand.NewPCG(515, 1))
	total := map[string]int{}
	for step := range 5000 {
		item := items[rng.IntN(len(items))]
		n := rng.IntN(40) + 1
		src, dst := a, b
		if rng.IntN(2) == 0 {
			src, dst = b, a
		}
		i, j := rng.IntN(src.Len()), rng.IntN(dst.Len())
		slot := gearSlots[rng.IntN(len(gearSlots))].Name
		switch rng.IntN(9) {
		case 0:
			rem, err := src.Add(item, n)
			if err != nil {
				t.Fatal(err)
			}
			total[item] += n - rem
		case 1:
			if err := src.Remove(item, n); err == nil {
				total[item] -= n
			}
		case 2:
			if s, err := src.RemoveAt(i, n); err == nil {
				total[s.Item] -= s.Count
			}
		case 3:
			_ = src.MoveTo(i, dst, j)
		case 4:
			_ = src.SplitTo(i, dst, j, n)
		case 5:
			_, _ = src.Transfer(i, dst)
		case 6:
			_ = src.Move(i, rng.IntN(src.Len()))
		case 7:
			_ = gear.Equip(slot, src, i)
		case 8:
			_ = gear.Unequip(slot, dst)
		}
		for _, c := range []*inventory.Container{a, b} {
			for k, s := range c.Slots() {
				if s == (S{}) {
					continue
				}
				def, ok := cat.Item(s.Item)
				if !ok || s.Count < 1 || s.Count > def.EffectiveMaxStack() {
					t.Fatalf("step %d: slot %d holds invalid stack %v", step, k, s)
				}
			}
		}
		equipped := map[string]int{}
		for _, s := range gearSlots {
			if it, ok := gear.Equipped(s.Name); ok {
				if !gear.CanEquip(s.Name, it) {
					t.Fatalf("step %d: %q equipped in %q", step, it, s.Name)
				}
				equipped[it]++
			}
		}
		for _, it := range items {
			if got := a.Count(it) + b.Count(it) + equipped[it]; got != total[it] {
				t.Fatalf("step %d: %s count = %d, want %d", step, it, got, total[it])
			}
		}
	}
}

func BenchmarkAddRemove(b *testing.B) {
	cat := testCatalog(b)
	bag := newBag(b, cat, 40)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := bag.Add("arrow", 500); err != nil {
			b.Fatal(err)
		}
		if err := bag.Remove("arrow", 500); err != nil {
			b.Fatal(err)
		}
	}
}

func ExampleContainer_Add() {
	cat, _ := inventory.Parse([]byte("version: 1\nitems:\n  - id: arrow\n    maxStack: 99\n"))
	quiver, _ := inventory.NewContainer(cat, 2)
	left, _ := quiver.Add("arrow", 250)
	fmt.Println(quiver.Slots(), left)
	// Output: [{arrow 99} {arrow 99}] 52
}

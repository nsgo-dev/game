package inventory_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/inventory"
	"github.com/nsgo-dev/game/pkg/save"
)

var gearSlots = []inventory.EquipSlot{
	{Name: "head", Kind: "head"},
	{Name: "ring-left", Kind: "ring"},
	{Name: "ring-right", Kind: "ring"},
}

func newGear(t testing.TB, cat *inventory.Catalog) *inventory.Equipment {
	t.Helper()
	e, err := inventory.NewEquipment(cat, gearSlots...)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func wantEquipped(t *testing.T, e *inventory.Equipment, slot, item string) {
	t.Helper()
	got, ok := e.Equipped(slot)
	if got != item || ok != (item != "") {
		t.Fatalf("Equipped(%q) = %q, %v; want %q", slot, got, ok, item)
	}
}

func TestNewEquipmentValidatesSlots(t *testing.T) {
	cat := testCatalog(t)
	bad := [][]inventory.EquipSlot{
		{{Name: "", Kind: "head"}},
		{{Name: "head", Kind: ""}},
		{{Name: "main hand", Kind: "weapon"}},
		{{Name: "head", Kind: "head"}, {Name: "head", Kind: "hat"}},
	}
	for _, slots := range bad {
		if _, err := inventory.NewEquipment(cat, slots...); err == nil {
			t.Fatalf("NewEquipment accepted %v", slots)
		}
	}
	if _, err := inventory.NewEquipment(nil); err == nil {
		t.Fatal("NewEquipment(nil) accepted")
	}
	e := newGear(t, cat)
	if !reflect.DeepEqual(e.Slots(), gearSlots) {
		t.Fatalf("Slots = %v", e.Slots())
	}
}

func TestEquipChecksKindAndSwaps(t *testing.T) {
	cat := testCatalog(t)
	bag := newBag(t, cat, 3)
	gear := newGear(t, cat)
	fill(t, bag, S{"gold-ring", 1}, S{"silver-ring", 1}, S{"potion", 3})

	if err := gear.Equip("head", bag, 0); !errors.Is(err, inventory.ErrNotEquippable) {
		t.Fatalf("ring on head = %v", err)
	}
	if err := gear.Equip("ring-left", bag, 2); !errors.Is(err, inventory.ErrNotEquippable) {
		t.Fatalf("potion on ring = %v", err)
	}
	if err := gear.Equip("feet", bag, 0); !errors.Is(err, inventory.ErrUnknownEquipSlot) {
		t.Fatalf("unknown slot = %v", err)
	}
	if !gear.CanEquip("ring-right", "gold-ring") || gear.CanEquip("head", "gold-ring") || gear.CanEquip("nope", "gold-ring") {
		t.Fatal("CanEquip wrong")
	}

	if err := gear.Equip("ring-left", bag, 0); err != nil {
		t.Fatal(err)
	}
	wantEquipped(t, gear, "ring-left", "gold-ring")
	wantSlots(t, bag, S{}, S{"silver-ring", 1}, S{"potion", 3})

	// Equipping the silver ring into the same slot puts the gold ring where the silver one was.
	if err := gear.Equip("ring-left", bag, 1); err != nil {
		t.Fatal(err)
	}
	wantEquipped(t, gear, "ring-left", "silver-ring")
	wantSlots(t, bag, S{}, S{"gold-ring", 1}, S{"potion", 3})

	// Two slots share the ring kind.
	if err := gear.Equip("ring-right", bag, 1); err != nil {
		t.Fatal(err)
	}
	wantEquipped(t, gear, "ring-right", "gold-ring")
	wantEquipped(t, gear, "head", "")
	if err := gear.Equip("ring-right", bag, 0); !errors.Is(err, inventory.ErrEmptySlot) {
		t.Fatalf("Equip from empty = %v", err)
	}
}

func TestEquipFromStackReturnsOldItemElsewhere(t *testing.T) {
	cat, err := inventory.NewCatalog(
		inventory.ItemDef{ID: "torch", MaxStack: 5, Equip: "hand"},
		inventory.ItemDef{ID: "lantern", Equip: "hand"},
		inventory.ItemDef{ID: "rock", MaxStack: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	bag := newBag(t, cat, 2)
	gear, err := inventory.NewEquipment(cat, inventory.EquipSlot{Name: "hand", Kind: "hand"})
	if err != nil {
		t.Fatal(err)
	}
	fill(t, bag, S{"torch", 3}, S{"lantern", 1})
	if err := gear.Equip("hand", bag, 1); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{"torch", 3}, S{})

	// One torch comes off a stack of three; the lantern goes to the empty slot.
	if err := gear.Equip("hand", bag, 0); err != nil {
		t.Fatal(err)
	}
	wantEquipped(t, gear, "hand", "torch")
	wantSlots(t, bag, S{"torch", 2}, S{"lantern", 1})

	// Equipping the same item again changes nothing.
	if err := gear.Equip("hand", bag, 0); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{"torch", 2}, S{"lantern", 1})

	// The lantern's slot empties as it is equipped, so the torch goes back into that slot.
	if err := gear.Equip("hand", bag, 1); err != nil {
		t.Fatal(err)
	}
	wantEquipped(t, gear, "hand", "lantern")
	wantSlots(t, bag, S{"torch", 2}, S{"torch", 1})
	if err := bag.Move(1, 0); err != nil {
		t.Fatal(err)
	}
	wantSlots(t, bag, S{"torch", 3}, S{})

	// No room for the lantern to come back while the torch stack stays: refused, nothing changes.
	if _, err := bag.Add("rock", 1); err != nil {
		t.Fatal(err)
	}
	if err := gear.Equip("hand", bag, 0); !errors.Is(err, inventory.ErrFull) {
		t.Fatalf("Equip without room = %v, want ErrFull", err)
	}
	wantEquipped(t, gear, "hand", "lantern")
	wantSlots(t, bag, S{"torch", 3}, S{"rock", 1})

	if err := gear.Unequip("hand", bag); !errors.Is(err, inventory.ErrFull) {
		t.Fatalf("Unequip without room = %v, want ErrFull", err)
	}
	wantEquipped(t, gear, "hand", "lantern")
	if err := bag.Remove("rock", 1); err != nil {
		t.Fatal(err)
	}
	if err := gear.Unequip("hand", bag); err != nil {
		t.Fatal(err)
	}
	wantEquipped(t, gear, "hand", "")
	wantSlots(t, bag, S{"torch", 3}, S{"lantern", 1})
	if err := gear.Unequip("hand", bag); !errors.Is(err, inventory.ErrEmptySlot) {
		t.Fatalf("Unequip empty = %v", err)
	}
	other := newBag(t, testCatalog(t), 1)
	if err := gear.Equip("hand", other, 0); !errors.Is(err, inventory.ErrCatalogMismatch) {
		t.Fatalf("Equip other catalog = %v", err)
	}
}

func TestEquipmentRestoreRejectsBadStateUnchanged(t *testing.T) {
	cat := testCatalog(t)
	gear := newGear(t, cat)
	if err := gear.Restore(inventory.EquipmentState{Slots: []inventory.EquippedState{{Slot: "head", Item: "iron-helm"}}}); err != nil {
		t.Fatal(err)
	}
	bad := []inventory.EquipmentState{
		{Slots: []inventory.EquippedState{{Slot: "feet", Item: "iron-helm"}}},
		{Slots: []inventory.EquippedState{{Slot: "head", Item: "sword"}}},
		{Slots: []inventory.EquippedState{{Slot: "head", Item: "gold-ring"}}},
		{Slots: []inventory.EquippedState{{Slot: "ring-left", Item: "gold-ring"}, {Slot: "ring-left", Item: "silver-ring"}}},
	}
	for i, st := range bad {
		if err := gear.Restore(st); err == nil {
			t.Fatalf("Restore %d accepted %+v", i, st)
		}
		wantEquipped(t, gear, "head", "iron-helm")
		wantEquipped(t, gear, "ring-left", "")
	}
}

// playerSave is how a game stores its inventory next to the rest of its save data.
type playerSave struct {
	Level int                      `json:"level"`
	Bag   inventory.ContainerState `json:"bag"`
	Gear  inventory.EquipmentState `json:"gear"`
}

func TestInventoryRoundTripsThroughSave(t *testing.T) {
	cat := testCatalog(t)
	bag := newBag(t, cat, 5)
	gear := newGear(t, cat)

	// Play a little: pick things up, stack, move, split and equip.
	for _, step := range []func() error{
		func() error { _, err := bag.Add("potion", 23); return err },
		func() error { return bag.AddAll("iron-helm", 1) },
		func() error { return bag.AddAll("gold-ring", 1) },
		func() error { _, err := bag.Add("arrow", 40); return err },
		func() error { return bag.Move(1, 4) },
		func() error { return gear.Equip("head", bag, 2) },
		func() error { return gear.Equip("ring-right", bag, 3) },
		func() error { return bag.Split(4, 2, 2) },
		func() error { return bag.Remove("potion", 1) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	wantSlots(t, bag, S{"potion", 20}, S{"arrow", 40}, S{"potion", 2}, S{}, S{})
	wantEquipped(t, gear, "head", "iron-helm")
	wantEquipped(t, gear, "ring-right", "gold-ring")

	dir := t.TempDir()
	store := save.NewFileStore(dir)
	if err := store.Save("slot1", playerSave{Level: 4, Bag: bag.Snapshot(), Gear: gear.Snapshot()}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "slot1.json"))
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"version":1,"data":{"level":4,"bag":{"slots":[{"slot":0,"item":"potion","count":20},{"slot":1,"item":"arrow","count":40},{"slot":2,"item":"potion","count":2}]},"gear":{"slots":[{"slot":"head","item":"iron-helm"},{"slot":"ring-right","item":"gold-ring"}]}}}`
	if string(raw) != want {
		t.Fatalf("save file =\n%s\nwant\n%s", raw, want)
	}

	var loaded playerSave
	if err := save.NewFileStore(dir).Load("slot1", &loaded); err != nil {
		t.Fatal(err)
	}
	bag2 := newBag(t, cat, 5)
	gear2 := newGear(t, cat)
	if err := bag2.Restore(loaded.Bag); err != nil {
		t.Fatal(err)
	}
	if err := gear2.Restore(loaded.Gear); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bag2.Slots(), bag.Slots()) {
		t.Fatalf("restored bag = %v, want %v", bag2.Slots(), bag.Slots())
	}
	if !reflect.DeepEqual(gear2.Snapshot(), gear.Snapshot()) || loaded.Level != 4 {
		t.Fatalf("restored gear = %+v, want %+v", gear2.Snapshot(), gear.Snapshot())
	}

	// The restored inventory keeps behaving: unequip into it and stack.
	if err := gear2.Unequip("head", bag2); err != nil {
		t.Fatal(err)
	}
	if rem, err := bag2.Add("potion", 18); err != nil || rem != 0 {
		t.Fatalf("Add after restore = %d, %v", rem, err)
	}
	wantSlots(t, bag2, S{"potion", 20}, S{"arrow", 40}, S{"potion", 20}, S{"iron-helm", 1}, S{})

	// An empty inventory saves as empty lists, not null, and restores to empty.
	empty, err := json.Marshal(playerSave{Bag: newBag(t, cat, 1).Snapshot(), Gear: newGear(t, cat).Snapshot()})
	if err != nil {
		t.Fatal(err)
	}
	if string(empty) != `{"level":0,"bag":{"slots":[]},"gear":{"slots":[]}}` {
		t.Fatalf("empty snapshot JSON = %s", empty)
	}
	gear2.Clear()
	bag2.Clear()
	if !bag2.IsEmpty() || len(gear2.Snapshot().Slots) != 0 {
		t.Fatal("Clear left items behind")
	}
}

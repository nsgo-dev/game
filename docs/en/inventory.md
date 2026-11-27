# inventory

`game/inventory` holds gameplay items: item definitions in versioned YAML, slot-based containers of stacks, typed equipment slots, and snapshots that round-trip through [`game/save`](save.md).

```go
import "github.com/nsgo-dev/game/pkg/inventory"
```

## Item definitions

A game's items live in one YAML file. Every definition has an `id`; the other fields are optional:

```yaml
version: 1
items:
  - id: potion
    name: items.potion.name
    description: items.potion.description
    icon: asset:icons/potion.png
    maxStack: 20
    tags: [consumable, healing]
    stats:
      heal: 25
      price: 12.5
  - id: iron-helm
    name: items.iron-helm.name
    equip: head
    stats:
      armor: 3
  - id: gold-ring
    equip: ring
```

| Field | Meaning |
| --- | --- |
| `id` | Unique ID that stacks and saves refer to; no whitespace or control characters. |
| `name`, `description` | Display text or [l10n](l10n.md) keys. |
| `icon` | Asset URI of the item's icon. |
| `maxStack` | How many fit in one slot, from 1 to 2^30; omitted means 1. |
| `equip` | The equipment kind the item fits, such as `head` or `ring`; omitted means it cannot be equipped. |
| `tags` | Unique free-form labels; `ItemDef.HasTag` checks one. |
| `stats` | Finite numbers such as damage, armor, weight or price. |

`inventory.Parse(data)` returns a `*Catalog`. It refuses a missing or unsupported `version`, unknown fields, duplicate IDs or tags, invalid stack sizes, non-finite stats and trailing YAML documents. `Catalog.Encode` writes deterministic 2-space YAML (definitions in file order, stat names sorted) that `Parse` reads back unchanged, so an editor or tool can rewrite the file without noise in review. `inventory.NewCatalog(defs...)` builds a catalog in code with the same checks. A catalog is immutable: `Item(id)` and `Items()` return copies.

## Containers

A `Container` is a fixed number of slots; each slot is empty or holds one `Stack{Item, Count}` no larger than the item's `maxStack`.

```go
cat, err := inventory.Parse(itemsYAML)
bag, err := inventory.NewContainer(cat, 24)

left, err := bag.Add("potion", 30) // tops up potion stacks, then fills empty slots; left did not fit
err = bag.AddAll("iron-helm", 1)    // everything or nothing: ErrFull when it does not fit
err = bag.Remove("potion", 5)       // from the last slot holding potions backwards; ErrNotEnough otherwise
n := bag.Count("potion")
room := bag.Room("potion")
```

Moving items follows the rules players expect from a grid inventory:

| Call | Behavior |
| --- | --- |
| `Move(from, to)` / `MoveTo(from, dst, to)` | Into an empty slot the stack moves; onto the same item as many as fit merge and the rest stay; onto a different item the stacks swap. |
| `Split(from, to, n)` / `SplitTo(from, dst, to, n)` | Moves `n` items into an empty slot or onto the same item with room for all `n`; `ErrSlotOccupied` otherwise. |
| `Transfer(from, dst)` | Quick-moves a stack into another container the way `Add` does; what does not fit stays. |
| `RemoveAt(slot, n)` | Takes `n` items out of one slot and returns them. |

`dst` may be another container (a chest, a shop) built from the same catalog; containers from different catalogs return `ErrCatalogMismatch`. Every call that returns an error leaves both containers unchanged.

## Equipment

`Equipment` is a set of named slots, each accepting one kind. Several slots may share a kind:

```go
gear, err := inventory.NewEquipment(cat,
	inventory.EquipSlot{Name: "head", Kind: "head"},
	inventory.EquipSlot{Name: "ring-left", Kind: "ring"},
	inventory.EquipSlot{Name: "ring-right", Kind: "ring"},
)

err = gear.Equip("head", bag, slot) // takes one item from bag[slot]
err = gear.Unequip("head", bag)
item, ok := gear.Equipped("head")
```

`Equip` checks that the item's `equip` matches the slot's kind (`ErrNotEquippable` otherwise) and takes one item from the container slot. An item already equipped goes back to the container: into the same slot when taking the item emptied it, otherwise wherever `AddAll` would put it. When the container has no room for it, `Equip` returns `ErrFull` and nothing changes; `Unequip` does the same.

## Saving

`Container.Snapshot` and `Equipment.Snapshot` return plain JSON-tagged values listing only the occupied slots, in slot order. Store them in your save data with a `save.Store`, and `Restore` them into a container or equipment built the same way:

```go
type Progress struct {
	Level int                      `json:"level"`
	Bag   inventory.ContainerState `json:"bag"`
	Gear  inventory.EquipmentState `json:"gear"`
}

err := store.Save("slot1", Progress{Level: 4, Bag: bag.Snapshot(), Gear: gear.Snapshot()})

var p Progress
if err := store.Load("slot1", &p); err != nil {
	return err
}
if err := bag.Restore(p.Bag); err != nil {
	return err
}
if err := gear.Restore(p.Gear); err != nil {
	return err
}
```

The saved JSON is deterministic:

```json
{"level":4,"bag":{"slots":[{"slot":0,"item":"potion","count":20},{"slot":1,"item":"arrow","count":40}]},"gear":{"slots":[{"slot":"head","item":"iron-helm"}]}}
```

`Restore` validates the whole snapshot before replacing anything: an unknown item, a count above the item's `maxStack`, an out-of-range or repeated slot, or an item in an equipment slot of the wrong kind returns an error and leaves the container or equipment as it was. When an update renames or removes items, migrate the save with a `save.Migration` before restoring it.

## Errors

| Error | Meaning |
| --- | --- |
| `ErrUnknownItem` | The catalog does not define the item. |
| `ErrInvalidCount` | A count is not positive, or larger than the stack or the item's `maxStack`. |
| `ErrSlotOutOfRange` | A container slot index is outside `[0, Len)`. |
| `ErrEmptySlot` | The slot holds nothing. |
| `ErrSlotOccupied` | The destination holds a different item, or lacks room for a split. |
| `ErrFull` | The container has no room for all of the items. |
| `ErrNotEnough` | The container holds fewer of the item than asked. |
| `ErrCatalogMismatch` | The containers or equipment use different catalogs. |
| `ErrUnknownEquipSlot` | The equipment has no slot with that name. |
| `ErrNotEquippable` | The item's kind does not match the equipment slot. |

Containers and equipment are deterministic and hold no global state, so the same code runs on a headless [server](server.md). They are not safe for concurrent use.

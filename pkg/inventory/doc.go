// Package inventory holds gameplay items: item definitions read from versioned YAML, slot-based containers of item stacks, typed equipment slots, and snapshots that round-trip through a game/save Store.
//
// # Item definitions
//
// A Catalog is the set of ItemDef values a game knows, parsed from a version 1 YAML document with Parse (unknown fields, duplicate IDs and trailing documents are refused) or built in code with NewCatalog. Catalog.Encode writes deterministic 2-space YAML that Parse reads back unchanged. Each definition names its maximum stack size (1 when omitted), the equipment kind it fits, if any, and free-form tags and numeric stats.
//
// # Containers
//
// A Container is a fixed number of slots, each empty or holding one Stack of a single item no larger than that item's maximum stack. Add tops up existing stacks of the item in slot order before filling empty slots in slot order, and returns what did not fit; AddAll adds everything or nothing. Remove takes items from the last matching slot backwards. MoveTo moves a whole stack into an empty slot, merges it into a stack of the same item as far as that stack has room, or swaps it with a different item; SplitTo moves part of a stack; Transfer quick-moves a stack into another container. Every operation that returns an error leaves both containers unchanged.
//
// # Equipment
//
// Equipment is a set of named slots, each accepting items whose ItemDef.Equip equals the slot's kind (two "ring" slots can share one kind). Equip takes one item from a container slot and returns the item it replaces to the container; Unequip returns the equipped item to a container. Both are all-or-nothing.
//
// # Saving
//
// Container.Snapshot and Equipment.Snapshot return plain JSON-tagged values to store in a save slot alongside the rest of a game's state; Restore validates a snapshot against the catalog (unknown items, oversized stacks, out-of-range slots and wrong equipment kinds are refused) before replacing anything.
//
// Nothing in inventory reads the clock, spawns goroutines or keeps package-level state, so it is deterministic and safe to run on a headless server. A Container or Equipment is not safe for concurrent use.
package inventory

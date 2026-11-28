# AI smart objects

`ai.SmartObjects` is a deterministic registry for usable world objects. Each object exposes tagged slots and a behavior name. `FindInRadius` returns free matching slots in stable object/slot order, `Claim` prevents double use, `Disable` releases every claim through the callback, and `Snapshot`/`Restore` preserve occupancy for saves.

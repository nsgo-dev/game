# Turn battles

`pkg/battle` is a deterministic, presentation-independent runner for a party and troop. `Order` sorts living battlers by agility with stable index ties. `Resolve` validates scopes and MP, consumes the skill cost, uses the supplied seeded random stream for hit rolls, and emits action, miss, damage, heal, defeat and terminal events. `Snapshot` and `Restore` support replay and save checkpoints.

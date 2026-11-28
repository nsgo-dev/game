### Added

- `pkg/battle` is a deterministic turn-based battle runner: `battle.New` copies a party and a troop, `Runner.Resolve` runs one turn of commands from either side in agility order (ties by side, then index), resolves the self, one-enemy, all-enemies, one-ally and all-allies scopes with MP costs, hit and critical rolls on a `random.Stream`, and returns damaged, healed, missed, defeated, won and lost events, and `Runner.Snapshot` and `Runner.Restore` save and replay a battle mid-fight (part of #1757).

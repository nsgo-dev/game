### Added

- `game/framedata`: per-frame hitboxes, hurtboxes, damage, hitstun, blockstun, knockback and cancel windows for fighting game moves in versioned YAML, resolved into hits deterministically each fixed step, with frame advantage summaries, `World.Save`/`Restore` for replay and rollback, and `debug` overlay drawing (#526).

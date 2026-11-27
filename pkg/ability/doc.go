// Package ability runs gameplay abilities with costs, cooldowns, charges, cast and channel times and conditions, activated by input actions and advanced one fixed game.Loop step at a time.
//
// # Ability Sets
//
// A Set is a versioned YAML file listing abilities. Parse decodes and validates one, refusing unknown fields, duplicate IDs and out-of-range values; Encode writes deterministic YAML that Parse reads back to an equal Set.
//
// # Casters
//
// A Caster runs one character's abilities. NewCaster turns every duration in a Set into whole steps at the loop's rate, rounding up, and Caster.Update advances cooldowns, recharges and the active cast or channel by exactly one step, then activates abilities whose input actions were pressed. Because every timer counts whole steps, the same inputs on the same steps give the same events on the same steps on every machine, and Snapshot and Restore carry that state through save slots, rollback and replays.
//
// Costs are paid through a Resources (a Pool, or an adapter over the game's own stats), conditions through a Condition function, and a game applies an ability's effect when it receives EventActivated through OnEvent.
package ability

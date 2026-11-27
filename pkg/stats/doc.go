// Package stats holds gameplay stats: named base values changed by additive, multiplicative and override modifiers that come from sources such as equipment and buffs, with timed modifiers counted in fixed steps.
//
// A Sheet keeps one entity's stats. Every change to a base value or a modifier recomputes the stats it touches at once, reports each value that changed through Sheet.OnChange, and leaves Sheet.Value a cached read. A stat's value is
//
//	clamp((base + sum of add values) * product of mul values)
//
// or, while any override modifier is present, clamp(the override with the highest priority, the latest added among equal priorities). Adds and muls are applied in the order they were added; with float64 arithmetic that order can change the last digits of a result (0.1 + 0.2 + 0.3 is not exactly 0.3 + 0.1 + 0.2). A sum or product that overflows float64 saturates at the largest finite value of its sign, so values are always finite and within their bounds. Sheet.Refresh replaces a source's modifiers in a single change. Timed modifiers count calls of Sheet.Step, which a game makes once per fixed step of its game.Loop, so a modifier that lasts n steps is removed by exactly the n-th Step after it was added.
//
// Stat definitions and named effects (groups of modifiers, such as an item's bonuses or a buff) are written in a versioned YAML file read with Parse and written with Encode.
//
// Nothing in stats reads the wall clock, spawns goroutines or keeps package-level state, so sheets are deterministic and replay alike.
package stats

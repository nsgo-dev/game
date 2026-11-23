// Package bullets spawns, moves and draws the bullet patterns of shooters: rings, spirals and aimed bursts defined in versioned YAML.
//
// # Pattern Files
//
// A pattern file holds named patterns. Parse decodes one, refusing unknown fields, unsupported versions and invalid values, and Encode writes it back as deterministic YAML that round-trips through Parse:
//
//	version: 1
//	patterns:
//	  - name: flower
//	    kind: spiral
//	    frame: orb
//	    count: 6
//	    speed: 140
//	    spin: 11
//	    shots: 0
//	    interval: 60ms
//	    lifetime: 5s
//	    radius: 4
//
// Angles are in degrees, clockwise from +X on the screen; speeds are in points a second; durations are Go durations such as 60ms or 2s.
//
// # Fields and Emitters
//
// A Field owns the live bullets: it takes them from a perf.Pool, moves them in Update, removes those that outlive their lifetime or leave its Bounds, and draws them through one sprite.Batch. An Emitter fires one pattern's shots into a field at the pattern's times, drawing any jitter from its own seeded random.Stream, so an emitter with the same pattern, seed and steps spawns the same bullets.
//
// Each fixed step, update the field first and then its emitters: an emitter places the bullets of a shot fired partway through the step where they are at the step's end.
package bullets

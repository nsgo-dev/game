// Package loot rolls nested weighted drop tables with guaranteed drops and pity counters on the deterministic streams of package random.
//
// # Tables
//
// A Document is the versioned YAML text a game's author writes: a list of named tables. Each Table holds weighted Entries, of which it picks Rolls times, and Guaranteed entries it drops every time it is rolled. An Entry drops an item, rolls another table (nesting), or drops nothing. Parse decodes and validates a Document, refusing unknown fields, unsupported versions, unknown or cyclic table references and runaway sizes; Encode writes the same Document back as deterministic YAML that parses to an equal value.
//
// # Rolling
//
// New compiles a Document into an immutable Library. A Roller rolls a Library's tables on one random.Stream, so a seed and a sequence of rolls always produce the same drops on every platform. A Library can be shared by many Rollers; each Roller is used by one goroutine.
//
// # Pity
//
// A table's Pity rules raise the chance of one of its entries after a run of picks without it. A hard rule (GuaranteeAt) forces the entry on the GuaranteeAt-th pick since it was last picked; a soft rule (SoftFrom, SoftStep) adds SoftStep to the entry's weight for every pick from the SoftFrom-th one on. A Roller keeps a miss counter per rule; State and SetState copy the counters out and back in for save slots, beside the stream's own random.State.
package loot

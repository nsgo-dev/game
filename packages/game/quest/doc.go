// Package quest runs quests with objectives (collect, reach, defeat, talk), quest states and rewards, defined in a versioned YAML Book and advanced by game events.
//
// A Book is the text file a designer writes: each Quest lists its objectives, the quests it requires, whether its objectives run in order, and the rewards it grants. Parse refuses unknown fields and checks references and prerequisite cycles; Encode writes deterministic YAML that parses back to the same Book.
//
// A Log tracks one player's progress through a Book. Events (Collected, Reached, Defeated, Talked, or a plain Event) advance the objectives of active quests whose kind and target match; Attach wires a Log to a flow.Bus so those events can be published from anywhere in a game, and every change is reported as an Update. Snapshot and Restore turn a Log's progress into a JSON-friendly value that round-trips through save.Store.
//
// Nothing in quest reads the wall clock, spawns goroutines or keeps package-level state, so a Log stays deterministic under nsgotest and replays.
package quest

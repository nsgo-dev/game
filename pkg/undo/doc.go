// Package undo records the reversible moves of puzzle and turn-based games, with linear undo and redo.
//
// A History owns a game's state and a list of moves. Do applies a move and records it, Undo reverts the last applied move, and Redo applies the last undone one again; a new move after an undo discards the moves that could have been redone. Every move is kept until Clear, so a player can undo back to the start of a level however long they played.
//
// In-game editors, such as level makers and character creators, get three more tools. DoMerge folds a move into the latest one when it is a Merger that accepts it, so one drag of a slider is one move to undo. Limit bounds how many moves are kept, forgetting the oldest applied ones first. MarkSaved and Dirty track whether the state differs from the last saved one, and Version identifies the current point in the history.
//
// Each Move both applies and reverts its change, so undoing every move returns the state to exactly where it started as long as each Revert is the exact inverse of its Apply. A move may record what its Apply changed (the crate it pushed, the piece it captured) in its own fields and read it back in Revert.
//
// A History whose moves are one concrete type can be saved as a versioned YAML Log and restored from it, which replays the applied moves on the starting state and keeps the undone ones for Redo.
//
// Nothing in undo reads the clock, spawns goroutines or keeps package-level state.
package undo

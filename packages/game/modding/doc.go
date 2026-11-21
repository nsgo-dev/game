// Package modding loads and runs sandboxed TSX/JS game mods on top of NSGo's tsx runtime.
//
// A mod lives in a directory containing a mod.json manifest ([ModManifest]) and an entry script
// (such as mod.tsx or mod.js). Mods import the capability-gated "game" module to subscribe to
// simulation steps and events, emit events, spawn entities, read and write scoped game state,
// play audio, persist per-mod storage, write logs, and optionally mount TSX UI panels or HUD
// widgets.
//
// [Manager] discovers mods from the filesystem or any [io/fs.FS], validates manifests, resolves
// dependencies in deterministic topological order, isolates faults so a broken mod never crashes
// the host game or sibling mods, persists enabled/disabled state via [core.Storage], and drives
// step hooks from [game.Loop].
package modding

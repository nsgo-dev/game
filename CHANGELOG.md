# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- `game`: fixed-step `Loop` built on NSGo's `RequestAnimationFrame`, pausing in the background and interpolating draw frames.
- `game/sprite`: atlases, frames, animations, sprites and batches drawn into a `Canvas`.
- `game/sound`: `Mixer` for sound effects and music, with background pause and resume.
- Create a runnable starter game with `nsgo init -template github.com/nsgo-dev/game/template` (#38 by @houko).
- Website and documentation in English and Simplified Chinese (`apps/website`, `docs/en`, `docs/zh`).

### Fixed

- `game/assets`: `asset:` URIs are confined to the `assets` directory (and `file://` is refused while a pack is mounted), downloaded packs are capped by entry count and unpacked size before mounting, mounting a pack into a reused group resets the group's previous specs, and `Group.Reset` cancels an in-flight load and forgets old specs.
- `game/modding`: a reload adopts the re-resolved topological order and reports (rather than swallows) a manifest read error.

- `game/editor`: the 3D gizmo drags now start from real pointer input on the mounted viewport (a press that becomes a pan picks a handle, then moves it) instead of being reachable only from the model API.
- `game/entities`: world snapshots keep parent-child links, restored dead slots advance their generation so stale handles cannot alias, oversized snapshot indices are rejected before allocating, and iteration bounds are documented and enforced.
- `game/save`: a literal `null` payload is rejected so backup recovery engages, and a failed decode no longer mutates the caller's value through shared maps or pointers.
- `game/net`: inputs and checksums are only accepted from the peer that sent them, the client ignores snapshots from anyone but its server, rollback inputs are bounded to a small future window, snapshot entity counts are validated before allocating, and a delta the client cannot apply asks the server for a full snapshot.
- `game/gltf`: required extensions that are not implemented are refused instead of silently loading zeroed data, inverse-bind-matrix counts follow the spec, scene roots that are children are rejected, GLB chunks must be 4-byte aligned, and animation key times are validated.
- `game/anim3d`: a NaN blend parameter clamps instead of panicking, and asking for the current state no longer restarts it every frame (`Restart` replays it explicitly).
- `game/net`: UDP refuses `ChannelReliableOrdered` with `ErrUnsupportedChannel` instead of silently dropping lockstep and rollback packets it cannot retransmit.

- `game/ai`: traversal costs below 1 are clamped so A*'s distance heuristic stays admissible and paths stay optimal.
- `game/net`: angle interpolation normalizes rotations in one step, snapshot counts are validated before allocating, WebSocket handshakes time out, and lockstep inputs and checksum history are bounded.
- `game/modding`: reloading a mod re-checks its game version and dependencies, closing the manager notifies listeners that panels went away, and mod storage keys cannot collide across mod IDs.
- `game/liveops`: tracking and flushing read the persisted consent choice, so a revocation made elsewhere stops events immediately.
- `game/sceneformat`: inline tilemap dimensions are capped before multiplying, so a huge width and height cannot overflow validation and panic the builder.
- `game/editor`: saves write through a temporary file and rename, a save with no target reports that instead of claiming success, opening YAML clears the stale save path, 3D views and picking skip screen-space layers, and a zero scale can be dragged away from zero.
- `game/skeletal`: `SkeletonData.Compile` is exported and `NewSkeleton` reports an invalid rig instead of rendering every bone at the origin.
- `examples/caverun`: the title backdrop snapshots slime positions so interpolation does not drift back toward spawn points.

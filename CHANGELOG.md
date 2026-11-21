# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- `game`: fixed-step `Loop` built on NSGo's `RequestAnimationFrame`, pausing in the background and interpolating draw frames.
- `game/sprite`: atlases, frames, animations, sprites and batches drawn into a `Canvas`.
- `game/sound`: `Mixer` for sound effects and music, with background pause and resume.
- Website and documentation in English and Simplified Chinese (`apps/website`, `docs/en`, `docs/zh`).

### Fixed

- `game/editor`: the 3D gizmo drags now start from real pointer input on the mounted viewport (a press that becomes a pan picks a handle, then moves it) instead of being reachable only from the model API.
- `game/net`: UDP refuses `ChannelReliableOrdered` with `ErrUnsupportedChannel` instead of silently dropping lockstep and rollback packets it cannot retransmit.

- `game/ai`: traversal costs below 1 are clamped so A*'s distance heuristic stays admissible and paths stay optimal.
- `game/net`: angle interpolation normalizes rotations in one step, snapshot counts are validated before allocating, WebSocket handshakes time out, and lockstep inputs and checksum history are bounded.
- `game/modding`: reloading a mod re-checks its game version and dependencies, closing the manager notifies listeners that panels went away, and mod storage keys cannot collide across mod IDs.
- `game/liveops`: tracking and flushing read the persisted consent choice, so a revocation made elsewhere stops events immediately.
- `game/sceneformat`: inline tilemap dimensions are capped before multiplying, so a huge width and height cannot overflow validation and panic the builder.
- `game/editor`: saves write through a temporary file and rename, a save with no target reports that instead of claiming success, opening YAML clears the stale save path, 3D views and picking skip screen-space layers, and a zero scale can be dragged away from zero.
- `game/skeletal`: `SkeletonData.Compile` is exported and `NewSkeleton` reports an invalid rig instead of rendering every bone at the origin.
- `examples/caverun`: the title backdrop snapshots slime positions so interpolation does not drift back toward spawn points.

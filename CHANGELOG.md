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
- Documentation, README and website state that consoles (Nintendo Switch, PlayStation and Xbox) are out of scope (#496).
- Website: a playground that runs the examples' web builds beside their source, with per-example and per-file links, built by `bun run playground` (#511).
- `game/server`: headless fixed-step `Runner` for dedicated servers, with several matches at their own rates, overrun reports and bounded signal shutdown (#549).
- `game/server`: a matchmaking queue with per-mode/region tickets, widening skill windows, whole parties and a pluggable rule (#552).
- `game/server`: rooms with capacity, private codes, a waiting/running/ended match lifecycle and fresh epochs, per-room packet inboxes, late-join full snapshots and empty-room timeouts (#551).
- `game/input`: keyboard keys drive actions and axes: `Key` and `KeyCode` inputs, held keys heard on the whole window with `WatchKeys` or on a focused view through `Attach`'s `OnKeyDown` and `OnKeyUp`, a held key keeping its action `Pressed`, two opposite keys driving an axis, and every key released as the window loses focus (#24).
- `game/server`: per-peer packet/byte rate limits with throttle/drop/disconnect policies, message and pending-reliable caps, and an input validator with per-peer violation counts (#555).
- `examples/caverun`: locks landscape on iPhones and Android phones with NSGo's `RequestOrientation`, and the screen and shipping guides explain how a game declares its orientation (#102).
- `game/cutscene`: video cutscenes in NSGo's `core.Video` over the game's view, pausing the loop and the mixer while they play, skippable by a press, a key or back after an optional delay, with an end/skip callback, a scene for playing one between scenes and a versioned YAML file format (#494).
- `game/sound`: `Mixer.Suspend` silences the mixer until resumed, as the background does, and `Mixer.Suspended` reports it (#494).
- `game/net`: a TLS WebSocket listener with an Origin allow-list, plus `game/server` health, readiness and Prometheus metrics endpoints and a container/drain guide (#554).
- `game/accounts`: a self-hostable player account service with guest accounts upgraded to email or a linked platform/OAuth identity, argon2id passwords, redacted secrets, SQLite and Postgres stores, an HTTP API and the `accountsd` server; `game/accounts/token` issues Ed25519 tokens that game servers verify offline against a versioned YAML key set (#556).
- `game/sprite`: `Sprite.Tint` multiplies a sprite's own pixels by a color through NSGo's `DrawTintedImageFrom`, leaving what is drawn under it unchanged (#29).
- `game/sound`: 2D positional effects, with `Listener2D` panning an effect by where it sits across the camera's view and attenuating it with distance (linear or inverse rolloff), and `Effect.PlayAt` and `Cue.PlayAt` (#105).
- `game/input`: mouselook through NSGo's pointer lock: `LockPointer`/`UnlockPointer` on the game view, pointer axes (`BindPointerAxis` with `PointerX`/`PointerY`, sensitivity and invert) read per step with `Delta`, `PointerDelta` or `Axis`, `Attach` forwarding `OnPointerDelta`, `PointerLockRequested`, and pointer axes saved with the other bindings; `Load` now refuses unknown fields and trailing data (#26).
- `game`: `Loop.Immersive` hides a phone's system bars through NSGo's `mobile.Immersive` while the game plays and brings them back while it is paused, as under a menu, and when it stops or its scope is disposed (#103).
- `game/input`: game controllers drive actions and axes on every target with gamepads: `GamepadButton` inputs, `BindGamepadAxis` stick axes with a radial or axial dead zone, sensitivity and inversion, `WatchGamepads` reading NSGo's `gamepad` package each step, per-player `SelectGamepad`, `OnGamepads` for tests and replays, and controller bindings in `Save`/`Load` (#25).
- `game/quality`: low, medium and high presets (resolution scale, particle budget, effects, shadows) picked from the device class and measured frame times, adapted while on auto and overridable through `save.DisplaySettings.Quality`, with a versioned YAML config; `Loop.FrameTime`, `lights2d.Scene.DisableShadows` and `world3d.QualityLevelFor` wire it in (resolution scale is a value for games to apply to buffers they size; nothing in this repository applies it yet), and Starburst starts low-end Android phones on the low preset (#540).

### Changed

- `game`, `template` and the examples build against NSGo `v0.3.0` instead of v0.2.0, so the key-up, gamepad, frame-time, `DrawAtlas`, GPU view and other engine APIs released since v0.2.0 are available; NSGo's changes since v0.2.0 needed no source change in this repository, and NSGo now brings its goja fork `github.com/nsgo-dev/goja` in place of `github.com/dop251/goja` (#567).
- `game/particles`: `Emitter.Draw` now tints each particle's sprite by its `ColorCurve` RGB, where it previously applied only the curve's alpha, so particle definitions with a non-white `colorCurve` draw in that color (#29).
- `game`: `Loop` adds each frame's time from `RequestAnimationFrame` as it is, the display's own frame time where the platform stamps one, and no longer smooths it (#27).
- `game/sound`: group, master and background volume changes now reach effects already playing through NSGo's `audio.Playback`, silencing them at 0 instead of stopping them; an `Effect` at its `Limit` stops its oldest playback to play a new one instead of refusing it; a voice with `Muted` set now plays nothing and `Play` returns false instead of playing muted and counting toward the limit; and a crossfade's new music track starts at once, muted, instead of waiting paused for the fade's first frame (#31).

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
- `game/render3d`: OpenGL-depth projections keep their near geometry, LOD levels that override one vertex/triangle count keep the other, hysteresis history evicts unseen IDs, `RenderStats.Add` does not double-count resident memory, transparent sorting uses clip-space depth, a stale occlusion buffer refuses another view, and an object whose center is behind the camera takes the finest LOD.
- `game/scene3d`/`game/world3d`: `Node.WorldBounds` unions the mesh and collider bounds, non-finite bounds and rays are rejected, and `MeshLibrary` revisions invalidate `GridMap` chunk bounds.
- `game/math3d`: `Decompose` refuses sheared matrices and accepts tiny invertible scales, `Inverse` only rejects a singular or non-finite determinant, and RMF frames initialize from the first non-zero tangent when the first points coincide.
- `game/net`: WebSocket writes have deadlines and `Close` breaks a blocked `Send`, full snapshots cap their entity count before allocating, and `DialWebSocket` has dial/handshake deadlines and parses IPv6 hosts.
- `game/save`: storage slot keys escape dots so overlapping prefixes cannot collide, and a file slot that differs from an existing one only by case or Unicode normalization is refused.
- `game/entities`: a keep-world reparent that needs shear returns `ErrNotDecomposable` instead of silently changing the world transform.
- `game/gltf`: optional extension payloads are preserved on `BufferView`, `Texture`, `Material` and `Node`.
- `game/physics3d`: box inertia uses the analytic 1/3 coefficient, and `Body.LockRotation` disables rotation on a dynamic body.
- `game/anim3d`: quaternion blending uses a weight-independent hemisphere reference so a weight crossing zero no longer pops the pose.
- `game/net`: angle interpolation normalizes rotations in one step, snapshot counts are validated before allocating, WebSocket handshakes time out, and lockstep inputs and checksum history are bounded.
- `game/modding`: reloading a mod re-checks its game version and dependencies, closing the manager notifies listeners that panels went away, and mod storage keys cannot collide across mod IDs.
- `game/liveops`: tracking and flushing read the persisted consent choice, so a revocation made elsewhere stops events immediately.
- `game/sceneformat`: inline tilemap dimensions are capped before multiplying, so a huge width and height cannot overflow validation and panic the builder.
- `game/editor`: saves write through a temporary file and rename, a save with no target reports that instead of claiming success, opening YAML clears the stale save path, 3D views and picking skip screen-space layers, and a zero scale can be dragged away from zero.
- `game/skeletal`: `SkeletonData.Compile` is exported and `NewSkeleton` reports an invalid rig instead of rendering every bone at the origin.
- `examples/caverun`: the title backdrop snapshots slime positions so interpolation does not drift back toward spawn points.

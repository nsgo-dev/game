# Agent Note: glTF 2.0 animation playback in anim3d

Status: implemented

## Problem

`game/gltf` already decodes glTF 2.0 node hierarchies, skins (`InverseBindMatrices`), and animation channels (`STEP`, `LINEAR`, `CUBICSPLINE`), and `game/anim3d` already provides higher-level pose graphs (`Clip`, `StateMachine`, `BlendSpace1D`, `BlendSpace2D`, `LayerStack`), yet there was no playback layer connecting a loaded `*gltf.Document` to `game.Loop`'s fixed `time.Duration` steps. Calling `gltf.Animation.Apply` directly mutated `doc.Nodes` in place (preventing multiple characters from sharing one loaded document), had no playback state (`Play`, `Pause`, `Stop`, `Seek`, `SetLoop`, `SetSpeed`, `Crossfade`), did not compute mesh-normalized glTF 2.0 skin joint matrices, and left callers without a bridge to `scene3d.Scene` or `anim3d.Clip`.

## Decision

`packages/game/anim3d/playback.go` adds two complementary entry points while leaving `packages/game/gltf` unmodified:

1. **Per-instance `Player` (`NewPlayer(doc)`) vs mutating `gltf.Document`**: `Player` validates the node tree once into a parent-before-child `evalOrder` and keeps its own rest transforms, local TRS/matrix buffers, morph weights, world matrices, and crossfade scratch buffers. Multiple `Player` instances can animate the same `*gltf.Document` independently at different clips and timestamps with zero mutations to `doc.Nodes`.
2. **Direct glTF channel evaluation vs converting through `anim3d.Clip`**: `Player` evaluates `gltf.AnimationChannel` directly in `float64` normalized segment time for `STEP`, `LINEAR` (shortest-path `Quat.Slerp` with hemisphere alignment), and `CUBICSPLINE` (glTF 2.0 Appendix C Hermite polynomial with quaternion normalization), writing into pre-allocated node buffers. Unanimated nodes authored with a raw `"matrix"` (such as COLLADA `Z_UP` roots in CesiumMan and RiggedSimple) keep their exact `LocalMatrix`, and every evaluation resets buffers to rest pose first so switching animations never leaks stale channels.
3. **Mesh-normalized skin matrices (`SkinMatrices`, `SkinMatricesForNode`)**: Joint matrices follow the glTF 2.0 specification formula `inverse(worldMatrix[meshNode]) * worldMatrix[joint] * inverseBindMatrix[i]`. When a skinned mesh sits under a transformed root node (such as `Z_UP`), bind-pose joint matrices evaluate to identity rather than double-applying the root transform.
4. **Clip bridge (`ClipFromGLTF`, `ClipsFromGLTF`) and `PoseSampler`**: `ClipFromGLTF` converts glTF animations into `anim3d.Clip` (seeding unanimated TRS components on targeted nodes from rest transforms to preserve bone offsets, omitting untargeted nodes from `Tracks` so `LayerStack` masks leave them untouched, and attaching `extras` events). `Player` also implements `PoseSampler` (`Update`, `Pose`) and synchronizes with `scene3d` via `ApplyToScene` (by node name) and `ApplyToNodes` (by index).

## Alternatives considered

### Why not drive playback inside `gltf.Document` via `gltf.Animation.Apply`?

Adding a stateful player directly on `gltf.Document` would keep all glTF animation helpers inside one package and reuse `Document.ComputeWorldMatrices` with less code. It was rejected because mutating `doc.Nodes` in place prevents instancing multiple characters from a single loaded `*gltf.Document`, couples `game/gltf` (a format loader) to runtime playback and `scene3d`, and would conflict with parallel work in `packages/game/gltf`.

### Why not force all glTF playback through `anim3d.Clip`?

Converting every glTF animation into `anim3d.Clip` and running a thin wrapper over `Clip.Sample` would avoid implementing a second channel evaluator. It was rejected because `anim3d.JointTrack` only stores linear/Slerp TRS keyframes: it cannot represent exact `STEP` holds, `CUBICSPLINE` in/out tangents, morph target `weights` channels, or raw non-TRS `"matrix"` nodes without changing `Clip`'s existing public data model. Providing both direct evaluation in `Player` and `ClipFromGLTF` preserves full glTF 2.0 spec fidelity in `Player` while still feeding `StateMachine` and `BlendSpace`.

## Consequences

- **Benefits**:
  - Zero-mutation shared `*gltf.Document` playback with full `STEP`, `LINEAR`, `CUBICSPLINE`, morph weights, crossfade (including interruption snapshotting), reverse speed, pause/seek/stop, and exact glTF 2.0 skin matrices.
  - `anim3d` now depends on `game/gltf` and `game/scene3d` with no import cycles (`gltf` and `scene3d` only depend on `math3d` and lower-level packages).
  - Existing `StateMachine`, `BlendSpace1D`/`2D`, `LayerStack`, and `EventDispatcher` workflows can consume glTF animations immediately via `ClipFromGLTF` or via `Player` as a `PoseSampler`.
- **Costs & limits**:
  - `anim3d` duplicates glTF channel sampling logic from `gltf/anim.go` (in allocation-free TRS form); if future glTF extensions add new interpolation modes or target paths, both `gltf/anim.go` and `anim3d/playback.go` must be updated.
  - `ClipFromGLTF` flattens `STEP` and `CUBICSPLINE` channels to keyframe values for linear/Slerp `Clip.Sample`; callers needing exact spline tangents or stepped curves must use `Player` directly. Revisit if `JointTrack` ever gains per-channel interpolation metadata.
  - `ApplyToScene` matches nodes by first-seen non-empty `Node.Name` in pre-order; scenes with duplicate or empty node names must use `ApplyToNodes` by index.

## Verification

- `packages/game/anim3d/playback_test.go` verifies a synthetic CesiumMan-style 9-bone rigged walk cycle (with `Z_UP` matrix root, exact inverse bind matrices, `CUBICSPLINE` pelvis translation, `LINEAR` opposite-hemisphere limb rotations, `LINEAR` chest scale, `STEP` head turn, and footstep `extras` events) driven at 60 Hz by `game.Loop` under `nsgotest`, plus exact interpolation math, pause/stop/speed/seek/non-looping controls, crossfade and interruption snapshotting, `ApplyToScene`/`ApplyToNodes`, `ClipFromGLTF` in `StateMachine`, error cases, and real Khronos `RiggedSimple.gltf` and `CesiumMilkTruck.gltf` fixtures.
- Verified with `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./anim3d/...`, `go tool nsgo test .`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, `gofmt`, and `bun run verify-notes`.

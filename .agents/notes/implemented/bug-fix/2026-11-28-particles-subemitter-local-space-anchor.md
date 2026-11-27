# Agent Note: Anchor local-space sub-emitter particles per trigger point

Status: implemented

## Problem

`Emitter` implements each `SubEmitterRule` with a single managed child emitter (`e.children[i]`) that is reused across triggers (`pkg/particles/space.go`). On each birth, death, or collision event, `triggerSubEmitters` moved `ch.X, ch.Y` to the trigger's world coordinates `(wx, wy)` and called `ch.burstWithInheritedVel(...)`.

When a sub-emitter's config sets `Space: SpaceLocal`, every particle stored coordinates relative to `(ch.X, ch.Y)` and `Draw`, `ParticleWorldPos`, collision resolution, trails, and nested sub-emitters transformed them using the shared `ch.X, ch.Y`. As a result, every subsequent trigger (or `ReloadConfig`, which resets `ch.X, ch.Y` to the config default) teleported all previously live child particles and their trails to the newest trigger point (#1591). In addition, `spawnAt` added world-space inherited velocity directly to local-space particle velocity without rotating it by `-ch.Rotation`.

## Decision

Keep the single managed child `*Emitter` per `SubEmitterRule`, and anchor each local-space child particle and its trail to the trigger origin recorded at spawn time:

- **Per-particle trigger anchor (`pkg/particles/particles.go`, `pkg/particles/space.go`)**: `Particle` carries unexported `anchorX`, `anchorY`, and `hasAnchor` fields. When `e.Space == SpaceLocal` and `e.subDepth > 0`, `spawnAt` records `originX, originY` as the particle's anchor. `particleOrigin`, `particleLocalToWorld`, `particleWorldToLocal`, `ParticleWorldPos`, `Draw`, collision resolution, and grandchild triggers all transform anchored particles relative to `(p.anchorX, p.anchorY)` and `e.Rotation`, while top-level `SpaceLocal` emitters (`subDepth == 0`, `hasAnchor == false`) continue following `(e.X, e.Y)` unchanged.
- **Per-trail trigger anchor (`pkg/particles/trail.go`, `pkg/particles/particles.go`)**: `Trail` carries unexported `anchored`, `anchorX`, and `anchorY` fields populated from the owning particle when acquired and cleared in `releaseTrail`. In `Draw`, when a `SpaceLocal` emitter has any anchored trail, each live and fading trail translates to its own `(tr.anchorX, tr.anchorY)` before applying `e.Rotation`; unanchored top-level local emitters keep the single batched `Save`/`Translate`/`Rotate`/`Restore` path.
- **Inherited velocity in the child's frame (`pkg/particles/particles.go`)**: `spawnAt` treats incoming `(ivx, ivy)` as world-space velocity and rotates it through `e.worldDirToLocal(ivx, ivy)` when `e.Space == SpaceLocal`, so inherited parent velocity produces identical world motion in both `SpaceWorld` and `SpaceLocal` regardless of `ch.Rotation`.
- **Format and editor compatibility**: `space: local` remains valid in YAML, JSON, the JSON Schema, and the particle emitter editor without breaking existing emitter files or hot-reload workflows.

## Alternatives considered

- **Force sub-emitters to `SpaceWorld` or refuse `space: local` in YAML**: Simpler because world coordinates need no per-particle anchor. Rejected because Go callers can still construct `SubEmitterRule{Config: Config{Space: SpaceLocal}}`, the particle editor exposes the `Space` selector on sub-emitter configs, and preserving `SpaceLocal` lets rotated local-space sub-emitters keep their local gravity, sprite rotation, and config fidelity while preventing trigger-to-trigger teleportation.
- **Allocate a pool of child `*Emitter` instances (one per trigger)**: Gives each burst an independent emitter transform, but breaks the 1:1 `e.Children()[i]`-to-`e.SubEmitters[i]` mapping, complicates `ReloadConfig`, `Reset`, `Clear`, and deterministic single-PRNG streams per rule, and adds per-burst emitter allocations and sprite batches.

## Consequences

- **Benefits**: Triggering a `SpaceLocal` sub-emitter at multiple points (or hot-reloading its config) never drags earlier live child particles, live/fading trails, collision checks, or grandchild spawns to the latest trigger point; inherited velocity is consistent across `SpaceWorld` and `SpaceLocal` even under rotation; `e.Children()` cardinality, determinism, and zero-allocation steady-state updates are preserved.
- **Costs**: `Particle` grows by 12 bytes (padded) and `Trail` by 12 bytes for the unexported anchor fields. When a `SpaceLocal` sub-emitter draws trails, each anchored trail emits its own `Save`/`Translate`/`Restore` pair instead of sharing one emitter-wide transform.

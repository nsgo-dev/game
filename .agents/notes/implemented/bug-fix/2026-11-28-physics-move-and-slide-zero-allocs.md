# Agent Note: Zero-allocation MoveAndSlide, World slide, and CharacterBody steps

Status: implemented

## Problem

`physics.MoveAndSlide`, `World.MoveAndSlideWith`, `CharacterBody.Move`, and `platformer.Controller.Step` allocated on every grounded or wall-sliding fixed step (#1660):

- `physics.MoveAndSlide` built `MoveResult.Contacts` in a fresh local slice on every call that hit a tile.
- `World.MoveAndSlideWith` allocated both a `planes` slice (`make([]vec2, 0, cfg.maxSlides)`) and a fresh `SlideResult.Collisions` slice in `sweepAndSlide` (building on [World.MoveAndSlide against bodies, shapes, tiles and static rigid bodies](../feature/2026-11-25-physics-world-move-and-slide.md)).
- `CharacterBody.Move` and `MoveWorld` allocated fresh `contacts`, `collisions`, and `platformBodies` slices on every call.
- `platformer.Controller.Step` and `StepWorld` allocated a closure per step to pass into `stepWith`.

Because platformer and top-down bodies touch tiles or walls on almost every frame, this produced 60 heap allocations per second per body and prevented Cave Run (`examples/caverun/swarm_test.go`) and templates from asserting zero-allocation fixed steps.

## Decision

Introduce explicit `*Into` buffer-reuse entry points alongside internal scratch buffers on `World` and `CharacterBody`, and remove the per-step closure in `platformer.Controller`:

- **Tile movement (`pkg/physics/tile.go`)**: add `MoveAndSlideInto(b *Body, grid TileGrid, dt time.Duration, res *MoveResult)` that clears `res.Collisions` pointers and resets `res.Contacts = res.Contacts[:0]` and `res.Collisions = res.Collisions[:0]` before moving, skipping contact recording when `res == nil`. Keep `MoveAndSlide` as a thin wrapper around `MoveAndSlideInto` so existing callers remain backwards-compatible and retain independent result ownership.
- **World shape sweep (`pkg/physics/slide.go`, `pkg/physics/world.go`)**: replace `make([]vec2, 0, cfg.maxSlides)` with a stack-backed `[8]vec2` array (falling back to heap only when `MaxSlides > 8`). Add `World.MoveAndSlideInto` and `World.MoveAndSlideWithInto` for caller-owned `*SlideResult` buffers, and reuse an unexported `slideCollisions []SlideCollision` scratch slice on `World` for `World.MoveAndSlide` and `World.MoveAndSlideWith`, clearing pointer fields with `clear` before resetting length to zero so removed bodies are never retained.
- **CharacterBody (`pkg/physics/character.go`)**: keep unexported `contactsBuf`, `collisionsBuf`, and `excludeBuf` scratch slices on `CharacterBody`, add `MoveInto` and `MoveWorldInto` for caller-owned `*MoveResult` buffers, and route `Move` and `MoveWorld` through them. `sweepAndSlide` appends directly into the destination `SlideResult.Collisions` slice and returns `hitAny bool` so `res == nil` keeps identical collision stopping behavior.
- **Platformer controller and examples (`pkg/platformer/controller.go`, `examples/caverun`, `examples/cryptquest`)**: replace `stepWith`'s closure parameter with direct arguments, switch Cave Run and Crypt Quest to `MoveAndSlideInto`, tighten `TestSwarmAddsNoAllocations` to assert `0` allocations per fixed step, and track `BenchmarkMoveAndSlide` in `benchmarks/baseline.yaml`.

## Alternatives considered

### Store the scratch contact slice on `physics.Body`

Keeping a `contactsBuf []TileContact` field directly on `Body` would allow `MoveAndSlide(b, grid, dt)` to allocate zero times without adding `MoveAndSlideInto`. It is rejected because `Body` is currently a comparable struct (`Shape` holds `*Chain`, not a slice), so adding a slice field to `Body` would break `==` comparisons in caller code and would silently alias `MoveResult.Contacts` across consecutive calls to the standalone `MoveAndSlide` function.

### Require every caller of `World.MoveAndSlide` and `CharacterBody.Move` to migrate to `*Into`

Leaving `World.MoveAndSlideWith` and `CharacterBody.Move` allocating and only making `*Into` zero-allocation would avoid scratch-slice aliasing across calls. It is rejected because `CharacterBody` is already a stateful per-character struct whose `Move` and `MoveWorld` methods back `platformer.Controller.Step` and `template/platformer2d`; reusing scratch slices on `CharacterBody` and `World` makes all existing `CharacterBody` and `platformer.Controller` callers zero-allocation immediately, while `*Into` remains available whenever a caller needs independent slice lifetime across calls.

## Consequences

- **Benefits**: `MoveAndSlideInto`, `World.MoveAndSlide`, `World.MoveAndSlideInto`, `CharacterBody.Move`, `CharacterBody.MoveWorld`, `platformer.Controller.Step`, and Cave Run's full fixed step (with and without the 1000-firefly swarm) all run at `0 allocs/op` in steady state, guarded by `debug.AssertZeroAllocs` tests and `BenchmarkMoveAndSlide` in `benchmarks/baseline.yaml`.
- **Costs**: callers that retain `SlideResult.Collisions` across multiple `World.MoveAndSlide` calls on the same `World`, or `MoveResult.Contacts`/`Collisions` across multiple `CharacterBody.Move` calls on the same `CharacterBody`, must copy the slice or call the `*Into` variant with their own result struct.
- **Verification**: `pkg/physics/move_alloc_test.go` verifies `MoveAndSlideInto` parity and buffer reuse plus `debug.AssertZeroAllocs` for grounded, wall-sliding, airborne, and `nil`-result `MoveAndSlideInto`, `World.MoveAndSlide`/`MoveAndSlideInto`, and `CharacterBody` (`Move`, `MoveInto`, `MoveWorld`, with world bodies, slopes, and moving platforms); `pkg/platformer/controller_test.go` verifies `debug.AssertZeroAllocs` for `Controller.Step` and `StepWorld`; `examples/caverun/swarm_test.go` enforces `0` allocations per fixed step; `TestNoFusedMultiplyAddOnArm64` confirms zero arm64 FMA contractions were introduced.

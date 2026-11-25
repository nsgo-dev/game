# Agent Note: Remove the fused multiply-adds left in XPBD, the character controller and tiles

Status: implemented

## Problem

`docs/en/determinism.md` and [the save and determinism note](../feature/2026-11-25-physics-save-determinism.md) say every stepped path in `packages/game/physics` is free of fused multiply-add, so a run is bit-identical on amd64 and arm64. Disassembling an arm64 test binary showed about 130 `FMADD`/`FMSUB`/`FNMADD`/`FNMSUB` instructions still in `xpbd.go`, plus `character.go` (the left-wall and ceiling snaps), `tile.go` (the same snaps in `MoveAndSlide`) and `world.go` (the tile boxes `QueryOverlapLayer` tests). `SoftWorld` also called `math.Cos`, `math.Sin`, `math.Hypot`, `math.Atan2` and `math.Pow`. The existing golden trajectories never reached that code, so CI could not see it, and a lockstep or rollback game using ropes, soft bodies or tile overlap queries drifted apart between an arm64 and an amd64 peer.

## Decision

- Every product that feeds an add or a subtract in `xpbd.go`, `character.go`, `tile.go` and `world.go` sits inside an explicit `float64(...)` or `float32(...)` conversion, which the Go specification forbids the compiler from fusing. This includes a product stored in a local first, such as `dx := float64(px * tb.invMass)` before `tb.curX += dx`, because SSA sees through the local and fuses it anyway.
- `SoftWorld` uses `detmath.Sincos`, `detmath.Hypot` and `detmath.Atan2` instead of the `math` versions.
- The shape-matching gain `1 - (1-stiffness)^(1/iterations)` comes from `shapeMatchGain`, Newton's method on `y^n = x` started at `y = 1`, in plain IEEE-754 arithmetic. It replaces `math.Pow`, whose `Exp` has its own assembly on amd64 and on arm64 (`exp_amd64.s`, `exp_arm64.s`) and whose `Log` is assembly on amd64 only (`log_amd64.s`), so the two architectures round it differently. Because `y^n` is convex the iteration decreases monotonically from above, so it stops at the first step that fails to decrease; stiffness is a `float32`, so `x >= 2^-24` and it takes about `|ln x| + 6` steps.
- `TestNoFusedMultiplyAddOnArm64` (`fma_test.go`) builds the package's test binary with `go test -c` (the binary `go test` runs is linked with `-s -w` and has no symbols to filter on), disassembles the `physics` and `physics_test` symbols with `go tool objdump`, and fails on any fused multiply-add. A function is judged by the file that declares it, since an inlined helper such as `Box.Translated` keeps its own file and line even though the product it fuses was written in the caller. `debugdraw*.go` and `xpbd_debugdraw*.go` are exempt because drawing never feeds back into the state, and of the test files only the golden drivers (`golden_test.go`, `snapshot_test.go`) are checked. It skips on other architectures, where the compiler does not fuse.
- `golden_test.go` adds three trajectories that must hash the same everywhere: a `SoftWorld` with a bending rope pinned to a tilted crate, a compliant rope on a wheel, a pressurized shape-matched soft body and free particles colliding with a `World` body, a static rigid ramp and tiles; two `CharacterBody` walkers, one on the bare grid with slopes and a one-way moving platform and one through a `World` with a low-gravity area; and four bodies in `MoveAndSlide` with a `QueryOverlapLayer` probe. The grid has a fractional origin and tile size so the products do not come out exact.

## Verification

- On arm64 with the fix, `TestNoFusedMultiplyAddOnArm64` passes; with `main`'s `xpbd.go`, `character.go`, `tile.go` and `world.go` it lists 129 instructions.
- The three goldens give the same hashes on arm64 and amd64. With `main`'s sources amd64 still gives those hashes while arm64 gives different `SoftWorld` and tile hashes, so those two catch the old contractions.
- The character hash is the same on `main`: the two contractions in `character.go` multiply a small integer by a `float32` tile size, a product that is exact in `float64`, so fusing it never changed a result. The disassembly check is what keeps them out.

## Alternatives considered

- **Add `Pow`, `Exp` and `Log` to `internal/detmath`**: a general deterministic `Pow` would serve other packages, but it is a large port of FDLIBM with its own accuracy tests, for a single caller whose exponent is always `1/n` with integer `n`. The Newton root is a dozen lines and is checked against `math.Pow` to `1e-14` in `xpbd_internal_test.go`.
- **Check the compiler's listing with `go build -gcflags=<pkg>=-S` instead of objdump**: it also lists functions no test calls, which the linker drops from a test binary, and it was used while fixing this. As a test it depends on the build cache handing back the compiler's output for a cached package, and it reads the compiler's listing rather than the machine code that runs. objdump on a linked test binary checks exactly what the tests and the goldens execute, and the package's tests reach every stepped path.
- **Disassemble the running test binary with `os.Executable`**: no extra build, but `go test` strips that binary, so objdump finds nothing to filter.

## Consequences

- `SoftWorld`, `CharacterBody`, `MoveAndSlide` and `World.QueryOverlapLayer` give the same results on amd64 and arm64. On amd64 the three goldens hash the same with `main`'s sources as with the fix, so amd64 results are unchanged in practice (`math.Hypot` is assembly on amd64 and `math.Pow` was replaced, so a last-bit difference is possible elsewhere); arm64 results change wherever a contraction used to round once.
- A new contraction anywhere in `packages/game/physics` outside debug drawing fails `TestNoFusedMultiplyAddOnArm64` on the macOS CI job, with the function, file and line, instead of waiting for a golden hash to drift.
- The check costs one extra link of the test binary, about two seconds, on arm64.

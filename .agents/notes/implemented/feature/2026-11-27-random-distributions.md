# Agent Note: Normal distribution and uniform 2D points in pkg/random

Status: implemented

## Problem

`random.Stream` offered only uniform draws, so games wrote bullet spread, damage variance and scatter by hand. The two textbook recipes — Box-Muller for a normal distribution and the polar form (`sqrt(radius)`, `cos/sin(angle)`) for a uniform disc — both rely on `math.Log`, `math.Sin` and `math.Cos`, whose Go standard library implementations take different code paths per architecture and round differently (see `docs/en/determinism.md` §5), which would break the package's bit-identical-across-architectures guarantee. Box-Muller also traditionally caches its second value in generator state outside the stream, which `State`/`SetState` could not capture.

## Decision

`pkg/random/stream.go` gains four `Stream` methods (issue #1366), all built from `Float64`/`Range` draws plus additions, multiplications, comparisons and `math.Sqrt` only (all correctly rounded or FMA-safe on every architecture):

- `Normal(mean, stddev)` sums 12 `Float64` draws and shifts by 6 (Irwin-Hall), then returns `mean + float64(stddev*z)`. The sum uses additions only, consumes a fixed 12 draws per call with no cached second value, and bounds output to ±6 standard deviations. Panics on a NaN mean or `stddev < 0`/NaN.
- `InCircle(r)` rejection-samples the unit disc from its enclosing square (`Range(-1, 1)` pairs accepted when `float64(x*x)+float64(y*y) < 1`) and scales by `r`. Panics on `r < 0`/NaN.
- `OnCircle()` normalizes such an inner point (`math.Sqrt`, then division — both correctly rounded), redrawing on the measure-zero zero vector. No trigonometry anywhere.
- `InRect(x, y, w, h)` is `x + float64(u*w)` per axis with the same `math.Nextafter` upper-bound clamp `Range` uses, keeping the half-open `[x, x+w) × [y, y+h)` contract. Panics on negative/NaN sizes or NaN origin.

Every multiply-add that Go's compiler could contract into an arm64 FMA is wrapped in an explicit `float64(...)` conversion, following the convention `noise.go` already uses for the same reason.

## Alternatives considered

### Box-Muller (or ziggurat) for `Normal`, polar form for `InCircle`/`OnCircle`

Strongest argument: exact normal distribution (unbounded tails) and exact uniform direction with fewer draws per call (2 for Box-Muller, 1 for polar).

Rejected because they need `math.Log`/`math.Sin`/`math.Cos`/`math.Exp`, which are not bit-identical between amd64 and arm64 in Go, so golden sequences would diverge across CI runners and lockstep replays across architectures. Rejection sampling and the Irwin-Hall sum keep to operations IEEE-754 defines exactly. The costs — Irwin-Hall is bounded to ±6σ and rejection sampling consumes a variable number of draws (mean 4/π · 2 ≈ 2.55 per point) — are documented on the methods; both are invisible to a seeded replay because the stream itself is deterministic.

### Caching Box-Muller's second value

Strongest argument: halves the draws per normal value.

Rejected independently of the transcendental problem: a cached value lives outside the 256-bit generator state, so `State`/`SetState` would silently lose it and a restored stream would diverge from the original.

## Consequences

- New API is additive only; no existing sequence changes (golden vectors in `random_test.go` are untouched, and new golden bit patterns pin the new methods on both amd64 and arm64 CI runners).
- `Normal` is an approximation: kurtosis is slightly lighter than a true normal and values never exceed ±6σ, which is the right trade for games (spread, variance, scatter) but not for statistical simulation.
- `InCircle`/`OnCircle` draw counts vary per call; `Normal` is fixed at 12. Anything that must align draw counts across code paths should rely on forking, not on call counts.

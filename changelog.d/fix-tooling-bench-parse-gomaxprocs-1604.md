### Fixed

- `game/pkg/tooling/bench`: `ParseOutput` strips a trailing `-GOMAXPROCS` suffix only when every parsed benchmark line shares the same valid `-N` (`N >= 2`) suffix and verbose output does not declare the raw names verbatim, preventing single-CPU runs from merging sub-benchmarks whose names end in numbers (#1604).

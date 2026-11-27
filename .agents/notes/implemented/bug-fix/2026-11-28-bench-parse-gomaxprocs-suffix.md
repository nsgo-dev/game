# Agent Note: Strip -GOMAXPROCS in bench.ParseOutput only when uniform across a run

Status: implemented

## Problem

`bench.ParseOutput` (`pkg/tooling/bench/bench.go`) previously stripped any trailing `-<digits>` from each benchmark line independently before grouping metrics. Go's `testing.benchmarkName` only appends `-<GOMAXPROCS>` when `GOMAXPROCS > 1` (omitting it entirely when `GOMAXPROCS == 1`). On a single-CPU runner (`GOMAXPROCS=1` or `-cpu=1`), sub-benchmark names that end in a hyphen and number (such as `BenchmarkDecode/size-1024` and `BenchmarkDecode/size-4096`, or `BenchmarkBar/step-1`) had their own trailing numbers stripped and were silently merged into one blended median (`BenchmarkDecode/size`) whose key no longer matched multi-CPU runs (`BenchmarkDecode/size-1024`) (#1604).

## Decision

`ParseOutput` groups parsed result lines by their raw benchmark name first, then strips a trailing `-N` suffix in a second pass only when all of the following hold:

- Every parsed benchmark line in the input ends with the exact same `-N` suffix.
- `N` is a valid Go `testing.benchmarkName` GOMAXPROCS value: a plain decimal integer `>= 2` with no leading zero (so `-1`, `-0`, and `-08` are never treated as GOMAXPROCS suffixes, even on a single-benchmark run).
- Verbose `go test -v` output (`=== RUN   Benchmark...` or a bare `Benchmark...` declaration line) did not declare every parsed raw name verbatim; when it did, the run had `GOMAXPROCS == 1` and nothing is stripped.

Because stripping a single constant suffix from distinct raw names is injective, `ParseOutput` can never merge two lines whose full names differ.

## Alternatives considered

- **Parse the `cpu:` header emitted by `go test -bench`**: Go's `testing` package prints `cpu: <model name>` (from `internal/sysinfo.CPUName()`), not `GOMAXPROCS`, and `nsgo test -bench` does not print `goos:`/`cpu:` headers at all (only `✓ Benchmark...` lines). Rejected because the header does not contain the needed value.
- **Pass `GOMAXPROCS` or `-cpu` into a new `ParseOutput` parameter**: works when `game bench` invokes `nsgo test` directly, but breaks or diverges for `game bench -in <file>` and callers of the existing `ParseOutput(io.Reader)` API that parse saved `go test -bench` logs from another machine. Rejected in favor of keeping the existing signature and inferring the suffix from the stream itself.
- **Strip per-line unless a name collision occurs**: avoids merging `BenchmarkDecode/size-1024` with `BenchmarkDecode/size-4096`, but still corrupts a 1-CPU run containing `BenchmarkRaycast` and a single numeric sub-benchmark `BenchmarkFoo/size-64` (stripping `-64` because nothing collided with `BenchmarkFoo/size`). Rejected because checking uniformity across all parsed lines protects mixed 1-CPU runs too.

## Consequences

- **Benefits**: 1-CPU and multi-CPU runs of suites with numeric sub-benchmark names produce identical baseline keys; `-1`, `-0`, and leading-zero suffixes are never stripped; `-cpu=1,4` and `-cpu=4,8` outputs keep distinct CPU counts separate instead of blending them; and `ParseOutput`'s public signature stays unchanged.
- **Costs and known limits**: A non-verbose stream containing only a single benchmark (or several sub-benchmarks that all happen to end in the exact same `-N` with `N >= 2`) run at `GOMAXPROCS=1` is byte-for-byte indistinguishable from the unsuffixed name run at `GOMAXPROCS=N`, so `ParseOutput` strips `-N` unless `-v` declarations or another benchmark line are present. Mixing results from two machines with different `GOMAXPROCS` values in a single `ParseOutput` input no longer merges their suffixed names.

## Testing

- `pkg/tooling/bench/bench_test.go`: `TestParseOutputTakesMediansFromGoAndNSGoOutput`, `TestParseOutputCPUSuffixHandling` (1-CPU numeric sub-benchmarks, 8-CPU parity, mixed plain + numeric 1-CPU, `-1`/`-0`/`-08` single-line cases, verbose `-v` and `=== RUN` 1-CPU and 64-CPU runs, `BenchmarkFoo-2-4`, `-16` `nsgo test` output, and `-cpu=1,4` / `-cpu=4,8` non-merging), and `TestParseOutputRealGoTest1CPUAnd8CPUKeysMatch`.
- Verified with `go tool nsgo test ./pkg/tooling/bench/...`, `go tool nsgo test ./internal/gamecli/...`, `go tool nsgo check -target macos_arm64,linux_x64,windows_x64 -jobs 2 ./pkg/tooling/bench`, `(cd tools && go run ./changelog check)`, and `bun run verify-notes`.

# Agent Note: Generated TypeScript wire codecs and cross-language golden vector contract

Status: implemented

## Problem

After introducing YAML wire schemas (`pkg/online/netplay/wire/*.wire.yaml`) and zero-allocation Go codecs in [2026-11-27-net-wire-schema.md](2026-11-27-net-wire-schema.md), only Go could speak the `pkg/online/netplay` binary protocols. Web dashboards, spectator clients, TypeScript bots, and alternate-engine ports still had to reimplement transport framing, handshake, relay, replication v2, rollback, and membership codecs by hand, with no automated guarantee that JavaScript float64 arithmetic, zigzag varints, bit packing, mixed endianness, or 64-bit integers above `Number.MAX_SAFE_INTEGER` agree with Go byte for byte.

## Decision

We extend `pkg/online/netplay/wire` (`GenerateTS`, `GenerateTSRuntime`, `GenerateTSIndex`) and `cmd/gamewire -lang ts` to emit deterministic, dependency-free TypeScript codecs for every `*.wire.yaml` schema, backed by shared YAML golden vectors under `pkg/online/netplay/wire/testdata/vectors/` verified in both Go and Bun in CI, plus a live Go `ServerWorld` to TypeScript decoder end-to-end test and a runnable example under `examples/ts-replication-client/`.

### TypeScript runtime and generated API

`cmd/gamewire -lang ts -out <dir>` emits a shared `runtime.ts` (zero runtime dependencies, standard `Uint8Array`, `DataView`, `BigInt`, `TextEncoder`, `TextDecoder` working identically in browsers, Node, and Bun), one `<stem>_wire.ts` per schema, and a barrel `index.ts` (or a single self-contained `.ts` file with the runtime embedded when `-out` targets a `.ts` file).

- Every message and union `X` exports `createX(): X`, `encodeX(m: X, w: Writer): void`, and `decodeX(r: Reader, m: X): void`. `Writer` and `Reader` wrap caller-owned `Uint8Array` buffers via `DataView` and support `.reset()` for reuse; `decodeX` mutates caller-owned objects, reuses existing message elements in variable-length arrays, and copies fixed-count `u8` arrays in place so a 60 Hz client avoids per-packet wrapper allocations.
- Delta `mask` groups (`delta: true`) export `diffX(base, cur)` and `applyX(base, delta)`. `diffX` compares `f32`/`qfloat` fields by IEEE-754 32-bit bit pattern (`float32Bits`, so `-0.0` differs from `+0.0` and `NaN` equals `NaN`), `fixed` fields by 64-bit bit pattern (`float64Bits`), and `bytes` via `bytesEqual`. `applyX` copies masked fields into `base`, cloning non-empty `Uint8Array` slices so reusing the packet buffer cannot corrupt world state.
- Cross-file schema `imports` resolve to explicit sibling ES module imports (`import { ..., type ... } from "./<stem>_wire.ts"`), keeping generated output valid under `strict`, `verbatimModuleSyntax`, `noUncheckedIndexedAccess`, and `noUnusedLocals`.

### Type mapping and quantization rules

- `u8`, `u16`, `u32`, `i8`, `i16`, `i32`, `uvarint`/`svarint` with `bits` in `{8, 16, 32}`, `bits` of width `1..32`, `f32`, `qfloat`, and `fixed` map to `number`.
- `u64`, `i64`, 64-bit `uvarint`/`svarint`, `bits` of width `33..64`, `u64`/`uvarint` enums, `uvarint` union tags, and `u64` presence masks map to `bigint`. This keeps `InputFrame.bits`, `ChecksumMsg.checksum`, `RelayAck.mask`, and full 64-bit varints exact above `2^53`.
- `bytes` and fixed-count `u8` arrays (`count: N, of: u8`) map to `Uint8Array`; other arrays map to `T[]`; `string` maps to UTF-8 `string` encoded via zero-alloc `utf8ByteLength` + `TextEncoder.encodeInto`.
- `quantizeFloat`, `dequantizeFloat`, `quantizeFixed`, `quantizeFixedUint`, `dequantizeFixed`, and `dequantizeFixedUint` port Go's `pkg/online/netplay/wire/quantize.go` line for line using pure `BigInt` integer arithmetic over IEEE-754 bit patterns extracted via a module-level 8-byte scratch `DataView`—no floating-point arithmetic or `Math.fround` shortcuts execute on encode or decode.
- Unsigned and zigzag signed varints mirror Go's `encoding/binary.Uvarint`/`Varint` overflow rules (rejecting 10th bytes `> 1` and >10-byte encodings with `WireError("overflow", ...)` and truncated sequences with `WireError("short_buffer", ...)`).

### Golden-vector contract

Golden vectors live in `pkg/online/netplay/wire/testdata/vectors/*.yaml` (one file per built-in schema, plus `custom_props.yaml`, `edge_cases.yaml`, and `quantize.yaml`), regenerated deterministically by `go test -run TestGoldenVectors ./pkg/online/netplay/wire -update-vectors`.

- All raw byte payloads (`hex`, `deltaHex`, `bytes` fields, fixed `u8` arrays) are serialized as double-quoted lowercase hex strings so no YAML parser interprets digit-only hex sequences as numbers or `Infinity`.
- All 64-bit integer fields (`u64`, `i64`, 64-bit varints, `bits > 32`) are serialized as decimal strings so JS/TS YAML parsers (`Bun.YAML.parse`) never truncate values past `Number.MAX_SAFE_INTEGER`.
- Special IEEE-754 floats use canonical YAML float tags (`-0.0`, `.nan`, `.inf`, `-.inf`) so both Go `go.yaml.in/yaml/v3` and `Bun.YAML.parse` preserve `-0.0` (`0x80000000`) and canonical quiet `NaN` (`0x7fc00000`).
- Every vector entry asserts: `encode(value) == hex`, `decode(hex) == (decoded ?? value)` (checked by exact float bit patterns) with zero trailing bytes, and `encode(decode(hex)) == hex`. Every delta entry asserts `diff(base, cur) == diffMask`, encoded delta bytes equal `deltaHex`, and `apply(base, decode(deltaHex)) == expected`. Every invalid entry asserts the exact error code (`short_buffer`, `overflow`, `invalid_bool`, `invalid_enum`, `invalid_tag`, `invalid_version`, `invalid_const`, `invalid_mask`, `non_zero_padding`, `limit_exceeded`).
- `quantize.yaml` carries exact `vBits` and `wantDeqBits` hex bit patterns for `qfloat` and `fixed` edge cases and seeded fuzzed inputs.

## Alternatives considered

### JavaScript float64 arithmetic with `Math.fround` for `qfloat` and `fixed`

Computing `Math.round((v - min) / (max - min) * maxQ)` and `Math.fround(min + q * (max - min) / maxQ)` in float64 would shrink `runtime.ts` by ~150 lines. It is rejected because float64 division and double rounding diverge from Go's exact pure-integer `QuantizeFloat`/`DequantizeFloat` on halfway ties and range-edge subnormals, breaking byte-for-byte cross-language agreement and deterministic checksums.

### Mapping `u64` to JS `number` or `[lo, hi]` tuples

Using `number` silently corrupts `InputFrame.bits`, `ChecksumMsg.checksum`, and `RelayAck.mask` above `2^53 - 1`. Representing 64-bit fields as `[number, number]` tuples adds allocations and awkward ergonomics. Native ES2020 `bigint` is supported across all modern browsers, Node, and Bun, maps directly to `DataView.getBigUint64`/`setBigUint64`, and keeps values exact.

### Shipping a hand-maintained npm package or using Node + external YAML libraries for tests

Publishing a separate npm package or adding `js-yaml`/`vitest` dependencies would introduce lockfile maintenance and supply-chain surface. Running the TypeScript test suite directly with Bun (`bun test`, using built-in `Bun.YAML.parse`) reuses the toolchain already present in CI (`bun run verify-notes`) with zero third-party JS dependencies.

## Consequences

### Benefits

- All seven `pkg/online/netplay` wire formats have checked, zero-dependency TypeScript codecs verified byte for byte against Go via shared golden vectors and a live bidirectional `ServerWorld` to TypeScript E2E test.
- Future language backends (C#, GDScript, Rust) can consume the exact same `testdata/vectors/*.yaml` golden suite to prove wire compatibility in CI.
- `cmd/gamewire -lang ts -check` in CI guarantees committed TypeScript codecs never drift from `*.wire.yaml` schemas.

### Costs

- Updating a wire schema now requires regenerating both Go and TypeScript outputs (`go generate ./pkg/online/netplay/wire`) and updating golden vectors if wire bytes change.
- Pure-`BigInt` quantization in `runtime.ts` adds ~200 lines of integer IEEE-754 logic compared to float64 shortcuts, trading a small amount of runtime code size for exact bit-level agreement across languages.

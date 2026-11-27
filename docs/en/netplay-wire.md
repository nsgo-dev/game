# netplay/wire

`game/pkg/online/netplay/wire` defines the YAML wire message schema format used by `pkg/online/netplay` and game-specific binary payloads (such as typed `EntityState.Props`), pure-integer float quantization helpers, MSB-first bit packing, and a deterministic zero-allocation Go codec generator driven by `cmd/gamewire`.

```go
import "github.com/nsgo-dev/game/pkg/online/netplay/wire"
```

## Why YAML schemas with binary codecs

`pkg/online/netplay` keeps compact binary formats on the wire for four reasons: presence-bitmask deltas, varints, bit-exact determinism across amd64, arm64, and WebAssembly ([Determinism](determinism.md)), and zero-allocation encode/decode hot paths ([GC and Frame Allocations](gc-and-allocations.md)). Describing each protocol in a versioned YAML schema adds machine-readable specifications, generated and checked codecs, editor validation via JSON Schema (`https://game.nsgo.dev/schemas/wire.v1.json`), and shared metadata for packet inspection and cross-language codegen—without adding per-field wire tags, runtime allocations, or external compiler toolchains.

## Schema file layout

Every schema is a YAML file named `*.wire.yaml` with top-level `version: 1`. Unknown keys, duplicate keys, trailing YAML documents, and unsupported versions are refused by `wire.Parse`.

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/wire.v1.json
version: 1
package: props
order: le
enums:
  - name: Stance
    type: u8
    values:
      - name: idle
        value: 0
      - name: running
        value: 1
messages:
  - name: FighterProps
    fields:
      - name: team
        type: bits
        bits: 4
      - name: combo
        type: bits
        bits: 4
      - name: aim_x
        type: qfloat
        qfloat:
          min: -1
          max: 1
          bits: 12
      - name: aim_y
        type: qfloat
        qfloat:
          min: -1
          max: 1
          bits: 12
      - name: stance
        type: Stance
      - name: hp_delta
        type: svarint
        bits: 32
      - name: stamina
        type: fixed
        fixed:
          scale: 100
          type: u16
      - name: state
        type: mask
        mask:
          type: u8
          delta: true
          fields:
            - name: shield
              type: qfloat
              qfloat:
                min: 0
                max: 100
                bits: 10
            - name: tag
              type: string
```

Top-level keys:

| Key | Required | Description |
| --- | --- | --- |
| `version` | yes | Schema format version (`1`, matching `wire.CurrentVersion`). |
| `package` | yes | Default Go package name emitted by `cmd/gamewire` when `-pkg` is omitted. |
| `order` | no | Default byte order for multi-byte fields: `be` (default) or `le`. Messages and individual fields may override it. |
| `imports` | no | Relative paths to sibling `*.wire.yaml` files in the same package whose enums and messages this schema references. |
| `enums` | no | Named integer enumerations validated on decode. |
| `messages` | no | Ordered binary message layouts. |
| `unions` | no | Discriminated message envelopes selected by a leading tag or nibble-split byte. |

## Field types

| Type | Go type | Wire encoding | Options |
| --- | --- | --- | --- |
| `u8`, `u16`, `u32`, `u64` | `uint8`..`uint64` | 1, 2, 4, or 8 bytes in `order` (`be` or `le`) | `order`, `const` |
| `i8`, `i16`, `i32`, `i64` | `int8`..`int64` | 1, 2, 4, or 8 two's-complement bytes in `order` | `order` |
| `uvarint` | `uint64` (or `uint8`/`uint16`/`uint32` with `bits`) | Unsigned LEB128 (`encoding/binary`) | `bits` (`8`, `16`, `32`, `64`), `const` |
| `svarint` | `int64` (or `int8`/`int16`/`int32` with `bits`) | Zigzag signed varint (`encoding/binary`) | `bits` (`8`, `16`, `32`, `64`) |
| `bool` | `bool` | 1 byte (`0` or `1`; values `> 1` return `ErrInvalidBool`) | none |
| `f32` | `float32` | Raw IEEE-754 32-bit bits in `order` | `order` |
| `qfloat` | `float32` | Integer-quantized `bits` (1..32) across `[min, max]` | `qfloat: {min, max, bits}`, `order` |
| `fixed` | `float64` | Integer `round(v * scale)` encoded as `fixed.type` | `fixed: {scale, type, order}` |
| `bytes` | `[]byte` | Length prefix (`uvarint` by default) followed by raw bytes | `length` (`uvarint`, `u8`, `u16`, `u32`, `rest`), `order`, `maxLength` |
| `string` | `string` | Length prefix (`uvarint` by default) followed by UTF-8 bytes | `length` (`uvarint`, `u8`, `u16`, `u32`, `rest`), `order`, `maxLength` |
| `array` | `[N]T` or `[]T` | Fixed `count` elements, or `length`-prefixed slice of `of` | `array: {count, length, order, of, ofOrder, ofBits, maxLength}` |
| `bits` | `uint8`..`uint64` | `bits` (1..64) packed MSB-first into adjacent bit runs | `bits`, `const` |
| `mask` | `<Name>Mask` + gated fields | Presence integer (`u8`..`u64`) followed by set fields in bit order | `mask: {type, order, delta, fields}` |
| `<Enum>` | `<Enum>` | Underlying integer type; unknown values return `ErrInvalidEnum` | `const` |
| `<Message>` | `<Message>` | Embedded message bytes in field order | none |

### Constant fields (`const`)

Setting `const` on an unsigned integer, `bits`, or enum field emits a generated Go constant (`<Message><Field>`), omits the field from the struct, writes the constant value automatically in `AppendX`, and verifies it in `DecodeX` (returning `ErrInvalidConst` on mismatch). `pkg/net` uses this for fixed protocol headers such as `HandshakeHello.version = 1` and `RelayFrame.version = 1`.

### Bit packing and `qfloat` alignment

Consecutive `bits` fields and big-endian/arbitrary-width `qfloat` fields pack tightly MSB-first via `wire.BitWriter` and `wire.BitReader`. For example, two 12-bit `qfloat` fields occupy exactly 3 bytes on the wire instead of 4. When a bit-packed run ends before a byte-aligned field or the end of a message, unused low bits in the partial byte are zero-padded on encode and verified to be zero on decode (`ErrNonZeroPadding`). A `qfloat` with `bits` of 8, 16, or 32 and `order: le` encodes byte-aligned in little-endian order.

### Presence masks and `delta: true`

A `mask` field declares a presence bitmask (`u8`, `u16`, `u32`, or `u64`, defaulting to `u8`) followed by its gated fields in bit order (index `i` maps to `1 << i`). On decode, fields whose bit is clear are reset to their zero values (preserving slice capacity) so reusing a struct across packets never leaks stale data, and any unknown high bit beyond the declared fields returns `ErrInvalidMask`.

When `delta: true` is set, `cmd/gamewire` also generates:

- `Diff<Message>(base, cur *<Message>) <maskType>`: compares float fields by bit pattern (`math.Float32bits` / `math.Float64bits`), byte slices with `bytes.Equal`, and scalars directly—matching `pkg/net`'s replication delta comparison.
- `Apply<Message>(dst, delta *<Message>)`: copies every field whose mask bit is set in `delta` into `dst`, reusing `dst`'s slice capacities for zero allocations.

### Discriminated unions and nibble-split headers

A `union` encodes a leading discriminator (`u8`, `u16`, `uvarint`, or `nibble`) followed by the selected case message. The generated Go struct holds `Tag` (plus the high-bit field for `nibble`) and value fields for every case—no interface boxing or heap allocation.

With `discriminator.type: nibble`, byte 0 splits into `highBits` MSB bits (default 4, named `version` by default) and `8 - highBits` LSB tag bits (default 4). Replication v2 uses `highBits: 4`, `highValue: 2`, and case tags `1..5`: `DecodeReplicationMessage` validates the low-nibble tag first (`ErrInvalidTag`) and then checks the high nibble (`ErrInvalidVersion`) while storing the received version in `m.Version`.

## Integer-only quantization (`qfloat` and `fixed`)

`QuantizeFloat`, `DequantizeFloat`, `QuantizeFixed`, `DequantizeFixed`, `QuantizeFixedUint`, and `DequantizeFixedUint` decompose IEEE-754 bit patterns into integers and perform all scaling, division, and rounding exclusively with `math/bits` integer arithmetic. No floating-point instructions (and therefore no arm64 FMA contractions) execute on either encode or decode:

- `QuantizeFloat(v, min, max, bits)` clamps `NaN` and values `<= min` to `0`, clamps values `>= max` to `(1<<bits)-1`, and rounds halfway ties up.
- `DequantizeFloat(q, min, max, bits)` returns `min` for `0`, `max` for `(1<<bits)-1`, and the exact IEEE-754 round-to-nearest-even `float32` for intermediate steps.
- `QuantizeFixed(v, scale, minInt, maxInt)` computes `round(v * scale)` with halfway ties rounding away from zero, clamped to `[minInt, maxInt]`.
- Golden vectors in `pkg/net/wire/quantize_test.go` verify exact bit patterns on both `amd64` and `arm64` in CI.

## Generated Go API and zero-allocation contract

For every message and union `X`, `cmd/gamewire` generates:

```go
func AppendX(dst []byte, m *X) []byte
func DecodeX(src []byte, m *X) (int, error)
```

- `AppendX` appends encoded bytes to `dst` without allocating whenever `dst` has enough capacity (reuse a buffer with `dst = AppendX(dst[:0], &m)`).
- `DecodeX` fills caller-owned storage `m` and returns the exact number of bytes consumed. Variable-length `bytes` and `array` fields reuse existing slice backing capacity (`m.Slice[:0]` / `m.Slice[:count]`), including nested slices inside struct elements, so steady-state decoding performs zero heap allocations.
- `DecodeX` bounds variable-length `array` allocations against remaining input bytes before growing a slice, preventing malformed packets from triggering large allocations.
- Go `string` values are immutable; `DecodeX` avoids allocating when a string is empty or matches the previous value in `m`. For hot-path fields whose text changes every frame, prefer `bytes`.

## Using `cmd/gamewire`

Generate Go or TypeScript codecs from a directory or individual schema file:

```sh
go run ./cmd/gamewire -in ./netmsg -out ./netmsg
go run ./cmd/gamewire -lang ts -in ./netmsg -out ./netmsg/ts
```

Or wire it into `go generate`:

```go
//go:generate go run github.com/nsgo-dev/game/cmd/gamewire -in . -out .
//go:generate go run github.com/nsgo-dev/game/cmd/gamewire -lang ts -in . -out ts
```

CLI flags:

| Flag | Default | Description |
| --- | --- | --- |
| `-in` | `.` | Input `*.wire.yaml` file or directory containing `*.wire.yaml` files (positional file arguments are also accepted). |
| `-out` | input dir | Output directory, or a single `.go`/`.ts` file path when given one input schema. |
| `-pkg` | schema `package` | Override the generated Go package name. |
| `-lang` | `go` | Target language (`go` or `ts`). |
| `-self` | `false` | Omit the `pkg/online/netplay/wire` import for Go files generated inside `pkg/online/netplay/wire` itself. |
| `-check` | `false` | Verify committed generated files match their schemas without writing; exits non-zero if missing or stale. |

CI runs `go run ./cmd/gamewire -in pkg/online/netplay/wire -out pkg/online/netplay/wire -self -check`, `go run ./cmd/gamewire -lang ts -in pkg/online/netplay/wire -out pkg/online/netplay/wire/ts -check`, and `go -C tools run ./gameschema -check`.

## Generated TypeScript API and cross-language golden vectors

With `-lang ts`, `cmd/gamewire` writes a shared zero-dependency `runtime.ts` (containing `Writer`, `Reader`, `WireError`, and pure-`BigInt` quantization helpers), one `<stem>_wire.ts` module per schema, and a barrel `index.ts` (or a single self-contained `.ts` file with the runtime embedded when `-out` ends in `.ts`):

| Wire type | TypeScript type | Notes |
| --- | --- | --- |
| `u8`, `u16`, `u32`, `i8`, `i16`, `i32` | `number` | Fixed-width integer via `DataView` |
| `u64`, `i64` | `bigint` | Exact 64-bit integers above `Number.MAX_SAFE_INTEGER` |
| `uvarint`, `svarint` (`bits: 8\|16\|32`) | `number` | Range-checked LEB128 / zigzag varint |
| `uvarint`, `svarint` (default / `bits: 64`) | `bigint` | Full 64-bit unsigned or zigzag signed varint |
| `bits` (`1..32` / `33..64`) | `number` / `bigint` | MSB-first bit packing with zero-padding verification |
| `bool` | `boolean` | Strict `0` or `1` wire byte |
| `f32`, `qfloat`, `fixed` | `number` | Raw `DataView` float32 or pure-`BigInt` integer quantization |
| `bytes`, fixed `array` of `u8` | `Uint8Array` | Caller-owned buffer reads/writes; fixed byte arrays decode in place |
| `string`, `array` | `string`, `T[]` | Length-checked UTF-8 strings and element-reusing arrays |

- Every message and union `X` exports `createX()`, `encodeX(m: X, w: Writer): void`, and `decodeX(r: Reader, m: X): void`, mutating caller-owned objects in place to avoid per-packet allocations at 60 Hz.
- Delta `mask` groups export `diffX(base, cur)` (comparing IEEE-754 float bit patterns via `float32Bits`/`float64Bits` and bytes via `bytesEqual`) and `applyX(base, delta)`.
- Shared golden vectors under `pkg/online/netplay/wire/testdata/vectors/*.yaml` cover every built-in schema, varint and zigzag boundaries, `u64` above `2^53`, mixed endianness, `-0.0` and `NaN` `f32` bit patterns, `qfloat` range edges, empty and maximum-length bytes, delta `diff`/`apply`, and invalid packets. Both Go (`go tool nsgo test ./pkg/online/netplay/wire`) and Bun (`bun test pkg/online/netplay/wire/testdata/ts`) verify every vector byte for byte in CI.

## Built-in `pkg/online/netplay` wire schemas

Every existing `pkg/online/netplay` wire format is described verbatim under `pkg/online/netplay/wire/` (and embedded in `wire.BuiltinFS` / `wire.LoadBuiltins()`), with differential fuzz tests proving byte-identical encoding and matching decoding against `pkg/online/netplay`:

| Schema file | Byte order | Protocol and messages |
| --- | --- | --- |
| `transport.wire.yaml` | `be` | `Channel`, `TransportFrame` (`channel(1) seq(4) payload`), `UDPPeerFrame` (`from(4) channel(1) seq(4) payload`) |
| `udp_sealed.wire.yaml` | `be` | `UDPDatagramType` (`0xB1`..`0xB6`), `UDPSealedDatagram` (`type(1) peer(4) seq(4) channel(1) sealed`, ChaCha20-Poly1305 ciphertext + 16-byte tag) |
| `handshake.wire.yaml` | `be` | `HandshakeMessage` (`0xA1` `HandshakeHello`, `0xA2` `HandshakeWelcome`, `0xA3` `HandshakeReject`, version 1) |
| `relay.wire.yaml` | `be` | `RelayFrame` (version 1), `RelayMembers` (`0x01`), `RelayBody` (`0x01` `RelayUnreliable`, `0x02` `RelayReliable`, `0x03` `RelayAck`) |
| `replication.wire.yaml` | `le` | `EntityRecord` (delta-masked fields), `ReplicationMessage` (nibble version 2 + types `1..5`: `SnapshotFull`, `SnapshotDelta`, `SnapshotAck`, `ResyncRequest`, `SnapshotFullPart`) |
| `rollback.wire.yaml` | `be` | `InputFrame` (20 bytes), `RollbackMessage` (tags `10..12`: `SingleInput`, `InputBundle`, `ChecksumMsg`) |
| `membership.wire.yaml` | `be` | `MemberChangeKind`, `MatchMember`, `JoinState`, `MembershipMessage` (tags `13..14`: `MemberChange`, `JoinStatePart`) |

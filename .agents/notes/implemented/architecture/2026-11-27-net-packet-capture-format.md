# Agent Note: Net packet capture JSONL format and schema-driven inspector

Status: implemented

## Problem

Investigating multiplayer netcode bugs (dropped snapshots, rollback desyncs, relay stalls) previously required decoding raw binary frames by hand from source comments, because `pkg/online/netplay` transports only exposed `InboxStats` counters, `cmd/game` had no network inspection subcommand, and `pkg/debug` had no network overlay panel. Now that `pkg/online/netplay/wire` (#1301) provides versioned YAML schemas and zero-allocation Go codecs for all seven protocols, packet capture, offline layered inspection, traffic statistics, and a live zero-allocation overlay can share the same protocol definitions.

## Decision

We add a text-based, versioned JSON Lines packet capture format in `pkg/online/netplay`, a schema-driven dynamic codec and layered inspector in `pkg/online/netplay/wire`, the `game net decode` and `game net stats` subcommands in `internal/gamecli`, and a live zero-allocation network panel in `pkg/debug`.

### Capture file format (version 1)

`netplay.NewCaptureTransport(inner, w, opts)` wraps any `netplay.Transport` (and forwards `netplay.InboxReporter`) and writes one UTF-8 JSON object per line (`*.capture.jsonl` / `*.jsonl`), terminated by `\n`:

```json
{"version":1,"t":16000000,"dir":"send","peer":2,"ch":1,"seq":1,"payload":"IwEAAAAB"}
```

Every line is self-contained so standard Unix tools (`head`, `tail`, `grep`, piping a single line to `game net decode -`) work without maintaining file-level headers:

- `version` (required integer): format version, currently `netplay.CaptureVersion` (`1`). `ParseCaptureRecord` refuses missing, zero, or unsupported versions.
- `t` (required non-negative int64): monotonic elapsed nanoseconds since `NewCaptureTransport` started capturing, computed from `CaptureOptions.Now` (defaulting to `time.Now`). Using elapsed nanoseconds avoids leaking wall-clock timestamps and keeps test captures deterministic.
- `dir` (required string): `"send"` (`CaptureSend`) or `"recv"` (`CaptureRecv`).
- `peer` (required uint32): destination `PeerID` (or `0` for `PeerBroadcast`) on send, or `Packet.From` on receive.
- `ch` (required uint8): `0` (`ChannelReliableOrdered`) or `1` (`ChannelUnreliable`).
- `seq` (required uint32): transport sequence number (received `Packet.Seq`, or a per-`(peer, ch)` monotonic counter starting at 1 on send).
- `payload` (required string): standard base64-encoded plaintext transport payload bytes (`""` when empty; `null` or missing is refused).

Because `NewCaptureTransport` wraps the `Transport` boundary, wrapping `UDPClient` or `UDPServer` records plaintext packets after authentication and decryption so captures never require session keys. Captures contain player inputs and game state and must never be shipped or collected without consent.

### Stability rules

- Strict decoding: `ParseCaptureRecord` enables `DisallowUnknownFields()`, refuses trailing tokens after the JSON object, validates all enums/bounds, and requires `version: 1`.
- Additive evolution: any future optional field bumps `CaptureVersion` (or adds a new schema version with an explicit migration path in `ParseCaptureRecord`) once `pkg/online/netplay` becomes stable; existing version 1 lines remain readable without breaking existing tooling.
- Lossless inspection: `wire.Registry` and `wire.Inspector` dynamically decode and re-encode every schema type (`bits`, `qfloat`, `fixed`, `varint`, `mask`, `array`, `enum`, `union`, custom `--schema` `Props`) using the `*.wire.yaml` AST, peeling trailing payload fields into inner layers (`transport` -> `relay` -> `replication`/`rollback`/`membership`/`handshake`/custom). Re-encoding every decoded frame reproduces the captured payload byte for byte.
- Zero-allocation overlay: `debug.NetMonitor` decodes observed packets using the `#1301` generated `wire.Decode*` codecs into reused struct buffers, maintains fixed-size rate windows and a preallocated peer table and recent-event ring buffer, and `Overlay.RecordFrame` copies snapshots into reused slices—verified at 0 allocs/op by `debug.AssertZeroAllocs`.

## Alternatives considered

### PCAP / PCAPNG capture files

Writing or reading pcap/pcapng files would integrate with Wireshark out of the box, but it is a binary container format (violating the repository's rule that tool-written files are text), would require a third-party dependency such as `github.com/google/gopacket`, and would capture encrypted UDP datagrams requiring key export.

### File-level JSON header line instead of per-record `version`

Putting `{"version":1}` only on line 1 would save 12 bytes per line, but would break slicing captures with `head`/`tail`/`grep` or piping individual captured frames into `game net decode -`. Repeating `"version":1` on every line keeps each line independently valid and self-describing.

## Consequences

- Any `Transport` (loopback, WebSocket, UDP, or relay) can be captured to a text JSONL file with channel/peer filtering and deterministic sampling, and inspected with `game net decode` (including `--schema` for game-defined `Props`, `--resolve-deltas` for full entity states, and hex/base64 stdin) and `game net stats`.
- Golden tests in `pkg/online/netplay` verify deterministic decoded YAML for loopback replication, lockstep, and relay sessions and prove byte-for-byte re-encoding of every frame.
- `pkg/debug` gains a live network panel (`Options.Net` / `NewNetMonitor`) with zero steady-state heap allocations and zero-cost `-tags release` stubs.

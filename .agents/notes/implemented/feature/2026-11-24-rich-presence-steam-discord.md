# Agent Note: Rich presence on Steam and Discord

Status: implemented

## Problem

Desktop games built with `nsgo-dev/game` need to display what the player is currently doing—level, game mode, party size, elapsed time, and join tokens—in Steam and Discord friend lists without hand-written C, cgo build requirements, or platform-specific call sites scattered across gameplay code. At the same time, games target mobile and web builds where neither Discord desktop IPC nor Steamworks exists, and desktop players may run with only Steam, only Discord, or neither client open.

## Decision

Package `packages/game/presence` defines a provider-neutral `Activity` value, a three-method `Provider` interface (`Set`, `Clear`, `Close`), and a `Manager` that fans updates out to all configured providers simultaneously while keeping the exported API identical across every NSGo target (`macos`, `windows`, `linux`, `ios`, `android`, `web`).

### Wire and native contracts

- **Discord IPC (`Discord`)**: speaks Discord's local IPC v1 protocol in pure Go with little-endian `[opcode uint32][length uint32][json payload]` framing. Build-tagged transports dial `discord-ipc-0`..`9` over Unix domain sockets (`$XDG_RUNTIME_DIR` including Flatpak/Snap paths, `$TMPDIR`, `$TMP`, `$TEMP`, `/tmp`) on macOS/Linux, open `\\.\pipe\discord-ipc-0`..`9` via `syscall.CreateFile` on Windows, and return `ErrUnavailable` on web/mobile/other targets. `Clear` explicitly emits `"activity":null` in `SET_ACTIVITY`. Connections open lazily, respond to `PING` frames with `PONG`, ignore unrelated `DISPATCH` events while awaiting a matching `nonce`, and perform one automatic reconnect + handshake when an established transport drops.
- **Steam Rich Presence (`Steam`)**: dynamically loads the Steamworks shared library (`libsteam_api.dylib`, `libsteam_api.so`, `steam_api64.dll`, or `steam_api.dll`) via `github.com/ebitengine/purego` (Unix `purego.Dlopen`/`Dlsym`, Windows `syscall.LoadDLL` + `purego.RegisterFunc`) with zero cgo, probing `SteamAPI_SteamFriends_v018`, `v017`, `v015`, and `SteamFriends` together with `SteamAPI_ISteamFriends_SetRichPresence` and `SteamAPI_ISteamFriends_ClearRichPresence`. `Activity` maps to `status`, `level`, `mode`, `steam_player_group`, `steam_player_group_size`, `connect`, optional `steam_display`, and `Custom` overrides. Keys are validated against Steamworks limits (`SteamMaxKeys = 30`, `SteamMaxKeyLength = 63`), values are truncated on UTF-8 rune boundaries at `SteamMaxValueLength = 255`, and removed keys between updates are cleared individually via `SetRichPresence(key, "")` so unchanged keys do not flicker.
- **Manager semantics (`Manager`)**: deduplicates unchanged `Activity` updates via deep-cloned state, supports synchronous rate-limit coalescing through `ManagerConfig.MinInterval` + `Poll`/`Flush` with an injectable `Now` clock (zero hidden background goroutines), and degrades gracefully when one provider returns `ErrUnavailable` while another succeeds (`ErrUnavailable` is returned only when no provider succeeds and no real error occurred; non-unavailable errors never short-circuit healthy providers).

## Alternatives considered

- **Linking the C++ Steamworks SDK via cgo** — Wrapping Steamworks headers in cgo would give compile-time type checking against a pinned SDK version. Rejected because `nsgo-dev/game` forbids hand-written C (`AGENTS.md`), cgo breaks zero-C cross-compilation for `nsgo check -target macos,windows,linux,android,web`, and dynamic symbol resolution via `purego` already works with the `ebitengine/purego` dependency present in `packages/game/go.mod` while allowing games to run gracefully when `libsteam_api` is absent.
- **Spawning a background debounce goroutine inside `Manager`** — A timer goroutine would flush `MinInterval`-throttled updates automatically without the caller calling `Poll`. Rejected because hidden goroutines complicate deterministic testing, risk shutdown races and leaks if a caller forgets `Close`, and break the explicit loop-driven lifecycle used across `packages/game`. Synchronous `Poll` and `Flush` with an injectable `Now` clock remain deterministic and race-free while still letting games opt into `MinInterval` throttling.
- **Calling `ClearRichPresence()` before every Steam `Set` update** — Clearing all keys before re-applying them is simpler than diffing `lastKeys`. Rejected because `ClearRichPresence()` briefly empties the friend-list status between calls and redundantly re-sends unchanged keys such as `steam_display` and `steam_player_group` on every level transition.

## Consequences

- **Benefits**:
  - Single cross-platform API (`SetLevel`, `Update`, `SetParty`, `Clear`) updates both Steam and Discord friend lists simultaneously with zero cgo and zero external daemons required in tests.
  - Identical exported Go API across all six NSGo targets keeps `apiref` and game code free of build-tag conditionals.
  - Deterministic unit tests verify raw Discord binary framing over `net.Pipe()` and real Unix domain sockets, Steam key/value diffing and limits via `SteamDriver`, and multi-provider `Manager` fan-out and rate-limiting without sleeps.
- **Costs and known limits**:
  - Steam dynamic binding requires the game's distribution to ship the Steamworks redistributable library beside the binary (or configure `SteamConfig.LibraryPath`); without it, `Steam` reports `ErrUnavailable`.
  - Games enabling `ManagerConfig.MinInterval` must call `Manager.Poll` periodically (for example from `game.Loop.Update`) or `Flush` on transitions to deliver coalesced updates; if a future use case requires background timers, a wrapper can be layered outside `Manager`.
  - Discord IPC on Windows uses blocking `syscall.CreateFile` pipe handles; if non-blocking overlapped I/O or Discord join-request callbacks (`ACTIVITY_JOIN`) are needed later, the Discord transport must be extended.

## Verification

- `packages/game/presence/discord_test.go` and `discord_unix_test.go` verify v1 binary framing, handshake, ping/pong, async dispatch filtering, `SET_ACTIVITY` updates across level changes, explicit `"activity":null` clear, automatic reconnect on broken pipe, UTF-8 truncation, and real Unix domain socket discovery.
- `packages/game/presence/steam_test.go` verifies `ISteamFriends` key mapping, incremental level-change diffs, removed-key deletion, `ClearRichPresence`, `SteamMaxKeys`/`SteamMaxKeyLength`/`SteamMaxValueLength` enforcement, and `ErrUnavailable` when the library is missing.
- `packages/game/presence/presence_test.go` verifies simultaneous Steam + Discord updates on `SetLevel`, deduplication, fake-clock `MinInterval` coalescing, and partial `ErrUnavailable` degradation.
- Verified with `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./presence/... .`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.

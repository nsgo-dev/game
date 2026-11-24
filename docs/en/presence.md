# presence

```go
import "github.com/nsgo-dev/game/packages/game/presence"
```

`presence` publishes what the player is doing (level, game mode, party size, timestamps, artwork, and custom localization tokens) to Steam and Discord friend lists through a single provider-neutral API. Both built-in providers are pure Go with no cgo or hand-written C: `Discord` speaks Discord's local IPC v1 protocol directly over Unix domain sockets or Windows named pipes, and `Steam` dynamically loads the Steamworks shared library through `purego` to call `ISteamFriends::SetRichPresence` and `ClearRichPresence`.

## Activity

An `Activity` describes the player's current state. Every field is optional; providers translate whichever fields are populated into their native representation.

| Field | Discord mapping | Steam mapping |
| --- | --- | --- |
| `Level` | `state` (fallback when `Status` is empty; `details` if `Mode` and `Details` are empty) | `level` key (`%level%`) and `status` summary |
| `Mode` | `details` (fallback when `Details` is empty) | `mode` key (`%mode%`) and `status` summary |
| `Details` | `details` | `status` summary (when `Status` is empty) |
| `Status` | `state` | `status` key |
| `PartyID` | `party.id` | `steam_player_group` |
| `PartySize`, `PartyMax` | `party.size` (`[size, max]`, clamped so `max >= size`) | `steam_player_group_size` |
| `StartTime`, `EndTime` | `timestamps.start`, `timestamps.end` (Unix seconds) | — |
| `LargeImage`, `LargeText`, `SmallImage`, `SmallText` | `assets.*` | — |
| `JoinSecret` | `secrets.join` | `connect` (when `Custom["connect"]` is unset) |
| `Custom` | — | Additional or overriding Steam keys (such as `steam_display`, `connect`, or localization token parameters); setting a key to `""` omits it |

## Updating presence across Steam and Discord

A `Manager` fans updates out to every configured `Provider`, deduplicates unchanged activities, optionally throttles bursts with `MinInterval`, and degrades gracefully when one platform client is not running.

```go
discord, err := presence.NewDiscord(presence.DiscordConfig{
	ClientID: "123456789012345678",
})
if err != nil {
	return err
}
steam := presence.NewSteam(presence.SteamConfig{
	DisplayToken: "#Status_InLevel",
})

mgr := presence.NewManager(
	presence.ManagerConfig{MinInterval: presence.DefaultMinInterval},
	steam,
	discord,
)
defer mgr.Close()

// Set initial state when entering gameplay:
_ = mgr.Update(ctx, presence.Activity{
	Mode:       "Campaign",
	Level:      "Forest 1-2",
	PartyID:    "squad-42",
	PartySize:  2,
	PartyMax:   4,
	LargeImage: "world_forest",
})

// Changing level updates both Steam and Discord while keeping Mode, party and artwork:
_ = mgr.SetLevel(ctx, "Cavern 2-1")

// Flush throttled updates from your game loop when MinInterval > 0:
_ = mgr.Poll(ctx)

// Clear presence when returning to the title screen or exiting:
_ = mgr.Clear(ctx)
```

- `SetLevel`, `SetMode` and `SetParty` update those fields on the current desired activity while preserving the remaining fields.
- Calling `Update` or `SetLevel` with a state equal to what was already sent is a no-op.
- When `ManagerConfig.MinInterval` is positive (such as `DefaultMinInterval`, 4 seconds), updates arriving inside the window are coalesced so only the latest state is kept pending; call `Poll(ctx)` once the interval elapses or `Flush(ctx)` to send immediately.
- `Clear(ctx)` cancels any pending update and clears presence on every provider.

## Discord IPC

`NewDiscord(DiscordConfig{ClientID: ...})` connects lazily on the first `Set` or `Clear` (or eagerly via `Connect(ctx)`):

- **macOS and Linux**: scans `discord-ipc-0` through `discord-ipc-9` in `$XDG_RUNTIME_DIR` (including Flatpak `app/com.discordapp.Discord` and Snap `snap.discord`), `$TMPDIR`, `$TMP`, `$TEMP`, and `/tmp`.
- **Windows**: opens `\\.\pipe\discord-ipc-0` through `\\.\pipe\discord-ipc-9` via `syscall.CreateFile`.
- **Web, iOS and Android**: returns `ErrUnavailable`.

Frames use Discord's v1 little-endian header (`[opcode uint32][length uint32][json payload]`). `Discord` performs the v1 handshake (`opcode 0`), responds to `PING` frames with `PONG`, sends `SET_ACTIVITY` (`opcode 1`) with `activity: null` on `Clear`, and automatically attempts one reconnect if an active socket breaks mid-session. Text fields longer than `DiscordMaxTextLength` (128 bytes) are truncated on UTF-8 rune boundaries.

## Steam Rich Presence

`NewSteam(SteamConfig{})` loads the Steamworks shared library lazily on desktop targets (`libsteam_api.dylib` on macOS, `libsteam_api.so` on Linux, `steam_api64.dll` / `steam_api.dll` on Windows) next to the executable or on the system loader path, or from `SteamConfig.LibraryPath`. It probes `SteamAPI_SteamFriends_v018`, `v017`, `v015` and `SteamFriends` together with `SteamAPI_ISteamFriends_SetRichPresence` and `SteamAPI_ISteamFriends_ClearRichPresence`.

- By default `Steam` assumes the game already initialized Steamworks; set `SteamConfig.AutoInit: true` to call `SteamAPI_Init` when `ISteamFriends` is null (and `SteamAPI_Shutdown` on `Close`).
- Keys removed between consecutive `Set` calls are deleted via `SetRichPresence(key, "")` without calling `ClearRichPresence()`, while unchanged keys are skipped.
- Keys are validated against Steamworks limits (`SteamMaxKeys = 30`, `SteamMaxKeyLength = 63` bytes; invalid keys return `ErrSteamKeyInvalid` or `ErrSteamTooManyKeys`). Values longer than `SteamMaxValueLength` (255 bytes) are truncated on UTF-8 rune boundaries.
- On web, iOS, Android, or when the Steamworks library or client is absent, `Set` and `Clear` return `ErrUnavailable`.

## Graceful degradation

If at least one provider in a `Manager` succeeds and the remaining providers return `ErrUnavailable` (for example Steam is running while Discord is closed), `Update`, `SetLevel`, `Clear`, `Poll` and `Flush` return `nil`. If every configured provider returns `ErrUnavailable`, they return `ErrUnavailable`. Real provider errors are joined and returned without short-circuiting healthy providers. Set `ManagerConfig.OnError` to observe every per-provider error, including `ErrUnavailable`.

## Testing

Both providers accept injectable backends so tests run deterministically without Discord or Steam installed:

- Pass `DiscordConfig.Dial` with a `net.Pipe()` connected to an in-process mock IPC server to verify raw wire frames, handshake, `SET_ACTIVITY`, and `Clear`.
- Pass `SteamConfig.Driver` with a mock `SteamDriver` to assert every `SetRichPresence(key, value)` and `ClearRichPresence()` call.
- Pass `ManagerConfig.Now` with a controllable clock to test `MinInterval` coalescing without sleeps.

# steamworks

```go
import "github.com/nsgo-dev/game/pkg/online/steamworks"
```

`steamworks` owns the one Steamworks client a process may have. It loads the Steamworks shared library through `purego` (no cgo or hand-written C), initializes Steamworks once, pumps Steam's callbacks once per display frame on the engine goroutine, and shuts Steamworks down when the last scope that opened it is disposed. [`presence.Steam`](presence.md#steam-rich-presence) and [`modding.SteamWorkshop`](modding.md#steam-workshop-workshop) attach to the same client instead of initializing Steamworks themselves. Achievements, stats and leaderboards go through [`steam`](steam-stats.md), whose `Driver` can be implemented on this client.

## Opening the client

```go
client, err := steamworks.Open(cx, steamworks.Options{
	AppID:              480, // your Steamworks App ID
	RestartIfNecessary: true,
})
switch {
case errors.Is(err, steamworks.ErrRestartRequired):
	os.Exit(0) // Steam is relaunching the game through the Steam client
case errors.Is(err, steamworks.ErrUnavailable):
	// No library, no running Steam client, or a target without Steam: play without Steam features.
case err != nil:
	return err
}
```

- `Open` loads `libsteam_api.dylib` (beside the executable or in `Contents/Frameworks`), `libsteam_api.so` (beside the executable or in `lib64`) or `steam_api64.dll` (beside the executable), or the file in `Options.LibraryPath`. Bundle the library with `nsgo-natives.yaml` as [Shipping on Steam](steam.md#bundling-the-steamworks-redistributable-nsgo-nativesyaml) describes, and set `steam.app_id` in `nsgo.yaml` for runs outside the Steam client.
- With `RestartIfNecessary` and a non-zero `AppID`, `Open` calls `SteamAPI_RestartAppIfNecessary` first and returns `ErrRestartRequired` when Steam relaunches the game.
- Initialization goes through `SteamAPI_InitFlat`. A failure returns an `*InitError` carrying the `ESteamAPIInitResult` (`InitNoSteamClient`, `InitVersionMismatch`, ...) and Steam's message; it matches `ErrUnavailable`.
- A second `Open` from another scope returns the same `*Client` without initializing again; it refuses a different non-zero `AppID` (`ErrAppIDMismatch`) or a different `Options.Driver` (`ErrDriverConflict`). `Current` returns the open client, or nil.
- Callbacks run through `SteamAPI_ManualDispatch_*` once per display frame. Every event handler below runs inside that frame, on the engine goroutine. When the scope that owns the pump is disposed, the pump moves to another attached scope; when the last one is disposed, `SteamAPI_Shutdown` runs, pending waits fail with `ErrClosed` and the client's methods return zero values.
- On web, iOS, Android and other targets without Steam, `Open` returns `ErrUnavailable` unless `Options.Driver` supplies a driver.

## Events

| Method | Steamworks callback | Event |
| --- | --- | --- |
| `OnOverlay` | `GameOverlayActivated_t` | `OverlayEvent{Active, UserInitiated, AppID}`; `OverlayActive` reports the last state |
| `OnJoin` | `GameRichPresenceJoinRequested_t`, or the `+connect` launch argument | `JoinEvent{FriendID, Connect, FromLaunch}` |
| `OnLobbyJoin` | `GameLobbyJoinRequested_t`, or the `+connect_lobby` launch argument | `LobbyJoinEvent{LobbyID, FriendID, FromLaunch}` |
| `OnShutdown` | `SteamShutdown_t` | `ShutdownEvent{}` |
| `OnGamepadTextInput` | `GamepadTextInputDismissed_t` | `GamepadTextInputEvent{Submitted, Text, AppID}` after `ShowGamepadTextInput` |
| `OnCallback` | any callback ID | a copy of the raw callback struct, for packages that decode their own callbacks (Workshop uses it for `ItemInstalled_t`) |

Each returns a function that unregisters the handler. A join that started the game (`+connect` or `+connect_lobby` in `os.Args`, or in `Options.Args`) is delivered once, with `FromLaunch` set, on the first frame after a handler is registered, so a handler registered after `Open` does not miss it. `LaunchConnect` returns the captured `+connect` value.

## Pausing while the overlay is open

```go
stop := client.PauseOnOverlay(loop)
defer stop()
```

`PauseOnOverlay` pauses a `game.Loop` while the Steam Overlay is open and resumes it when the overlay closes. A pause that was already set when the overlay opened, such as an open pause menu, stays set; `stop` unregisters the handler and resumes the loop only if the helper paused it.

## Identity, overlay and auth tickets

- `SteamID`, `PersonaName`, `AppID`, `RunningOnSteamDeck` and `OverlayEnabled` read the signed-in user and session.
- `OpenOverlay(dialog)` and `OpenOverlayURL(url)` open the overlay to a dialog (`"friends"`, `"achievements"`, ...) or a web page.
- `SetRichPresence` and `ClearRichPresence` write `ISteamFriends` rich presence; `presence.Steam` with `SteamConfig.Client` calls them for you.
- `AuthTicketForWebAPI(ctx, identity)` requests a session ticket with `GetAuthTicketForWebApi` and waits for `GetTicketForWebApiResponse_t`; send `AuthTicket.Hex()` to your server, which checks it with the `ISteamUserAuth/AuthenticateUserTicket` Web API using the same identity. Canceling `ctx` cancels the ticket.
- `AwaitCall(ctx, call, callbackID)` waits for a `SteamAPICall_t` result delivered by the pump; `ResolveSymbol` and `CallbackPack` let packages bind further flat API functions from the same library.

## Testing

`NewFakeDriver` returns an in-process `*FakeDriver`. Pass it in `Options.Driver`, queue callbacks with `EmitOverlay`, `EmitJoin`, `EmitLobbyJoin`, `EmitShutdown`, `EmitGamepadTextInput`, `EmitAuthTicket`, `CompleteAPICall` or `EmitCallback`, run a display frame, and inspect `InitCount`, `ShutdownCount`, `RichPresence` and the other counters.

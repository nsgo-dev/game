# steamworks

```go
import "github.com/nsgo-dev/game/pkg/online/steamworks"
```

`steamworks` 管理一个进程内唯一的 Steamworks 客户端。它通过 `purego` 加载 Steamworks 共享库（无需 cgo 或手写 C），只初始化一次 Steamworks，在引擎 goroutine 上每个显示帧泵送一次 Steam 回调，并在最后一个打开它的 scope 销毁时关闭 Steamworks。[`presence.Steam`](presence.md#steam-丰富状态) 和 [`modding.SteamWorkshop`](modding.md#steam-创意工坊workshop) 挂接到同一个客户端，不再各自初始化 Steamworks。成就、统计和排行榜走 [`steam`](steam-stats.md)，它的 `Driver` 可以基于这个客户端实现。

## 打开客户端

```go
client, err := steamworks.Open(cx, steamworks.Options{
	AppID:              480, // 你的 Steamworks App ID
	RestartIfNecessary: true,
})
switch {
case errors.Is(err, steamworks.ErrRestartRequired):
	os.Exit(0) // Steam 正通过 Steam 客户端重新启动游戏
case errors.Is(err, steamworks.ErrUnavailable):
	// 没有库、Steam 客户端未运行，或目标平台没有 Steam：不使用 Steam 功能继续运行。
case err != nil:
	return err
}
```

- `Open` 加载 `libsteam_api.dylib`（可执行文件同目录或 `Contents/Frameworks`）、`libsteam_api.so`（可执行文件同目录或 `lib64`）或 `steam_api64.dll`（可执行文件同目录），或 `Options.LibraryPath` 指定的文件。按[发布到 Steam](steam.md#打包-steamworks-可再分发库nsgo-nativesyaml) 所述用 `nsgo-natives.yaml` 打包该库；在 Steam 客户端之外运行时，在 `nsgo.yaml` 中设置 `steam.app_id`。
- 设置 `RestartIfNecessary` 且 `AppID` 非零时，`Open` 先调用 `SteamAPI_RestartAppIfNecessary`，Steam 重新启动游戏时返回 `ErrRestartRequired`。
- 初始化使用 `SteamAPI_InitFlat`。失败时返回 `*InitError`，其中包含 `ESteamAPIInitResult`（`InitNoSteamClient`、`InitVersionMismatch` 等）和 Steam 给出的消息；它匹配 `ErrUnavailable`。
- 其他 scope 再次调用 `Open` 时返回同一个 `*Client`，不会重复初始化；`AppID` 不同（非零）时返回 `ErrAppIDMismatch`，`Options.Driver` 不同时返回 `ErrDriverConflict`。`Current` 返回当前打开的客户端，没有则返回 nil。
- 回调经 `SteamAPI_ManualDispatch_*` 每个显示帧处理一次。下文所有事件处理函数都在该帧内、在引擎 goroutine 上运行。持有泵的 scope 销毁时，泵转交给另一个已挂接的 scope；最后一个 scope 销毁时调用 `SteamAPI_Shutdown`，未完成的等待以 `ErrClosed` 失败，客户端方法返回零值。
- 在 Web、iOS、Android 等没有 Steam 的目标上，除非 `Options.Driver` 提供了驱动，`Open` 返回 `ErrUnavailable`。

## 事件

| 方法 | Steamworks 回调 | 事件 |
| --- | --- | --- |
| `OnOverlay` | `GameOverlayActivated_t` | `OverlayEvent{Active, UserInitiated, AppID}`；`OverlayActive` 返回最近一次状态 |
| `OnJoin` | `GameRichPresenceJoinRequested_t`，或 `+connect` 启动参数 | `JoinEvent{FriendID, Connect, FromLaunch}` |
| `OnLobbyJoin` | `GameLobbyJoinRequested_t`，或 `+connect_lobby` 启动参数 | `LobbyJoinEvent{LobbyID, FriendID, FromLaunch}` |
| `OnShutdown` | `SteamShutdown_t` | `ShutdownEvent{}` |
| `OnGamepadTextInput` | `GamepadTextInputDismissed_t` | 调用 `ShowGamepadTextInput` 后的 `GamepadTextInputEvent{Submitted, Text, AppID}` |
| `OnCallback` | 任意回调 ID | 原始回调结构体的副本，供自行解码回调的包使用（Workshop 用它接收 `ItemInstalled_t`） |

每个方法都返回一个注销处理函数的函数。启动游戏的那次加入（`os.Args` 或 `Options.Args` 中的 `+connect`、`+connect_lobby`）会在注册处理函数后的第一帧投递一次，并设置 `FromLaunch`，所以在 `Open` 之后注册的处理函数也不会错过它。`LaunchConnect` 返回捕获到的 `+connect` 值。

## Overlay 打开时暂停

```go
stop := client.PauseOnOverlay(loop)
defer stop()
```

`PauseOnOverlay` 在 Steam Overlay 打开时暂停 `game.Loop`，关闭时恢复。Overlay 打开前已经设置的暂停（例如打开的暂停菜单）保持不变；`stop` 注销处理函数，并且只在暂停是它设置的情况下恢复循环。

## 身份、Overlay 与身份票据

- `SteamID`、`PersonaName`、`AppID`、`RunningOnSteamDeck` 和 `OverlayEnabled` 读取已登录用户和会话信息。
- `OpenOverlay(dialog)` 和 `OpenOverlayURL(url)` 将 Overlay 打开到指定对话框（`"friends"`、`"achievements"` 等）或网页。
- `SetRichPresence` 和 `ClearRichPresence` 写入 `ISteamFriends` 富状态；设置了 `SteamConfig.Client` 的 `presence.Steam` 会替你调用它们。
- `AuthTicketForWebAPI(ctx, identity)` 用 `GetAuthTicketForWebApi` 请求会话票据并等待 `GetTicketForWebApiResponse_t`；把 `AuthTicket.Hex()` 发给服务器，由服务器用相同的 identity 调用 `ISteamUserAuth/AuthenticateUserTicket` Web API 校验。取消 `ctx` 会取消该票据。
- `AwaitCall(ctx, call, callbackID)` 等待泵送来的 `SteamAPICall_t` 结果；`ResolveSymbol` 和 `CallbackPack` 让其他包从同一个库绑定更多 flat API 函数。

## 测试

`NewFakeDriver` 返回进程内的 `*FakeDriver`。将它传入 `Options.Driver`，用 `EmitOverlay`、`EmitJoin`、`EmitLobbyJoin`、`EmitShutdown`、`EmitGamepadTextInput`、`EmitAuthTicket`、`CompleteAPICall` 或 `EmitCallback` 排队回调，运行一个显示帧，再检查 `InitCount`、`ShutdownCount`、`RichPresence` 等计数。

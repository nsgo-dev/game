# presence

```go
import "github.com/nsgo-dev/game/pkg/online/presence"
```

`presence` 通过一套与平台无关的 API，把玩家当前正在做什么（关卡、游戏模式、队伍人数、计时器、图标资源和自定义本地化键）同步到 Steam 和 Discord 的好友列表。两个内置提供者均使用纯 Go 实现，不含 cgo 或手写 C：`Discord` 直接通过 Unix 域套接字或 Windows 命名管道实现 Discord 本地 IPC v1 协议；`Steam` 在桌面端通过 `purego` 动态加载 Steamworks 共享库，调用 `ISteamFriends::SetRichPresence` 和 `ClearRichPresence`。

## Activity

`Activity` 描述玩家当前状态。每个字段都是可选的，提供者会把填写的字段映射到各自平台的格式。

| 字段 | Discord 映射 | Steam 映射 |
| --- | --- | --- |
| `Level` | `state`（`Status` 为空时回退；若 `Mode` 和 `Details` 均为空则填充 `details`） | `level` 键（`%level%`）与 `status` 摘要 |
| `Mode` | `details`（`Details` 为空时回退） | `mode` 键（`%mode%`）与 `status` 摘要 |
| `Details` | `details` | `status` 摘要（`Status` 为空时） |
| `Status` | `state` | `status` 键 |
| `PartyID` | `party.id` | `steam_player_group` |
| `PartySize`, `PartyMax` | `party.size`（`[size, max]`，保证 `max >= size`） | `steam_player_group_size` |
| `StartTime`, `EndTime` | `timestamps.start`、`timestamps.end`（Unix 秒） | — |
| `LargeImage`, `LargeText`, `SmallImage`, `SmallText` | `assets.*` | — |
| `JoinSecret` | `secrets.join` | `connect`（未设置 `Custom["connect"]` 时） |
| `Custom` | — | 额外或覆盖的 Steam 键值对（如 `steam_display`、`connect` 或本地化模板参数）；设为空字符串 `""` 可省略该键 |

## 同时更新 Steam 与 Discord 状态

`Manager` 把状态扇出到所有已配置的 `Provider`，对未变化的状态去重，可选地通过 `MinInterval` 合并高频更新，并在某个平台客户端未运行时优雅降级。

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

// 进入对局时设置初始状态：
_ = mgr.Update(ctx, presence.Activity{
	Mode:       "Campaign",
	Level:      "Forest 1-2",
	PartyID:    "squad-42",
	PartySize:  2,
	PartyMax:   4,
	LargeImage: "world_forest",
})

// 切换关卡时同时更新 Steam 和 Discord，并保留模式、队伍和图标：
_ = mgr.SetLevel(ctx, "Cavern 2-1")

// 开启 MinInterval 时，在游戏循环里周期性调用 Poll 推送合并后的更新：
_ = mgr.Poll(ctx)

// 回到标题画面或退出时清除状态：
_ = mgr.Clear(ctx)
```

- `SetLevel`、`SetMode` 和 `SetParty` 更新当前期望活动中的对应字段，并保留其余字段。
- 使用与已发送状态相同的 `Activity` 重复调用 `Update` 或 `SetLevel` 不会触发重复发送。
- `ManagerConfig.MinInterval` 大于 0 时（如 `DefaultMinInterval`，4 秒），窗口期内连续到达的更新会合并为最新状态并挂起；间隔过后调用 `Poll(ctx)` 推送，或调用 `Flush(ctx)` 立即推送。
- `Clear(ctx)` 取消挂起更新并清除所有提供者的状态。

## Discord IPC

`NewDiscord(DiscordConfig{ClientID: ...})` 在首次 `Set` 或 `Clear` 时按需建立连接（也可提前调用 `Connect(ctx)`）：

- **macOS 与 Linux**：依次在 `$XDG_RUNTIME_DIR`（含 Flatpak 的 `app/com.discordapp.Discord` 与 Snap 的 `snap.discord`）、`$TMPDIR`、`$TMP`、`$TEMP` 和 `/tmp` 下查找 `discord-ipc-0` 到 `discord-ipc-9`。
- **Windows**：通过 `syscall.CreateFile` 打开 `\\.\pipe\discord-ipc-0` 到 `\\.\pipe\discord-ipc-9`。
- **Web、iOS 与 Android**：返回 `ErrUnavailable`。

协议帧采用 Discord v1 小端帧头（`[opcode uint32][length uint32][json payload]`）。`Discord` 会完成 v1 握手（`opcode 0`），自动响应 `PING` 返回 `PONG`，通过 `SET_ACTIVITY`（`opcode 1`）更新状态并在 `Clear` 时发送 `activity: null`；连接中途断开时会自动重连并重新握手一次。超过 `DiscordMaxTextLength`（128 字节）的文本按 UTF-8 字符边界安全截断。

## Steam 丰富状态

`NewSteam(SteamConfig{})` 在桌面目标上按需加载 Steamworks 共享库（macOS 为 `libsteam_api.dylib`，Linux 为 `libsteam_api.so`，Windows 为 `steam_api64.dll` / `steam_api.dll`），会依次检查可执行文件同目录、系统加载路径或 `SteamConfig.LibraryPath`，并按新旧顺序查找 `SteamAPI_SteamFriends_v018`、`v017`、`v015`、`SteamFriends` 以及 `SteamAPI_ISteamFriends_SetRichPresence` 和 `SteamAPI_ISteamFriends_ClearRichPresence`。

- 默认假定游戏已在别处初始化 Steamworks；若希望在 `ISteamFriends` 为空时由本包调用 `SteamAPI_Init`（并在 `Close` 时调用 `SteamAPI_Shutdown`），可设置 `SteamConfig.AutoInit: true`。
- 连续调用 `Set` 时，已移除的旧键会通过 `SetRichPresence(key, "")` 逐个删除而无需 `ClearRichPresence()`，未变化的键则跳过，避免好友列表闪烁。
- 键名与键数遵循 Steamworks 限制（`SteamMaxKeys = 30`、`SteamMaxKeyLength = 63` 字节，超限或非法键返回 `ErrSteamKeyInvalid` 或 `ErrSteamTooManyKeys`）；超过 `SteamMaxValueLength`（255 字节）的值按 UTF-8 字符边界截断。
- 在 Web、iOS、Android 上，或缺少 Steamworks 共享库 / Steam 客户端未运行时，`Set` 和 `Clear` 返回 `ErrUnavailable`。

## 优雅降级

如果 `Manager` 中至少有一个提供者成功，其余返回 `ErrUnavailable`（例如 Steam 正在运行而 Discord 未启动），`Update`、`SetLevel`、`Clear`、`Poll` 和 `Flush` 返回 `nil`。若全部提供者都返回 `ErrUnavailable`，则返回 `ErrUnavailable`。非 `ErrUnavailable` 的真实错误不会中断其他健康提供者的更新，并会合并后返回。需要观察每个提供者的错误（包括 `ErrUnavailable`）时，可设置 `ManagerConfig.OnError`。

## 测试

两个提供者都支持注入替身，无需安装 Discord 或 Steam 即可确定性测试：

- 通过 `DiscordConfig.Dial` 传入基于 `net.Pipe()` 的进程内模拟 IPC 服务端，验证原始小端帧、握手、`SET_ACTIVITY` 和 `Clear`。
- 通过 `SteamConfig.Driver` 传入模拟 `SteamDriver`，断言每一次 `SetRichPresence(key, value)` 和 `ClearRichPresence()` 调用。
- 通过 `ManagerConfig.Now` 注入可控时钟，无需等待即可测试 `MinInterval` 节流与合并。

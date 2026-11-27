# 平台支持

NSGo Game 面向六个主要构建目标（macOS、Windows、Linux、iOS、Android 和 Web），同时也与 NSGo 的四个实验性目标（鸿蒙、tvOS、watchOS 和小程序）共用同一套代码，但各平台的功能可用性与 CI 测试覆盖并不一致。下表每个单元格都对照 NSGo `v0.3.4` 与本仓库当前的构建标签和工作流逐项核验，并为每一项缺失附上了对应的上游 issue 链接。

- **支持**：在 NSGo `v0.3.4` 与 `nsgo-dev/game` 中已具备完整的平台支持。
- **部分支持 (N)**：可用，但存在[脚注](#脚注)中说明的平台限制或待补全环节。
- **不可用**：运行时返回不支持/不可用错误，或在编译期被排除；已链接追踪 issue。
- **不在范围内**：目标平台本身没有对应的原生能力，或该功能不属于此平台范畴。

## 功能矩阵

| 功能 | macOS | Windows | Linux | iOS | Android | Web | 鸿蒙 | tvOS | watchOS | 小程序 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 2D 渲染 | 支持 | 支持 | 支持 | 支持 | 支持 | 支持 | 部分支持 (1) | 支持 | 部分支持 (2) | 部分支持 (3) |
| 3D 渲染（GPU 视图） | 支持 | 支持 | 支持 | 支持 | 支持 | 支持 | 不可用（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)） | 不可用（[nsgo#1317](https://github.com/nsgo-dev/nsgo/issues/1317)） | 不在范围内（[nsgo#1318](https://github.com/nsgo-dev/nsgo/issues/1318)） | 不在范围内（[nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)） |
| 音频 | 支持 | 部分支持 (4) | 支持 | 支持 | 部分支持 (4) | 部分支持 (5) | 不可用（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)） | 不可用（[nsgo#1317](https://github.com/nsgo-dev/nsgo/issues/1317)） | 不可用（[nsgo#1318](https://github.com/nsgo-dev/nsgo/issues/1318)） | 不可用（[nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)） |
| 键盘与鼠标 | 支持 | 支持 | 支持 | 部分支持 (6) | 部分支持 (6) | 支持 | 部分支持 (6) | 部分支持 (7) | 不在范围内 | 不在范围内 |
| 游戏手柄 | 支持 | 支持 | 支持 | 支持 | 支持 | 支持 | 不可用（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)） | 支持 | 不在范围内（[nsgo#1318](https://github.com/nsgo-dev/nsgo/issues/1318)） | 不可用（[nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)） |
| 触摸 | 部分支持 (8) | 支持 (8) | 支持 (8) | 支持 | 支持 | 支持 | 支持 | 不在范围内 | 支持 | 支持 |
| 触觉反馈 | 不在范围内 | 不在范围内 | 不在范围内 | 支持 | 支持 | 部分支持 (9) | 不可用（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)） | 不在范围内 | 不可用（[nsgo#1318](https://github.com/nsgo-dev/nsgo/issues/1318)） | 不可用（[nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)） |
| 运动传感器 | 不在范围内 | 不在范围内 | 不在范围内 | 支持 | 支持 | 部分支持 (10) | 不可用（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)） | 不在范围内 | 支持 | 不可用（[nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)） |
| 本地存档 | 支持 | 支持 | 支持 | 支持 | 支持 | 部分支持 (11) | 部分支持 (12) | 支持 | 支持 | 部分支持 (11) |
| 系统云存档 | 部分支持 (13) | 不在范围内 | 不在范围内 | 支持 | 部分支持 (14) | 不在范围内 | 不可用（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)） | 支持 | 不可用（[nsgo#1318](https://github.com/nsgo-dev/nsgo/issues/1318)） | 不在范围内 |
| 自部署云存档 | 支持 | 支持 | 支持 | 支持 | 支持 | 支持 | 部分支持 (15) | 支持 | 支持 | 不可用（[nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)） |
| UDP | 支持 | 支持 | 支持 | 支持 | 支持 | 不在范围内 | 不可用（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)） | 支持 | 支持 | 不在范围内 |
| WebSocket | 支持 | 支持 | 支持 | 支持 | 支持 | 支持 (16) | 部分支持 (16) | 支持 | 支持 | 不可用（[nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)） |
| 游戏服务 | 支持 | 不在范围内 | 不在范围内 | 支持 | 部分支持 (17) | 不在范围内 | 不可用（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)） | 支持 | 不可用（[nsgo#1318](https://github.com/nsgo-dev/nsgo/issues/1318)） | 不在范围内 |
| 应用内购买 | 支持 | 不可用（[nsgo#1370](https://github.com/nsgo-dev/nsgo/issues/1370)） | 不可用（[nsgo#1370](https://github.com/nsgo-dev/nsgo/issues/1370)） | 支持 | 不可用（[nsgo#1369](https://github.com/nsgo-dev/nsgo/issues/1369)） | 不可用（[nsgo#1370](https://github.com/nsgo-dev/nsgo/issues/1370)） | 不可用（[nsgo#1370](https://github.com/nsgo-dev/nsgo/issues/1370)、[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)） | 支持 | 不可用（[nsgo#1370](https://github.com/nsgo-dev/nsgo/issues/1370)、[nsgo#1318](https://github.com/nsgo-dev/nsgo/issues/1318)） | 不可用（[nsgo#1370](https://github.com/nsgo-dev/nsgo/issues/1370)、[nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)） |
| 本地通知 | 支持 | 支持 | 部分支持 (18) | 支持 | 支持 | 部分支持 (18) | 不可用（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)） | 不在范围内 | 支持 | 不在范围内 |
| 远程推送 | 不可用（[nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)、[nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)） | 不可用（[nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)、[nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)） | 不可用（[nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)、[nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)） | 不可用（[nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)、[nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)） | 不可用（[nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)、[nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)） | 不可用（[nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)、[nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)） | 不可用（[nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)、[nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)） | 不可用（[nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)、[nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)） | 不可用（[nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)、[nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)） | 不可用（[nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59)、[nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323)） |
| Steam 集成 | 部分支持 (19) | 部分支持 (19) | 部分支持 (19) | 不在范围内 | 不在范围内 | 不在范围内 | 不在范围内 | 不在范围内 | 不在范围内 | 不在范围内 |
| 场景编辑器 | 支持 | 支持 | 支持 | 不在范围内 | 不在范围内 | 不在范围内 | 不在范围内 | 不在范围内 | 不在范围内 | 不在范围内 |
| CI 测试覆盖 | 支持 | 部分支持 (20) | 支持 | 部分支持 (21) | 部分支持 (21) | 部分支持 (22) | 不可用（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)） | 不可用（[nsgo#1317](https://github.com/nsgo-dev/nsgo/issues/1317)） | 不可用（[nsgo#1318](https://github.com/nsgo-dev/nsgo/issues/1318)） | 不可用（[nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)） |

## 脚注

1. **鸿蒙 2D 渲染**：`Canvas` 在 ArkUI `Canvas` 上回放绘制，但 `core.Shader` 尚无鸿蒙视图宿主（`docs/architecture/harmonyos.md`；追踪 issue：[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)）。
2. **watchOS 2D 渲染**：`Canvas` 在 SwiftUI 宿主中绘制，而由于 watchOS 没有 Metal 视图，`KindShader` 渲染为空盒子（`internal/platform/watchos/watchos.go`；[nsgo#1318](https://github.com/nsgo-dev/nsgo/issues/1318)）。
3. **小程序 2D 渲染**：`Canvas` 绘制到离屏画布（`canvas2d`）；微信支持像素图像视图，QQ、抖音和支付宝尚未支持像素图像视图绑定；`nsgo_noshader` 禁用了 `core.Shader`，且 2 MB 分包体积上限限制了打包代码和素材大小（`docs/architecture/miniprogram.md`；[nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)）。
4. **Windows 与 Android 音频**：两者均满足 `Env.CanPlayAudio() == true`（Windows 使用 XAudio2 + Media Foundation，Android 使用 SoundPool + MediaPlayer 且 SoundPool 单个解码样本上限 1 MiB），但 `Env.CanScheduleAudio()` 为 `false`（`windows/env.go` 与 `android/protocol.go` 未设置 `AudioSchedule`，因此不支持定时采样调度播放）。
5. **Web 音频**：支持 Web Audio（`AudioContext`）与 `HTMLAudioElement`，但浏览器会阻止音频输出直到首次受信任的用户手势（`docs/architecture/audio.md`；详见 [sound](sound.md)）。
6. **iOS、Android 与鸿蒙键盘鼠标**：外接物理键盘可派发 `EventKeyDown` / `EventKeyUp`，但 `Env.CanLockPointer()` 与 `Env.SupportsCustomCursor()` 均为 `false`（不支持桌面端指针锁定或自定义图片光标）。
7. **tvOS 键盘与鼠标**：Apple TV 通过 Siri Remote 进行焦点导航（`ios/keycommands.go` 在 `onTV()` 下跳过物理键盘命令），无鼠标指针和指针锁定（[nsgo#1317](https://github.com/nsgo-dev/nsgo/issues/1317)）。
8. **桌面端触摸**：macOS 无触控屏硬件，对监听 `OnTouch` 的视图将鼠标按下映射为 0 号手指（`macos/macos.go`）；Windows（`windows/touch.go` 通过 `WM_POINTER`）与 Linux（`linux/callback.go` 通过 GTK 触摸事件）在具备触控屏的设备上支持多点触控及指针输入。
9. **Web 触觉反馈**：`mobile.PlayHapticPattern` 在支持 Vibration API 的浏览器（主要是 Android 浏览器）上通过 `navigator.vibrate` 触发；`mobile.PlayHaptic` 为空操作，且 Safari/iOS 不支持 `navigator.vibrate`（`web/env.go`、`web/haptics.go`）。
10. **Web 运动传感器**：`WatchMotion` 仅在安全上下文（HTTPS）的移动端浏览器中读取 `DeviceMotionEvent` 与 `DeviceOrientationEvent`（Safari 需在用户手势内调用 `RequestPermission(PermissionMotion)`）；桌面浏览器及磁力计均返回不可用（`docs/architecture/location-motion.md`；[nsgo#1208](https://github.com/nsgo-dev/nsgo/issues/1208)）。
11. **Web 与小程序本地存档**：`save.StorageBackend(cx.Storage(), ...)` 持久化到浏览器 `localStorage` 或小程序宿主存储，但 `cx.DataDir()` 为空，`save.FileBackend` 不适用（`pkg/save/backend.go`）。
12. **鸿蒙本地存档**：`internal/platform/harmonyos/harmonyos.go` 未向 `engine.Start` 传入持久化 `Storage`，因此 `cx.Storage()` 退化为内存存储（`core.NewStore(nil)`），且 `js/wasm` 无法直接读写宿主 `DataDir` 路径（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)）。
13. **macOS 系统云存档**：Apple iCloud 容器需要携带 iCloud 权限的描述文件，NSGo 仅在 Mac App Store 构建（`sign.macos_profile`）中嵌入该描述文件；Developer ID 与 ad hoc 构建会省略 iCloud 并返回 `ErrCloudUnavailable`（`docs/architecture/cloud.md`）。
14. **Android 系统云存档**：Google Play Games Saved Games 通过 `CloudStorage.java` 反射调用，仅在应用或宿主打包了 Play Games Services SDK 且玩家已登录时可用，否则 `Env.CanUseCloudStorage()` 为 `false`，`liveops.PlatformCloud` 返回 `ErrCloudUnavailable`（`docs/architecture/cloud.md`、`android/protocol.go`）。
15. **鸿蒙自部署云存档**：`liveops.CloudClient` 在 ArkWeb 中通过浏览器的 `fetch` 运行 Go 的 `net/http`，需配置网络权限与 CORS `allowedOrigins`，且尚未在 CI 中测试（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)）。
16. **Web 与鸿蒙 WebSocket**：`pkg/online/netplay/ws_js.go` 封装浏览器全局 `WebSocket`，仅支持客户端 `DialWebSocket`（`ListenWebSocket` 仅限原生目标）。鸿蒙端在 ArkWeb 中运行，尚未在 CI 中测试（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)）。
17. **Android 游戏服务**：Go 侧线协议（`gameservices_android.go`）已就位，但 NSGo `v0.3.4` 预构建的 `NsgoView.java` 尚未置位 Play Games Services 能力位（`more & 64`），因此 `Env.CanUseGameServices()` 目前为 `false`，在 Java 反射挂钩落地前 `achievements` 与 `leaderboard` 会返回 `ErrUnsupported`（`docs/architecture/gameservices.md`）。
18. **Linux 与 Web 本地通知**：即时通知可用且 `Env.CanScheduleNotifications()` 为 `true`，但定时通知由 `driver.NotificationScheduler` 保存在内存中，仅在应用或页面保持打开期间触发（`linux/env.go`、`web/env.go`、`docs/architecture/services.md`）。
19. **Steam 集成**：`pkg/online/presence`（经 `purego` 调用 `ISteamFriends`）、`pkg/modding`（经 `purego` 调用 `SteamWorkshop`）和 `liveops.DirCloud`（Steam Auto-Cloud）可在 macOS、Windows 与 Linux 上编译运行，但在 Steam 客户端、Steam Linux Runtime、Steam Deck / gamescope、Steam 叠加层与 Steam Input 下的真机验证仍在 [nsgo#1228](https://github.com/nsgo-dev/nsgo/issues/1228) 中推进。
20. **Windows CI**：`.github/workflows/check.yml` 使用 `go tool nsgo check` 交叉编译并检查 `windows`，但 `.github/workflows/test.yml` 尚未在 Windows 运行器上执行 `go tool nsgo test ./...`（追踪 issue：[game#1305](https://github.com/nsgo-dev/game/issues/1305)）。
21. **iOS 与 Android CI**：`.github/workflows/check.yml` 使用 `go tool nsgo check` 编译并检查 `ios`、`ios_simulator` 与 `android`，但 iOS 模拟器与 Android 模拟器上的运行测试和示例冒烟测试尚未接入 CI（追踪 issue：[game#1306](https://github.com/nsgo-dev/game/issues/1306)）。
22. **Web CI**：`.github/workflows/check.yml` 使用 `go tool nsgo check` 交叉编译并检查 `web`，`.github/workflows/website.yml` 会构建 WebAssembly 演练场示例，但无头浏览器运行时测试尚未接入 `nsgo-dev/game` 的 CI。

## 各行的判定依据

审阅者可对照以下运行时检查、构建标签与工作流文件保持本矩阵与源码同步：

| 功能 | 运行时检查或构建标签 | 判定源码 |
| --- | --- | --- |
| 2D 渲染 | `core.Canvas`（`driver.ItemCanvas`）、`core.Pixels`、`core.Shader`、`nsgo_noshader` | NSGo `internal/platform/*/canvas*`、`internal/platform/watchos/watchos.go`、`docs/architecture/harmonyos.md`、`docs/architecture/miniprogram.md` |
| 3D 渲染（GPU 视图） | `core.Env.SupportsGPU()` | NSGo `core/env.go`（`SupportsGPU`）、`gpu/view.go`、`internal/platform/ios/env.go`、`docs/architecture/gpu.md` |
| 音频 | `core.Env.CanPlayAudio()`、`core.Env.CanScheduleAudio()` | NSGo `core/env.go`（`CanPlayAudio`、`CanScheduleAudio`）、`internal/platform/ios/env.go`、`internal/platform/harmonyos/env.go`、`internal/platform/miniprogram/env.go`、`docs/architecture/audio.md` |
| 键盘与鼠标 | `driver.EventKeyDown`、`core.Env.CanLockPointer()`、`core.Env.SupportsCustomCursor()` | NSGo `core/env.go`（`CanLockPointer`、`SupportsCustomCursor`）、`internal/platform/ios/keycommands.go`、`internal/platform/harmonyos/keyboard.go`；本仓库 [`pkg/input`](../../pkg/input/) |
| 游戏手柄 | `core.Env.SupportsGamepads()`、`nsgo/gamepad` 构建标签 | NSGo `core/env.go`（`SupportsGamepads`）、`gamepad/gamepad_apple.go`、`gamepad/gamepad_other.go`（`(ios && nsgowatch) \|\| (js && wasm && (nsgominiprogram \|\| nsgoharmony))`） |
| 触摸 | `driver.EventTouch`、`core.Env.Pointer()` | NSGo `core/env.go`（`Pointer`）、`internal/platform/macos/macos.go`、`internal/platform/windows/touch.go`、`internal/platform/linux/callback.go`、`internal/platform/ios/touch.go`、`docs/architecture/device-differences.md` |
| 触觉反馈 | `mobile.PlayHaptic`、`core.Env.CanPlayHapticPattern()` | NSGo `core/env.go`（`CanPlayHapticPattern`）、`internal/platform/ios/ios.go`、`internal/platform/android/protocol.go`、`internal/platform/web/haptics.go`、`docs/architecture/services.md` |
| 运动传感器 | `core.Scope.WatchMotion`、`core.PermissionMotion` | NSGo `internal/platform/apple/sensors.go`、`internal/platform/watchos/watchos.go`、`docs/architecture/location-motion.md` |
| 本地存档 | `core.Scope.Storage()`、`core.Scope.DataDir()` | 本仓库 [`pkg/save/backend.go`](../../pkg/save/backend.go)；NSGo `internal/platform/web/web.go`、`internal/platform/harmonyos/harmonyos.go`、`internal/platform/miniprogram/miniprogram.go` |
| 系统云存档 | `core.Env.CanUseCloudStorage()`、`nsgo/cloud`（`darwin`、`android`、`!darwin && !android`） | 本仓库 [`pkg/online/liveops/cloud_platform.go`](../../pkg/online/liveops/cloud_platform.go)（`CloudProviderFor`、`PlatformCloud`）；NSGo `core/env.go`（`CanUseCloudStorage`）、`cloud/cloud_apple.go`、`cloud/cloud_android.go`、`cloud/cloud_other.go`、`docs/architecture/cloud.md` |
| 自部署云存档 | `net/http` 客户端（`liveops.CloudClient`）；服务端 SQLite/Postgres `!js && !wasip1` | 本仓库 [`pkg/online/liveops/cloudclient.go`](../../pkg/online/liveops/cloudclient.go)、[`pkg/online/cloudsave/doc.go`](../../pkg/online/cloudsave/doc.go) |
| UDP | `//go:build !js` | 本仓库 [`pkg/online/netplay/udp_native.go`](../../pkg/online/netplay/udp_native.go)、[`pkg/online/netplay/udp_session_native.go`](../../pkg/online/netplay/udp_session_native.go) |
| WebSocket | `//go:build !js` 与 `//go:build js && wasm`（`js.Global().Get("WebSocket").Truthy()`） | 本仓库 [`pkg/online/netplay/ws_native.go`](../../pkg/online/netplay/ws_native.go)、[`pkg/online/netplay/ws_js.go`](../../pkg/online/netplay/ws_js.go) |
| 游戏服务 | `core.Env.CanUseGameServices()`、`nsgo/gameservices`（`darwin && !nsgowatch`、`android`、其他） | 本仓库 [`pkg/online/achievements/services.go`](../../pkg/online/achievements/services.go)、[`pkg/online/leaderboard`](../../pkg/online/leaderboard/)；NSGo `core/env.go`（`CanUseGameServices`）、`gameservices/gameservices.go`、`internal/platform/android/java/dev/oneo/nsgo/NsgoView.java`、`docs/architecture/gameservices.md` |
| 应用内购买 | `core.Env.CanPurchase()` | 本仓库 [`pkg/online/liveops/purchases.go`](../../pkg/online/liveops/purchases.go)（`Purchases.Available`）；NSGo `core/env.go`（`CanPurchase`）、`internal/platform/macos/macos.go`、`internal/platform/ios/env.go` |
| 本地通知 | `core.Scope.ShowNotification`、`core.Env.CanScheduleNotifications()` | NSGo `core/env.go`（`CanScheduleNotifications`）、`internal/platform/ios/env.go`、`internal/platform/watchos/watchos.go`、`docs/architecture/services.md` |
| 远程推送 | NSGo `v0.3.4` 尚无公开 API | NSGo `docs/architecture/remote-push.md` |
| Steam 集成 | `//go:build (darwin && !ios) \|\| (linux && !android) \|\| windows` | 本仓库 [`pkg/online/presence/steam_unix.go`](../../pkg/online/presence/steam_unix.go)、[`pkg/online/presence/steam_windows.go`](../../pkg/online/presence/steam_windows.go)、[`pkg/online/presence/steam_unsupported.go`](../../pkg/online/presence/steam_unsupported.go)、[`pkg/modding/workshop_steam.go`](../../pkg/modding/workshop_steam.go) |
| 场景编辑器 | `//go:build ios \|\| android \|\| js`（禁用桩） | 本仓库 [`pkg/editor/agent_runner_other.go`](../../pkg/editor/agent_runner_other.go)、[`pkg/editor/run_game_unsupported.go`](../../pkg/editor/run_game_unsupported.go)、[`pkg/editor/build_device_other.go`](../../pkg/editor/build_device_other.go) |
| CI 测试覆盖 | CI 工作流矩阵与 `nsgo check` 目标列表 | 本仓库 [`.github/workflows/test.yml`](../../.github/workflows/test.yml)、[`.github/workflows/check.yml`](../../.github/workflows/check.yml) |

## 实验性目标平台

鸿蒙、tvOS、watchOS 和小程序不在 NSGo Game 第一阶段范围内，本仓库的 CI 也不会为它们执行构建或 `nsgo check`。在 NSGo `v0.3.4` 中，这四个目标全部关闭了 `Env.SupportsGPU()` 与 `Env.CanPlayAudio()`，因此 3D 游戏只会显示空的回退视图，使用 `pkg/sound` 的游戏也将全程静音：

- **鸿蒙**：ArkWeb + ArkUI 预览目标；缺少 GPU 视图、音频、手柄、触觉反馈、运动传感器、持久化 `core.Storage`、原生通知、UDP 与游戏服务（[nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521)、[nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320)）。
- **tvOS**：与 iOS 共用后端，支持手柄、Game Center、StoreKit 和 iCloud，但 `ios/env.go` 在完成 tvOS 验证前禁用了音频与 GPU 视图（[nsgo#1317](https://github.com/nsgo-dev/nsgo/issues/1317)）。
- **watchOS**：SwiftUI 宿主，支持 2D `Canvas`、触摸、运动传感器、本地存档与本地通知，但无 Metal/GPU 视图、音频、手柄、触觉反馈、iCloud 与 Game Center（[nsgo#1318](https://github.com/nsgo-dev/nsgo/issues/1318)）。
- **小程序**（微信、QQ、支付宝、抖音）：运行在逻辑层 `js/wasm` 中，带 `nsgo_noshader` 标签并受 2 MB 分包体积限制；无 GPU 视图、音频、手柄、触觉反馈、运动传感器，也没有浏览器标准的 `fetch` 与 `WebSocket` 全局对象（[nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505)）。

## 上游能力缺口

矩阵中每一项“不可用”或受运行时阻塞的功能均对应以下追踪 issue：

| Issue | 范围 |
| --- | --- |
| [nsgo#1369](https://github.com/nsgo-dev/nsgo/issues/1369) | Android 应用内购买（Google Play Billing） |
| [nsgo#1370](https://github.com/nsgo-dev/nsgo/issues/1370) | Windows、Linux、Web 及其他目标的应用内购买 |
| [nsgo#59](https://github.com/nsgo-dev/nsgo/issues/59) | 远程推送通知（APNs 与 FCM） |
| [nsgo#1323](https://github.com/nsgo-dev/nsgo/issues/1323) | 远程推送待决设计问题与厂商适配器 |
| [nsgo#1521](https://github.com/nsgo-dev/nsgo/issues/1521) | 鸿蒙平台能力对齐 |
| [nsgo#1320](https://github.com/nsgo-dev/nsgo/issues/1320) | 鸿蒙预览目标仍缺失的能力 |
| [nsgo#1317](https://github.com/nsgo-dev/nsgo/issues/1317) | Apple TV（tvOS）仍缺失的能力（音频、GPU 视图） |
| [nsgo#1318](https://github.com/nsgo-dev/nsgo/issues/1318) | Apple Watch（watchOS）仍缺失的能力 |
| [nsgo#1505](https://github.com/nsgo-dev/nsgo/issues/1505) | 小程序大型应用与 2 MB 分包体积约束 |
| [nsgo#1208](https://github.com/nsgo-dev/nsgo/issues/1208) | Web 运动传感器（`WatchMotion`） |
| [nsgo#1228](https://github.com/nsgo-dev/nsgo/issues/1228) | Steam 客户端、Steam Linux Runtime、Steam Deck、叠加层与 Steam Input 验证 |
| [game#1305](https://github.com/nsgo-dev/game/issues/1305) | 在 Windows CI 运行器上运行 `nsgo-dev/game` 测试套件 |
| [game#1306](https://github.com/nsgo-dev/game/issues/1306) | 在 iOS 模拟器与 Android 模拟器 CI 上冒烟运行示例 |

## 不在支持范围内

- **游戏主机**（Nintendo Switch、PlayStation、Xbox）：主机 SDK 受保密协议（NDA）约束且需要 C 工具链，而 NSGo 与 NSGo Game 坚持纯 Go 且不含手写 C 代码。
- **VR 与 AR**：在 NSGo 提供 XR 运行时之前暂不在范围内（[#495](https://github.com/nsgo-dev/game/issues/495)；详见[VR 与 AR](index.md#vr-与-ar)）。
- **平台特性排除项**：桌面操作系统没有系统级云存档服务（可使用搭配 Steam Auto-Cloud 的 `liveops.DirCloud` 或基于 [cloudsave](cloudsave.md) 的 `liveops.CloudClient`）；桌面端没有移动端触觉反馈或 Core Motion 传感器 API；浏览器无法打开原生 UDP 套接字；Steamworks 仅限桌面端；可视化[场景编辑器](editor.md)为桌面端创作工具。

关于六个主要构建目标的打包、签名、公证、商店发布通道与 `nsgo check` 命令，详见[构建与发布](shipping.md)。

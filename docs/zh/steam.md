# 发布到 Steam

配置 Steamworks 应用与 Depot ID，通过 `nsgo-natives.yaml` 打包 Steamworks 运行库，使用 `go tool game release -channel steam` 构建桌面端 Depot，通过 SteamPipe 将构建上传到测试分支，将 NSGo 的存档目录映射到 Steam Auto-Cloud，并对照 Steam Deck 与 Steam 审核清单完成验收。

## Steamworks 配置

在 [Steamworks 合作伙伴门户](https://partner.steamgames.com/)创建应用并记下其 **App ID**。进入 **SteamPipe → Depots**，为你要发布的每个桌面操作系统各创建一个 Depot：

| 平台 | NSGo 目标 | Steamworks 常见操作系统设置 |
| --- | --- | --- |
| macOS（Apple Silicon + Intel） | `macos_universal` | macOS（64 位） |
| Windows（64 位） | `windows_x64` | Windows（64 位） |
| Linux / SteamOS（64 位） | `linux_x64` | Linux（64 位） |

Steamworks 会为每个 Depot 分配独立的 **Depot ID**（通常紧跟在 App ID 之后递增，例如 `AppID + 1`、`AppID + 2`、`AppID + 3`；请始终以门户中显示的实际 ID 为准）。

### 使用 `steam.app_id` 进行本地测试

开发期在 Steam 客户端之外直接启动游戏时，Steamworks 会在当前工作目录查找 `steam_appid.txt`。在 `nsgo.yaml` 中设置 `steam.app_id`：

```yaml
steam:
  app_id: 480 # 替换为你的 Steamworks App ID（480 是 Valve 的测试应用 Spacewar）
```

- `go tool nsgo dev` 与 `go tool game dev` 会把 `steam_appid.txt` 写入原生构建目录，并以该目录为工作目录启动应用，只要 Steam 客户端处于运行状态，`SteamAPI_Init` 就能成功初始化。
- `go tool game release -channel steam`（以及 `nsgo build -channel steam`）绝不会向发布 Depot 写入 `steam_appid.txt`，并会忽略 `resources/` 中的 `steam_appid.txt`。切勿提交或上传 `steam_appid.txt` 到 Steam Depot。

### 打包 Steamworks 可再分发库（`nsgo-natives.yaml`）

NSGo 游戏通过 `purego` / `syscall` 动态绑定 Steamworks，不需要 cgo。从 Steamworks 合作伙伴门户下载 Steamworks SDK，将三个桌面平台的动态库（`libsteam_api.dylib`、`steam_api64.dll`、`libsteam_api.so`）和 Valve SDK 许可证文件放入你项目内的一个 Go 包目录（例如 `internal/steamlib/`），并在 `internal/steamlib/nsgo-natives.yaml` 中声明：

```yaml
version: 2
libraries:
  macos_arm64:
    - name: libsteam_api.dylib
      path: lib/macos/libsteam_api.dylib
      sha256: "<64-hex-sha256-of-libsteam_api.dylib>"
      licenses: [licenses/STEAMWORKS_LICENSE.txt]
  macos_x64:
    - name: libsteam_api.dylib
      path: lib/macos/libsteam_api.dylib
      sha256: "<64-hex-sha256-of-libsteam_api.dylib>"
      licenses: [licenses/STEAMWORKS_LICENSE.txt]
  windows_x64:
    - name: steam_api64.dll
      path: lib/windows-x64/steam_api64.dll
      sha256: "<64-hex-sha256-of-steam_api64.dll>"
      licenses: [licenses/STEAMWORKS_LICENSE.txt]
  windows_arm64: []
  linux_x64:
    - name: libsteam_api.so
      path: lib/linux-x64/libsteam_api.so
      sha256: "<64-hex-sha256-of-libsteam_api.so>"
      licenses: [licenses/STEAMWORKS_LICENSE.txt]
  linux_arm64: []
  ios: []
  ios_simulator: []
  tvos: []
  tvos_simulator: []
```

- 用 `shasum -a 256 <文件>`（macOS/Linux）或 `Get-FileHash -Algorithm SHA256 <文件>`（Windows）计算每个文件的 SHA-256 摘要。
- Valve 提供的 `libsteam_api.dylib` 已是通用（Universal）macOS 二进制，因此在构建 `macos_universal` 时，`macos_arm64` 和 `macos_x64` 可以指向同一份文件。
- 在桌面端构建中导入该包（例如 `_ "example.com/mygame/internal/steamlib"`），NSGo 会在签名前把动态库放到正确位置：
  - **macOS**：`<Name>.app/Contents/Frameworks/libsteam_api.dylib`
  - **Windows**：与 `<Name>.exe` 同目录的 `steam_api64.dll`
  - **Linux**：与 `<Name>` 同目录的 `libsteam_api.so`
- Steamworks 可再分发库属于专有二进制文件，请不要提交到公开仓库，也不要放在公开 URL 上，而是在私有仓库或 CI 私有制品存储中获取。
- 目前，[`pkg/online/presence`](presence.md#steam-丰富状态) 会自动在 macOS 上探测 `<exeDir>/../Frameworks/libsteam_api.dylib`，在 Windows 和 Linux 上探测 `<exeDir>`；[`pkg/modding`](modding.md#steam-创意工坊workshop) 默认按库名或 `SteamWorkshopOptions.LibraryPath` 加载。用于统一管理 `SteamAPI_Init`、回调轮询、Steam 界面暂停事件和加入事件的共享 Steamworks 客户端包计划在 [#1763](https://github.com/nsgo-dev/game/issues/1763) 中实现。

## 构建 Depot

使用 `go tool game release` 一次性构建三个桌面端 Depot：

```sh
go tool game release -channel steam -target macos_universal,windows_x64,linux_x64
```

为什么需要显式传入 `-target macos_universal,windows_x64,linux_x64`：

- 如果 `game.yaml` 的 `export.presets` 包含移动端或 Web 目标，`game release` 在未指定 `-target` 时会沿用这些预设，而 `-channel steam` 会拒绝非桌面目标。
- 未配置 `export.presets` 时，`-channel steam` 默认展开为 `macos,windows,linux`，分别生成 `macos_arm64` 和 `macos_x64` 两个目录；传入 `macos_universal` 会把两种 macOS 架构合并为一个通用 `.app` 包，对应单个 macOS Steam Depot。
- `macos_universal` 必须在 macOS 主机上构建；在 Linux 或 Windows 主机上可只构建 `-target windows_x64,linux_x64`，再在 macOS 上构建 `macos_universal`。
- 如果项目仅通过 Steam 分发，也可以在 `nsgo.yaml` 中设置 `updates: { channel: steam }`，将 `-channel steam` 设为默认渠道。

`go tool game release` 会自动以 `GOFLAGS=-tags=release` 调用 `nsgo build`（裁剪 `pkg/tooling/gamedebug`、`pkg/tooling/hotreload`、`pkg/tooling/devstate` 以及物理与 AI 调试绘制），构建后校验桌面端二进制不含泄漏的调试符号，检查资源署名并在 Depot 旁写出 `CREDITS.md`（若要随每个 Depot 一起打包，请在构建前先运行 `go tool game credits -out resources/CREDITS.md`），并在 `nsgo.yaml` 的 `compliance.steam.content_survey` 仍为 `false` 时输出提醒。

每个 Depot 会输出到 `<output>/<name>-<version>-<target>/`，不含安装器、自更新器或 `steam_appid.txt`：

| Depot 目录 | 内容 |
| --- | --- |
| `dist/<name>-<version>-macos_universal/` | `<name>.app`（配置 `sign.identity` 与 `sign.notarize` 后会完成签名与公证；内含 `Contents/Resources/` 与 `Contents/Frameworks/`）。 |
| `dist/<name>-<version>-windows_x64/` | `<name>.exe`、`resources/` 中的运行时资源文件、`THIRD_PARTY_NOTICES.txt` 以及声明的 Windows DLL（如 `steam_api64.dll`）。 |
| `dist/<name>-<version>-linux_x64/` | `<name>`、`steam-launch.sh`、`nsgo-gtk/`（GTK 4 运行时与 `nsgo-gtk/licenses/`）、`<id>.desktop`、`resources/` 中的运行时资源文件、`THIRD_PARTY_NOTICES.txt` 以及声明的 Linux `.so` 库（如 `libsteam_api.so`）。 |

请为 macOS `.app` 完成签名与公证（`sign.identity`、`sign.notarize`，或在 CI 中配置 `NSGO_NOTARY_*`），并为 Windows `.exe` 及 DLL 完成 Authenticode 签名（`sign.certificate` + `NSGO_CERTIFICATE_PASSWORD` 或 `sign.sign_command`），避免玩家经 Steam 启动时遇到 Gatekeeper 或 SmartScreen 警告。签名配置详见[构建与发布](shipping.md)。

## 启动项与 Linux 运行时

在 Steamworks 的 **安装（Installation）→ 常规安装（General Installation）** 下为每个操作系统配置一条启动项：

| 操作系统 | 可执行文件 | 工作目录 | 说明 |
| --- | --- | --- | --- |
| macOS | `<Name>.app` | （留空 / Depot 根目录） | 来自 `macos_universal` 的通用 `.app` 应用包。 |
| Windows（64 位） | `<Name>.exe` | （留空 / Depot 根目录） | 来自 `windows_x64` 的可执行文件。 |
| Linux（64 位） | `steam-launch.sh` | （留空 / Depot 根目录） | `nsgo build -channel steam` 为 `linux_x64` 生成的启动脚本。 |
| Linux（arm64，可选） | `<Name>` | （留空 / Depot 根目录） | 需要宿主运行时自带 GTK 4（不含 `nsgo-gtk/`）。 |

在 **安装 → Linux 运行时（Linux Runtime）** 下选择 **Steam Linux Runtime 3.0 (sniper)**（或可用的 4.0）。Steam Linux Runtime 提供显示协议栈，但不包含 GTK 4.14+。针对 `linux_x64`，`nsgo build -channel steam` 会在 `steam-launch.sh` 旁附带 `nsgo-gtk/`：

- `steam-launch.sh` 会通过 `ldconfig` 检查系统是否存在 4.14 及以上版本的 `libgtk-4.so.1`。
- 仅当系统缺少 GTK 4.14+（如 Steam Linux Runtime 3.0/4.0 容器内）时，脚本才会把 `nsgo-gtk/lib` 前置到 `LD_LIBRARY_PATH`、把 `nsgo-gtk/share` 前置到 `XDG_DATA_DIRS`，随后带着原始启动参数 `exec` 游戏二进制。
- 请务必在 Linux Depot 中保留 `nsgo-gtk/` 目录，尤其是其中的 `nsgo-gtk/licenses/`（包含 `NOTICE`、`build.sh` 与上游许可证文本），以满足随包分发的 GLib、Pango 和 GTK 库的 LGPL 合规要求。

## 使用 SteamPipe 上传

`nsgo publish` 尚未支持直接上传 Steam（跟踪于 [nsgo-dev/nsgo#2691](https://github.com/nsgo-dev/nsgo/issues/2691)）。当前通过 Valve 的 `steamcmd` 与 SteamPipe VDF 构建脚本上传 Depot。

在项目根目录新建 `steam/` 文件夹，放入一份应用构建脚本和三份 Depot 构建脚本。请将示例中的 `1000` 替换为你的 App ID、`1001`–`1003` 替换为你的 Depot ID、将 `My Game-1.0.0` 替换为实际的 `<name>-<version>`（或者在发布脚本中把三个 Depot 目录复制到固定路径如 `dist/steam/macos`、`dist/steam/windows`、`dist/steam/linux`，这样版本递增时无需修改 VDF）：

```text
// steam/app_build.vdf
"AppBuild"
{
	"AppID" "1000"
	"Desc" "1.0.0 release candidate"
	"BuildOutput" "../build/steam-output"
	"ContentRoot" "../dist"
	"SetLive" "staging"
	"Depots"
	{
		"1001" "depot_macos.vdf"
		"1002" "depot_windows.vdf"
		"1003" "depot_linux.vdf"
	}
}
```

```text
// steam/depot_macos.vdf
"DepotBuild"
{
	"DepotID" "1001"
	"FileMapping"
	{
		"LocalPath" "My Game-1.0.0-macos_universal/*"
		"DepotPath" "."
		"recursive" "1"
	}
	"FileExclusion" "*.pdb"
	"FileExclusion" ".DS_Store"
	"FileExclusion" "steam_appid.txt"
}
```

```text
// steam/depot_windows.vdf
"DepotBuild"
{
	"DepotID" "1002"
	"FileMapping"
	{
		"LocalPath" "My Game-1.0.0-windows_x64/*"
		"DepotPath" "."
		"recursive" "1"
	}
	"FileExclusion" "*.pdb"
	"FileExclusion" "steam_appid.txt"
}
```

```text
// steam/depot_linux.vdf
"DepotBuild"
{
	"DepotID" "1003"
	"FileMapping"
	{
		"LocalPath" "My Game-1.0.0-linux_x64/*"
		"DepotPath" "."
		"recursive" "1"
	}
	"FileExclusion" "*.debug"
	"FileExclusion" "steam_appid.txt"
}
```

在项目根目录运行 `steamcmd`（若 `build/steam-output` 尚未创建请先创建）：

```sh
mkdir -p build/steam-output
steamcmd +login <builder_account> +run_app_build "$(pwd)/steam/app_build.vdf" +quit
```

### 分支管理与安全上线

- **切勿在 VDF 脚本或 CI 任务中把 `"SetLive"` 设为 `"default"`。** 始终先上传到非默认测试分支，例如 `staging`（可在 Steamworks 的 **SteamPipe → 生成版本（Builds）** 中为该分支设置密码）。
- 在 macOS、Windows 和 Linux / Steam Deck 的真实 Steam 客户端中切换到 `staging` 分支下载测试，确认冷启动、音频、手柄输入、Steam 界面叠加层与 Steam 云存档均正常。
- 验收通过后，在 Steamworks 合作伙伴门户的 **SteamPipe → 生成版本（Builds）** 中选中已验证的 BuildID，将其设为 `default` 分支的当前版本，点击 **预览更改 → 立即将生成版本上线**。
- 保留一个 `previous` 分支（或记录上一个线上 BuildID），一旦线上出现回退问题可立即回滚。

### 在 CI 中自动化上传

1. **创建专用构建账号**：在 Steamworks 的 **用户与权限（Users & Permissions）** 中新建独立 Steam 账号，仅授予该应用的 **编辑应用元数据** 与 **向 Steam 发布应用更改** 权限。切勿在 CI 中使用个人管理员账号。
2. **一次性缓存 Steam Guard 凭证**：先在受信任的机器上交互运行 `steamcmd +login <builder_account> +quit` 完成 Steam Guard 验证，将 `steamcmd` 缓存的 `config/config.vdf`（以及存在的 `ssfn*`）做 Base64 编码后存入 CI 密钥（如 `STEAM_CONFIG_VDF`），并保存 `STEAM_USERNAME`。
3. **只构建并上传到 `staging`**：在 macOS Runner 上构建 `macos_universal`（在 macOS 或 Linux 上构建 `windows_x64,linux_x64`），把三个 Depot 汇总到同一 `dist/` 目录，还原 `config.vdf` 后执行 `steamcmd +login "$STEAM_USERNAME" +run_app_build "$PWD/steam/app_build.vdf" +quit`。待 `staging` 冒烟验证完成后，再在 Steamworks 后台手动提升到 `default`。

## Steam 云存档

NSGo 的 `cx.DataDir()` 指向以 `nsgo.yaml` 中 `id` 命名的应用数据目录（例如 `com.example.mygame`）：

| 操作系统 | NSGo `cx.DataDir()` | Steamworks Auto-Cloud 根目录（Root） | `DirCloud` 子目录 | 匹配模式 |
| --- | --- | --- | --- | --- |
| Windows | `%APPDATA%\<id>` | `WinAppDataRoaming` | `<id>/cloud` | `*.json` |
| macOS | `~/Library/Application Support/<id>` | `MacAppSupport` | `<id>/cloud` | `*.json` |
| Linux / SteamOS | `$XDG_DATA_HOME/<id>`（默认 `~/.local/share/<id>`） | `LinuxXdgDataHome` | `<id>/cloud` | `*.json` |

在 Steamworks 的 **应用程序 → Steam 云（Steam Cloud）** 下配置 Auto-Cloud：

1. 启用 Steam Cloud，并设置每名用户的字节配额与文件数量上限（例如 100 MB 和 100 个文件）。
2. 在 **Auto-Cloud 配置** 中添加一条规则：根目录选择 `WinAppDataRoaming`，子目录填 `<id>/cloud`（例如 `com.example.mygame/cloud`），模式填 `*.json`，不勾选 **包含子目录**。
3. 在 **根目录覆盖（Root Overrides）** 中为 `WinAppDataRoaming` 添加覆盖映射：
   - **macOS**：根目录 `MacAppSupport`，添加/替换路径 `<id>/cloud`
   - **Linux + SteamOS**：根目录 `LinuxXdgDataHome`，添加/替换路径 `<id>/cloud`

在 Go 代码中将 [`liveops.NewDirCloud`](liveops.md#云存档) 指向 `filepath.Join(cx.DataDir(), "cloud")`，并与 `liveops.NewCloudSyncer` 配合使用：

```go
dirCloud, err := liveops.NewDirCloud(filepath.Join(cx.DataDir(), "cloud"))
if err != nil {
	return err
}
syncer, err := liveops.NewCloudSyncer(liveops.CloudSyncOptions{
	Local:  save.StorageBackend(cx.Storage(), "mygame"),
	Cloud:  dirCloud,
	State:  cx.Storage(),
	Device: deviceID,
	Policy: liveops.PolicyKeepBoth,
})
if err != nil {
	return err
}
```

为什么建议用 `DirCloud` 同步 `cx.DataDir()/cloud` 而不是同步整个 `cx.DataDir()`：

- **本机设置互不干扰**：保存在 `cx.Storage()`（或 `save.NewDefaultSettingsStore(syncer.Backend())`）里的分辨率、全屏开关、帧率上限与音量设置留在本地，不会因在 1280×800 的 Steam Deck 上游玩而覆盖台式机的显示设置。
- **冲突安全**：`DirCloud` 实现了 `ConditionalCloudSync`，采用临时文件原子写入与目录同步；`CloudSyncer` 能在多设备离线修改发生冲突时按策略处理，避免静默丢失进度（详见[在线运营](liveops.md#解决冲突539)）。
- **直接同步文件的备选方案**：不使用 `CloudSyncer` 的游戏也可将 Auto-Cloud 指向 `<id>`（配合 `save.FileBackend(cx.DataDir())` 与模式 `*.json`），并将本机设置保存在非 `.json` 键或 `cx.Storage()` 中以免被同步。
- **原生 Linux 与 Proton**：如果你发布了原生 `linux_x64` Depot，Steam Deck 会使用 `LinuxXdgDataHome` 覆盖路径；如果仅发布 `windows_x64` Depot 而玩家在 Steam Deck 上通过 Proton 运行，Steam 会自动把 `WinAppDataRoaming` 映射到游戏的 Proton 前缀目录（`compatdata/<AppID>/pfx/drive_c/users/steamuser/AppData/Roaming`）。

## Steamworks 功能

### 丰富状态本地化

[`pkg/online/presence`](presence.md#steam-丰富状态) 通过 `ISteamFriends::SetRichPresence` 写入 `status`、`level`、`mode`、`steam_player_group`、`steam_player_group_size`、`connect` 和 `steam_display`。只有当 `steam_display` 指向的令牌已在 Steamworks 的 **社区 → 丰富状态本地化（Rich Presence Localization）** 上传时，Steam 好友列表才会显示本地化文案。

编写与传给 `presence.NewSteam` 的 `DisplayToken`（如 `"#Status_InLevel"`）相匹配的本地化 VDF 并上传到 Steamworks：

```text
// steam/rich_presence_loc.vdf
"lang"
{
	"english"
	{
		"tokens"
		{
			"#Status_InMenu" "In Menus"
			"#Status_InLevel" "%mode%: %level%"
		}
	}
	"schinese"
	{
		"tokens"
		{
			"#Status_InMenu" "在菜单中"
			"#Status_InLevel" "%mode%：%level%"
		}
	}
}
```

`presence.NewSteam` 会自动将 `Activity.Mode` 填入 `%mode%`、将 `Activity.Level` 填入 `%level%`（详见 [presence](presence.md#activity)）。

### Steam 创意工坊

[`pkg/modding`](modding.md#steam-创意工坊workshop) 通过 `purego` 绑定 `ISteamUGC`（`modding.NewSteamWorkshop` 与 `modding.NewWorkshop`），支持以确定性的 `workshop.yaml` 发布模组，并自动将已订阅条目同步到 `modding.Manager`。

- 在 Steamworks 的 **应用程序 → 创意工坊 → 常规（Workshop → General）** 中启用创意工坊，设置可见性状态，并配置与 `workshop.yaml` / `PublishOptions.Tags` 一致的允许标签。
- 在 macOS 上通过 `nsgo-natives.yaml` 打包 `libsteam_api.dylib` 时，NSGo 会将其放入 `<Name>.app/Contents/Frameworks/libsteam_api.dylib`。在共享 Steamworks 客户端（[#1763](https://github.com/nsgo-dev/game/issues/1763)）统一库查找路径之前，请在 `modding.SteamWorkshopOptions.LibraryPath` 中传入该路径（基于 `os.Executable()` 推导）。

### 成就、统计与排行榜（规划中）

目前，[`pkg/online/achievements`](achievements.md) 和 [`pkg/online/leaderboard`](leaderboard.md) 通过 NSGo 的 `gameservices` 包向 Apple Game Center 和 Google Play 游戏服务上报。面向 Steam `ISteamUserStats` 的内置成就、统计与 Steam 排行榜适配**尚未实现**，由 [#1764](https://github.com/nsgo-dev/game/issues/1764) 跟踪（基于 [#1763](https://github.com/nsgo-dev/game/issues/1763)）。

目前在 Steam 上已经可用的能力：

- `achievements.Tracker` 可以在桌面端追踪并持久化本地解锁与进度到 `core.Storage`，让游戏内成就界面先跑起来。
- 需要跨平台排行榜的游戏目前可以部署 [`pkg/online/leaderboard/selfhost`](leaderboard-selfhost.md)。

## Steam Deck 验证清单

提交审核前，请对照 Valve 的四项 **Steam Deck Verified（通过验证）** 标准测试你的 `linux_x64` Depot（或 Proton 下的 `windows_x64`）。内置 Steam Deck 按键图标、自动呼出 Steam 屏幕键盘、Deck 下默认 `1280×800` 全屏以及从 `game.yaml` 导出 Steam Input 动作集规划在 [#1765](https://github.com/nsgo-dev/game/issues/1765)；下表标明了当前可直接满足的条目与等待该 issue 的项。

### 输入

- [ ] 所有菜单、暂停界面、对话框和游玩操作都能完全仅用手柄完成：通过 [`input.Actions`](input.md#游戏手柄)（`GamepadButton`、`BindGamepadAxis`、`WatchGamepads`）和 [`gameui`](gameui.md) 的空间焦点导航实现。
- [ ] 当前手柄断开时能够干净暂停或提醒玩家（内置手柄断开提示条跟踪于 [#1568](https://github.com/nsgo-dev/game/issues/1568)）。
- [ ] 使用手柄游玩时显示手柄按键提示，而不是键盘或鼠标图标（原生 Deck 图标检测与基于 `game.yaml` 的 Steam Input 动作集规划于 [#1765](https://github.com/nsgo-dev/game/issues/1765)）。
- [ ] 正常游玩流程不依赖无法用手柄完成的强制文字输入（通过 `ShowFloatingGamepadTextInput` 自动调起 Steam 屏幕键盘规划于 [#1765](https://github.com/nsgo-dev/game/issues/1765)）。

### 显示

- [ ] 游戏在 `1280×800`（Steam Deck 原生 16:10）与 `1280×720`（16:9）下完整渲染且无 HUD 裁切，使用 [`screen.Viewport`](screen.md)（`ModeFit` 或 `ModeExpand`）与 [`screen.Video`](screen.md#显示设置)（`Fullscreen: true`）。Deck 上自动默认 `1280×800` 全屏跟踪于 [#1765](https://github.com/nsgo-dev/game/issues/1765)。
- [ ] 在 7 英寸 1280×800 屏幕上所有界面文字、提示与字幕清晰易读（避免小于 9px 的极小字体）。
- [ ] 帧步调稳定且兼顾续航：接入 `screen.Video` 的 `FrameCap`（支持玩家调节或锁定 60/90 fps）与 `AutoDropLowPower`。

### 无缝体验

- [ ] 点击 **开始游戏** 直接进入游戏，无外部启动器、配置向导或必须用鼠标点击的弹窗。
- [ ] 单人模式可离线游玩，不强制打开浏览器登录或持续联网。
- [ ] Steam 云存档在台式机与 Steam Deck 之间同步进度，但不会覆盖各设备独立的分辨率或音频设置（用 `DirCloud` 同步 `cx.DataDir()/cloud`，设置留在 `cx.Storage()`）。

### 系统支持

- [ ] 使用 `-channel steam` 构建原生 `linux_x64` Depot，在 **Steam Linux Runtime 3.0 (sniper)** 下运行并保持 `steam-launch.sh` 与 `nsgo-gtk/` 完整；若只发 Windows 版，则在 Proton 下完整测试 `windows_x64` Depot。
- [ ] `resources/` 中所有资源路径严格区分大小写（macOS 和 Windows 开发机通常不区分大小写，但 Linux 文件系统区分大小写）；发布前先运行 `go tool game check`。

## 商店审核与上线流程

1. **内容调查**：完成 [Steamworks 内容调查](https://partner.steamgames.com/doc/gettingstarted/contentsurvey) 以提交分级与成人内容披露，并在 `nsgo.yaml` 中记录状态：
   ```yaml
   compliance:
     steam:
       content_survey: false # 在 Steamworks 提交后改为 true
   ```
   当该项为 `false` 时，`go tool game release -channel steam`（及 `nsgo build -channel steam`）会在构建时输出提醒。
2. **商店页面审核与“即将推出”**：准备好商店文案、宣传图、截图、预告片、系统配置与控制器支持标签并提交 Valve 审核（通常需要 3–5 个工作日）。Valve 要求商店页面以 **即将推出（Coming Soon）** 状态公开展示**至少两周**后才能正式发售。
3. **版本审核**：通过 SteamPipe 上传发布候选 Depot，完成 Depot、启动项、Linux 运行时与 Steam 云存档配置后提交版本审核（通常需要 3–5 个工作日；如游戏需要联网账号，请在审核说明中提供测试账号）。
4. **发售前最终核查**：
   - 确认所有 Depot 中都没有 `steam_appid.txt`。
   - 确认 `THIRD_PARTY_NOTICES.txt`、`CREDITS.md`（若打包进 `resources/`）以及 Linux x64 的 `nsgo-gtk/licenses/` 齐全。
   - 在每个目标操作系统的 Steam 客户端中全新安装 `staging` 分支，验证启动、存档、云同步以及丰富状态 / 创意工坊。
   - 在 Steamworks 点击 **发布应用（Release App）** 前，确认已通过审核的 BuildID 已上线到 `default` 分支。

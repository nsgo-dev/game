# modding

`game/modding` 基于 NSGo 的 `tsx` 运行时加载并运行按能力沙箱隔离的 TSX/JS 游戏模组，提供 `mod.yaml` 清单校验、依赖拓扑排序、故障隔离、持久化启停状态以及 `"game"` 模块 API。

```go
import "github.com/nsgo-dev/game/pkg/modding"
```

## 模组清单（`mod.yaml`）与权限能力

每个模组位于独立目录中，包含 `mod.yaml`（`ModManifest`）和入口脚本（`mod.tsx` 或 `mod.js`）。[`pkg/modding/testdata/mods/boss-rush/`](../../pkg/modding/testdata/mods/boss-rush/) 中的示例模组就是本页展示的清单和脚本，包内测试会加载它：

```yaml
manifestVersion: 1
id: com.example.boss-rush
name: Boss Rush
version: 1.2.0
author: Example Studio
description: Spawns a slime every five seconds and shows a HUD badge while the rush runs.
entry: mod.tsx
gameVersion: '>=0.2.0'
capabilities: [readState, spawnEntity, emitEvent, ui]
```

| 字段 | 必填 | 含义 |
| --- | --- | --- |
| `manifestVersion` | 是 | 文件的格式版本，目前为 `1`（`modding.ManifestVersion`） |
| `id` | 是 | 由字母、数字、`.`、`-` 和 `_` 组成的唯一 ID，例如 `com.example.boss-rush` |
| `name` | 是 | 便于阅读的名称 |
| `version` | 是 | 模组自身的语义化版本（`MAJOR.MINOR.PATCH[-prerelease]`） |
| `author`、`description` | 否 | 自由文本 |
| `entry` | 是 | 相对模组目录的入口脚本：`.tsx`、`.ts`、`.jsx`、`.js` 或 `.mjs` |
| `gameVersion` | 否 | 对宿主 `ManagerOptions.GameVersion` 的版本约束，例如 `'>=0.2.0'` |
| `capabilities` | 否 | 模组申请的权限能力名称 |
| `dependencies` | 否 | 模组 ID 到版本约束的映射，例如 `com.example.core-lib: ^1.0.0` |

版本约束支持 `*`、`=`、`>=`、`>`、`<=`、`<`、`^` 和 `~`，多个条件用空格或逗号连接。以 `>` 或 `*` 开头的值要加引号（`'>=0.2.0'`、`'*'`），因为 YAML 会把值开头的这两个字符分别当作折叠块和别名。

格式是严格的：`ParseManifest` 会拒绝未知或拼错的字段、重复的键、第二个 YAML 文档、未知或重复的权限能力名称，以及缺失、不受支持或没有写成普通整数 `1` 的 `manifestVersion`，统一返回 `ErrInvalidManifest`。`EncodeManifest` 以唯一的规范形式写出清单（字段按上表顺序，权限能力按下文顺序，依赖按 ID 的字节序排序），因此已经是这种形式的文件读入再写出不会变化。在 Go 代码中构造的 `ModManifest` 可以让 `ManifestVersion` 保持为 0：`Validate` 和 `ResolveOrder` 接受它，`EncodeManifest` 会写出 `manifestVersion: 1`。`Manager` 只读取 `mod.yaml`；仍然只有旧版 `mod.json` 的模组目录会被拒绝，并提示改用 `mod.yaml`。

权限由 `Capability` 位标志控制：`CapReadState`（`readState`）、`CapWriteState`（`writeState`）、`CapSpawnEntity`（`spawnEntity`）、`CapEmitEvent`（`emitEvent`）、`CapUI`（`ui`）、`CapAudio`（`audio`）和 `CapStorage`（`storage`）。超出授权范围的调用会返回 `*PermissionError`，不会导致宿主游戏崩溃。

NSGo 为每个沙箱提供 `"nsgo"` 模块的 `Storage`，使用沙箱自己的分区，且没有可以收回它的权限能力。因此没有 `CapStorage` 的模组得到的分区配额是 1 字节，即 NSGo 接受的最小值，它最多只能保存一条键和值合计 1 字节的条目（例如 `setItem("a", "")`），更大的 `Storage.setItem` 调用都会失败；读取仍能看到分区中已有的内容，例如该模组在拥有此能力时写入的数据。

## `"game"` TSX 模块

模组通过导入 `"game"`（`GameModule`）挂载固定步长回调、收发事件、生成预制体、读写受控状态、播放音效、访问隔离存储以及挂载 TSX 界面面板。可用 `modding.WriteGameAPIDTS("game.d.ts")` 为模组作者导出 TypeScript 类型声明。

```tsx
import { Text, View } from "nsgo";
import { onStep, onEvent, spawn, emit, getState, mountPanel, log } from "game";

let elapsed = 0;
let wave = 0;

onStep((dt) => {
  elapsed += dt;
  if (elapsed >= 5) {
    elapsed = 0;
    wave++;
    spawn("slime", 120, 80, { hp: 30 });
    emit("boss-rush:wave", { wave });
  }
});

onEvent("boss_defeated", (payload) => {
  log("Defeated:", payload?.name, "score:", getState("score"));
});

mountPanel("boss-timer", () => (
  <View>
    <Text>Boss Rush Active</Text>
  </View>
), "hud");
```

## 在 Go 中管理模组生命周期（`Manager`）

在 NSGo 作用域上创建 `Manager`，通过 `GameBridge` 将模组请求接入你的游戏引擎，从目录或 `io/fs.FS` 发现模组，并在 `game.Loop` 中步进：

```go
mgr := modding.NewManager(cx, modding.ManagerOptions{
	StorageKey:  "mods",
	GameVersion: "0.2.0",
	Bridge: modding.GameBridge{
		OnSpawn: func(req modding.SpawnRequest) (string, error) {
			return world.SpawnPrefab(req.Prefab, req.X, req.Y, req.Props)
		},
		OnPlaySound: func(req modding.SoundRequest) error {
			return audioMgr.Play(req.Name, req.Volume, req.Rate, req.Loop)
		},
	},
})
defer mgr.Close()

if _, err := mgr.Discover("mods"); err != nil {
	return err
}
_ = mgr.LoadAll() // 校验清单、按依赖拓扑顺序加载、隔离单个模组故障

// 在 game.Loop.Update 中：
mgr.Step(dt)

// 在 NSGo 视图树中渲染模组挂载的 HUD 面板：
hudPanels := mgr.PanelsView("hud")
_ = hudPanels
```

运行时可通过 `Enable`、`Disable`、`Install`、`InstallFS`、`Uninstall`、`Load`、`Unload`、`Reload`、`SetGrant`、`Emit`、`Call`、`List`、`Info` 和 `Logs` 管理单个模组。异常抛错的模组会被标记为 `StateFailed`，不会影响宿主游戏或其他正常模组。

`Reload` 会先校验更新后的 `mod.yaml`（语法、ID、游戏版本和依赖），再卸载任何内容，因此清单损坏时会返回错误，已加载的版本继续运行。

对已加载的模组调用 `SetGrant` 会立即生效：`"game"` 模块从下一次调用起按新能力检查，收回 `CapUI` 会移除该模组已挂载的面板；当授权改变了沙箱自身执行的 `CapAudio` 或 `CapStorage` 时，模组会被卸载并在按新能力构建的沙箱中重新加载，入口脚本会再次运行。`Close` 卸载所有模组，之后 `Info` 把它们报告为 `StateDiscovered`（或 `StateDisabled`）；模组卸载后，它保留的 `onEvent` 或 `onStep` 取消订阅函数不再有任何作用。

浏览器无法把模组与页面隔离：在 Web 上运行的模组无论声明了哪些能力，都能使用页面自身的 API（`XMLHttpRequest`、`WebSocket`、`localStorage`）。因此在 Web 上加载模组会以 `ErrWebUnisolated` 失败，除非设置了 `ManagerOptions.WebUnisolated`；只有对像自己的代码一样可信的模组才应这样做。其他平台会隔离模组并忽略该选项。

## Steam 创意工坊（`Workshop`）

`Workshop` 将 Steamworks `ISteamUGC` 与 `Manager` 衔接，使作者可以发布模组和自定义关卡，已订阅的玩家会自动下载、校验、安装、更新和卸载对应内容。

### 创意工坊元数据（`workshop.yaml`）

发布时会在 `mod.yaml` 同目录下写入确定性的 `workshop.yaml`（`WorkshopMetadata`），保存分配到的 `publishedFileId`，以便后续发布更新同一个创意工坊条目而不是重复创建：

```yaml
version: 1
appId: 480
publishedFileId: 123456789
title: Boss Rush
description: Spawns a slime every five seconds and shows a HUD badge while the rush runs.
tags: [bosses, gameplay]
visibility: public
preview: preview.png
```

| 字段 | 必填 | 含义 |
| --- | --- | --- |
| `version` | 是 | `workshop.yaml` 的格式版本，目前为 `1`（`modding.WorkshopMetadataVersion`） |
| `appId` | 是 | 拥有该创意工坊条目的 Steam 应用 ID |
| `publishedFileId` | 否 | Steam 创意工坊分配的 `PublishedFileID`（首次发布时自动写入） |
| `title`、`description` | 否 | 在创意工坊上覆盖 `mod.yaml` 的 `name` 和 `description` |
| `tags` | 否 | Steam 创意工坊标签（编码时按字节序排序；拒绝空标签或重复标签） |
| `visibility` | 否 | `public`、`friendsOnly`、`private` 或 `unlisted` 之一（默认为 `public`） |
| `preview` | 否 | 相对模组目录的预览图路径（以 `/` 分隔） |

与 `mod.yaml` 相同，`ParseWorkshopMetadata` 会拒绝未知字段、重复键、尾随 YAML 文档、非法可见性/标签/预览图路径，以及缺失或未写成普通整数 `1` 的 `version`，统一返回 `ErrInvalidWorkshopMetadata`。`EncodeWorkshopMetadata` 输出规范的 2 空格缩进 YAML，不包含时间戳或易变字段，读写往返逐字节一致。

### 发布与同步订阅内容

在 macOS、Linux 和 Windows 上使用 `NewSteamWorkshop` 通过 `purego` 动态绑定 `libsteam_api.dylib`、`libsteam_api.so` 或 `steam_api64.dll`（无需 CGo）。当 Steamworks 动态库缺失、Steam 客户端未运行，或在 Web、iOS、Android 等不支持平台上调用时，`NewSteamWorkshop` 会返回 `ErrWorkshopUnavailable`。在测试和离线工具中可使用 `NewMemoryWorkshop`，它在本地目录模拟多用户发布、查询、订阅、下载和更新流程。

```go
backend, err := modding.NewSteamWorkshop(modding.SteamWorkshopOptions{})
if err != nil {
	// Steam 未运行时可优雅降级（errors.Is(err, modding.ErrWorkshopUnavailable)）。
	return err
}
defer backend.Close()

ws := modding.NewWorkshop(backend, mgr, modding.WorkshopOptions{
	AppID:    480,
	AutoLoad: true,
})

// 作者端：先校验 mod.yaml，再创建或更新创意工坊条目。
res, err := ws.Publish(ctx, "mods/boss-rush", modding.PublishOptions{
	Tags:       []string{"gameplay", "bosses"},
	Visibility: modding.VisibilityPublic,
	Preview:    "preview.png",
	ChangeNote: "Initial release",
})
if errors.Is(err, modding.ErrWorkshopLegalAgreementRequired) {
	// 条目 ID 已写入 workshop.yaml；提示作者打开 res.LegalAgreementURL 接受协议。
	_ = res.LegalAgreementURL
}

// 玩家端：查询、订阅并将 Manager 与所有已订阅的创意工坊条目同步。
if _, err := ws.SyncSubscribed(ctx); err != nil {
	return err
}
```

- `Publish` 会在创建或更新任何创意工坊条目之前校验 `mod.yaml`、入口脚本、预览图路径和 `workshop.yaml`。如果 Steamworks 要求作者先接受 Steam 创意工坊订户协议，`Publish` 仍会将 `publishedFileId` 写入 `workshop.yaml`，并返回包含 `NeedsLegalAgreement: true` 和 `LegalAgreementURL: WorkshopLegalAgreementURL` 的 `PublishResult` 以及 `ErrWorkshopLegalAgreementRequired` 错误。
- `SyncSubscribed` 会卸载并移除已取消订阅的模组，下载缺失或有更新的条目，校验路径边界（通过 `WorkshopOptions.AllowedRoots` 和逃逸符号链接检查返回 `ErrWorkshopUnsafePath`），并按依赖拓扑顺序安装有效模组，同时隔离单个损坏条目的错误。`InstallSubscribed` 用于按需安装单个已订阅条目。

# modding

`game/modding` 基于 NSGo 的 `tsx` 运行时加载并运行按能力沙箱隔离的 TSX/JS 游戏模组，提供 `mod.yaml` 清单校验、依赖拓扑排序、故障隔离、持久化启停状态以及 `"game"` 模块 API。

```go
import "github.com/nsgo-dev/game/pkg/modding"
```

## 模组、插件与包（#1268）

引擎有三种扩展游戏的方式，它们不能互相替代：

| | 编辑器（Go）插件 | 脚本模组 | 库包 |
| --- | --- | --- | --- |
| 用什么写 | Go | TSX | YAML 与媒体文件 |
| 谁来写 | 游戏的开发者、第三方工具作者 | 玩家 | 任何人 |
| 何时加载 | 编译进编辑器程序 | 运行时加载，支持热重载（`Manager.Reload`） | 安装进项目 |
| 信任级别 | 完全信任 | 沙箱隔离，按能力授权 | 不含代码 |
| 平台 | 全部，包括 iOS 和 Web | 原生平台；Web 仅在设置 `ManagerOptions.WebUnisolated` 后可用 | 全部 |
| 扩展编辑器 | 可以 | 不能（目前） | 只能启用内置插件 |

编辑器插件是编辑器程序用 `Editor.RegisterPlugin` 注册、项目再在 `game.yaml` 的 `editor.plugins` 下按名字启用的 `editor.Plugin`；见[编辑器插件](editor.md#编辑器插件492)。脚本模组是一份 `mod.yaml` 清单加一个 TSX 入口脚本，由 `modding.Manager` 在沙箱中运行，能做什么仅限于清单申请、宿主授予的能力，本页详细说明。库包是 `library.Install` 写入项目并记录在 `game.yaml` 中的一组文件；见[资源与插件库](library.md)。包不得包含 Go 源码或模块文件（`.go`、`go.mod` 等都会被拒绝），所以插件包只能启用编辑器程序编译时已包含的插件，而不会携带它们的代码。

该选哪一种？

- 给编辑器加新的面板、检查器、视口工具或 3D 操纵杆，或让编辑器按某些组件类型检查场景：用编辑器插件，见[编辑器插件](editor.md#编辑器插件492)。
- 让玩家在游戏设定的限度内改动已发布的游戏：用脚本模组，见下文。
- 分享精灵图、场景、声音或起步模板：用库包，见[资源与插件库](library.md)。

Go 插件编译进程序而不是在运行时加载，是因为 Go 没有跨平台的运行时加载代码的手段——iOS 和 Web 都不能在运行时加载原生代码——所以插件随它所扩展的编辑器命令一起发布。

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
| `version` | 是 | 模组自身的语义化版本（`MAJOR.MINOR.PATCH[-prerelease][+build]`，可带可选前导 `v`） |
| `author`、`description` | 否 | 自由文本 |
| `entry` | 是 | 相对模组目录的入口脚本：`.tsx`、`.ts`、`.jsx`、`.js` 或 `.mjs` |
| `gameVersion` | 否 | 对宿主 `ManagerOptions.GameVersion` 的版本约束，例如 `'>=0.2.0'` |
| `capabilities` | 否 | 模组申请的权限能力名称 |
| `dependencies` | 否 | 模组 ID 到版本约束的映射，例如 `com.example.core-lib: ^1.0.0` |

版本约束支持 `*`、`=`、`>=`、`>`、`<=`、`<`、`^` 和 `~`（或直接写版本号，等价于 `=`），多个条件用空格或逗号连接。子句支持可选前导 `v` 和省略后缀的版本号（`MAJOR` 或 `MAJOR.MINOR`）：`^1.2` 等价于 `>=1.2.0 <2.0.0`，`^0.2` 和 `^0.0` 以下一个次版本为上界，`~1.2` 和 `1.2` 等价于 `>=1.2.0 <1.3.0`，`<=1.2` 等价于 `<1.3.0`，`>1.2` 等价于 `>=1.3.0`。所有比较按纯粹的 SemVer 2.0.0 优先级求值：忽略构建元数据，预发布版本（包括 Go 伪版本）只要落在区间边界内就会匹配（`>=1.0.0` 匹配 `1.1.0-beta`，不匹配 `1.0.0-beta`），而 `<2.0.0`（包括 `^1.0.0` 的上界）会匹配 `2.0.0-alpha`，如需排除可写成 `<2.0.0-0`。以 `>` 或 `*` 开头的值要加引号（`'>=0.2.0'`、`'*'`），因为 YAML 会把值开头的这两个字符分别当作折叠块和别名。

格式是严格的：`ParseManifest` 会拒绝未知或拼错的字段、重复的键、第二个 YAML 文档、未知或重复的权限能力名称，以及缺失、不受支持或没有写成普通整数 `1` 的 `manifestVersion`，统一返回 `ErrInvalidManifest`。`EncodeManifest` 以唯一的规范形式写出清单（字段按上表顺序，权限能力按下文顺序，依赖按 ID 的字节序排序），因此已经是这种形式的文件读入再写出不会变化。在 Go 代码中构造的 `ModManifest` 可以让 `ManifestVersion` 保持为 0：`Validate` 和 `ResolveOrder` 接受它，`EncodeManifest` 会写出 `manifestVersion: 1`。`Manager` 只读取 `mod.yaml`；仍然只有旧版 `mod.json` 的模组目录会被拒绝，并提示改用 `mod.yaml`。

权限由 `Capability` 位标志控制：`CapReadState`（`readState`）、`CapWriteState`（`writeState`）、`CapSpawnEntity`（`spawnEntity`）、`CapEmitEvent`（`emitEvent`）、`CapUI`（`ui`）、`CapAudio`（`audio`）和 `CapStorage`（`storage`）。超出授权范围的调用会返回 `*PermissionError`，不会导致宿主游戏崩溃。

`mod.yaml` 的 `capabilities` 同样接受带点号的宿主自定义能力名称（`<namespace>.<name>`，由点号分隔的小驼峰分段，例如 `inventory.read` 或 `editor.contextAction`）。内置能力名称解码到 `ModManifest.Capabilities`，带点号的宿主能力名称解码到 `ModManifest.HostCapabilities`，编码时排在内置能力之后并按字节序排序，保证读写往返一致。若模组申请了未在 `ManagerOptions.Capabilities` 中声明的宿主能力，只有该模组会在加载时以 `ErrUnknownCapability` 失败，其余模组仍会正常加载。

没有 `CapStorage` 的模组在启用 `tsx.Sandbox.DisableStorage` 的沙箱中运行，因此 `"nsgo"` 模块的 `Storage` 上的所有方法（`getItem`、`setItem`、`removeItem`、`clear`、`keys`）都会以 `tsx: Storage is disabled for this sandbox` 拒绝，同时 `"game"` 模块的 `storageGet`、`storageSet` 和 `storageRemove` 也会返回 `*PermissionError`。禁用存储不会删除该模组分区中已有的数据，再次授予 `CapStorage` 后仍可读取。

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

## 宿主注册的 TSX 模块与自定义能力

游戏和运行时插件可以在 `"game"` 之外暴露自定义类型化 TSX 模块，每个模块由 `ManagerOptions.Capabilities`（`HostCapability`）声明的宿主能力与 `ManagerOptions.Modules`（`ModModule`）门控。[`pkg/modding/testdata/mods/loot-tracker/`](../../pkg/modding/testdata/mods/loot-tracker/) 中的示例模组展示了申请 `inventory.read` 并导入宿主 `"mygame/inventory"` 模块的完整写法。

```go
mgr := modding.NewManager(cx, modding.ManagerOptions{
	Capabilities: []modding.HostCapability{
		{Name: "inventory.read", Doc: "Read items in the player's inventory"},
		{Name: "inventory.write", Doc: "Add or remove items in the player's inventory"},
	},
	Modules: []modding.ModModule{
		{
			Name:       "mygame/inventory",
			Capability: "inventory.read",
			Doc:        "Read-only inventory queries.",
			Funcs: map[string]any{
				"countItem": func(cx *core.Scope, item string) (int, error) {
					api, err := modding.Caller(cx)
					if err != nil {
						return 0, err
					}
					if err := api.RequireHost("inventory.read", "inventory.countItem"); err != nil {
						return 0, err
					}
					return inventory.Count(item), nil
				},
			},
		},
	},
})
```

- `NewManager` 会校验 `Capabilities` 和 `Modules`，拒绝重复的模块名、与 `"game"` 或 NSGo 保留 `"nsgo"` 前缀冲突的模块名、非法的能力名称，以及未声明所需能力的模块。如果在调用 `NewManager` 之前进程中可能已经启动过 TSX 运行时（例如多局会话或测试环境），可在 `init()` 中先调用 `modding.RegisterModule` 向 NSGo 的 TSX 注册表预注册模块签名；之后的每个 `NewManager` 都会独立绑定各自的闭包实现，不会在不同管理器间串扰状态。
- 每个模组的沙箱 `Modules` 白名单包含 `"game"` 以及所有该模组既在 `mod.yaml` 中申请、又获得授权的宿主模块（匹配 `Capability`，或在多权限模块中匹配 `Capabilities` 的任意一项）。因此未授权的 `import` 会在加载阶段立即以清晰错误失败，而不是等到首次调用才报错。
- 宿主模块函数以 `*core.Scope` 作为首个参数（异步函数紧跟在 `context.Context` 之后），通过 `modding.Caller(cx)` 获取调用方模组的 `*ModAPI`，并用 `api.RequireHost(capability, action)` 执行细粒度权限检查；拒绝时返回包装了 `ErrPermission` 与 `core.CodeAccessDenied` 的 `*PermissionError`。
- 可通过 `ManagerOptions.GrantHost` 在全局范围内收窄宿主能力（保持 `nil` 表示不收窄，传入空切片 `[]string{}` 表示拒绝全部宿主能力），并在运行时通过 `Manager.SetHostGrant(id, caps)` 针对单个模组动态收窄。对已加载模组调用 `SetHostGrant` 会从下一次宿主模块调用起立即生效；可通过 `Manager.HostCapabilities()` 读取宿主声明的能力文档、通过 `ModInfo.HostCapabilities` 读取模组当前实际获得的宿主能力。
- 使用 `modding.WriteModAPIDTS(dir, opts)` 可向目录同时写出 `game.d.ts` 与每个宿主模块对应的 `<module>.d.ts`（来自 `tsx.Declarations()`）；仅需 `"game"` 声明时仍可继续使用 `WriteGameAPIDTS`。

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

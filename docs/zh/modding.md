# modding

`game/modding` 基于 NSGo 的 `tsx` 运行时加载并运行按能力沙箱隔离的 TSX/JS 游戏模组，提供 `mod.yaml` 清单校验、依赖拓扑排序、故障隔离、持久化启停状态以及 `"game"` 模块 API。

```go
import "github.com/nsgo-dev/game/packages/game/modding"
```

## 模组清单（`mod.yaml`）与权限能力

每个模组位于独立目录中，包含 `mod.yaml`（`ModManifest`）和入口脚本（`mod.tsx` 或 `mod.js`）。[`packages/game/modding/testdata/mods/boss-rush/`](../../packages/game/modding/testdata/mods/boss-rush/) 中的示例模组就是本页展示的清单和脚本，包内测试会加载它：

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

版本约束支持 `*`、`=`、`>=`、`>`、`<=`、`<`、`^` 和 `~`，多个条件用空格或逗号连接。以 `>`、`<`、`*` 或 `=` 开头的值要加引号（`'>=0.2.0'`），因为这些字符在 YAML 中有特殊含义。

格式是严格的：`ParseManifest` 会拒绝未知或拼错的字段、重复的键、第二个 YAML 文档、未知或重复的权限能力名称，以及缺失或不受支持的 `manifestVersion`，统一返回 `ErrInvalidManifest`。`EncodeManifest` 以唯一的规范形式写回清单（字段按上表顺序，权限能力按下文顺序，依赖按 ID 排序），因此读入再写出的文件不会变化。`Manager` 只读取 `mod.yaml`；仍然只有旧版 `mod.json` 的模组目录会被拒绝，并提示改用 `mod.yaml`。

权限由 `Capability` 位标志控制：`CapReadState`（`readState`）、`CapWriteState`（`writeState`）、`CapSpawnEntity`（`spawnEntity`）、`CapEmitEvent`（`emitEvent`）、`CapUI`（`ui`）、`CapAudio`（`audio`）和 `CapStorage`（`storage`）。超出授权范围的调用会返回 `*PermissionError`，不会导致宿主游戏崩溃。

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

运行时可通过 `Enable`、`Disable`、`Load`、`Unload`、`Reload`、`SetGrant`、`Emit`、`Call`、`List`、`Info` 和 `Logs` 管理单个模组。异常抛错的模组会被标记为 `StateFailed`，不会影响宿主游戏或其他正常模组。

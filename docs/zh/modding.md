# modding

`game/modding` 基于 NSGo 的 `tsx` 运行时加载并运行按能力沙箱隔离的 TSX/JS 游戏模组，提供 `mod.json` 清单校验、依赖拓扑排序、故障隔离、持久化启停状态以及 `"game"` 模块 API。

```go
import "github.com/nsgo-dev/game/packages/game/modding"
```

## 模组清单（`mod.json`）与权限能力

每个模组位于独立目录中，包含 `mod.json`（`ModManifest`）和入口脚本（`mod.tsx` 或 `mod.js`）：

```json
{
  "id": "com.example.boss-rush",
  "name": "Boss Rush",
  "version": "1.2.0",
  "entry": "mod.tsx",
  "gameVersion": ">=0.2.0",
  "capabilities": ["readState", "spawnEntity", "emitEvent", "ui", "storage"],
  "dependencies": {
    "com.example.core-lib": "^1.0.0"
  }
}
```

权限由 `Capability` 位标志控制：`CapReadState`（`"readState"`）、`CapWriteState`（`"writeState"`）、`CapSpawnEntity`（`"spawnEntity"`）、`CapEmitEvent`（`"emitEvent"`）、`CapUI`（`"ui"`）、`CapAudio`（`"audio"`）和 `CapStorage`（`"storage"`）。超出授权范围的调用会返回 `*PermissionError`，不会导致宿主游戏崩溃。

## `"game"` TSX 模块

模组通过导入 `"game"`（`GameModule`）挂载固定步长回调、收发事件、生成预制体、读写受控状态、播放音效、访问隔离存储以及挂载 TSX 界面面板。可用 `modding.WriteGameAPIDTS("game.d.ts")` 为模组作者导出 TypeScript 类型声明。

```tsx
import { onStep, onEvent, spawn, getState, mountPanel, log } from "game";

let elapsed = 0;

onStep((dt) => {
  elapsed += dt;
  if (elapsed >= 5) {
    elapsed = 0;
    spawn("slime", 120, 80, { hp: 30 });
  }
});

onEvent("boss_defeated", (payload) => {
  log("Defeated:", payload?.name, "score:", getState("score"));
});

mountPanel("boss-timer", () => <text>Boss Rush Active</text>, "hud");
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

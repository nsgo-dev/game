# assets

`game/assets` 在后台 goroutine 异步加载精灵图集、音效、图片、原始字节与自定义游戏数据而不阻塞显示帧，按场景组织成带响应式进度的资源组，在场景结束时统一释放，并提供开箱即用的 `LoadingScreen` 组件。

```go
import "github.com/nsgo-dev/game/packages/game/assets"
```

`Manager` 与 `Group` 的方法均在引擎 goroutine 上调用；文件读取与解析在后台执行，并通过 `core.Scope.Post` 回到引擎 goroutine，确保动画与 `game.Loop` 不会卡顿。

## 创建 Manager 并定义资源组

在 `*core.Scope`（通常是游戏根组件的作用域）下创建 `Manager`。通过 `WithFS` 让 `"asset:"` URI 从 `embed.FS` 或 `os.DirFS` 读取，或通过 `WithReadAsset` 自定义读取方式：

```go
//go:embed assets/*
var assetFS embed.FS

mgr := assets.NewManager(cx, assets.WithFS(assetFS))

// 按场景注册命名资源组
level1 := mgr.Group("level1").
	AddAtlas("hero", "asset:hero.json", core.Src("asset:hero.png")).
	AddSound("jump", audio.Source{URI: "asset:sounds/jump.wav"}).
	AddImage("bg", core.Src("asset:bg.png")).
	AddCustom("map", "asset:maps/level1.tmj", parseMap, nil)
```

`*Group` 支持的链式添加方法：

| 方法 | 加载内容 |
| --- | --- |
| `AddAtlas(name, jsonURI, img)` | 后台读取并用 `sprite.LoadAtlas` 解析，若 `img` 为 URI 则同时预取图片。 |
| `AddSound(name, src)` | 通过 `audio.LoadSound` 解码短音效。 |
| `AddImage(name, src)` | 通过 `core.Scope.PrefetchImage` 预取 URI 图片源。 |
| `AddBytes(name, uri)` | 后台读取 `uri` 的原始字节。 |
| `AddCustom(name, uri, parse, release)` | 后台读取并调用 `parse` 解析；释放资源组或取消加载时调用 `release(val)`。 |
| `AddCustomLoader(name, uri, load, release)` | 与 `AddCustom` 相同，并向 `load` 传入可取消的 `context.Context`。 |

对于不需要分组的单次加载，`Manager` 也提供 `LoadAtlas`、`LoadSound`、`LoadBytes` 和 `LoadCustom`。

## 加载、响应式进度与读取资源

调用 `mgr.LoadGroup("level1", onDone)` 或 `level1.Load(onDone)` 开始异步加载。`Load` 是幂等的：已加载完成的组会立刻触发 `onDone(nil)`，正在加载中的组会把回调排入当前加载队列。

`*Group` 的状态方法均由 `core.Signal` 驱动，在 NSGo 视图中读取即可自动订阅变化：

- `g.Progress()`（`[0, 1]` 范围的 `float64`）
- `g.Loaded()` 与 `g.Total()`（`int`）
- `g.Loading()`、`g.Done()`（`bool`）与 `g.Err()`（`error`）

加载完成后，可从资源组或 `Manager` 按类型读取（`Manager` 会从最近注册且包含该名称的组中查找）：

```go
heroAtlas, ok := mgr.Atlas("hero")
jumpSound, ok := mgr.Sound("jump")
bgImage, ok := mgr.Image("bg")
rawLevel, ok := mgr.Get("map")
```

## 释放资源组

离开场景时调用 `mgr.ReleaseGroup("level1")`（或 `level1.Release()`、`mgr.ReleaseAll()`）：

```go
mgr.ReleaseGroup("level1")
```

释放操作会关闭组内所有已加载的 `*audio.Sound`，执行自定义 `release` 回调，丢弃图集、图片与字节引用供 GC 回收，取消尚未完成的加载，并将进度重置为 `0` 以便日后重新加载。当 `Manager` 所属的 `cx` 作用域销毁时，所有资源组也会自动释放，延迟返回的结果会被安全丢弃而不会泄漏。

> 将解码后的图集图片固定在 NSGo 的 64 源画布缓存之外由 [nsgo#1127](https://github.com/nsgo-dev/nsgo/issues/1127) 跟踪。

## LoadingScreen

将 `assets.LoadingScreen`（或 `assets.LoadingView(cx, props)`）直接放入组件树，即可在挂载时启动加载、展示响应式进度条并在就绪后跳转：

```go
return assets.LoadingScreen{
	Manager: mgr,
	Group:   "level1",
	Title:   "正在加载洞窟",
	OnReady: func() {
		stage.Set("play")
	},
	OnError: func(err error) {
		loadErr.Set(err)
	},
}
```

设置 `LoadingScreen.View`（或 `LoadingScreenProps.Render`）为 `func(progress float64, err error) core.Node` 可完全替换默认进度条界面。

## 保存时重新加载

用 [`go tool game dev`](cli.md#dev) 运行游戏，并在它指定的资源目录上启动监视器，Manager 已加载的每个图集、图片和原始字节资源在文件保存后一秒内就会在运行中的游戏里更新：

```go
mgr := assets.NewManager(cx, assets.WithFS(os.DirFS("resources")))
w := hotreload.NewDevWatcher(cx) // 不在 game dev 下运行或在发布构建中时为 nil
w.WatchAssets(mgr, func(path string, names []string, err error) {
	if err != nil {
		log.Printf("reload %s: %v", path, err)
	}
})
```

`WatchAssets` 把目录下每个改动的文件以 `"asset:<path>"` 的形式在引擎 goroutine 上交给 `Manager.Reload`。JSON 改动的图集会重新解析，图片改动的图集以相同的帧绘制文件的新字节；资源组持有的 `*sprite.Atlas` 会原地更新，因此保留了该指针的代码也会画出改动。无法解析的 JSON 会保留之前的帧并报告错误。图片显示新的字节，原始字节被替换；音效和自定义资源只在其资源组重新加载时才会重新加载。`Manager.Revision` 统计替换了资源的重新加载次数，在 `Draw` 中读取它的 `core.Canvas` 会在每次重新加载后重绘；由 `game.Loop` 每帧绘制的游戏无需额外处理。

对于游戏自行加载的图集，例如用 `go:embed` 嵌入的图集，`w.WatchAtlasFiles("sprites.json", "sprites.png", img, onReload)` 会交付从保存后的文件解析出的图集。游戏不在 `game dev` 下运行时 `NewDevWatcher` 返回 nil，而 nil 监视器的方法什么也不做，因此这些调用可以一直留在游戏里；在发布构建（`-tags release`）中它们会被编译为空操作。

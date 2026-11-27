# assets

`game/assets` 在后台 goroutine 异步加载精灵图集、音效、图片、原始字节与自定义游戏数据而不阻塞显示帧，按场景组织成带响应式进度的资源组，在场景结束时统一释放，并提供开箱即用的 `LoadingScreen` 组件。

```go
import "github.com/nsgo-dev/game/pkg/assets"
```

`Manager` 与 `Group` 的方法均在引擎 goroutine 上调用；文件读取与解析在后台执行，并通过 `core.Scope.Post` 回到引擎 goroutine，确保动画与 `game.Loop` 不会卡顿。

## 创建 Manager 并定义资源组

在 `*core.Scope`（通常是游戏根组件的作用域）下创建 `Manager`。不传任何选项时，`"asset:"` URI 会通过 `core.Scope.ReadAsset` 读取应用打包的 `resources/` 目录，与图片（`core.Src("asset:...")`）和声音（`audio.Source{URI: "asset:..."}`）在所有目标平台上遵循同一规则（在 Web 上按需获取，无需嵌入二进制）：

```go
mgr := assets.NewManager(cx)

// 按场景注册命名资源组（全部从 resources/ 加载）
level1 := mgr.Group("level1").
	AddAtlas("hero", "asset:hero.json", core.Src("asset:hero.png")).
	AddSound("jump", audio.Source{URI: "asset:sounds/jump.wav"}).
	AddImage("bg", core.Src("asset:bg.png")).
	AddCustom("map", "asset:maps/level1.tmj", parseMap, nil)
```

需要覆写 `"asset:"` 字节读取来源时，可传入 `assets.WithFS(fsys)` 使用 `embed.FS` 或 `os.DirFS`（例如嵌入二进制的少量首屏资源，或在 `game dev` 下使用 `os.DirFS(hotreload.DevAssetsDir())`），或传入 `assets.WithReadAsset(fn)` 接入自定义存储。

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

用 [`nsgo game dev`](cli.md#dev) 运行游戏，并在它指定的资源目录上启动监视器，Manager 已加载的每个图集、图片和原始字节资源在文件保存后一秒内就会在运行中的游戏里更新：

```go
var opts []assets.Option
if dir := hotreload.DevAssetsDir(); dir != "" {
	// Under game dev, read "asset:" URIs from the directory the watcher watches.
	opts = append(opts, assets.WithFS(os.DirFS(dir)))
}
mgr := assets.NewManager(cx, opts...)
w := hotreload.NewDevWatcher(cx) // 不在 game dev 下运行或在发布构建中时为 nil
w.WatchAssets(mgr, func(path string, names []string, err error) {
	if err != nil {
		log.Printf("reload %s: %v", path, err)
	}
})
```

监视器和 Manager 必须看到同一批文件：没有 `WithFS` 的 `Manager` 会通过 `core.Scope.ReadAsset` 读取应用打包的 `resources/` 中的 `"asset:"` URI，所以在 `game dev` 下要像上面那样传入 `assets.WithFS(os.DirFS(hotreload.DevAssetsDir()))`，让 Manager 读取与监视器相同的目录（这样 `ReloadAsset` 和 `ReloadGroup` 也能同步重读修改后的文件）。`WatchAssets` 把目录下每个改动的文件以 `"asset:<path>"` 的形式在引擎 goroutine 上交给 `Manager.Reload`。JSON 改动的图集会重新解析，图片改动的图集以相同的帧绘制文件的新字节；资源组持有的 `*sprite.Atlas` 会原地更新，因此保留了该指针的代码也会画出改动。无法解析的 JSON 会保留之前的帧并报告错误。图片显示新的字节，原始字节被替换；音效和自定义资源只在其资源组重新加载时才会重新加载。有多种像素密度（`Scales`）的图片从屏幕实际显示的那个文件重新加载，例如 2x 屏幕上的 `hero@2x.png`；保存其他密度的文件不会有变化，直到屏幕显示该密度。用 `SetFS` 挂载了 DLC 包的资源组会保留包里已有文件的副本，保存基础文件不会改动它。`Manager.Revision` 统计替换了资源的重新加载次数，在 `Draw` 中读取它的 `core.Canvas` 会在每次重新加载后重绘；由 `game.Loop` 每帧绘制的游戏无需额外处理。

对于游戏自行加载的图集，例如用 `go:embed` 嵌入的图集，`w.WatchAtlasFiles("sprites.json", "sprites.png", img, onReload)` 会交付从保存后的文件解析出的图集。对于 WGSL/Kage 着色器和 3D 着色器材质，可使用 `w.WatchShaders` 和 `w.WatchMaterials` 挂载 `hotreload.ShaderReloader` 与 `hotreload.MaterialReloader`，见[着色器与材质热重载](render3d.md#着色器与材质热重载)。游戏不在 `game dev` 下运行时 `NewDevWatcher` 返回 nil，而 nil 监视器的方法什么也不做，因此这些调用可以一直留在游戏里；在发布构建（`-tags release`）中它们会被编译为空操作。

## 可下载资源包（DLC）

`assets.NewDLCManager(assets.DLCOptions{...})` 下载由 `PackSpec` 描述的 ZIP 资源包（通常用 `ParsePackManifest` 从清单解析），用 HTTP Range 请求续传中断的下载，校验大小、SHA-256 以及 Ed25519 签名，并缓存在 `PackCache` 中。`Fetch` 以 `fs.FS` 返回校验通过的资源包；`FetchGroup` 还会把它挂载到 `Group` 并完成加载。

```go
dlc := assets.NewDLCManager(assets.DLCOptions{
	AppBuild:  42,
	PublicKey: packPublicKey,
	Cache:     assets.NewDirPackCache(cacheDir),
	Limits:    assets.PackLimits{MaxTotalBytes: 256 << 20},
})
group, err := dlc.FetchGroup(ctx, mgr, spec, registerAssets, onProgress)
```

- 未设置 `PublicKey` 时，所有资源包都会以 `ErrInvalidSignature` 被拒绝，否则提供 `PackSpec`（URL、大小和哈希）的一方就能决定挂载什么。开发期间，或 `PackSpec` 随应用一起发布时，可设置 `AllowUnsigned` 接受未签名的资源包。
- 已缓存的资源包会在挂载所用的同一个读取器上再次校验。`DirPackCache` 通过一个保持打开的文件读取已挂载的资源包，因此之后替换或删除缓存文件不会改变挂载内容（在 Windows 上，资源包释放前删除会失败）。
- `Fetch` 返回的 `fs.FS` 同时也是 `io.Closer`。不再使用资源包时应关闭它；从未关闭的会在被垃圾回收时释放。`FetchGroup` 把资源包交给资源组，资源组在 `Group.Reset`、`Group.SetFS`、下一次向同一资源组 `FetchGroup`，或资源管理器的作用域被销毁时释放它。对仍在挂载中的资源包调用 `Remove`，会立即删除其未完成的下载，并在最后一个挂载释放时删除已缓存的资源包，因此不会从已加载的资源组下方抽走数据；在此之前再次 `Fetch` 该资源包会保留它。
- `Limits` 限制条目数量、单个文件大小、解压后的总大小以及压缩包本身的大小（零值字段使用 `DefaultPackLimits()`：32768 个条目、256 MiB、1 GiB 和 1088 MiB）。超出限制的资源包在挂载前就会被拒绝。下载一旦将超过 `MaxPackBytes` 就会以 `ErrPackTooLarge` 停止，这也限制了没有 `Size` 的 `PackSpec` 的 `.part` 文件。已挂载的资源包在解压时逐步读取每个文件，缓冲区随实际到达的字节增长，而不是按头部声明的大小预先分配，超过 `MaxFileBytes` 时以 `ErrPackTooLarge` 失败。
- 不可能通过校验的 `PackSpec`（格式错误的 `sha256` 或 `signature`，或负的 `size`）会在接触缓存之前就被拒绝，因此不会删除完好的缓存资源包。只有当缓存字节与 `PackSpec` 的大小或哈希不符时才会删除缓存；字节相符但签名或内容被拒绝的资源包会保留，因为重新下载得到的仍是相同的字节。
- 续传时收到不带 `Content-Range` 头的 `206 Partial Content`，会从第一个字节重新下载，而不是追加一段无法核对范围的响应体。
- `ParsePackManifest` 会拒绝它未定义的字段（例如拼错的 `sha265`）、同一对象中重复出现的键，以及清单之后的多余内容，并给出行号与修复方法。

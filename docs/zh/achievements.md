# 成就

```go
import "github.com/nsgo-dev/game/pkg/online/achievements"
```

`achievements` 用一个带版本号的 YAML 文件一次性声明游戏的成就，跟踪玩家在每个成就上的进度，并通过 NSGo 的 [`gameservices`](https://nsgo.dev/zh/docs/services/) 包上报：macOS、iOS 和 tvOS 上是 Game Center，Android 上是 Google Play 游戏服务（Play Games）。它只依赖 Go 标准库、NSGo 的公开 API 和本模块已经依赖的 YAML 库。

## 成就文件

每个成就都有一个游戏内通用的 `id` 供游戏代码使用，以及它在各平台服务上的 ID。Game Center 的 ID 由开发者在 App Store Connect 中自行指定，Play Games 的 ID 由 Play 管理中心生成，所以两者都要明确写出；某个服务没有 ID 的成就不会上报到该服务。

```yaml
version: 1
achievements:
  - id: first-match
    title: First Match
    description: Clear your first line of gems.
    platforms:
      gameCenter: com.example.mygame.first_match
      playGames: CgkIxxxxxxxxxxEAIQAQ
  - id: gem-collector
    title: Gem Collector
    description: Clear 300 gems.
    steps: 300
    platforms:
      gameCenter: com.example.mygame.gem_collector
      playGames: CgkIxxxxxxxxxxEAIQAg
  - id: high-roller
    title: High Roller
    hidden: true
    platforms:
      gameCenter: com.example.mygame.high_roller
```

| 字段 | 含义 |
| --- | --- |
| `version` | 结构版本，`1` |
| `id` | 1 到 128 个 ASCII 字母、数字、`.`、`_` 或 `-`，在文件内唯一 |
| `title`、`description` | 可选，供游戏自己的界面使用；平台自带的成就界面显示的是各自后台中填写的内容 |
| `hidden` | 成就解锁前，游戏自己的界面不应展示它 |
| `steps` | 完成成就所需的步数，1 到 `MaxSteps`；省略表示 1，即一次性成就 |
| `platforms.gameCenter`、`platforms.playGames` | 成就在各服务上的 ID：不含空白或控制字符，最多 255 字节，同一服务上不能被两个成就共用 |

`Parse` 拒绝未知字段、非 1 的版本、多余的 YAML 文档、重复 ID 和格式错误的平台 ID。`Encode` 按上表顺序输出确定性的 2 空格缩进 YAML，`Encode` 写出的文件解析后再编码会得到完全相同的字节，因此可以用测试保证文件始终是规范形式：

```go
//go:embed achievements.yaml
var achievementsYAML []byte

set, err := achievements.Parse(achievementsYAML)
out, err := achievements.Encode(set) // 文件为规范形式时与 achievementsYAML 相同
```

## 跟踪进度

`Tracker` 保存玩家在一个 `Set` 中每个成就上已完成的步数。进度只会前进。

- `Unlock(id)` 完成一个成就；对已完成的成就调用不做任何事。
- `Add(id, n)` 增加步数，并在达到成就的步数时封顶；`SetSteps(id, n)` 把进度提高到 `n`，低于当前值时忽略。
- `Steps`、`Percent`、`Unlocked` 和 `Lookup` 读取状态；`Definitions` 按声明顺序列出成就。
- `OnUnlock(fn)` 在成就通过 `Unlock`、`Add` 或 `SetSteps` 完成时调用一次，恢复进度时不会调用。
- 集合中未声明的 ID 会返回包装了 `ErrUnknownAchievement` 的错误。

```go
tracker, err := achievements.NewTracker(set)
tracker.OnUnlock(func(d achievements.Definition) {
	status.Set("Achievement unlocked: " + d.Title)
})

tracker.Unlock("first-match")
tracker.Add("gem-collector", cleared)
```

`Tracker` 的方法可以并发调用，回调执行时不持有它的锁，因此回调里可以再调用它。

## 保存进度

`Persist(store, key)` 读取 `key` 下已保存的进度（如果有），之后每次变化都写回。`core.Storage` 就是一个 `Store`，任何带 `GetItem` 和 `SetItem` 方法的类型也都可以。已保存的进度无法解析时，`Persist` 返回错误且不做任何改动，由游戏决定是否丢弃：

```go
store := cx.Storage()
if err := tracker.Persist(store, "mygame.achievements"); err != nil {
	store.RemoveItem("mygame.achievements")
	err = tracker.Persist(store, "mygame.achievements")
}
```

保存的进度是一个带版本号、按 ID 排序的 YAML 文档，记录已完成的步数，以及每个服务最近确认过的步数：

```yaml
version: 1
achievements:
  - id: first-match
    steps: 1
    reported:
      gameCenter: 1
  - id: gem-collector
    steps: 42
```

`Progress` 和 `Restore` 在内存中取出和替换同样的状态，`ParseProgress` 和 `EncodeProgress` 按与成就文件相同的规则读写它。恢复时会丢弃集合中已不存在的成就记录，并把步数限制在成就的步数以内。保存失败时进度仍留在内存中，并返回包装了 `ErrSave` 的错误；下一次成功写入时会一并保存。

## 上报到 Game Center 和 Play Games

`SignIn(cx, tracker, done)` 用 `ServiceFor` 选出当前平台的服务，通过 `gameservices.Authenticate` 让玩家登录，登录成功后把 `Tracker` 连接到该服务，并上报所有该服务尚未确认的进度。`done` 在引擎 goroutine 上收到玩家或错误：

- 平台没有游戏服务（Windows、Linux、Web、watchOS）或 `cx.Env().CanUseGameServices()` 为 false 时，返回 `gameservices.ErrUnsupported`。
- 服务应答时没有已登录的玩家，返回 `gameservices.ErrUnauthenticated`；玩家关闭登录界面，返回 `gameservices.ErrCancelled`。

登录失败时 `Tracker` 会继续计数，之后再次调用 `SignIn` 会上报期间产生的进度。服务的应答经由 `cx` 送达，因此释放 `cx`，或让它的当前所有者重新运行（视图正文每次渲染都会如此），都会断开 `Tracker`；请在事件处理函数或 effect 中登录，或用与游戏同寿命的作用域（例如根作用域）登录，也可以在下一个界面中再次调用 `SignIn`。`gameservices.ShowAchievements(cx)` 打开平台自带的成就界面。

```go
signedIn := core.NewSignal(cx, false)
achievements.SignIn(cx, tracker, func(_ gameservices.Player, err error) {
	signedIn.Set(err == nil)
})

core.Show{
	When: signedIn.Get,
	Then: func(*core.Scope) core.Node {
		return ui.Button{Text: "Achievements", OnPress: func() { gameservices.ShowAchievements(cx) }}
	},
}
```

`Tracker` 按服务记住该服务最近确认的步数，因此离线时或登录前产生的进度会在连接服务后上报，服务已有的进度不会重复发送。某个成就的上报还在等待应答时，之后的进度会暂存，等应答到达后合并成一次上报，所以连续调用 `Add` 不会刷屏式地请求服务。上报失败的进度保持待上报状态：`OnError` 会收到这次失败，下一次变化、`Flush` 或 `Connect` 会重新发送。`Pending(service)` 列出某个服务尚未确认的成就。

进度以百分比上报：300 步的成就完成 42 步时上报 14%。由于 `gameservices` 上报的是百分比，在 Play 管理中心中应把它配置为标准成就，而不是递增式成就。

不通过 `SignIn` 登录的游戏可以用 `Connect(service, reporter)` 和 `Disconnect` 直接连接和断开服务；`GameServices(cx)` 是基于 `gameservices.ReportAchievement` 的 `Reporter`，任何带 `ReportAchievement(id, percent, done)` 方法的类型都是 `Reporter`。`Reporter` 必须对每次上报恰好应答一次；`cx` 被释放后 `GameServices(cx)` 不再应答，此时应调用 `Disconnect`。

## 平台配置

- **Game Center**：在 App Store Connect 中为应用的 ID 启用 Game Center，并用 `platforms.gameCenter` 中的 ID 创建每个成就。NSGo 目前还不会给构建出的应用添加 Game Center 权限（entitlement）。
- **Play Games**：在 Play 管理中心创建每个成就，并把生成的 ID 填到 `platforms.playGames`。NSGo 的 `gameservices` 在 Android 上的 Go 部分已经就绪，但连接 Play 游戏服务的 Java 运行时钩子尚未进入 NSGo 运行时，因此目前 Android 上 `CanUseGameServices` 为 false，`SignIn` 会返回 `ErrUnsupported`；进度仍会保留，等该钩子发布后再上报。

## 测试

NSGo 的 `nsgotest` 为每个测试应用提供一个内存中的游戏服务替身。用 `nsgotest.Environment(core.Environment{OS: core.OSAndroid, CanUseGameServices: true})` 启用它，用 `App.SetGameServicesPlayer` 让玩家登录，再用 `App.GameServicesAchievements` 读取已上报的内容：

```go
app := nsgotest.Start(t, root, nsgotest.Environment(core.Environment{OS: core.OSMacOS, CanUseGameServices: true}))
app.SetGameServicesPlayer(gameservices.Player{ID: "p1", DisplayName: "Ada"})
// ... 一直玩到成就解锁 ...
app.Settle()
got := app.GameServicesAchievements() // [{ID: "com.example.mygame.first_match", PercentComplete: 100, Unlocked: true}]
```

`App.SetGameServicesError` 和 `App.SetGameServicesAuthError` 可以模拟服务故障和取消登录。[SlideGem 示例](https://github.com/nsgo-dev/game/tree/main/examples/slidegem)就是这样在 Game Center 和 Play Games 上各解锁它的第一个成就。

## Epic Online Services

`pkg/online/eos` 为 Epic 成就、统计分数、排行榜和状态提供异步 EOS 边界。将启动器传入的 `-AUTH_PASSWORD` exchange code 交给 `Service.Login`，并在每个引擎帧调用驱动的 `Tick`。驱动回调会投递到引擎 Scope。

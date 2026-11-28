# leaderboard

## 小程序好友排行

在目录中使用包含 `key` 和 `order` 的 `miniGame` 条目，并通过 `ProviderMiniGame` 使用。`Submit` 只会把更好的成绩写入宿主用户云存储，`Show` 和 `Hide` 控制开放数据域矩形。好友行始终留在隔离的数据域中，因此 `Scores` 仍不支持。模拟移动环境只有在具备小程序宿主能力时才报告 `ProviderMiniGame`。

```go
import "github.com/nsgo-dev/game/pkg/online/leaderboard"
```

`leaderboard` 向平台自带的排行榜提交分数并显示排行榜：iOS、iPadOS、tvOS 和 macOS 上是 Game Center，Android 上是 Play 游戏服务，微信和抖音小游戏上是通过宿主云托管与开放数据域实现的好友排行。它是 NSGo [`gameservices`](https://github.com/nsgo-dev/nsgo/tree/main/gameservices) 包与小游戏宿主 API 上的一层薄封装，让游戏在一个 YAML 文件里一次性声明排行榜的名字，代码里不再到处传平台 ID。

## 声明排行榜

目录把每个排行榜在游戏里的名字对应到它在各个平台服务里的配置：

```yaml
version: 1
leaderboards:
  - name: high-score
    gameCenter: com.example.game.high_score
    playGames: CgkIxxxxxxxxxxEAIQAQ
    miniGame:
      key: best
      order: desc
  - name: fastest-run
    gameCenter: com.example.game.fastest_run
    miniGame:
      key: fastest
      order: asc
```

| 字段 | 含义 |
| --- | --- |
| `version` | 格式版本，`1` |
| `leaderboards` | 排行榜列表，按游戏列出的顺序 |
| `name` | 游戏自己给排行榜起的键；不可重复，不含控制字符，首尾不能有空白 |
| `gameCenter` | 在 App Store Connect 注册的排行榜 ID；不能与其他排行榜重复 |
| `playGames` | Google Play 管理中心生成的排行榜 ID；不能与其他排行榜重复 |
| `miniGame` | 微信和抖音小游戏好友排行配置：`key`（宿主用户云存储键，目录内不可重复，最多 128 字节）以及可选的 `order`（`desc` 或 `asc`，省略时默认为 `desc`） |

每个排行榜至少要有一项平台配置（`gameCenter`、`playGames` 或 `miniGame`）。Game Center 和 Play Games 的 ID 最多 100 个字符，只能是 ASCII 字母、数字、`.`、`_` 或 `-`，排序方式、分数格式和重置周期在 App Store Connect 和 Play 管理中心里设置。目录中最多声明 128 个 `miniGame` 键（`MaxMiniGameKeys`），每个键最多 128 字节（`MaxMiniGameKeyLength`），与微信用户云存储上限一致。

`Parse` 拒绝未知字段、重复键、其他版本和第二个 YAML 文档。`Catalog.Encode` 按格式定义的顺序写出字段并省略缺少的配置，所以 `Encode` 写出的文件解析后再编码，逐字节不变。

## 提交和显示

```go
//go:embed leaderboards.yaml
var leaderboardsYAML []byte

catalog, err := leaderboard.Parse(leaderboardsYAML)
boards, err := leaderboard.New(cx, catalog)

boards.SignIn(nil) // 启动时调用；平台可能弹出自己的登录界面

// 一局结束时：
boards.Submit("high-score", int64(score), func(err error) {
	if err != nil {
		status.Set("分数未提交")
	}
})

// 在按钮里：
if err := boards.Show("high-score"); err != nil { ... }
```

- `SignIn` 让本地玩家登录，并把结果保存在信号里：读取 `Player` 和 `SignedIn` 的视图会随之更新。
- `Submit` 为已登录的玩家提交分数。Game Center 和 Play Games 由平台按排行榜自身的排序方式保留最好成绩；在小游戏（`ProviderMiniGame`）上，`Service` 仅当新分数按排行榜的 `order` 优于玩家此前最好成绩时才调用宿主用户云存储（`setUserCloudStorage`），并将最好成绩缓存在 `cx.Storage()` 中以跨重启保留，因此游戏同样可以在每局结束时直接提交。
- `Show` 打开平台的排行榜界面。在小游戏上，`Show` 在整个游戏视图上展示开放数据域好友排行；`ShowIn(name, bounds)` 将其放到指定的 `core.Bounds` 矩形区域内，`Hide()` 用于关闭它。
- `Scores` 加载排名数据，供自己绘制排行榜的游戏使用。`Query` 选择 `Global` 或 `Friends`，`AllTime`、`Week` 或 `Today`，以及条数上限（默认 25，最多 100）。在 `ProviderMiniGame` 上，由于好友云数据被隔离在开放数据域中且不会返回给主游戏，`Scores`（包含 `Friends`）始终返回 `ErrUnsupported`。

所有回调都在引擎 goroutine 上运行，且在发起它的调用返回之后，所以可以直接设置信号。

## 小游戏（微信与抖音）

在小程序宿主中（`cx.Env().CanSignInMiniProgram()`），即使设备系统是 iOS 或 Android，`ProviderFor(cx.Env())` 也会选择 `ProviderMiniGame`，绝不会在微信或抖音小游戏里报告 Game Center 或 Play Games。

- 在 `js/wasm` 构建中，只要宿主提供 `setUserCloudStorage`，`New` 会自动对接 `nsgoMP.api`、`wx` 或 `tt`。`Submit` 按微信的 `{"wxgame":{"score":...,"update_time":...}}` 格式编码分数（`EncodeMiniGameScore` / `DecodeMiniGameScore`）并校验 1 KiB 键值对上限（`MaxMiniGameValueBytes`）；`Show` / `ShowIn` / `Hide` 将区域和键消息发往 `getOpenDataContext().postMessage`（或在宿主提供 NSGo 开放数据域桥接时走该桥接）。
- 由于主游戏作用域无法读取好友云数据，请使用 `Show` / `ShowIn` 和 `Hide` 展示宿主开放数据域排行，而不是调用 `Scores`。
- `SignIn` 向宿主请求登录（`cx.SignInMiniProgram`），成功后登录的 `Player` 设置了 `Authenticated`，`ID` 与 `DisplayName` 为空：宿主的登录 code 是交给游戏服务器换取会话的短期凭据，不是玩家身份。需要玩家身份的游戏自行调用 `cx.SignInMiniProgram`，在服务器上换取 code。这里的 `Submit` 不需要先 `SignIn`。
- 在 NSGo 自己合成开放数据域之前（nsgo-dev/nsgo#2672），开放数据域脚本由游戏提供：它接收 `{type: "nsgo.leaderboard.show", key, order, x, y, width, height}` 与 `{type: "nsgo.leaderboard.hide"}` 消息，并用 `getFriendCloudStorage` 绘制排行。`setUserCloudStorage` 与 `getOpenDataContext` 只在小游戏中存在；在小程序（NSGo 目前构建的形态）中找不到宿主，`Available` 为 false，所有调用都报告 `ErrUnsupported`。

## 没有游戏服务的平台

`ProviderFor(env)` 与 `Service.Provider()` 表示游戏当前对接的服务（`ProviderGameCenter`、`ProviderPlayGames`、`ProviderMiniGame` 或 `ProviderNone`）。当 `env.Simulated()` 为 true 且不在小程序宿主中时（例如带 `--nsgo-os ios` 运行的 Web 构建），`ProviderFor(env)` 返回 `ProviderNone`，不会报告 Game Center 或 Play Games。`Available` 表示这里能否使用该服务，`Supports(name)` 表示某个排行榜能否提交和显示；两者都以响应式方式读取环境，视图可以用它们隐藏排行榜按钮。

| 错误 | 何时出现 |
| --- | --- |
| `ErrUnknownLeaderboard` | 目录里没有这个名字。所有平台都会报告，所以在桌面上也能发现拼错的名字 |
| `ErrUnsupported` | 这里没有游戏服务（Windows、Linux、Web、watchOS、没有小游戏宿主的模拟系统构建），或在 `ProviderMiniGame` 上调用 `Scores` |
| `ErrNoPlatformID` | 该排行榜没有当前平台服务的 ID 或 `miniGame` 配置 |
| `ErrUnauthenticated` | 没有玩家登录；`SignedIn` 变为 false |
| `ErrCancelled` | 玩家关闭了登录界面 |
| `ErrUnavailable` | 无法连接该服务，或服务未启用 |

`Submit`、`Show`、`ShowIn` 和 `Scores` 在访问平台之前先检查前三项（`ErrUnknownLeaderboard`、`ErrUnsupported`、`ErrNoPlatformID`）。

需要一张跨平台共享的排行榜，或要在没有游戏服务的平台上提供排行榜的游戏，改用[可自行部署的排行榜服务](leaderboard-selfhost.md)。

## 测试

nsgotest 为每个测试应用提供独立的模拟游戏服务：

```go
app := nsgotest.Start(t, root, nsgotest.Environment(core.Environment{CanUseGameServices: true, OS: core.OSIOS}))
app.SetGameServicesPlayer(leaderboard.Player{ID: "p1", DisplayName: "Ada"})
boards.SignIn(nil)
app.Settle()

boards.Submit("high-score", 4200, nil)
app.Settle()
app.GameServicesSubmittedScores()   // [{LeaderboardID: "com.example.game.high_score", Score: 4200, ...}]
app.GameServicesShownLeaderboards() // 传给 Show 的 ID
```

把 `OS` 设为 `core.OSAndroid` 可以测试 `playGames` ID；用 `SeedLeaderboardScores`、`SetGameServicesFriends` 和 `SetGameServicesError` 测试 `Scores` 和失败路径。

要在 `nsgotest` 中测试小游戏好友排行，使用 `core.Environment{OS: core.OSIOS, Simulated: true, MiniProgramSignIn: true}` 启动测试应用，并通过 `leaderboard.WithMiniGameHost(host)` 传入 `MiniGameHost` 测试替身，以验证最好成绩写入、`Show` / `ShowIn` 区域、`Hide` 以及 `Scores` 返回 `ErrUnsupported`。

## 示例

[Starburst](https://github.com/nsgo-dev/game/tree/main/examples/starburst) 在 `leaderboards.yaml` 里声明 `high-score` 排行榜，一局结束时提交分数，玩家登录后在游戏结束界面显示 LEADERBOARD 按钮，未登录时显示 SIGN IN 按钮，登录后补交这一局的分数。

# leaderboard

```go
import "github.com/nsgo-dev/game/packages/game/leaderboard"
```

`leaderboard` 向平台自带的排行榜提交分数并显示排行榜：iOS、iPadOS、tvOS 和 macOS 上是 Game Center，Android 上是 Play 游戏服务。它是 NSGo [`gameservices`](https://github.com/nsgo-dev/nsgo/tree/main/gameservices) 包上的一层薄封装，让游戏在一个 YAML 文件里一次性声明排行榜的名字，代码里不再到处传平台 ID。

## 声明排行榜

目录把每个排行榜在游戏里的名字对应到它在各个游戏服务里注册的 ID：

```yaml
version: 1
leaderboards:
  - name: high-score
    gameCenter: com.example.game.high_score
    playGames: CgkIxxxxxxxxxxEAIQAQ
  - name: fastest-run
    gameCenter: com.example.game.fastest_run
```

| 字段 | 含义 |
| --- | --- |
| `version` | 格式版本，`1` |
| `leaderboards` | 排行榜列表，按游戏列出的顺序 |
| `name` | 游戏自己给排行榜起的键；不可重复，不含控制字符，首尾不能有空白 |
| `gameCenter` | 在 App Store Connect 注册的排行榜 ID；不能与其他排行榜重复 |
| `playGames` | Google Play 管理中心生成的排行榜 ID；不能与其他排行榜重复 |

每个排行榜至少要有一个 ID，ID 最多 100 个字符，只能是 ASCII 字母、数字、`.`、`_` 或 `-`。排序方式、分数格式和重置周期在 App Store Connect 和 Play 管理中心里设置，不写在目录里。

`Parse` 拒绝未知字段、重复键、其他版本和第二个 YAML 文档。`Catalog.Encode` 按格式定义的顺序写出字段并省略缺少的 ID，所以 `Encode` 写出的文件解析后再编码，逐字节不变。

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
- `Submit` 为已登录的玩家提交分数。平台按排行榜自己的排序方式保留每个玩家的最好成绩，所以游戏可以每局结束都提交。
- `Show` 打开平台的排行榜界面。
- `Scores` 加载排名数据，供自己绘制排行榜的游戏使用。`Query` 选择 `Global` 或 `Friends`，`AllTime`、`Week` 或 `Today`，以及条数上限（默认 25，最多 100）。

所有回调都在引擎 goroutine 上运行，且在发起它的调用返回之后，所以可以直接设置信号。

## 没有游戏服务的平台

`Provider` 表示游戏当前对接的服务（`ProviderGameCenter`、`ProviderPlayGames` 或 `ProviderNone`）。`Available` 表示这里能否使用该服务，`Supports(name)` 表示某个排行榜能否提交和显示；两者都以响应式方式读取环境，视图可以用它们隐藏排行榜按钮。

| 错误 | 何时出现 |
| --- | --- |
| `ErrUnknownLeaderboard` | 目录里没有这个名字。所有平台都会报告，所以在桌面上也能发现拼错的名字 |
| `ErrUnsupported` | 这里没有游戏服务：Windows、Linux、Web、watchOS 和小程序 |
| `ErrNoPlatformID` | 该排行榜没有当前平台服务的 ID |
| `ErrUnauthenticated` | 没有玩家登录；`SignedIn` 变为 false |
| `ErrCancelled` | 玩家关闭了登录界面 |
| `ErrUnavailable` | 无法连接该服务，或服务未启用 |

`Submit`、`Show` 和 `Scores` 在访问平台之前先检查前三项。

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

## 示例

[Starburst](https://github.com/nsgo-dev/game/tree/main/examples/starburst) 在 `leaderboards.yaml` 里声明 `high-score` 排行榜，一局结束时提交分数，玩家登录后在游戏结束界面显示 LEADERBOARD 按钮，未登录时显示 SIGN IN 按钮，登录后补交这一局的分数。

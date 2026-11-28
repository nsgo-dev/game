# eos

```go
import "github.com/nsgo-dev/game/pkg/online/eos"
```

`eos` 是 Epic Online Services 的 Go 边界：exchange code 登录、成就解锁与进度、排行榜提交与查询，以及富状态，所有结果都在引擎 goroutine 上送达。本包定义原生适配器要实现的契约 `eos.Driver`，以及游戏调用的 `eos.Platform`。它暂未附带原生适配器：通过 purego 绑定 EOS SDK 共享库、成就与排行榜文件中的 `epic` ID 以及随游戏分发共享库，都在 [#1770](https://github.com/nsgo-dev/game/issues/1770) 中跟踪。在此之前，游戏传入自己的 driver，测试传入 fake。

## Platform

`eos.New(cx, cfg, driver)` 用 `eos.Config` 中的产品、沙盒和部署 ID 创建平台，并在 `cx` 销毁时关闭它。任一 ID 为空时返回 `eos.ErrInvalidConfig`；`driver` 为 nil 时返回 `eos.ErrUnavailable`，原生适配器在找不到 EOS 共享库时也报告这个错误。

```go
platform, err := eos.New(cx, eos.Config{
	ProductID:    "…",
	SandboxID:    "…",
	DeploymentID: "…",
	ExchangeCode: eos.ExchangeCodeFromArgs(os.Args[1:]),
}, driver)
```

Epic 启动器以 `-AUTH_PASSWORD=<code> -AUTH_TYPE=exchangecode` 启动游戏；`eos.ExchangeCodeFromArgs` 返回这个 code，其他登录类型返回 `""`。

在游戏循环的每一步调用 `platform.Tick()`：EOS 在它的 tick 中完成请求。其他方法同样在引擎 goroutine 上调用：

- `Login(done)` 用 exchange code 登录；之后 `Player()` 和 `SignedIn()` 报告该账号，二者是响应式读取。没有 exchange code 时以 `eos.ErrUnauthenticated` 失败。
- `UnlockAchievement(id, done)` 和 `ProgressAchievement(id, percent, done)`，`percent` 取值 [0,100]。
- `SubmitScore(board, score, done)` 和 `QueryLeaderboard(board, friends, limit, done)`，查询全球或好友排行，`limit` 为正数。
- `SetPresence(details, status, done)`。

`Login` 成功前上报以 `eos.ErrUnauthenticated` 失败，ID 为空或参数越界以 `eos.ErrInvalidArgument` 失败，`Close` 之后的任何调用以 `eos.ErrClosed` 失败。`done` 从不在发起请求的调用内运行：平台把它投递到自己的 scope，并丢弃在 `Close` 之后或 scope 销毁之后才到达的结果。

## Driver

`Driver` 发起每个请求后立即返回，然后对该请求的 `done` 恰好调用一次，通常在它的 `Tick` 内调用（EOS SDK 在那里分发回调），请求无法发起时则立即调用。平台只在引擎 goroutine 上调用 driver；driver 可以在任何 goroutine 上调用 `done`。

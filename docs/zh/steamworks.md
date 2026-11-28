# steam

```go
import "github.com/nsgo-dev/game/pkg/online/steam"
```

`steam` 是 Steam 成就、统计、排行榜和富状态的引擎边界。它定义由 Steamworks 适配器实现的 `Driver` 接口，以及为一个 `core.Scope` 包装驱动的 `Platform`。本包不附带原生适配器：游戏注入自己的 `Driver`，在此之前用 nil 驱动调用 `New` 会返回 `ErrUnavailable`。在 Steam 上发布构建见[发布到 Steam](steam.md)。

## 使用 Platform

用 `steam.New(cx, driver)` 创建平台；scope 销毁时它会自动关闭。调用 `Init` 登录用户，在固定循环中执行 `Tick` 让驱动运行 Steam 的回调，并使用 `Unlock`、`SetStatInt`、`SetStatFloat`、`StoreStats`、`UploadScore`、`Scores` 和 `SetPresence`。成就和统计只有在 `StoreStats` 提交后才会到达 Steam。

每个调用都接收一个完成回调，它通过 `Scope.Post` 在 scope 的 goroutine 上运行，scope 销毁后不再运行。驱动未返回 Steam ID 时 `Init` 报告 `ErrUnauthenticated`，在已关闭的平台上的每个调用都会立即报告 `ErrClosed`。

## 测试

测试中用 fake 实现 `Driver`：fake 记录收到的成就、统计和分数，`nsgotest.Start` 在 `Advance` 时运行投递的回调。

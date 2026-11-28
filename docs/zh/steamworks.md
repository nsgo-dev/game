# Steamworks 统计与排行榜

`game/online/steam` 提供 Steam 成就、统计、排行榜和富状态的引擎边界。将 Steamworks 适配器的 native `Driver` 注入后调用 `Init`，在固定循环中执行 `Tick`，并使用 `Unlock`、`SetStatInt`、`SetStatFloat`、`StoreStats`、`UploadScore`、`Scores` 和 `SetPresence`。所有完成回调都会投递到引擎 scope。测试可以注入 fake driver，不支持的平台返回 `ErrUnavailable`。

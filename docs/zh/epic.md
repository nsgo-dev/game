# Epic Online Services

`game/online/eos` 定义 Epic Online Services 的原生边界。使用产品、沙盒和部署 ID 以及注入的 driver 创建 `eos.Platform`。driver 的 `Login` 接收 Epic 启动器传入的 exchange code，`Tick` 在每个游戏循环步执行一次。可在引擎 goroutine 调用 `UnlockAchievement`、`ProgressAchievement`、`SubmitScore`、`QueryLeaderboard` 和 `SetPresence`。桌面适配器在找不到 EOS 共享库时返回 `eos.ErrUnavailable`。测试可以注入 fake driver，无需加载原生代码。

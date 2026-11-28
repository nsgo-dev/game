# Steamworks

`pkg/online/steamworks` 为 Steam 功能管理单一的进程级生命周期。使用游戏 App ID 只打开一次，并为桌面绑定提供平台驱动。每个引擎帧调用一次 `Pump`，应用关闭时调用 `Close`。Presence 和 Workshop 可以共享此客户端，因此只有客户端所有者负责初始化和关闭 Steamworks。

客户端会解析 Steam 传入的 `+connect <steam-id>` 启动参数，并在监听器订阅前暂存加入事件。覆盖层和关闭事件也使用相同的类型化事件通道。本机库文件由游戏负责，并须根据 SDK 的再分发条款在游戏的 `nsgo-natives.yaml` 中声明。

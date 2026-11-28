# Steamworks 客户端

`game/online/steamworks` 在任意游戏 scope 连接期间维护一个进程级 Steamworks 客户端。它通过 purego 加载平台库，每帧泵送手动分发回调，提供 Overlay 和富状态辅助，处理 `+connect` 与 `+connect_lobby` 启动参数，并提供 Web API 身份票据和 Steam Deck 文本输入。测试使用 `NewFakeDriver`，不支持的平台处理 `ErrUnavailable`。

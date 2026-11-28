
### Match 生命周期处理器

房间可以在接纳玩家前绑定 `gameserver.MatchHandler`。`JoinWithMeta` 在占用席位前调用 `JoinAttempt`，接纳后调用 `Joined`。返回 false 时房间保持不变，并返回处理器给出的原因。`LeaveWithReason` 通过 `Left` 报告主动退出、掉线、踢出或替换。`Room.Signal` 将复制后的消息交给处理器，`Room.Terminate` 在结束房间前调用 `Terminate(grace)`。`Room.SetLabel` 会替换复制到 `RoomInfo.Label` 的可搜索元数据。

旧的 `Join` 和 `Leave` 方法仍然有效，分别使用空元数据和 `quit` 原因。回调应快速返回，较长工作应通过服务器的 match mailbox 协调。

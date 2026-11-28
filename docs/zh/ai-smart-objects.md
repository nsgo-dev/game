# AI 智能对象

`ai.SmartObjects` 是用于可交互世界对象的确定性注册表。每个对象提供带标签的槽位和行为名称。`FindInRadius` 按稳定的对象/槽位顺序返回可用匹配槽位，`Claim` 防止重复使用，`Disable` 通过回调释放所有占用，`Snapshot`/`Restore` 为存档保留占用状态。

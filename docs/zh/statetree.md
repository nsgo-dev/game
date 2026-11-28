# 状态树

`ai.ParseStateTree` 校验版本 1 YAML 状态树，支持嵌套状态、进入条件、行为树任务名与参数、完成模式和带优先级的转换。`ai.EncodeStateTree` 写出 Schema 注释与确定性的 YAML，`ai.LoadStateTreeFS` 可从资源文件系统读取相同格式。目前交付的是格式解析边界；运行时步进、绑定、快照与检查器集成留待后续工作。

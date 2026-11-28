# records

`github.com/nsgo-dev/game/pkg/online/records` 提供服务端权威 JSON 记录边界。记录由所有者、集合和键组成，携带单调递增版本，并可在一次 compare-and-set 事务中应用。`PublicRead` 记录可被其他玩家列出，所有者记录保持私有。

`Store.Apply` 会在修改任何记录前校验所有期望版本。`Expected: 0` 要求记录不存在，正数期望版本执行条件更新。冲突返回 `ErrChanged`，整个事务保持不变。内存核心供持久化 SQLite/Postgres 适配器以及钱包和比赛结果事务使用。

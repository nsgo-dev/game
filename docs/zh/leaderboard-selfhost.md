# leaderboard/selfhost

```go
import "github.com/nsgo-dev/game/pkg/online/leaderboard/selfhost"
```

`leaderboard/selfhost` 是可自行部署的排行榜服务，面向跨多个平台运行、或运行在没有游戏服务的平台（Web、Windows、Linux）上的游戏。[`leaderboard`](leaderboard.md) 显示的是 Game Center 和 Play 游戏服务自己的排行榜；这个服务则为所有平台维护同一张榜，以[账号服务](accounts.md)登录的账号为键，所以浏览器里的成绩和手机上的成绩落在同一张榜上。

它是服务端代码，只依赖标准库、YAML 包和 `accounts/token`，不会链接账号服务的密码哈希或数据库驱动。

| 包 | 内容 |
| --- | --- |
| `leaderboard/selfhost` | `Service`、`Config`、`Store` 接口及 `MemoryStore` 与 `FileStore`、`ProofSigner` 和 HTTP `Handler` |
| `cmd/leaderboardd` | 把成绩存在一个目录里的独立 HTTP 服务器 |

## 声明排行榜

服务从带版本号的 YAML 文件读取排行榜：

```yaml
version: 1
audience: leaderboard
proofTTL: 10m
boards:
  - name: high-score
    order: descending
    reset: allTime
  - name: weekly-sprint
    order: ascending
    reset: weekly
    weekStart: monday
    keep: 4
    validation: signed
    min: 1
    max: 3600000
serverKeys:
  - id: 2317febd373fcbcd
    key: hze+0cYruVRddgjkbHzgCeWWJbEsBkn6K8JNHRDf6Wg=
allowedOrigins:
  - https://play.example.com
```

| 字段 | 含义 |
| --- | --- |
| `version` | 模式版本，`1` |
| `audience` | 处理器接受的令牌受众，缺省为 `leaderboard`；账号服务必须在 `audiences` 中列出它，而且它必须与密钥集的受众不同 |
| `proofTTL` | 游戏服务器的成绩证明最长有效多久，`1s` 到 `24h`；缺省为 `10m` |
| `boards` | 排行榜，按服务列出它们的顺序 |
| `name` | 排行榜在 URL 和数据目录里的键：`a-z`、`0-9`、`.`、`_` 和 `-`，最多 64 个字符，以字母或数字开头 |
| `order` | `descending` 把最高分排在最前（积分），`ascending` 把最低分排在最前（用时） |
| `reset` | `allTime`、`daily`（UTC 午夜）或 `weekly`（`weekStart` 那天的 UTC 午夜） |
| `weekStart` | 周榜重置的那一天，小写英文星期名；缺省为 `monday` |
| `keep` | 日榜或周榜重置后仍可读取的往期数量，1 到 1000；缺省为 1 |
| `validation` | `signed` 只接受带游戏服务器证明的成绩；缺省则信任玩家的客户端 |
| `min`、`max` | 排行榜接受的分数范围；正负都不超过 2^53-1，保证每个分数在 JavaScript 的 JSON 数字里不失真 |
| `serverKeys` | 游戏服务器签发证明所用的 Ed25519 公钥；`signed` 排行榜至少需要一个 |
| `allowedOrigins` | 允许其页面调用服务的网页 origin；为空时省略 |

`ParseConfig` 拒绝未知字段、重复键、其他版本、第二个 YAML 文档，以及重复或带路径、查询、片段、用户信息、通配符、1 到 65535 以外端口或 `http`、`https` 以外协议的 origin。可选字段保留文件里写的内容，`Config.Marshal` 按上表的顺序写出字段并在为空时省略 `allowedOrigins`，所以这种形式的文件解析后再写出，逐字节不变。

## 排名

排行榜为每个账号记录它在当前周期内的最好成绩。只有按排行榜的顺序胜过该账号最好成绩的分数才会计入；相同的分数保留较早的那一次。分数相同时先达到的排在前面，再按账号 ID 排序，所以每个条目都有自己的名次。

周期以 UTC 计算。日榜或周榜在重置时开始一个新的空周期，上一个周期仍可读取，更早的周期按 `keep` 的数量保留。每个周期都有 ID：总榜为 `all`，其余为周期开始那天的 UTC 日期，例如 `2026-11-23`。查询用 `current`（默认）、`previous` 或往期的 ID 指定周期。

```go
svc, err := selfhost.New(ctx, cfg, selfhost.Options{
	Store:  store,      // selfhost.OpenFileStore("scores", selfhost.FileStoreOptions{})
	Tokens: verifier,   // token.NewVerifierFor(keySet, cfg.TokenAudience())，不要用 token.NewVerifier
	Logger: slog.Default(),
})

res, err := svc.Submit(ctx, selfhost.Submission{Board: "high-score", Account: claims.Subject, Score: 4200})
// res.Entry.Rank、res.Improved、res.Period.ID

page, err := svc.Top(ctx, "high-score", "current", 0, 25)               // 第 1-25 名
near, err := svc.Around(ctx, "weekly-sprint", "previous", account, 5)   // 上下各 5 名
pals, err := svc.Friends(ctx, "high-score", "", friendIDs)              // 这些账号的排名
```

| 方法 | 作用 |
| --- | --- |
| `Submit` | 在排行榜的当前周期记录一个分数；`Result.Improved` 表示它是否成为该账号的最好成绩 |
| `Top` | 从第 `offset+1` 名起最多 `limit` 个条目（最多 100） |
| `Around` | 该账号的条目及其上下各最多 `radius` 个条目（最多 50）；靠近两端时窗口会平移，保持大小不变 |
| `Friends` | 游戏提供的最多 200 个账号（平台好友列表或游戏自己的社交关系）的条目，每个都带它在整张榜上的名次 |
| `Rank` | 单个账号的条目 |
| `Boards` | 所有排行榜及其当前周期和仍可读取的往期 |

错误有 `ErrUnknownBoard`、`ErrUnknownPeriod`（没有这个周期，或已不再保留）、`ErrNotRanked`、`ErrInvalidAccount`、`ErrScoreOutOfRange`、`ErrProofRequired`、`ErrInvalidProof`、`ErrProofExpired` 和 `ErrInvalidQuery`。

每个周期在内存里是一个顺序统计跳表，所以每次查询的开销是 O(log n) 加上返回的条目数。在一百万个账号的排行榜上，基准测试取前 100 名约 7 µs，取榜单深处的 100 个条目约 60 µs，取账号附近的条目约 40 µs，提交约 10 µs（`go tool nsgo test ./pkg/online/leaderboard/selfhost -run '^$' -bench .`）；任何查询超过 50 ms 时基准测试失败，CI 在每个拉取请求上运行这些查询基准测试。

## 服务器校验的成绩

客户端可以发送任意分数。`validation: signed` 的排行榜只接受带游戏服务器证明的成绩：服务器判定一局比赛或检查完回放后，对排行榜、账号和分数签名，把证明交给该玩家的客户端，客户端连同分数一起提交。

```go
key, err := token.ParsePrivateKey(pemData) // 来自 leaderboardd genkey
prover, err := selfhost.NewProofSigner(key)
proof, err := prover.Sign("weekly-sprint", claims.Subject, result.TimeMillis)
// 把 proof 随比赛结果发给玩家
```

证明的形式是 `v1.<密钥 id>.<unix 秒>.<签名>`，即对排行榜、账号、分数和时间的 Ed25519 签名。服务拒绝缺少证明的成绩（`ErrProofRequired`），拒绝由不在 `serverKeys` 里的密钥签发、针对其他排行榜、账号或分数、或签发时间在未来的证明（`ErrInvalidProof`），也拒绝超过 `proofTTL` 的证明，以及在每日或每周排行榜上于当前周期开始之前签发的证明（`ErrProofExpired`），所以重置之前签发的成绩不会计入重置之后的周期。`Options.Leeway` 容许机器之间的时钟偏差。证明里写明了账号，所以玩家无法提交别人的证明。

## HTTP

`Service.Handler` 以 JSON 提供服务。除 `GET /v1/openapi.yaml` 外，每个请求都在 `Authorization: Bearer <token>` 里携带玩家的账号令牌；处理器用 `Options.Tokens` 离线校验，只接受签给配置中受众的令牌（见[令牌受众](accounts.md#令牌受众)），所以玩家发给游戏服务器的令牌无法替玩家提交成绩；`New` 拒绝其他受众的校验器，也拒绝接受其密钥集中游戏服务器受众的校验器，配置也不能写 `accounts`（账号服务自身的受众）。令牌的主体就是提交成绩的账号，所以玩家只能提交自己的成绩。完整的机器可读契约以 [OpenAPI 3.1 描述文件](https://game.nsgo.dev/schemas/leaderboard-selfhost-openapi.v1.yaml) 发布，也通过 `GET /v1/openapi.yaml` 在线提供。

| 请求 | 响应 |
| --- | --- |
| `GET /v1/openapi.yaml` | YAML 格式的 OpenAPI 3.1 描述（无需认证） |
| `GET /v1/boards` | `{"boards": [{"name", "order", "reset", "validation", "current", "previous"}]}` |
| `POST /v1/boards/{board}/scores` `{"score", "proof"}` | `{"period", "entry", "total", "improved"}` |
| `GET /v1/boards/{board}/top?period=&offset=&limit=` | 一页条目；缺省 `limit` 为 25 |
| `GET /v1/boards/{board}/around?period=&radius=` | 令牌所属账号附近的一页条目；缺省 `radius` 为 5 |
| `POST /v1/boards/{board}/friends?period=` `{"accounts": [id]}` | 这些账号的一页条目 |

一页是 `{"board", "period", "total", "entries"}`，周期是 `{"id", "start", "end"}`（总榜没有 `start` 和 `end`），条目是 `{"rank", "account", "score", "achieved"}`。错误是 `{"error": code}`：`invalid_token` 401；`proof_required`、`invalid_proof` 和 `proof_expired` 403；`unknown_board`、`unknown_period` 和 `not_ranked` 404；`bad_request`、`invalid_account` 和 `score_out_of_range` 400；`request_too_large` 413；`unavailable` 503；`internal` 500。

条目里是账号 ID；要显示名字的游戏到自己的玩家资料里查找。对 `allowedOrigins` 中的 origin，处理器会应答 CORS 预检请求并允许 `Authorization` 和 `Content-Type`；其他 origin 不会收到 CORS 响应头。处理器要放在 TLS 和限流器之后。

## 存储

服务把记录保存在 `Store` 里。`NewMemoryStore` 用于测试。`OpenFileStore` 为每个排行榜周期保存一个 JSON Lines 文件，路径为 `<dir>/<board>/<period>.jsonl`，每创一次新的最好成绩写一行：

```json
{"account":"3f9a...","score":4200,"achieved":"2026-11-24T09:15:02.5Z"}
```

`New` 读取这些文件。它截掉崩溃时写了一半的最后一行，把大多是已被超越的成绩的文件改写为每个账号一行，并删除比排行榜保留的周期更早的周期；重置发生时也会随即删除。写入失败时会撤回已写的部分，所以文件里不会留下已告知客户端丢失的成绩。`New` 只删除它能归位的周期：从配置里移除的排行榜的成绩，以及在排行榜修改 `reset` 或 `weekStart` 之前保存的周期，都留在磁盘上，不再提供，并记录一条警告。比当前周期更晚的周期（开机时时钟落后的机器会看到这种情况）会保留下来，等时钟走到它时再提供。`FileStoreOptions.Sync` 让每次写入都刷到磁盘，适用于可能断电的机器。自己实现的 `Store` 需要提供 `Load`、`Save` 和 `Drop`。

## leaderboardd

```sh
leaderboardd init   -config leaderboards.yaml
leaderboardd genkey -key server.pem
leaderboardd serve  -config leaderboards.yaml -keyset keys.yaml -data scores -addr :8081
```

`init` 写出一份只有一个总榜 `high-score` 且不含 `allowedOrigins` 的配置供修改；如果网页构建需要从其他 origin 调用该服务，请在 `serve` 之前把游戏页面的 origin 加入 `leaderboards.yaml` 的 `allowedOrigins`。`genkey` 写出一个游戏服务器签名密钥，并打印要粘贴进配置的 `serverKeys` 条目。`serve` 用账号服务的密钥集文件（由 `accountsd genkey` 写出）校验玩家令牌，只接受配置中 `audience` 的令牌，如果它等于密钥集的受众（即游戏服务器的受众）就拒绝启动，把成绩保存在 `-data` 下，收到 SIGINT 或 SIGTERM 时停止。`-sync` 在确认每条成绩之前先把它刷到磁盘，适用于可能断电的机器。

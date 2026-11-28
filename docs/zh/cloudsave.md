# cloudsave

`github.com/nsgo-dev/game/pkg/online/cloudsave` 是可自行部署的云存档服务：每个玩家账号都有经 HTTPS 访问的存档槽位，按版本保存。两台设备同时写同一个槽位时不会互相覆盖，超过大小上限的槽位会以明确的错误被拒绝，每个槽位最近的几个版本可以列出并恢复。游戏通过 `liveops.CloudClient`（一个 `liveops.CloudSync`）访问它，因此 `liveops.CloudSyncer` 能让本地存档与它保持一致，并按自身策略解决冲突。

该服务是服务端代码，放在独立的包里；游戏客户端只导入 `liveops`。它自己不保存账号：它用 `accounts/token` 离线校验[账号服务](accounts.md)签发的令牌，只接受签给它自己受众的令牌（默认是 `cloudsave`，见[令牌受众](accounts.md#令牌受众)），并把每位玩家的槽位归在令牌的 subject（即账号 ID）名下。玩家在网页和 Android 手机上登录同一个账号，读写的就是同一组槽位。

| 包 | 内容 |
| --- | --- |
| `cloudsave` | `Service`、`Config`、`Store` 接口、`MemoryStore` 与 HTTP `Handler` |
| `cloudsave/sqlstore` | 存放在 SQLite（`OpenSQLite`，纯 Go，除网页外的所有目标平台）或 Postgres（`OpenPostgres`，可由多个服务进程共享）中的 `Store` |
| `cloudsave/storetest` | `Store` 契约测试，供自定义存储使用 |
| `cmd/cloudsaved` | 独立运行的 HTTP 服务器 |
| `liveops` | `CloudClient`，游戏这一侧 |

## 在设备上

```go
cloud, err := liveops.NewCloudClient(liveops.CloudClientOptions{
	URL: "https://saves.example.com",
	Token: func(ctx context.Context) (string, error) {
		return session.TokenFor(ctx, "cloudsave") // POST /v1/refresh {"session", "audience": "cloudsave"}，过期时刷新
	},
})
if err != nil {
	return err
}
syncer, err := liveops.NewCloudSyncer(liveops.CloudSyncOptions{
	Local:       save.StorageBackend(cx.Storage(), "mygame"),
	Cloud:       cloud,
	State:       cx.Storage(),
	Device:      deviceID,
	SaveVersion: 2, // 当前构建写入的 Store.Version
	Policy:      liveops.PolicyKeepBoth,
})
```

通过 `save.NewStore(syncer.Backend(), save.WithVersion(2), ...)` 保存，并在游戏启动、保存之后以及回到前台时调用 `syncer.Sync(ctx)`；同步器如何发现和解决冲突，见 [liveops](liveops.md#云存档)。

## 混合客户端版本

当玩家在较新版本（例如存档版本 3）游玩后，又打开较旧版本的客户端（存档版本 2）时，将 `CloudSyncOptions.SaveVersion` 设为当前构建的 `save.Store.Version`，即可避免旧客户端覆盖任一侧的存档：

- 写入云端副本到本地前，`CloudSyncer` 会检查其存档信封版本（`save.EnvelopeVersion`）。当版本号高于 `SaveVersion` 时，同步器不会改动本地槽位和同步状态，而是报告 `liveops.OutcomeNeedsUpdate` 并填充 `SlotResult.LocalVersion` 与 `SlotResult.CloudVersion`（可通过 `res.NeedsUpdate()` 获取），游戏据此提示玩家更新，同时本地存档仍可正常加载。
- 即使玩家在旧版本上继续游玩或重新开始并保存，下一次同步也会看到两端分叉，绝不会把旧版本存档上传覆盖云端较新版本的存档。
- 在两端存档信封版本不同的冲突中，`CloudSyncer` 绝不会偏向较旧信封版本的副本，而是回退为保留两份（当云端副本仍高于 `SaveVersion` 时，把本地副本分叉保留而不覆盖本地槽位，并报告 `OutcomeNeedsUpdate`）。`Conflict.Local.EnvelopeVersion` 与 `Conflict.Cloud.EnvelopeVersion` 会同时给出两端版本号，便于 `PolicyAsk` 对话框向玩家说明该副本由更新的游戏版本保存。

```go
res, err := syncer.Sync(ctx)
if err != nil {
	return err
}
for _, slot := range res.NeedsUpdate() {
	showUpdatePrompt(slot.Slot, slot.LocalVersion, slot.CloudVersion)
}
```

`CloudClient` 要求 `https` URL；`http` 只在回环主机（`localhost`、`127.0.0.1`、`::1`）上用于开发。它使用 `net/http`，因此在所有目标平台上都能工作：在网页上，请求经由浏览器的 `fetch` 发出，页面的 origin 必须列在服务的 `allowedOrigins` 中；Android 应用需要 `INTERNET` 权限。它的错误包装了 `liveops` 的哨兵错误：

| 错误 | 何时出现 |
| --- | --- |
| `ErrSlotNotFound` | 槽位或所请求的版本不存在 |
| `ErrCloudChanged` | 条件写入输给了另一台设备的写入；没有写入任何内容 |
| `ErrSlotTooLarge` | 负载超过服务的 `maxSlotBytes`；错误信息中给出上限 |
| `ErrTooManySlots` | 新槽位会超过服务的 `maxSlots` |
| `ErrCloudUnavailable` | 网络或服务出错、令牌来源出错，或服务拒绝了令牌；同步遇到它会停止，和离线时一样 |

上下文结束时，原样返回上下文的错误。

## 并发写入

每次写入都指明它要替换的负载。`CloudClient` 实现了 `liveops.ConditionalCloudSync`：`SaveIf(ctx, slot, data, match)` 只在槽位保存的负载的 `liveops.PayloadHash`（`"sha256:"` 加原始字节 SHA-256 的十六进制）等于 `match` 时写入，`match` 为空时只在槽位不存在时写入；`DeleteIf` 按同样的条件删除。在 HTTP 上，条件写作 `If-Match: "<hash>"` 或 `If-None-Match: *`；服务在一个原子步骤中检查并写入，槽位内容不符时返回 `412 changed`。

`CloudSyncer` 的每次云端写入都以它读到的副本为条件。手机和浏览器都改动了同一个槽位、浏览器先上传时，手机的上传以 `ErrCloudChanged` 失败且不做任何改动；手机下次同步时会把两份副本视为冲突，由策略保留较新的一份、把两份都保留为分叉槽位，或询问玩家。任何上传都不会被悄悄覆盖。

## 版本与恢复

每次写入或删除都会给槽位增加一个从 1 开始编号的版本，服务保留最新的 `keepVersions` 个。删除是一个把槽位标记为已删除的版本，因此已删除的槽位仍保留历史，可以恢复。

```go
versions, err := cloud.Versions(ctx, "main") // 从新到旧：Number、Deleted、Hash、Size、Created
data, err := cloud.LoadVersion(ctx, "main", versions[2].Number)
err = cloud.Restore(ctx, "main", versions[2].Number)
err = cloud.Purge(ctx, "main") // 永久删除槽位及其历史
```

`Restore` 把旧副本写成一个新版本，条件是槽位仍保存着它读到的副本（否则返回 `ErrCloudChanged`）；恢复一个删除版本会删除该槽位。`CloudSyncer` 写入的负载会用下一个修订号和当前时间重新标记，因此每台设备都把恢复的副本当作最新副本：自上次同步以来副本没有改动的设备会下载它，有未同步改动的设备会得到一个由其策略解决的冲突。其他负载按字节原样恢复。服务端的 `Service.Restore` 按原样恢复字节。

## 上限

- `maxSlotBytes` 限制负载大小。更大的负载会在服务读过上限之前以 `413 slot_too_large` 和 `{"limit": maxSlotBytes}` 被拒绝，槽位保留当前副本。`CloudSyncer` 的负载是包在 JSON 信封里的存档：多出约 250 字节，存档中的引号和反斜杠会被转义；非 UTF-8 数据改用 base64，体积大三分之一。
- `maxSlots` 限制一个账号的有效槽位数；会再创建一个槽位的写入以 `409 too_many_slots` 被拒绝。`PolicyKeepBoth` 创建的分叉槽位也计入。已删除的槽位不计入，但一个账号的已删除槽位超过 `maxSlots` 个后，最早删除的会连同历史一起被清除，因此一个账号保存的槽位永远不超过 `maxSlots` 的两倍。
- 槽位名是 1 到 200 字节、不含控制字符和 `/` 的 UTF-8，`.` 和 `..` 除外（`ValidateSlot`）；`save.ValidateSlot` 接受的名称都能通过。在 URL 中它们是路径段，按 `url.PathEscape` 的方式转义。

## 配置

`Config` 是带版本号的 YAML 文件：

```yaml
version: 1
audience: cloudsave
maxSlotBytes: 1048576
maxSlots: 32
keepVersions: 5
allowedOrigins:
  - https://play.example.com
```

| 字段 | 含义 |
| --- | --- |
| `version` | 模式版本，`1` |
| `audience` | 服务接受的令牌受众，省略时为 `cloudsave`；账号服务必须在 `audiences` 中列出它，而且它必须与密钥集的受众不同 |
| `maxSlotBytes` | 最大负载，1 字节到 64 MiB |
| `maxSlots` | 每个账号的有效槽位数，1 到 10000 |
| `keepVersions` | 每个槽位保留的版本数（含当前版本），1 到 100 |
| `allowedOrigins` | 允许其页面调用服务的网页 origin；为空时省略 |

`ParseConfig` 拒绝未知字段、尾随文档、超出范围的值，以及带路径、通配符、1 到 65535 以外端口或 `http`、`https` 以外协议的 origin。`Config.Marshal` 写出规范形式：字段按上表顺序，`audience` 只在设置时写出，`allowedOrigins` 只在有条目时写出。规范形式的文件（例如 `Marshal` 或 `cloudsaved init` 写出的）读回后再写出，内容不变。`DefaultConfig` 返回上面所示的设置，不含 origin。

## 存储

`Store` 保存版本。它的 `Append` 是原子的：基于同一版本的两次写入，至多一次提交成功。

```go
store, err := sqlstore.OpenSQLite(ctx, "saves.db")                     // 单个服务进程
store, err := sqlstore.OpenPostgres(ctx, os.Getenv("CLOUDSAVE_POSTGRES_DSN")) // 多个进程
verifier, err := token.NewVerifierFor(keySet, cfg.TokenAudience())   // 不要用 token.NewVerifier，它接受游戏服务器令牌
svc, err := cloudsave.New(cfg, cloudsave.Options{Store: store, Tokens: verifier, Logger: slog.Default()})
```

`New` 拒绝受众不是 `Config.TokenAudience` 的校验器。

SQL 存储有两张表：`cloudsave_slots`（每个槽位的最新版本）和 `cloudsave_versions`（保留的版本及其负载），模式版本记在 `cloudsave_schema` 中。每次写入在一个事务中比较并设置槽位的最新版本号；在 Postgres 上还会对账号加咨询锁，因此槽位计数和版本修剪能看到所有并发写入。SQLite 使用纯 Go 的 `modernc.org/sqlite` 驱动，开启预写日志并只用一个连接；Postgres 使用 `github.com/jackc/pgx/v5`。两者都会在表缺失时创建表，并拒绝比自身更新的模式。`NewMemoryStore` 把一切保存在内存中，用于测试和原型；自定义存储需通过 `storetest.Run`。

## HTTP

`Service.Handler` 提供该服务。除 `GET /v1/openapi.yaml` 外，每个请求都带有 `Authorization: Bearer <token>`，令牌由账号服务签给本服务的受众；其他受众的令牌（例如玩家发给游戏服务器的那个）返回 `invalid_token`。每个响应都带有 `Cache-Control: no-store`。负载为 `application/octet-stream`；版本是 `{"slot", "version", "deleted", "hash", "size", "created"}`，`created` 为带毫秒的 RFC 3339，`deleted` 和 `hash` 只在有值时出现。完整的机器可读契约以 [OpenAPI 3.1 描述文件](https://game.nsgo.dev/schemas/cloudsave-openapi.v1.yaml) 发布，也通过 `GET /v1/openapi.yaml` 在线提供。

| 请求 | 应答 |
| --- | --- |
| `GET /v1/openapi.yaml` | YAML 格式的 OpenAPI 3.1 描述（无需认证；对 `allowedOrigins` 允许 CORS） |
| `GET /v1/saves` | `{"slots": [version]}`，按名称排序的有效槽位 |
| `GET /v1/saves/{slot}` | 负载，带 `ETag`（加引号的哈希）和 `Save-Version` |
| `PUT /v1/saves/{slot}` | version；`If-Match: "<hash>"` 或 `If-None-Match: *` 使其成为条件写入 |
| `DELETE /v1/saves/{slot}` | 204；同样的请求头使其成为条件删除 |
| `GET /v1/saves/{slot}/versions` | `{"versions": [version]}`，从新到旧 |
| `GET /v1/saves/{slot}/versions/{n}` | 版本 `n` 的负载 |
| `DELETE /v1/saves/{slot}/versions` | 204，槽位及其历史被清除 |

错误为 `{"error": code}`：`bad_request` 400（无效的槽位名、版本号或前置条件请求头）、`invalid_token` 401、`not_found` 404、`too_many_slots` 409、`deleted` 410（删除版本没有负载）、`changed` 412、带 `limit` 的 `slot_too_large` 413、请求被取消或超时时的 `unavailable` 503，以及 `internal` 500。对 `allowedOrigins` 中的 origin，处理器会应答 CORS 预检请求，并暴露 `ETag` 和 `Save-Version`。它不限制请求速率：请在 TLS 和限流器之后运行它。

## cloudsaved

`cmd/cloudsaved` 运行该服务：

```sh
cloudsaved init  -config cloudsave.yaml
cloudsaved serve -config cloudsave.yaml -keyset keys.yaml -sqlite saves.db -addr :8081
CLOUDSAVE_POSTGRES_DSN=postgres://... cloudsaved serve -config cloudsave.yaml -keyset keys.yaml -postgres
```

`init` 从不覆盖已有配置。`-keyset` 是 `accountsd genkey` 维护的密钥集文件；`serve` 只在启动时读取一次，因此密钥轮换添加新密钥后需要重启它。`serve` 只接受配置中 `audience` 的令牌；如果它等于密钥集的受众（即游戏服务器的受众），`serve` 拒绝启动。`serve` 向 stderr 输出 JSON 日志，收到 SIGINT 或 SIGTERM 时停止。

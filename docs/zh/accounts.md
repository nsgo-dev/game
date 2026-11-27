# accounts

`github.com/nsgo-dev/game/pkg/online/accounts` 是可自行部署的玩家账号服务。玩家以游客身份开始，之后可升级为邮箱账号，或绑定 OAuth 与平台身份（Game Center、Play Games、Steam），数据不会丢失；每台设备都会拿到短时效令牌，游戏服务器离线即可校验。

该服务是服务端代码，放在独立的包里。不导入它们的游戏客户端或游戏服务器不会链接它们的任何依赖（`golang.org/x/crypto` 的 argon2id、SQLite 与 Postgres 驱动）；只校验令牌的游戏服务器只导入 `accounts/token`，它只依赖标准库和 YAML 包。

| 包 | 内容 |
| --- | --- |
| `accounts` | `Service`、`Config`、`Store` 接口、`MemoryStore`、argon2id 密码哈希与 HTTP `Handler` |
| `accounts/token` | Ed25519 令牌 `Signer` 与离线 `Verifier`、`KeySet` YAML 文件和 PEM 签名密钥 |
| `accounts/sqlitestore` | 存于 SQLite 文件的 `Store`（纯 Go，无 cgo）；Web 以外的所有目标 |
| `accounts/pgstore` | 存于 Postgres 的 `Store`，可供多个服务进程共享 |
| `accounts/sqlstore` | 两种存储共用的 SQL 表结构与查询 |
| `accounts/storetest` | `Store` 契约测试，用于自己实现的存储 |
| `cmd/accountsd` | 支持游客与邮箱账号的独立 HTTP 服务器 |

## 账号、会话与令牌

账号是一个永久的随机 ID。游戏为玩家保存的一切（存档、进度、购买记录）都以这个 ID 为键：绑定邮箱或身份都不会改变它，所以升级后的游客保留自己的数据。

每次登录都返回 `Credentials`：

- `Session` 是设备保存的长期密钥。用 `Refresh` 换取新令牌，用 `SignOut` 吊销。对游客来说，它是回到账号的唯一凭据。
- `Token` 是游戏发给自家服务器的短时效签名令牌，在配置的 `tokenTTL` 后过期。
- `Created` 表示这次登录是否新建了账号。

```go
svc, err := accounts.New(cfg, accounts.Options{
	Store:      store,
	Key:        key, // 来自 token.ParsePrivateKey
	Identities: map[string]accounts.IdentityVerifier{"gamecenter": gameCenter, "steam": steam},
	Logger:     slog.Default(),
})

guest, _ := svc.CreateGuest(ctx)                                  // 立即开玩
_ = svc.LinkEmail(ctx, guest.AccountID, "ada@example.com", pass)  // 升级，ID 不变
again, _ := svc.SignInEmail(ctx, "ada@example.com", pass)         // 第二台设备
fresh, _ := svc.Refresh(ctx, again.Session)                       // 新令牌
```

| 方法 | 作用 |
| --- | --- |
| `CreateGuest` | 新建既无邮箱也无身份的账号（配置了 `disableGuests` 时返回 `ErrGuestsDisabled`） |
| `Register` | 用邮箱和密码新建账号 |
| `LinkEmail` | 为没有邮箱的账号设置邮箱和密码 |
| `SignInEmail` | 用邮箱和密码登录 |
| `SignInIdentity` | 用身份提供方的凭据登录，身份是新的时会新建账号 |
| `LinkIdentity` | 把身份提供方的身份绑定到账号 |
| `Refresh` / `SignOut` | 为会话签发新令牌 / 吊销会话 |
| `Profile` | 账号、邮箱及已绑定的身份；`Profile.Guest` 表示是否为游客 |

错误有 `ErrNotFound`、`ErrTaken`（邮箱或身份属于另一个账号）、`ErrAlreadyLinked`、`ErrInvalidCredentials`（从不说明错的是邮箱、密码还是会话）、`ErrInvalidEmail`、`ErrWeakPassword` 和 `ErrUnknownProvider`。

## 身份

`IdentityVerifier` 把设备拿到的凭据转换成身份提供方给该玩家的稳定标识（subject）：

```go
steam := accounts.IdentityVerifierFunc(func(ctx context.Context, ticket string) (string, error) {
	return steamWebAPI.AuthenticateUserTicket(ctx, ticket) // SteamID
})
```

Game Center 的校验器用 Apple 的证书校验签名身份，Play Games 的校验器兑换服务器授权码，Steam 的校验器通过 Steam Web API 认证会话票据，OAuth 提供方的校验器检查 OpenID Connect ID 令牌。这些都需要各平台的凭据，所以由游戏自己提供校验器，并自选提供方名称（`a-z`、`0-9`、`.`、`_`、`-`，最多 32 个字符）。

绑定身份后，第二台设备可以用 `SignInIdentity` 登录同一账号。每个账号对每个提供方最多绑定一个身份，一个身份只属于一个账号：绑定已属于其他账号的身份会返回 `ErrTaken`。两台设备同时用一个新身份登录，只会得到同一个账号。

校验器的错误可能引用凭据本身，所以服务从不把它返回给调用方，也不写进日志；调用方得到的是 `ErrInvalidCredentials`。

## 在游戏服务器上校验令牌

令牌是用 Ed25519 签名（`"alg": "EdDSA"`）的紧凑 JWT，包含签发方、作为 subject 的账号 ID、受众、签发与过期时间以及随机 ID。游戏服务器从密钥集文件加载服务的公钥，无需网络请求即可校验每个令牌：

```go
data, _ := os.ReadFile("keys.yaml")
ks, err := token.ParseKeySet(data)
v, err := token.NewVerifier(ks)
v.Leeway = 5 * time.Second // 两台机器之间的时钟偏差

claims, err := v.Verify(tok)
if err != nil { /* 拒绝该玩家 */ }
player := claims.Subject // 账号 ID
```

`Verify` 检查签名、签发方、受众和有效期，错误包装 `ErrMalformed`、`ErrUnknownKey`、`ErrSignature`、`ErrIssuer`、`ErrAudience`、`ErrExpired` 或 `ErrNotYetValid`。长于 `MaxLength` 的令牌在做任何处理前就被拒绝。被吊销的会话无法再换取新令牌，但已签发的令牌在过期前仍然有效，所以 `tokenTTL` 要设得短一些。

密钥集文件：

```yaml
version: 1
issuer: https://accounts.example.com
audience: my-game
keys:
  - id: 2317febd373fcbcd
    key: hze+0cYruVRddgjkbHzgCeWWJbEsBkn6K8JNHRDf6Wg=
```

每个 `id` 是公钥 SHA-256 前 8 字节的十六进制，必须与公钥一致。轮换签名密钥时，先把新公钥加在旧公钥旁边，把新文件发给游戏服务器，再让服务切换到新密钥，等旧密钥签发的最后一批令牌过期后删掉旧公钥。把旧公钥也交给服务，即 `Options.KeySet`（或 `accountsd serve -keyset keys.yaml`）：服务会接受用密钥集中任一密钥签名的 bearer 令牌，`GET /v1/keys` 也会先返回签名密钥、再返回这些公钥，所以重启前签发的令牌在服务和游戏服务器上都继续有效，直到旧公钥从文件中删掉。

## 配置

服务读取带版本号的 YAML 文件：

```yaml
version: 1
issuer: https://accounts.example.com
audience: my-game
tokenTTL: 15m
password:
  minLength: 10
  argon2id:
    time: 2
    memoryKiB: 19456
    threads: 1
```

| 字段 | 含义 |
| --- | --- |
| `version` | 结构版本，`1` |
| `issuer` / `audience` | 写入每个令牌，校验器会检查 |
| `tokenTTL` | 令牌有效期，`1s` 到 `24h` 之间的整秒数 |
| `disableGuests` | 拒绝 `CreateGuest`，每个账号都必须从邮箱或身份开始 |
| `password.minLength` | 密码最少字符数，至少 8；密码最长 1024 字节 |
| `password.argon2id` | argon2id 成本：迭代次数、以 KiB 计的内存和并行度 |

`ParseConfig` 拒绝未知字段、多余的文档和越界的值。`Config.Marshal` 写出规范形式：按上表顺序写出字段，时长用最短写法（`15m` 而不是 `15m0s`），`disableGuests` 只在为 true 时写出。规范形式的文件（例如 `Marshal` 或 `accountsd init` 写出的文件）读入再写出不会变化；手写的文件若顺序或写法不同，会被规范化。`DefaultConfig` 返回上面展示的设置。

## 密码与密钥

- 密码只以 PHC 格式的 argon2id 哈希保存（`$argon2id$v=19$m=19456,t=2,p=1$salt$hash`），默认使用 OWASP 推荐的成本。用旧成本哈希的密码在登录时会按当前配置重新哈希。`Options.MaxConcurrentHashes` 限制同时运行的哈希数量，避免一波登录耗尽服务器内存。
- 拒绝一个不存在的邮箱所花的时间与拒绝错误密码相同，所以登录不会暴露某个邮箱是否有账号。`Register` 和 `LinkEmail` 则会暴露：邮箱已属于某个账号时它们返回 `ErrTaken`（`409 taken`），任何人不需要密码就能在这里试探一个邮箱。请按客户端对这些请求限流。
- 会话密钥是 256 位随机数，存储里只保存其 SHA-256。
- `Credentials.Session` 与 `Credentials.Token` 是 `Secret` 值，打印、写日志和序列化时都显示为 `[redacted]`；只有真正要发送它的地方才调用 `Reveal` 取值。服务只记录账号事件（创建、登录、绑定）及账号 ID 和提供方名称，从不记录密码、会话密钥、令牌或身份凭据。
- 签名密钥是以 `0600` 权限写入的 PKCS #8 PEM 文件；Postgres DSN 从环境变量读取，不放在命令行上。

## 存储

`Store` 是服务保存记录的接口。实现必须以原子方式保证唯一性，两台设备抢同一个邮箱或身份时不会都成功。

```go
store, err := sqlitestore.Open(ctx, "accounts.db")                  // 单个服务进程
store, err := pgstore.Open(ctx, os.Getenv("ACCOUNTS_POSTGRES_DSN")) // 多个进程
```

`sqlitestore` 使用纯 Go 的 `modernc.org/sqlite` 驱动，启用预写日志并只用一个连接；`pgstore` 使用 `github.com/jackc/pgx/v5`，建表时持有咨询锁。两者都会在表缺失时创建，并拒绝结构版本比自己新的数据库。`NewMemoryStore` 把一切保存在内存中，用于测试和原型。

自己实现的存储要通过 `storetest.Run`：

```go
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) accounts.Store { return openEmptyStore(t) })
}
```

## HTTP

`Service.Handler` 以 JSON 请求体提供服务，每个响应都带 `Cache-Control: no-store`：

| 请求 | 请求体 | 响应 |
| --- | --- | --- |
| `POST /v1/guest` | | 凭据 |
| `POST /v1/register` | `{"email", "password"}` | 凭据 |
| `POST /v1/signin/email` | `{"email", "password"}` | 凭据 |
| `POST /v1/signin/identity` | `{"provider", "credential"}` | 凭据 |
| `POST /v1/refresh` | `{"session"}` | 凭据 |
| `POST /v1/signout` | `{"session"}` | 204 |
| `POST /v1/link/email` | `{"email", "password"}`，带 bearer 令牌 | 204 |
| `POST /v1/link/identity` | `{"provider", "credential"}`，带 bearer 令牌 | 204 |
| `GET /v1/me` | bearer 令牌 | 账号资料 |
| `GET /v1/keys` | | YAML 格式的密钥集 |

凭据为 `{"account", "session", "token", "expires", "created"}`，新建账号时状态码为 201。错误为 `{"error": code}`：`invalid_credentials` 和 `invalid_token` 为 401，`guests_disabled` 为 403，`not_found` 为 404，`taken` 和 `already_linked` 为 409，`request_too_large` 为 413，`bad_request`、`invalid_email`、`weak_password` 和 `unknown_provider` 为 400，请求被取消或超时（例如高负载下等待密码哈希槽位）时 `unavailable` 为 503，`internal` 为 500。请求体限制为 32 KiB，未知字段会被拒绝。处理器本身不限制请求速率：请放在 TLS 和限流器之后运行。

## accountsd

`cmd/accountsd` 为游客和邮箱账号运行该服务：

```sh
accountsd init   -config accounts.yaml -issuer https://accounts.example.com -audience my-game
accountsd genkey -config accounts.yaml -key key.pem -keyset keys.yaml
accountsd serve  -config accounts.yaml -key key.pem -keyset keys.yaml -sqlite accounts.db -addr :8080
ACCOUNTS_POSTGRES_DSN=postgres://... accountsd serve -config accounts.yaml -key key.pem -keyset keys.yaml -postgres
```

`init` 从不覆盖已有配置，`genkey` 从不覆盖已有密钥。`genkey` 把新公钥加进密钥集文件，这是轮换的第一步；它先写临时文件再改名替换，游戏服务器读取时不会读到写了一半的文件，密钥集写入失败时还会删掉刚生成的密钥。`serve -keyset` 接受并提供密钥集中的所有公钥，所以轮换后用旧密钥签名的令牌在服务上依然有效。`serve` 把 JSON 日志写到 stderr，收到 SIGINT 或 SIGTERM 时停止。需要绑定平台身份的游戏，用自己的校验器围绕 `accounts.New` 构建自己的命令。

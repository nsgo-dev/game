# gameserver

`github.com/nsgo-dev/game/pkg/online/gameserver` 在普通 Go 进程中以固定步长运行游戏更新，不需要 NSGo 应用、窗口、[`core.Scope`](loop.md) 或 cgo。

游戏传给 [`game.Loop`](loop.md) 的 `Update` 函数在服务器上原样运行：两边都用相同的固定 `1/Rate` 步长，所以专用服务器与客户端共享同一套模拟。

## Runner

```go
r := &gameserver.Runner{
	Matches: []*gameserver.Match{
		{Name: "match-1", Rate: 60, Update: update},
		{Name: "match-2", Rate: 30, Update: update},
	},
	OnOverrun: func(m *gameserver.Match, lag time.Duration, missed int) {
		log.Printf("%s 超时 %v（错过 %d tick）", m.Name, lag, missed)
	},
}
if err := r.Run(ctx); err != nil { ... }
```

`Runner.Run` 会阻塞到 context 取消，然后等待每场比赛当前 tick 完成并调用其 `OnStop` 后返回。每场比赛在自己的 goroutine 上按各自速率运行。

## Tick 与超时

每步为 `1/Rate` 秒（`Rate` 为 0 或负数时按 60 Hz）。`Runner` 按固定截止时间调度每次 tick（`next += step`），因此 `Update` 自身的耗时和轻微的定时器抖动会从当步余量中扣除，而不会拖慢 tick 速率。当更新结束时已超过下一次 tick 的截止时间，`Runner` 调用 `OnOverrun` 报告延迟时间与错过的整步数（至少为 1）。错过的 tick **不会**补跑：截止时间会重置为慢更新返回时刻之后的一步，所以更新变慢会拖慢比赛，而不是爆发式补帧。

## 停止

专用服务器通常配合 `signal.NotifyContext`：

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
```

收到 SIGTERM/SIGINT 后 context 结束；runner 让当前 tick 完成，再调用 `OnStop`（例如通知对端、落盘状态）并返回。要让停止有界，`Update` 必须及时返回。

某场比赛的 `Update` 发生 panic 不会拖垮整个进程。runner 会恢复它，通过 `OnPanic` 上报，并且——在默认的 `StopMatch` 动作下——只结束这一场比赛：它不再 tick（状态可能只更新了一半），其 `OnStop` 执行一次，其余比赛继续运行。若返回 `Crash`，则停止所有比赛、执行每个 `OnStop`，待它们全部返回后由 `Run` 重新抛出 panic。`OnStop` 内部的 panic 同样被恢复并上报，此时 `Tick` 为 0。

```go
r := &gameserver.Runner{
	Matches: matches,
	OnPanic: func(p gameserver.Panic) gameserver.PanicAction {
		save(p.Report()) // crashreport.CrashReport：与客户端一致的崩溃报告结构
		return server.StopMatch
	},
}
```

`Panic` 携带所属比赛、recover 到的值、goroutine 栈、从 1 开始的 tick 序号和时间。`Panic.Report()` 构造 [`crashreport.CrashReport`](liveops.md#崩溃报告)，使服务端的 panic 文件与客户端的崩溃文件共用同一套 JSON 结构。

可运行示例位于 `examples/dedicatedserver`：

```sh
go build ./examples/dedicatedserver
```

它用普通 Go 工具链构建，不需要 NSGo 运行时。

数据表同样可以无头加载：[`pkg/datatable`](datatable.md) 不依赖任何 NSGo 包，服务器用 `datatable.LoadFile` 或 `datatable.NewTable` 读取物品、敌人等数据表，方式与客户端完全一致。热重载（`pkg/datatable/datatablewatch`）是只有客户端链接的开发期工具。


## 匹配

`Matchmaker` 按模式和地区排队 `Ticket`，按 `RoomSize` 人组成房间：

```go
mm := gameserver.NewMatchmaker(4)
mm.BaseWindow = 100       // 入队时允许的评分差
mm.WindowGrowthPerSecond = 10
mm.MaxWindow = 400
mm.Enqueue(gameserver.Ticket{ID: 7, Mode: "coop", Region: "eu", Rating: 1200, Party: []string{"ada", "lin"}})
rooms := mm.Match()       // []gameserver.Matchup，含 TicketIDs 与 Players
```

同票队伍整体入房，不会被拆分。默认规则从等待最久的票开始补房：候选票与房内最老票的评分差必须同时落在**双方**当前窗口内——`BaseWindow + WindowGrowthPerSecond * 等待秒数`，并由 `MaxWindow` 封顶。匹配不会超出较新票的窗口；老票窗口随时间扩大后可以匹配后来的玩家。`Cancel(id)` 只移除该票，不影响其他等待者；`Waiting` 按等待时间从旧到新列出队列。

可设置 `Rule` 替换匹配函数；它收到同一模式/地区的一组票、房间大小和 `window` 函数，返回若干 `Matchup`；matchmaker 会移除各房间列出的票。

## 房间

`Rooms` 把 peer 分成相互独立的房间，每场比赛有独立生命周期与各自的收件箱：

```go
rs := gameserver.NewRooms()
rs.EmptyTimeout = 30 * time.Second
rs.Snapshot = func(r *gameserver.Room) []byte { return encodeWorldState(r) }
room, _ := rs.Create(4, true)             // 容量 4，私有并带房间码
join, _ := rs.JoinCode(room.Code(), peer) // 或 rs.Join(room.ID(), peer)
room.Start()                              // epoch 1, tick 0
room.Advance()                            // 运行中 tick 加一
room.End()
room.Start()                              // epoch 2, tick 0：新一场比赛
rs.Leave(room.ID(), peer)
rs.Tick()                                 // 关闭空置超过 EmptyTimeout 的房间
```

- 房间的 `Deliver`/`Recv` 只接受和暴露本房间成员的包，所以两个房间互不可见。
- 对运行中房间 `Join` 会调用 `Snapshot`，连同当前 epoch 与 tick 返回；加入者先应用完整状态，再处理增量。
- `End` 后 `Start` 会推进 epoch 并清零 tick，客户端无需等待计数器越过旧值即可丢弃上一场比赛的 tick 状态。
- 私有房间的 `Code` 可由 `JoinCode` 使用；满员返回 `ErrRoomFull`，未知房间码返回 `ErrRoomNotFound`。
- 空房间（无论是从未有人加入，还是最后一个成员已经离开）会在空置满 `EmptyTimeout` 后于 `Tick` 运行时关闭，并释放其 ID 与私有房间码。

## 速率限制与输入校验

`Guard` 在包进入房间或模拟前执行每个 peer 的策略：

```go
g := gameserver.NewGuard(gameserver.Limits{
	PacketsPerSecond:   60,
	BytesPerSecond:     64 << 10,
	MaxMessageSize:     8 << 10,
	MaxPendingReliable: 256 << 10,
}, gameserver.LimitDisconnect)
g.Validate = func(peer netplay.PeerID, p netplay.Packet) error { return checkInput(p) }
action, err := g.Check(peer, packet)
if action == server.GuardDisconnect {
	_ = srv.Kick(peer, err.Error()) // 彻底结束传输层会话
}
```

- 包与字节使用令牌桶，可配 burst；`LimitThrottle`/`LimitDrop` 拒绝该包但保持连接，`LimitDisconnect` 之后拒绝该 peer（返回 `GuardDisconnect`）。`GuardDisconnect` 只翻转 guard 自己的标志，因此要配合 `WebSocketServer` 或 `UDPServer` 的 `Kick` 来彻底移除会话并告知客户端原因（见 [netplay 指南](netplay.md#连接握手与重连)）。
- `MaxMessageSize` 在缓冲前拒绝超大包；`ReserveReliable`/`ReleaseReliable` 限制待处理可靠字节。
- `Validate` 只对已通过的包运行；返回非 nil 错误时丢弃该包、返回 `ErrInvalidInput`、累加该 peer 的 `Violations`，且绝不会进入模拟。
- `Stats(peer)` 返回该 peer 的包数、字节数、丢弃数、违规数与待处理可靠字节；`Reset` 用于重连 peer。状态按 peer 隔离、检查 O(1)，不影响其他 peer 的 tick。

`Guard` 与传输无关：在收包循环调用 `Check`，按返回的 `GuardAction` 处理即可。

## 审计日志

`audit` 以 JSON Lines 追加记录服务器的管理与安全相关事件——踢人、会话顶替、运维命令、破坏性路由——每个 UTC 日一个文件：

```go
log, err := audit.Open(audit.Options{Dir: "audit", Retain: 30})
log.Log(audit.Record{Actor: "admin:ada", Action: "peer.kick", Subject: "peer:42", Data: map[string]any{"reason": "cheating"}})
// 停止时，在 runner 排空之后：
log.Close()
```

- 每行一条记录：`{"v":1,"time":"2026-11-28T09:15:02.500Z","actor":"admin:ada","action":"peer.kick","subject":"peer:42","data":{"reason":"cheating"}}`；模式由每行的 `v` 标记版本，见[文件格式](formats.md)。`Log` 会补全 `v`、当前 `time`，并把空的 `actor` 写成 `"system"`；记录按自身 UTC 日期落盘，所以补录的一批记录会落在事情发生的那几天的文件里。
- 文件为 `<Dir>/<yyyy-mm-dd>.jsonl`，以只追加方式打开、从不重写，在 UTC 零点滚动：可 grep、可 diff、可直接送进日志系统，无需数据库。
- `Log` 从不阻塞 tick：记录在 `Queue`（默认 4096）里缓冲，队列满时丢弃并计数。`Dropped()` 返回累计丢弃数，值得导出到 `/metrics`。一个后台 goroutine 批量追加并 fsync；`Close` 冲刷剩余记录并返回第一个写入错误。
- `Retain` 从正在写入的日期往前保留相应天数的文件，滚动时删除更早的；为 0 时全部保留。

## 点对点中继

锁步与回滚会话是点对点的，但位于 NAT 之后或运行在浏览器里的玩家无法互相直连。`netplay.Relay` 负责转发他们的数据包：每个玩家用 `netplay.RelayTransport` 连接中继，中继把每一帧交给该玩家所在会话中被寻址的成员。

```go
relay, err := netplay.NewRelay(netplay.RelayConfig{
	// 校验玩家的令牌并给出它加入的对局；返回错误的文本即拒绝原因。
	Join: func(peer netplay.PeerID, token []byte) (string, error) {
		return matchOf(token)
	},
	MaxSessionPeers: 2,
})
auth := relay.Auth(netplay.Auth{GracePeriod: 15 * time.Second})
srv, err := netplay.ListenWebSocketTLSFiles(":443", 1, auth, "cert.pem", "key.pem", nil) // 或 netplay.ListenUDPServer(":7777", 1, auth)
go relay.Serve(ctx, srv)
```

```go
cli, err := netplay.DialWebSocket("wss://relay.example.com/", token) // 或 netplay.DialUDP(addr, serverPubKey, token)
peerTx := netplay.NewRelayTransport(cli, netplay.RelayTransportConfig{})
// 等 peerTx.Peers() 列出对手之后：
session := netplay.NewRollbackSession(netplay.RollbackConfig{Transport: peerTx, Sim: sim})
```

- 中继运行在 `WebSocketServer`、`WebTransportServer` 或 `UDPServer` 之上，因此每个玩家都要先通过[连接握手](netplay.md#连接握手与重连)。`relay.Auth` 先运行基础 `Auth.Verify`（如果设置了），再运行 `Join`；会话名为空时以 `no session` 拒绝，会话已有 `MaxSessionPeers`（默认 8）名成员时以 `session full` 拒绝。
- 帧只在同一会话的成员之间转发。发往其他会话成员、发往中继本身、发给发送者自己或格式错误的帧会被丢弃，并计入 `Stats().Dropped`；`RelayTransport.Send` 对会话外的 ID 返回 `ErrUnknownPeer`。
- `Peers` 列出其他成员，包括处于宽限期内的成员，因此会话不会因为某个玩家正在重连而失去他。中继每秒重发一次成员列表，在 UDP 上丢失了更新的客户端也能跟上。
- 对局进行中加入中继会话的玩家，在对局主机用 `AddPeer` 接纳之后参与对局；离开的玩家在主机调用 `RemovePeer` 之后不再被等待（见 [netplay 指南](netplay.md#对局中途加入与离开的玩家)）。
- `ChannelReliableOrdered` 端到端可靠：每条消息按对端编号，接收方确认（并选择性确认其后 64 条），发送方在根据实测往返时间调整的超时之后重发缺失的消息。它在 UDP 上同样有效；玩家用内层传输的 `Reconnect` 重连期间中继没能送达的消息，会在玩家回来之后到达。每个对端最多有 1024 条消息等待确认，超过时 `Send` 返回 `ErrRelayBacklog`。对于在某条缺失消息之后先到的消息，接收方为每个对端最多保留 4 MB，其余的丢弃，等待发送方重发。`ChannelUnreliable` 的包只发送一次。
- 当内层传输的 `Reconnect` 因宽限期已过或中继重启而把玩家作为新对端接纳、而不是恢复会话时，`RelayTransport` 会从头开始：即使中继再次分配了相同的 ID，它也会忘掉旧的成员和未确认的消息，并等待新会话的成员列表。
- 确认与重发在 `Recv` 内进行，会话每个 tick 都会通过 `Poll` 调用它。
- 中继从不读取负载。它为每个成员在独立的 goroutine 上写出，经由容量为 `QueueSize`（默认 256）帧的队列，因此慢速玩家只拖慢自己；超出队列的帧会被丢弃，其中的可靠消息由发送方重发。
- 一个中继在其整个生命周期内只服务一个服务端，因此 WebSocket 玩家与 UDP 玩家不能同处一个会话；每种协议各运行一个中继。`Serve` 返回后可以在同一个服务端上再次调用，仍然连接着的玩家保留各自的会话。负载最多 `MaxRelayPayload` 字节。
- 客户端到中继的 UDP 链路通过 Noise NK 与 ChaCha20-Poly1305 认证加密（见 [netplay 指南](netplay.md#连接握手与重连)）；WebSocket 链路请使用 `wss://`。中继本身仍能看到转发的负载，中继对等端之间的端到端加密不在范围内。

### 中继延迟

`game/online/netplay` 中的 `BenchmarkRelayRoundTrip` 在同一台机器上经回环接口测量 48 字节消息的往返时间。“直连”是客户端与其服务端，单程一跳；“经中继”是两个 `RelayTransport` 经本地中继通信，单程两跳，并像回滚一样使用可靠通道发送：

| 路径 | Linux x86-64，24 核 | macOS arm64，10 核 |
| --- | --- | --- |
| UDP 直连 | 63 µs | 34 µs |
| UDP 经中继 | 188 µs | 94 µs |
| WebSocket 直连 | 44 µs | 33 µs |
| WebSocket 经中继 | 187 µs | 87 µs |

本地中继让往返时间增加约 55 到 145 µs，远低于一毫秒，也不到 60 Hz 一个 tick 的十分之一。在互联网上，中继的代价是绕行带来的额外网络距离，因此请把中继部署在玩家所在的地区。用以下命令复现这些数字：

```sh
go tool nsgo test ./net -run '^$' -bench BenchmarkRelayRoundTrip
```

### WebRTC 数据通道

决定：`game/online/netplay` 目前不实现带 STUN 的 WebRTC 数据通道；所有点对点会话都走中继。

- WebRTC 需要 ICE（含 STUN 与 TURN）、DTLS 和 SCTP。浏览器通过 `RTCPeerConnection` 提供这些能力，但原生平台需要全部用 Go 实现；NSGo 没有暴露平台的 WebRTC 栈，而唯一完整的 Go 实现是一个庞大的第三方栈，每个原生构建都得带上它。
- 中继无论如何都需要：STUN 在移动运营商常见的对称型 NAT 后会失败，而 TURN 本身就是中继。信令同样需要服务端。
- 回滚的流量很小，就近部署的中继增加的延迟也很少（见上文）。

当测量表明玩家彼此相近却远离所有中继地区，或 NSGo 在原生平台暴露了 WebRTC 时，再重新评估。届时的设计是在同一 `Transport` 接口之后提供一个 WebRTC 传输：先用 STUN 尝试直连，失败时回退到 `RelayTransport`，会话本身无需改动。

## TLS、Origin、健康检查与指标

HTTPS 页面里的浏览器需要 `wss://`（WebSocket）或 HTTP/3 QUIC 上的 `https://`（WebTransport）。`netplay.ListenWebSocketTLS` 用 TLS 包装 WebSocket 监听并在升级时检查浏览器 Origin，`ListenWebSocketTLSFiles` 直接加载 PEM 证书与私钥，`ListenWebTransport` 与 `ListenWebTransportFiles` 则在 QUIC/HTTP/3 上提供同样的 Origin 校验策略。它们都接受与 `ListenWebSocket` 相同的 `Auth`（见[连接握手与重连](netplay.md#连接握手与重连)）：

```go
srv, err := netplay.ListenWebSocketTLSFiles(":443", 1, auth, "cert.pem", "key.pem",
	[]string{"https://game.example.com"})
```

不在允许列表的 `Origin` 会在 WebSocket 握手前被 `403` 拒绝；允许列表为空表示接受任意 Origin，`"*"` 也接受全部，而原生客户端不带 Origin 时同样允许。每个条目是 `scheme://host` 形式的 Origin，例如 `https://game.example.com`；末尾的斜杠会被忽略，而带路径或查询参数的条目会让监听在启动时失败。

`gameserver.Health` 提供部署端点：

```go
health := gameserver.NewHealth(func() bool { return worldReady() }, func() gameserver.Metrics {
	return gameserver.Metrics{Peers: peers, Rooms: rooms, BytesIn: in, BytesOut: out}
})
http.ListenAndServe(":8080", health)
```

- `GET /healthz`：进程存活；
- `GET /readyz`：`Ready` 为 true 时 200，否则 503；
- `GET /metrics`：Prometheus 文本格式指标（见下表）。

每次服务器 tick 用 `health.ObserveTick(time.Since(start))` 记录耗时。需要按对局（match）统计时，给 runner 设置 `Health` 字段，它就会自动记录每个对局的 tick 耗时；再用 `health.OnOverrun` 包装 runner 的 overrun 回调，即可按对局统计超时与错过的 tick 数；未命名的对局以 `match_<index>` 上报。`health.AddCollector` 在每次抓取时追加更多序列：`gameserver.GuardCollector(guard)` 导出 guard 在所有见过的 peer 上的合计（`Guard.Totals` 在 `Reset` 后仍保留该 peer 的计数），`gameserver.InboxCollector(name, transport)` 导出传输层收件箱的丢弃与拒绝计数，任意 `func(io.Writer)` 都可以追加游戏自己的序列。

| 序列 | 类型 | 标签 | 含义 |
| --- | --- | --- | --- |
| `nsgo_server_peers` | gauge | | 连接到本服务器的 peer 数 |
| `nsgo_server_rooms` | gauge | | 开放的房间数 |
| `nsgo_server_tick_duration_seconds` | gauge | | 上一次定步 tick 的耗时 |
| `nsgo_server_bytes_in_total`、`nsgo_server_bytes_out_total` | counter | | 收到与发送的字节数 |
| `nsgo_server_match_tick_seconds` | histogram | `match` | 每个对局的 tick 耗时，桶从 0.5 ms 到 100 ms |
| `nsgo_server_match_overruns_total` | counter | `match` | 超过下一个 tick 截止时间才完成的 tick 数 |
| `nsgo_server_match_missed_ticks_total` | counter | `match` | 这些超时总共覆盖的完整 tick 间隔数 |
| `nsgo_server_guard_packets_total`、`nsgo_server_guard_bytes_total`、`nsgo_server_guard_dropped_total`、`nsgo_server_guard_violations_total` | counter | | guard 在所有 peer（含已遗忘）上的合计 |
| `nsgo_net_inbox_unreliable_dropped_total`、`nsgo_net_inbox_reliable_refused_total`、`nsgo_net_inbox_reliable_warnings_total` | counter | `transport` | 传输层收件箱的丢弃、拒绝与积压告警计数 |

### 容器与优雅退出

把示例构建成静态、无 cgo 的二进制并以小容器运行：

```dockerfile
FROM golang:1.27 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go -C examples/dedicatedserver build -o /out/server .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
```

使用 `SIGTERM` 作为停止信号（`docker stop` 默认发送 SIGTERM）。示例用 `signal.NotifyContext`；`Runner.Run` 会等当前 tick 完成、调用每场比赛的 `OnStop` 通知对端并落盘后退出，因此容器可以优雅排空而不是丢掉一个 tick。

## 玩家令牌

专用服务器通过玩家设备从[账号服务](accounts.md)拿到的令牌识别玩家。它只需加载一次服务的密钥集文件，再用 `accounts/authtoken` 离线校验每个令牌；该包不会链接账号服务的密码哈希或数据库驱动：

```go
ks, err := authtoken.ParseKeySet(keysYAML)
v, err := authtoken.NewVerifier(ks)
claims, err := v.Verify(tok) // claims.Subject 是账号 ID
```

玩家发来的令牌属于游戏服务器，即密钥集的受众；运营服务器的人在它过期前都可以重放它。云存档、排行榜和账号服务都拒绝这种令牌，所以服务器无法用它触及玩家的存档、成绩或账号（见[令牌受众](accounts.md#令牌受众)）。不要把玩家的令牌转发给其他服务，也不要向玩家索要其他受众的令牌或会话。

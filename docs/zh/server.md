# server

`github.com/nsgo-dev/game/packages/game/server` 在普通 Go 进程中以固定步长运行游戏更新，不需要 NSGo 应用、窗口、[`core.Scope`](loop.md) 或 cgo。

游戏传给 [`game.Loop`](loop.md) 的 `Update` 函数在服务器上原样运行：两边都用相同的固定 `1/Rate` 步长，所以专用服务器与客户端共享同一套模拟。

## Runner

```go
r := &server.Runner{
	Matches: []*server.Match{
		{Name: "match-1", Rate: 60, Update: update},
		{Name: "match-2", Rate: 30, Update: update},
	},
	OnOverrun: func(m *server.Match, lag time.Duration, missed int) {
		log.Printf("%s 超时 %v（错过 %d tick）", m.Name, lag, missed)
	},
}
if err := r.Run(ctx); err != nil { ... }
```

`Runner.Run` 会阻塞到 context 取消，然后等待每场比赛当前 tick 完成并调用其 `OnStop` 后返回。每场比赛在自己的 goroutine 上按各自速率运行。

## Tick 与超时

每步为 `1/Rate` 秒（`Rate` 为 0 或负数时按 60 Hz）。`Update` 超过一步时，`Runner` 调用 `OnOverrun` 报告延迟时间与错过的整步数。错过的 tick **不会**补跑：下一次 tick 从慢更新返回的时刻开始计时，所以更新变慢会拖慢比赛，而不是爆发式补帧。

## 停止

专用服务器通常配合 `signal.NotifyContext`：

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
```

收到 SIGTERM/SIGINT 后 context 结束；runner 让当前 tick 完成，再调用 `OnStop`（例如通知对端、落盘状态）并返回。要让停止有界，`Update` 必须及时返回。

可运行示例位于 `server/cmd/dedicatedserver`：

```sh
go build ./server/cmd/dedicatedserver
```

它用普通 Go 工具链构建，不需要 NSGo 运行时。


## 匹配

`Matchmaker` 按模式和地区排队 `Ticket`，按 `RoomSize` 人组成房间：

```go
mm := server.NewMatchmaker(4)
mm.BaseWindow = 100       // 入队时允许的评分差
mm.WindowGrowthPerSecond = 10
mm.MaxWindow = 400
mm.Enqueue(server.Ticket{ID: 7, Mode: "coop", Region: "eu", Rating: 1200, Party: []string{"ada", "lin"}})
rooms := mm.Match()       // []server.Matchup，含 TicketIDs 与 Players
```

同票队伍整体入房，不会被拆分。默认规则从等待最久的票开始补房：候选票与房内最老票的评分差必须同时落在**双方**当前窗口内——`BaseWindow + WindowGrowthPerSecond * 等待秒数`，并由 `MaxWindow` 封顶。匹配不会超出较新票的窗口；老票窗口随时间扩大后可以匹配后来的玩家。`Cancel(id)` 只移除该票，不影响其他等待者；`Waiting` 按等待时间从旧到新列出队列。

可设置 `Rule` 替换匹配函数；它收到同一模式/地区的一组票、房间大小和 `window` 函数，返回若干 `Matchup`；matchmaker 会移除各房间列出的票。

## 房间

`Rooms` 把 peer 分成相互独立的房间，每场比赛有独立生命周期与各自的收件箱：

```go
rs := server.NewRooms()
rs.EmptyTimeout = 30 * time.Second
rs.Snapshot = func(r *server.Room) []byte { return encodeWorldState(r) }
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
- 最后一个成员离开后，房间在 `Tick` 运行且空置超过 `EmptyTimeout` 时关闭。

## 速率限制与输入校验

`Guard` 在包进入房间或模拟前执行每个 peer 的策略：

```go
g := server.NewGuard(server.Limits{
	PacketsPerSecond:   60,
	BytesPerSecond:     64 << 10,
	MaxMessageSize:     8 << 10,
	MaxPendingReliable: 256 << 10,
}, server.LimitDisconnect)
g.Validate = func(peer net.PeerID, p net.Packet) error { return checkInput(p) }
action, err := g.Check(peer, packet)
```

- 包与字节使用令牌桶，可配 burst；`LimitThrottle`/`LimitDrop` 拒绝该包但保持连接，`LimitDisconnect` 之后拒绝该 peer（返回 `GuardDisconnect`）。
- `MaxMessageSize` 在缓冲前拒绝超大包；`ReserveReliable`/`ReleaseReliable` 限制待处理可靠字节。
- `Validate` 只对已通过的包运行；返回非 nil 错误时丢弃该包、返回 `ErrInvalidInput`、累加该 peer 的 `Violations`，且绝不会进入模拟。
- `Stats(peer)` 返回该 peer 的包数、字节数、丢弃数、违规数与待处理可靠字节；`Reset` 用于重连 peer。状态按 peer 隔离、检查 O(1)，不影响其他 peer 的 tick。

`Guard` 与传输无关：在收包循环调用 `Check`，按返回的 `GuardAction` 处理即可。

## 点对点中继

锁步与回滚会话是点对点的，但位于 NAT 之后或运行在浏览器里的玩家无法互相直连。`net.Relay` 负责转发他们的数据包：每个玩家用 `net.RelayTransport` 连接中继，中继把每一帧交给该玩家所在会话中被寻址的成员。

```go
relay, err := gamenet.NewRelay(gamenet.RelayConfig{
	// 校验玩家的令牌并给出它加入的对局；返回错误的文本即拒绝原因。
	Join: func(peer gamenet.PeerID, token []byte) (string, error) {
		return matchOf(token)
	},
	MaxSessionPeers: 2,
})
auth := relay.Auth(gamenet.Auth{GracePeriod: 15 * time.Second})
srv, err := gamenet.ListenWebSocketTLSFiles(":443", 1, auth, "cert.pem", "key.pem", nil) // 或 gamenet.ListenUDPServer(":7777", 1, auth)
go relay.Serve(ctx, srv)
```

```go
cli, err := gamenet.DialWebSocket("wss://relay.example.com/", token) // 或 gamenet.DialUDP(addr, token)
peerTx := gamenet.NewRelayTransport(cli, gamenet.RelayTransportConfig{})
// 等 peerTx.Peers() 列出对手之后：
session := gamenet.NewRollbackSession(gamenet.RollbackConfig{Transport: peerTx, Sim: sim})
```

- 中继运行在 `WebSocketServer` 或 `UDPServer` 之上，因此每个玩家都要先通过[连接握手](net.md#连接握手与重连)。`relay.Auth` 先运行基础 `Auth.Verify`（如果设置了），再运行 `Join`；会话名为空时以 `no session` 拒绝，会话已有 `MaxSessionPeers`（默认 8）名成员时以 `session full` 拒绝。
- 帧只在同一会话的成员之间转发。发往其他会话成员、发往中继本身、发给发送者自己或格式错误的帧会被丢弃，并计入 `Stats().Dropped`；`RelayTransport.Send` 对会话外的 ID 返回 `ErrUnknownPeer`。
- `Peers` 列出其他成员，包括处于宽限期内的成员，因此会话不会因为某个玩家正在重连而失去他。中继每秒重发一次成员列表，在 UDP 上丢失了更新的客户端也能跟上。
- `ChannelReliableOrdered` 端到端可靠：每条消息按对端编号，接收方确认（并选择性确认其后 64 条），发送方在根据实测往返时间调整的超时之后重发缺失的消息。它在 UDP 上同样有效；玩家用内层传输的 `Reconnect` 重连期间中继没能送达的消息，会在玩家回来之后到达。每个对端最多有 1024 条消息等待确认，超过时 `Send` 返回 `ErrRelayBacklog`。对于在某条缺失消息之后先到的消息，接收方为每个对端最多保留 4 MB，其余的丢弃，等待发送方重发。`ChannelUnreliable` 的包只发送一次。
- 当内层传输的 `Reconnect` 因宽限期已过或中继重启而把玩家作为新对端接纳、而不是恢复会话时，`RelayTransport` 会从头开始：即使中继再次分配了相同的 ID，它也会忘掉旧的成员和未确认的消息，并等待新会话的成员列表。
- 确认与重发在 `Recv` 内进行，会话每个 tick 都会通过 `Poll` 调用它。
- 中继从不读取负载。它为每个成员在独立的 goroutine 上写出，经由容量为 `QueueSize`（默认 256）帧的队列，因此慢速玩家只拖慢自己；超出队列的帧会被丢弃，其中的可靠消息由发送方重发。
- 一个中继在其整个生命周期内只服务一个服务端，因此 WebSocket 玩家与 UDP 玩家不能同处一个会话；每种协议各运行一个中继。`Serve` 返回后可以在同一个服务端上再次调用，仍然连接着的玩家保留各自的会话。负载最多 `MaxRelayPayload` 字节。
- 除非连接使用 `wss://`，中继能看到明文负载；[net 指南](net.md#连接握手与重连)中关于 UDP 握手的注意事项同样适用。

### 中继延迟

`game/net` 中的 `BenchmarkRelayRoundTrip` 在同一台机器上经回环接口测量 48 字节消息的往返时间。“直连”是客户端与其服务端，单程一跳；“经中继”是两个 `RelayTransport` 经本地中继通信，单程两跳，并像回滚一样使用可靠通道发送：

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

决定：`game/net` 目前不实现带 STUN 的 WebRTC 数据通道；所有点对点会话都走中继。

- WebRTC 需要 ICE（含 STUN 与 TURN）、DTLS 和 SCTP。浏览器通过 `RTCPeerConnection` 提供这些能力，但原生平台需要全部用 Go 实现；NSGo 没有暴露平台的 WebRTC 栈，而唯一完整的 Go 实现是一个庞大的第三方栈，每个原生构建都得带上它。
- 中继无论如何都需要：STUN 在移动运营商常见的对称型 NAT 后会失败，而 TURN 本身就是中继。信令同样需要服务端。
- 回滚的流量很小，就近部署的中继增加的延迟也很少（见上文）。

当测量表明玩家彼此相近却远离所有中继地区，或 NSGo 在原生平台暴露了 WebRTC 时，再重新评估。届时的设计是在同一 `Transport` 接口之后提供一个 WebRTC 传输：先用 STUN 尝试直连，失败时回退到 `RelayTransport`，会话本身无需改动。

## TLS、Origin、健康检查与指标

HTTPS 页面里的浏览器需要 `wss://`。`net.ListenWebSocketTLS` 用 TLS 包装 WebSocket 监听，并在升级时检查浏览器 Origin；`ListenWebSocketTLSFiles` 直接加载 PEM 证书与私钥。两者都接受与 `ListenWebSocket` 相同的 `Auth`（见[连接握手与重连](net.md#连接握手与重连)）：

```go
srv, err := gamenet.ListenWebSocketTLSFiles(":443", 1, auth, "cert.pem", "key.pem",
	[]string{"https://game.example.com"})
```

不在允许列表的 `Origin` 会在 WebSocket 握手前被 `403` 拒绝；允许列表为空表示接受任意 Origin，`"*"` 也接受全部，而原生客户端不带 Origin 时同样允许。每个条目是 `scheme://host` 形式的 Origin，例如 `https://game.example.com`；末尾的斜杠会被忽略，而带路径或查询参数的条目会让监听在启动时失败。

`server.Health` 提供部署端点：

```go
health := server.NewHealth(func() bool { return worldReady() }, func() server.Metrics {
	return server.Metrics{Peers: peers, Rooms: rooms, BytesIn: in, BytesOut: out}
})
http.ListenAndServe(":8080", health)
```

- `GET /healthz`：进程存活；
- `GET /readyz`：`Ready` 为 true 时 200，否则 503；
- `GET /metrics`：Prometheus 的 peers、rooms、上一 tick 耗时与字节计数。

每次服务器 tick 用 `health.ObserveTick(time.Since(start))` 记录耗时。

### 容器与优雅退出

把示例构建成静态、无 cgo 的二进制并以小容器运行：

```dockerfile
FROM golang:1.27 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./packages/game/server/cmd/dedicatedserver

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
```

使用 `SIGTERM` 作为停止信号（`docker stop` 默认发送 SIGTERM）。示例用 `signal.NotifyContext`；`Runner.Run` 会等当前 tick 完成、调用每场比赛的 `OnStop` 通知对端并落盘后退出，因此容器可以优雅排空而不是丢掉一个 tick。

## 玩家令牌

专用服务器通过玩家设备从[账号服务](accounts.md)拿到的令牌识别玩家。它只需加载一次服务的密钥集文件，再用 `accounts/token` 离线校验每个令牌；该包不会链接账号服务的密码哈希或数据库驱动：

```go
ks, err := token.ParseKeySet(keysYAML)
v, err := token.NewVerifier(ks)
claims, err := v.Verify(tok) // claims.Subject 是账号 ID
```

# net

`game/net` 为 NSGo 游戏提供跨平台多人联机能力：横跨 WebSocket（原生 + WebAssembly）、原生 UDP 与仿真回环的统一 `Transport` 接口；带客户端预测与插值的服务端权威增量状态复制；以及确定性锁步（lockstep）与回滚（rollback）网络同步。

```go
import "github.com/nsgo-dev/game/packages/game/net"
```

## 跨平台传输层（`Transport`）

所有传输实现共用同一套非阻塞 `Transport` 接口（`Send`、`Recv`、`LocalID`、`Peers`、`Close`），支持 `ChannelReliableOrdered` 与 `ChannelUnreliable` 通道以及广播地址 `PeerBroadcast`（`PeerID(0)`）：

| 传输实现 | 构造函数 | 适用平台与场景 |
| --- | --- | --- |
| `WebSocketServer` / `WebSocketTransport` | `ListenWebSocket`、`DialWebSocket` | 原生 + 浏览器（`js/wasm`），适合 Web 构建与跨 NAT 联机。客户端用令牌认证，ID 由服务端分配（见下文）。 |
| `UDPServer` / `UDPClient` | `ListenUDPServer`、`DialUDP` | 原生平台（桌面/移动）与服务端之间的低延迟数据报通信，握手与 WebSocket 相同，每个数据报都绑定到其会话。 |
| `UDPTransport` | `ListenUDP` + `AddPeer` | 原生平台点对点数据报，双方事先知道彼此地址：只接受来自其所声称 ID 已登记地址的数据报。 |
| `Relay` / `RelayTransport` | `NewRelay`、`NewRelayTransport` | 彼此无法直连的玩家之间的点对点会话：每人通过 WebSocket 或 UDP 连接中继，中继只在同一会话的成员之间转发（见[服务端指南](server.md#点对点中继)）。 |
| `LoopbackHub` / `LoopbackTransport` | `NewLoopbackHub` + `AddPeer` | 内存内确定性测试，可配置 `Latency`、`Jitter`、`LossRate` 与 `Seed`。 |

UDP 没有重传与排序保证，因此两种 UDP 传输都会以 `ErrUnsupportedChannel` 拒绝 `ChannelReliableOrdered`；锁步与回滚需要 WebSocket 或回环传输，或者 `RelayTransport`：它做端到端重传，因此在 UDP 上也能运行锁步与回滚。

一个传输只能有一个消费者。`Recv` 把每个包只交给一个调用方，而 `ServerWorld`、`ClientWorld`、锁步与回滚会话、`Relay` 和 `RelayTransport` 都会取空所有等待中的包并丢弃不属于自己的包，所以两个消费者共用一个传输会互相抢走对方的包。请为每个消费者准备各自的传输，或在它们前面放一个分流器，为每个消费者各实现一个 `Transport`。无论队列中有多少包，`Recv` 的开销都相同，因此取空一批突发的包仍是线性的。不可靠通道的收件队列（`UDPTransport`、`UDPServer`、`UDPClient` 以及回环、中继和浏览器 WebSocket 上的 `ChannelUnreliable`）与服务端 peer 事件队列均以 4096 条未读项为上限（满时淘汰最旧条目），防止未轮询的消费者或洪泛对端无限增长内存；原生 `WebSocketTransport` 与 `WebSocketServer` 在缓冲满 4096 个未读包后暂停读取，借助 TCP 流控施加背压而不丢弃可靠帧。仍应在每一帧取空每个传输。

`UDPTransport` 或 `UDPServer` 底层的套接字出错时，`Err` 返回包装了套接字错误的 `ErrReceiveFailed`，`Send` 也返回该错误；此时应关闭传输并重新监听；`Close` 之后 `Err` 返回 nil。只涉及单个数据报的错误（例如之前某次发送引起的 ICMP 端口不可达）不会使其停止。`UDPClient` 同样会在 ICMP 错误之后继续读取（例如服务端正在重启时），由空闲超时来判断服务端是否已经消失。

```go
hub := net.NewLoopbackHub(net.LoopbackConfig{
	Latency:  40 * time.Millisecond,
	Jitter:   10 * time.Millisecond,
	LossRate: 0.02,
	Seed:     7,
})
serverTx := hub.AddPeer(1)
clientTx := hub.AddPeer(2)
_ = serverTx.Send(2, net.ChannelReliableOrdered, []byte("hello"))
hub.Advance(60 * time.Millisecond)
pkt, ok := clientTx.Recv()
_, _ = pkt, ok
```

## 连接握手与重连

`WebSocketServer` 和 `UDPServer` 只在握手之后接纳客户端：客户端出示应用自定义的令牌，由 `Auth.Verify` 校验，服务端分配该客户端的 `PeerID` 并回传给它，或附带原因拒绝。被拒绝的客户端永远不会出现在 `Recv` 或 `Peers` 中。

```go
auth := net.Auth{
	// Check a signed token locally; the error text is sent to the client.
	Verify: func(peer net.PeerID, token []byte) error {
		if !validToken(token) {
			return errors.New("invalid token")
		}
		return nil
	},
	GracePeriod: 15 * time.Second, // a dropped client keeps its ID this long
}
srv, _ := net.ListenWebSocket(":8080", 1, auth) // or net.ListenUDPServer(":7777", 1, auth)

// Each server tick:
for ev, ok := srv.NextEvent(); ok; ev, ok = srv.NextEvent() {
	switch ev.Kind {
	case net.PeerJoined: // new ID
	case net.PeerDropped: // connection lost; ID and state kept for GracePeriod
	case net.PeerResumed: // same ID back within GracePeriod
	case net.PeerLeft: // closed, or GracePeriod ran out
		rooms.Leave(roomOf(ev.Peer), ev.Peer)
		guard.Reset(ev.Peer)
	}
}
```

```go
cli, err := net.DialWebSocket("wss://play.example.com/game", token) // or net.DialUDP(addr, token)
if errors.Is(err, net.ErrRejected) {
	// err is a *net.RejectError carrying the server's reason
}
me := cli.LocalID() // assigned by the server

// After a network drop (State() == net.ConnDisconnected):
_ = cli.Reconnect() // cli.Resumed() reports whether it kept its ID
```

- `NextEvent` 在被调用时检查宽限期与空闲超时，因此服务端应在每个 tick 与 `Recv` 一起轮询它。peer 处于宽限期内时，向它 `Send` 会返回 `ErrDisconnected`，`Peers` 也不会列出它。
- 客户端调用 `Close` 即为主动告别：服务端立即报告 `PeerLeft`，不再为其保留会话。`Reconnect` 则不算告别，即使连接仍处于打开状态也是如此：浏览器关闭旧连接时会带上一个关闭码，服务端据此将其视为掉线。
- 服务端最多保留 `Auth.MaxPeers` 个会话（默认 1024），处于宽限期内的 peer 也计算在内；超出后客户端会以原因 `server full` 被拒绝。不设置 `Verify` 时任何人都能加入，因此可从互联网访问的服务端务必设置它。
- 恢复会话需要出示上一次欢迎消息中的密钥，服务端每次握手都会更换该密钥；仅凭 ID 无法接管会话。超过宽限期后，只要令牌仍然有效，`Reconnect` 会作为新 peer 重新加入。
- 在浏览器中 `DialWebSocket` 会立即返回：欢迎消息到达之前 `State` 为 `ConnConnecting`、`LocalID` 为 0，此间发送的包会排队。被拒绝时转为 `ConnDisconnected`，`Err` 返回 `*RejectError`。
- `WebSocketServer` 每隔 `Auth.IdleTimeout` 的三分之一（默认 10 秒，最少 3 秒）向每个客户端发送一次 ping，在这段时间内没有收到客户端的任何字节（连 pong 都没有）就会把它判为掉线，这样网络消失却没有关闭 TCP 连接的客户端不会永远占着会话和 ID。浏览器会自动回应 ping。原生 `WebSocketTransport` 每 2 秒向服务端发送一次 ping，10 秒内没有收到任何字节就转为 `ConnDisconnected`。只要字节持续到达，慢慢传来的大帧也会被完整读取。这些超时使用墙上时钟，而不是 `Auth.Now`。`WebSocketServer.Close` 会同时向所有客户端发送关闭帧，对不读取数据的客户端最多等待一秒。
- WebSocket 帧的负载按实际到达的字节读取，而不是按帧头声明的长度预先分配，握手请求也不能超过合法请求的长度，因此客户端声明一个很大的帧后再慢慢发送，也无法占用它尚未发送的内存。
- UDP 握手之后，每个客户端都绑定到完成握手时的地址和一把会话密钥：每个数据报携带截断的 HMAC-SHA256 标签和一个按重放窗口检查的序号，伪造或重放的数据报会被丢弃，也无法改变该 peer 的地址。数据报经过认证但不加密。`UDPClient` 空闲时每秒发送一次心跳；静默超过 `Auth.IdleTimeout`（默认 10 秒，最少 3 秒）的客户端视为掉线。服务端只回应至少 128 字节的握手请求，且回复永远不超过收到的字节数。
- 握手不加密：握手请求携带令牌，欢迎消息携带恢复密钥，UDP 的欢迎消息还携带会话密钥。生产环境的 WebSocket 请使用 `wss://`。UDP 没有这样一层加密，它的地址与密钥绑定只能防住看不到流量的攻击者：任何能观察到握手的人（共享 Wi-Fi、路径上的任一跳）都能伪造该会话的数据报，并从自己的地址恢复该会话。UDP 令牌应短期有效且只能使用一次；若需防范路径上的窃听者，优先使用 `wss://` 上的 WebSocket。

## 服务端权威增量复制

在动作、合作或竞技场游戏中，用 `ServerWorld` 与 `ClientWorld` 同步实体状态。`ServerWorld.Step(tick)` 跟踪每个客户端已确认的快照并发送紧凑的字段级增量（必要时自动回退为全量快照）：

```go
// 服务端：
server := net.NewServerWorld(serverTx)
heroID := server.Spawn(net.EntityState{
	ComponentState: net.ComponentState{Kind: 1, X: 100, Y: 50},
})
server.Update(heroID, updatedState)
_ = server.Step(tick)

// 客户端：
client := net.NewClientWorld(clientTx, serverID)
client.OnSpawn = func(e net.EntityState) { /* 创建表现层节点 */ }
client.OnDespawn = func(id net.EntityID) { /* 移除表现层节点 */ }

client.Poll()
client.Predict(heroID, localCmd, applyInput)
renderEntities := client.Interpolate(renderTick) // 平滑插值远端实体
_ = renderEntities
```

可通过 `server.Stats()`、`client.PredictedState(id)`、`client.Reconciliations()` 与 `client.LastReconcileError()` 监控带宽与纠偏误差。

- `Step` 交给 `Transport.Send` 的负载永远不超过 `DefaultMaxSnapshotSize`（60000 字节，UDP、中继和 WebSocket 都能容纳），或通过 `server.SetMaxSnapshotSize(n)` 设置的大小。更大的完整快照会被拆成若干分片，`ClientWorld` 在全部到齐后重新组装再应用；超出上限的增量快照会改为发送拆分后的完整快照。丢失一个分片只损失这一份快照：客户端不会确认它，下一次 `Step` 会重新发送一份完整快照。`Stats().PacketsSent` 按分片计数，`SnapshotsSent` 按整份快照计数。单个实体自身就放不进一个分片时，`Step` 返回指明该实体的 `ErrPayloadTooLarge`。
- tick 为 `uint32`，按序列号算术（serial number arithmetic）比较先后，因此计数器在 2^32 个 tick 后回绕（60 Hz 下约 2.27 年）时复制仍能继续工作。`Interpolate` 以最新快照的 tick 为基准、按模 2^32 解读 `renderTick`，所以 `float64(latest) - delay` 在回绕前后都能正常工作。服务端为新一局或重启后从头计数 tick 时，客户端会把这些 tick 当作过期快照：请在新一局的第一份快照之前调用 `client.Reset()`，它会丢弃所有快照与预测，并通过 `OnDespawn` 报告每个已知实体。

## 确定性锁步与回滚同步

面向格斗、RTS 或高响应街机合作游戏，实现 `SimState`（`Step`、`Save`、`Load`、`Checksum`）并交由 `LockstepSession` 或 `RollbackSession` 驱动：

```go
session := net.NewRollbackSession(net.RollbackConfig{
	Transport:        peerTx,
	Sim:              sim,
	Peers:            []net.PeerID{1, 2},
	MaxRollback:      8,
	InputDelay:       1,
	ChecksumInterval: 30,
	OnDesync: func(ev net.DesyncEvent) {
		log.Printf("desync at tick %d: local=%x remote=%x", ev.Tick, ev.LocalChecksum, ev.RemoteChecksum)
	},
})

// 在 game.Loop.Update 的每个固定步长中：
if err := session.Poll(); err != nil {
	return err
}
if err := session.Step(net.InputFrame{Tick: session.Tick(), Peer: peerTx.LocalID(), Bits: buttons}); err != nil {
	return err
}
```

- `RollbackSession` 会预测尚未到达的远端输入、按帧保存快照，并在延迟到达的远端输入与预测不一致时自动回滚重算（若延迟超出 `MaxRollback` 则返回 `ErrRollbackWindowExceeded`）。
- `LockstepSession`（`SubmitInput`、`Poll`、`Step`）仅在所有对等端当前帧输入齐全时推进模拟，适合 RTS 或严格同步玩法。
- 两种会话均支持通过 `Peers`、`AddPeer` 与 `RemovePeer` 动态调整成员（中途加入者在通过 `Save`/`Load` 同步 `SimState` 快照后加入，掉线玩家移除后不再阻塞会话），以序列号算术跨 `uint32` 回绕比较 tick，并支持周期性校验和比对（可配合 `ChecksumBytes`），通过 `OnDesync` 与 `Desyncs()` 第一时间捕获不同步缺陷。

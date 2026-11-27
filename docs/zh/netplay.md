# netplay

`game/online/netplay` 为 NSGo 游戏提供跨平台多人联机能力：横跨 WebSocket（原生 + WebAssembly）、原生 UDP 与仿真回环的统一 `Transport` 接口；带客户端预测与插值的服务端权威增量状态复制；以及确定性锁步（lockstep）与回滚（rollback）网络同步。

```go
import "github.com/nsgo-dev/game/pkg/online/netplay"
```

## 跨平台传输层（`Transport`）

所有传输实现共用同一套非阻塞 `Transport` 接口（`Send`、`Recv`、`LocalID`、`Peers`、`Close`），支持 `ChannelReliableOrdered` 与 `ChannelUnreliable` 通道以及广播地址 `PeerBroadcast`（`PeerID(0)`）：

| 传输实现 | 构造函数 | 适用平台与场景 |
| --- | --- | --- |
| `WebSocketServer` / `WebSocketTransport` | `ListenWebSocket`、`DialWebSocket` | 原生 + 浏览器（`js/wasm`），适合 Web 构建与跨 NAT 联机。客户端用令牌认证，ID 由服务端分配（见下文）。 |
| `UDPServer` / `UDPClient` | `ListenUDPServer`、`DialUDP` | 原生平台（桌面/移动）与服务端之间的低延迟加密数据报通信，使用 Noise NK 密钥交换（第 2 版握手）与 ChaCha20-Poly1305 AEAD。 |
| `UDPTransport` | `ListenUDP` + `AddPeerKey` / `AddPeer` | 原生平台点对点数据报，双方事先知道彼此地址：`AddPeerKey` 使用预共享密钥做认证加密，普通 `AddPeer` 不加密、不认证，仅用于局域网与测试。 |
| `Relay` / `RelayTransport` | `NewRelay`、`NewRelayTransport` | 彼此无法直连的玩家之间的点对点会话：每人通过 WebSocket 或 UDP 连接中继，中继只在同一会话的成员之间转发（见[服务端指南](server.md#点对点中继)）。 |
| `LoopbackHub` / `LoopbackTransport` | `NewLoopbackHub` + `AddPeer` | 内存内确定性测试，可配置 `Latency`、`Jitter`、`LossRate` 与 `Seed`。 |

UDP 没有重传与排序保证，因此两种 UDP 传输都会以 `ErrUnsupportedChannel` 拒绝 `ChannelReliableOrdered`；锁步与回滚需要 WebSocket 或回环传输，或者 `RelayTransport`：它做端到端重传，因此在 UDP 上也能运行锁步与回滚。

`DialWebSocket` 说 netplay 自己的握手与分帧协议，只能连接 `WebSocketServer`。要连接不属于本仓库的 WebSocket 服务器——第三方后端、聊天服务、自定义协议——请改用 [wsclient](wsclient.md)。

一个传输只能有一个消费者。`Recv` 把每个包只交给一个调用方，而 `ServerWorld`、`ClientWorld`、锁步与回滚会话、`Relay` 和 `RelayTransport` 都会取空所有等待中的包并丢弃不属于自己的包，所以两个消费者共用一个传输会互相抢走对方的包。请为每个消费者准备各自的传输，或在它们前面放一个分流器，为每个消费者各实现一个 `Transport`。无论队列中有多少包，`Recv` 的开销都相同，因此取空一批突发的包仍是线性的。

`UDPTransport` 或 `UDPServer` 底层的套接字出错时，`Err` 返回包装了套接字错误的 `ErrReceiveFailed`，`Send` 也返回该错误；此时应关闭传输并重新监听；`Close` 之后 `Err` 返回 nil。只涉及单个数据报的错误（例如之前某次发送引起的 ICMP 端口不可达）不会使其停止。`UDPClient` 同样会在 ICMP 错误之后继续读取（例如服务端正在重启时），由空闲超时来判断服务端是否已经消失。

```go
hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{
	Latency:  40 * time.Millisecond,
	Jitter:   10 * time.Millisecond,
	LossRate: 0.02,
	Seed:     7,
})
serverTx := hub.AddPeer(1)
clientTx := hub.AddPeer(2)
_ = serverTx.Send(2, netplay.ChannelReliableOrdered, []byte("hello"))
hub.Advance(60 * time.Millisecond)
pkt, ok := clientTx.Recv()
_, _ = pkt, ok
```

### 收件队列上限

传输会保留每一个到达的包，直到 `Recv` 取走它，并按通道和对端限制保留的数量。每个对端的 `ChannelUnreliable` 包最多保留 `DefaultUnreliableInbox`（4096）个：某个对端已排队这么多不可靠包时再到达一个，就丢弃该对端最旧的不可靠包，因此落后的游戏保留的是最新的状态，向服务端狂发包的对端只会挤掉自己的包，`InboxStats().UnreliableDropped` 记录丢弃的数量。

`ChannelReliableOrdered` 的包从不丢弃，否则会破坏这个通道的保证。对端不可信任的传输改为把每个对端的可靠包限制在 `DefaultReliableInbox`（16384）个，并拒收超出的那个包，记入 `InboxStats().ReliableRefused`：`WebSocketServer` 会结束该客户端的会话，如同它主动离开，因此它已送达的包仍保持顺序；`RelayTransport` 则不再确认该对端的消息，对端会在 `Recv` 腾出空间后重发。其他传输只从自己的服务端或同一进程接收可靠包，会全部保留。每个传输在全部可靠包的积压每次超过 `DefaultReliableInboxWarning`（4096）时，`InboxStats().ReliableWarnings` 记一次，`ReliableBacklog` 一直为 true，直到积压降到阈值的一半，所以在阈值附近徘徊的队列不会每个包都告警一次。`Recv` 仍按所有通道和对端合起来的到达顺序返回包。`UDPTransport` 从不发送可靠包，因此会丢弃自称可靠的数据报。

每个传输都实现 `InboxReporter`：`SetInboxLimits` 随时修改上限（调低不可靠包上限会立即丢弃每个对端最旧的不可靠包），`InboxStats` 读取计数器。计数器只增不减，游戏或调试叠加层把它们与上一帧的值比较即可。`LoopbackConfig.Inbox` 设置集线器之后添加的对端的上限，`RelayTransportConfig.Inbox` 设置 `RelayTransport` 的上限。`InboxLimits` 中零表示使用默认值，负值表示取消上限或关闭告警。`RelayTransport` 的内层传输有自己的上限，所有经中继转发的对端都从中继到达内层传输，因此共用它的单个对端上限；在那里被丢弃的可靠消息会被重发。

```go
tx.SetInboxLimits(netplay.InboxLimits{Unreliable: 512, ReliableWarning: 1024})

var lastWarnings, lastDropped uint64

// Each frame, after draining Recv:
st := tx.InboxStats()
if st.ReliableWarnings != lastWarnings {
	log.Printf("reliable backlog of %d packets", st.Reliable)
}
if st.UnreliableDropped != lastDropped {
	log.Printf("dropped %d stale unreliable packets", st.UnreliableDropped-lastDropped)
}
lastWarnings, lastDropped = st.ReliableWarnings, st.UnreliableDropped
```

## 连接握手与重连

`WebSocketServer` 和 `UDPServer` 只在握手之后接纳客户端：客户端出示应用自定义的令牌，由 `Auth.Verify` 校验，服务端分配该客户端的 `PeerID` 并回传给它，或附带原因拒绝。被拒绝的客户端永远不会出现在 `Recv` 或 `Peers` 中。

```go
auth := netplay.Auth{
	// Check a signed token locally; the error text is sent to the client.
	Verify: func(peer netplay.PeerID, token []byte) error {
		if !validToken(token) {
			return errors.New("invalid token")
		}
		return nil
	},
	GracePeriod: 15 * time.Second, // a dropped client keeps its ID this long
}
srv, _ := netplay.ListenWebSocket(":8080", 1, auth) // or netplay.ListenUDPServer(":7777", 1, auth)

// Each server tick:
for ev, ok := srv.NextEvent(); ok; ev, ok = srv.NextEvent() {
	switch ev.Kind {
	case netplay.PeerJoined: // new ID
	case netplay.PeerDropped: // connection lost; ID and state kept for GracePeriod
	case netplay.PeerResumed: // same ID back within GracePeriod
	case netplay.PeerLeft: // closed, or GracePeriod ran out
		rooms.Leave(roomOf(ev.Peer), ev.Peer)
		guard.Reset(ev.Peer)
	}
}
```

```go
cli, err := netplay.DialWebSocket("wss://play.example.com/game", token) // 或 netplay.DialUDP(addr, serverPubKey, token)
if errors.Is(err, netplay.ErrRejected) {
	// err is a *netplay.RejectError carrying the server's reason
}
me := cli.LocalID() // assigned by the server

// After a network drop (State() == netplay.ConnDisconnected):
_ = cli.Reconnect() // cli.Resumed() reports whether it kept its ID
```

- `NextEvent` 在被调用时检查宽限期与空闲超时，因此服务端应在每个 tick 与 `Recv` 一起轮询它。排队的生命周期事件（`PeerJoined`、`PeerDropped`、`PeerResumed`、`PeerLeft`）按先进先出顺序以均摊常数时间取出，不会静默丢弃。peer 处于宽限期内时，向它 `Send` 会返回 `ErrDisconnected`，`Peers` 也不会列出它。
- 客户端调用 `Close` 即为主动告别：服务端立即报告 `PeerLeft`，不再为其保留会话。`Reconnect` 则不算告别，即使连接仍处于打开状态也是如此：浏览器关闭旧连接时会带上一个关闭码，服务端据此将其视为掉线。
- 服务端最多保留 `Auth.MaxPeers` 个会话（默认 1024），处于宽限期内的 peer 也计算在内；超出后客户端会以原因 `server full` 被拒绝。不设置 `Verify` 时任何人都能加入，因此可从互联网访问的服务端务必设置它。
- 恢复会话需要出示上一次欢迎消息中的密钥，服务端每次握手都会更换该密钥；仅凭 ID 无法接管会话。超过宽限期后，只要令牌仍然有效，`Reconnect` 会作为新 peer 重新加入。
- 在浏览器中 `DialWebSocket` 会立即返回：欢迎消息到达之前 `State` 为 `ConnConnecting`、`LocalID` 为 0，此间发送的包会排队。被拒绝时转为 `ConnDisconnected`，`Err` 返回 `*RejectError`。
- `WebSocketServer` 每隔 `Auth.IdleTimeout` 的三分之一（默认 10 秒，最少 3 秒）向每个客户端发送一次 ping，在这段时间内没有收到客户端的任何字节（连 pong 都没有）就会把它判为掉线，这样网络消失却没有关闭 TCP 连接的客户端不会永远占着会话和 ID。浏览器会自动回应 ping。原生 `WebSocketTransport` 每 2 秒向服务端发送一次 ping，10 秒内没有收到任何字节就转为 `ConnDisconnected`。只要字节持续到达，慢慢传来的大帧也会被完整读取。这些超时使用墙上时钟，而不是 `Auth.Now`。`WebSocketServer.Close` 会同时向所有客户端发送关闭帧，对不读取数据的客户端最多等待一秒。
- WebSocket 帧的负载按实际到达的字节读取，而不是按帧头声明的长度预先分配，握手请求也不能超过合法请求的长度，因此客户端声明一个很大的帧后再慢慢发送，也无法占用它尚未发送的内存。控制帧（`Close`、`Ping`、`Pong`）遵循 RFC 6455 §5.5：分片控制帧、负载超过 125 字节的控制帧以及保留控制操作码（`0xB`–`0xF`）会被拒绝。
- UDP 会话运行基于 `Noise_NK_25519_ChaChaPoly_SHA256` 的第 2 版握手：`DialUDP(addr, serverKey, token)` 依据服务端的静态 X25519 公钥在无需证书的情况下认证服务端，在消息 1 中加密传输客户端令牌与恢复密钥，并在加密的消息 2 中接收服务端分配的 ID 与新的恢复密钥（欢迎消息不再携带数据包密钥）。第 1 版握手请求会收到既有的版本拒绝。
- 服务端的静态 X25519 公钥需通过带外渠道分发：若客户端从配置文件读取公钥或需要在服务端重启后重连，请设置持久化的 `Auth.StaticKey`（`ecdh.X25519()` 私钥）；若将 `Auth.StaticKey` 留空，每次监听会生成新的 X25519 密钥对，可通过匹配票据分发 `srv.PublicKey().Bytes()`（客户端用 `ecdh.X25519().NewPublicKey` 解析）。缺失或非 X25519 密钥会返回 `ErrInvalidKey`。
- UDP 握手之后，每个数据报都使用 Noise split 派生的两把方向性密钥做 ChaCha20-Poly1305 认证加密：10 字节明文头部（`type`、`peer`、`seq`、`channel`）作为关联数据参与认证，后接密文与 16 字节 Poly1305 标签（总开销 26 字节），使用隐式的 `direction || peer || seq` nonce 与 64 项重放窗口。发送和接收都会在 32 位序号即将回绕时拒绝继续使用（返回 `ErrSequenceExhausted`）；调用 `Reconnect` 会执行一次新的 Noise NK 握手来更换会话密钥并重置序号。`UDPClient` 空闲时每秒发送一次心跳；静默超过 `Auth.IdleTimeout`（默认 10 秒，最少 3 秒）的客户端视为掉线。服务端只回应至少 128 字节的握手请求，且回复永远不超过收到的字节数。
- 在 `UDPTransport` 上，`AddPeerKey(id, addr, key)` 用 32 字节预共享密钥为点对点数据报提供相同的 26 字节 ChaCha20-Poly1305 封装、重放窗口、防反射保护与序号回绕拒绝（使用新密钥再次调用 `AddPeerKey` 即可换钥）；声称来自已设密钥对端的明文数据报会被丢弃。普通 `AddPeer` 保持无认证、无加密，仅适用于受信任的局域网环境与测试。
- WebSocket 握手（第 1 版）在应用层不加密，生产环境请使用 `wss://`（`ListenWebSocketTLS` / `ListenWebSocketTLSFiles`）。

## 服务端权威增量复制

在动作、合作或竞技场游戏中，用 `ServerWorld` 与 `ClientWorld` 同步实体状态。`ServerWorld.Step(tick)` 跟踪每个客户端已确认的快照并发送紧凑的字段级增量（必要时自动回退为全量快照）：

```go
// 服务端：
server := netplay.NewServerWorld(serverTx)
heroID := server.Spawn(netplay.EntityState{
	ComponentState: netplay.ComponentState{Kind: 1, X: 100, Y: 50},
})
server.Update(heroID, updatedState)
_ = server.Step(tick)

// 客户端：
client := netplay.NewClientWorld(clientTx, serverID)
client.OnSpawn = func(e netplay.EntityState) { /* 创建表现层节点 */ }
client.OnDespawn = func(id netplay.EntityID) { /* 移除表现层节点 */ }

client.Poll()
client.Predict(heroID, localCmd, applyInput)
renderEntities := client.Interpolate(renderTick) // 平滑插值远端实体
_ = renderEntities
```

可通过 `server.Stats()`、`client.PredictedState(id)`、`client.Reconciliations()` 与 `client.LastReconcileError()` 监控带宽与纠偏误差。

- `Step` 交给 `Transport.Send` 的负载永远不超过 `DefaultMaxSnapshotSize`（60000 字节，UDP、中继和 WebSocket 都能容纳），或通过 `server.SetMaxSnapshotSize(n)` 设置的大小。更大的完整快照会被拆成若干分片，`ClientWorld` 在全部到齐后重新组装再应用；超出上限的增量快照会改为发送拆分后的完整快照。丢失一个分片只损失这一份快照：客户端不会确认它，下一次 `Step` 会重新发送一份完整快照。`Stats().PacketsSent` 按分片计数，`SnapshotsSent` 按整份快照计数。单个实体自身就放不进一个分片时，`Step` 返回指明该实体的 `ErrPayloadTooLarge`。
- tick 为 `uint32`，按序列号算术（serial number arithmetic）比较先后，因此计数器在 2^32 个 tick 后回绕（60 Hz 下约 2.27 年）时复制仍能继续工作。`Interpolate` 以最新快照的 tick 为基准、按模 2^32 解读 `renderTick`，所以 `float64(latest) - delay` 在回绕前后都能正常工作。服务端在同一 epoch 内从头计数 tick 时，客户端会把这些 tick 当作过期快照：请在新一局的第一份快照之前调用 `client.Reset()`，它会丢弃所有快照、预测与 epoch，并通过 `OnDespawn` 报告每个已知实体；也可以用 `server.SetEpoch` 开始新一局，让客户端自行跟上。

### 复制 epoch

每份快照都携带服务端的复制 epoch，这是 `NewServerWorld` 随机选取的一个数，因此即使 tick 有重叠，重启后服务端的快照也能与上一次运行区分开。`ClientWorld` 收到新 epoch 的一份完整快照时，会丢弃旧 epoch 的所有快照、未拼完的分片与预测，通过 `OnDespawn` 报告每个已知实体，并从新 epoch 继续，游戏无需调用 `Reset`。这份快照会先完整解析，因此损坏的数据包无法清空世界。新 epoch 的增量快照无法应用到客户端已有的任何状态上，因此只会让客户端向服务端请求该 epoch 的完整快照。客户端会记住最近离开的 16 个 epoch 并忽略它们的快照，因此迟到的旧运行快照无法把客户端拉回去。`client.Epoch()` 返回客户端当前跟随的 epoch。

- 服务端不重启、但以从零开始的 tick 开始新一局时，调用 `server.SetEpoch(server.Epoch() + 1)`：下一次 `Step` 会向每个对端发送完整快照，客户端像遇到重启一样丢弃上一局。自行为运行编号的服务端，或需要固定 epoch 的测试，也用同样的方式设置。epoch 不应重复，因为客户端会忽略已离开的 epoch，直到连续收到它的 32 份快照。
- 随机 epoch 无法比较先后，因此如果客户端从未见过的某个 epoch 的迟到快照在当前 epoch 的第一份快照之后到达，客户端会转到那个已经结束的 epoch。所以，已离开的 epoch 若持续发送快照，连续 32 份且其间没有当前 epoch 的快照，就被视为仍在运行的 epoch，客户端重新跟随它；已结束运行的迟到快照远少于这个数。重启时恰好抽到已离开的 16 个 epoch 之一，也以同样方式恢复。
- 两次运行随机选到同一 epoch 的概率为 2^32 分之一；这种重启看起来就像 tick 回退的服务端，用 `Reset` 或 `SetEpoch` 处理。
- 确认消息同样携带 epoch，服务端会忽略其他 epoch 的确认，因此客户端对旧运行的确认永远不会成为新运行中的增量基线。

### 线路格式

复制使用其线路协议的第 2 版（`ReplicationVersion`）；第 2 版加入了 epoch。每条消息以一个字节开头，高 4 位是协议版本，低 4 位是消息类型，随后是 4 字节小端序的 epoch。`uv` 指 `encoding/binary` 写出的无符号 varint。

| 消息 | 类型 | 方向 | 5 字节头之后的布局 |
| --- | --- | --- | --- |
| 完整快照 | 1 | 服务端到客户端 | `tick(uv) count(uv) entity...` |
| 增量快照 | 2 | 服务端到客户端 | `tick(uv) baselineTick(uv) despawnCount(uv) id(uv)... updateCount(uv) entity...` |
| 确认 | 3 | 客户端到服务端 | `tick(uv)` |
| 重新同步请求 | 4 | 客户端到服务端 | 无 |
| 完整快照分片 | 5 | 服务端到客户端 | `tick(uv) index(uv) total(uv) count(uv) entity...` |

实体为 `id(uv) mask(1)`，后面按位序跟随掩码中置位的字段：kind（uv），x、y、vx、vy 与 rotation（4 字节小端序浮点数），flags（uv），以及 props（uv 长度加字节）。完整快照为每个非零字段置位；增量快照为相对基线发生变化的字段置位，对基线中没有的实体则为其每个非零字段置位。

第 1 版来自加入 epoch 之前的版本，没有消息头：其消息直接以类型 1 到 5 开头，按第 1 版解读。不同版本的对端之间无法复制，且双方都不会误读对方的消息。客户端拒绝其他版本的快照并通过 `client.Err()` 报告，服务端拒绝其他版本的确认与重新同步请求并通过 `server.PeerErr(peer)` 报告；两种错误都包装了 `ErrReplicationVersion` 并写明双方版本，游戏可以据此提示玩家更新，而不是一直等待永远不会到来的世界。

`pkg/online/netplay` 的每一种线路格式——传输帧、UDP 认证封包、连接握手、中继帧与控制体、复制协议 v2、锁步/回滚输入以及对局中途成员变更——都在 `pkg/online/netplay/wire/` 下以带版本的 YAML Schema（`*.wire.yaml`）描述，并由 `cmd/gamewire` 生成零分配 Go 编解码器（`AppendX` / `DecodeX`，以及增量掩码的 `DiffX` / `ApplyX`）。游戏也可以用同一套 Schema 工具配合位打包和纯整数 `qfloat`/`fixed` 量化为 `EntityState.Props` 或自定义消息生成编解码器，详见 [netplay/wire](netplay-wire.md)。

## 确定性锁步与回滚同步

面向格斗、RTS 或高响应街机合作游戏，实现 `SimState`（`Step`、`Save`、`Load`、`Checksum`）并交由 `LockstepSession` 或 `RollbackSession` 驱动：

```go
session := netplay.NewRollbackSession(netplay.RollbackConfig{
	Transport:        peerTx,
	Sim:              sim,
	Peers:            []netplay.PeerID{1, 2},
	MaxRollback:      8,
	InputDelay:       1,
	ChecksumInterval: 30,
	OnDesync: func(ev netplay.DesyncEvent) {
		log.Printf("desync at tick %d: local=%x remote=%x", ev.Tick, ev.LocalChecksum, ev.RemoteChecksum)
	},
})

// 在 game.Loop.Update 的每个固定步长中：
if err := session.Poll(); err != nil {
	return err
}
if err := session.Step(netplay.InputFrame{Tick: session.Tick(), Peer: peerTx.LocalID(), Bits: buttons}); err != nil {
	return err
}
```

- `RollbackSession` 会预测尚未到达的远端输入、按帧保存快照，并在延迟到达的远端输入与预测不一致时自动回滚重算（若延迟超出 `MaxRollback` 则返回 `ErrRollbackWindowExceeded`）。
- `LockstepSession`（`SubmitInput`、`Poll`、`Step`）仅在所有对等端当前帧输入齐全时推进模拟，适合 RTS 或严格同步玩法。`SubmitInput` 会忽略已执行过的 tick 或超前 256 个 tick 以上的输入。
- 两种会话均支持周期性校验和比对（可配合 `ChecksumBytes`），通过 `OnDesync` 与 `Desyncs()` 第一时间捕获不同步缺陷。
- 两种会话均按序列号算术（RFC 1982）比较 `uint32` tick（包括成员参与窗口与待处理加入/离开），因此长时间运行的对局在 2^32 tick 回绕时仍可无缝继续。`RollbackSession` 在首个 tick 确认之前不裁剪历史记录，因此即使某个对等端的首条输入晚于 `MaxRollback + 2` 个 tick 到达，仍可正常确认并推进。

### 对局中途加入与离开的玩家

对局的对等端并不在创建会话时固定。由其中一个对等端，即主机（配置中的 `Host`，默认为最小的对等端 ID），在对局进行中接纳和移除对等端，每个对等端都在同一个 tick 应用每一次变更：

```go
// 在主机上，当它的传输报告新玩家时（PeerJoined，或 RelayTransport.Peers 中出现新 ID）：
joinTick, err := session.AddPeer(newPeer)

// 在新玩家的机器上；照常每帧调用 Step，在 Active 报告 true 之前它只做轮询：
session := netplay.NewRollbackSession(netplay.RollbackConfig{Transport: peerTx, Sim: sim, Host: hostID, Join: true})

// 在主机上，当它的传输报告该玩家已离开时（PeerLeft，或 Peers 中不再有该 ID）：
leaveTick, err := session.RemovePeer(oldPeer)
```

- 加入 tick 是主机下一条自身输入所对应的 tick：锁步取 `Tick` 与其最新已提交输入的下一个 tick 中较晚者，回滚取 `Tick + InputDelay`。任何对等端都不可能已经执行（锁步）或确认（回滚）该 tick，因为这需要主机在该 tick 的输入，而变更消息走可靠通道、先于那条输入送达。已经预测越过该 tick 的回滚对等端会回滚到该 tick。
- 当主机的模拟处在加入 tick 的开头（锁步），或加入 tick 之前的每个 tick 都已确认（回滚）时，主机把该 tick 的 `Sim.Save()` 连同 tick、对等端列表，以及每个已在离开中的对等端从加入 tick 到其离开 tick 由主机声明的输入发给新玩家，按 48000 字节分片。新玩家把状态载入自己的 `Sim`，其 `Tick` 从加入 tick 开始，此后与其他人一样参与对局，主机在新玩家拿到状态之前移除的对等端所参与的 tick 也不例外。状态大于 `MaxJoinStateSize`（16 MiB）时 `AddPeer` 返回 `ErrPayloadTooLarge`；到加入 tick 时状态才增长到超过上限的，这次加入会变为一次离开，因此没有人会一直等待新玩家。
- 加入 tick 之前不需要新玩家的输入。回滚中，新玩家最初 `InputDelay` 个 tick 的输入在每个对等端上都是零输入，与对局开始时每个对等端的情况相同。每个对等端会把自己已经发出的、加入 tick 及以后的输入补发给新玩家；新玩家的输入如果先于主机的通知到达某个对等端，会被保留到通知到达为止。
- 离开 tick 是主机下一条自身输入的 tick，若该对等端的加入 tick 更晚则取加入 tick。变更消息携带主机持有的、该对等端在离开 tick 之前的输入，主机从未收到的 tick 则以零输入代替，因此每个对等端以相同的方式执行这些 tick；此后直接来自离开者的输入一律忽略。请在对等端停止发送之后再移除它：仍在发送时被移除的对等端，可能已有某条主机从未见过的输入送达其他对等端，这会表现为不同步。被移除者自己的会话在离开 tick 停止，其 `Active` 报告 false。
- `Members` 列出参与当前 tick 的对等端。只有主机可以调用 `AddPeer` 与 `RemovePeer`（其他对等端调用返回 `ErrNotHost`），主机不能离开（`ErrHostLeave`），离开过的对等端要以新的 ID 回来（`ErrPeerRejoin`，无论离开了多久），中继和服务端本来也会分配新 ID。对已经离开的对等端调用 `RemovePeer` 会返回其离开 tick。
- 传输暂时无法投递的消息（中继尚未把新玩家告知发送方，或某个对等端正在重连）会留在会话中，由下一次 `Poll` 重新发送，并与其后的消息保持顺序。会话为每个对等端最多保留 4096 条这样的消息；超过之后该对等端已错过对局的一部分，此后发给它的消息一律丢弃，下一次 `SubmitInput`（锁步）或 `Step`、`Poll`（回滚）会为它返回一次 `ErrSessionBacklog`，由主机移除它或结束对局。
- 加入中的会话从 `Host` 得知主机，否则取 `Peers` 中最小的其他 ID；两者都没有指明其他对等端时，它在收到加入状态之前的每次 `Poll` 中取传输报告的最小的其他 ID。通过中继加入时，中继的对等端列表可能仍为空或包含其他新玩家，请显式设置 `Host`。

## 抓包与协议检查

`netplay.NewCaptureTransport(inner, w, opts)` 可以包装任意 `Transport`，对每次成功的 `Send` 以及 `Recv` 返回的每个数据包写入一行带版本号的 JSON Lines 记录（`version: 1`）：

```json
{"version":1,"t":16000000,"dir":"send","peer":2,"ch":1,"seq":1,"payload":"IwEAAAAB"}
```

```go
f, _ := os.Create("session.capture.jsonl")
defer f.Close()

tr := netplay.NewCaptureTransport(inner, f, netplay.CaptureOptions{
	Channels:    []netplay.Channel{netplay.ChannelUnreliable},
	Peers:       []netplay.PeerID{2},
	SampleEvery: 1,
})
```

- **解密后的明文捕获。** 因为 `NewCaptureTransport` 位于 `Transport` 接口层，包装 `UDPClient` 或 `UDPServer` 时捕获的是认证与解密后的明文载荷，离线解析无需会话密钥。
- **过滤与采样。** `CaptureOptions` 支持按 `Channels` 和 `Peers` 过滤，并支持按固定间隔 `SampleEvery` 或基于 `SampleRate` 与 `SampleSeed` 的确定性采样。写入抓包流出错时会通过 `OnError` 回调通知，不会让 `Send` 或 `Recv` 失败。
- **透传 `InboxReporter`。** 包装器会将 `SetInboxLimits` 与 `InboxStats` 转发到底层传输。
- **隐私与安全警示。** 抓包文件包含原始玩家输入、实体位置与游戏状态。切勿随游戏分发抓包文件、提交真实玩家对局数据，或在未征得明确同意的情况下收集玩家抓包。

### 用 `game net` 解析抓包

`game net` 子命令由 [线协议 Schema](netplay-wire.md) 驱动，可逐层解析抓包文件（传输层帧头、中继帧与控制体，以及复制、锁步/回滚、成员变更、握手或自定义游戏消息），并输出流量统计：

```sh
# 按 YAML 或 JSON 逐层解析抓包文件
nsgo game net decode session.capture.jsonl
nsgo game net decode --format json session.capture.jsonl

# 使用游戏自定义的 *.wire.yaml Schema 解析 EntityState.Props 等消息
nsgo game net decode --schema ./proto session.capture.jsonl

# 跟踪每个对等端的复制基线，在 SnapshotDelta 中同时输出完整实体状态
nsgo game net decode --resolve-deltas session.capture.jsonl

# 从标准输入解析单帧十六进制或 Base64 数据
echo "230100000001" | nsgo game net decode -

# 按消息类型与对等端统计包数、字节数以及平均增量大小
nsgo game net stats session.capture.jsonl
```

### 实时网络调试浮层

如需在游戏运行时以零每帧分配的方式查看实时流量，可用 `debug.NewNetMonitor` 包装传输并将 `mon.Snapshot` 传入 `debug.Options.Net`。浮层面板会借助复用的 `wire` 编解码缓冲区展示各通道与各对等端的字节率与包率、`InboxStats` 计数器以及最近解码的若干消息类型。详见 [debug](debug.md#网络检查面板)。

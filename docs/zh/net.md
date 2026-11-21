# net

`game/net` 为 NSGo 游戏提供跨平台多人联机能力：横跨 WebSocket（原生 + WebAssembly）、原生 UDP 与仿真回环的统一 `Transport` 接口；带客户端预测与插值的服务端权威增量状态复制；以及确定性锁步（lockstep）与回滚（rollback）网络同步。

```go
import "github.com/nsgo-dev/game/packages/game/net"
```

## 跨平台传输层（`Transport`）

所有传输实现共用同一套非阻塞 `Transport` 接口（`Send`、`Recv`、`LocalID`、`Peers`、`Close`），支持 `ChannelReliableOrdered` 与 `ChannelUnreliable` 通道以及广播地址 `PeerBroadcast`（`PeerID(0)`）：

| 传输实现 | 构造函数 | 适用平台与场景 |
| --- | --- | --- |
| `WebSocketServer` / `WebSocketTransport` | `ListenWebSocket`、`DialWebSocket` | 原生 + 浏览器（`js/wasm`），适合 Web 构建与跨 NAT 联机。 |
| `UDPTransport` | `ListenUDP` + `AddPeer` | 原生平台（桌面/移动）低延迟数据报通信。UDP 没有重传与排序保证，因此会以 `ErrUnsupportedChannel` 拒绝 `ChannelReliableOrdered`；锁步与回滚需要 WebSocket 或回环传输。 |
| `LoopbackHub` / `LoopbackTransport` | `NewLoopbackHub` + `AddPeer` | 内存内确定性测试，可配置 `Latency`、`Jitter`、`LossRate` 与 `Seed`。 |

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
- 两种会话均支持周期性校验和比对（可配合 `ChecksumBytes`），通过 `OnDesync` 与 `Desyncs()` 第一时间捕获不同步缺陷。

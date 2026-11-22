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

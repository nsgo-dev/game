# flow

`game/flow` 提供完全由 `game.Loop` 固定步长 `dt` 驱动的玩法流程原语：游戏时钟计时器 `Timers`、泛型有限状态机 `Machine[S]`、可组合步进序列（`Seq`、`Parallel`、`Race`）以及零全局状态的类型化事件总线 `Bus`。

```go
import "github.com/nsgo-dev/game/packages/game/flow"
```

`flow` 中没有任何代码读取系统墙钟、启动 goroutine 或保存包级全局状态——所有原语都随循环暂停、保持确定性回放，并在场景间干净隔离。

## 游戏时钟 Timers

`Timers` 在游戏时钟上调度一次性（`After`）和无漂移周期（`Every`）回调，零值即可直接使用：

```go
var timers flow.Timers

// 游戏时间 2 秒后触发一次
invuln := timers.After(2*time.Second, func() {
	player.Invulnerable = false
})

// 每 500ms 触发一次，结转溢出时间避免周期漂移
spawner := timers.Every(500*time.Millisecond, spawnWave)

// 在 Loop.Update 中：
timers.Update(dt)
```

在暂停菜单或顿帧期间调用 `timers.SetPaused(true)`（或 `timers.Paused = true`）暂停计时；通过 `invuln.Remaining()`、`invuln.Active()` 查询状态，通过 `spawner.Cancel()` 或 `timers.Clear()` 取消。

## 有限状态机（`Machine[S]`）

`Machine[S]` 用任意可比较类型 `S` 标识角色、Boss 或对局状态：

```go
type HeroState int

const (
	StateIdle HeroState = iota
	StateRun
	StateHurt
)

fsm := flow.NewMachine(StateIdle)
fsm.AddState(StateIdle, flow.State{
	Enter:  func() { playAnim("idle") },
	Update: func(dt time.Duration) { recoverStamina(dt) },
})
fsm.AddState(StateRun, flow.State{
	Enter:  func() { playAnim("run") },
	Update: func(dt time.Duration) { moveHero(dt) },
	Exit:   func() { stopFootsteps() },
})
fsm.AddState(StateHurt, flow.State{
	Enter: func() { knockback() },
})

fsm.AddTransition(StateIdle, StateRun, func() bool { return moveSpeed > 0 })
fsm.AddTransition(StateRun, StateIdle, func() bool { return moveSpeed == 0 })
fsm.AddAnyTransition(StateHurt, func() bool { return hitThisStep })
fsm.AddTransition(StateHurt, StateIdle, func() bool { return fsm.TimeInState() >= 400*time.Millisecond })

// 在 Loop.Update 中：
fsm.Update(dt)
```

- `Update(dt)` 在首次调用时自动启动状态机（也可显式调用 `fsm.Start()`），按注册顺序评估转换（每步最多转换一次），累加 `TimeInState()`，再执行当前状态的 `Update`。
- 调用 `fsm.SetState(to)` 立即切换状态；通过 `fsm.Current()`、`fsm.Previous()` 与 `fsm.TimeInState()` 读取状态信息。

## 步进序列（`Runner`、`Seq`、`Parallel`、`Race`）

无需启动 goroutine，用 `Runner` 和 `Step` 组合子编写过场、攻击招式与脚本事件：

```go
var runner flow.Runner

bossIntro := flow.Seq(
	flow.Do(func() { lockPlayerInput(true) }),
	flow.Parallel(
		flow.Wait(600*time.Millisecond),
		flow.While(func() bool { return doorOpen < 1 }, func(dt time.Duration) {
			doorOpen += float32(dt.Seconds()) * 2
		}),
	),
	flow.Race(
		flow.WaitUntil(func() bool { return actions.JustPressed("skip") }),
		flow.Seq(
			flow.Do(func() { showBossTitle() }),
			flow.Wait(2*time.Second),
		),
	),
	flow.Do(func() { lockPlayerInput(false) }),
)

handle := runner.Start(bossIntro)

// 在 Loop.Update 中：
runner.Update(dt)
```

内置 `Step` 组合子：

| 组合子 | 行为 |
| --- | --- |
| `Do(fn)` | 调用 `fn` 一次并立即完成，将全部剩余 `dt` 交给下一步。 |
| `Wait(d)` | 等待 `d` 游戏时间。 |
| `WaitUntil(cond)` | 在 `cond()` 首次返回 `true` 的步长完成。 |
| `While(cond, update)` | 只要 `cond()` 为 `true`，每步调用 `update(dt)`。 |
| `Seq(steps...)` | 顺序执行各步，并在同一帧内把前一步剩余的 `dt` 传递给下一步。 |
| `Parallel(steps...)` | 每步推进全部分支，直到所有分支完成。 |
| `Race(steps...)` | 按顺序推进分支，首个分支完成时立即取消其余分支。 |
| `Repeat(n, makeStep)` | 依次重复执行 `makeStep()` 返回的步骤 `n` 次。 |
| `Loop(makeStep)` | 无限重复，直到句柄或外层 `Race` 取消它。 |

使用 `handle.Cancel()`（或 `runner.Clear()`）安全取消正在运行的序列，用 `handle.Done()` 判断是否结束。

## 类型化事件总线（`Bus`）

`Bus` 在场景内部解耦模块通信，无需全局变量或字符串事件名：

```go
type EnemyDefeated struct {
	Kind  string
	Score int
}

var bus flow.Bus

unsub := flow.Subscribe(&bus, func(ev EnemyDefeated) {
	score += ev.Score
})
defer unsub()

// 按订阅顺序立即同步派发：
flow.Publish(&bus, EnemyDefeated{Kind: "slime", Score: 50})

// 或在物理/碰撞阶段先入队，再在步长末尾统一刷新：
flow.Enqueue(&bus, EnemyDefeated{Kind: "bat", Score: 100})
bus.Flush()
```

一次性监听可使用 `flow.SubscribeOnce(&bus, fn)`，场景卸载时调用 `bus.Clear()` 清空。在回调内部增删订阅不会跳过当前事件尚未执行的其他订阅者。

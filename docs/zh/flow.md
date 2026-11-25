# flow

`game/flow` 提供完全由 `game.Loop` 固定步长 `dt` 驱动的玩法流程原语：游戏时钟计时器 `Timers`、泛型有限状态机 `Machine[S]`、层次状态机 `HSM[S]`、可组合步进序列（`Seq`、`Parallel`、`Race`）以及零全局状态的类型化事件总线 `Bus`。

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

## 层次状态机（`HSM[S]`）

当角色或敌人逻辑超出扁平状态机的表达范围时，`HSM[S]` 允许在父状态内嵌套子状态机，使 `stunned` 或 `dead` 等共享打断转换自动覆盖所有后代状态，而无需在每个子状态上重复声明：

```go
type EnemyState string

const (
	StatePatrol   EnemyState = "patrol"
	StateCombat   EnemyState = "combat"
	StateApproach EnemyState = "approach"
	StateAttack   EnemyState = "attack"
	StateRecover  EnemyState = "recover"
	StateStunned  EnemyState = "stunned"
)

type StunHit struct{ Poise float64 }

combat := flow.NewHSM(StateApproach)
combat.SetHistory(true) // 重新进入 combat 时恢复上次激活的子状态
combat.AddState(StateApproach, flow.State{Enter: func() { playAnim("approach") }})
combat.AddState(StateAttack, flow.State{Enter: func() { playAnim("attack") }})
combat.AddState(StateRecover, flow.State{Enter: func() { playAnim("recover") }})
combat.AddTransition(StateApproach, StateAttack, func() bool { return inRange })
combat.AddEventTransition(StateAttack, StateRecover, "attack_done")
combat.AddTransition(StateRecover, StateApproach, func() bool { return combat.TimeInState() >= 400*time.Millisecond })

enemy := flow.NewHSM(StatePatrol)
enemy.AddState(StatePatrol, flow.State{Enter: func() { playAnim("patrol") }})
enemy.AddState(StateCombat, flow.State{
	Enter: func() { drawWeapon() },
	Exit:  func() { sheatheWeapon() },
})
enemy.AddChild(StateCombat, combat)
enemy.AddState(StateStunned, flow.State{Enter: func() { playAnim("stunned") }})

enemy.AddTransition(StatePatrol, StateCombat, func() bool { return spotted })
flow.OnEvent(enemy, StateCombat, StateStunned, func(e StunHit) bool { return e.Poise >= 15 })
enemy.AddTransition(StateStunned, StateCombat, func() bool { return enemy.TimeInState() >= 800*time.Millisecond })

// 可选：将 flow.Bus 上的类型化事件转发到 HSM 队列
unsub := flow.BindBus[StunHit](enemy, &bus)
defer unsub()

// 在 Loop.Update 中：
enemy.Update(dt)
```

- **由外向内 `Enter` / 由内向外 `Exit`**：进入 `StateCombat` 时先执行父状态 `Enter` 再执行子状态 `Enter`；离开时先执行子状态 `Exit` 再执行父状态 `Exit`。跨分支转换只退出到最低公共祖先（LCA），再逐层进入目标叶子状态。
- **最内层优先评估**：父状态上的转换在其任意后代状态激活期间都生效，且总是从最内层激活状态开始向上评估，子状态可以拦截并覆盖父状态的转换。
- **确定性事件驱动**：通过 `enemy.Send(ev)` 发送事件，或通过 `flow.BindBus[E](enemy, &bus)` 订阅 `flow.Bus` 事件；使用 `AddEventTransition` / `AddAnyEventTransition` 注册值匹配转换，或使用 `flow.OnEvent` / `flow.OnAnyEvent` 注册带载荷的类型化事件转换。入队事件在每次 `Update` 步长内按 FIFO 发送顺序至多处理一次；回调内部新发送的事件顺延至下一步长。
- **浅历史（Shallow History）**：调用 `child.SetHistory(true)` 后，再次进入父状态会恢复上次激活的直接子状态而非 `initial`（可调用 `ClearHistory()` 清空历史）。
- **路径检查**：`enemy.Current()` 返回从根到叶子的激活路径（`[]S`），`enemy.Leaf()` 返回最内层状态，`enemy.In(state)` 检查某状态是否在当前激活路径上，`enemy.TimeIn(state)` 返回该激活状态的停留时间。

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

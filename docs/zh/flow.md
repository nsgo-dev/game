# flow

`game/flow` 提供完全由 `game.Loop` 固定步长 `dt` 驱动的玩法流程原语：游戏时钟计时器 `Timers`、泛型有限状态机 `Machine[S]`、层次状态机 `HSM[S]`、可组合步进序列（`Seq`、`Parallel`、`Race`）以及零全局状态的类型化事件总线 `Bus`。

```go
import "github.com/nsgo-dev/game/pkg/flow"
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
- 调用 `fsm.SetState(to)` 立即切换状态；通过 `fsm.Current()`、`fsm.Previous()`、`fsm.TimeInState()`、`fsm.Started()` 与 `fsm.Step()` 读取状态信息。
- **保存与恢复**：`snap := fsm.Snapshot()` 返回可直接序列化为 YAML 或 JSON 的纯结构体 `MachineSnapshot[S]`（包含 `Version`、`Current`、`Previous`、`TimeInState` 与 `Started`）。`fsm.Restore(snap)` 会先校验 `Current`（以及非零 `Previous`）是否属于当前状态机；遇到未知状态或非法计时时返回 `ErrInvalidSnapshot` 且不修改状态机，并在恢复过程中绝不触发 `Exit` 或 `Enter` 回调。
- **有界转换历史**：`Machine[S]` 默认在环形缓冲区中保留最近 `DefaultHistoryLimit`（32）条转换记录（`TransitionRecord[S]{From, To, Step, Transition}`），可通过 `SetHistoryLimit(n)` 调整上限（`0` 表示关闭记录），通过 `ClearHistory()` 清空。可用 `AddNamedTransition` / `AddNamedAnyTransition` 为转换命名（未命名默认记为 `"from->to"` / `"*->to"`，`SetState` 记为 `"SetState"`），并用 `History()` 或零分配的 `HistoryInto(dst)` 按时间顺序读取。
- **检查与 `statedump` 文本**：每个 `*Machine[S]` 均实现类型擦除的 `MachineInspector` 接口（`Inspect() MachineInspection`、`FormatState() string`），可直接接入 [`game/pkg/tooling/gamedebug`](gamedebug.md) 的 AI 检查面板，或将 `fsm.FormatState()` 写入 [`statedump`](statedump.md) `Values` 以对比不同运行间的状态转换。

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

## 项目全局共享变量与开关（`Vars` 与 `vars.yaml`）

将 `Vars` 作为项目状态的唯一来源。`SetFromExpr` 只有在声明类型和范围允许时才接受表达式值，未在 `vars.yaml` 中声明的名称会返回 `ErrUndeclaredVar`。需要 UI、任务或场景规则同步更新时注册 `OnChange`，并使用 `Snapshot`/`Restore` 创建存档检查点。

在带版本号的 `vars.yaml`（或 `*.vars.yaml`）中统一声明项目全局开关（`bool`）与类型化变量（`int`、`float`、`string`），使对话脚本、任务、场景规则、存档和编辑器远程检查器共享同一份状态：

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/vars.v1.json
version: 1
vars:
  - name: bridge_repaired
    kind: bool
    doc: True once the east bridge has been repaired.
  - name: gold
    kind: int
    default: 50
    min: 0
    max: 9999
    doc: Player gold coins.
  - name: mira_affinity
    kind: float
    default: 0.5
    min: 0
    max: 1
    doc: Relationship score with Mira.
  - name: hero_name
    kind: string
    default: Aria
    doc: Display name of the protagonist.
```

使用 `flow.LoadVarsFS(fsys, "vars.yaml")`（或 `flow.ParseVars(data)`）解析声明，再通过 `flow.NewVars(doc)` 创建运行时存储：

```go
doc, err := flow.LoadVarsFS(assetsFS, flow.VarsFileName)
if err != nil {
	return err
}
vars, err := flow.NewVars(doc)
if err != nil {
	return err
}

// 按注册顺序同步监听变更（空名称监听全部全局变量）：
unsub := vars.OnChange("bridge_repaired", func(ch flow.VarChange) {
	unlockEastBridge(ch.New.(bool))
})
defer unsub()

// 类型化读写严格校验声明的类型与 [min, max] 范围：
_ = vars.SetInt("gold", 120)
gold, _ := vars.Int("gold")

// 按场景对象作用域隔离的独立开关/变量（如“这个宝箱已打开”）：
vars.Self("chest_01").SetBool("opened", true)
opened := vars.Self("chest_01").Bool("opened")
```

- **与对话共享**：向 `dialogue.NewRunner`（[dialogue](dialogue.md)）传入 `dialogue.Options{Vars: vars}`，脚本中的 `<<if>>`、选项守卫、`{expr}` 插值和 `<<set>>` 将直接读写共享存储，而不再使用私有 map。配置 `Vars` 后，脚本中 `<<declare>>` 或 `<<set>>` 未声明的变量名会返回 `flow.ErrUndeclaredVar`，赋值同时受声明类型与范围约束。
- **保存与恢复**：`snap := vars.Snapshot()` 返回可直接序列化为 JSON 或 YAML 的 `flow.VarsSnapshot`（`Version`、`Vars`、`Self`），可直接存入 `save.Store`。`vars.Restore(snap)` 会先原子校验版本号、变量名、类型和范围，校验失败时返回 `flow.ErrInvalidSnapshot` 且不改动原状态，并在恢复期间不触发 `OnChange` 回调。后续版本新增的变量在读取旧存档时自动保持声明默认值，零值 `VarsSnapshot` 会重置为默认值并清空对象独立变量。
- **静态检查**：`go tool game check` 会校验每个 `vars.yaml` / `*.vars.yaml` 文件，并核对 `.yarn` 对话脚本中的每个 `$name` 和 `{name}` 变量引用，对未声明名称报告精确的文件路径与行号。
- **实时远程检查**：将 `Vars: vars`（或 `VarsFunc`）配置到 `mcp.Config` 即可注册 `game_vars_list`、`game_var_get` 与 `game_var_set`；编辑器的远程检查器会实时列出并允许编辑所有已声明变量，同时展示当前活跃的对象独立变量。

## 地图事件（`ParseMapEvent`、`EventRunner`）

地图事件把宝箱、告示牌、门和一次性触发写成 YAML，而不是 Go 代码：一个按顺序排列的页面列表，每页有条件、触发方式和一串命令。

```yaml
version: 1
pages:
  - when:
      self:
        opened: false
    trigger: action
    commands:
      - say:
          text: chest.found_potion
      - wait:
          seconds: 0.5
      - self:
          name: opened
          value: true
  - when:
      self:
        opened: true
    trigger: action
```

- **格式**：`flow.ParseMapEvent` 读取第 1 版（`flow.MapEventVersion`），`flow.EncodeMapEvent` 写出它的规范形式。未知字段和命令、没有命令或有多个命令的列表项、未知触发方式、负数、无穷大或 NaN 的等待时长，以及没有名字的 `set` 或 `self`，都会在加载时被拒绝，并返回指明字段和行号的 `loaderr.Error`。没有写 `trigger` 的页面按 `action` 读取。
- **页面**：`doc.ActivePage(self, switches, variables)` 与 RPG Maker 一样，返回 `when` 条件全部成立的最后一页，没有成立的页面时返回 -1：每个 `self` 和 `switches` 条目等于给定值，每个 `variables` 条目不小于给定值。缺少的名字读作 false 或 0。
- **触发方式**：页面的触发方式是 `action`、`touch`、`autorun` 或 `parallel`（`flow.TriggerAction` 等）。运行器不监听输入或移动，由游戏在触发条件满足时启动该页，例如玩家面对事件按下按键时启动 `action` 页。
- **命令**：`say` 把文本（通常是本地化键）传给 `EventRunner.Say`；`wait` 按游戏时间暂停若干秒；`set` 把整数写入 `EventRunner.Variables`；`self` 把独立开关写入 `EventRunner.Self`。
- **运行**：`flow.NewEventRunner(doc)` 在调用 `Start(page)` 之前处于空闲状态。每个固定步调用 `Update(dt)`，依次执行命令，直到遇到等待或页面结束，从不阻塞循环，运行器空闲后返回 true；等待结束时剩余的时间会继续用于后面的命令。
- **事件中途存档**：`Snapshot` 返回可序列化为 YAML 或 JSON 的 `flow.EventSnapshot`（当前页，空闲时为 -1、下一条命令、等待剩余时间，以及独立开关和变量）。`Restore` 遇到与运行器文档不匹配的快照时返回 `flow.ErrInvalidEventSnapshot`，并保持运行器不变。

目前只有第一批命令。选项、条件分支、物品与传送命令、游戏注册的命令以及与 `Vars` 的集成还不在格式中。

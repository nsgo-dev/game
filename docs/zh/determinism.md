# 确定性指南

确定性模拟意味着：给定相同的初始种子和输入，无论运行多少次，也无论运行在 macOS、iOS、Windows、Linux、Android 还是 Web 上，都会产生完全相同的状态。确定性让回放、每日种子挑战、存档读档一致性，以及在 `nsgotest` 下的自动化玩法测试都稳定可靠。

本指南说明在 NSGo 游戏中保持确定性的核心规则，以及如何使用 `packages/game/random` 编写自动化校验。

## 1. 只用 `game.Loop` 的固定步长 `dt` 推进时间

所有模拟状态的推进都应放在 `game.Loop.Update(dt)` 中，并只使用固定的 `dt`（或整数 tick 计数器）。不要在模拟逻辑中读取 `time.Now()`、`time.Since` 或显示帧之间的真实时间差。

```go
loop := &game.Loop{
	Rate: 60,
	Update: func(dt time.Duration) {
		sim.Tick++
		sim.Step(dt) // dt 始终是 1/60 秒
	},
	Draw: func(alpha float64) {
		// 只做画面插值，绝不修改模拟状态。
		render(sim.Prev, sim.Curr, alpha)
	},
}
```

- `Draw(alpha)` 按显示器刷新率运行，对游戏逻辑状态必须保持只读。
- 冷却时间和计时器应使用整数 tick 或 `dt` 的精确累加，而不是基于系统墙钟时间的截止点。

## 2. 按用途使用带种子的 `random.Stream`

不要在模拟代码中调用 Go 全局的 `math/rand` 或 `math/rand/v2`。全局生成器会在不同子系统之间共享隐藏的可变状态，而且标准库的辅助方法在不同 Go 版本之间也可能改变实现。

改用 `github.com/nsgo-dev/game/packages/game/random`：

```go
import "github.com/nsgo-dev/game/packages/game/random"

streams := random.NewStreams(masterSeed)
ai := streams.Stream("ai")
loot := streams.Stream("loot")
particles := streams.Stream("particles")
```

- 每个 `Stream` 都内置了独立的 SplitMix64 + Xoshiro256\*\* 算法和固定的拒绝采样实现（`Intn`、`IntRange`、`Float64`、`Float32`、`Range`、`Range32`、`Bool`、`Chance`、`Pick`、`WeightedIndex`、`Shuffle`）。同一个种子在所有 Go 版本、操作系统和 CPU 架构（32 位、64 位、ARM64、x64 和 WebAssembly）上产生的比特序列完全一致。
- 通过 `root.Fork("loot")`、`root.Fork("level-1").Fork("spawns")` 或 `streams.Stream("loot")` 按用途派生子流时，既不会推进父流，也不会影响兄弟流。新增粒子特效或调整创建顺序，都不会改变敌人 AI 或战利品掉落。
- 把纯表现层的随机性（粒子、镜头震动、音效微调）放在独立的流里，避免视觉改动影响玩法逻辑。

## 3. 不要依赖 Go map 的遍历顺序

Go 会刻意随机化 `for k, v := range m` 的遍历顺序。如果在对顺序敏感的游戏逻辑中直接遍历 map——例如结算伤害、处理碰撞、生成波次，或在循环内从 `random.Stream` 取随机数——就会导致每次运行结果分叉。

- 在 `Update` 需要遍历的数据结构中，优先使用按创建顺序或稳定实体 ID 排列的切片。
- 如果必须遍历 map，先对键排序：

```go
for _, id := range slices.Sorted(maps.Keys(enemies)) {
	enemies[id].Update(dt, ai)
}
```

## 4. 保持模拟单线程，不依赖 goroutine 调度顺序

所有对模拟状态的修改都应在引擎的 goroutine 上、于 `Loop.Update` 内按顺序执行。

- 不要让多个 goroutine 并发修改游戏状态或共享同一个 `random.Stream`。
- 不要用 `select` 监听多个同时就绪的 channel 来做玩法决策，因为 Go 会在就绪分支中伪随机选择。
- 如果后台 goroutine 承担了寻路或区块生成等计算，应在确定的 tick 边界接收结果，并按规范的稳定键排序后再应用，而不要按完成到达顺序应用。

## 5. 保持浮点数计算顺序一致（并防止 arm64 FMA 漂移）

IEEE-754 浮点加法和乘法不满足结合律：`(a + b) + c` 与 `a + (b + c)` 在最低位上可能不同。此外，Go 语言规范允许编译器在 arm64 上将 `a*b + c` 收缩为单次舍入的融合乘加指令（`FMADD`/`FMSUB`），而 amd64 执行的是两次独立舍入的乘法与加法。经过几百个 tick 后，即使 arm64 与 amd64 之间仅差 1 ULP，也可能跨过碰撞或条件分支的阈值，从而放大成明显分叉。

- 累加力、速度、得分等浮点值时，始终按固定、确定的实体顺序执行。
- 在要求跨 amd64 与 arm64 比特完全一致的自定义模拟代码中，用显式类型转换包裹乘积（`float32(a*b) + c` 或 `float64(a*b) + c`）——Go 规范保证显式转换会禁止 FMA 收缩——并避免在步进路径中直接调用在 arm64 上会生成 FMA 的标准库超越函数（`math.Sin`、`math.Cos`、`math.Atan2`、`math.Hypot`）。
- `packages/game/physics`、`packages/game/physics3d` 和 `packages/game/vehicle` 已在步进模拟路径中全面消除 arm64 FMA 指令，并在 CI 中同时于 Ubuntu amd64 与 macOS arm64 上验证跨架构黄金轨迹哈希。
- 避免在不同路径中混用 `float32` 和 `float64`。
- 计数器、计时器、网格坐标和资源数量优先使用整数或定点表示。

## 6. 存档、回放与回滚时保存并恢复 PRNG 与物理状态

`Stream` 的完整状态（初始种子以及 256 位生成器内部状态）可以通过 `State()` 导出，并通过 `SetState(state)` 恢复。`State` 支持直接使用 `encoding/json` 序列化，读档后会无缝续接完全相同的后续随机序列：

```go
// 保存
data, err := json.Marshal(streams.States())

// 读取
var saved map[string]random.State
if err := json.Unmarshal(data, &saved); err == nil {
	streams.Restore(saved)
}
```

对于单个流，使用 `s.State()` 和 `s.SetState(state)`。回放或重开关卡时，调用 `s.Reset()` 或 `streams.Reset()` 即可把流重置回初始种子起点。

对于物理世界，`physics.World`、`physics.RigidWorld`、`physics3d.World` 以及 `vehicle.Vehicle` 均提供 `Save()`、`Restore(snap)` 和 `Checksum()`，完整捕获所有私有求解器状态（热启动冲量缓存、休眠计时器、上一帧插值姿态、活跃接触/触发器对集合以及关节状态）。快照支持纯文本 JSON 序列化，可直接接入 `net.RollbackSession`（`net.SimState`）：

```go
// 在 net.SimState 实现中：
func (s *MatchSim) Save() []byte {
	data, _ := json.Marshal(s.world.Save())
	return data
}
func (s *MatchSim) Load(b []byte) error {
	var snap physics3d.WorldSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return err
	}
	return s.world.Restore(snap)
}
func (s *MatchSim) Checksum() uint64 { return s.world.Checksum() }
```

## 7. 在 `nsgotest` 测试中验证确定性

`random` 包内置了确定性校验辅助函数，会从头多次运行模拟，并在任意一次运行出现分叉时报错。

用 `AssertDeterministic` 检查完整运行，包括在 `nsgotest` 下驱动真实的 `game.Loop`：

```go
func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestSimulationIsDeterministic(t *testing.T) {
	random.AssertDeterministic(t, 4, func() WorldSnapshot {
		streams := random.NewStreams(42)
		world := NewWorld(streams)
		loop := &game.Loop{
			Rate:   60,
			Update: func(dt time.Duration) { world.Step(dt) },
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			loop.Run(cx)
			return core.View{}
		})
		app.Advance(2 * time.Second)
		return world.Snapshot()
	})
}
```

当多步模拟出现分叉时，用 `CheckSteps` 或 `AssertDeterministicSteps` 精确定位第一次发生分叉的步骤编号并输出两份状态：

```go
func TestStepByStepDeterminism(t *testing.T) {
	random.AssertDeterministicSteps(t, 300, func() func(step int) WorldSnapshot {
		world := NewWorld(random.NewStreams(42))
		dt := time.Second / 60
		return func(step int) WorldSnapshot {
			world.Step(dt)
			return world.Snapshot()
		}
	}, nil)
}
```

运行测试：

```sh
go tool nsgo test ./...
```

如果 `AssertDeterministicSteps` 失败，它会报告第一个分叉的步数以及 `run A`、`run B` 各自的状态，方便直接定位引入非确定性的那一帧逻辑。

# replay

`game/replay` 将固定步长下的游戏输入以增量格式记录到 YAML 文本文件中，并支持确定性回放，用于问题复现、幽灵竞速（ghost）和自动化回归测试。

```go
import "github.com/nsgo-dev/game/packages/game/replay"
```

## ActionSource 抽象

让游戏模拟逻辑面向 `ActionSource` 接口编程（`Pressed`、`JustPressed`、`JustReleased`、`Axis`、`Step`）：

```go
type ActionSource interface {
	Pressed(name string) bool
	JustPressed(name string) bool
	JustReleased(name string) bool
	Axis(name string) float64
	Step()
}
```

`*input.Actions` 满足实时游玩场景，`*replay.Recorder` 透明包装任意 `ActionSource` 完成录制，`*replay.Player` 满足回放场景——你的游戏更新循环在三种模式下无需任何改动。

## 录制实时游玩

用 `NewRecorder` 包装 `*input.Actions`，传入随机种子、固定步长频率（`<= 0` 默认 `60`）以及需要监听的动作与轴名称：

```go
rec := replay.NewRecorder(
	actions,
	seed,
	60,
	[]string{"jump", "attack"},
	[]string{"move_x"},
)

// 用 rec 代替 actions 传入模拟循环：
loop := &game.Loop{
	Rate: 60,
	Update: func(dt time.Duration) {
		sim.Step(rec, dt) // 在开头调用一次 rec.Step()
	},
}

// 导出快照并保存为 YAML 文本
if err := replay.SaveFile("runs/boss-bug.replay.yaml", rec.Recording()); err != nil {
	return err
}
```

`Recording` 采用增量编码：只有当监听的动作按下状态或轴数值相比上一步发生变化时，才会向 `Events` 追加记录，长时间对局也能保持很小体积。内存编解码使用 `rec.Encode()` 与 `replay.Decode`，读写文件使用 `replay.SaveFile` 与 `replay.LoadFile`。

## 录像文件

录像是 YAML 文本文件，可以直接阅读、在评审中查看 diff，也可以手工编辑。文件头包含格式版本、随机种子、步频、监听的动作与轴名称以及总步数；其后每个输入发生变化的步占一行，写明按下和松开的动作以及轴的新值：

```yaml
version: 2
seed: 99537266
rate: 60
actions: [left, right, jump, dash]
axes: [move_x]
total_steps: 254
steps:
  - {step: 6, press: [right], axes: {move_x: 1}}
  - {step: 39, press: [left], release: [right], axes: {move_x: -1}}
  - {step: 85, press: [jump, dash]}
  - {step: 95, release: [jump, dash]}
```

步序号从 0 开始，必须按递增顺序排列且小于 `total_steps`。动作从按下它的那一步起保持按下，直到某一步将其松开；轴的值保持不变，直到后面的某一步设置新值。编辑后的文件会按编辑后的内容回放：把一次按下移到另一步、修改轴的值或新增一行，`LoadFile` 和 `Player` 都会照此执行。

`Decode` 会拒绝未知字段、缺少 `seed` 或 `total_steps` 的文件头、缺少 `step` 的行、文件头中不存在的名称以及多余的 YAML 文档，并在错误中指出出错的步或行。`Encode` 对同一份录像总是写出相同的字节，因此加载它写出的文件再保存不会产生任何变化；写成 `-0.0` 的轴值按 `0` 读取。手工加入的注释和块风格条目可以正常读取，但程序再次保存时会写成标准的单行形式，不保留这些内容。约定的扩展名是 `.replay.yaml`。早期紧凑 JSON 形式的版本 1 已不再读取。

## 回放 Recording

加载录像并用 `NewPlayer(recording)` 驱动同一套游戏模拟：

```go
recording, err := replay.LoadFile("runs/boss-bug.replay.yaml")
if err != nil {
	return err
}

player := replay.NewPlayer(recording)
sim := newGameSim(player.Seed())

for !player.Done() {
	sim.Step(player, recording.StepDuration())
}
```

通过 `player.StepIndex()`、`player.TotalSteps()` 和 `player.Done()` 查看回放进度，调用 `player.Reset()` 回到第 0 步。回放结束（`Done() == true`）后，再次调用 `Step()` 会释放最后一步仍按住的动作（触发一次 `JustReleased`），随后保持全部输入空闲。

## 自动化回归测试（`Run` 与 `AssertEndState`）

将录制好的通关或复现文件提交到 `testdata/`，在 `go test` 中用 `replay.Run` 和 `replay.AssertEndState` 校验确定性结果：

```go
func TestLevel1ClearReplay(t *testing.T) {
	rec, err := replay.LoadFile("testdata/level1-clear.replay.yaml")
	if err != nil {
		t.Fatal(err)
	}

	want := SimState{Score: 1200, Cleared: true}
	replay.AssertEndState(t, rec, want, nil, func(seed uint64, src replay.ActionSource) (func(time.Duration), func() SimState) {
		sim := newGameSim(seed)
		return func(dt time.Duration) {
				sim.Step(src, dt)
			}, func() SimState {
				return sim.Snapshot()
			}
	})
}
```

`Run` 会以 `rec.StepDuration()` 精确执行 `rec.TotalSteps` 次步进并返回最终状态；`AssertEndState` 用自定义的 `equal`（为 `nil` 时使用 `reflect.DeepEqual`）比对 `want`，不一致时输出清晰诊断信息并令测试失败。

若要检查完整状态而不是手选的结构体，可用 [`statedump.Run`](statedump.md) 把状态转储为文本，再与黄金文件比较。

## 在 CI 中回放录像

`go tool game replay` 无界面回放项目的录像：它先检查给定目录下的每个 `*.replay.yaml` 文件都能回放，再运行名字中含 `Replay` 的测试，例如上面的 `TestLevel1ClearReplay`，录像损坏、某次回放的结束状态与测试期望的不同，或者没有任何测试通过时失败，这样命名不对的测试不会让录像无人回放。见[命令行](cli.md#replay)。

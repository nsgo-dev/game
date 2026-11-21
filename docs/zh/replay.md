# replay

`game/replay` 将固定步长下的游戏输入以紧凑的增量格式记录到版本化文件中，并支持确定性回放，用于问题复现、幽灵竞速（ghost）和自动化回归测试。

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

// 导出快照并保存（以 ".gz" 结尾时自动进行 gzip 压缩）
if err := replay.SaveFile("runs/boss-bug.json.gz", rec.Recording()); err != nil {
	return err
}
```

`Recording` 采用增量编码：只有当监听的动作按下状态或轴数值相比上一步发生变化时，才会向 `Events` 追加记录，长时间对局也能保持很小体积。内存编解码可使用 `rec.Encode()` / `rec.EncodeGzip()` 与 `replay.Decode` / `replay.DecodeGzip`，读写文件使用 `replay.SaveFile` / `replay.LoadFile`（自动识别普通 JSON 与 gzip）。

## 回放 Recording

加载录像并用 `NewPlayer(recording)` 驱动同一套游戏模拟：

```go
recording, err := replay.LoadFile("runs/boss-bug.json.gz")
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
	rec, err := replay.LoadFile("testdata/level1-clear.json.gz")
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

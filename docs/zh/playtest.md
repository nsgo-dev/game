# 自动化试玩（Playtest）

用 `playtest.yaml` 定义自动化试玩，通过确定性输入录制无头运行多组种子，并将测得的指标与游戏目标对比。

```go
import "github.com/nsgo-dev/game/packages/game/playtest"
```

## `playtest.yaml`

`playtest.yaml` 是带版本号的纯文本 YAML 文件，并发布了 JSON Schema（`https://game.nsgo.dev/schemas/playtest.v1.json`）。未知字段、缺失或不支持的版本号、重复的指标名以及非法目标都会被拒绝；`EncodeConfig` 写出规范的确定性 YAML，加载后再保存字节完全一致。

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/playtest.v1.json
version: 1
scope:
  project: dungeon-run
  scene: level_01
  template: platformer2d
runs:
  count: 20
  seedStart: 1
  rate: 60
  speed: 1
  maxDuration: 1m30s
bot:
  profile: platformer-runner
  strategy: Greedy route to the door, jumps at ledges and gaps, 120-220 ms reaction jitter
  reactionMin: 120ms
  reactionMax: 220ms
  params:
    jumpHoldMs: 220
metrics:
  - name: finished
    label: Finished
    measure:
      kind: outcome
      key: finished
      aggregate: count
    goal:
      summary: most first-timers clear floor 1
      source: brief.yaml
      min: 16
  - name: median_time
    label: Median time
    unit: s
    measure:
      kind: timer
      key: clear_time
      aggregate: median
    goal:
      summary: under a minute
      source: brief.yaml
      min: 40
      max: 60
  - name: frame_time
    label: Frame time
    unit: ms
    measure:
      kind: perf
      key: frame_ms
      aggregate: median
    goal:
      summary: 16.7 ms at 60 Hz
      source: game.yaml
      max: 16.7
  - name: coins_taken
    label: Coins taken
    unit: '%'
    measure:
      kind: ratio
      key: coins_taken
      denomKey: coins_total
      aggregate: mean
    goal:
      summary: every coin on a route
      source: playtest.yaml
      min: 100
  - name: deaths
    label: Deaths
    measure:
      kind: event
      key: death
      aggregate: sum
      heatMap: true
```

### 字段说明

- **`scope`**：项目名 `project`、必填的场景名 `scene`，以及可选的模板名 `template`。
- **`runs`**：运行次数 `count`、可选显式种子列表 `seeds`、顺序种子起点 `seedStart`（默认为 `1`）、固定步频 `rate`（默认 `60`）、倍速元数据 `speed`（默认 `1`），以及单次运行的模拟时长上限 `maxDuration`（默认 `60s`）。
- **`bot`**：机器人配置名 `profile`（例如 `platformer-runner`）、可选的策略描述 `strategy`、人类反应抖动区间 `reactionMin` / `reactionMax`，以及可选的标量参数 `params`。
- **`metrics`**：每项指标有唯一的 `name`、可选的显示名称 `label` 与单位 `unit`、测量方式 `measure` 及可选目标 `goal`：
  - `measure.kind` 可选 `event`、`counter`、`timer`、`outcome`、`ratio`（需提供 `denomKey`）或 `perf`（`frame_ms`、`update_ms`、`draw_ms`、`p50_frame_ms`、`p95_frame_ms`、`p99_frame_ms`、`max_frame_ms`、`allocs_per_frame`）。
  - `measure.aggregate` 可选 `count`、`sum`、`mean`、`median`、`min`、`max`、`p95`、`p99` 或 `rate`。
  - 在 `event` 指标上设置 `measure.heatMap: true` 会把所有事件坐标收集到报告的 `heatMaps` 字段中。
  - `goal` 指定来源文件 `source`（如 `brief.yaml`、`game.yaml` 或 `playtest.yaml`）、可选的摘要 `summary`，以及 `min`、`max`（或区间）或 `equal`。

## 在 Go 代码中上报指标

每次运行时，游戏会收到一个 `*playtest.Collector`，用来上报带坐标的事件、计数器、计时器和结果。所有方法都支持 `nil` 接收者，因此游戏代码无需区分实机运行和自动化试玩：

```go
metrics.StartTimer("clear_time")
metrics.SetCounter("coins_total", float64(len(coins)))

// 拾取金币时：
metrics.Event("pickup", float64(cx), float64(cy), coin.ID)
metrics.Inc("coins_taken")

// 死亡时：
metrics.Event("death", float64(px), float64(py), "slime_pit")

// 通关或失败时：
metrics.StopTimer("clear_time")
metrics.Finish() // 或 metrics.Fail("out of lives")
```

帧耗时来自性能读数：在 `playtest.Sim` 上传入 `FrameStats: stats` 和 `GC: gcMon`（或调用 `metrics.RecordPerf(stats, gcMon)`），其均值和分位计算方式与 `game/mcp` 的 `game_perf_readout` 一致。

## 内置机器人：`platformer-runner`

`platformer-runner`（`playtest.ProfilePlatformerRunner`）沿水平方向贪心奔向 `PlatformerObservation.ExitX`，在遇到台阶、坑洞和危险时，以每个种子独立的 `random.Stream` 在 `[reactionMin, reactionMax]` 内采样的确定性反应抖动起跳。使用 `playtest.ObservePlatformerGrid` 可以从 `physics.TileGrid` 和危险碰撞箱构造 `PlatformerObservation`。

可以用 `playtest.RegisterBot(name, factory)` 注册更多机器人配置。

## 无头运行与回放

直接调用 `playtest.Run(cfg, makeSim, opts)`，或在测试中调用 `playtest.RunTest` 供 `go tool game playtest` 驱动：

```go
func TestLevel1Playtest(t *testing.T) {
	playtest.RunTest(t, "playtest.yaml", makeSim)
}
```

随后在项目目录运行：

```sh
go tool game playtest
go tool game playtest -config playtest.yaml -out playtest.report.yaml -recordings playtest-recordings
go tool game playtest -check
```

每个种子的运行都会通过 `replay.Recorder` 记录输入并写出 `<recordings>/run_NNN_seed_<seed>.replay.yaml`，因此任意一次运行都可以用 `replay.NewPlayer` 确定性回放或转为 `game replay` 回归测试。输出的 YAML 报告（`playtest.report.yaml`，`version: 1`）包含每次运行的结果、各项指标值、目标达成状态（`met`、`missed`、`info`）、热力图坐标以及录制文件路径。

## 编辑器 MCP 工具

编辑器的 MCP 服务器（`game/editor/editormcp`，通过 `go tool game mcp` 接入）提供两个试玩工具：

- `game_playtest`（`run`）：校验 `playtest.yaml`，无头运行机器人并录制回放，写出 YAML 报告，返回结构化报告和汇总文本。
- `playtest_define`（`edit`）：校验提议的新指标，生成规范化的 `playtest.yaml` 差异并作为待审查提案排队（`Server.PlaytestReviews`、`Server.ApplyPlaytestReview`、`Server.DismissPlaytestReview`），在用户确认前不会改写磁盘上的 `playtest.yaml`。

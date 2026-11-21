# 线上运营（Live operations）

```go
import "github.com/nsgo-dev/game/packages/game/liveops"
```

`liveops` 为 NSGo 游戏提供与具体服务商无关的线上运营基础能力。它只依赖 Go 标准库、`packages/game` 和 NSGo 的公开 `core` API，不内置任何第三方崩溃上报、埋点分析或广告 SDK。

## 范围决策（[#194](https://github.com/nsgo-dev/game/issues/194)）

| 能力 | 状态 | 说明 |
| --- | --- | --- |
| **崩溃报告**（[#195](https://github.com/nsgo-dev/game/issues/195)） | 纳入范围 | 与服务商无关的 panic 捕获，附带固定大小步长环形缓冲区、堆栈、元数据和可选状态快照。 |
| **统计分析**（[#198](https://github.com/nsgo-dev/game/issues/198)） | 纳入范围 | 与服务商无关、严格以显式同意为前提的事件 API，同意状态保存在 `core.Storage`。未显式同意前不会发送也不会缓存任何事件。 |
| **云存档**（[#196](https://github.com/nsgo-dev/game/issues/196)） | 纳入范围（接口） | 当前提供与平台无关的 `CloudSync` 接口；iCloud、Play Games、Steam Cloud 等内置适配器等待平台云存档绑定就绪。 |
| **广告**（[#199](https://github.com/nsgo-dev/game/issues/199)） | 不纳入核心层 | 广告网络（AdMob、AppLovin 等）依赖专有原生 SDK，并伴随繁重的商店政策与隐私合规义务，不放入核心 `game` 模块；需要广告的游戏可直接通过 NSGo 嵌入原生 SDK 视图。 |

## 崩溃报告

`CrashReporter` 用预分配的环形缓冲区保存最近若干次模拟步长（`DefaultHistorySize` = 120，稳态零堆分配），并为 `game.Loop` 回调或任意函数提供 panic 防护。

- `RecordStep(dt, summary)` 与 `RecordStepData(dt, summary, data)` 向环形缓冲区追加一条 `StepRecord` 并返回单调递增的步数。
- `WrapLoop(l)` 返回带防护的 `*game.Loop`；`GuardLoop(l)` 原地包装 `l.Update` 和 `l.Draw`。如果 `Update` 在单步内未自行调用 `RecordStep`，防护层会自动记录一条 `"update"` 步长（包含触发 panic 的那一步）。
- `RunSafe(fn)` 防护任意回调并报告是否发生 panic。
- 发生 panic 时，报告器构造可 JSON 序列化的 `CrashReport`（包含 `Timestamp`、`Panic`、`Message`、`Stack`、`Step`、按时间从旧到新排列的 `LastSteps`、来自 `CaptureState` 的可选 `State` 以及 `Metadata` 副本），并交给 `Reporter`。
- 需要上报后继续抛出 panic 时，可设置 `Rethrow: true`。

```go
reporter := liveops.NewCrashReporter(liveops.CrashOptions{
	HistorySize:  120,
	Reporter:     func(r liveops.CrashReport) { uploadOrSave(r) },
	CaptureState: func() any { return world.Snapshot() },
	Metadata:     map[string]string{"version": appVersion, "scene": "level-1"},
	Now:          cx.Now,
})

loop := reporter.WrapLoop(&game.Loop{
	Update: func(dt time.Duration) {
		reporter.RecordStep(dt, "world.step")
		world.Step(dt)
	},
	Draw: func(alpha float64) { world.Draw(alpha) },
})
loop.Run(cx)
```

## 统计分析

`Analytics` 要求玩家显式同意（`ConsentGranted`）后才分发任何事件，且不内置任何第三方埋点后端。

- 同意状态为 `ConsentUnknown`（默认）、`ConsentDenied` 和 `ConsentGranted`。
- `NewAnalytics(store, key, provider)` 把同意状态持久化到 `core.Storage` 的 `key` 下（为空时使用 `DefaultConsentKey`）；缺失或无法识别的值一律按默认关闭处理（`ConsentUnknown`）。`NewMemoryAnalytics(provider)` 仅在内存中保存状态。
- 处于 `ConsentUnknown` 或 `ConsentDenied` 时，`Track(name, props)` 直接丢弃事件并返回 `false`，绝不发送，也不缓存供后续补发。
- 处于 `ConsentGranted` 时，`Track` 对 `props` 做防御性拷贝，并通过 `AnalyticsProvider.Send` 分发 `Event`。
- 调用 `SetConsent(ConsentDenied)` 撤回同意后，所有后续 `Track` 和 `Flush` 调用会立即停止。

```go
analytics := liveops.NewAnalytics(cx.Storage(), "liveops.consent", myProvider)
analytics.SetClock(cx.Now)

// 未显式同意前直接丢弃：
analytics.Track("app_open", nil) // 返回 false，发送 0 条事件

// 玩家同意后：
_ = analytics.SetConsent(liveops.ConsentGranted)
analytics.Track("level_complete", map[string]any{"level": 1})
```

## 云存档

`CloudSync` 定义了与服务商无关的存档槽位接口（`Load`、`Save`、`Delete`、`List`，以及 `ErrSlotNotFound` 和 `ErrCloudUnavailable` 哨兵错误）。游戏目前可以针对自有后端或内存测试替身实现该接口；iCloud、Google Play Games 和 Steam Cloud 的平台适配器等待平台云存档绑定就绪（[#196](https://github.com/nsgo-dev/game/issues/196)）。

# GC 与帧分配预算

60 fps 下单帧预算为 16.67 ms（120 fps 为 8.33 ms）。Go 的垃圾回收器采用并发标记且 STW（Stop-The-World）停顿较短，但如果每帧持续产生堆分配，仍会频繁触发 GC 周期、消耗后台 CPU（并引发 mark assist），进而导致肉眼可见的掉帧卡顿。保持更新与绘制热路径“每帧零分配”，是避免游戏过程中产生 GC 抖动的最有效手段。

## 调整 `GOGC` 与 `GOMEMLIMIT`

Go 提供两个核心 GC 参数，可通过环境变量或 `runtime/debug` 配置：

- **`GOGC`**（`debug.SetGCPercent`，默认 `100`）：控制在下一次 GC 触发前允许新增的堆内存占当前存活堆的百分比。
  - 较小值（`50`–`100`）可压低内存占用，但会提高 GC 频率。
  - 较大值（`200`–`400`，或在配合 `GOMEMLIMIT` 时设为 `off`）可显著减少游玩期间的 GC 次数，前提是稳态帧分配接近于零。
- **`GOMEMLIMIT`**（`debug.SetMemoryLimit`，默认不限制）：为 Go 运行时设置软内存上限。
  - 将 `GOMEMLIMIT` 与较高的 `GOGC` 搭配使用，既能避免无谓的回收，也能防止超出平台内存上限。
  - 切勿将 `GOMEMLIMIT` 设置得低于存活资源与场景的工作集大小，否则 GC 会因持续逼近上限而频繁抖动。

```go
import rdebug "runtime/debug"

func configureGC() {
	// 示例：设置 256 MiB 软上限并放宽 GC 触发比例。
	rdebug.SetMemoryLimit(256 << 20)
	rdebug.SetGCPercent(200)
}
```

## 热路径零分配准则

保持 `Loop.Update`、`Loop.Draw` 和 `Canvas.Draw` 回调在稳态下不发生堆分配：

1. **预分配并复用切片与对象池。** 将切片保存在结构体字段中，每帧通过 `s = s[:0]` 清空复用，避免重复 `make`。
2. **避免在热循环中调用 `fmt.Sprintf` 或拼接字符串。** 仅在数值变化时格式化 HUD 文本，或在受状态变化驱动的响应式文本回调中格式化。
3. **避免每帧创建捕获变量的闭包和接口装箱。** 优先使用预分配结构体的方法及值类型令牌（例如 `debug.ScopeToken`），避免每帧分配闭包或装箱为 `any`。
4. **复用 `sprite.Batch` 与定长环形缓冲区。** 粒子、弹幕与采样数据应使用固定容量缓冲区，而非每帧动态扩容缩容。
5. **在场景切换间隙加载与释放资源。** 使用 `debug.MemoryTracker` 按分组统计资源占用，切换关卡时调用 `ReleaseGroup`，并在加载界面显式调用 `runtime.GC()`，避免在游玩帧中集中回收。
6. **发布构建使用 `-tags release`。** `pkg/debug` 中的全部调试功能在发布构建下都会编译为零开销空桩。

## 检测 GC 卡顿与 CI 回归检查

### 运行时覆盖层（`GCMonitor`）

将 `debug.NewGCMonitor` 传入 `debug.Options.GC`，每帧以零稳态分配采样 `runtime/metrics`（`/gc/pauses:seconds`、`/gc/cycles/total:gc-cycles`、`/memory/classes/heap/objects:bytes`、`/gc/heap/allocs:objects`、`/gc/heap/allocs:bytes`）：

```go
gcMon := debug.NewGCMonitor(debug.FrameHistory)
overlay := debug.New(debug.Options{
	GC: gcMon,
})
```

当某帧内发生 GC 周期或停顿时，该帧会被标记为 `GCFrameSample.Hitched == true`，并直接在调试覆盖层的帧耗时柱状图上高亮标出，同时显示累计周期数、最近/最大停顿、存活堆大小以及近期卡顿帧数。

每个 `GCFrameSample` 还记录该帧内分配的堆对象数与字节数（`AllocsDelta`、`AllocBytesDelta`）。`gcMon.AllocsPerFrame()` 与 `gcMon.AllocBytesPerFrame()` 给出它们在保留历史内的平均值，浮层在 GC 摘要下以 `allocs 12.0/f (1.5 KB/f)` 的形式显示。稳定游玩时这个值始终大于零，说明某条热路径每帧都在分配。在加载画面或切换关卡之后调用 `gcMon.Reset()`，即可丢弃历史，从此刻重新计量。

### CI 分配回归检查（`AssertZeroAllocs`）

在单元测试中使用 `debug.AssertZeroAllocs`（封装 `testing.AllocsPerRun`），防止后续改动在热路径引入意外的堆分配：

```go
func TestPhysicsStepZeroAllocs(t *testing.T) {
	world := newTestWorld()
	// 先预热一帧，完成一次性缓存初始化。
	world.Step(16 * time.Millisecond)

	debug.AssertZeroAllocs(t, 100, func() {
		world.Step(16 * time.Millisecond)
	})
}
```

需要记录具体分配次数或断言非零上限时，可使用 `debug.MeasureAllocs`。

## 各平台注意事项

| 目标平台 | 内存与 GC 注意事项 |
| --- | --- |
| **桌面端**（macOS、Windows、Linux） | 内存较充裕且多核后台标记能力强；适度提高 `GOGC`（如 `200`）并设置合理的 `GOMEMLIMIT`，有助于保持 60–120 fps 稳定帧步调。 |
| **iOS** | 不同机型的 jetsam 内存上限非常严格（尤其是低内存设备）。设置 `GOMEMLIMIT` 时，必须扣除 Go 堆之外的原生 GPU 纹理与已解码音频缓冲占用，留足安全余量。 |
| **Android** | 机型间可用内存与温控降频差异大，低核心设备上后台标记协程容易与渲染争抢 CPU。务必保持每帧零分配并保守设定 `GOMEMLIMIT`。 |
| **WebAssembly**（`js/wasm`） | 单线程执行环境下 GC 无法分摊到其他 CPU 核心，且线性内存只增不减。降低每帧分配对避免帧率抖动和浏览器标签页内存压力尤为关键。 |

> **真机性能数据说明：** 桌面端、iOS、Android 与 WebAssembly 的真机 GC 停顿与延迟基线尚未在实体硬件上采集记录。本文中的配置建议基于 Go 运行时机制总结，后续完成真机基准测试后将进一步校准具体参数。

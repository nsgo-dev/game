# tween

`game/tween` 使用 NSGo 的 `core.Curve` 缓动曲线与具名缓动函数，在 `game.Loop` 固定步长上确定性地驱动数值、坐标、颜色、自定义类型、时间轴以及 `tilemap.Camera` 运镜补间。

```go
import "github.com/nsgo-dev/game/pkg/tween"
```

## 创建与推进 Tween

使用 `Float32`、`Float64`、`Point`、`Color` 或泛型 `New[T]` 创建 `*Tween`，可链式配置 `Delay`、`Repeat` 和 `OnComplete`，再通过 `Update(dt)` 或 `Group` 推进：

```go
var group tween.Group

popup := tween.Float32(0, 1, 300*time.Millisecond, core.EaseOut, func(v float32) {
	banner.ScaleX, banner.ScaleY = v, v
}).Delay(100 * time.Millisecond).
	OnComplete(func() { bannerReady = true })

bob := tween.Float32(0, -6, 600*time.Millisecond, core.EaseInOut, func(y float32) {
	coin.Y = baseY + y
}).Repeat(tween.RepeatForever, true) // 无限往返（yoyo）

group.Add(popup, bob)

// 在 game.Loop.Update 中：
group.Update(dt) // 完成的补间会自动移除
```

- **缓动曲线**：零值 `core.Curve` 默认为 `core.Linear`。
- **`Repeat(count, yoyo)`**：在首轮之后额外重复 `count` 次（`0` 表示只播一次；`tween.RepeatForever` 或任意负数表示无限重复）。`yoyo` 为 `true` 时，奇数轮反向从 `to` 插值回 `from`。
- **跳转与重置**：`tw.Seek(at)` 跳转到指定时间点并立即应用该时刻的值（不触发 `OnComplete`）；`tw.Reset()` 倒回起点并清除延迟捕获的起始状态（使 `To`、`By`、`FromCurrent` 与相机补间在下一次运行时重新读取当前值，而 `Seek(0)` 会保留已捕获的起始值以保证拖拽回放的确定性）。可通过 `tw.Done()` 与 `tw.Duration()` 查询状态。
- **自定义类型**：用 `tween.New[T](from, to, d, curve, lerp, set)` 对任意结构体或向量做补间。

## 具名缓动与延迟读取起始值

`tween.Ease` 是普通的缓动函数类型，提供完整的具名集合：sine、quad、cubic、quart、quint、expo、circ、back、elastic、bounce、spring 十一个族，每族都有 `In`、`Out`、`InOut`、`OutIn` 四个变体（`tween.BackOut`、`tween.ElasticOut` 等）。名称与资源曲线关键帧的 `interp` 值一致，动画剪辑与代码共享同一套缓动。

`To`、`By`、`FromCurrent` 在步骤真正运行时（而不是构建时）延迟读取起始值，因此每一步总是从前一步留下的结果继续：

```go
// 先弹出放大，再向上漂移 40 点——无需手写起始值。
coin := tween.Sequence().
	Then(tween.To(&coin.Scale, 1.4, 80*time.Millisecond, tween.BackOut)).
	With(tween.By(&coin.Y, -40, 300*time.Millisecond, tween.QuadOut, nil, nil)).
	Build()
```

- `tween.To(&v, target, d, ease)` 将 `*v` 动画到 `target`。
- `tween.By(&v, delta, d, ease, add, lerp)` 将 `*v` 动画到步骤开始时的值加上 `delta`；内置类型（`float32`、`float64`、`core.Point`、`core.Color`）传 `nil, nil` 即可，自定义类型需要提供 `add` 和 `lerp`。
- `tween.FromCurrent(get, set, target, d, ease)` 通过 `get` 读取起始值，适用于不是普通字段的值。
- `To` 和 `FromCurrent` 接受可选的 `lerp` 参数用于自定义类型；省略时保持起始值并在结束时跳到目标值，与 `New` 一致。
- 回绕时间轴（或对补间调用 `Reset`）会清除已捕获的起始值，下一次运行重新读取。

## 用 Sequence 串联步骤

`Sequence` 无需手工计算起始时间即可构建 `Timeline`：`Then` 把步骤接到前一块之后，`With` 让步骤与最后一步并行，`Wait` 插入间隔，`Call` 在游标处安排回调，`Loops` 设置循环次数：

```go
seq := tween.Sequence().
	Then(tween.To(&coin.Scale, 1.4, 80*time.Millisecond, tween.BackOut)).
	With(tween.By(&coin.Y, -40, 300*time.Millisecond, tween.QuadOut, nil, nil)). // 与最后一步并行
	Wait(100 * time.Millisecond).
	Then(tween.FromCurrent(getGlow, setGlow, 0, 200*time.Millisecond, tween.ElasticOut)).
	Call(func() { score.Add(1) }).
	Loops(3)

var group tween.Group
group.AddTimeline(seq.Build())
group.SetSpeed(0.25) // 慢动作
group.SetPaused(true)

// 在 game.Loop.Update 中：
group.Update(dt)
```

- `With` 与最后一个 `Then` 的步骤同时开始；游标推进到并行步骤中较长的那个的结束点。
- `Loops(n)`：`n <= 0` 无限循环，`n == 1` 播放一次，更大的 `n` 额外回绕 `n-1` 次。每次循环都完全重播所有步骤，保持确定性。
- `Timeline.SetLoops(n)` 在任意时间轴上同样有效，`Timeline.SetSpeed(s)` 缩放其步长；`Group.SetSpeed` 与 `Group.SetPaused` 统一缩放或冻结组内所有补间与时间轴（用于慢动作或随游戏暂停）。`Group.Bind(cx)` 在作用域结束时清空补间组。

## 使用 Timeline 编排多轨动画

`Timeline` 在同一条确定性时钟上协调多段补间与定时回调，支持播放、暂停、倒放、循环和精确拖拽（`Seek`）：

```go
tl := tween.NewTimeline().
	Add(0, tween.Float32(0, 1, 400*time.Millisecond, core.EaseOut, setFade)).
	Append(tween.Point(startPos, endPos, 800*time.Millisecond, core.EaseInOut, setPos)).
	Call(600*time.Millisecond, func() { playSfx("whoosh") })

tl.Play()

// 在 game.Loop.Update 中：
tl.Update(dt)
```

- `Add(at, tw)` 将 `tw` 放到偏移量 `at` 处；`Append(tw)` 将 `tw` 追加到时间轴当前的 `Duration()` 末尾。
- `Call(at, fn)` 在播放跨越时间戳 `at` 时触发 `fn`（调用 `Seek` 时不触发）。
- 通过 `Play()`、`Pause()`、`Restart()`、`Reverse(bool)`、`SetLoop(bool)` 或 `SetLoops(n)`（按次数循环）、`SetSpeed(s)` 和 `Seek(at)` 控制回放；`Seek` 会把每条轨道（无论处于区间前、中、后）精确设到 `at` 时刻对应的状态。

## 相机运镜辅助函数

内置相机补间直接操作 `*tilemap.Camera`，并在设置了 `cam.Bounds` 时自动限位：

```go
// 平移到 Boss 区域、拉近镜头，最后平滑切回跟随玩家
intro := tween.NewTimeline().
	Add(0, tween.CameraPan(cam, boss.X, boss.Y, 900*time.Millisecond, core.EaseInOut)).
	Add(200*time.Millisecond, tween.CameraZoom(cam, 1.5, 700*time.Millisecond, core.EaseOut)).
	Append(tween.CameraZoom(cam, 1.0, 500*time.Millisecond, core.EaseInOut)).
	Append(tween.CameraFollowTransition(cam, func() core.Point {
		return player.Pos
	}, 600*time.Millisecond, core.EaseInOut))

intro.Play()
```

可用的相机补间构造函数：

| 辅助函数 | 动画属性 |
| --- | --- |
| `CameraPan(cam, toX, toY, d, curve)` | 将 `cam.X`、`cam.Y` 从起始位置平移至 `(toX, toY)`。 |
| `CameraZoom(cam, toZoom, d, curve)` | 将 `cam.Zoom`（`<= 0` 视为 `1`）缩放至 `toZoom`。 |
| `CameraRotate(cam, toRot, d, curve)` | 将 `cam.Rotation`（弧度）旋转至 `toRot`。 |
| `CameraMove(cam, toX, toY, toZoom, toRot, d, curve)` | 同时驱动位置、缩放与旋转。 |
| `CameraFollowTransition(cam, targetFn, d, curve)` | 从相机起始位置平滑过渡到持续移动的 `targetFn()`，结束时精确落在该目标上。 |

每个相机补间在首次运行时捕获相机当前的位置、缩放或旋转。调用 `Reset()` 会清除已捕获的起始状态，使重播时从相机当前状态出发；而 `Seek(0)` 会保留已捕获的起始状态，使拖拽与时间轴重启保持确定性。

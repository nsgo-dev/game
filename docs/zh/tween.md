# tween

`game/tween` 使用 NSGo 的 `core.Curve` 缓动曲线，在 `game.Loop` 固定步长上确定性地驱动数值、坐标、颜色、自定义类型、时间轴以及 `tilemap.Camera` 运镜补间。

```go
import "github.com/nsgo-dev/game/packages/game/tween"
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
- **跳转与重置**：`tw.Seek(at)` 跳转到指定时间点并立即应用该时刻的值（不触发 `OnComplete`）；`tw.Reset()` 倒回起点。可通过 `tw.Done()` 与 `tw.Duration()` 查询状态。
- **自定义类型**：用 `tween.New[T](from, to, d, curve, lerp, set)` 对任意结构体或向量做补间。

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
- 通过 `Play()`、`Pause()`、`Restart()`、`Reverse(bool)`、`SetLoop(bool)` 和 `Seek(at)` 控制回放；`Seek` 会把每条轨道（无论处于区间前、中、后）精确设到 `at` 时刻对应的状态。

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

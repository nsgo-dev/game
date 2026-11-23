# Loop

`game.Loop` 在 NSGo 作用域的显示帧上驱动固定步长更新与插值绘制，并在应用进入后台时自动暂停。

```go
import "github.com/nsgo-dev/game/packages/game"
```

## 固定步长如何工作

不同显示器的刷新率各不相同（60 Hz、90 Hz、120 Hz 或可变刷新率）。如果直接拿每帧原始时间差推进物理逻辑，跳跃高度和碰撞结果会随刷新率变化。

`Loop` 把逻辑模拟和画面绘制解耦：

```go
type Loop struct {
	Rate     int                    // 每秒更新次数，0 表示 60
	MaxSteps int                    // 单帧最多运行的更新次数，0 表示 max(1, Rate/10)
	Update   func(dt time.Duration) // 按固定步长 dt（始终为 1/Rate 秒）推进游戏状态
	Draw     func(alpha float64)    // 绘制这一帧，alpha 在 [0, 1) 之间
}

func (l *Loop) Run(cx *core.Scope) (stop func())
```

在 NSGo 的 `RequestAnimationFrame` 触发每个显示帧时：

1. 循环把距上一帧经过的时间累加到累加器中。
2. 只要累加器里还包含至少一个完整步长（`time.Second / time.Duration(Rate)`），就减去一个步长并调用一次 `Update(step)`。单个显示帧可能运行零次、一次或多次更新。
3. 当剩余时间不足一个完整步长时，以 `alpha = float64(acc) / float64(step)`（范围 `[0, 1)`）调用一次 `Draw(alpha)`。

`Run` 每一帧都会读取 `Rate`、`MaxSteps`、`Update` 和 `Draw`，所以你可以在循环运行期间随时替换回调或调整频率。

## 用 alpha 做插值

当显示刷新率不能被 `Rate` 整除时（例如在 120 Hz 显示器上跑 60 Hz 模拟），直接绘制 `Update` 的最新状态会产生肉眼可见的抖动，因为有的显示帧跑 0 次更新，有的跑 1 次。

做法是在 `Update` 中保存上一步和当前步的位置，在 `Draw` 中把 `alpha` 写入 `core.Signal[float64]`，并在 `core.Canvas.Draw` 中插值：

```go
func Ball(cx *core.Scope) core.Node {
	var x, last float32
	alpha := core.NewSignal(cx, 0.0)

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			last = x
			x += 120 * float32(dt.Seconds())
		},
		Draw: func(a float64) {
			alpha.Set(a)
		},
	}
	loop.Run(cx)

	return core.Canvas{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1},
		Draw: func(d *core.Drawing, _ core.Size) {
			at := last + (x-last)*float32(alpha.Get())
			d.SetFillColor(core.RGB(0xff5a1f)).FillRect(at, 40, 16, 16)
		},
	}
}
```

由于只有 `Canvas.Draw` 读取了 `alpha`，每次更新它只会触发画布重绘，不会重新计算或提交外层视图树。

## 卡顿保护与 MaxSteps

如果某一帧耗时远超平常（例如调试器断点、单帧过重或设备卡顿），试图一次性补跑所有落下的步长会让下一帧更慢，陷入“死亡螺旋”。

`MaxSteps` 限制单个显示帧最多执行多少次 `Update`：

- 当 `MaxSteps <= 0` 时，默认值为 `max(1, Rate/10)`，即 0.1 秒内的步数（60 Hz 下为 6 步，120 Hz 下为 12 步）。
- 当单帧达到 `MaxSteps` 上限时，循环会丢弃超出的完整步长（`acc %= step`）并直接进入 `Draw`。此时游戏会短暂变慢，而不是彻底卡死。

如果物理计算较重，可以显式设一个更小的上限（例如 `MaxSteps: 3`）：

```go
loop := &game.Loop{
	Rate:     60,
	MaxSteps: 3,
	Update:   stepWorld,
	Draw:     renderWorld,
}
```

## 帧时间

每帧累加的是它的时间戳与上一帧时间戳之差，原样采用 `RequestAnimationFrame` 给出的值。平台为显示帧打时间戳时，这就是显示器自己的帧时间，所以一帧的长度是显示器的帧长，而不取决于引擎何时处理到这一帧；其他平台上则是图时钟的时间。循环开始或恢复后的第一帧累加从那一刻到它时间戳的时间；显示器以帧将显示的时刻打时间戳时，这个时间戳可能领先一帧，因此循环的时间就是显示出来那一帧的时间。时间戳早于循环开始或恢复时刻的帧不累加任何时间，因此恢复运行永远不会让累加器倒退，下一帧则从那一刻起累加。

设置 `FrameTime` 可以在本帧的更新之前读取每帧的帧长，即它累加的时间，不累加时间的帧传入 0；[quality](quality.md) 的 `Selector` 这类帧时间统计就读取它。从后台回来的第一帧从窗口重新显示时开始计时，不包含离开的时间；`FrameTime` 中也可以调用 `Run` 返回的 `stop`，这会在任何更新之前结束本帧。设置了 `Paused` 时它同样会被调用，只想统计实际模拟的帧的调用方需要自己跳过这些帧。

```go
loop := &game.Loop{
	FrameTime: func(dt time.Duration) { selector.Observe(dt) },
	Update:    stepWorld,
	Draw:      renderWorld,
}
```

## 后台暂停与停止

`Loop.Run` 会监听 `cx.Env().Lifecycle()`：

- **`core.LifecycleBackground`**（桌面端窗口最小化、隐藏或被完全遮挡；iOS/Android 应用退到后台）：循环取消挂起的帧请求，不再运行任何更新。
- **`core.LifecycleActive` 或 `core.LifecycleInactive`**（窗口可见，包括桌面端失焦但可见）：循环照常运行。从后台切回前台时，它会把时钟锚点重置为 `cx.Now()`，离开期间不会触发任何补帧更新。

以下任一情况发生时，循环会永久结束：

- 作用域 `cx` 被销毁（例如切换页面，或用 `core.Show` 隐藏组件）；
- 调用 `Run` 返回的 `stop` 函数。

重复调用 `stop()` 是安全的，在 `Update` 内部调用同样安全：如果 `Update` 停止了循环，当前帧后续的更新和 `Draw` 都不会再执行，应用前后台切换也不会重启它。

```go
var stop func()
loop := &game.Loop{
	Update: func(dt time.Duration) {
		if gameOver {
			stop()
		}
	},
	Draw: func(alpha float64) {
		frame.Set(alpha)
	},
}
stop = loop.Run(cx)
```

## 手机上的沉浸模式

设置 `Immersive` 后，游戏进行时会隐藏手机的系统栏，进入菜单时再恢复。循环调用 NSGo 的 `mobile.Immersive`：Android 隐藏状态栏和导航栏，从屏幕边缘滑动时会短暂显示；iOS 隐藏状态栏并让主屏幕指示条自动淡出；HarmonyOS 隐藏状态栏、导航栏和导航条。桌面端和 Web 目标会忽略此设置。

```go
loop := &game.Loop{
	Immersive: true,
	Update:    stepWorld,
	Draw:      renderWorld,
}
loop.Run(cx)

// 打开暂停菜单后，下一帧显示系统栏；关闭菜单后再次隐藏。
openMenu := func() { loop.SetPaused(true) }
closeMenu := func() { loop.SetPaused(false) }
```

- 循环运行且未处于 `Paused` 时隐藏系统栏；处于 `Paused`、调用 `stop` 或 `cx` 被销毁时恢复系统栏，因此替换掉游戏视图的菜单页面同样会让系统栏回来。
- 循环在启动或从后台返回时隐藏系统栏，`Immersive` 或 `Paused` 的变化在下一帧生效（若由 `Update` 暂停，则在同一帧生效），且只在状态改变时才向平台发出请求。应用处于后台时，系统栏保持原样。
- 隐藏和显示系统栏可能改变平台报告的安全区边距（例如 HarmonyOS 在系统栏隐藏期间只保留屏幕挖孔区域），因此请用 [`screen.Viewport.SafeGameRectForEnv`](screen.md#移动端-hud-安全区) 布局 HUD，它会随边距变化而更新。
- 同一窗口中的循环共享系统栏：只要有任何设置了 `Immersive` 的循环在进行游戏，系统栏就保持隐藏，直到最后一个这样的循环暂停或结束时才恢复。在上一个场景的循环仍然存活时启动自己循环的场景（例如场景过渡期间），在旧循环被销毁后系统栏仍保持隐藏；下方场景暂停自己的循环时，被推入场景的循环也会让系统栏保持隐藏。以 NSGo 页面或对话框显示的菜单也可以设置 `StatusBar: core.StatusBarSetting{Immersive: core.ToggleOff}`，在显示期间覆盖窗口的设置。

## 使用 nsgotest 测试

`nsgotest` 在虚拟时钟上以确定性的方式运行显示帧（每 16.667 ms 一帧，即每秒 60 帧），因此无需 `time.Sleep` 就能测试游戏循环、`MaxSteps` 卡顿和后台暂停行为。

```go
package mygame_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestLoopSimulation(t *testing.T) {
	updates := 0
	l := &game.Loop{
		Rate:     60,
		MaxSteps: 4,
		Update: func(dt time.Duration) {
			updates++
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})

	// Advance 按帧推进一整秒。
	app.Advance(time.Second)
	if updates != 60 {
		t.Fatalf("got %d updates, want 60", updates)
	}

	// FastForward 在单帧内跨越 500 ms，用来测试 MaxSteps 上限。
	app.FastForward(500 * time.Millisecond)
	if updates != 64 {
		t.Fatalf("got %d updates after hitch, want 64", updates)
	}

	// 进入后台时循环暂停，恢复后不补跑更新。
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	app.Advance(time.Second)
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	if updates != 64 {
		t.Fatalf("got %d updates while backgrounded, want 64", updates)
	}
}
```

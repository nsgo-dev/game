# input

`game/input` 将触摸区域、鼠标按键、指针锁定下的鼠标相对移动与键盘按键映射为命名动作（action）和轴（axis），在每个 `game.Loop` 固定步长采样一次，并支持通过 `core.Storage` 跨启动持久化。

```go
import "github.com/nsgo-dev/game/packages/game/input"
```

与 `core.View` 回调和 `game.Loop.Update` 一样，`Actions` 的所有方法都运行在引擎 goroutine 上。

## 定义动作与轴

`Actions` 把逻辑名称（如 `"jump"`、`"left"`、`"right"` 或 `"move_x"`）绑定到一个或多个物理 `Input` 触发器。零值开箱即用，也可以通过 `input.NewActions()` 创建：

```go
actions := input.NewActions()

// 虚拟画布坐标（例如 320×180）下的触摸区域
actions.Bind("left",
	input.TouchRegion(core.Bounds{X: 0, Y: 100, Width: 70, Height: 80}),
	input.Key("left"),     // 按名称，随键盘布局而定
	input.KeyCode("KeyA"), // 按物理位置
)
actions.Bind("right",
	input.TouchRegion(core.Bounds{X: 70, Y: 100, Width: 70, Height: 80}),
	input.Key("right"),
	input.KeyCode("KeyD"),
)
actions.Bind("jump",
	input.TouchRegion(core.Bounds{X: 220, Y: 100, Width: 100, Height: 80}),
	input.MouseButton(core.MouseButtonLeft),
	input.Key("space"),
)
actions.Bind("interact",
	input.MouseRegion(core.Bounds{X: 260, Y: 10, Width: 50, Height: 30}, core.MouseButtonLeft),
)

// 将一对反向动作组合为 [-1, 1] 的轴
actions.BindAxis("move_x", "left", "right")
```

物理 `Input` 构造函数：

| 构造函数 | 触发条件 |
| --- | --- |
| `TouchRegion(rect)` | 任意按下的触摸手指落在 `rect` 内（按 `core.Bounds.Contains` 判定）。 |
| `MouseButton(btn)` | 鼠标按键 `btn` 在已挂载的视图上处于按下状态。 |
| `MouseRegion(rect, btn)` | 鼠标按键 `btn` 按下且指针位于 `rect` 内。 |
| `Key(name)` | 名为 `name` 的按键处于按住状态，名称与 `core.KeyEvent.Key` 一致（`"a"`、`"space"`、`"left"`、`"shift"`），匹配时不区分大小写。字母随键盘布局而定。 |
| `KeyCode(code)` | 物理位置 `code` 上的按键处于按住状态，名称与 `core.KeyEvent.Code` 一致（`"KeyW"`、`"ArrowLeft"`），在任何布局下都是同一个键。 |

调用 `Bind(name, ...)` 会替换 `name` 原有的绑定；不传任何输入或调用 `Unbind(name)` 可解绑。可通过 `Bindings(name)` 与 `AxisBinding(name)` 查询当前绑定，通过 `UnbindAxis(name)` 解绑轴。

## 挂载到视图与坐标映射

使用 `Attach` 将 `Actions` 挂载到游戏的 `core.View` 上。`Attach` 会链式保留视图上已有的事件回调，不会覆盖它们：

```go
view := core.View{
	Style: core.Style{FlexGrow: 1, FlexShrink: 1},
	Children: []core.Node{gameCanvas},
}
actions.Attach(&view)
```

也可以手动转发单个事件回调：`OnTouch`、`OnPressDown`、`OnPressUp`、`OnMouseMove`、`OnMouseLeave`、`OnContextMenu`、`OnPointerDelta`、`OnKeyDown` 和 `OnKeyUp`。

- `OnPressDown` 会忽略键盘按压以及有触摸手指处于活动状态时系统合成的指针按压，确保触摸只触发 `TouchRegion`。
- `OnContextMenu` 将右键点击记录为 `ev.Position` 处持续一个步长的 `core.MouseButtonRight` 脉冲。
- `OnMouseLeave` 在指针离开视图时清空按住的鼠标按键。

## 接收按键

视图只有在自身拥有键盘焦点时才能收到按键，而点击只会按下视图、不会让它获得焦点，因此游戏通常在整个窗口上接收按键。在运行循环的 scope 中调用一次 `WatchKeys`：

```go
actions.WatchKeys(cx)
loop.Run(cx)
```

`WatchKeys` 跟随 NSGo 的 `core.KeysDown` 与 `core.KeyCodesDown`，直到 scope 被销毁或调用返回的 `stop`。在文本输入框中键入的按键和输入法组字中的按键永远不算按住。

`Attach` 同样会挂上 `OnKeyDown` 和 `OnKeyUp`，它们在视图自身拥有键盘焦点时接收按键，例如桌面上设置了 `Focusable` 的游戏视图。`OnKeyDown` 会消费绑定到动作的按键，使快捷键映射（keymap）的绑定以及视图自身的 Return 和 Space 不再处理它；未绑定的按键照常传递。按住按键产生的自动重复不改变任何状态。

无论使用哪种方式：

- 按住的按键在每个步长都保持其动作的 `Pressed`，直到按键抬起；在两个步长之间按下并抬起的按键与轻点一样被锁存一个步长。
- 用 `BindAxis` 把绑定到一对反向按键的两个动作组合为轴：按住负向键时 `Axis` 为 `-1`，按住正向键时为 `+1`，两者都按或都不按时为 `0`。
- 窗口失去键盘焦点时，所有按住的按键都会抬起，并在下一个步长触发 `JustReleased`，玩家切到其他窗口时不会有动作卡住。

### 将视图坐标映射到虚拟画布

当游戏通过带黑边的 [`screen.Viewport`](screen.md) 绘制时，设置 `MapPoint` 即可让 `TouchRegion` 和 `MouseRegion` 在虚拟游戏坐标下判定：

```go
actions.MapPoint = func(p core.Point) (core.Point, bool) {
	return viewport.ViewToGame(windowSize, p)
}
```

当 `MapPoint` 返回 `false`（例如点在黑边区域）时，该点会被区域判定忽略。

## 在固定步长中采样

在每次 `game.Loop.Update` 开头调用一次 `actions.Step()`，随后使用 `Pressed`、`JustPressed`、`JustReleased` 和 `Axis` 读取当前步长的输入快照：

```go
loop := &game.Loop{
	Rate: 60,
	Update: func(dt time.Duration) {
		actions.Step()

		vx := float32(actions.Axis("move_x")) * 140
		if actions.JustPressed("jump") {
			startJump()
		}
		if actions.JustReleased("jump") {
			cutJumpShort()
		}
		movePlayer(vx, dt)
	},
	Draw: func(alpha float64) {
		frame.Set(alpha)
	},
}
```

- **轻点锁存（Tap latching）**：在两个固定步长之间完成按下并抬起的快速点击会被锁存，确保在下一个步长仍能触发一次 `Pressed` / `JustPressed`，并在再下一步触发 `JustReleased`。
- **`Axis(name)`**：仅负向动作按下时返回 `-1`，仅正向动作按下时返回 `+1`，两者都不按或同时按下时返回 `0`。若 `name` 未绑定为轴，则回退为同名动作按下时返回 `1`、否则返回 `0`。
- **`Reset()`**：清空活动触摸、按住的鼠标键与按键、待处理的锁存、鼠标相对移动与当前步长快照，同时保留所有绑定和指针锁定，因此切换场景后鼠标视角仍然有效。请在失焦、切后台或切换场景时调用，防止按键卡死。

## 通过指针锁定实现鼠标视角

第一人称相机可以把指针轴绑定到鼠标的相对移动，在游戏视图被按下时锁定指针，并用 `Delta` 读取每一步的移动量：

```go
var gameRef core.Ref

actions.BindPointerAxis("yaw", input.PointerX, input.PointerAxisOptions{Sensitivity: 0.003})
actions.BindPointerAxis("pitch", input.PointerY, input.PointerAxisOptions{Sensitivity: 0.003, Invert: true})

view := core.View{
	Ref: &gameRef,
	OnPress: func(core.PressEvent) {
		actions.LockPointer(cx, &gameRef)
	},
}
actions.Attach(&view)

// 在 Loop.Update 中，actions.Step() 之后：
camera.Yaw += actions.Delta("yaw")
camera.Pitch += actions.Delta("pitch")
```

- `PointerX` 读取向右的移动，`PointerY` 读取向下的移动，单位与视图的 `OnPointerDelta` 报告的一致。`Sensitivity` 对其缩放（为零时取 1），`Invert` 取反。
- `Delta(name)` 返回自上一次 `Step` 以来经缩放的移动量，不做截断，因此快速甩动比缓慢移动让相机转得更多。`PointerDelta()` 返回这一步未经缩放的 `(dx, dy)`。`Axis(name)` 把经缩放的移动量与绑定到同名 `name` 的其他轴相加，并把总和截断到 `[-1, 1]`，适合配合较小的 `Sensitivity` 做转向。
- 视图只在持有锁定时才收到移动：`Attach` 会把它的 `OnPointerDelta` 转发给 `actions.OnPointerDelta`。
- 当 `cx.Env().CanLockPointer()` 为 false 或视图已被移除时，`LockPointer(cx, &ref)` 返回 `false` 且不做任何事。请在每次按下时调用它，因为浏览器只在用户手势期间授予锁定；再次锁定会替换之前的锁定。Wayland 下的 Linux 需要支持指针约束与相对移动的合成器。
- `UnlockPointer()` 释放锁定，平台会在原来的位置重新显示指针。释放前收到的移动仍会计入下一步。`Reset()` 不会释放锁定。
- 按下 Escape、窗口失去焦点、隐藏或移除视图同样会释放锁定，但 NSGo 不会通知应用。因此 `PointerLockRequested()` 只报告 `actions` 是否持有一个尚未被 `UnlockPointer` 释放的请求，而不是平台是否仍持有锁定，请不要用它来决定是否重新锁定或显示恢复提示。下一次按下会重新锁定。

指针轴会与其他绑定一起保存和加载。

## 保存与加载绑定

使用 `Save` 和 `Load` 将自定义按键绑定以 JSON 形式持久化到 `core.Storage`：

```go
// 保存当前所有动作和轴绑定
if err := actions.Save(cx.Storage(), "controls.v1"); err != nil {
	// 可处理 *core.StorageFullError
	log.Println("save controls:", err)
}

// 启动时恢复（键不存在时保留默认绑定）
found, err := actions.Load(cx.Storage(), "controls.v1")
if err != nil {
	log.Println("corrupt saved controls, keeping defaults:", err)
}
_ = found
```

当 `key` 不存在时，`Load` 返回 `(false, nil)` 并保留现有默认绑定；若存储内容不是有效 JSON 或格式版本未知，则返回 `(false, err)` 且不改动当前绑定。

# input

`game/input` 将触摸区域、鼠标按键、指针锁定下的鼠标相对移动、键盘按键与游戏手柄映射为命名动作（action）和轴（axis），在每个 `game.Loop` 固定步长采样一次，并支持通过 `core.Storage` 跨启动持久化。

```go
import "github.com/nsgo-dev/game/pkg/input"
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
| `GamepadButton(btn)` | `btn` 在 `Actions` 读取的手柄上按下（见下文“游戏手柄”）。 |

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

### 将视图坐标映射到虚拟画布

当游戏通过带黑边的 [`screen.Viewport`](screen.md) 绘制时，设置 `MapPoint` 即可让 `TouchRegion` 和 `MouseRegion` 在虚拟游戏坐标下判定：

```go
actions.MapPoint = func(p core.Point) (core.Point, bool) {
	return viewport.ViewToGame(windowSize, p)
}
```

当 `MapPoint` 返回 `false`（例如点在黑边区域）时，该点会被区域判定忽略。

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
- `Reset` 会释放所有按住的按键；此时仍按着的按键会保持释放状态，直到它抬起后再次按下。

挂载的视图在按键按住期间离开视图树时，该按键会一直保持按住，因为 NSGo 不再把它的抬起事件发给这个视图：替换游戏视图时请调用 `Reset`，或改用不依赖视图的 `WatchKeys`。

`Key` 接受 `core.KeyEvent.Key` 报告的名称，不区分大小写。`core.KeyHeld` 额外接受的别名，例如 `"mod"`、`"cmd"` 或 `"option"`，永远不算按住；在 `Env.AppleKeys` 表明 Command 是平台修饰键的平台上绑定 `"meta"`，其他平台绑定 `"ctrl"`。

## 游戏手柄

用 `GamepadButton` 像其他 `Input` 一样绑定手柄按键，用 `BindGamepadAxis` 绑定摇杆轴，再调用一次 `WatchGamepads`，此后每次 `Step` 都会通过 NSGo 的 [`gamepad`](https://github.com/nsgo-dev/nsgo/tree/main/gamepad) 包读取连接到应用的手柄，支持所有具备手柄的目标平台（macOS、iOS、Windows、Linux、Android 和 Web）：

```go
actions.Bind("jump", input.GamepadButton(gamepad.ButtonSouth), input.TouchRegion(jumpRect))
actions.Bind("left", input.GamepadButton(gamepad.ButtonDPadLeft))
actions.Bind("right", input.GamepadButton(gamepad.ButtonDPadRight))
actions.BindAxis("move_x", "left", "right")

// 左摇杆与十字键驱动同一个轴，使用默认的径向死区
actions.BindGamepadAxis("move_x", gamepad.AxisLeftX, input.GamepadAxisOptions{})
// 右摇杆向上推时读数为正
actions.BindGamepadAxis("look_y", gamepad.AxisRightY, input.GamepadAxisOptions{DeadZone: 0.2, Invert: true})

actions.WatchGamepads(cx)
```

按键采用 W3C 标准手柄布局；按键值达到 0.5 即视为按下，因此模拟扳机 `ButtonLT` 与 `ButtonRT` 按到一半时触发。`Axis` 会把摇杆轴与同名的其他绑定（`BindAxis`、`BindMotionAxis`、`VirtualPad` 摇杆）相加，并将结果限制在 `[-1, 1]`。

| `GamepadAxisOptions` 字段 | 作用 |
| --- | --- |
| `DeadZone` | 偏移量低于该值时轴读数为 0，超出部分重新缩放，使读数从 0 平滑升到 1。`0` 使用 `DefaultGamepadDeadZone`（0.15），负值关闭死区。 |
| `Axial` | 仅对该轴单独应用死区。默认对整根摇杆应用径向死区，斜推时方向保持不变。 |
| `Sensitivity` | 在限制范围之前乘以经过死区处理的值（`<= 0` 时为 1）。 |
| `Invert` | 取反。 |

默认情况下一个 `Actions` 读取所有已连接的手柄：任一手柄按下某键即视为按下，摇杆轴取离中心最远的读数。调用 `SelectGamepad(id)`（传入 `gamepad.Pads` 中的 ID）可将单个 `Actions` 限制为只读取某一只手柄，`SelectAnyGamepad` 恢复读取所有手柄；多玩家场景请使用下文的 `Players`。手柄断开时会释放它按住的输入，传给 `WatchGamepads` 的作用域销毁后停止读取。

手柄每个步长轮询一次，因此在两个步长之间完成的按下和松开不会被察觉。测试和回放可以不读取实际手柄，改用 `OnGamepads` 传入 `[]gamepad.Pad` 快照，它会像触摸一样锁存步长之间的按下；`Gamepads()` 返回最近一次读取的状态。

## 屏幕触控控件

在手机上，玩家需要拇指底下的控件。`TouchControls` 按 YAML 文档 `TouchLayout` 绘制一层控件，驱动的命名动作和轴与键盘、手柄相同，所以无论玩家拿的是什么，游戏都读 `Pressed("jump")` 和 `Axis("move")`：

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/touchlayout.v1.json
version: 1
reference: {width: 844, height: 390}
maxScale: 1.5
idleAfter: 4s
idleAlpha: 0.45
controls:
  - id: move
    kind: dpad
    anchor: bottomLeft
    offset: {x: 10, "y": 8}
    size: {width: 120, height: 120}
    label: Move
    ways: 8
    left: left
    right: right
  - id: jump
    kind: button
    anchor: bottomRight
    offset: {x: 36, "y": 32}
    size: {width: 110, height: 110}
    label: Jump
    action: jump
    text: JUMP
```

```go
//go:embed controls.touch.yaml
var touchYAML []byte

layout, err := input.ParseTouchLayout(touchYAML)
if err != nil {
	return err
}
touch, err := input.NewTouchControls(cx, layout)
if err != nil {
	return err
}
touch.BindActions(actions)
touch.Haptics = input.NewHaptics(cx)

actions.Attach(&gameView)
touch.Attach(&gameView) // 在 Actions.Attach 之后

// 在 Loop.Update 中，actions.Step() 之后：
touch.Update(dt)

// 在覆盖整个视图的游戏 Canvas 的 Draw 中：
touch.Resize(size, cx.Env().SafeArea())
touch.Draw(d)

// 从没有绘制控件的菜单开始或回到游戏时：
touch.Wake()
```

这一层负责：

- **摆放。** 每个控件贴在安全区的九个锚点之一（`topLeft`、`top`、`topRight`、`left`、`center`、`right`、`bottomLeft`、`bottom`、`bottomRight`），再按 `offset` 向内移动。偏移和尺寸以 `reference` 大小的屏幕上的点为单位，按安全区宽、高与参考尺寸之比中较小的一个缩放，设置了 `maxScale` 时不超过它，因此布局在任何屏幕上都保持比例。控件不会小于 `MinTouchTarget`（44 点），也不会超出安全区。把 `Env.SafeArea`，或在屏幕、模态框占去一部分安全区时把视图作用域的 `core.Insets` 传给 `Resize`；控件使用视图的点坐标，因此不受 `Actions.MapPoint` 影响。
- **显示与隐藏。** `Env.Pointer` 为 `core.PointerCoarse`（手机和平板）时控件一开始就显示，其他设备上先隐藏。在挂载的视图上第一次触摸，或按下某个控件，就显示控件；按键或手柄会再次隐藏它们，来源包括视图的按键事件，以及 `Update` 时 `WatchKeys` 读到的按键和 `WatchGamepads` 读到的手柄。`Tracker` 返回背后的 `DeviceTracker`，`SetVisible` 可直接覆盖；无论哪种方式改变显示状态，下一次按键或手柄操作仍会隐藏控件，下一次触摸仍会显示控件。
- **闲置淡出。** 设置了 `idleAfter` 时，手指离开控件超过这段时间后控件淡到 `idleAlpha`，下次按下时恢复完全不透明。`idleAlpha` 为默认的 0 时控件会完全淡出视野，但仍接收触摸，也仍向读屏软件报告自己。无论游戏是否在绘制控件，闲置时间都在累计，所以控件重新出现时（例如从菜单回到游戏）要调用 `Wake`，否则它们一出现就已经是淡出状态。
- **左手模式。** `mirrored: true`，或运行时调用 `SetMirrored(true)`，把每个控件在屏幕左右两侧互换；方向含义不变。
- **触觉反馈。** 设置了 `Haptics` 时，每次按下都会轻触一下：在按钮上按下、手指滑到另一个按钮上，或方向键摇到新方向。
- **无障碍。** `Draw` 把每个控件命名为画布节点，带上它的 `label`、按钮的按钮角色，以及测试 ID `touch-` 加上它的 `id`，读屏软件和 `nsgotest` 都能找到它们，与透明度无关。`Controls` 列出每个控件的 ID、种类、标签和位置。
- **玩家编辑。** `Layout` 返回布局，`SetLayout` 应用编辑后的布局，`Save` 和 `Load` 以同样的 YAML 存进 `core.Storage`；保存的布局解析失败时，`Load` 保持现有布局不变，并返回指明该键的 `*loaderr.Error`。

### 控件种类

| 种类 | 构建出 | 字段 |
| --- | --- | --- |
| `button` | `VirtualButton` | `action`（必填）、`text` 或 `icon`、`mode`（`hold`、`toggle`、`tap`）、`shape`（`circle`、`rect`） |
| `dpad` | `VirtualDPad` | `up`、`down`、`left`、`right` 动作，`axisX` 和 `axisY` 轴，`ways`（4 或 8），`deadZone` |
| `stick` | 设置了 `UpPositive` 的 `VirtualStick` | `axisX` 和 `axisY` 轴，`deadZone`，`floating`，`radius` |

所有种类都接受 `id`、`kind`、`anchor`、`offset`、`size` 和 `label`；上面三种会拒绝它们不接受的字段。按钮模式：

- `hold`：手指按在按钮上时按下动作。
- `toggle`：每次按下切换一次，手指抬起后保持，直到下次按下或 `Reset`。
- `tap`：每次按下只让动作按下一个 `Actions.Step`，与手指停留多久无关。

方向键整体归一根手指所有：拇指不抬起就能在方向间摇动、滑动，滑出圆形后仍继续控制。`ways: 8` 时斜向会同时按下两个方向。`axisY` 向上为 `+1`，与手柄方向键一致。在这一层里，手指从一个按钮滑到另一个按钮上会按下后者，拇指因此可以在相邻按钮间摇动；切换按钮不会以这种方式被按下。

### 插件控件种类

包可以在 `init` 函数中用 `RegisterTouchControlKind` 加入自己的种类。它的工厂函数拿到控件的 `TouchControlSpec`，读取用到的通用字段，并从 `options` 读取自己的设置，返回一个 `TouchControl`：也由 `TouchControls` 摆放的 `VirtualControl`。

```go
func init() {
	input.RegisterTouchControlKind("acme.wheel", func(spec input.TouchControlSpec) (input.TouchControl, error) {
		steer := spec.Options["axis"]
		if steer == "" {
			return nil, loaderr.Errorf("options.axis", "name the axis the wheel drives", "a wheel needs options.axis")
		}
		return &wheel{axis: steer, action: spec.Action}, nil
	})
}
```

```yaml
  - id: wheel
    kind: acme.wheel
    anchor: bottomLeft
    size: {width: 160, height: 160}
    label: Steering wheel
    options: {axis: steer, gain: "1.5"}
```

`options` 的值都是字符串。YAML 会读成数字、布尔值或 null 的值要加引号，如上面的 `gain`；`ParseTouchLayout` 会在所在行拒绝没加引号的值，因为它会被读成字符串、保存时又加上引号，从而改动文件。

`VirtualControl` 包含 `Claim`（接管在某点按下的手指）、`Move` 和 `Release`（跟随手指直到抬起）、`Reset`、`ActionNames`、`ActionPressed` 和 `AxisValues`（它驱动的内容），以及 `Draw`；`TouchControl` 再加上 `Place`，接收 `TouchControls` 算出的位置和缩放。`ParseTouchLayout` 接受任何种类，所以布局可以在定义其种类的插件导入之前解析；`NewTouchControls` 拒绝没有注册的种类，并把工厂返回的 `*loaderr.Error` 报告到该控件自己的字段上。`RegisterTouchIcon` 加入按钮的 `icon` 可以引用的图标，内置的有 `arrowUp`、`arrowDown`、`arrowLeft` 和 `arrowRight`。两者遇到空名称或重复名称都会 panic。

两个可选接口让插件控件得到内置种类拥有的能力：

- `ButtonControl` 加上 `Button() *VirtualButton`，用于底层是一个 `VirtualButton` 的控件，例如自绘按钮或带冷却的按钮。`VirtualPad` 会直接驱动这个按钮：在它上面按下的手指优先于摇杆和其他控件被接管，可以和相邻按钮之间来回滑动，`ButtonTap` 的按下在一步之后结束；`TouchControls.Button` 返回它，`Draw` 把它命名为按钮。`VirtualPad` 不会为它的手指调用控件的 `Claim`、`Move` 和 `Release`。
- `AccessibleControl` 加上 `Accessibility() (core.Role, bool)`，即 `Draw` 赋给该控件画布节点的角色和选中状态，取代按钮或分组的默认值，例如 `core.RoleSwitch` 以及它是否打开。

### 不用布局的虚拟控件

`TouchControls` 构建的部件也可以单独放在 `VirtualPad` 上使用；`VirtualPad` 把每根手指交给它按下时所在的控件，直到手指抬起：

```go
pad := input.NewVirtualPad()
pad.SlideBetweenButtons = true
pad.AddStick(&input.VirtualStick{AxisXName: "move_x", AxisYName: "move_y", UpPositive: true, Zone: leftHalf, Radius: 56, Floating: true})
pad.AddControl(&input.VirtualDPad{Center: dpadCenter, Radius: 70, Ways: 8, Left: "left", Right: "right"})
pad.AddButton(&input.VirtualButton{Center: jumpCenter, Radius: 48, Label: "JUMP", Action: "jump"})
pad.AddButton(&input.VirtualButton{Center: crouchCenter, Radius: 36, Label: "C", Action: "crouch", Mode: input.ButtonToggle})
pad.BindActions(actions)
```

- `VirtualStick` 的 `AxisY` 按屏幕坐标向下为正，除非设置了 `UpPositive`；设置它，触屏摇杆就能和手柄反转后的摇杆共用一个轴名。
- 普通 `VirtualPad` 默认关闭 `SlideBetweenButtons`，`TouchControls` 中则默认开启。
- 每次按下都会调用 `OnPress`，适合触觉反馈。
- `ButtonTap` 按钮的按下在下一次 `Actions.Step` 结束，因此需要把 pad 绑定到某个 `Actions`。

## 本地多人

`Players` 管理多个玩家槽位（`0` 到 `Len()-1`），每位玩家拥有各自的 `Actions`：每只已连接的手柄最多路由给一位玩家，键盘与指针事件也只转发给已分配对应设备的玩家：

```go
players := input.NewPlayers(2)
for _, p := range players.All() {
	a := p.Actions()
	a.Bind("jump", input.GamepadButton(gamepad.ButtonSouth))
	a.Bind("left", input.GamepadButton(gamepad.ButtonDPadLeft))
	a.Bind("right", input.GamepadButton(gamepad.ButtonDPadRight))
	a.BindAxis("move_x", "left", "right")
	a.BindGamepadAxis("move_x", gamepad.AxisLeftX, input.GamepadAxisOptions{})
}

players.AssignGamepad(0, pad1ID)
players.AssignGamepad(1, pad2ID)
players.WatchGamepads(cx)

// 在 Loop.Update 中：
players.Step()
p0Move := players.Player(0).Axis("move_x")
p1Jump := players.Player(1).JustPressed("jump")
```

- **设备隔离与手柄独占**：`AssignGamepad(index, padID)` 将手柄 `padID` 独占分配给玩家 `index`（若其他玩家原先持有 `padID`，会先解除其分配并释放按住的按键）。在手柄连接前预分配 ID 会记住该槽位，`p.GamepadConnected()` 与 `p.Gamepad()` 用于查询已分配手柄当前是否在线。
- **同键盘分键位与混合设备**：为一位或多位玩家调用 `AssignKeyboard(index)`，并在 `Players` 上调用 `WatchKeys(cx)`（或 `Attach(&view)`）。多位玩家可以共享同一把键盘并使用互不冲突的键位（例如玩家 0 用 WASD + 空格、玩家 1 用方向键 + 回车），也可以让一位玩家用键盘、另一位玩家用手柄——即使两位玩家套用同一套绑定模板，仅分配手柄的玩家也不会响应键盘事件。调用 `AssignPointer(index)` 可将触摸、鼠标和指针锁定事件路由给指定玩家（例如分屏触摸区域或玩家 0 使用键鼠）。
- **自动分配与按键加入大厅**：调用 `AssignUnassignedGamepads()` 可按手柄 ID 升序将未分配的已连接手柄依次分配给尚未分配设备的空槽位（`!p.HasDevice()`）；也可以结合 `UnassignedGamepads()` 与 `NextUnassigned()` 实现“按任意键加入”的大厅流程。使用 `Add()` 动态新增槽位，使用 `UnassignGamepad`、`UnassignKeyboard`、`UnassignPointer` 或 `Unassign` 解除分配。
- **单步轮询、重置与持久化**：`players.Step()` 每步仅轮询一次 `gamepad.Pads` 并步进所有玩家的 `Actions`；`players.Reset()` 重置所有玩家状态并保留槽位、绑定与设备分配；测试和回放可通过 `players.OnGamepads(pads)` 注入手柄快照。每位玩家的按键绑定可通过 `p.Actions().Save` 与 `p.Actions().Load` 各自保存和加载。

### 手柄断开、重连与回退模式

调用 `Players.SetDisconnectPolicy` 可在单人（`NewPlayers(1)`）和本地多人游戏中妥善处理对局途中的手柄断开：

```go
players.SetDisconnectPolicy(input.DisconnectPolicy{
	PauseOnDisconnect:    true,
	AllowReassignOnPress: true,
	OnPause:              loop.SetPaused,
	OnDisconnect: func(ev input.DisconnectEvent) {
		showReconnectPrompt(ev.PlayerIndex)
	},
	OnReconnect: func(ev input.ReconnectEvent) {
		if !ev.ShouldPause {
			hideReconnectPrompt()
		}
	},
})
```

- **立即释放输入并自动暂停**：当某位玩家此前已连接的手柄从快照中消失时，`Players` 会在该步立即释放其按住的按键与摇杆轴、记住丢失的手柄 ID、触发 `OnDisconnect`（`DisconnectEvent`），并在开启 `PauseOnDisconnect`（且未启用回退模式）时调用 `OnPause(true)`。只要 `WatchGamepads` 持有存活的作用域且仍有玩家等待重连，即使 `game.Loop` 已暂停，`Players` 也会在显示帧上自动轮询手柄，以便手柄重连或其他手柄按键时自动恢复循环；若在 `game.Loop` 之外自行驱动轮询，可直接调用 `PollGamepads()`。
- **原手柄重连与按键接管**：原手柄重新连接后会自动恢复该玩家的槽位并触发 `OnReconnect`（`Reassigned: false`）。启用 `AllowReassignOnPress` 时，任意未分配的已连接手柄新按下任一按键即可接管编号最小的断开玩家槽位，并触发 `OnReconnect`（`Reassigned: true`）。待所有需要暂停的断开玩家均已恢复（或手动重新分配/解除分配）后，`Players` 调用 `OnPause(false)` 恢复游戏。
- **单人键盘/触摸回退**：设置 `FallbackToKeyboardTouch: true` 后，手柄断开不会暂停游戏（`DisconnectEvent.FallbackActive == true`、`ShouldPause == false`）。`Players` 会临时为该玩家开启键盘与指针/触摸路由（并保留原有的键盘或指针分配），待原手柄重连或新手柄接管后平滑切回手柄并清理临时回退状态。
- **状态查询**：可通过 `p.GamepadDisconnected()`、`p.FallbackActive()`、`players.DisconnectedPlayers()` 与 `players.PausedByDisconnect()` 驱动提示文案或暂停遮罩。

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
- **`Reset()`**：清空活动触摸、按住的鼠标键与按键、最近一次手柄状态、待处理的锁存、鼠标相对移动与当前步长快照，同时保留所有绑定和指针锁定，因此切换场景后鼠标视角仍然有效。跨越 `Reset` 一直按住的手柄按键在松开之前保持未激活，与按住的鼠标键一致。请在失焦、切后台或切换场景时调用，防止按键卡死。

### 唤醒空闲的 `OnDemand` 循环

使用带空闲检测的 [`game.Loop`](loop.md#按需绘制与空闲休眠ondemandidlemarkdirty)（设置 `OnDemand: true` 与 `Idle`）驱动菜单密集型、回合制或叙事类游戏时，将 `actions.Wake` 设为 `loop.MarkDirty`，即可在触摸、鼠标按键、键盘按键、已连接手柄状态变化及程序注入触发到来时唤醒循环执行一次 `Step` 并重绘，同时忽略空闲时的鼠标悬停和死区内的摇杆抖动。`TouchControls` 与 `GestureRecognizer` 在绑定了 `Actions` 时会自动通过其唤醒，单独使用时则可设置 `TouchControls.OnWake` 或 `GestureRecognizer.Wake`。

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

## 运动传感器（倾斜与陀螺仪）

使用 `BindMotionAxis` 将加速度计/重力倾斜（`TiltX`、`TiltY`、`TiltZ`）和陀螺仪角速度（`GyroX`、`GyroY`、`GyroZ`）绑定到命名轴，然后调用 `WatchMotion` 订阅 NSGo 的运动传感器流：

```go
actions.BindMotionAxis("steer", input.TiltX, input.MotionAxisOptions{DeadZone: 0.08, MaxRange: 0.6})
actions.BindMotionAxis("pitch", input.TiltY, input.MotionAxisOptions{MaxRange: 0.5})
actions.BindMotionAxis("yaw", input.GyroZ, input.MotionAxisOptions{MaxRange: math.Pi})

motion, err := actions.WatchMotion(cx, input.MotionOptions{
	Orientation: input.MotionOrientationLandscapeLeft,
	Smoothing:   0.25,
	Interval:    16 * time.Millisecond,
})
if err != nil {
	return err
}

// 游戏暂停时停止传感器，恢复时自动重启并保留校准。
loop.OnPause = motion.SetPaused
```

- **屏幕方向映射**：NSGo 在设备的右手自然坐标系下上报运动向量（竖屏时 `+X` 向右、`+Y` 向上、`+Z` 指向屏幕外）。设置 `MotionOptions.Orientation` 可在平滑与校准之前将倾斜和陀螺仪向量旋转到屏幕坐标系：
  - `MotionOrientationPortrait`（零值，默认）：自然竖屏坐标系（`+X`、`+Y`、`+Z`）。
  - `MotionOrientationPortraitUpsideDown`：旋转 180°（`-X`、`-Y`、`+Z`）。
  - `MotionOrientationLandscapeLeft`：逆时针旋转 90°，设备顶部在屏幕左侧（`-Y`、`+X`、`+Z`），与 Android、HarmonyOS 和 Web 上 NSGo 的 `core.OrientationLandscapeLeft` 一致。
  - `MotionOrientationLandscapeRight`：顺时针旋转 90°，设备顶部在屏幕右侧（`+Y`、`-X`、`+Z`）。
  - `MotionOrientationAuto`：根据 `cx.Env().Window()` 的宽高比（或无作用域时通过 `SetWindowSize` 传入的尺寸）推断竖屏或横屏。由于 NSGo 尚未上报当前处于哪一侧横屏，窗口宽大于高时采用 `MotionOptions.LandscapeSide`（默认 `MotionOrientationLandscapeLeft`，也可设为 `MotionOrientationLandscapeRight`）。运行时可通过 `SetOrientation` 切换方向、`Orientation()` 查询当前生效的方向；生效方向变化时会自动重置平滑历史。
- **校准**：调用 `motion.Calibrate()`（或 `actions.CalibrateMotion()`）可在屏幕坐标系中记录玩家当前的中立握持姿态，使 `TiltX`、`TiltY`、`TiltZ` 在该姿态下读数为 `0`。若尚未收到倾斜样本，则会在收到下一条倾斜样本时自动完成校准。`ResetCalibration` 清除中立偏移。
- **暂停与后台**：应用进入 `core.LifecycleBackground` 时 NSGo 会自动暂停 `WatchMotion`，作用域销毁时自动停止。将 `loop.OnPause = motion.SetPaused` 接上后，打开暂停菜单时同样会停止传感器流（清空实时和快照读数，同时保留校准与选项），恢复游玩时自动重新订阅。调用 `Stop()` 会永久停止监听，直到再次调用 `Start(cx)`。
- **叠加与测试**：运动轴会与同名的 `BindAxis`、`BindGamepadAxis`、`BindPointerAxis` 及 `VirtualPad` 摇杆叠加（限制在 `[-1, 1]` 内），并随 `Save` / `Load` 持久化。在无实时传感器的单元测试或回放中，可用 `NewMotionInput`（或调用 `actions.OnMotion`）直接注入 `core.MotionSample`。

## 保存与加载绑定

使用 `Save` 和 `Load` 将自定义按键绑定（包括手柄按键与摇杆轴）以 JSON 形式持久化到 `core.Storage`：

```go
// 保存当前所有动作和轴绑定
if err := actions.Save(cx.Storage(), "controls.v1"); err != nil {
	// 可处理 *core.StorageFullError
	log.Println("save controls:", err)
}

// 启动时先绑定默认键位，再合并玩家保存的绑定
found, err := actions.Load(cx.Storage(), "controls.v1")
if err != nil {
	log.Println("corrupt saved controls, keeping defaults:", err)
}
_ = found
```

`Load` 按名称合并动作、反向动作轴、运动轴、指针轴与手柄摇杆轴：存档中出现的名称会替换当前对应绑定，未出现的名称则保留当前（默认）绑定。这样新版本新增的键位在加载旧版本存档时仍保持默认绑定。

通过 `Unbind`、`UnbindAxis`、`UnbindMotionAxis`、`UnbindPointerAxis`、`UnbindGamepadAxis`（或不带输入调用 `Bind`）显式解绑的条目也会由 `Save` 记录下来（动作写为 `[{"kind":"none"}]`，反向动作轴写为 `{}`，运动轴、指针轴与手柄轴写为 `{"axis":"none"}`），因此 `Load` 会保持它们未绑定，而不会恢复默认键位。如果需要按存档快照完整替换所有绑定、丢弃存档中未出现的默认绑定，请改用 `LoadReplace`。

当 `key` 不存在时，`Load` 和 `LoadReplace` 都返回 `(false, nil)` 并保留现有默认绑定；若存储内容不是有效 JSON、包含未知字段或格式版本未知，则返回 `(false, err)` 且不改动当前绑定。

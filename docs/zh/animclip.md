# animclip

`game/animclip` 在场景对象上播放动画片段，即类型为 `animation` 的[资源文件](resources.md#animation)：在时间轴上为任何已注册属性打关键帧，与 Godot 的 AnimationPlayer 相同。[编辑器](editor.md#动画编辑器)用同一个包为片段打关键帧，因此片段在游戏中的播放效果与编辑器预览一致。

```go
import "github.com/nsgo-dev/game/pkg/animclip"
```

## 属性

轨道的 `property` 按场景文件中的写法书写：

- 对象字段：`x`、`y`、`rotation`、`scaleX`、`scaleY`、`alpha`（0 到 1）、`originX`、`originY`、`flipX`、`flipY`、`sprite.frame`（`PropertySpriteFrame`，按图集帧名离散步进且不做插值）、`posZ`、`rotX`、`rotY`、`rotZ` 和 `scaleZ`（`ObjectProperties` 连同说明列出它们）；
- 已注册的组件字段，写作 `component.field`，在第一个点处拆分，例如 `body.speed`。该字段必须在 `sceneformat.ComponentRegistry` 中注册（见[组件类型](editor.md#组件类型)），种类为 `FieldFloat`、`FieldInt` 或 `FieldBool`；字符串组件字段没有可打关键帧的数值。

`Properties(reg)` 返回所有可打关键帧的属性，对象字段在前；`Lookup(reg, name)` 解析其中一个。属性的 `Kind` 决定关键帧如何写入：浮点数原样写入，整数四舍五入到最近的整数，布尔值在 0.5 及以上为 true，`KindFrame`（`sprite.frame`）写入离散的图集帧名（`Property.GetFrame`、`Property.SetFrame`、`Property.CheckFrame`）。数值都会被钳制到字段的范围内。

`Check(clip, reg)` 检查整个片段：每个属性都必须能解析，`sprite.frame` 轨道必须使用非空的 `frames` 关键帧，数值轨道必须使用 `keys` 且每个关键帧的值都是该属性能接受的值（布尔为 0 或 1，整数为整数值，并且在字段范围内）。轨道的 `target` 是场景中另一个对象的 ID，为空时即播放该片段的对象。

## 逐帧动画片段

有三个辅助函数可以直接从精灵图集构造 `sprite.frame` 片段：

- `FromAsepriteTag(atlas, tag)` 将 `atlas.Animations` 中的 Aseprite 帧标签转换为动画片段，保留标签的帧顺序（含方向与重复次数）、精确毫秒时长和循环标记。
- `FromSpriteAnimation(name, anim)` 对任意 `sprite.Animation` 执行相同转换，并将帧上的 `Event` 转为对应帧起始时间的片段事件。
- `FromFrames(name, frames, fps, loop)` 按固定帧率 `fps` 从有序图集帧名列表生成逐帧片段。

## 播放片段

```go
lib := resource.NewLibrary(os.DirFS("."))
clip, err := resource.LoadAs[*resource.Animation](lib, "anims/hop.animation.yaml")
if err != nil {
	return err
}
player, err := animclip.NewPlayer(clip, components, animclip.BindScene(built, "hero"))
if err != nil {
	return err // 未知属性、错误的关键帧，或场景中没有的目标
}
player.OnEvent = func(name string) {
	if name == "land" {
		mixer.Play(landSound)
	}
}
player.Play()

// 在 game.Loop.Update 中：
player.Update(dt)
```

`NewPlayer` 把片段转换为一个 `tween.Timeline`：每对相邻关键帧对应一个补间，按前一个关键帧的 `interp`（`linear`、`smooth` 或 `step`）缓动，因此播放与其他[时间轴](tween.md)运行在同一个确定性时钟上。轨道在第一个关键帧之前保持其值，在最后一个关键帧之后保持最后的值，与 `Animation.Sample` 一致。在第一次 `Seek` 或 `Update` 之前，播放器不会写入任何值。

- `Play`、`Pause` 和 `Restart` 开始、暂停和重新播放；`Done` 表示不循环的片段已播放到结尾。
- `Seek(at)` 写入每条轨道在 `at` 处的值，不触发事件。循环片段把早于 `0` 或晚于 `Length` 的时间折回 `[0, Length)`，恰好为 `Length` 时显示片段末尾，因此拖到末尾的播放头显示最后的关键帧；不循环的片段则将 `at` 钳制。
- 播放越过片段事件时，按时间顺序以事件名调用 `OnEvent`。
- `SetLoop` 为单个播放器覆盖片段的 `loop`。
- 关键帧的时间和值从文件中的 float32 按最短十进制形式扩展（`KeyValue`、`Seconds`），因此写作 `0.1` 的关键帧恰好位于 100ms，播放的值也是 0.1。

播放器只读取片段一次；要播放经过编辑或经 `Library.Reload` 重载的片段，请创建新的播放器。

## 绑定

`Binder` 把每条轨道连接到它驱动的对象。它接收轨道的目标和解析后的 `Property`，返回写入值的函数，传入的值已按属性四舍五入并钳制；返回错误会让 `NewPlayer` 失败，因此引用了不存在对象的片段在播放前就会被拒绝。

- `BindDoc(doc, self)` 写入 `sceneformat.SceneDoc` 中的对象，并对没有精灵的对象拒绝 `sprite.frame` 轨道。
- `BindScene(built, self)` 写入 `sceneformat.BuiltScene` 中的对象，并同步每个被写入的对象（`BuiltObject.Sync`），因此下一次 `Draw` 会按新的世界变换、精灵帧和透明度显示该对象及其子对象，透明度与 `Build` 一样乘以材质的不透明度。对于 `sprite.frame` 轨道，绑定阶段会预先检查目标对象是否有精灵，以及其图集中是否包含每个关键帧指定的帧名。轨道的目标可以是嵌套子对象的 ID。

组件字段以 `float64`、`int` 或 `bool` 写入对象的 `components` 映射，对象没有该组件的映射时会创建，因此用 `ComponentRegistry.Resolve` 读取它们的代码能看到动画后的值。自定义 `Binder` 在绑定 `KindFrame` 轨道时会收到 `Property.Frames`，可用 `p.Frame(v)` 将索引还原为帧名：

```go
bind := func(target string, p animclip.Property) (func(float64), error) {
	e := world.Find(target)
	if e == nil {
		return nil, fmt.Errorf("no entity %q", target)
	}
	switch p.Name {
	case "x":
		return func(v float64) { e.Pos.X = float32(v) }, nil
	case animclip.PropertySpriteFrame:
		return func(v float64) { e.Frame = p.Frame(v) }, nil
	case "body.speed":
		return func(v float64) { e.Speed = v }, nil
	}
	return nil, fmt.Errorf("entity cannot play %s", p.Name)
}
```

## 精灵状态机

精灵状态机保存在 `*.states.yaml` 文件中（版本 1，Schema 为 [`states.v1.json`](schemas.md#已发布的-schema)）：包含指向 `*.animation.yaml` 片段的具名 `states`、带类型的参数 `parameters`（`float`、`bool`、`trigger`）、入口状态 `entry`，以及按顺序求值、由 `when` 条件表达式和/或 `onEnd: true` 保护的 `transitions`。通配符 `from: '*'` 过渡（例如受击中断 `hurt`）会从目标状态以外的任意状态进行检查。

在场景文件中为对象挂载内置的 `animator` 组件（`sceneformat.AnimatorComponentName`）：

```yaml
components:
  animator:
    states: anim/knight.states.yaml
    startClip: idle
```

使用 `animclip.NewAnimator`（或配合自定义 `Binder` 的 `animclip.NewStateMachine`）在 `game.Loop` 的固定步长中播放：

```go
lib := resource.NewLibrary(os.DirFS("."))
sm, err := animclip.NewAnimator(built, "knight", animclip.FSStates{FS: os.DirFS(".")}, lib, components)
if err != nil {
	return err
}
sm.OnTransition = func(from, to string) { /* ... */ }
sm.OnEvent = func(state, event string) { /* ... */ }

// 在 game.Loop.Update(dt) 中：
sm.SetFloat("move.x", vx)
sm.SetFloat("velocity.y", vy)
sm.SetBool("on_floor", onFloor)
if jumpPressed {
	sm.Trigger("jump")
}
sm.Update(dt)
```

- **条件表达式**：`when` 支持 `||`/`or`、`&&`/`and`、`!`/`not`、`|param|` / `abs(param)`、一元 `-` 以及比较运算（`==`、`!=`、`<`、`<=`、`>`、`>=`、`≤`、`≥`）。表达式在加载时即对照 `parameters` 解析并做类型检查（`ParseStateMachine`）。
- **触发器**：`Trigger(name)` 激活触发器参数；当引用它的过渡触发时自动消费，或在当前固定步长结束时清除（`Update`）。
- **脚本化输入与测试**：`InputDriver` 将项目的 `input.Actions`（如 `move_left`、`move_right`、`jump`、`attack`、`hurt`）映射为运动参数（`move.x`、`velocity.y`、`on_floor`、`jump`、`attack`、`hurt`），`SimulateStateMachine` 在固定步长下确定性地驱动状态机，报告访问过的状态、切换轨迹与卡死状态。
- **校验与引用**：`game check`（`tooling/assetcheck`）校验每个 `*.states.yaml` 文件与 `animator` 组件，拒绝指向缺失状态的过渡、不存在的片段文件以及没有任何过渡读取的参数，并给出精确的文件与行号。移动文件时，`tooling/refs` 会跟踪 `animator.states` 和 `states[].clip` 路径。

### 场景连接

内置的 `animator` 组件（`sceneformat.RegisterAnimatorComponent` / `sceneformat.AnimatorComponentRegistry`）声明了两个事件和两个处理器，并在运行时提供完整实现：

| 成员 | 种类 | 参数 / 载荷 | 说明 |
| --- | --- | --- | --- |
| `animator.stateChanged` | 事件 | `sceneformat.AnimatorStateChangedEvent{ObjectID, From, To}` | 每次状态机切换到不同状态（经由过渡或 `SetState`）时发出一次。 |
| `animator.clipEvent` | 事件 | `sceneformat.AnimatorClipEvent{ObjectID, State, Name}` | 每次活动状态的片段在 `Update` 期间越过具名提示事件时发出一次。 |
| `animator.setState` | 处理器 | `state`（`string`） | 通过 `SetState` 将目标对象的状态机直接切换到 `state`。 |
| `animator.trigger` | 处理器 | `name`（`string`） | 通过 `Trigger` 激活目标对象状态机上名为 `name` 的触发器参数。 |

使用 `animclip.Connect` 将状态机接入场景的 `sceneformat.Signals`：

```go
sig, err := sceneformat.Connect(built.Doc, components, built)
if err != nil {
	return err
}
if err := animclip.Connect(sm, sig, "knight"); err != nil {
	return err
}
```

`animclip.Connect` 会将 `sm` 绑定到 `sig`（`sig.BindAnimator`），使 `animator.setState` 和 `animator.trigger` 处理器能够驱动它，并在触发状态切换或片段事件时与 `OnTransition` / `OnEvent` 回调一起通过 `sig` 发出 `animator.stateChanged` 与 `animator.clipEvent`。`NewAnimator` 也会将 `sm` 注册到 `built`（`BuiltScene.SetAnimator`，实现了 `sceneformat.AnimatorProvider`），因此将 `built` 作为上下文传入 `sceneformat.Connect` 时，即使尚未调用 `animclip.Connect`，`animator.setState` 和 `animator.trigger` 处理器也能直接驱动该状态机。

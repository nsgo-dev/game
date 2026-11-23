# animclip

`game/animclip` 在场景对象上播放动画片段，即类型为 `animation` 的[资源文件](resources.md#animation)：在时间轴上为任何已注册属性打关键帧，与 Godot 的 AnimationPlayer 相同。[编辑器](editor.md#动画编辑器)用同一个包为片段打关键帧，因此片段在游戏中的播放效果与编辑器预览一致。

```go
import "github.com/nsgo-dev/game/packages/game/animclip"
```

## 属性

轨道的 `property` 按场景文件中的写法书写：

- 对象字段：`x`、`y`、`rotation`、`scaleX`、`scaleY`、`alpha`（0 到 1）、`originX`、`originY`、`flipX`、`flipY`、`posZ`、`rotX`、`rotY`、`rotZ` 和 `scaleZ`（`ObjectProperties` 连同说明列出它们）；
- 已注册的组件字段，写作 `component.field`，在第一个点处拆分，例如 `body.speed`。该字段必须在 `sceneformat.ComponentRegistry` 中注册（见[组件类型](editor.md#组件类型)），种类为 `FieldFloat`、`FieldInt` 或 `FieldBool`；字符串字段没有可打关键帧的数值。

`Properties(reg)` 返回所有可打关键帧的属性，对象字段在前；`Lookup(reg, name)` 解析其中一个。属性的 `Kind` 决定关键帧数值如何写入：浮点数原样写入，整数四舍五入到最近的整数，布尔值在 0.5 及以上为 true。所有值都会被钳制到字段的范围内。

`Check(clip, reg)` 检查整个片段：每个属性都必须能解析，每个关键帧的值都必须是该属性能接受的值，布尔为 0 或 1，整数为整数值，并且在字段范围内。轨道的 `target` 是场景中另一个对象的 ID，为空时即播放该片段的对象。

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

- `BindDoc(doc, self)` 写入 `sceneformat.SceneDoc` 中的对象。
- `BindScene(built, self)` 写入 `sceneformat.BuiltScene` 中的对象，并把写入的变换、翻转和透明度复制到对象的精灵上，透明度与 `Build` 一样乘以材质的不透明度，因此下一次 `Draw` 就会显示出来。

组件字段以 `float64`、`int` 或 `bool` 写入对象的 `components` 映射，对象没有该组件的映射时会创建，因此用 `ComponentRegistry.Resolve` 读取它们的代码能看到动画后的值。自己维护状态的游戏可以用自己的 `Binder` 绑定：

```go
bind := func(target string, p animclip.Property) (func(float64), error) {
	e := world.Find(target)
	if e == nil {
		return nil, fmt.Errorf("no entity %q", target)
	}
	switch p.Name {
	case "x":
		return func(v float64) { e.Pos.X = float32(v) }, nil
	case "body.speed":
		return func(v float64) { e.Speed = v }, nil
	}
	return nil, fmt.Errorf("entity cannot play %s", p.Name)
}
```

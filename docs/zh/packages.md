# 包

每个包包含什么、各部分如何工作，内容来自包的文档注释。完整的 API 见各个包的 Go doc。

## Loop

```go
import "github.com/nsgo-dev/game/packages/game"
```

`Loop` 在作用域的显示帧上以固定频率运行游戏的更新，并在两次更新之间每帧绘制一次。

```go
type Loop struct {
	Rate     int                     // updates a second, 60 when 0
	MaxSteps int                     // most updates one frame runs, Rate/10 and at least 1 when 0
	Update   func(dt time.Duration)  // moves the game on by dt, always 1/Rate seconds
	Draw     func(alpha float64)     // draws the frame, alpha in [0, 1)
}

func (l *Loop) Run(cx *core.Scope) (stop func())
```

- 每一帧累加距上一帧的时间，其中包含几个 `1/Rate` 秒的完整步长，就运行几次 `Update`，然后用剩余时间在下一步中走到的比例运行一次 `Draw`。游戏在 `Update` 中按固定的 `dt` 推进状态，所以在任何刷新率下表现都一样；`Draw` 按 `alpha` 把运动中的物体放在最近两个状态之间，所以运动是平滑的。
- 如果一帧要运行的更新超过 `MaxSteps` 次，超出的完整步长会被丢弃。因此 `Update` 比它的步长还慢，或者出现长时间卡顿时，游戏会变慢，而不会让每一帧运行的更新越来越多。
- `Run` 启动循环，直到作用域被销毁或调用 `stop`。它每一帧都会读取这些字段，所以游戏可以在循环运行时修改它们。
- 作用域所在的窗口进入后台时（`core.Env.Lifecycle`），循环会暂停，并从窗口再次显示的时刻继续，所以离开期间不会运行任何更新。
- 平台为显示帧打时间戳时，一帧的时间是显示器的帧时间，否则是图时钟的时间；循环原样累加它。

## sprite

```go
import "github.com/nsgo-dev/game/packages/game/sprite"
```

图集、帧、动画、精灵，以及绘制到 `Canvas` 中的批次。

### Atlas 与 Frame

`Atlas` 是把许多图片打包在一起的一张图（`Image`），加上说明每张图片在哪里的帧（`Frames`，按名称）以及它的动画（`Animations`，按名称）。

`LoadAtlas(img, data)` 读取 TexturePacker（“JSON (Hash)”和“JSON (Array)”）或 Aseprite（json-hash 和 json-array）为所打包图片导出的 JSON 图集数据：

- 矩形以图片的像素为单位，再除以导出数据中的 `meta.scale`，所以图片按 1x 图片的方式计算点数。
- Aseprite 的帧标签会成为 `Animations`，每帧的时长取自导出数据，按标签的方向播放；标签没有设置重复次数时循环播放。
- 被打包工具旋转过的帧会被拒绝：导出时请关闭旋转。

游戏也可以把这张表手写成 `Atlas` 字面量。`Frame` 是一张图片：它绘制的是图片中的哪一部分（`X`、`Y`、`W`、`H`），这部分在未裁剪的图片中的位置（`OffsetX`、`OffsetY`），以及未裁剪的尺寸（`Width`、`Height`）。

### Animation

`Animation` 依次播放各帧，每帧有自己的时长，由 `Update` 按 `Loop` 的固定步长推进。它是一个值，记录自己播放到哪一帧，所以每个精灵播放自己的副本：从图集的 `Animations` 中复制一份，或者手写一个。

- `Update(dt)` 把它向前推进 `dt` 所覆盖的帧数。
- `Frame()` 是当前显示的帧，`Index()` 是它的索引；`Done()` 报告一个不循环的动画是否已经把最后一帧显示满了该帧的时长。
- `Reset()` 让它从头开始。

### Sprite 与 Batch

`Sprite` 是放在绘图中的一帧：可以移动（`X`、`Y`）、旋转（`Rotation`，单位为弧度，顺时针）、缩放（`ScaleX`、`ScaleY`；负值表示镜像）、在未裁剪的帧内翻转（`FlipX`、`FlipY`），淡化（`Alpha`）以及着色（`Tint`），这些都以它的原点（`OriginX`、`OriginY`）为中心。`Z` 决定批次中精灵的先后顺序。

它的零值不绘制任何内容；`NewSprite(f)` 返回一个原始尺寸、完全不透明的精灵。着色只与精灵自身的像素相乘，不会改变它下方已绘制的内容。

`Batch` 把同一图集的精灵绘制到 `core.Drawing` 中，`Z` 最小的先画，`Z` 相同时按批次中的顺序，每个精灵调用一次 `DrawImageFrom`，着色的精灵则调用 `DrawTintedImageFrom`。它会把绘图的状态恢复成绘制前的样子。用 NSGo 的批量 `DrawAtlas` 绘制批次见 [#28](https://github.com/nsgo-dev/game/issues/28)。

```go
walk := atlas.Animations["walk"]
hero := sprite.NewSprite(walk.Frame())
batch := &sprite.Batch{Atlas: atlas, Sprites: []*sprite.Sprite{&hero}}

loop := &game.Loop{
	Update: func(dt time.Duration) {
		walk.Update(dt)
		hero.Frame = walk.Frame()
	},
}
// In a core.Canvas's Draw:
batch.Draw(d)
```

## sound

```go
import "github.com/nsgo-dev/game/packages/game/sound"
```

`Mixer` 及其音效和音乐，构建在 NSGo 的 `audio` 包之上。和 `audio` 一样，它的方法要在引擎的 goroutine 上调用。

### Mixer

`Mixer` 决定游戏的声音和音乐有多响：一个主音量，以及应用命名的每个分组的音量，例如 `"effects"`、`"music"` 和 `"ui"`。每个音量的范围是 0 到 1，未设置时为 1。

- `NewMixer(cx, key)` 把音量保存在应用的存储（`core.Storage`）中，键以 `key` 开头，所以音量在多次启动之间保持不变。
- `Master`、`SetMaster`、`Volume(group)` 和 `SetVolume(group, v)` 读取和设置音量。在计算中读取音量时，这个计算会订阅音量的变化；存储已满时，设置会以 `*core.StorageFullError` 失败。
- 音效的播放音量是它自己这次播放的音量乘以所在分组的音量和主音量，音乐的播放音量是所在分组的音量乘以主音量。
- 音量的变化立即作用于正在播放的音效和音乐。分组音量或主音量降为 0 时，正在播放的音效静音继续播放，音量恢复后重新可闻。
- 窗口在后台时，混音器会把正在播放的音效静音并暂停音乐；窗口再次显示时，恢复仍在播放的音效的音量，并从暂停处继续播放音乐。`Suspend()` 也会这样做，直到调用它返回的 `resume` 为止，过场动画播放期间就是如此（[cutscene](cutscene.md)）。

### Effect

`mixer.Effect(s, group, limit)` 通过混音器在某个分组中播放一个 `*audio.Sound`，同时最多 `limit` 个播放（0 表示自身不设上限，只受平台对所有声音的 32 个上限约束），这样同一个声音连续触发时不会占满所有发声通道。

`Play(voice)` 开始一次播放，并报告是否真的播放了。音量为 0、voice 设置了 `Muted` 或应用在后台时，它不会播放。仍有 `Limit` 个播放在进行时，它会停止最早的一个来腾出名额。`Stop()` 停止这个声音的所有播放。

### Music

`mixer.Music(group)` 通过混音器播放较长的音轨，一次只播一首，每首通过一个 `audio.Player` 播放。

- `Play(src, fade)` 让新音轨在 `fade` 时长内淡入，同时正在播放的音轨淡出；淡化按显示帧进行，音轨静音后即关闭。`fade` 为 0 时立即切换。
- `Stop(fade)` 让音乐淡出。

```go
mixer := sound.NewMixer(cx, "sound")
music := mixer.Music("music")
music.Play(audio.Source{URI: "asset:theme.m4a"}, 2*time.Second)
```

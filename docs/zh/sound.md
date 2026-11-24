# sound

`game/sound` 在 NSGo 的 `audio` 包之上提供可持久化的音量混音器 `Mixer`、带并发上限的音效 `Effect`、支持淡入淡出交叉切换的音乐 `Music`，以及自动后台静音。

```go
import (
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
)
```

与 `audio` 包一样，`Mixer`、`Effect` 和 `Music` 的所有方法都必须在引擎的 goroutine 上调用；从其他 goroutine 调用时请通过 `cx.Post` 派发。放到 `resources/` 中的音频文件请使用 **WAV**、**AAC（`.m4a`）** 或 **MP3** 格式。

## Mixer 与持久化音量

`Mixer` 管理一个主音量，以及游戏自定义的任意分组音量（例如 `"effects"`、`"music"` 和 `"ui"`）。每个音量的取值范围都是 `[0, 1]`，未设置时默认为 `1`。

```go
mixer := sound.NewMixer(cx, "sound")
```

`NewMixer(cx, key)` 会把音量保存在 `cx.Storage()`（`core.Storage`）的以下键中：

- `<key>.master`
- `<key>.group.<group>`

音量会在多次启动之间自动保留。超出 `[0, 1]` 的值会被截断到范围内（`NaN` 会恢复为 `1`）。当存储已满时，`SetMaster` 和 `SetVolume` 会返回 `*core.StorageFullError` 并保持原有音量不变。

`SetDefaultMaster` 和 `SetDefaultVolume(group, v)` 设置玩家调整音量之前混音器使用的默认音量，例如[项目文件](project.md)中的总线音量。它们不会写入存储，因此玩家设置的音量优先；请在游戏读取音量之前调用。

### 响应式的设置界面

`Master()` 和 `Volume(group)` 直接读取 `core.Storage`，因此在响应式视图函数中读取它们会自动订阅音量变化：

```go
core.Text{
	ContentFunc: func() string {
		return fmt.Sprintf("音乐：%.0f%%", mixer.Volume("music")*100)
	},
}
```

### 音量变化何时生效

- **音效（`Effect`）**：每次播放的实际音量为 `voice.Volume * mixer.Volume(group) * mixer.Master()`。音量调整会通过每次播放的 `audio.Playback` 立即作用于正在播放的音效，也作用于之后开始的音效。分组音量或主音量降为 `0` 时，正在播放的音效会静音继续播放，音量恢复后重新可闻。
- **音乐（`Music`）**：正在播放的音轨会实时更新为 `mixer.Volume(group) * mixer.Master()`；有效音量为 `0` 时暂停，调高后从暂停位置继续播放。

## Effect 与并发发声上限

短音效（编码后不超过 8 MiB、解码后不超过 5 秒）先通过 `audio.LoadSound` 一次性解码到内存，再交给混音器包装成 `Effect`：

```go
var hit *sound.Effect
audio.LoadSound(cx, audio.Source{URI: "asset:sounds/hit.wav"}, func(s *audio.Sound, err error) {
	if err != nil {
		return
	}
	hit = mixer.Effect(s, "effects", 4)
})
```

第三个参数 `limit` 限制该音效同时最多有几个重叠播放（`0` 表示不设单独上限，仍受平台全部声音最多 32 个并发发声通道的约束），避免连续拾取金币或脚步声占满全部声道。

- `Play(v audio.Voice) bool` 启动一次播放并返回是否发出了播放请求。有效音量为 `0`（包括设置了 `Muted` 的 voice）或应用在后台时返回 `false`，其余情况返回 `true`，即使后端起播失败、该播放立刻结束。仍有 `Limit` 个播放在进行时，它会停止其中最早的一个来腾出名额，让最新的声音被听到；自然播放结束的播放会直接让出名额，不会停止其他播放，起播失败的播放也不会让其他播放停下。无法单独控制播放的后端会按开始时的设置播放，不受上限和之后音量变化的约束；NSGo 所有支持音频的后端都可以单独控制。
- `Stop()` 停止该音效的所有播放（以及共享同一音频源的其他 `*audio.Sound`）。
- 不再需要该音效时调用底层 `*audio.Sound` 的 `Close()`，或等待 `cx` 作用域销毁时自动释放。

```go
if hit != nil {
	hit.Play(audio.Voice{Volume: 0.9, Rate: 1.05})
}
```

## 2D 定位音效

`Listener2D` 从 2D 摄像机所在的位置收听摆放在世界中的音效：摄像机左边的音效偏向左声道播放，右边的偏向右声道，离得越远声音越小。

```go
listener := sound.Listener2D{
	Position: math2d.V2(cam.X, cam.Y),
	Rotation: cam.Rotation,
	Near:     64,  // 64 个世界单位以内为满音量
	Far:      640, // 640 以外静音
	Rolloff:  sound.RolloffInverse,
}
hit.PlayAt(listener, math2d.V2(enemy.X, enemy.Y), audio.Voice{Volume: 0.9})
```

- `Position` 和 `Rotation` 跟随摄像机，例如取 `tilemap.Camera` 的 `X`、`Y` 和 `Rotation`，这样摄像机旋转时左右仍按画面来算。距离以世界单位计，不受摄像机缩放影响。
- `Gain(pos)` 在 `Near` 以内为 `1`，在 `Far` 及更远处为 `0`，两者之间按 `Rolloff` 衰减：`RolloffLinear`（默认）为直线下降，`RolloffInverse` 按 `Near / 距离` 下降并重新缩放使其在 `Far` 处恰好为 `0`，`RolloffExponential` 按 `(Near / 距离)^2` 下降并重新缩放使其在 `Far` 处为 `0`。`Far` 不大于 `Near`、为无穷大或 `NaN` 时关闭距离衰减。
- `Pan(pos)` 是音效在画面横向上的偏移除以 `PanWidth`，限制在 `[-1, 1]`；`PanWidth` 为 `0` 时取 `Far`。正上方或正下方的音效居中播放。
- `PlayAt(l, pos, v)` 以 `v.Volume * l.Gain(pos)` 播放，再像 `Play` 一样乘上分组音量和主音量，并用 `l.Pan(pos)` 替换 `v.Pan`。超出 `Far` 时返回 `false`、不播放，因此听不到的音效不会占用发声数，也不计入 `Limit`。
- `Cue.PlayAt(l, pos)` 像 `Play` 一样选出 cue 的下一个变体，并以该变体的音量和音高通过其音效的 `PlayAt` 播放，因此随机化的脚步声、击打声也能定位。
- `Voice(pos, v)` 返回 `PlayAt` 会使用的 voice，用于直接播放 `audio.Sound`。音效听不到时会设置 `Muted`，因为 `audio` 会把音量 `0` 当作满音量播放。
- 摄像机旋转只影响声像，距离不受其影响；`Rotation` 为无穷大或 `NaN` 时按 `0` 处理。

声像和音量在播放开始时就已确定，因为 NSGo 的 `audio.Playback` 播放后无法再修改声像。它适合短音效；跟随移动声源的长音需要重新播放。

## 3D 定位音效

`Listener3D` 在右手 Y 轴朝上的 3D 世界（`+X` 向右、`+Y` 向上、`-Z` 向前，与 `math3d` 和 `scene3d.Camera` 一致）中收听音效。`sound.Vec3` 与 `math3d.Vec3` 具有完全相同的字段布局，因此可以直接用 `sound.Vec3(...)` 转换摄像机向量：

```go
listener := sound.Listener3D{
	Position: sound.Vec3(cam.Pos),
	Forward:  sound.Vec3(cam.Forward()),
	Up:       sound.Vec3(cam.Up()),
	Near:     2,
	Far:      40,
	Rolloff:  sound.RolloffInverse,
}
mixer.SetListener3D(listener)

// 一次性 3D 音效（对应 Effect.PlayAt）：
hit.PlayAt3D(listener, sound.Vec3(enemyPos), audio.Voice{Volume: 0.9})

// 跟随摄像机与声源移动的实时 3D 播放：
engine := loopSFX.Play3D(sound.Vec3(shipPos), audio.Voice{Loop: true}, sound.Spatial3DOptions{})
engine.SetPosition(sound.Vec3(nextShipPos))
```

- **距离衰减**：`Gain(pos)` 在 `Near` 以内为 `1`，在 `Far` 及更远处为 `0`，两者之间按 `Rolloff`（`RolloffLinear`、`RolloffInverse` 或 `RolloffExponential`，可通过 `RolloffFactor` 调整陡峭度）衰减。`Far` 不大于 `Near`、为无穷大或 `NaN` 时关闭距离衰减。
- **后方头影衰减**：位于听者后方（`dot(dir, forward) < 0`）的声源从侧面平面的 `1` 线性衰减到正后方的 `RearGain`（`RearGain` 为零、负数或 `NaN` 时默认使用 `DefaultRearGain` 即 `0.7`；设为 `1` 可关闭）。因此摄像机身后的音效比前方等距音效更轻，同时仍准确偏向其所在左右声道。
- **立体声声像**：`Pan(pos)` 将听者指向声源的单位方向投影到听者右轴（`dot(dir, right)`），限制在 `[-1, 1]`。位于听者原点、正前、正后、正上、正下或含 `NaN` 坐标的声源居中（`0`）播放。设置正有限值 `PanWidth` 可改用类似 `Listener2D` 的横向偏移线性声像。
- **定向声源锥与单声源覆盖**：将 `Spatial3DOptions` 传入 `Listener3D.Spatialize`、`Spatialize3D`、`Listener3D.VoiceWithOptions`、`Effect.Play3D` 或 `Cue.Play3D`，可配置定向发射锥（`SourceForward`、`InnerConeAngle`、`OuterConeAngle`、`OuterGain`）或覆盖单个声源的 `Near`/`Far`/`Rolloff`/`RolloffFactor`。
- **实时 `Playback3D` 跟踪**：`Mixer.SetListener3D(l)` 会刷新所有通过 `Effect.Play3D` 或 `Cue.Play3D` 启动的活跃 `Playback3D`，`Playback3D.SetPosition` / `SetOptions` 会刷新单个声源。距离、后方头影与定向声源锥衰减都会通过 `audio.Playback.SetVolume` 实时更新，同时完整遵循主音量、分组音量、`SetPaused`、`SetPauseExempt`、`Suspend` 与后台静音。由于 NSGo 的 `audio.Playback` 在起播后无法修改立体声声像，`Playback3D.Pan()` 记录最新计算的声像，底层播放保持起播时的声像。
- **姿态辅助与 Cue**：`Listener3D.SetPose(pos, forward, up)` 和 `Listener3D.LookAt(target, up)` 在保留衰减参数的前提下更新姿态；`Cue.PlayAt3D` 和 `Cue.Play3D` 可在 3D 空间中播放随机化音效变体。

## Music 与交叉淡化

`mixer.Music(group)` 通过 `audio.Player` 播放较长的流式音轨，同一时间只播一首，并支持平滑的交叉淡化（crossfade）：

```go
music := mixer.Music("music")
music.Loop = true
music.Play(audio.Source{URI: "asset:music/overworld.m4a"}, 2*time.Second)
```

- `Loop`：设为 `true` 后，之后通过 `Play` 启动的音轨都会在播完时循环。
- `Play(src, fade)`：立即以静音状态开始播放新音轨，按显示帧（`core.Scope.Animate`）在 `fade` 时长内把它淡入、同时把正在播放的音轨淡出，旧音轨静音后立即关闭。`fade <= 0` 时立即切换。在一次交叉淡化尚未完成时再次调用 `Play`，正在淡入的音轨也会从当前音量平滑淡出。
- `Stop(fade)`：在 `fade` 时长内淡出并关闭所有音乐播放器（`fade <= 0` 时立即停止）。

在 Web 平台上，浏览器有自动播放策略，在首次用户交互前会拒绝播放（`audio.NotAllowed`）；请在按钮点击或玩家首次操作时启动音乐。

## 随游戏暂停

打开暂停菜单时调用 `mixer.SetPaused(true)` 暂停玩法音频，关闭菜单时调用 `mixer.SetPaused(false)` 恢复：

- **玩法分组**（未标记豁免的分组）：正在播放的 `Effect` 会通过各自的 `audio.Playback` 静音而不停止，新的 `Effect.Play` 调用返回 `false`，`Music` 在当前位置暂停。恢复后，仍在播放的音效恢复原音量，音乐从暂停处继续播放。由于 NSGo v0.3.0 的 `audio.Playback` 只提供音量、速率和停止控制而没有播放头暂停命令，如果暂停时间长于短一次性音效的剩余时长，它会在静音中自然结束；循环音效和 `Music`（`audio.Player`）则会从原处恢复。
- **豁免分组**：调用 `mixer.SetPauseExempt("ui", true)` 让暂停菜单点击音或菜单音乐在 `mixer.Paused()` 为 `true` 时继续播放。`mixer.PauseExempt(group)` 查询分组是否豁免，在暂停期间切换豁免状态会立即生效。
- **与循环同步**：把 `mixer.SetPaused` 赋给 [`game.Loop.OnPause`](loop.md#暂停与同步子系统)，当 `loop.SetPaused` 或 `loop.Paused` 变化时，玩法音频会自动随循环暂停和恢复：

```go
mixer.SetPauseExempt("ui", true)
loop := &game.Loop{
	Rate:    60,
	OnPause: mixer.SetPaused,
	Update:  stepWorld,
	Draw:    renderWorld,
}
loop.Run(cx)
```

## 后台静音

`NewMixer` 以与 `game.Loop.Run` 相同的条件监听 `cx.Env().Lifecycle()`，因此切入后台和回到前台时音频会与游戏循环同步暂停和恢复：

- 应用进入 **`core.LifecycleBackground`**（桌面端最小化、隐藏或被完全遮挡；iOS 或 Android 退到后台）时，混音器会把所有正在播放的音效静音（包括 `"ui"` 等暂停豁免分组）、忽略新的 `Effect.Play` 调用，并暂停所有音乐。
- 窗口回到 **`core.LifecycleActive`** 或 **`core.LifecycleInactive`**（窗口可见，包括桌面端失焦状态）时，暂停豁免分组立即恢复；玩法分组只有在 `mixer.Paused()` 为 `false` 时才恢复——因此暂停菜单仍打开时从后台切回前台，玩法音效仍保持静音，玩法音乐仍停在原位置，直到关闭菜单才继续播放。

## 为过场动画挂起

`mixer.Suspend()` 像进入后台一样让混音器静音，直到调用它返回的 `resume` 函数为止：正在播放的音效静音，新的 `Effect.Play` 调用不再发声，音乐暂停，恢复后从暂停处继续。挂起可以嵌套，只有每个 `Suspend` 都恢复之后混音器才重新发声；同一个 `resume` 调用两次不会多做什么。`mixer.Suspended()` 报告是否还有未恢复的挂起。

```go
resume := mixer.Suspend()
// ……其他东西在占用扬声器……
resume()
```

[cutscene](cutscene.md) 的 `Player` 在视频播放期间会挂起混音器。

## 使用 nsgotest 测试

在 `nsgotest` 中传入 `nsgotest.Environment(core.Environment{Audio: true})` 并设置 `app.AudioDurations`，即可确定性地测试并发上限、交叉淡化、持久化存储和后台静音：

```go
package mygame_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestMixerCrossfadeAndMute(t *testing.T) {
	var mixer *sound.Mixer
	var music *sound.Music

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mixer = sound.NewMixer(cx, "sound")
		music = mixer.Music("music")
		music.Loop = true
		return core.View{}
	},
		nsgotest.Environment(core.Environment{Audio: true}),
		func(a *nsgotest.App) {
			a.AudioDurations = map[string]time.Duration{
				"asset:a.m4a": time.Minute,
				"asset:b.m4a": time.Minute,
			}
		},
	)

	mixer.SetVolume("music", 0.5)
	music.Play(audio.Source{URI: "asset:a.m4a"}, 0)
	music.Play(audio.Source{URI: "asset:b.m4a"}, time.Second)
	app.Advance(1100 * time.Millisecond)

	if !app.Players[0].Closed || !app.Players[1].Playing {
		t.Fatalf("expected track A closed and track B playing")
	}

	app.SetEnvironment(core.Environment{Audio: true, Lifecycle: core.LifecycleBackground})
	app.Settle()
	if app.Players[1].Playing {
		t.Fatal("expected music paused in background")
	}
}
```

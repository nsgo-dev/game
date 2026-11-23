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

- **音效（`Effect`）**：每次播放的实际音量为 `voice.Volume * mixer.Volume(group) * mixer.Master()`。音量调整作用于之后开始播放的音效；已经在播放的短音效保持开始时的音量，但如果将其分组音量或主音量设为 `0`，它会立即停止（[nsgo#1088](https://github.com/nsgo-dev/nsgo/issues/1088)）。
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

- `Play(v audio.Voice) bool` 启动一次播放并返回是否真的播放了。有效音量为 `0`、应用在后台，或者仍有 `Limit` 个播放在进行中时返回 `false`（并发计数按 `s.Duration()` 除以 `v.Rate` 推算，`Rate` 截断在 `[0.5, 2]`，`0` 视为 `1`）。它不会打断正在播放的旧声音来腾出名额（[nsgo#1088](https://github.com/nsgo-dev/nsgo/issues/1088)）。
- `Stop()` 停止该音效的所有播放（以及共享同一音频源的其他 `*audio.Sound`）。
- 不再需要该音效时调用底层 `*audio.Sound` 的 `Close()`，或等待 `cx` 作用域销毁时自动释放。

```go
if hit != nil {
	hit.Play(audio.Voice{Volume: 0.9, Rate: 1.05})
}
```

## Music 与交叉淡化

`mixer.Music(group)` 通过 `audio.Player` 播放较长的流式音轨，同一时间只播一首，并支持平滑的交叉淡化（crossfade）：

```go
music := mixer.Music("music")
music.Loop = true
music.Play(audio.Source{URI: "asset:music/overworld.m4a"}, 2*time.Second)
```

- `Loop`：设为 `true` 后，之后通过 `Play` 启动的音轨都会在播完时循环。
- `Play(src, fade)`：按显示帧（`core.Scope.Animate`）在 `fade` 时长内把新音轨淡入、同时把正在播放的音轨淡出，旧音轨静音后立即关闭。`fade <= 0` 时立即切换。在一次交叉淡化尚未完成时再次调用 `Play`，正在淡入的音轨也会从当前音量平滑淡出。
- `Stop(fade)`：在 `fade` 时长内淡出并关闭所有音乐播放器（`fade <= 0` 时立即停止）。

在 Web 平台上，浏览器有自动播放策略，在首次用户交互前会拒绝播放（`audio.NotAllowed`）；请在按钮点击或玩家首次操作时启动音乐。

## 后台静音

`NewMixer` 会监听 `cx.Env().Lifecycle()`：

- 应用进入 **`core.LifecycleBackground`**（桌面端最小化、隐藏或被完全遮挡；iOS 或 Android 退到后台）时，混音器会停止所有正在播放的音效、忽略新的 `Effect.Play` 调用，并暂停音乐。
- 窗口回到 **`core.LifecycleActive`** 或 **`core.LifecycleInactive`**（窗口可见，包括桌面端失焦状态）时，音乐从暂停处继续，音效也恢复播放。

## 为过场动画挂起

`mixer.Suspend()` 像进入后台一样让混音器静音，直到调用它返回的 `resume` 函数为止：正在播放的音效停止，新的 `Effect.Play` 调用不再发声，音乐暂停，恢复后从暂停处继续。挂起可以嵌套，只有每个 `Suspend` 都恢复之后混音器才重新发声；同一个 `resume` 调用两次不会多做什么。`mixer.Suspended()` 报告是否还有未恢复的挂起。

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

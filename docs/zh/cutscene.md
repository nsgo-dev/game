# cutscene

`game/cutscene` 在游戏里播放预渲染的视频过场动画与引擎内时间轴过场动画，既可以放在场景之中，也可以放在两个场景之间。视频过场动画在 NSGo 自带的视频视图（`core.Video`）中覆盖播放，播放期间暂停 `Loop` 和 `Mixer`。时间轴过场动画在 `game.Loop` 固定步长上针对 `sceneformat.BuiltScene` 或 `sceneformat.SceneDoc` 确定性地运行，驱动相机、角色、本地化对白、音效和音乐轨道。两种过场动画均可跳过，并在结束时回调游戏。

```go
import "github.com/nsgo-dev/game/pkg/cutscene"
```

## 过场动画文件

一段过场动画是一个 `Cutscene`，以 YAML 文件编写，支持版本 1（仅视频，`cutscene.MinVersion`）和版本 2（视频或时间轴，`cutscene.CurrentVersion`）。已发布的 JSON Schema 位于 `https://game.nsgo.dev/schemas/cutscene.v1.json` 和 `https://game.nsgo.dev/schemas/cutscene.v2.json`。

### 视频过场动画（版本 1 与版本 2）

```yaml
version: 1
source: asset:cutscenes/intro.mp4
group: cutscenes
skippable: true
skipAfter: 1.5s
fit: cover
```

| 键 | 含义 |
| --- | --- |
| `version` | 文件格式版本，`1`（`cutscene.MinVersion`）或 `2`（`cutscene.CurrentVersion`）。 |
| `kind` | 版本 2 可选：`video`（设置 `source` 时的默认值）或 `timeline`。版本 1 禁止使用。 |
| `source` | 视频：应用资源里的 `asset:` 路径、项目相对路径或 URL，与 `core.Video` 接受的一致。视频过场动画必填。 |
| `group` | 视频音量所跟随的混音器分组，实际音量为该分组音量乘以主音量。省略时只用主音量。 |
| `skippable` | 玩家能否跳过。省略时为 `false`。 |
| `skipAfter` | 可跳过的过场动画要播放到多少才允许跳过，按 Go 的时长写法书写（`500ms`、`2s`），避免玩家在动画开始前一刻为游戏按下的操作把它直接跳过。省略时从一开始就能跳过。 |
| `fit` | 画面如何适配屏幕：`contain`（默认，留黑边）、`cover`（裁切）或 `stretch`（拉伸变形）。 |
| `transparent` | 画面背后的屏幕保持透明而不是黑色，让游戏在黑边处显示出来；在平台保留 Alpha 通道时，带 Alpha 的视频也能透出游戏。 |

### 时间轴过场动画（版本 2）

版本 2 时间轴过场动画（`*.cutscene.yaml`）设置 `kind: timeline`、总时长 `length` 和按顺序排列的轨道 `tracks`，每个片段包含 `start`、`duration` 和可选的缓动曲线 `easing`（`linear`、`easeIn`、`easeOut`、`easeInOut`、`hold`）：

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/cutscene.v2.json
version: 2
kind: timeline
scene: scenes/level_01.scene.yaml
length: 12s
skippable: true
skipAfter: 1s
tracks:
  - id: camera
    name: Camera
    kind: camera
    clips:
      - id: cam1
        start: 0s
        duration: 1.6s
        easing: hold
        action: hold
        target: door
      - id: cam2
        start: 1.6s
        duration: 2.4s
        easing: easeInOut
        action: pan
        target: knight
      - id: cam3
        start: 4s
        duration: 8s
        action: follow
        target: knight
  - id: knight
    name: Knight
    kind: actor
    target: knight
    clips:
      - id: kn1
        start: 1s
        duration: 2.4s
        easing: easeOut
        action: walk
        fromX: 60
        toX: 220
        state: run
      - id: kn2
        start: 3.4s
        duration: 4.6s
        action: state
        state: idle
  - id: slime
    name: slime_01
    kind: actor
    target: slime_01
    clips:
      - id: sl1
        start: 6s
        duration: 800ms
        easing: easeIn
        action: drop
        fromY: -40
        toY: 276
      - id: sl2
        start: 6.8s
        duration: 5.2s
        action: patrol
        fromX: 420
        toX: 520
        speed: 40
  - id: dialogue
    name: Dialogue
    kind: dialogue
    clips:
      - id: dl1
        start: 3.6s
        duration: 2.4s
        line: intro.knight.01
  - id: sfx
    name: Sound
    kind: sound
    group: sfx
    clips:
      - id: sx1
        start: 200ms
        duration: 600ms
        source: audio/door_slam.wav
  - id: music
    name: Music
    kind: music
    group: music
    clips:
      - id: mu1
        start: 2s
        duration: 10s
        source: audio/cellar_theme.ogg
        fadeIn: 2s
```

| 轨道类型 | 轨道字段 | 片段动作与字段 |
| --- | --- | --- |
| `camera` | `id`、`name`、`kind`、`clips` | `action: hold`（`target` 或 `toX`/`toY`，可选 `offsetX`/`offsetY`、`zoom`）、`action: pan`（`target` 或 `toX`/`toY`，可选 `fromX`/`fromY`、`offsetX`/`offsetY`、`zoom`、`easing`）、`action: follow`（`target`，可选 `offsetX`/`offsetY`、`zoom`）。 |
| `actor` | `id`、`name`、`kind`、`target`（场景对象 ID）、`clips` | `action: walk`（`toX`/`toY`，可选 `fromX`/`fromY`、`state`、`easing`）、`action: state`（`state`）、`action: drop`（`toY`，可选 `fromY`/`fromX`/`toX`、`state`、`easing`）、`action: patrol`（`toX`/`toY`，可选 `fromX`/`fromY`、`speed`、`state`、`easing`）。 |
| `dialogue` | `id`、`name`、`kind`、`clips` | `line`（必填的本地化键——文本只存放在 l10n 表中，不内联），可选 `speaker`。 |
| `sound` | `id`、`name`、`kind`，可选 `group`（默认 `sfx`）、`clips` | `source`（音频路径或 URI），可选 `group`、`volume`（`[0, 1]`）。 |
| `music` | `id`、`name`、`kind`，可选 `group`（默认 `music`）、`clips` | `source`，可选 `group`、`volume`（`[0, 1]`）、`fadeIn`、`fadeOut`、`loop`。 |

`cutscene.Parse(data)` 严格读取两个版本的文件，拒绝未知键、第二个 YAML 文档、不支持的版本、版本 1 中的时间轴字段、混用视频与时间轴字段、非音效轨道上重叠的片段、非有限数值以及为负数或非字符串的时长。`cutscene.Encode(c)` 输出规范的 2 空格 YAML（版本 2 文件首行写入 `SchemaCommentV2`，版本 1 输出保持不变），读入再写回逐字节一致。在代码中构造的过场动画可以把 `Version` 留为 0。

## 播放时间轴过场动画

`NewTimelinePlayer(c, target)` 在 `game.Loop` 的固定步长上针对 `*sceneformat.BuiltScene`（或 `*sceneformat.SceneDoc`）确定性地运行时间轴过场动画，通过 `*l10n.Catalog` 解析对白行 ID，并触发音效与音乐提示：

```go
tl, err := cutscene.NewTimelinePlayer(c, cutscene.TimelineTarget{
	Scene:        builtScene,
	Catalog:      catalog,
	Locale:       "zh",
	Mixer:        mixer,
	OnActorState: func(actorID, state string) { setAnim(actorID, state) },
	OnSound:      func(clip cutscene.Clip, group string, vol float32) { playSFX(clip.Source, group, vol) },
	OnDone:       func(r cutscene.Result) { resumeGameplay() },
})
if err != nil {
	return err
}
tl.Play()

// 在 game.Loop.Update 中：
tl.Update(dt)
```

`tl.Seek(at)` 可直接拖动到 `[0, Length]` 内任意时间点，纯粹计算角色、相机和对白状态而不触发音效、音乐或结束回调。`tl.Reset()` 回到 0 时刻并恢复角色与相机的初始位置。

如需复用与视频过场动画一致的跳过键、点按跳过、返回拦截、`CanSkip()`、响应式 `Dialogue()` 与场景间切换（`TimelineScene`），通过 `Player` 播放：

```go
_, err := player.PlayTimeline(c, cutscene.TimelineTarget{Scene: builtScene, Catalog: catalog}, func(r cutscene.Result) {
	resumeGameplay()
})
// 在 game.Loop.Update 中：
player.Step(dt)
```

与视频过场动画不同，`PlayTimeline` 保持 `Options.Loop` 和 `Options.Mixer` 运行，使场景继续绘制并播放过场音频，同时由 `Player.View()` 接收跳过输入。

## `game check` 与 `tooling/refs` 检查

`game check`（`tooling/assetcheck` 包）会检查项目中的每个 `*.cutscene.yaml` / `*.cutscene.yml` 文件：

- 解析并校验过场动画格式。
- 校验 `scene`（如有）及每个项目相对路径的视频或音频 `source` 文件是否存在。
- 校验每个角色轨道的 `target` 与相机片段的 `target` 是否存在于引用场景（含继承和实例化的场景）中。
- 校验每个对白片段的 `line` ID 是否存在于项目的 YAML 本地化字符串表（`l10n.ParseYAML`）中，缺失时报告精确的文件与行号。

`tooling/refs`（`game mv` 与 `game index`）会跟踪 `*.cutscene.yaml` 中的 `scene`、项目相对路径的视频 `source` 与时间轴音频 `source`，在移动场景或音频资源时原地重写过场动画中的路径引用。

## 播放视频过场动画

为游戏创建一个 `Player`，把它的 `View` 挂在游戏视图之上，作为承载画布的那个视图的最后一个子节点：

```go
loop := &game.Loop{Update: update, Draw: draw}
loop.Run(cx)
mixer := sound.NewMixer(cx, "sound")
player := cutscene.NewPlayer(cx, cutscene.Options{Loop: loop, Mixer: mixer})

return core.View{Class: "flex-1", Children: []core.Node{
	core.Canvas{Class: "flex-1" /* ... */},
	player.View(),
}}
```

没有过场动画播放时 `View` 什么也不显示。`player.Play(c, done)` 播放一段，可以在游戏的 `Update` 中调用，也可以在引擎 goroutine 上的任何地方调用：

```go
if bossDoorOpened {
	player.Play(bossIntro, func(r cutscene.Result) {
		startBossFight()
	})
}
```

播放期间：

- `Options.Loop` 被暂停（`Loop.SetPaused`），`Update` 停止，`Draw` 仍在视频下方继续运行。过场动画结束时循环恢复到原来的状态，游戏自己暂停过的循环仍保持暂停。
- `Options.Mixer` 被挂起（`Mixer.Suspend`，见 [sound](sound.md)）：音效停止，音乐暂停，之后从暂停处继续。
- 屏幕保持常亮；应用进入后台时视频暂停，回到前台后继续播放。

`done` 收到一个 `Result`：结束原因 `Reason`（`Finished`、`Skipped`、`Stopped` 或 `Failed`），平台最后一次报告的播放位置 `Position`，以及 `Failed` 时放在 `Err` 中的平台 `*core.MediaError`。`done` 运行时循环和混音器都已恢复，因此可以在其中切换场景或播放下一段过场动画。对于缺少 source、fit 未知或 `skipAfter` 为负的过场动画，`Play` 返回错误且不播放任何东西；在一段过场动画播放时再播放另一段，会先停止前一段，并以 `Stopped` 通知它的 `done`。如果那个 `done` 自己又播放了一段过场动画，就改为播放那一段，`Play` 返回 `cutscene.ErrReplaced`，不播放任何东西，也不会调用传给它的 `done`。`Stop` 结束正在播放的那段，无论能否跳过。销毁 player 所在的作用域会停止过场动画但不调用 `done`，循环和混音器依然会恢复。

## 场景之间

`player.Scene(c, done)` 把一段过场动画做成独立的[场景](scene.md)：场景进入时开始播放，由 `done` 切换到下一个场景。

```go
intro, err := player.Scene(c, func(cutscene.Result) {
	scenes.Switch(level1, scene.Crossfade(500*time.Millisecond))
})
if err != nil {
	return err
}
scenes.Switch(intro, nil)
```

如果过场动画结束前通过其他方式离开了这个场景，过场动画会停止，但不调用 `done`。带过渡效果从一个过场动画场景切换到同一 player 的另一个过场动画场景时也是如此，尽管新场景会在旧场景退出之前进入。

## 跳过

`skippable` 的过场动画播放到 `skipAfter` 之后，以下操作会跳过它：

- 点按视频；
- 跳过键，默认为 `Enter` 或 `Space`，可用 `Options.SkipKeys` 指定其他键（空切片表示不用按键）；
- 返回：`Escape`、返回键或返回手势，以及浏览器的后退；
- `player.Skip()`，供自己读取输入的游戏调用。

只要有过场动画在播放，返回操作都会被接管，避免游戏在视频下方被导航离开；对不能跳过的过场动画，返回不起作用。`player.CanSkip()` 报告当前能否跳过；在视图中读取时会随之更新，游戏可以借此在视频上方显示自己的跳过提示：

```go
core.Show{When: player.CanSkip, Then: func(*core.Scope) core.Node {
	return core.Text{Class: "absolute bottom-6 right-6 text-white", Content: "跳过 ›"}
}}
```

当平台自行暂停视频时（例如来电或拔出耳机），点按或跳过键会让视频继续播放，而不是跳过它。在此之前视频保持暂停，窗口失去或重新获得焦点时也一样；只有从后台返回时才会自动继续播放。

## 音频路由

视频由平台的播放器解码并播放声音，声音不经过游戏的混音器：

- 视频的音量为混音器主音量乘以过场动画 `group` 的音量，并会随设置菜单的调整实时变化；音量为 0 时视频静音。
- 混音器的总线效果（`EffectChain`）以及其他处理采样的功能都不作用于视频。
- 游戏绘制的任何内容都无法插在视频画面和屏幕之间；只有挂在 `player.View()` 之上的 NSGo 视图能显示在视频上方。

要把解码后的帧绘制进游戏、让声音进入混音器，需要 NSGo 提供目前还没有的帧解码 API。

## 使用 nsgotest 测试

`nsgotest` 会模拟平台播放器的报告：`app.Play` 加载视频，`app.Progress` 把视频播放到某个位置（到达时长时即播放结束），`app.PauseVideo` 和 `app.PlayVideo` 模拟平台暂停和继续播放，`app.Report` 发送 `EventError`。视频的测试 ID 为 `cutscene`，或 `Options.TestID`。

```go
app.Play("id:cutscene", core.VideoInfo{Duration: 3 * time.Second})
app.Progress("id:cutscene", 3*time.Second, 3*time.Second) // done 收到 Finished
```

## 对话字幕

`cutscene.NewCaptionBridge(player, captions)` 把时间线当前的对话行同步到 `a11y.CaptionTrack`，过场对话因此会在 `CaptionsView` 中套用玩家的字幕设置。在每次固定步长更新中，于 `Player.Step` 之后调用它的 `Update`。每条对话只显示一次并带上说话人，在对话片段结束或被下一行替换时撤下字幕，因此字幕持续时间与片段一致，重播时会再次显示。

# cutscene

`game/cutscene` 在游戏里播放视频过场动画，可以放在场景之中，也可以放在两个场景之间。视频在 NSGo 自带的视频视图（`core.Video`）中播放，覆盖在游戏视图之上；播放期间游戏的 `Loop` 和 `Mixer` 会暂停，玩家可以跳过，结束时回调游戏。

```go
import "github.com/nsgo-dev/game/packages/game/cutscene"
```

## 过场动画文件

一段过场动画就是一个 `Cutscene`，游戏作者把它写成 YAML 文件：

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
| `version` | 文件格式版本，`1`（`cutscene.CurrentVersion`）。 |
| `source` | 视频：应用资源里的 `asset:` 路径、文件路径或 URL，与 `core.Video` 接受的一致。必填。 |
| `group` | 视频音量所跟随的混音器分组，实际音量为该分组音量乘以主音量。省略时只用主音量。 |
| `skippable` | 玩家能否跳过。省略时为 `false`。 |
| `skipAfter` | 可跳过的过场动画要播放到多少才允许跳过，按 Go 的时长写法书写（`500ms`、`2s`），避免玩家在动画开始前一刻为游戏按下的操作把它直接跳过。省略时从一开始就能跳过。 |
| `fit` | 画面如何适配屏幕：`contain`（默认，留黑边）、`cover`（裁切）或 `stretch`（拉伸变形）。 |
| `transparent` | 画面背后的屏幕保持透明而不是黑色，让游戏在黑边处显示出来；在平台保留 Alpha 通道时，带 Alpha 的视频也能透出游戏。 |

`cutscene.Parse(data)` 读取文件，拒绝未知的键、第二个 YAML 文档、`1` 以外的版本、缺少的 source、未知的 fit，以及为负数或不是字符串的 `skipAfter`。`cutscene.Encode(c)` 按上表的固定顺序写出各键并省略零值，因此文件读入再写回不会有任何变化。在代码中构造的过场动画可以把 `Version` 留为 0，`Player.Play` 不检查它。

## 播放过场动画

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

# vn

`github.com/nsgo-dev/game/packages/game/vn` 把对话脚本演成视觉小说：带表情的角色立绘、带名牌与打字机效果的文本框、选项、回看记录（backlog）、自动与快进模式，以及语音。

`Presenter` 用 [`flow`](flow.md) 的 `DialogueRunner` 运行 Yarn Spinner 脚本，并配合从 YAML 角色表文件读入的角色表。`Update` 按 [`game.Loop`](loop.md) 的固定步长推进，`Draw` 绘制到 `Canvas` 中，所以每次播放结果一致，在 `nsgotest` 下也一样。

## 角色表文件

角色表文件列出立绘站位的舞台槽位，以及各个角色：每个角色有名牌，每种表情对应一帧立绘：

```yaml
version: 1
slots:
  - name: left
    x: 0.25
  - name: center
    x: 0.5
  - name: right
    x: 0.75
characters:
  - id: aiko
    name: Aiko
    color: '#e07a5f'
    slot: left
    expressions:
      - name: neutral
        frame: aiko_neutral
      - name: smile
        frame: aiko_smile
```

- `version` 必须为 `1`；未知字段与第二个 YAML 文档都会被拒绝。
- 槽位的 `x` 是立绘中心在舞台宽度上的比例。没有 `slots` 时使用 left（0.2）、center（0.5）、right（0.8）。
- 角色的 `id` 供命令引用；台词的说话人先匹配 `id`，再匹配 `name`。`name` 是名牌文字（为空时用 `id`），`color` 是名牌颜色，写 CSS 颜色。
- `slot` 是立绘登场的槽位（为空时用中间的槽位），`defaultExpression` 是登场时的表情（为空时用第一个）。
- 每个表情的 `frame` 是立绘图集中的帧名；`Cast.CheckFrames(atlas)` 在游戏加载时就报告缺失的帧。

`ParseCast` 读取并校验文件，`EncodeCast` 按 schema 的字段顺序、列表原有顺序写出，所以读入再保存不改变任何内容。

## 脚本

台词后面的标签控制舞台：

| 标签 | 作用 |
| --- | --- |
| `#expr:smile` | 设置说话人的表情 |
| `#at:center` | 把说话人的立绘移到某个槽位 |
| `#voice:aiko_001` | 播放一条语音 |
| `#offstage` | 说这句台词时不让说话人的立绘上台 |

角色表中的角色说话时，其立绘会上台并标为正在说话，其余立绘变暗。旁白没有名牌。不在角色表中的说话人按脚本原样显示名字，不能带 `#expr:` 或 `#at:` 标签。

有三个命令由 Presenter 自己处理；其他命令照旧交给 `DialogueOptions.OnCommand`，所有命令也都会发到 `DialogueOptions.Bus`：

```text
<<show aiko>>                 // 以默认表情在自己的槽位登场
<<show ren right doubt>>      // 槽位与表情顺序不限
<<expr aiko worry>>
<<hide aiko>>
<<hide>>                      // 所有立绘
```

立绘登场到其他角色占着的槽位时，那个角色会下台。未知的角色、槽位或表情是错误，由执行它的那次调用返回。

```yarn
title: Start
---
<<show aiko>>
<<show ren right doubt>>
Aiko: Welcome to the old observatory. #line:obs_01 #voice:aiko_01
Ren: It is smaller than I pictured. #line:obs_02
Aiko: Small, but the sky is the same size. #expr:smile #line:obs_03
-> Look through the telescope #line:obs_c1
    Aiko: Careful, the lens is old. #expr:worry #line:obs_05
-> Ask about the logbook #line:obs_c2
    Ren: Who kept this logbook? #at:center #line:obs_07
===
```

## Presenter

```go
p := vn.New(script, cast, vn.Options{
	Voices:       voices,
	AutoDelay:    time.Second,
	SkipInterval: 80 * time.Millisecond,
})
if err := p.Start("Start"); err != nil { ... }

loop := &game.Loop{Update: func(dt time.Duration) {
	if err := p.Update(dt); err != nil { ... }
}}
```

- `Advance` 是玩家的点击：台词还在逐字出现时显示其余部分，否则继续。它会停止快进模式；有选项等待时返回 `flow.ErrChoiceRequired`。
- `Choices` 列出等待中的选项，`Choose(index)` 选择其中一个。
- `Line` 是当前显示的台词，含名牌的 `Name` 与 `Color`、语音 `Voice`，以及 `Read`：玩家此前是否读过。
- `Portraits` 按绘制顺序列出台上的立绘，每个都有槽位、表情、帧，以及其角色是否正在说话。
- `Runner` 返回 `flow.DialogueRunner`，用于变量与存档状态。

`Options.CharsPerSecond` 设置打字机速度（为 0 时 40；为负时一次显示整句）。

## 自动与快进

`SetAuto(true)` 在台词文字全部出现、语音结束并经过 `AutoDelay`（为 0 时 1.5 秒）后自动继续，每个字符再加 `AutoDelayPerRune`。遇到选项时等待。

`SetSkip(true)` 每隔 `SkipInterval`（为 0 时每次 `Update`）立即显示一句台词，不播放语音。它在选项处、结尾、`Advance` 时以及玩家此前没读过的台词上停止，那句台词照常显示；`SkipUnread` 让它连这些台词也快进过去。

台词显示过一次就算已读。它的键是 `#line:` id，没有标签的台词则用节点、说话人与文字。`ReadKeys` 返回这些键供存档保存，`MarkRead` 恢复它们，让快进在多次游戏之间都知道哪些台词已读。

## 回看记录

`Backlog` 按从旧到新返回显示过的台词与选过的选项（`Choice` 为 true），最多 `BacklogSize` 条（为 0 时 200；为负时不保留）。`ReplayVoice(i)` 重新播放第 `i` 条的语音。

## 语音

`VoicePlayer` 按 id 播放语音并返回其时长，自动模式会等它结束。继续到下一句时会停止正在播放的语音。`EffectVoices` 通过 [`sound.Mixer`](sound.md) 播放语音：

```go
voices := &vn.EffectVoices{Clips: map[string]vn.VoiceClip{}}
audio.LoadSound(cx, src, func(snd *audio.Sound, err error) {
	if err == nil {
		voices.Clips["aiko_01"] = vn.VoiceClip{Effect: mixer.Effect(snd, "voice", 1), Length: snd.Duration()}
	}
})
```

开启 `VoiceFromLineID` 后，没有 `#voice:` 标签的台词播放其 `#line:` id 对应的语音。

## 绘制

```go
style := vn.Style{Atlas: portraits, PortraitHeight: 300, InactiveAlpha: 0.55}

core.Canvas{Draw: func(d *core.Drawing, size core.Size) {
	if showLog {
		p.DrawBacklog(d, size, style, scroll)
		return
	}
	p.Draw(d, size, style)
}}
```

`Draw` 绘制站在舞台底边的立绘、带名牌与已显示文字的文本框，以及选项按钮。`Style` 中为零的字段使用默认值；`Box` 可用 `sprite.NineSlice` 绘制文本框，`Font` 可用位图字体绘制文字，否则使用平台文字。`DrawBacklog` 绘制回看记录，最新的在底部，并跳过最新的 `scroll` 条。

`Style.Layout(size, n)` 返回文本框与选项按钮的范围，`ChoiceAt(size, style, point)` 返回点击处的选项：

```go
if i, ok := p.ChoiceAt(size, style, ev.Position); ok {
	err = p.Choose(i)
}
```

## 示例

`vn/cmd/vnsample` 播放一段双角色的短场景，立绘与语音都由代码绘制和合成。点击继续、点选选项，并使用 Auto、Skip 与 Log；选择重看后，Skip 会快进过已读的台词。

```sh
cd packages/game
go tool nsgo dev ./vn/cmd/vnsample
```

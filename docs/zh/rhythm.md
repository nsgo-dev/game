# rhythm

`game/rhythm` 让节奏游戏与音乐保持同步：从 YAML 读取的节拍图、在任意引擎时间给出曲目位置和节拍的音乐时钟，以及按设备校准音频和画面延迟。

```go
import (
	"github.com/nsgo-dev/game/pkg/rhythm"
	"github.com/nsgo-dev/nsgo/audio"
)
```

时钟和校准与 `audio` 包一样运行在引擎的 goroutine 上。它们依赖 NSGo 的音频时钟：`audio.Now`、`audio.Voice.StartAt` 开始播放声音的时间、`audio.CanSchedule` 和 `audio.OutputLatency`。

## 节拍图

节拍图是曲目的速度图，以 YAML 文件放在曲目旁边：

```yaml
version: 1
title: Opening
offsetMs: 120
tempos:
  - beat: 0
    bpm: 120
  - beat: 62.5
    bpm: 140
meters:
  - beat: 0
    beatsPerBar: 4
  - beat: 64
    beatsPerBar: 3
```

| 字段 | 含义 |
| --- | --- |
| `version` | 结构版本，`1` |
| `title` | 曲目名称，供阅读文件的人参考 |
| `offsetMs` | 第 0 拍在音频中的位置，单位毫秒，从音频开头算起；音频在第 0 拍之后才开始时为负数 |
| `tempos` | 按拍序排列的速度变化，第一个在第 0 拍；变化可以从两拍之间开始 |
| `meters` | 按拍序排列的拍号变化，第一个在第 0 拍，每个都落在前一拍号的整小节上；省略时每小节 4 拍 |

`ParseBeatMap` 拒绝未知字段、第二个 YAML 文档、YAML 锚点与别名、应为整数处出现的小数，以及顺序错误或超出范围的变化（1 到 2000 bpm、最多 10,000 个变化、第 1,000,000 拍）。这些范围保证有效节拍图给出的每个时间都是有限值，且远在 `time.Duration` 的范围之内；查询远超这些范围的拍子时，结果会停在最长的 `Duration` 上，而不会溢出回绕。`EncodeBeatMap` 按上述固定顺序、2 空格缩进写出字段，因此节拍图读入后再保存不会改变。

`BeatMap.Timing` 把节拍图转换为 `Timing`，用于在曲目时间与节拍、小节之间换算：

```go
m, err := rhythm.ParseBeatMap(data)
if err != nil {
	return err
}
timing, err := m.Timing()
if err != nil {
	return err
}
at := timing.Time(64)        // 第 64 拍的播放时间，从音频开头算起
beat := timing.Beat(at)      // 64
bar, inBar := timing.Bar(65) // 第 16 小节，进入 1 拍
```

每一拍的时间都从它所在速度变化的起点开始用 `float64` 计算，因此误差不会随曲目长度累积：数小时、上千次速度变化的音乐也与节拍图相差不到一微秒，远在节奏游戏所需的一毫秒之内。第 0 拍之前或最后一次变化之后的拍子沿用最近的速度。`Timing` 不会改变，可以在多个 goroutine 间共享。

## 音乐时钟

`Clock` 可以在任意引擎时间读取曲目位置。在曲目开始播放的那一刻启动它，之后它根据引擎时钟外推，因此在播放器两次进度报告之间也能平滑前进：

```go
clock := rhythm.NewClock(timing)
clock.SetOffsets(offsets) // 来自 OffsetStore

player := audio.NewPlayer(cx, audio.Source{URI: "asset:music/opening.m4a"}, audio.PlayerOptions{
	OnProgress: func(pos, _ time.Duration) { clock.Sync(pos, audio.Now(cx)) },
})
player.Play(func(err error) {
	if err == nil {
		clock.Start(audio.Now(cx), player.Position())
	}
})
```

以 `audio.Sound` 加载的曲目，用 `audio.Voice{StartAt: at}` 播放，并用 `clock.Start(at, 0)` 启动时钟。只有当播放器报告的位置与时钟相差超过 `Tolerance`（默认 30 毫秒）时，`Sync` 才会把时钟移到该位置，例如跳转、循环或卡顿之后，因此进度抖动不会让音符跳动。`Pause` 和 `Resume` 用于跟随播放器暂停和继续。

| 方法 | 用途 |
| --- | --- |
| `Track(now)` | 输出正在播放的位置，未经校正 |
| `Position(now)`、`Beat(now)` | 玩家听到的位置：输出位置减去音频偏移；用它判定输入 |
| `Judge(at, beat)` | 在 `at` 时刻按下的输入相对 `beat` 提前（负数）或延后多少 |
| `DrawPosition(now)`、`DrawBeat(now)` | 此刻绘制的一帧应把判定线放在哪里：输出位置加上画面偏移再减去音频偏移 |
| `BeatTime(beat)`、`TrackTime(pos)` | 输出播放某一拍的引擎时间，传给 `audio.Voice.StartAt` 用来播放击打音或节拍器；两者经过同一输出，因此不加偏移 |

## 校准

每台设备对声音和画面的延迟都不同：输出缓冲、蓝牙耳机、显示器和输入设备都会各自加上一段。校准测量真正到达玩家的延迟，分为两个偏移：

- `Offsets.Audio`：引擎开始播放声音后，玩家跟着它点击所隔的时间。
- `Offsets.Video`：引擎绘制一帧后，玩家跟着画面点击所隔的时间。

`Calibrator` 把点击与每隔 `Interval`（默认 750 毫秒）出现的提示配对，跳过开头几次热身点击后，取各次延后量的中位数作为结果，并以偏差的中位数作为 `Spread`；离散度不超过 30 毫秒时结果为 `Steady`。它没有自己的时钟，因此测试可以用精确的时间驱动它。

`Calibration` 在引擎上运行一个校准器。`CalibrateAudio` 在每个提示时刻播放一次咔嗒声：`audio.CanSchedule` 表示平台支持时，用 `audio.Voice.StartAt` 提前安排；否则在提示时刻当时或之后的第一帧播放，并从实际播放时刻开始测量。它用 `audio.OutputLatency` 作为点击配对的初始值，因此即使蓝牙延迟数百毫秒，点击也能找到对应的提示。`CalibrateVideo` 不播放声音，而是在每个提示时刻让屏幕闪烁，并从第一帧显示闪烁的时刻开始测量。

`CalibrationScreen` 是玩家进行校准的界面，用 `gameui` 绘制到 `core.Canvas` 中：

```go
store := rhythm.NewOffsetStore(cx, "rhythm.calibration")
device := rhythm.DeviceKey(cx)
cal := rhythm.NewCalibration(cx, rhythm.CalibrateAudio, click)
screen := rhythm.NewCalibrationScreen(cal, store, device)
screen.OnSave = func(o rhythm.Offsets) { clock.SetOffsets(o) }

core.View{
	OnPressDown: func(ev core.PressEvent) { screen.OnPressDown(ev) },
	OnPress:     func(ev core.PressEvent) { screen.OnPress(ev) },
	OnKeyDown:   screen.OnKeyDown,
	Children: []core.Node{core.Canvas{
		Draw: func(d *core.Drawing, size core.Size) { screen.Draw(d, core.Bounds{Size: size}) },
	}},
}
```

界面显示期间要像 `game.Loop` 那样每帧重绘画布。拍击在按下的那一刻计入：按钮以外的 `OnPressDown`，或按键按下。完整的按压（`OnPress`）要等手指抬起才触发，以它计时会把每次按住的时长也算进去，这段时长有几十毫秒，而且每次都不一样。游戏中的输入也要在同一时刻判定，即用 `OnPressDown` 或按键按下，否则校准测得的偏移与之对不上。

按在 Retry 或 Save 上会按下按钮。Tab 和方向键在两个按钮之间移动焦点，Return 或 Space 按下获得焦点的按钮；校准完成后焦点会移到 Save，结果不稳定时移到 Retry，因此只用键盘或开关控制的玩家也能保存。结果稳定后 Save 才可用。界面在校准途中关闭时调用 `Stop`，停止已提前安排的咔嗒声；最后一次拍击计入时咔嗒声也会停止。设置 `Text` 可以翻译界面文字。

## 按设备保存偏移

`OffsetStore` 把每台设备的偏移以 YAML 文档保存在应用存储（`core.Storage`）的一个键下：

```yaml
version: 1
devices:
  ios/iPhone15,2:
    audioMs: 182.5
    videoMs: 21
```

`DeviceKey` 用操作系统和硬件型号命名设备，例如 `ios/iPhone15,2`；平台不提供型号时为 `web/unknown` 这样的形式。NSGo 不报告音频输出路由，因此需要分别保存耳机和扬声器偏移的游戏，可以在键后加上由玩家选择的后缀，例如 `/bluetooth`。设备按键名顺序写出，因此相同的偏移总是编码为相同的文本。`Set` 会替换无法读取的文档（例如更新版本结构写入的文档）；存储已满时返回 `*core.StorageFullError`，并保留原有内容。

# 教程 7：声音

为每次接住、被砸、漏接和游戏结束播放一个声音：用 Go 合成音效，用 NSGo 的 `audio` 加载，通过 `sound.Mixer` 播放，并给玩家一个游戏会记住的静音开关。

完成后的代码在 [`examples/tutorial/ch07-sound`](../../examples/tutorial/ch07-sound/)。

## 用 Go 制作音效

使用录制音效的游戏会把 WAV、AAC 或 MP3 文件放在 `resources/` 下，并通过 `"asset:"` URI 加载，见[快速开始](getting-started.md#用-soundmixer-播放声音)。Star Catcher 改在 `audio.go` 里用代码生成四个音效，所以教程不需要任何音频文件。每个音效由几个音调或一段噪声组成，用 `sound.EncodeWAV` 编码：

```go
func makeSounds() (map[event]audio.Source, error) {
	clips := map[event][]float64{
		eventCaught: join(tone(880, 880, 0.06, 0.5), tone(1320, 1320, 0.09, 0.5)),
		eventHit:    thud(0.22, 0.7),
		eventMissed: tone(440, 220, 0.25, 0.45),
		eventOver:   join(tone(660, 660, 0.15, 0.45), tone(495, 495, 0.15, 0.45), tone(330, 300, 0.35, 0.45)),
	}
	sources := make(map[event]audio.Source, len(clips))
	for e, samples := range clips {
		wav, err := sound.EncodeWAV(samples, sampleRate, 1)
		if err != nil {
			return nil, err
		}
		sources[e] = audio.Source{Data: wav}
	}
	return sources, nil
}
```

`tone` 让正弦波从一个音高滑到另一个音高；`thud` 把随机噪声平滑成低沉的隆隆声，噪声来自固定种子的 `random.Stream`，所以每次启动听起来都一样。两者都给片段加上短暂的淡入和一直到静音的淡出，所以既不会咔哒一声开始，也不会戛然而止。

## 混音器

`sound.Mixer` 设置各组声音的音量，把音量保存在应用的存储里，并在应用位于后台时不播放任何声音。游戏创建一个混音器，并把每个音效加载进去：

```go
s.mixer = sound.NewMixer(cx, "starcatcher.sound")

func (s *starCatcher) loadSounds(cx *core.Scope) {
	sources, err := makeSounds()
	if err != nil {
		return
	}
	for e, src := range sources {
		audio.LoadSound(cx, src, func(snd *audio.Sound, err error) {
			if err == nil {
				s.effects[e] = s.mixer.Effect(snd, effectsGroup, 4)
			}
		})
	}
}
```

`audio.LoadSound` 让平台解码一次声音，完成后回调。`mixer.Effect` 把它放进 `"effects"` 组，同时最多播放四份。加载失败的声音保持静默，而不会让游戏停下。

[第 5 章](tutorial-05-rules.md)里世界的事件决定播放什么：

```go
for _, e := range s.world.events {
	if fx := s.effects[e]; fx != nil {
		fx.Play(audio.Voice{Volume: 1})
	}
}
```

音效在固定步长里播放，所以和世界发生的事同步，游戏暂停时也不会播放。

## 静音开关

混音器的主音量就是这个开关。它保存在 `starcatcher.sound.master` 下，所以上次静音的游戏下次启动时仍是静音；在视图里读取它会让视图订阅它：

```go
func (s *starCatcher) muted() bool { return s.mixer.Master() == 0 }

func (s *starCatcher) toggleSound() {
	master := float32(0)
	if s.muted() {
		master = 1
	}
	_ = s.mixer.SetMaster(master)
}

func (s *starCatcher) soundButton() core.Node {
	return buttonWith("Sound", s.toggleSound, func() string {
		if s.muted() {
			return "Sound: off"
		}
		return "Sound: on"
	})
}
```

按钮位于标题界面和暂停菜单里。`SetMaster` 只会在存储已满时失败，这时声音保持原样。

在 Web 上，页面收到用户的第一次操作之前，浏览器不播放任何声音。Star Catcher 的第一个声音总在按下 Start 之后，所以总是被允许的。

## 测试声音

测试平台只在有音频的环境里播放声音，并且只解码测试给出了时长的音源。`withSounds` 同时设置这两项：

```go
func withSounds(t *testing.T) (nsgotest.Option, map[event]audio.Source) {
	t.Helper()
	sources, err := makeSounds()
	if err != nil {
		t.Fatal(err)
	}
	return func(a *nsgotest.App) {
		nsgotest.Environment(core.Environment{Audio: true})(a)
		a.AudioDurations = map[string]time.Duration{}
		for _, src := range sources {
			a.AudioDurations[string(src.Data)] = time.Second / 2
		}
	}, sources
}
```

之后 `app.Sounds` 会列出游戏加载的每个声音，以及它的每次播放。测试接住一颗星星和一块石头，检查接住、被砸和结束的声音各播放一次、漏接的声音一次都没有；用主音量为 0 的 `nsgotest.Storage` 启动第二个应用，检查它以静音启动且什么都不播放；并用 `sound.DecodeWAV` 解码每个音效，检查它很短且以静音结束。

## 下一章

应用关闭后最高分就被忘掉了。[第 8 章](tutorial-08-save.md)把它保存下来。

# quality

`game/quality` 按游戏所运行的设备挑选低、中、高三档画质预设，再根据游戏实测的帧时间调整档位；玩家在设置里选定档位时则改为固定使用玩家的选择。

```go
import "github.com/nsgo-dev/game/packages/game/quality"
```

## 预设

`Preset` 描述一个档位开启哪些内容、把游戏的开销缩放到多少：

| 字段 | Low | Medium | High | 含义 |
| --- | --- | --- | --- | --- |
| `ResolutionScale` | 0.5 | 0.75 | 1 | 缩放游戏自行渲染或光栅化的缓冲区，如 `render3d.OcclusionBuffer` 和程序生成的图像；通过 `Scale` 与 `ScaleSize` 读取 |
| `ParticleScale` | 0.25 | 0.6 | 1 | 缩放粒子上限与爆发数量；通过 `Particles` 读取 |
| `Effects` | 关 | 开 | 开 | 只起装饰作用的效果：闪屏、视差背景、后期效果、拖尾 |
| `Shadows` | 关 | 关 | 开 | 阴影投射，如 `lights2d.Scene.DisableShadows` |

`DefaultPreset(level)` 返回上表中的值。`Particles(n)` 与 `Scale(n)` 四舍五入，且 `n` 为正时结果至少为 1，所以缩放后的上限不会把发射器关掉：

```go
p := quality.DefaultPreset(quality.Low)
emitter.MaxParticles = p.Particles(512) // 128
emitter.Burst(p.Particles(24))          // 6
lights.DisableShadows = !p.Shadows
foliage := world3d.DefaultQualityProfile(world3d.QualityLevelFor(quality.Low))
```

`ResolutionScale` 不会降低 `core.Canvas` 本身的分辨率：NSGo v0.2.0 总是按显示器的完整像素密度绘制画布，没有先以较小尺寸渲染再放大的设置。

## 起始档位

`DeviceFromEnv(cx.Env())` 读取操作系统、设备类型、设备型号、系统版本和 CPU 数量。没有设备规则命中的设备由 `Classify` 决定起始档位：

| 设备 | 起始档位 |
| --- | --- |
| Android | Low；NSGo 既不报告内存也不报告 GPU，由实测帧时间把它往上调 |
| iPhone | iPhone 13（`iPhone14,x`）起为 High，iPhone 11（`iPhone12,x`）起为 Medium，更早为 Low |
| iPad | `iPad13,x` 起为 High，`iPad11,x` 起为 Medium，更早为 Low |
| macOS、Windows、Linux | 8 个及以上 CPU 为 High，4 到 7 个为 Medium，更少为 Low |
| 电视与手表 | Low |
| Web 及未知设备 | Medium |

配置中的设备规则优先：第一条型号是设备型号前缀（不区分大小写）、且在写了 OS 时 OS 也相符的规则，决定起始档位。

## 根据帧时间调整

`Selector` 从配置给出的档位开始；设置为 `auto` 时，它每次启动或换档后先跳过 30 帧，然后按窗口（默认 90 帧）评估：

- 一个窗口中超过四分之一的帧慢于目标帧率（默认 30 fps）时降一档。
- 连续若干个窗口（默认 4 个）中九成的帧都在目标帧时间的 60% 以内时升一档。
- 降过档的档位不会再升回去，因此它会稳定下来，而不是在两档之间来回切换。
- 超过半秒的帧（如加载时或从后台回来的第一帧）不计入。

通过 `Loop.FrameTime` 把循环未平滑的帧长交给它，并在 `OnChange` 中应用新的预设：

```go
sel, err := quality.NewSelector(quality.DefaultConfig(), quality.DeviceFromEnv(cx.Env()))
if err != nil {
	panic(err) // DefaultConfig always validates
}
sel.OnChange = func(_ quality.Level, p quality.Preset) { arena.ApplyQuality(p) }
arena.ApplyQuality(sel.Preset())

loop := &game.Loop{
	FrameTime: func(dt time.Duration) { sel.Observe(dt) },
	Update:    arena.Step,
	Draw:      draw,
}
```

`Selector` 不能并发使用，请在运行循环的作用域里调用它。

## 玩家设置

`save.DisplaySettings.Quality` 保存玩家的选择：`"auto"`（默认）或 `"low"`、`"medium"`、`"high"`。`Settings.Validate` 会规范化这个值，其他任何值都会变成 `"auto"`。`Selector.SetSetting` 应用该设置：档位名会固定在该档位，不再受帧时间影响；`"auto"` 回到起始档位并重新开始调整。`Selector.Setting` 返回应保存的值。

```go
store := save.NewStorageSettingsStore(cx.Storage(), "mygame")
if s, err := store.Load(); err == nil {
	_ = sel.SetSetting(s.Display.Quality) // Load has validated it
}

// From the settings menu:
if err := sel.SetSetting("high"); err == nil {
	s, err := store.Load()
	if err != nil {
		s = save.DefaultSettings()
	}
	s.Display.Quality = sel.Setting()
	_ = store.Save(s)
}
```

## 配置文件

游戏可以把预设、设备规则和调整参数写在一个 YAML 文件里。`ParseConfig` 会拒绝未知字段、缺失或不支持的 `version`、超出 (0, 1] 的缩放值、重复的设备规则、未知的档位或操作系统，以及多余的文档。`EncodeConfig` 按固定的字段顺序输出，因此读入再写回的文件保持不变。

```yaml
version: 1
presets:
  low:
    resolution_scale: 0.5
    particle_scale: 0.25
    effects: false
    shadows: false
  medium:
    resolution_scale: 0.75
    particle_scale: 0.6
    effects: true
    shadows: false
  high:
    resolution_scale: 1
    particle_scale: 1
    effects: true
    shadows: true
devices:
  - model: Google Pixel 8
    os: android
    level: high
adapt:
  target_fps: 30
  window_frames: 120
  upgrade_windows: 3
```

| `adapt` 字段 | 范围 | 默认值 |
| --- | --- | --- |
| `off` | `true` 时整局保持起始档位 | `false` |
| `target_fps` | 1 到 240 | 30 |
| `window_frames` | 10 到 3600 | 90 |
| `upgrade_windows` | 1 到 100 | 4 |

## Starburst 示例中的用法

`examples/starburst` 让低端 Android 手机从低档开始：粒子只有四分之一，没有视差星空、地面网格和闪屏。HUD 上的画质按钮依次切换 auto、low、medium、high，并把选择保存到玩家设置里。`BenchmarkArenaPresets` 测量每个档位下压力模式一次步进加绘制的开销：

```sh
cd examples/starburst
go tool nsgo test -bench BenchmarkArenaPresets ./...
```

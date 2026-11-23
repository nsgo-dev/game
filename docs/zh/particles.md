# particles

`game/particles` 提供由固定种子驱动的确定性 2D 粒子发射器 `Emitter`，由 `game.Loop` 推进并通过可复用的 `sprite.Batch` 绘制。

```go
import "github.com/nsgo-dev/game/packages/game/particles"
```

两个配置和 `Seed` 完全相同、且按相同 `dt` 步进的发射器，会生成逐比特完全一致的粒子状态与绘制指令。

## 配置 Emitter

为 `Emitter` 指定图集 `Atlas`、帧列表 `Frames`、发射形状、运动参数和生命周期曲线：

```go
fire := &particles.Emitter{
	Atlas:        fxAtlas,
	Frames:       []sprite.Frame{fxAtlas.Frames["spark_0"], fxAtlas.Frames["spark_1"]},
	X:            160,
	Y:            120,
	Shape:        particles.Circle(6),
	Rate:         45, // 每秒发射粒子数
	MaxParticles: 128,
	LifetimeMin:  300 * time.Millisecond,
	LifetimeMax:  700 * time.Millisecond,
	SpeedMin:     20,
	SpeedMax:     65,
	AngleMin:     -math.Pi * 0.75,
	AngleMax:     -math.Pi * 0.25,
	GravityY:     -30, // 向上飘升
	SizeCurve: particles.FloatCurve{
		{T: 0, Value: 0.6},
		{T: 0.3, Value: 1.2},
		{T: 1, Value: 0},
	},
	AlphaCurve: particles.FloatCurve{
		{T: 0, Value: 1},
		{T: 1, Value: 0},
	},
	Seed: 42,
	Z:    20,
}
```

在 `game.Loop.Update` 中更新，在 `core.Canvas.Draw` 中绘制：

```go
// 在 Loop.Update 中：
fire.Update(dt)

// 在 Canvas.Draw 中：
fire.Draw(d)
```

- `MaxParticles <= 0` 时默认为 `256`。
- `LifetimeMin` 和 `LifetimeMax` 均为 `0` 时默认为 1 秒。
- `AngleMin` 与 `AngleMax` 以 `+X` 为起点顺时针计量（弧度）。
- `InitialRotationMin`/`Max` 与 `RotationSpeedMin`/`Max` 控制初始角度与角速度（弧度 / 弧度每秒）。

## 发射形状

`Emitter.Shape` 控制新粒子相对于 `(X, Y)` 的出生位置：

| 形状 | 分布方式 |
| --- | --- |
| `Point()` | 精确出生在 `(X, Y)`（`Shape` 的零值）。 |
| `Circle(radius)` | 在以 `(X, Y)` 为圆心、半径为 `radius` 的圆盘内均匀分布。 |
| `Ring(radius)` | 在半径为 `radius` 的圆周上均匀分布。 |
| `Rect(width, height)` | 在以 `(X, Y)` 为中心的 `width × height` 矩形内均匀分布。 |

## 生命周期曲线

曲线按粒子归一化年龄 `t = Age / Lifetime`（`[0, 1]`）在按 `T` 排列的控制点之间线性插值：

- **`SizeCurve`（`FloatCurve`）**：生命周期内的等比缩放（空曲线恒为 `1`）。
- **`AlphaCurve`（`FloatCurve`）**：生命周期内的不透明度（空曲线恒为 `1`）。
- **`RotationCurve`（`FloatCurve`）**：生命周期内叠加的旋转弧度（空曲线恒为 `0`）。
- **`ColorCurve`（`ColorCurve`）**：线性 RGBA 颜色插值并写入 `Particle.Color`（空曲线恒为不透明白色）。`Draw` 用颜色的 RGB 为每个粒子的精灵着色，并将其 alpha 通道与 `AlphaCurve` 相乘。

```go
glow := particles.ColorCurve{
	{T: 0, Color: core.RGBA(0xff, 0xd1, 0x66, 0xff)},
	{T: 1, Color: core.RGBA(0xef, 0x47, 0x6f, 0x00)},
}
```

## 爆发发射、读取与重置

对于爆炸、拾取金币或落地扬尘等一次性特效，保持 `Rate: 0` 并按需调用 `Burst(count)`：

```go
sparks.X, sparks.Y = hit.X, hit.Y
sparks.Burst(24) // 立即发射，最多到 MaxParticles 上限
```

`*Emitter` 的其他方法：

- `Count()`：返回当前存活的粒子数。
- `Particles()`：按生成顺序返回存活粒子 `[]Particle`（复用内部切片，需要保留快照请自行复制）。
- `Clear()`：清除全部存活粒子并重置发射累加器，但保留当前 PRNG 状态。
- `Reset()`：清除全部存活粒子并按 `Seed` 重新播种，以便完全复现后续发射。

## 用 YAML 编写发射器定义

发射器的配置可以放在 YAML 文件里，人和工具都能以文本形式编辑和审阅。键名是 `Config` 字段名的小驼峰形式，`version: 1` 写在最前面，取零值的字段省略不写，但 `space` 和 `shape` 总会写出：

```yaml
version: 1
x: 160
y: 120
space: world
shape:
  kind: circle
  radius: 6
rate: 45
maxParticles: 128
lifetimeMin: 300ms
lifetimeMax: 700ms
gravityY: -30
sizeCurve:
  - t: 0
    value: 0.6
  - t: 1
    value: 0
colorCurve:
  - t: 0
    color: "#ffd166ff"
  - t: 1
    color: "#ef476f00"
seed: 42
frameNames:
  - spark_0
  - spark_1
subEmitters:
  - trigger: death
    burstCount: 6
    config:
      space: world
      shape:
        kind: ring
        radius: 8
      lifetimeMin: 100ms
      lifetimeMax: 200ms
```

```go
cfg, err := particles.LoadConfigFS(assets, "fx/fire.yaml")
if err != nil {
	return err
}
fire := &particles.Emitter{Atlas: fxAtlas}
fire.ReloadConfig(cfg) // frameNames 按 fire.Atlas 解析
```

- `LoadConfigFile`、`LoadConfigFS` 和 `UnmarshalConfigYAML` 读取定义；`SaveConfigFile` 和 `MarshalConfigYAML` 写出定义。
- 保存是确定性的：键的顺序固定，缩进为两个空格；已经是这种形式的定义，读入后再保存会逐字节一致。
- 读取时拒绝 `version` 不等于 `FormatVersion`、未知的键、重复的键、没有值的键，以及第二个 YAML 文档。
- 时长写成 Go `time.Duration` 形式的字符串，例如 `500ms` 或 `1m30s`；不带单位的数字会被拒绝，因为单位有歧义。
- 颜色写成带引号的 `"#rrggbb"`（不透明）或 `"#rrggbbaa"` 字符串。一定要加引号：不带引号的 `#` 会开始一段 YAML 注释，读取时会报告该键没有值。
- `shape.kind` 取 `point`、`circle`、`ring` 或 `rect`；`space` 取 `world` 或 `local`；`collision.mode` 取 `none`、`stop`、`bounce` 或 `kill`；子发射器的 `trigger` 取 `birth`、`death` 或 `collision`。
- 运行时的 `Collider` 不写入文件，拖尾纹理按 URI 保存（`trail.textureURI`）。
- `MarshalConfigJSON` 和 `UnmarshalConfigJSON` 以 JSON 编码同一套结构，供以 JSON 交换定义的程序使用；人编写的文件用 YAML。

### 热重载

`HotConfig` 保存一份定义，并把变更推送给绑定的发射器，不会重置存活粒子、PRNG、附着关系或运行时碰撞体。`ReloadBytes` 接收 `tooling/hotreload` 这类监视器交付的字节，`ReloadHook` 适合传递路径的监视器；读取失败的定义会返回错误，并保留当前定义。

```go
hot, err := particles.LoadHotConfigFile("fx/fire.yaml")
if err != nil {
	return err
}
hot.Bind(fire)
watcher.Watch("fx/fire.yaml", func(data []byte) {
	if err := hot.ReloadBytes(data); err != nil {
		log.Printf("fx/fire.yaml: %v", err)
	}
})
```

在后台 goroutine 中调用 `StageBytes`，再在游戏循环中用 `Flush` 应用暂存的定义。

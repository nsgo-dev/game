# particles

`game/particles` 提供由固定种子驱动的确定性 2D 与 3D 粒子发射器：2D `Emitter` 通过可复用的 `sprite.Batch` 绘制，3D `Emitter3D` 与 `GPUParticles3D` 通过单次实例化 `gpu.View` 调用高效绘制。

```go
import "github.com/nsgo-dev/game/pkg/particles"
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

可通过 `Kind()`（`"point"`、`"circle"`、`"ring"`、`"rect"`）、`Radius()`、`Width()` 与 `Height()` 读取已有 `Shape` 的参数。

## 生命周期曲线

曲线按粒子归一化年龄 `t = Age / Lifetime`（`[0, 1]`）在按 `T` 排列的控制点之间线性插值：

- **`SizeCurve`（`FloatCurve`）**：生命周期内的等比缩放（空曲线恒为 `1`）。
- **`AlphaCurve`（`FloatCurve`）**：生命周期内的不透明度（空曲线恒为 `1`）。
- **`RotationCurve`（`FloatCurve`）**：生命周期内叠加的旋转弧度（空曲线恒为 `0`）。
- **`ColorCurve`（`ColorCurve`）**：线性 RGBA 颜色插值并写入 `Particle.Color`（空曲线恒为不透明白色）。`Draw` 用颜色的 RGB 为每个粒子的精灵着色，并将其 alpha 通道与 `AlphaCurve` 相乘。

```go
glow := particles.ColorCurve{
	{T: 0, Color: core.RGBA(0xffd166, 1)},
	{T: 1, Color: core.RGBA(0xef476f, 0)},
}
```

## 爆发发射、读取与重置

对于爆炸、拾取金币或落地扬尘等一次性特效，保持 `Rate: 0` 并按需调用 `Burst(count)`：

```go
sparks.X, sparks.Y = hit.X, hit.Y
sparks.Burst(24) // 立即发射，最多到 MaxParticles 上限
```

对于定时或周期性爆发，可配置 `Bursts`（`[]Burst`，YAML 键为 `bursts`），设置 `Time`（从启动或重置起的延迟）、`Count`（每次触发发射的粒子数）、`Cycles`（`0` 或 `1` 表示触发一次，`> 1` 表示固定重复次数，`< 0` 表示无限循环）和 `Interval`（各次循环间隔）。`Update(dt)` 按确定性时序触发到期爆发，`ReloadConfig` 会更新爆发计划而不清除存活粒子，`Clear()` / `Reset()` 会重置爆发计时。

`*Emitter` 的其他方法：

- `Count()`：返回当前存活的粒子数。
- `Particles()`：按生成顺序返回存活粒子 `[]Particle`（复用内部切片，需要保留快照请自行复制）。
- `Clear()`：清除全部存活粒子并重置发射累加器，但保留当前 PRNG 状态。
- `Reset()`：清除全部存活粒子并按 `Seed` 重新播种，以便完全复现后续发射。

## 用 YAML 编写发射器定义

发射器的配置可以放在 YAML 文件里，人和工具都能以文本形式编辑和审阅。键名是 `Config` 字段名的小驼峰形式，`version: 1` 写在最前面，取零值的字段省略不写。少数字段即使是默认值也总会写出：`space` 和 `shape`，子发射器的 `trigger` 和 `config`（其中的 `space` 和 `shape` 也会写出），以及存在 `collision` 块时的 `collision.mode`：

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
- 读取时拒绝 `version` 不等于 `FormatVersion` 或出现在子发射器的 `config` 中、未知的键、重复的键、没有值的键、第二个 YAML 文档、整数字段带小数（例如 `maxParticles: 12.7`），以及不适用于该形状类型的尺寸（例如 `circle` 上的 `width`）。
- 时长写成 Go `time.Duration` 形式的字符串，例如 `500ms` 或 `1m30s`；不带单位的数字会被拒绝，因为单位有歧义。
- 颜色写成带引号的 `"#rrggbb"`（不透明）或 `"#rrggbbaa"` 字符串。一定要加引号：不带引号的 `#` 会开始一段 YAML 注释，读取时会报告该键没有值。
- `shape.kind` 取 `point`、`circle`、`ring` 或 `rect`；`space` 取 `world` 或 `local`；`collision.mode` 取 `none`、`stop`、`bounce` 或 `kill`；子发射器的 `trigger` 取 `birth`、`death` 或 `collision`。
- 在子发射器的 `config` 中，`space: world` 直接在触发点的世界坐标下生成子粒子；`space: local` 将每次子爆发的局部坐标系锚定在各自的触发点上，后续触发不会拖走先前存活的子粒子，并将继承的父粒子速度旋转到该局部坐标系中。
- 运行时的 `Collider` 不写入文件，拖尾纹理按 URI 保存（`trail.textureURI`）。
- `MarshalConfigJSON` 和 `UnmarshalConfigJSON` 以 JSON 编码同一套结构，供以 JSON 交换定义的程序使用；人编写的文件用 YAML。
- 其 JSON Schema 发布在 `https://game.nsgo.dev/schemas/particles.v1.json`（`particles.SchemaURL`，见 [YAML 文件的 Schema](schemas.md)）。发射器文件（`*.particles.yaml` / `*.emitter.yaml`）也可以在[粒子发射器编辑器](editor.md#粒子发射器编辑器883)中带实时预览交互式编辑，并直接放置到版本 2 场景的对象上（`ObjectDoc.Particles`）。

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

## 效果文件

效果文件把作为一个效果一起播放的多个发射器组合在一起，例如爆炸的闪光、火花和烟雾，并声明游戏为它设置的类型化参数。`UnmarshalEffectYAML` 把它读成 `Effect`，`MarshalEffectYAML` 把它写回，保存方式与发射器定义一样是确定性的（参数按名称排序）：

```yaml
version: 1
emitters:
  - config:
      space: world
      shape:
        kind: circle
        radius: 6
      rate: 45
      lifetimeMin: 300ms
      lifetimeMax: 700ms
    duration: 200ms
  - file: fx/sparks.particles.yaml
    offsetX: 4
    offsetY: -2
    delay: 50ms
    minQuality: medium
params:
  speed:
    type: float
    float: 1.5
  tint:
    type: color
    color: "#ff8800ff"
```

- 每个发射器恰好有 `file` 与 `config` 之一：`file` 是相对资源根目录、以斜杠分隔的发射器定义路径，`config` 是按发射器文件 schema 编写、不带 `version` 的内联定义。
- `offsetX` 和 `offsetY` 让发射器偏离效果原点，`delay` 是效果开始后它多久开始，`duration` 是它发射多久（为零表示效果播放期间一直发射），`minQuality` 是它播放的最低画质等级（`low`、`medium` 或 `high`；省略表示所有等级）。`delay` 和 `duration` 是时长字符串，不能为负。
- 参数的 `type` 是 `float`、`int`、`color` 或 `vec2`，默认值写在与类型同名的键下：数字、整数、带引号的 `"#rrggbb"` 或 `"#rrggbbaa"` 颜色，或两个数字组成的列表。写在其他类型键下的值、缺少的值以及非有限值都会被拒绝。
- `Effect.SetParam(name, value)` 把已声明的参数设为 `float64` 或 `float32`、`int`、`core.Color` 或 `[2]float32`，对未声明的名称或类型不符的值返回错误，且不改动该参数。`Effect.Validate` 报告 `MarshalEffectYAML` 会拒绝的问题。

目前效果文件只是一种格式加上它的参数值：还没有东西播放 `Effect`，`UnmarshalEffectYAML` 不会加载以文件引用的发射器，发射器配置中的字段也还不能绑定到参数。带延迟、时长和 `minQuality` 的播放、参数绑定、粒子事件和剔除在 [#1730](https://github.com/nsgo-dev/game/issues/1730) 中跟踪。

## 性能

每个存活粒子都是发射器 `sprite.Batch` 中的一个精灵。未着色的粒子（没有 `ColorCurve` 或 `ColorCurve` 为白色）按连续的一段合并为一次 `DrawAtlas` 调用绘制（[nsgo-dev/game#28](https://github.com/nsgo-dev/game/issues/28)）。被非白色 `ColorCurve` 着色的粒子仍各自单独绘制一次图像，并在 NSGo 的 `Drawing` 内部分配一次内存，因此给数千个粒子着色比用 `AlphaCurve` 淡出它们开销更大。

`examples/fountain` 是对应的压力测试场景：四个按种子生成的喷泉在每个固定步之后补足到 5000 个存活粒子，全部带旋转和淡出。用 NSGo 的性能监视器运行它，并在窗口可见时读取右上角的 FPS 一行（窗口被遮挡时 `game.Loop` 会暂停）：

```sh
cd examples/fountain
go tool nsgo dev -fps
```

上下方向键以 1000 为步长调整粒子数（1000 到 20000），用来找出机器跌破显示刷新率的位置；`R` 按种子重新开始场景。

两个基准测试跟踪一帧中 CPU 的部分，分别对应粒子的两种绘制方式：

```sh
# 5000 个着色粒子执行一次 Update 和一次 Draw，每个粒子单独绘制一次图像
go tool nsgo test ./pkg/particles -- -run '^$' -bench Emitter5000UpdateDraw
# 喷泉场景的 5000 个未着色粒子执行一个固定步和一次绘制，按 DrawAtlas 分段绘制
cd examples/fountain && go tool nsgo test -run '^$' -bench Scene5000 .
```

在 AMD Ryzen 9 3900X（Linux）上它们分别约需 1.7 ms（5000 次内存分配）和 1.8 ms（25 次内存分配）。这只是推进粒子并记录绘制操作的 CPU 开销：不包括平台每帧绘制约 5000 张图像的开销，对着色粒子也不包括垃圾回收器回收每次绘制所分配内存的开销，因此不能据此认为设备能保持 60 fps。macOS 上用 `--nsgo-fps` 测得的 60 fps 读数尚未记录（[nsgo-dev/game#85](https://github.com/nsgo-dev/game/issues/85)）。

## 3D GPU 友好粒子

`Emitter3D` 与 2D 发射器共享同一套核心模型（`Rate`、`MaxParticles`、`LifetimeMin`/`Max`、`SpeedMin`/`Max`、`SizeCurve`、`AlphaCurve`、`RotationCurve`、`ColorCurve`、`SimulationSpace`、`Seed`、`Burst`、`Clear`、`Reset`），并扩展了 3D 发射形状、3D 重力、线性阻力、散射角、多种公告板朝向模式、从远到近深度排序，以及面向 `gpu.View` 的紧凑实例化 GPU 上传管线。

```go
sparks := particles.NewEmitter3D(particles.Emitter3DConfig{
	Position:     math3d.Vec3{Y: 1},
	Direction:    math3d.Vec3Up(),
	Shape:        particles.Cone(0.3, 0.4),
	Billboard:    particles.BillboardStretched,
	Blend:        particles.BlendAdditive,
	Rate:         4000,
	MaxParticles: 10000,
	LifetimeMin:  600 * time.Millisecond,
	LifetimeMax:  1400 * time.Millisecond,
	SpeedMin:     3,
	SpeedMax:     9,
	Spread:       0.2,
	Drag:         0.5,
	Gravity:      math3d.Vec3{Y: -9.8},
	SizeMin:      0.05,
	SizeMax:      0.15,
	ColorCurve: particles.ColorCurve{
		{T: 0, Color: core.RGBA(0xffd166, 1)},
		{T: 1, Color: core.RGBA(0xef476f, 0)},
	},
	Seed: 280,
})
```

### 3D 发射形状

`Emitter3D.Shape`（`Shape3D`）在发射器的 `(side, Direction, forward)` 局部坐标系中相对于 `Position` 生成粒子：

| 形状 | 分布方式 |
| --- | --- |
| `Point3D()` | 精确出生在 `Position`（`Shape3D` 的零值）。 |
| `Sphere(radius)` | 在半径为 `radius` 的球体内部均匀分布。 |
| `Hemisphere(radius)` | 在沿 `Direction` 方向的上半球内部均匀分布。 |
| `Box(width, height, depth)` | 在以 `Position` 为中心的 `width × height × depth` 长方体内均匀分布。 |
| `Cone(radius, angle)` | 在垂直于 `Direction`、半径为 `radius` 的底面圆盘上均匀出生，并在半角 `angle`（弧度）范围内向外发散。 |
| `Ring3D(radius)` | 在垂直于 `Direction`、半径为 `radius` 的圆周上均匀分布。 |

### 公告板模式、混合模式与深度排序

- **`BillboardSpherical`**（默认）：与相机视平面平行的全向公告板，支持逐粒子滚转（`InitialRotationMin`/`Max`、`RotationSpeedMin`/`Max`、`RotationCurve`）。
- **`BillboardCylindrical`**：锁定到 `Emitter3D.Axis`（默认 `+Y`）轴的圆柱公告板，仅绕该轴转向相机。
- **`BillboardStretched`**：沿粒子速度方向对齐并按速率拉伸的公告板（长度 `size + speed * StretchFactor`，`StretchFactor` 默认 `0.05` 秒）。
- **`BlendAdditive`**（`gpu.BlendAdditive()`）：顺序无关的加法混合，适用于火焰、火花与光效，无需深度排序。
- **`BlendAlpha`**（`gpu.BlendAlpha()`，默认）与 **`BlendPremultiplied`**（`gpu.BlendPremultipliedAlpha()`）：在打包上传前调用 `e.SortByDepth(camPos, camForward)`，使用确定性、零分配的 4 趟基数排序按从远到近排列粒子。

### 实例化 GPU 绘制（`GPUParticles3D`）

`GPUParticles3D` 持有共享的 4 顶点单位四边形缓冲（`StepVertex`）以及可自动扩容的 40 字节逐实例缓冲（`StepInstance`：`center: vec3<f32>`、`right: vec3<f32>`、`up: vec3<f32>`、`color: unorm8x4`），只需单次 `DrawIndexedInstanced` 调用即可绘制上万个粒子。它不依赖计算着色器或存储缓冲，因此可在 WebGL 2 / OpenGL ES 3.0、Metal、Vulkan、D3D11 与 WebGPU 上一致运行。

`ParticleWGSL`、`ParticleSoftCircleWGSL` 与 `ParticleTexturedWGSL` 均在 `@group(0) @binding(0)` 绑定标准 3D 相机 uniform（`view`、`proj`、`view_proj`、`eye`），与 `render3d.CameraBuffer` 逐字节兼容：

```go
// 在 gpu.View 的 OnReady 中：
pipe, err := particles.NewParticlePipeline(cx, dev, particles.ParticlePipelineOptions{
	CameraLayout: camBuf.Layout(),
	DepthFormat:  gpu.FormatDepth24Plus,
	Blend:        sparks.Blend,
	SoftCircle:   true,
})
gpuParticles, err := particles.NewGPUParticles3D(cx, dev, particles.GPUParticles3DOptions{
	Capacity: sparks.MaxParticles,
})

// 在固定步 Update 中：
sparks.Step(dt)

// 在 gpu.View 的 OnFrame 中：
if sparks.Blend != particles.BlendAdditive {
	sparks.SortByDepth(cam.Pos, cam.Forward())
}
_ = gpuParticles.Upload(sparks, cam.Right(), cam.Up())
pass.SetPipeline(pipe)
pass.SetBindGroup(0, camBuf.BindGroup())
gpuParticles.Draw(pass) // 单次 DrawIndexedInstanced 绘制全部存活粒子
```

### 用 YAML 编写 3D 发射器定义

`Emitter3DConfig` 遵循与 2D `Config` 相同的版本化 YAML（及 JSON）约定，提供 `LoadConfig3DFile`、`LoadConfig3DFS`、`UnmarshalConfig3DYAML`、`SaveConfig3DFile`、`MarshalConfig3DYAML`、`UnmarshalConfig3DJSON` 与 `MarshalConfig3DJSON`。保存时按固定键序确定性输出；读取时拒绝未知字段、错误或缺失的 `version`、空值、非有限浮点数以及与 `shape.kind` 不匹配的形状参数。

### 3D 一万粒子性能基准

`Emitter3D` 采用紧凑的数组结构体（SoA）存储、交换删除（swap-remove）过期回收与可复用基数排序缓冲：

```sh
go tool nsgo test ./pkg/particles -- -run '^$' -bench BenchmarkEmitter3D10k
```

在 Apple Silicon 上对 10,000 个启用重力、指数阻力、自旋以及尺寸/透明度/颜色/旋转曲线的活跃粒子测得：

- `BenchmarkEmitter3D10kStepAndPack`：约 `0.34 ms/帧`，`0 B/op`，`0 allocs/op`（加法混合 / 无需排序路径）。
- `BenchmarkEmitter3D10kStepSortAndPack`：约 `0.44 ms/帧`，`0 B/op`，`0 allocs/op`（透明混合 + 从远到近基数排序路径）。

两种路径均仅占用 16.6 ms（60 fps）桌面帧预算的不到 3%，全部 10,000 个实例合并为一次 400 KB 缓冲写入和单次 `DrawIndexedInstanced` 调用。

3D 发射器定义可以包含 `simulation: cpu` 或 `simulation: gpu`。版本化格式会保留并验证该字段，即使目标不支持 CPU 以外的路径。GPU 模拟仅用于视觉效果，需要支持 compute 的渲染器。引入 compute 模拟器期间，CPU 仍是默认值。

# resource

`game/resource` 读写共享资源文件：多个场景和多处代码共用的数据各自保存在独立的 YAML 文件里，通过路径引用而不是重复书写，作用与 Godot 的 `.tres` 文件相同。材质、曲线、渐变、动画片段、UI 主题、音频总线布局和物理材质都是资源文件。

```go
import "github.com/nsgo-dev/game/packages/game/resource"
```

## 公共文件头

每个资源文件都是一个 YAML 文档，开头是相同的几个字段：

| 字段 | 说明 |
| --- | --- |
| `type` | 资源类型：`material`、`curve`、`gradient`、`animation`、`theme`、`busLayout` 或 `physicsMaterial`。 |
| `version` | 模式版本，必须为 `1`。 |
| `uid` | 可选的稳定 ID，文件改名或移动后保持不变，由 13 个字符组成，取自 `0`-`9` 和去掉 `i`、`l`、`o`、`u` 的 `a`-`z`。由 `nsgo game index` 写入，见[稳定引用](project.md#稳定引用)。 |

其后是该类型自己的字段。`Parse` 先读取文件头，遇到未知类型或 `1` 以外的版本时直接给出相应的错误信息，然后把整个文档解码成对应的 Go 类型（`*resource.Material`、`*resource.Curve` 等），并拒绝未知字段、多余的 YAML 文档和越界的值。`Encode` 先写文件头，其余字段按固定顺序写出，省略取默认值的字段，因此它写出的文件读回后再编码得到的字节完全相同，diff 里只会出现真正改动的部分。

文件名不受限制；采用 `<名称>.<类型>.yaml` 的形式（例如 `stone.material.yaml`）能让目录保持清晰。

## 类型

### material

描述表面如何绘制。2D 精灵使用 `opacity` 和 `blend`；3D 渲染器还会读取 PBR 字段。

```yaml
type: material
version: 1
name: Stone
baseColor: '#8a8f99'
texture: textures/stone.png
opacity: 0.75
blend: multiply
roughness: 0.9
```

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `name` | | 可选的显示名称。 |
| `baseColor` | 白色 | CSS 颜色。 |
| `texture` | | 从项目根目录起、以斜杠分隔的图片路径。 |
| `opacity` | `1` | 与使用该材质绘制的内容的 alpha 相乘，取值 `0` 到 `1`。 |
| `blend` | `sourceOver` | 驼峰形式的画布合成操作，例如 `multiply`、`screen` 或 `lighter`（见 `resource.BlendModes`）。 |
| `metallic` | `0` | PBR 金属度，取值 `0` 到 `1`。 |
| `roughness` | `1` | PBR 粗糙度，取值 `0` 到 `1`。 |
| `emissive` | | 表面自发光的 CSS 颜色。 |
| `doubleSided`、`unlit` | `false` | 绘制背面；不受光照影响。 |

### curve

随时间变化的数值，例如缓动或粒子在生命周期内的大小。关键帧按 `time` 严格递增排列，`interp` 决定从当前关键帧到下一个关键帧的过渡方式：`linear`（默认）、`step` 或 `smooth`。

```yaml
type: curve
version: 1
name: Jump height
keys:
  - time: 0
    value: 0
    interp: smooth
  - time: 0.5
    value: 1
  - time: 1
    value: 0
```

`Curve.Eval(t)` 返回 `t` 处的值，在第一个和最后一个关键帧之外保持端点的值；它的方法值可以直接用作 `tween.EaseFunc`。

### gradient

按 `0` 到 `1` 的偏移排列的颜色，偏移不递减；同一偏移上的两个色标形成硬边。

```yaml
type: gradient
version: 1
stops:
  - offset: 0
    color: '#ffcc66'
  - offset: 1
    color: '#331a4d'
```

`Gradient.At(t)` 混合 `t` 两侧的颜色，`Gradient.CoreStops()` 返回可用于画布渐变的 `core.GradientStop`。

### animation

由属性轨道组成的动画片段，关键帧时间从 `0` 到 `length` 秒。`target` 指定轨道驱动的对象，由 [animclip](animclip.md) 播放时即场景中对象的 ID（为空时即播放该片段的对象）；每个 target 与 property 的组合只有一条轨道。每条轨道要么设置数值关键帧 `keys`（`time`、`value` 和可选 `interp`），要么设置离散精灵帧 `frames`（`time`、`frame`），不能同时设置；`frames` 按时间步进切换图集帧名且不做插值，用于 `sprite.frame` 逐帧动画轨道。`events` 是按时间排列的具名提示点，每个位于 `0` 到 `length` 之间的 `time`，播放越过时由播放器报告。

```yaml
type: animation
version: 1
name: Torch flicker
length: 1.5
loop: true
tracks:
  - target: flame
    property: alpha
    keys:
      - time: 0
        value: 1
      - time: 0.75
        value: 0.6
        interp: smooth
      - time: 1.5
        value: 1
  - property: sprite.frame
    frames:
      - time: 0
        frame: torch_0
      - time: 0.75
        frame: torch_1
events:
  - time: 0.75
    name: flare
```

`Animation.Sample(t, set)` 先把 `t` 循环或钳制到片段范围内（`ClipTime`），再以每条数值轨道的 target、property 和值调用 `set`；`Animation.SampleFrames(t, set)` 对离散帧轨道做同样处理（`Track.IsFrameTrack`、`Track.EvalFrame`）。[animclip](animclip.md) 在场景对象上播放片段，并按已注册的组件类型检查其属性；[编辑器的动画面板](editor.md#动画编辑器)用于打关键帧和保存片段。

### theme

`gameui` 主题。所有字段都是可选的，`Theme.Apply(base)` 只替换文件中设置了的字段，因此一个文件可以只改 `gameui.DefaultTheme()` 的几种颜色。

```yaml
type: theme
version: 1
colors:
  panelBackground: '#10141c'
  accent: '#f97316'
padding: 10
font:
  family: Inter
  size: 18
  weight: semibold
```

`colors` 中每个键对应 `gameui.Theme` 的一种颜色（`panelBackground`、`panelBorder`、`background`、`hover`、`active`、`focusedBorder`、`disabled`、`border`、`text`、`disabledText`、`mutedText`、`accent`、`accentHover`、`accentActive`、`track`）。尺寸字段是 `padding`、`cornerRadius`、`borderWidth` 和 `focusWidth`，`font.weight` 取 `light`、`normal`、`medium`、`semibold` 或 `bold`。

### busLayout

`sound.Mixer` 使用的音频总线及每条总线上的效果链。每个效果条目必须且只能设置 `lowPass`、`highPass`（`cutoffHz`、`q`）、`delay`（`seconds`、`feedback`、`wet`）、`reverb`（`roomSize`、`damping`、`wet`）、`gain`（`factor`）或 `bitcrush`（`bits`、`downsample`）中的一个。

```yaml
type: busLayout
version: 1
buses:
  - name: music
    effects:
      - lowPass:
          cutoffHz: 8000
  - name: effects
    effects:
      - reverb:
          roomSize: 0.5
          wet: 0.2
```

`BusLayout.Apply(mixer)` 用 `SetGroupEffects` 设置每条总线的效果链；没有效果的总线会清空其效果链。音量仍由玩家自己的设置决定。

### physicsMaterial

2D 与 3D 刚体的表面属性：`friction`、`restitution`（`0` 到 `1`）、`linearDamping` 和 `angularDamping`。`PhysicsMaterial.ApplyTo(body)` 把文件中设置的字段写入 `physics.RigidBody`，`PhysicsMaterial.ApplyTo3D(body)` 将其写入 `physics3d.Body`，未设置的字段保持不变。`Library.PhysicsMaterial(path)` 按路径加载并共享单个 `*PhysicsMaterial`，同时实现 `sceneformat.PhysicsMaterialResolver` 供场景物理组件引用。

```yaml
type: physicsMaterial
version: 1
friction: 0.02
restitution: 0.1
```

## 用 Library 共享资源

`Library` 按以斜杠分隔的路径（例如 `materials/stone.material.yaml`）从 `fs.FS` 加载资源文件，每个路径只保留一个值：对同一路径的每次 `Load` 都返回同一个指针，因此使用该文件的所有场景和代码持有的是同一个值。

```go
lib := resource.NewLibrary(os.DirFS("project"))

stone, err := lib.Material("materials/stone.material.yaml")
jump, err := resource.LoadAs[*resource.Curve](lib, "curves/jump.curve.yaml")
```

`Library.Reload(path, data)` 解析新内容并原地复制到已加载的值中，所有持有者都能看到修改。无法解析或改变了类型的文件会保留上一个有效值并返回错误。`Library.Revision()` 统计成功重载的次数，供从资源派生值的代码判断何时需要重新派生，例如用 `Theme.Apply` 生成的 `gameui.Theme`。请在读取资源的 goroutine（通常是引擎的 goroutine）上调用 `Reload`。

## 场景中的材质

`sceneformat` 的对象通过 `material` 按路径引用材质文件：

```yaml
objects:
  - id: wall
    sprite:
      atlas: cave_tiles
      frame: "1"
    material: materials/stone.material.yaml
```

`sceneformat.Build` 通过实现了 `sceneformat.MaterialResolver` 的解析器解析它；`Library` 实现了该接口，因此解析器可以把它与图集一起嵌入：

```go
type resolver struct {
	sceneformat.AtlasMap
	*resource.Library
}

cave, err := sceneformat.Build(caveDoc, resolver{atlases, lib})
keep, err := sceneformat.Build(keepDoc, resolver{atlases, lib})
```

对象的精灵以自身 alpha 乘以材质的 opacity 绘制，并使用材质的混合模式。`BuiltScene.Draw` 会先把材质的当前状态复制到精灵上（`SyncMaterials`），因此文件重载后两个场景都会绘制出修改后的效果。

## 编辑时重载

开发期间，`hotreload.Watcher.WatchResources` 监视其目录下的所有 `.yaml` 和 `.yml` 文件，并在引擎 goroutine 上把库已加载且发生变化的文件交给 `Library.Reload`。监视器的目录必须是库的根目录，路径才能对应：

```go
lib := resource.NewLibrary(os.DirFS(root))
w := hotreload.NewWatcher(cx, root)
w.WatchResources(lib, func(path string, err error) {
	if err != nil {
		log.Printf("reload %s: %v", path, err)
	}
})
```

在发布构建（`-tags release`）中 `WatchResources` 不做任何事。

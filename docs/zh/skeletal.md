# skeletal

```go
import "github.com/nsgo-dev/game/packages/game/skeletal"
```

2D 骨骼绑定、正向运动学、换肤、关键帧动画与交叉淡入混合，通过 `sprite.Batch` 绘制到 `core.Drawing` 中。

## 格式选型

`skeletal` 采用开放、带版本号的 YAML 格式（`.skel.yaml`，版本 `1`），由 `Load` 读取、`Encode` 写出，使用 `go.yaml.in/yaml/v3`：

- **无专有运行时许可限制**：Spine 官方运行时及导出格式受 Spine Runtimes License 约束，要求使用该运行时的各方持有付费 Spine 许可。开放的格式规范让 NSGo 游戏保持 MIT 许可的完全自由。
- **便于评审的文本**：与本仓库中游戏作者编辑的其他文件一样，骨骼文件是纯 YAML，每根骨骼、每个插槽、附件和关键帧各占一行，人和 AI 都能直接手改，diff 能准确显示改动了什么。
- **无额外依赖**：所用 YAML 库与 `sceneformat` 和数据表相同，不需要二进制解码器或代码生成。
- **原生屏幕坐标系**：与 `sprite` 和 `core.Drawing` 使用相同的坐标系（`+X` 向右、`+Y` 向下，旋转为屏幕顺时针弧度），无需坐标翻转或单位换算。
- **易于导出与手写**：扁平的结构（骨骼、插槽、皮肤、动画）便于从 DragonBones、Blender 或自定义脚本编写简短导出器，也可以直接手写和调试。
- **严格且带版本号**：每个文件均包含 `version: 1`；`Load` 会拒绝缺失或不支持的版本、未知字段、重复键、多余的 YAML 文档以及非有限数（`.nan`、`.inf`）。
- **JSON 仍可加载**：以前版本读取的 JSON 格式版本 1 骨骼文件，只要同时是合法的 YAML 即可加载：键名与结构定义的拼写完全一致（区分大小写），且不含 YAML 没有的 `\/` 转义。`Encode` 会将其写回为 YAML。

## 坐标与单位

- **位置（`x`、`y`、`originX`、`originY`）**：NSGo 2D 绘图空间中的点数（`+X` 向右，`+Y` 向下）。
- **旋转（`rotation`、`angle`）**：弧度，在屏幕上按顺时针方向为正。
- **缩放（`scaleX`、`scaleY`）**：围绕骨骼或附件原点的倍率，省略时默认为 `1`。
- **时间（`duration`、`time`）**：YAML 中以秒为单位的数值，范围为 `[0, MaxSeconds]`（一百万秒），加载时转换为最接近的纳秒级 `time.Duration`。

## YAML 结构（版本 1）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `version` | `int`（必填） | 格式版本号，必须为 `1`。 |
| `name` | `string` | 可选的骨骼名称。 |
| `bones` | `[]Bone` | 绑定姿势下的骨骼层级。父子顺序任意；循环引用和不存在的父骨骼会被拒绝。 |
| `slots` | `[]Slot` | 按绘制顺序（从后到前）排列的插槽，每个插槽绑定到一个骨骼。 |
| `skins` | `map[string]map[string]Attachment` | 皮肤名（默认 `"default"`）到“插槽名 -> 附件”的映射。 |
| `animations` | `map[string]Animation` | 动画名到骨骼和插槽时间轴的映射。 |

### Bone（骨骼）

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `name` | `string`（必填） | — | 唯一的骨骼名称。 |
| `parent` | `string` | `""` | 父骨骼名称；为空表示根骨骼。 |
| `x`、`y` | `float32` | `0` | 在父骨骼局部空间中的绑定姿势偏移。 |
| `rotation` | `float32` | `0` | 绑定姿势下的局部旋转（顺时针弧度）。 |
| `scaleX`、`scaleY` | `float32` | `1` | 绑定姿势下的局部缩放。 |
| `length` | `float32` | `0` | 可选的骨骼长度，供编辑器或调试视图使用。 |

### Slot（插槽）

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `name` | `string`（必填） | — | 唯一的插槽名称。 |
| `bone` | `string`（必填） | — | 该插槽跟随的骨骼名称。 |
| `attachment` | `string` | `""` | 当皮肤未指定时可选的默认图集帧名。 |
| `alpha` | `float32` | `1` | 绑定姿势下插槽的不透明度，范围 `[0, 1]`。 |

### Attachment（附件，位于 `skins[skin][slot]`）

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `frame` | `string` | `""` | `sprite.Atlas.Frames` 中的图集帧名；为空时隐藏该插槽。 |
| `x`、`y` | `float32` | `0` | 相对所属骨骼的局部偏移。 |
| `rotation` | `float32` | `0` | 局部旋转（顺时针弧度）。 |
| `scaleX`、`scaleY` | `float32` | `1` | 局部缩放。 |
| `originX`、`originY` | `float32` | `0` | 未裁剪图集帧中的枢轴点（与 `sprite.Sprite` 一致）。 |

当前皮肤中未定义的插槽会自动回退到 `"default"` 皮肤。若要在皮肤中隐藏某个插槽，显式设置 `frame: ""` 即可。

### Animation（动画）

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `duration` | `float64` | 最后一个关键帧时间 | 动画时长（秒）；省略时自动取所有时间轴的最大关键帧时间。 |
| `loop` | `bool` | `false` | 文件记录的默认循环标志。 |
| `bones` | `map[string]BoneTimeline` | — | 每个骨骼的 `translate`、`rotate`、`scale` 轨道。 |
| `slots` | `map[string]SlotTimeline` | — | 每个插槽的 `attachment` 和 `alpha` 轨道。 |

骨骼轨道相对于绑定姿势生效：`translate`（`x`、`y`）和 `rotate`（`angle`）叠加到绑定姿势上，`scale`（`x`、`y`，省略时默认为 `1`）与绑定姿势相乘。旋转轨道始终沿跨越 `±π` 的最短角路径插值。

插槽 `attachment` 关键帧（`{time, frame}`）在到达 `time` 时立即切换帧（空字符串隐藏插槽）；`alpha` 关键帧（`{time, value}`）在 `[0, 1]` 内插值不透明度。

每个连续关键帧都可指定从该帧到下一帧的插值曲线 `curve`：

- `"linear"`（默认）：线性匀速插值。
- `"stepped"`：保持当前关键帧值，直到下一关键帧时间点跳变。
- `"bezier"`：三次贝塞尔曲线，配合 `bezier: [cx1, cy1, cx2, cy2]`，其中 `cx1`、`cx2` 必须在 `[0, 1]` 内（`cy1`、`cy2` 允许超调）。其他曲线上给出控制点会被拒绝。

### 示例角色

包含 7 根骨骼、2 套皮肤（`"default"` 与 `"armored"`）及 3 段动画（`"walk"`、`"run"`、`"jump"`）的完整示例见 [runner.skel.yaml](../../packages/game/skeletal/testdata/runner.skel.yaml)。精简片段如下：

```yaml
version: 1
name: runner
bones:
  - {name: root}
  - {name: torso, parent: root, y: -18, length: 16}
  - {name: head, parent: torso, y: -16, length: 12}
slots:
  - {name: torso, bone: torso}
  - {name: head, bone: head}
skins:
  armored:
    head: {frame: helmet, originX: 6, originY: 12}
    torso: {frame: torso_armor, originX: 6, originY: 16}
  default:
    head: {frame: head, originX: 6, originY: 12}
    torso: {frame: torso, originX: 6, originY: 16}
animations:
  walk:
    duration: 0.8
    loop: true
    bones:
      torso:
        translate:
          - {time: 0, x: 0, y: 0, curve: bezier, bezier: [0.25, 0.1, 0.25, 1]}
          - {time: 0.2, x: 0, y: -2}
          - {time: 0.4, x: 0, y: 0}
```

### 规范形式

`Encode` 只写出一种规范形式，因此保存骨骼文件本身不会产生 diff：

- 键按上文的结构顺序排列；皮肤、附件、动画和时间轴按名称以普通字节序排序，与 `sort.Strings` 一致（`B` 在 `a` 之前，`a10` 在 `a2` 之前）；骨骼和插槽保持原有顺序。
- 每根骨骼、每个插槽、附件和关键帧都是独占一行的流式映射。
- 等于默认值的字段会被省略，但关键帧总是写出 `time` 和取值，动画总是写出 `duration`。
- 数值使用能读回同一值的最短数字，以不带指数的普通十进制写出（`1000000`、`0.0000001`），因此 PyYAML 等 YAML 1.1 解析器也会将其读为数值；时间能读回到同一纳秒。
- 注释、锚点等格式信息不会保留。

加载已是该形式的文件再编码，得到完全相同的字节；加载 `Encode` 写出的内容，得到相同的骨骼数据。凡是 `Load` 会拒绝的骨骼数据，`Encode` 也会以相同的错误拒绝。

## 运行时 API

### 加载与创建

- `Load(data []byte) (*SkeletonData, error)` 解析并校验骨骼文件，拒绝骨骼环、缺失父骨骼、重名骨骼/插槽、无效引用与非法曲线。同一份 `*SkeletonData` 可被任意多个 `Skeleton` 实例共享。
- `Encode(sd *SkeletonData) ([]byte, error)` 将骨骼数据写成规范形式的 `.skel.yaml` 文档，供保存骨骼文件的编辑器和导出器使用。
- `NewSkeleton(data *SkeletonData, atlas *sprite.Atlas) *Skeleton` 创建一个原始缩放、完全不透明、处于绑定姿势的骨骼实例；若数据定义了 `"default"` 皮肤，会自动激活它。

### 根变换、皮肤与附件

- 根字段 `X`、`Y`、`Rotation`、`ScaleX`、`ScaleY`、`FlipX`、`FlipY`、`Alpha` 和 `Z` 用于控制整套骨骼的位置、旋转、缩放、镜像、透明度与层级。
- `SetSkin(name string) error` 切换当前皮肤（未覆盖的插槽回退到 `"default"`）。
- `SetAttachment(slot, frame string)` 覆盖指定插槽绘制的图集帧（空字符串隐藏该插槽）；`ClearAttachment(slot string)` 清除手动覆盖。
- `UpdateWorldTransforms()` 沿父子骨骼树执行正向运动学；`WorldBone(name string) (BoneTransform, bool)` 读取任意骨骼的世界位置、旋转和缩放，便于挂接武器、判定框或粒子。
- `Draw(d *core.Drawing)` 更新世界变换，并通过可复用的 `sprite.Batch` 按插槽顺序绘制所有可见附件。

### 动画与交叉淡入混合

- `Play(anim string, loop bool) error` 从 0 时刻开始播放 `anim`，取消正在进行的混合并立即更新世界变换。
- `Crossfade(anim string, duration time.Duration, loop bool) error` 在 `duration` 时长内从当前姿态平滑过渡到 `anim`：旋转采用最短角混合，平移、缩放与透明度采用线性混合。如果在混合中途再次调用 `Crossfade`，会先快照当前混合姿态，避免跳帧。
- `Update(dt time.Duration)` 确定性地推进动画与混合，并自动刷新世界变换。
- `Animation() string`、`Done() bool` 和 `Blending() bool` 用于查询当前播放状态。

```go
data, err := skeletal.Load(runnerYAML) // runner.skel.yaml 的字节
if err != nil {
	return err
}
hero := skeletal.NewSkeleton(data, atlas)
hero.X, hero.Y = 200, 300
_ = hero.Play("walk", true)

loop := &game.Loop{
	Update: func(dt time.Duration) {
		if sprinting && hero.Animation() != "run" {
			_ = hero.Crossfade("run", 200*time.Millisecond, true)
		}
		hero.Update(dt)
		if hand, ok := hero.WorldBone("arm_r"); ok {
			emitSpark(hand.X, hand.Y)
		}
	},
}
// 在 core.Canvas 的 Draw 中：
hero.Draw(d)
```

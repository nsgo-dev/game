# skeletal

```go
import "github.com/nsgo-dev/game/packages/game/skeletal"
```

2D 骨骼绑定、正向运动学、换肤、关键帧动画与交叉淡入混合，通过 `sprite.Batch` 绘制到 `core.Drawing` 中。

## 格式选型

`skeletal` 采用开放、带版本号的 JSON 格式（`.skel.json`，版本 `1`），仅使用 Go 标准库 `encoding/json` 解析：

- **无专有运行时许可限制**：Spine 官方运行时及导出格式受 Spine Runtimes License 约束，要求使用该运行时的各方持有付费 Spine 许可。开放的 JSON 规范让 NSGo 游戏保持 MIT 许可的完全自由。
- **零额外依赖**：完全依赖 Go 标准库解析，不需要第三方解析库、二进制解码器或代码生成。
- **原生屏幕坐标系**：与 `sprite` 和 `core.Drawing` 使用相同的坐标系（`+X` 向右、`+Y` 向下，旋转为屏幕顺时针弧度），无需坐标翻转或单位换算。
- **易于导出与手写**：扁平的 JSON 结构（骨骼、插槽、皮肤、动画）便于从 DragonBones、Blender 或自定义脚本编写简短导出器，也可以直接手写和调试。
- **带版本号**：每个文件均包含 `"version": 1`，`Load` 会明确拒绝缺失或不支持的版本。

## 坐标与单位

- **位置（`x`、`y`、`originX`、`originY`）**：NSGo 2D 绘图空间中的点数（`+X` 向右，`+Y` 向下）。
- **旋转（`rotation`、`angle`）**：弧度，在屏幕上按顺时针方向为正。
- **缩放（`scaleX`、`scaleY`）**：围绕骨骼或附件原点的倍率，省略时默认为 `1`。
- **时间（`duration`、`time`）**：JSON 中以秒为单位的浮点数，加载时转换为 `time.Duration`。

## JSON 结构（版本 1）

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

当前皮肤中未定义的插槽会自动回退到 `"default"` 皮肤。若要在皮肤中隐藏某个插槽，显式设置 `"frame": ""` 即可。

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
- `"bezier"`：三次贝塞尔曲线，配合 `"bezier": [cx1, cy1, cx2, cy2]`，其中 `cx1`、`cx2` 必须在 `[0, 1]` 内（`cy1`、`cy2` 允许超调）。

### 示例角色

包含 7 根骨骼、2 套皮肤（`"default"` 与 `"armored"`）及 3 段动画（`"walk"`、`"run"`、`"jump"`）的完整示例见 [runner.skel.json](../../packages/game/skeletal/testdata/runner.skel.json)。精简片段如下：

```json
{
  "version": 1,
  "name": "runner",
  "bones": [
    { "name": "root" },
    { "name": "torso", "parent": "root", "x": 0, "y": -18, "length": 16 },
    { "name": "head", "parent": "torso", "x": 0, "y": -16, "length": 12 }
  ],
  "slots": [
    { "name": "torso", "bone": "torso" },
    { "name": "head", "bone": "head" }
  ],
  "skins": {
    "default": {
      "torso": { "frame": "torso", "originX": 6, "originY": 16 },
      "head": { "frame": "head", "originX": 6, "originY": 12 }
    },
    "armored": {
      "torso": { "frame": "torso_armor", "originX": 6, "originY": 16 },
      "head": { "frame": "helmet", "originX": 6, "originY": 12 }
    }
  },
  "animations": {
    "walk": {
      "duration": 0.8,
      "loop": true,
      "bones": {
        "torso": {
          "translate": [
            { "time": 0.0, "y": 0, "curve": "bezier", "bezier": [0.25, 0.1, 0.25, 1.0] },
            { "time": 0.2, "y": -2 },
            { "time": 0.4, "y": 0 }
          ]
        }
      }
    }
  }
}
```

## 运行时 API

### 加载与创建

- `Load(data []byte) (*SkeletonData, error)` 解析并校验骨骼文件，拒绝骨骼环、缺失父骨骼、重名骨骼/插槽、无效引用与非法曲线。同一份 `*SkeletonData` 可被任意多个 `Skeleton` 实例共享。
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
data, err := skeletal.Load(runnerJSON)
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

# lights2d

`game/lights2d` 为 NSGo 画布提供 2D 光照能力：环境暗场层、点光源与聚光灯、基于遮挡体的精确 2D 可见性多边形阴影，以及 CPU 切线空间法线贴图精灵着色。

```go
import "github.com/nsgo-dev/game/pkg/lights2d"
```

## 环境暗场、点光源与聚光灯

创建带有 `Ambient` 环境色与 `*Light` 列表的 `Scene`，在 `Canvas.Draw` 中绘制完 2D 世界后叠加绘制：

```go
torch := lights2d.NewLight(160, 120, 180, core.RGB(255, 196, 120))
torch.CastShadows = true

flashlight := lights2d.NewLight(240, 140, 260, core.RGB(220, 235, 255))
flashlight.ConeAngle = 0          // 弧度：0 为 +X，pi/2 为 +Y
flashlight.ConeSpread = math.Pi / 3
flashlight.CastShadows = true

lighting := &lights2d.Scene{
	Ambient: core.RGBA(12, 16, 30, 0.85),
	Lights:  []*lights2d.Light{torch, flashlight},
}

// 在 Canvas.Draw 中绘制完瓦片和精灵之后：
lighting.Draw(d, worldBounds)
```

- `NewLight(x, y, radius, color)` 创建默认启用的全向点光源，`Intensity: 1`，二次衰减 `Falloff: 2`。
- 将 `ConeSpread` 设为 `(0, 2*math.Pi)` 即可变为方向聚光灯，沿 `ConeAngle` 做 smoothstep 角度衰减。`Scene.Draw` 使用 16 层同心嵌套径向渐变扇形近似该 smoothstep 角度衰减（在整个光锥内与 `AttenuationAt` / `EvaluateAt` 的误差约在 3% 以内）。
- 当 `Scene.LightComposite` 未设置（零值 `0`）时，光晕默认使用加法混合 `core.CompositeLighter`。可直接赋值非零混合模式（如 `core.CompositeScreen`）获得更柔和的滤色光照，或调用 `lighting.SetLightComposite(core.CompositeSourceOver)` 显式使用常规 Alpha 叠加（因为 `core.CompositeSourceOver` 即为零值）。可通过 `lighting.EffectiveLightComposite()` 查询当前生效的混合模式。
- 可用 `light.AttenuationAt(x, y)` 或 `scene.EvaluateAt(x, y)` 在 CPU 侧查询任意世界坐标的精确照度。

## 可见性多边形阴影遮挡体

通过 `RectOccluder`、`PolygonOccluder` 或 `SegmentOccluder` 将墙体、柱子或瓦片碰撞体加入 `Scene.Occluders`：

```go
lighting.Occluders = []lights2d.Occluder{
	lights2d.RectOccluder(core.Bounds{Origin: core.Point{X: 200, Y: 90}, Size: core.Size{Width: 48, Height: 64}}),
	lights2d.SegmentOccluder(lights2d.Segment{
		A: core.Point{X: 80, Y: 180},
		B: core.Point{X: 160, Y: 180},
	}),
}
```

对于瓦片地图，[`tileshape.View`](tileshape.md) 会把实心格子与逐瓦片碰撞形状合并为遮挡体，投下相同的阴影而线段少得多：设置 `lighting.Occluders = view.Occluders()`，地图改变后再设置一次。

设置 `Scene.DisableShadows` 可以让所有光源都跳过遮挡计算，而不必逐个修改光源的 `CastShadows`，关闭阴影的画质预设就是这样做的（`lights.DisableShadows = !preset.Shadows`，见 [quality](quality.md)）。`Draw`、`EvaluateAt`、`ShadeNormalMap` 与 `NormalSprite` 都会遵循这个设置，切换它也会让 `NormalSprite` 重新着色。

当 `light.CastShadows` 为 `true` 时，`Scene.Draw` 会用 `VisibilityPolygon(origin, radius, occluders)` 裁剪该光源。你也可以直接调用几何辅助函数实现潜行视野、视线判断或战争迷雾：

| 函数 | 用途 |
| --- | --- |
| `VisibilityPolygon(origin, radius, occluders)` | 计算从 `origin` 出发在 `radius` 范围内的有序 2D 可见多边形。 |
| `Visible(origin, target, occluders)` | 判断 `origin` 到 `target` 的连线是否被遮挡体阻断。 |
| `LineOfSight(&occluders)` | 以 `ai.LineOfSight` 的形式返回针对这些遮挡体的 `Visible`，让它们同样遮挡 AI 感知（[ai](ai.md#感知)）。 |
| `PointInPolygon(p, poly)` | 测试点 `p` 是否位于可见多边形内。 |

## 法线贴图精灵

为像素画或手绘 2D 精灵配上一张切线空间法线贴图（`*image.NRGBA`），即可获得随光源方向变化的立体高光与凹凸感：

```go
knight := lights2d.NewNormalSprite(diffuseImg, normalMapImg)
knight.X = 160
knight.Y = 120
knight.LightZ = 48 // 光源相对精灵平面的虚拟 Z 高度

// 在 Canvas.Draw 中：
knight.Draw(d, lighting)
```

`NormalSprite` 会缓存 CPU 着色后的像素缓冲，仅当精灵坐标、`LightZ`、环境光或影响范围内的光源/遮挡体变化时才重新计算。若原地修改了 `Diffuse` 或 `NormalMap` 像素，请调用 `knight.Invalidate()`；若需离线烘焙自定义缓冲，可直接调用 `ShadeNormalMap`、`ShadeNormalMapWithOccluders` 或 `scene.ShadeNormalMap`。

## 在场景文件中编排光源与遮挡体

你也可以直接在 `*.scene.yaml` 场景文件中声明 `ambientColor`、`light2d` 点光源/聚光灯以及 `occluder2d` 矩形/多边形/瓦片地图遮挡体，并在 [`game/editor`](editor.md#内置-2d-光照与遮挡体组件887) 中通过实时光照预览与交互手柄进行编辑。`sceneformat.Build` 会将其构建为 `BuiltScene.Lights2D`，在编辑器预览与游戏中投射完全一致的阴影。

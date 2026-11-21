# render3d

`game/render3d` 提供与后端无关、稳态零分配的 CPU 侧 3D 渲染管线与开发工具：视锥体裁剪、不透明/半透明绘制排序、自动实例化合批、带滞回防抖动的 LOD 选择、3D 调试线框与变换手柄（Gizmo）、帧性能分析器，以及移动端/桌面端 GPU 预算检查。

```go
import "github.com/nsgo-dev/game/packages/game/render3d"
```

所有矩阵、包围盒、射线与视锥体均遵循 [math3d](math3d.md) 约定。

## 视锥裁剪、绘制排序、实例化与 LOD（`Queue`）

跨帧复用同一个 `Queue`，把当前帧的 `[]DrawItem` 传入 `Queue.Build(viewProj, camPos, items)`：

```go
queue := &render3d.Queue{
	LODHysteresis: 0.1, // 10% 滞回区间，防止边界处 LOD 反复闪烁
}

plan := queue.Build(cam.ViewProj(), cam.Pos, drawItems)

// 1. 提交不透明实例化批次（连续的 Mat4 变换切片）：
for _, batch := range plan.Batches {
	submitInstanced(batch.PipelineID, batch.MaterialID, batch.MeshID, batch.Transforms)
}
// 2. 严格按由远及近提交半透明绘制：
for _, draw := range plan.Transparent {
	submitDraw(draw)
}
```

`Queue.Build` 在单次稳态零分配流程中完成：

1. **视锥裁剪**：通过 `math3d.FrustumFromMat4`（开启 `Queue.FrustumNO` 时用 `FrustumFromMat4NO`）提取六平面并剔除视锥外的 `WorldBounds`。
2. **带滞回的 LOD 选择**：结合相机距离与 `ProjectScreenRatio` 评估每个物体的 `LODs`（`[]LODLevel`，由精细到粗糙），按 `DrawItem.ID` 记忆上一帧级别（`SelectLOD`）。镜头硬切时调用 `queue.ResetLODHistory()`。
3. **不透明排序与实例化**：将 `BlendOpaque` 与 `BlendAlphaMask` 按 `(PipelineID, MaterialID, MeshID, Blend, 由近及远距离)` 排序以减少状态切换与早期深度过绘，并将相邻同材质同网格项合并为 `InstanceBatch`（除非设置 `DisableInstancing`）。
4. **半透明排序**：将 `BlendAlphaBlend` 项放入 `plan.Transparent`，严格按由远及近排序。

## 3D 调试线框与变换手柄（Gizmo）

`DebugDraw` 将 3D 线框经近裁剪面裁剪后直接投影绘制到 NSGo 的 `*core.Drawing` 叠加层：

```go
var dbg render3d.DebugDraw
dbg.Grid(math3d.Vec3Zero(), 10, 20, core.RGBA(120, 130, 150, 0.4))
dbg.AABB(enemyBounds, core.RGB(255, 80, 80))
dbg.Sphere(alertCenter, 4, core.RGB(255, 200, 60), 24)
dbg.Axes(selectedPos, 1.5)
dbg.Flush(d, cam.ViewProj(), viewportBounds)
dbg.Clear()
```

面向关卡编辑器和场景检查器，`Gizmo` 支持平移、旋转与缩放操作柄（`GizmoTranslate`、`GizmoRotate`、`GizmoScale`）：

```go
var gizmo render3d.Gizmo
axis, _ := gizmo.PickAxis(ray, objPos, gizmoScale, render3d.GizmoTranslate)
if axis != render3d.AxisNone {
	delta := gizmo.DragTranslate(objPos, axis, startRay, currentRay, 0.25) // 0.25m 步进吸附
	objPos = objPos.Add(delta)
}
gizmo.DrawGizmo(d, cam.ViewProj(), viewportBounds, objPos, gizmoScale, render3d.GizmoTranslate, axis)
```

## 渲染统计、分析器与 GPU 预算

`plan.Stats`（`RenderStats`）精确记录裁剪/可见物体数、DrawCall 数、实例化批次数、三角形与顶点数、材质/管线切换次数及显存占用。用 `NewFrameProfiler(120)` 统计滑动窗口帧耗时（`Record`、`AverageFrameTime`、`P95FrameTime`、`FPS`、`PeakDrawCalls`、`PeakTriangles`），并用 `CheckBudget` 在开发构建或 CI 测试中校验预算：

| 预设 | MaxDrawCalls | MaxTriangles | MaxVertices | MaxMaterialSwitches | MaxPipelineSwitches | MaxTextureBytes | MaxBufferBytes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `MobileBudget()` | 150 | 150,000 | 300,000 | 60 | 32 | 128 MiB | 64 MiB |
| `DesktopBudget()` | 2,000 | 2,000,000 | 4,000,000 | 500 | 200 | 1 GiB | 512 MiB |

```go
if violations := render3d.CheckBudget(plan.Stats, render3d.MobileBudget()); len(violations) > 0 {
	for _, v := range violations {
		log.Println("budget exceeded:", v)
	}
}
```

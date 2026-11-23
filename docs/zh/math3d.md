# 3D 数学约定

`math3d` 提供纯 Go 实现的 `float32` 向量、矩阵、四元数、变换、包围体、射线、平面与视锥体，遵循与 glTF 2.0、WebGPU 和 Metal 一致的统一数学约定。

```go
import "github.com/nsgo-dev/game/packages/game/math3d"
```

## 速查表

| 项目 | 约定 | 说明 |
| --- | --- | --- |
| 坐标系手性 | 右手坐标系，Y 轴向上 | `+X` 向右、`+Y` 向上、`-Z` 向前 |
| 角度单位 | 弧度 | 沿旋转轴正方向回看原点为逆时针；提供 `DegToRad` / `RadToDeg` |
| 矩阵存储 | 列主序（`[9]float32`、`[16]float32`） | 直接匹配 WGSL `mat4x4<f32>`、Metal `simd::float4x4`、GLSL `mat4`、glTF 2.0 |
| 向量约定 | 列向量（`M * v`） | 矩阵乘法自右向左组合：`(A * B) * v = A * (B * v)` |
| 变换顺序 | `TRS`（`T * R * S`） | 先缩放、再旋转、最后平移 |
| 裁剪空间深度（NDC） | 默认 `[0, 1]`（`ZO`） | 适配 WebGPU / Metal / Vulkan；`[-1, 1]` 可用 `*NO` 变体 |
| 四元数 | `(X, Y, Z, W)` | 单位四元数 `{0, 0, 0, 1}`；与 glTF 2.0 `[x, y, z, w]` 一致 |
| 欧拉角顺序 | 默认内旋 `YXZ` | `Ry(yaw) * Rx(pitch) * Rz(roll)`；`XYZ` 可用 `*XYZ` 变体 |

## 坐标系

`math3d` 采用**右手、Y 轴向上**的坐标系：

- `+X` 指向**右方**（`Vec3Right()`）。
- `+Y` 指向**上方**（`Vec3Up()`）。
- `-Z` 指向**前方**（`Vec3Forward()`）。位于原点、使用单位视图矩阵的相机朝 `-Z` 方向观察，`+Z` 指向屏幕外侧（朝向观察者）。

叉积满足右手定则：

```text
+X × +Y = +Z
+Y × +Z = +X
+Z × +X = +Y
```

所有旋转函数均使用**弧度**。从旋转轴正方向看向原点时，正角度表示**逆时针**旋转（右手拇指指向 `+axis`，其余四指弯曲方向为正角方向）：

- `RotateX(+π/2)` 将 `+Y` 旋转到 `+Z`。
- `RotateY(+π/2)` 将 `-Z`（前方）旋转到 `-X`（左方）。
- `RotateZ(+π/2)` 将 `+X` 旋转到 `+Y`。

## 矩阵与存储布局

`Mat3`（`[9]float32`）与 `Mat4`（`[16]float32`）均以**列主序**存储。第 `row` 行、第 `col` 列的元素在 `Mat3` 中的索引为 `col*3 + row`，在 `Mat4` 中为 `col*4 + row`：

```text
Mat4 内存索引：
  第 0 列  第 1 列  第 2 列  第 3 列
[  m[0]    m[4]    m[8]   m[12] ]   第 0 行
[  m[1]    m[5]    m[9]   m[13] ]   第 1 行
[  m[2]    m[6]   m[10]   m[14] ]   第 2 行
[  m[3]    m[7]   m[11]   m[15] ]   第 3 行
```

由于 WGSL（`mat4x4<f32>`）、Metal（`simd::float4x4`）、GLSL（`mat4`）以及 glTF 2.0 的 `node.matrix` 全都是列主序，`Mat4` 可以直接上传到 GPU uniform 缓冲区而无需转置。

向量作为列向量右乘矩阵（`M * v`），因此矩阵乘法按**自右向左**的顺序生效：

```go
// 先缩放，再旋转，最后平移：M = T * R * S
model := math3d.TRS(pos, rot, scale)

// 模型 -> 世界 -> 视图 -> 裁剪：MVP = P * V * M
mvp := proj.Mul(view).Mul(model)
```

在非均匀缩放下变换表面法线时，请使用 `NormalMatrix(model)`（左上 3×3 的逆转置矩阵）并在变换后重新归一化。

## 裁剪空间与深度范围

`Perspective` 和 `Ortho` 默认采用 **零到一（`[0, 1]`）** 的 NDC 深度范围，与 **WebGPU**、**Metal**、**Vulkan** 和 **Direct3D** 一致：

- 视图空间的 `z = -near` 映射到 NDC `z = 0`。
- 视图空间的 `z = -far` 映射到 NDC `z = 1`。
- 视图空间的 `x`、`y` 映射到 `[-1, 1]`。

包内同时提供两种深度约定的显式函数，面向 OpenGL / WebGL 时可直接选用 `[-1, 1]`：

| 深度范围 | 透视投影 | 正交投影 | 视锥体提取 |
| --- | --- | --- | --- |
| `[0, 1]`（WebGPU / Metal / Vulkan） | `Perspective` / `PerspectiveZO` | `Ortho` / `OrthoZO` | `FrustumFromMat4` / `FrustumFromMat4ZO` |
| `[-1, 1]`（OpenGL / WebGL） | `PerspectiveNO` | `OrthoNO` | `FrustumFromMat4NO` |

## 四元数与变换

`Quat` 按 `{X, Y, Z, W}` 存储，单位四元数为 `QuatIdentity() = {0, 0, 0, 1}`，与 glTF 2.0 的 `[x, y, z, w]` 顺序一致。

- **双覆盖等价**：`q` 与 `-q` 表示同一个 3D 旋转。`Slerp` 与 `Nlerp` 会在 `q.Dot(b) < 0` 时自动取反目标四元数，确保始终沿**最短弧**插值。
- **乘法组合**：`q1.Mul(q2)` 先应用 `q2` 再应用 `q1`，与 `q1.ToMat4().Mul(q2.ToMat4())` 完全一致。
- **欧拉角**：`QuatFromEuler(pitch, yaw, roll)` 与 `ToEuler()` 采用相机与角色常用的内旋 **YXZ** 顺序（`Ry(yaw) * Rx(pitch) * Rz(roll)`）；需要内旋 **XYZ** 顺序（`Rx(x) * Ry(y) * Rz(z)`）时可使用 `QuatFromEulerXYZ` 与 `ToEulerXYZ`。两者都以 float64 提取，偏航与横滚在接近 ±90° 万向节极点时仍能分开，只有恰好锁死时才把横滚并入偏航。
- **朝向与视图矩阵**：`LookRotation(forward, up)` 构造一个世界朝向四元数，使物体局部 `-Z` 对齐 `forward`、局部 `+Y` 尽量朝向 `up`。对于位于 `eye`、看向 `target` 的相机，`Transform{Pos: eye, Rot: LookRotation(target.Sub(eye), up), Scale: Vec3One()}.ToMat4()` 恰好是 `LookAt(eye, target, up)` 的逆矩阵。

`Transform` 组合了 `Pos`、`Rot` 与 `Scale`，用于场景层级与插值（`Lerp` 对位置和缩放线性插值，对旋转使用 `Slerp`）。`parent.Mul(child)` 直接组合父子 TRS；若父节点含非均匀缩放且子节点带旋转（会产生切变），请转为 `Mat4` 相乘。

## 包围体、射线、平面与视锥体

- **`AABB`**：轴对齐包围盒 `{Min, Max}`。通过 `AABBEmpty()` 创建空包围盒并用 `ExpandPoint` / `Union` 扩展，`Transform(m)` 可计算变换后的紧凑包围盒。
- **`Sphere`**：包围球 `{Center, Radius}`，支持 `Contains`、`IntersectsSphere` 与 `IntersectsAABB`。
- **`Plane`**：平面 `{Normal, D}`，满足 `Normal · p + D = 0`。经 `Normalized()` 归一化后，`SignedDist(p)` 返回有符号欧氏距离（法线指向的一侧为正）。
- **`Ray`**：射线 `P(t) = Origin + t * Dir`（`t >= 0`），提供 `IntersectAABB`（slab 算法）、`IntersectSphere`、`IntersectPlane` 与 `IntersectTriangle`（双面 Möller–Trumbore 算法，返回 `(t, u, v, hit)`）。平行判定按向量长度取相对阈值，很小的三角形和很短的射线方向也能命中。
- **`Frustum`**：由 6 个指向内部、已归一化的平面（`FrustumLeft` 至 `FrustumFar`）组成的视锥体，使用 Gribb–Hartmann 算法从任意透视或正交视图投影矩阵提取（`[0, 1]` 用 `FrustumFromMat4`，`[-1, 1]` 用 `FrustumFromMat4NO`），支持 `ContainsPoint`、`IntersectsSphere` 与 `IntersectsAABB`。恰好落在平面上的点视为在内部，并按平面到原点的距离留出少量余量，避免浮点舍入把远处远平面上的几何体剔除。

```go
view := math3d.LookAt(math3d.Vec3{X: 0, Y: 3, Z: 8}, math3d.Vec3Zero(), math3d.Vec3Up())
proj := math3d.Perspective(math3d.DegToRad(60), 16.0/9.0, 0.1, 200)
frustum := math3d.FrustumFromMat4(proj.Mul(view))

if frustum.IntersectsAABB(worldBounds) {
	// 物体在视锥体内，需要绘制。
}
```

## 曲线

`QuadraticBezier3D`、`CubicBezier3D`、`CubicHermite3D`、`HermiteSpline3D` 和 `CatmullRom3D` 共用 `Curve3D` 接口：`Point`、`Tangent`、`Length`、按弧长查找（`DistanceToT`、`PointAtDistance`），以及供沿路径移动的相机和物体使用的旋转最小化标架（`FrameAt`、`FrameAtDistance`）。

- `NewHermiteSpline3D` 对 `tangents` 中没有对应项的点使用均匀 Catmull-Rom 切线 `(next - previous) / 2`；希望曲线在某点停住时，显式传入零切线。
- `NewCatmullRom3DAlpha` 的 `alpha` 为 0 时是均匀样条，即使相邻点重合，节点间距也保持均匀；默认的 `NewCatmullRom3D` 是向心参数化（`alpha` 为 0.5）。

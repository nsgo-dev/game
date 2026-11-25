# physics3d

```go
import "github.com/nsgo-dev/game/packages/game/physics3d"
```

纯 Go 实现的紧凑、确定性 3D 碰撞检测、射线检测、刚体动力学与运动学角色控制器。

## 架构决策（#180）

`physics3d` 采用**方案 (a)**：仅依赖 Go 标准库的零 cgo 纯 Go 实现。

选择纯 Go 而不封装 C/C++ 物理引擎（如 Jolt、Bullet 或 PhysX）的原因：

- **全平台开箱即用**：macOS、Windows、Linux、iOS、Android 和 WebAssembly 都可以用普通的 `go build` / `go tool nsgo check` 直接编译，不需要 C/C++ 工具链、各平台静态库或 Wasm/FFI 胶水代码。
- **零 FFI 开销**：重叠查询、射线检测、刚体步进和角色移动直接在 Go 值上运行，没有 cgo 调用边界成本。
- **按位一致的确定性**：配合 `game.Loop` 的固定步长推进 `World`，宽相、窄相、冲量求解和位置修正都按固定的刚体与接触顺序执行，不依赖 map 遍历顺序或墙钟时间，相同的初始状态在多次运行中产生完全一致的轨迹。

包的能力范围保持克制和清晰：盒体（轴对齐或带朝向的 Box）、球体（Sphere）、胶囊体（Capsule）、凸包（ConvexHull）和圆柱体（Cylinder），可以与物体和角色控制器碰撞的静态三角网格（`TriMeshCollider`，用 `World.AddTriMesh` 加入）、高度场（`HeightfieldCollider`，用 `World.AddHeightfield` 加入）与体素网格（`VoxelCollider`，用 `World.AddVoxelCollider` 加入），精确的重叠、射线与体素扫掠查询，带线性与角动力学的刚体、关节、布娃娃，以及胶囊角色控制器。刚体使用标量（各向同性）惯量而非完整的惯性张量。盒体、球体与胶囊体之间每对刚体的接触只用一个代表点；涉及凸包或圆柱体的一对刚体最多得到四个分布在接触区域上的点。

## 坐标系约定

`physics3d` 采用右手、Y 轴向上的坐标系（`+X` 向右，`+Y` 向上，`+Z` 朝向观察者）。`NewWorld()` 将默认重力设为 `(0, -9.81, 0)`。

## 形状、重叠查询与射线检测

五种基本形状通过带标签的 `Shape` 封装（`BoxShape`、`SphereShape`、`CapsuleShape`、`ConvexHullShape`、`CylinderShape`）：

- `Box`：由 `Center` 和 `HalfExtents` 定义的盒体（也可用 `BoxFromMinMax(min, max)` 构造）。默认轴对齐；外层 `Shape` 设置 `Rot` 后，盒体绕自身 `Center` 旋转。`Shape.Transformed(pos, rot)` 与 `Body.WorldShape()` 会保留旋转刚体的盒体朝向，重叠检测、射线检测和调试绘制都按真实的角点计算；`Shape.Bounds()` 返回用于宽相的外接轴对齐包围盒。
- `Sphere`：由 `Center` 和 `Radius` 定义的球体。
- `Capsule`：由线段 `A` 到 `B` 沿半径 `Radius` 扫略而成的胶囊体（也可用 `VerticalCapsule(center, radius, height)` 构造竖直胶囊）。
- `ConvexHull`：凸多面体，用于岩石、碎块、家具、楔形块和低多边形载具外壳等道具，由 `NewConvexHull(points, maxVertices)` 构建一次，之后被所有使用它的形状和刚体只读共享。顶点相对于 `Center`，外层 `Shape` 的 `Rot` 让它像盒体一样绕 `Center` 旋转。
- `Cylinder`：两端为平面端盖的实心圆柱体，轴线从 `A` 到 `B`，半径为 `Radius`（也可用 `VerticalCylinder(center, radius, height)` 构造），用于木桶、车轮和柱子。`A` 与 `B` 重合或 `Radius` 不为正的圆柱体没有体积，碰撞、射线检测和区域都会忽略它。

### 凸包

`NewConvexHull` 用 quickhull 构建凸包：先取极值点组成四面体，再不断加入离当前凸包最远的外部点，直到没有外部点或顶点数达到 `maxVertices`（上限 ≤ 0 时取 `DefaultHullVertices`，即 64，最多 `MaxHullVertices`，即 256；上限为 1 到 3 时提高到 4，这是有体积的凸包最少的顶点数）。上限提前终止构建时，凸包保留最突出的角点，并位于完整凸包之内。凸包内部或离某个面在很小容差以内的点会被丢弃，共面的三角形合并为多边形面，所以立方体得到 8 个顶点和 6 个正方形面。结果只取决于点及其顺序，相同输入总是得到相同的凸包。不能张成体积的点（少于四个、全部共面或共线、或不是有限值）返回 `ErrDegenerateHull`。

`ConvexHullFromMesh(positions, indices, maxVertices)` 从三角网格构建凸包，只使用索引引用到的顶点，所以 glTF 图元或 `render3d.Mesh` 的 `Positions` 和 `Indices` 可以直接传入：

```go
doc, _ := gltf.LoadFS(assets, "rock.gltf")
prim := doc.Meshes[0].Primitives[0]
hull, err := physics3d.ConvexHullFromMesh(prim.Positions, prim.Indices, 32)
if err != nil {
	return err
}
// Move the hull's centroid to the body's origin, so the body rotates about its center of mass.
hull = hull.Recentered()
pos := hull.Center
hull.Center = physics3d.Vec3{}
rock := world.AddBody(physics3d.NewDynamicBody(physics3d.ConvexHullShape(hull), pos, 40))
```

凸包可以给出 `Vertices`、`Faces`（从外侧看逆时针排列的顶点索引）、`FaceNormals`、`Edges`、`Volume`、`Centroid`，以及均匀密度下的 `InertiaTensor(mass)`。刚体的惯量取自凸包的精确惯性张量（取其主惯量的平均值，即上文的标量惯量），圆柱体的惯量取自实心圆柱公式。

重叠检测返回 `Contact`，其中单位法线 `Normal` 从 `B` 指向 `A`（将 `A` 沿 `Normal` 移动 `Depth` 即可分离两者）：

- `TestBoxBox`、`TestOrientedBoxBox`（在 15 条面轴与边轴上做分离轴测试）、`TestSphereSphere`、`TestCapsuleCapsule`、`TestBoxSphere`、`TestBoxCapsule`、`TestSphereCapsule`，以及能处理带朝向盒体的 `TestShapes(a, b)`。
- 涉及凸包或圆柱体的每一对形状，无论另一方是什么形状，`TestShapes` 都用 GJK 求距离、用 EPA 求穿透深度与法线，以 float64 和固定的迭代顺序计算，相同的形状总是得到相同的接触。`Point` 是接触区域的中心（一个面贴在另一个面上时报告重叠部分的中点，而不是某个角点）。同样的检测也用于 `TriMeshCollider` 和 `HeightfieldCollider` 的每个三角形，以及体素碰撞体。
- 胶囊的线段穿过盒体时，`TestBoxCapsule` 在盒体各面轴与“边轴叉乘线段方向”中取穿透最浅的轴，因此把盒体沿 `Normal*Depth` 移动即可让整个胶囊分离。两个交叉的胶囊，以及球心落在胶囊轴线上的球，得到垂直于轴线的法线，而不是任意方向。
- `world.QueryOverlap(shape)` 按确定的插入顺序返回所有重叠的刚体。

射线检测支持对单个形状测试 `Ray{Origin, Dir, MaxDist}`（`RaycastBox`、`RaycastSphere`、`RaycastCapsule`、`RaycastConvexHull`、`RaycastCylinder`、`RaycastShape`），以及对整个世界发射线段（`world.Raycast(from, to)` 和 `world.RaycastFirst(from, to)`），返回按 `[0, 1]` 范围内的 `T` 升序排列的 `RaycastHit`，包含世界坐标命中点 `Point`、距离 `Distance` 和向外单位法线 `Normal`。起点位于任意形状内部的射线在 `T` 为 0 处命中，`Point` 为起点，`Normal` 沿射线反方向（`-Dir`）。

### 形状投射（Shape Cast）

将任意 `Shape`（`Box`、`Sphere` 或 `Capsule`）以朝向 `rot` 置于 `origin` 并沿位移 `delta` 扫掠，检测与刚体、三角网格（`TriMeshCollider`）、高度场（`HeightfieldCollider`）以及体素碰撞体（`VoxelCollider`）的首次碰撞：

- `world.ShapeCast(shape, origin, rot, delta)` 返回最早命中的 `RaycastHit`（包含 `Body`/`TriMesh`/`Heightfield`/`Voxel`、`T`、`Distance`、`Point`、`Normal`）；`world.ShapeCastAll(...)` 按 `T` 升序返回全部命中，采用与 `Raycast` 相同的确定性决胜顺序（`Body` < `TriMesh` < `Heightfield` < `Voxel`，再按 ID 升序）。
- 使用 `world.ShapeCastLayer(shape, origin, rot, delta, mask, ignore)` 与 `ShapeCastAllLayer` 按 `LayerMask` 过滤并忽略指定 `*Body`，或使用 `ShapeCastWith` / `ShapeCastAllWith`（`ShapeCastOptions`）额外支持 `IgnoreBodyID` 与 `IgnoreTriggers`。
- 独立辅助函数 `ShapeCastShape(moving, delta, target)`、`TriMeshCollider.ShapeCast(shape, delta)` 与 `HeightfieldCollider.ShapeCast(shape, delta)` 可直接测试单个碰撞体。球体、盒体/OBB 与盒体-三角形对采用精确解析扫掠（闵可夫斯基和与运动 SAT）；胶囊-盒体、胶囊-胶囊与胶囊-三角形对采用精确距离保守推进（误差 `<= 1e-4`）。初始重叠在 `T == 0`、`Distance == 0` 处返回 `TestShapes` 分离法线。

```go
// 基于 3D 形状投射的第三人称相机弹簧臂，在碰到墙面前拉近相机避免穿墙。
cameraProbe := physics3d.SphereShape(physics3d.Sphere{Radius: 0.25})
camPos := pivot.Add(armDelta)
if hit, ok := world.ShapeCastLayer(cameraProbe, pivot, physics3d.QuatIdentity(), armDelta, physics3d.LayerAll, playerBody); ok {
	camPos = pivot.Add(armDelta.Scale(hit.T))
}
```

## 刚体动力学

`World` 管理静态（`Static: true` 或零质量）和动态 `Body`，在 `Step(dt)` 中通过半隐式欧拉积分、确定性碰撞检测、带热启动和三维各向同性库仑摩擦的迭代冲量求解器、非线性高斯-赛德尔位置修正以及静止休眠机制推进模拟。

```go
world := physics3d.NewWorld()
world.AddBody(physics3d.NewStaticBody(
	physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 1, Z: 20}}),
	physics3d.Vec3{Y: -1},
))
crate := world.AddBody(physics3d.NewDynamicBody(
	physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
	physics3d.Vec3{Y: 3},
	1.0,
))

loop := &game.Loop{
	Update: func(dt time.Duration) {
		world.Step(dt)
		_ = crate.Pos
	},
}
```

接触冲量作用在接触点上，因此摩擦会让滑动的球滚起来，立在棱上的盒子会倒向一个面。`LockRotation` 可禁止动态刚体旋转。`AddBody` 会把非有限的输入（如 NaN 速度或质量）回退为默认值，没有质量的运动学刚体仍保持运动学。

### 物理材质

可以将共享的 `*PhysicsMaterial`（`Friction`、`Restitution`、`LinearDamping`、`AngularDamping`）挂到 `Body.Material` 上，或调用 `Body.ApplyMaterial(mat)` 将其数值复制到刚体上。当 `Body.Material` 非空时，接触求解与速度阻尼直接读取材质经限幅后的表面属性（`Friction >= 0`、`Restitution` 在 `[0, 1]` 内、阻尼非负），因此修改共享材质会立即作用于所有引用它的刚体。来自 [`game/resource`](resources.md#physicsmaterial) 的共享 `*.physics.yaml` 文件可通过 `mat.ApplyTo3D(body)` 应用到 3D 刚体。

静止的刚体会进入休眠。以下情况会唤醒休眠刚体：运动中的动态刚体或运动学刚体（通过速度运动，或直接设置 `Pos`/`Rot`）碰到它；与它相连的关节另一端在运动；它的某个接触结束（支撑物移开）；附近的 `VoxelCollider` 方块改变实体性或脚下支撑方块消失；以及调用 `Wake`、`WakeInBox`、`ApplyImpulse` 或移除刚体、网格、体素碰撞体、关节时。

堆叠的刚体静止时不会互相陷入。迭代求解之后，每一步会自下而上对与竖直方向（以 `-Gravity` 为上）夹角约 45° 以内的接触再做一遍：把下方刚体当作不可移动，让上方刚体停止向下靠近，并把它推出到只剩约 1 mm 的重叠（冲击传播，shock propagation）。重物压在轻物上，或很高的一叠刚体，都会停在这个深度，而不是像只做迭代求解那样在大质量比下残留几厘米的穿透。仍陷入相邻刚体超过 5 mm 的刚体不会进入休眠，被压得深于 1 cm 的休眠刚体会被唤醒，因此休眠不会把穿透冻结下来。重力为零时没有"上"方向，这一遍会跳过。

落到高度场表面以下的形状会被向上推出，推出深度沿朝上的三角形法线测量，因此一次修正就能离开表面，而不是只推出到下方最近点的较浅距离。位于格子旁边的形状（例如站在台阶下、碰到台面边缘的形状）保留自身接触的深度，不会被抬到台面上。

单步位移超过自身最小尺寸一半的动态刚体，会以子步对静态、运动学和休眠刚体以及三角网格、高度场、体素碰撞体做扫掠，在第一次出现新的重叠处停下，因此高速刚体不会穿过薄的几何体。本步开始时已经存在的接触（例如高速滑行或滚动的刚体身下的地面）不会让它停下，除非刚体陷入其中明显更深。高速动态刚体之间不做扫掠。

## 体素碰撞体

`VoxelCollider`（通过 `NewVoxelCollider(grid)` 创建并用 `World.AddVoxelCollider` 加入，或由 `game/voxel` 的 `voxel.NewCollider(w)` / `w.Collider()` 直接适配）无需构建三角网格，即可让刚体、`CharacterController`、CCD 以及世界级射线/重叠查询直接与任意 `VoxelGrid` 碰撞：

- **直接实体块查询**：`QueryBlocks`、`TestShape`、`CollectContacts`、`Raycast`（3D Amanatides & Woo DDA；网格实现 `VoxelBoundsGrid` 时自动包围盒裁剪）和 `Sweep`（轴对齐盒体采用解析闵可夫斯基和扫掠；球体、胶囊与带朝向盒体采用子步加二分 TOI 精化）按确定性的 `(y, z, x)` 顺序访问实体体素。
- **内部接缝过滤**：自动过滤被相邻实体方块遮挡的内部面，滑行的木箱、球体、胶囊与角色不会在平整体素地面或墙面的接缝处卡住。
- **编辑零重建与自动唤醒**：方块编辑立即生效无需重建。实现 `VoxelEditNotifier` 的网格会在实体性变化或清空时自动触发 `NotifyBlockChanged` / `NotifyReset`（通过 `World.WakeInBox` 传递唤醒附近及堆叠的休眠刚体）；普通 `VoxelGrid` 也会在下次 `World.Step` 时检测支撑消失并唤醒休眠刚体。

## glTF、GridMap 与高度图碰撞体

无需手动拷贝顶点或逐格创建包围盒，即可直接从 glTF 场景、`world3d` 网格地图和标准 Go 高度图图像构建静态关卡碰撞体：

- **glTF 三角网格**：`NewTriMeshFromGLTFPrimitive(prim, worldMat)`、`NewTriMeshFromGLTFMesh(mesh, worldMat)` 与 `NewTriMeshFromGLTFNode(doc, nodeIdx)` 从带索引或无索引的 `Triangles`、`TriangleStrip`、`TriangleFan` 图元构建带 BVH 加速的 `*TriMeshCollider`，自动应用节点完整世界变换并在负行列式镜像变换时翻转绕序以保持外法线朝向。
- **glTF 碰撞节点约定（`-colonly` 与 `extras.collider`）**：`World.AddGLTFColliders(doc, opts)` 与 `BuildGLTFColliders(doc, opts)` 识别 Godot 风格的节点名后缀（`-col`、`-colonly`、`-boxcol`、`-boxcolonly`、`-spherecol`、`-spherecolonly`、`-capsulecol`、`-capsulecolonly`、`-convcol`、`-convcolonly`，不区分大小写并兼容 Blender 的 `.001` 复制编号）以及 `node.Extras`（`{"collider": "box" | true | {"shape", "draw", "only", "trigger", "tag", "center", "halfExtents"|"size", "radius", "height", "friction", "restitution", "layer", "mask"}}`）。盒体与胶囊体完整保留节点旋转（定向盒体与旋转胶囊体）；在原生凸包形状加入前，`-convcol` / `-convcolonly` 会回退构建 `TriMeshCollider`。渲染器可通过 [`game/gltf`](3d-models-blender.md#碰撞节点-colonly-与-extrascollider) 的 `doc.DrawableNodes()` 或 `node.IsDrawable()` 跳过仅碰撞节点；卸载流式关卡时调用 `GLTFColliders.RemoveFromWorld(world)` 即可一键移除。
- **3D 贪心合并盒体与 `GridMap` 刚体**：`MergeBoxes(boxes, eps)` 按 X → Z → Y 顺序在容差 `eps`（默认 `1e-4`）内贪心合并相邻对齐的轴对齐盒体，输出完全等价的确定性合并结果。对 [`world3d.GridMap`](world3d.md#模块化-3d-gridmap) 调用 `grid.AddCollidersToWorld(world, opts)` 可提取所有格子的 `ColliderBoxes` 并自动合并相邻格子为更少的静态刚体（设置 `DisableMerge: true` 可按单格创建）。
- **从 `image.Image` 构建高度场**：`NewHeightfieldFromImage(img, HeightfieldImageOptions{...})` 支持任意 8 位/16 位灰度或彩色 `image.Image`（可选 `HeightmapLuminance`、`HeightmapRed`、`HeightmapGreen`、`HeightmapBlue`、`HeightmapAlpha` 通道），可配置 `CellSize` 或 `WorldWidth`/`WorldDepth`、`MinHeight`/`MaxHeight`、`Origin`、`FlipZ`，以及可选的 `(Cols, Rows)` 重采样（设置 `Bilinear: true` 启用双线性插值）。已有 `world3d.Heightmap` 时可直接调用 `hm.Collider()`。

```go
doc, err := gltf.LoadFS(assetsFS, "levels/courtyard.glb")
if err != nil {
	return err
}
levelCols, err := world.AddGLTFColliders(doc, physics3d.GLTFColliderOptions{})
if err != nil {
	return err
}
// 仅绘制视觉节点（自动跳过 -colonly / draw:false 碰撞节点）：
for _, nodeIdx := range doc.DrawableNodes() {
	drawGLTFNode(doc, nodeIdx)
}

hf, err := physics3d.NewHeightfieldFromImage(heightImg, physics3d.HeightfieldImageOptions{
	WorldWidth: 64,
	MinHeight:  -2,
	MaxHeight:  18,
})
if err == nil {
	world.AddHeightfield(hf)
}
_ = levelCols
```

## 重力缩放与区域体积

`Body` 和 `CharacterController` 均支持独立的 `GravityScale`（`NewDynamicBody` 与 `NewCharacterController` 默认为 `1`；调用 `SetGravityScale(0)` 可设为失重，负值可反转重力）。

任意触发器刚体（`Body.IsTrigger = true`）都可以配置 `Body.Area`（`AreaSettings`），在 `World.Step` 和 `CharacterController.Move` 中对重叠的动态刚体和角色覆盖重力、线性/角阻尼以及流体浮力与阻力：

- **重力模式与优先级**：`GravityMode` 支持 `AreaOverrideCombine`、`AreaOverrideReplace` 和 `AreaOverrideAdd`，按 `Priority` 降序解算（同优先级按刚体插入顺序确定性决胜）。支持定向 `Gravity`，也支持 `PointGravity` 向 `GravityPoint` 施加中心点引力，并配置 `GravityStrength`、`GravityFalloff`（`GravityFalloffNone`、`GravityFalloffLinear`、`GravityFalloffInverse`、`GravityFalloffInverseSquare`）与 `GravityUnitDistance`。
- **阻尼覆盖**：`LinearDampingMode` / `LinearDamping` 与 `AngularDampingMode` / `AngularDamping` 使用相同的优先级和合并/替换/叠加规则覆盖刚体自身阻尼。
- **浮力与流体阻力**：设置 `FluidDensity`（kg/m³）后，根据刚体包围盒的浸没比例施加阿基米德浮力，使隐含密度为 `Mass / Volume` 的刚体稳定悬浮在浸没比例 `(Mass / Volume) / FluidDensity` 处，并配合 `FluidLinearDrag`、`FluidAngularDrag` 以及可选的水流速度 `FluidVelocity`。
- **区域形状**：区域可以是任意形状。判断点是否在凸包或圆柱体区域内时按其精确形状而非包围盒，凸包和圆柱体的浮力体积也取其自身体积（`ConvexHull.Volume`，或 πr²h）。
- **查询与角色控制器**：可通过 `world.GravityAt(point)` 或 `world.AreaEffectAtShape(shape, layer, mask)` 直接查询解算后的区域效果。`CharacterController.Move` 自动应用重叠区域的重力（乘以 `CharacterController.GravityScale`）、流体阻力与线性阻尼，并通过 `CharacterController.EffectiveGravity(world)` 返回当前生效的重力向量。

```go
water := physics3d.NewStaticBody(
	physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 5, Z: 20}}),
	physics3d.Vec3{Y: -5},
)
water.IsTrigger = true
water.Area = physics3d.AreaSettings{
	FluidDensity:     1000,
	FluidLinearDrag:  5.0,
	FluidAngularDrag: 2.0,
}
world.AddBody(water)
```

## 关节与布娃娃

`World.AddJoint` 按确定的插入顺序求解 `DistanceJoint`、`BallJoint`、`SwingTwistJoint`、`HingeJoint`、`SliderJoint`、`FixedJoint` 和 `SixDOFJoint`：

- `HingeJoint.Angle` 会跨步累计，持续转动的铰链会报告超过 ±π 的角度，`MinAngle`/`MaxAngle` 也可以跨越一圈以上。除速度马达（`MotorTargetVel`、`MotorMaxTorque`）外，还支持可选的弹簧与位置驱动（`DriveTargetAngle`、`DriveStiffness`、`DriveDamping`、`DriveMaxTorque`），可将门或主动关节平滑拉回目标角度。
- `SliderJoint` 除限位与速度马达外，同样支持沿滑轨轴的弹簧与位置驱动（`DriveTargetPos`、`DriveStiffness`、`DriveDamping`、`DriveMaxForce`）。
- `DistanceJoint` 在距离位于 `[MinDistance, MaxDistance]` 之内时是松弛的；`Stiffness` 和 `Damping` 只让上下限变软，若要静止长度为 `L` 的弹簧，把两个上下限都设为 `L`。
- `SwingTwistJoint` 为球窝关节提供四象限非对称椭圆摆动锥（`SwingMin1`、`SwingMax1`、`SwingMin2`、`SwingMax2`）与独立的扭转范围（`MinTwist`、`MaxTwist`），并可通过 `SwingAngles()` 和 `TwistAngle()` 读取当前角度。`BuildRagdoll` 对非铰链关节（肩、髋、躯干、头部等）使用 `SwingTwistJoint`，对肘、膝及显式指定 `HingeAxis` 的骨骼使用 `HingeJoint`；`SkeletonJoint` 支持可选的非对称摆动限位以及 `TwistLimit` / `MinTwist` / `MaxTwist`（仅设置 `SwingLimit > 0` 时默认启用 ±π/4 扭转限制，防止四肢绕自身轴自由旋转）。
- `SixDOFJoint` 通过 `AxisConfig` 独立配置 3 个线性轴（`Linear[0..2]`）和 3 个旋转轴（`Angular[0..2]`）：支持 `Mode`（`AxisFree`、`AxisLocked`、带 `Min`/`Max` 的 `AxisLimited`）、弹簧刚度与阻尼（`Stiffness`/`Damping`），以及按 `MaxForce` 限幅的 `Drive`（`DriveNone`、`DrivePosition`、`DriveVelocity`，目标为 `TargetPos`/`TargetVel`）。
- 每个内置关节都可通过 `ReactionForce()`（牛顿）和 `ReactionTorque()`（牛·米）读取上一步施加在 `BodyB` 上的世界空间反作用力与反作用力矩，并支持可选的 `BreakForce` 和 `BreakTorque` 断裂阈值。当关节在 `World.Step` 中超过任一阈值时，会被标记为 `Broken()`，在位置修正前从世界中移除并唤醒动态刚体，同时按确定顺序通过 `World.JointBreakEvents()` 和 `World.OnJointBreak` 上报。

## 角色控制器

`CharacterController` 通过 `Move(world, wishMove, dt)` 在 `World` 中移动一个运动学胶囊（`Pos` 为脚底位置，尺寸为 `Radius` 和 `Height`）：

- 在平地上行走，并在不超过 `MaxSlopeAngle`（默认 45°）的斜坡上平滑上下滑行，下坡时保持贴地。
- 阻止攀爬超过 `MaxSlopeAngle` 的陡坡，并在重力作用下沿陡坡滑落。
- 在验证头顶净空、水平前进增益和可站立踏面后，自动迈上不超过 `StepHeight` 的台阶。
- 遇到竖直墙面时干净停止，斜向撞墙时沿墙面切向滑行而不穿透。
- 以 `PushForce`（牛顿）推动走进的动态刚体；默认值 0 时动态刚体像墙一样挡住角色。
- 在 `CharacterController` 和 `MoveResult` 中上报支撑刚体（`GroundBody`）、接触点处的地面速度（`GroundVelocity`）以及本步偏航角增量（`GroundYawDelta`）。
- 每步按运动学或动态地面刚体的精确线性与角运动携带角色——绕平台枢轴无漂移旋转胶囊位置，并在 `ApplyPlatformYaw` 为 `true`（`NewCharacterController` 默认开启）时将 `GroundYawDelta` 累加到 `Yaw`。
- 通过 `PlatformOnLeave` 配置起跳或走离移动平台时的速度继承策略：`PlatformLeaveAddVelocity`（默认，继承完整地面速度）、`PlatformLeaveAddUpwardVelocity`（继承水平与向上速度，忽略向下分量）或 `PlatformLeaveDoNothing`（离开时丢弃平台速度）。
- 通过沿分离法线推出角色来解算向胶囊移动的运动学刚体（滑动门、移动墙、升降梯）；当角色被夹在对向表面之间且无法有效分离时，在 `MoveResult.Crushed`（及 `CharacterController.Crushed`）中上报挤压状态。

```go
cc := physics3d.NewCharacterController(physics3d.Vec3{Y: 0}, 0.3, 1.8)
cc.PlatformOnLeave = physics3d.PlatformLeaveAddUpwardVelocity
loop := &game.Loop{
	Update: func(dt time.Duration) {
		res := cc.Move(world, physics3d.Vec3{X: 3.5}, dt)
		_, _, _ = res.Pos, res.GroundBody, res.Crushed
	},
}
```

角色控制器不是 `World` 中的刚体，因此动态刚体自身不会与它碰撞。需要碰撞时，添加一个跟随 `Pos` 的运动学刚体，并把 `IgnoreBodyID` 设为它的 ID。

## 调试绘制

`DrawWorld`、`DrawBody`、`DrawShape`、`DrawCharacter`、`DrawContact` 和 `DrawRaycast` 通过视图投影矩阵把碰撞体线框、接触和射线画到 `core.Drawing` 上，样式由 `DebugStyle` 决定；`DrawLine3D` 绘制单条线段。凸包以 `HullColor` 绘制每一条棱，圆柱体以 `CylinderColor` 绘制两端端盖的圆环和四条侧边线；`ConvexHullEdges` 和 `CylinderEdges` 返回这些线段，供其他渲染器使用。release 构建中它们编译为空操作。

线段在投影前先按相机视锥裁剪。默认的近平面是裁剪空间 `z >= -w`，与本包的 `Mat4Perspective` 和 `Mat4Ortho`（`[-1, 1]` 深度）一致。对于 `[0, 1]` 深度的投影，例如 `math3d.Perspective`、`math3d.Ortho` 或 `render3d` 的相机，请设置 `DebugStyle.DepthZO`（单条线段用 `DrawLine3DZO`）：它在 `z >= 0` 处裁剪，与 `render3d.ProjectSegment` 相同。不设置时，`w` 处处为 1 的 `[0, 1]` 正交相机会画出相机背后的碰撞体。

```go
style := physics3d.DefaultDebugStyle()
style.DepthZO = true // viewProj 来自 math3d.Perspective 或 math3d.Ortho
physics3d.DrawWorld(d, physics3d.Mat4(viewProj), viewport, world, style)
```

## 绳索、布料与软体（XPBD）

`SoftWorld` 使用纯 Go、确定性的扩展基于位置动力学（XPBD）求解器模拟 3D 绳索、布料网格与四面体软体：

- **粒子与约束**：`Particle`（`Pos`、`Vel`、`InvMass`、`Radius`、`Friction`、`Restitution`、`Pinned`、`Layer`/`Mask`）、双向与单向距离约束 `DistanceConstraint`（`AddDistance`、`AddTether`）、无奇异点的二面角弯曲约束 `BendingConstraint`（`AddBending`），以及四面体体积约束 `VolumeConstraint`（`AddVolume`）。
- **刚体双向附着**：`AttachBody` 在局部锚点处将粒子耦合到动态或运动学 `*Body`（`Compliance == 0` 为刚性挂点，`> 0` 为弹性连接），将线性与角向反作用冲量反馈回刚体并自动唤醒休眠刚体。
- **碰撞与层级过滤**：配置 `SoftWorld.World` 后，粒子可与非触发器 `Body`（带朝向盒体、球体、胶囊体）、`TriMeshCollider` 以及 `HeightfieldCollider` 碰撞，并遵循 `Layer`/`Mask` 过滤规则。
- **构建器与调试绘制**：`AddRope(RopeConfig)` 构建带单向长距离拉绳约束的 3D 不可拉伸绳索；`AddCloth(ClothConfig)` 构建含结构、剪切与二面角弯曲约束的三角化布料网格（`ClothGrid.Index(col, row)`）；`AddSoftBox(SoftBoxConfig)` 构建 8 顶点 5 四面体可变形立方体；`Edges()` 返回结构线框线段，`DrawSoftWorld(d, viewProj, viewport, sw, style)` 绘制调试覆盖层（`release` 构建下为空操作）。

```go
world := physics3d.NewWorld()
world.AddBody(physics3d.NewStaticBody(
	physics3d.SphereShape(physics3d.Sphere{Radius: 1.0}),
	physics3d.Vec3{Y: 1.0},
))

sw := physics3d.NewSoftWorld()
sw.World = world
sw.AddCloth(physics3d.ClothConfig{
	Origin:      physics3d.Vec3{X: -1.1, Y: 2.25, Z: -1.1},
	Width:       2.2,
	Height:      2.2,
	Cols:        10,
	Rows:        10,
	PinTopLeft:  true,
	PinTopRight: true,
})

// 在 game.Loop.Update 中：
world.Step(dt)
sw.Step(dt)
```
## 快照、回滚与跨架构确定性

`World` 支持完整的世界状态快照、确定性 64 位 FNV-1a 校验和，以及跨 amd64 与 arm64 CPU 架构的比特级确定性：

- **`Save() / Restore(snap) / Checksum()`**：`World.Save()` 将所有公开和私有求解器状态捕获到带版本号的 `WorldSnapshot`（`SnapshotVersion`）中，包括重力与求解器/休眠配置、刚体（含完整形状，包括 `ConvexHull` 的几何数据和 `Cylinder`，以及私有休眠计时器、重力缩放标记、残余穿透深度、上一帧插值姿态及体素静置状态）、有序热启动接触冲量缓存、活跃接触/触发器对集合、上一帧接触与关节断裂事件、忽略碰撞对、全部 7 种内置 `Joint3D` 类型（`DistanceJoint`、`BallJoint`、`HingeJoint`、`SliderJoint`、`FixedJoint`、`SwingTwistJoint`、`SixDOFJoint`，含展开角度状态与最近反作用冲量），以及 `TriMeshCollider`、`HeightfieldCollider` 和 `VoxelGrid` 碰撞体绑定。
- **纯文本 JSON 序列化**：`WorldSnapshot` 支持直接使用 `encoding/json` 序列化与反序列化（不使用 `encoding/gob` 或自定义二进制布局）。
- **原地保留指针与回调**：当数量和类型匹配时，`Restore` 原地更新现有的 `*Body`、`Joint3D` 和碰撞体实例，在回滚期间保持游戏实体指针、`UserData` 以及世界回调（`OnContact`、`OnTrigger`、`OnJointBreak`）有效。
- **跨架构确定性**：所有步进 3D 积分、窄相碰撞（OBB、网格/高度场、体素）、顺序冲量求解器、关节、区域以及 `vehicle` 悬架/轮胎路径都避免了 arm64 FMA 收缩及含 FMA 的标准库超越函数，确保在 amd64 与 arm64 上产生比特完全一致的轨迹和 `Checksum()` 值。

```go
snap := world.Save()
hash := world.Checksum()

// 序列化为纯文本 JSON（或存入 net.SimState 回滚缓冲）：
raw, _ := json.Marshal(snap)

var restored physics3d.WorldSnapshot
_ = json.Unmarshal(raw, &restored)
if err := world.Restore(restored); err != nil {
	return err
}
_ = hash
```

## 在场景文件中编写物理

3D 物理刚体、碰撞体与触发器可以直接在 `*.scene.yaml` 场景文件中使用 [`sceneformat`](editor.md#内置物理组件817) 内置的 `body3d`、`collider3d` 和 `trigger3d` 组件编写，并复用 [`game.yaml`](project.md) `physics:` 小节中定义的 3D 重力、求解器迭代次数和具名碰撞层。`sceneformat.BuildPhysics(built, gameProject.PhysicsSettings(), lib)` 会填充 `physics3d.World`（支持盒体、球体与胶囊体）、解析具名 `layer`/`mask` 列表、应用 `*.physics.yaml` 文件，并在运行时和编辑器运行模式中将刚体位姿同步回 `BuiltScene`。

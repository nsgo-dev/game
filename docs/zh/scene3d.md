# scene3d

`game/scene3d` 提供 3D 场景树（含脏标记传播）、命名插槽与类型化附件、用于大规模裁剪与查询的动态胖 AABB / SAH `BVH`、透视与正交相机及轨道/第一人称/第三人称控制器、屏幕空间 3D 拾取，以及面向移动端前向管线的盒投影 3D 贴花。

```go
import "github.com/nsgo-dev/game/pkg/scene3d"
```

所有变换、投影、视锥体与射线均遵循 [math3d](math3d.md) 约定（右手 Y 轴向上、列主序、`[0, 1]` NDC 深度）。

## 节点层级、插槽与附件

使用 `NewScene()` 与 `NewNode(name)` 构建场景树，确保默认变换、可见性、图层和包围盒正确初始化：

```go
sc := scene3d.NewScene()
hero := scene3d.NewNode("hero")
hero.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: -4})
hero.SetLocalBounds(math3d.AABB{Min: math3d.Vec3{X: -0.5, Y: 0, Z: -0.5}, Max: math3d.Vec3{X: 0.5, Y: 1.8, Z: 0.5}})

// 定义命名插槽并挂载武器节点：
hero.DefineSocket("hand_r", math3d.Transform{
	Pos:   math3d.Vec3{X: 0.4, Y: 1.1, Z: -0.2},
	Rot:   math3d.QuatIdentity(),
	Scale: math3d.Vec3One(),
})
sword := scene3d.NewNode("sword")
_ = hero.Attach("hand_r", sword)

// 在任意节点上存取类型化业务数据：
hero.SetAttachment("hp", 100)
hp, ok := scene3d.AttachmentAs[int](hero, "hp")
_ = sc.Add(hero)
_, _ = hp, ok
```

支持 `Reparent` / `ReparentKeepWorld`、`Detach`、`FindByName`、`FindByTag`、`Walk`、`WorldTransform()`、`WorldMatrix()`、`WorldBounds()` 与 `HierarchyBounds()`。调用 `sc.UpdateTransforms()` 仅更新脏子树，并把变化的节点加入内部 BVH 的待同步队列；每次查询都会先应用该队列，也可以调用 `sc.SyncBVH()` 立即应用。

`ReparentKeepWorld`（以及 `SetParent(parent, true)`）根据世界矩阵求出新的局部变换，节点的绘制位置完全不变。若该局部变换需要切变（例如父节点带旋转且非均匀缩放、子节点不带旋转），或新父节点的缩放有零分量，则返回 `ErrNotDecomposable`，节点在层级中保持原样。

## 动态胖 AABB / SAH `BVH` 与空间查询

`Scene` 内置动态胖 AABB 包围体层次树（`BVH`），可在万级乃至五万级节点场景中高效执行裁剪与查询：

```go
sc.UpdateTransforms()

// 视锥体裁剪（用 CullFrustumInto 复用切片可避免分配）：
visible = sc.CullFrustumInto(cam.Frustum(), visible[:0])

// 球体、盒体与射线查询：
nearby := sc.QuerySphere(math3d.Sphere{Center: heroPos, Radius: 12})
inBox := sc.QueryAABB(triggerBox)
hit, ok := sc.RaycastFirst(ray, scene3d.LayerAll)
_, _, _, _ = nearby, inBox, hit, ok
```

大批量加载关卡节点后调用 `sc.RebuildBVH()` 可按表面积启发式（SAH）重建平衡树。也可独立创建 `scene3d.NewBVH()`（`Insert`、`Update`、`Remove`、`Rebuild`、`QueryFrustum`、`QueryAABB`、`QuerySphere`、`Raycast`、`RaycastAll`）。

## 相机与控制器

通过 `NewPerspectiveCamera` / `NewOrthographicCamera` 创建相机（或用 `cam.SyncFromNode(node)` 跟随场景节点），并搭配三种内置控制器：

| 控制器 | 构造函数 | 核心方法 |
| --- | --- | --- |
| `OrbitController` | `NewOrbitController(target, dist)` | `Orbit`、`Zoom`、`Pan`、`SetTarget`、`SetAngles`、`Snap`、`Update(cam, dt)` |
| `FPSController` | `NewFPSController(pos)` | `Look(dYaw, dPitch)`、`Move(fwd, right, up, dt)`、`Apply(cam)`（`Grounded` 锁定在 XZ 平面移动） |
| `ThirdPersonController` | `NewThirdPersonController()` | `Snap`、`Update(cam, targetPos, targetForward, dt)`，支持防穿墙回调 `ObstructionCheck` |

```go
cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 3, Z: 8}, math3d.DegToRad(60), 16.0/9.0, 0.1, 300)
tp := scene3d.NewThirdPersonController()
tp.ObstructionCheck = scene3d.SceneObstructionCheck(sc, scene3d.LayerDefault, hero)

// 在 game.Loop.Update 中：
tp.Update(cam, heroPos, heroForward, dt)
```

### 投影、视口与插值

相机的 `Projection` 是透视投影（`FOVY`，弧度）或正交投影（`OrthoHeight`，以世界单位计的可见高度），另有 `Aspect`、`Near` 和 `Far`；`ViewMatrix`、`ProjMatrix` 与 `ViewProj` 给出裁剪深度为 [0, 1] 的矩阵，与 WebGPU、Metal 和 Direct3D 的约定一致。绘制目标尺寸变化时调用 `cam.SetViewport(width, height)`，让 `Aspect` 随之更新；尺寸不是有限正数（零、负数、NaN 或无穷大）时相机保持不变。`cam.Eye()` 返回齐次视点：透视相机为 `(Pos, 1)`，正交相机的视点在无穷远处，为 `(-Forward, 0)`，因此着色器在两种投影下都可以用 `normalize(eye.xyz - p * eye.w)` 求出指向观察者的方向。

在 `game.Loop.Update` 中推进相机，在 `Draw` 中绘制插值后的相机：保留上一次更新时的相机，绘制 `prev.Lerp(cam, alpha)`。`Lerp` 对 `Pos` 线性插值，对 `Rot` 做最短路径球面插值，对投影的各字段在 `Matrix` 实际使用的值之间线性插值，因此留为零的字段从其默认值开始插值，而不是从零开始；类型不同的投影不混合，`t` 到达 1 之前保持第一个。

```go
prev := *cam
loop := &game.Loop{
	Update: func(dt time.Duration) {
		prev = *cam
		orbit.Update(cam, dt)
	},
	Draw: func(alpha float64) {
		drawn := prev.Lerp(cam, float32(alpha))
		camBuf.WriteCamera(&drawn) // render3d.CameraBuffer
		// 录制并提交绘制命令
	},
}
```

## 3D 拾取

为可拾取节点挂载 `Collider`（`*MeshCollider` 或 `*SphereCollider`；未设置时回退到 `LocalBounds`），即可从屏幕坐标直接拾取。节点的世界包围盒同时覆盖 `LocalBounds` 与碰撞体。`MeshCollider` 会缓存自身包围盒，并能发现 `Triangles` 被替换，但节点的世界包围盒和 BVH 条目只有在节点被标记为脏时才会刷新：替换 `Triangles` 后调用 `node.MarkDirty()`（或用 `node.SetCollider` 重新设置碰撞体），原地修改顶点后则需同时调用 `collider.MarkDirty()` 和 `node.MarkDirty()`。可见性过滤在每次查询内做了记忆化，层级很深的命中不会各自遍历全部祖先。

```go
if hit, ok := scene3d.Pick(sc, cam, pointerX, pointerY, viewW, viewH, scene3d.LayerAll); ok {
	fmt.Printf("picked %s at %v (tri %d)\n", hit.Node.Name, hit.Point, hit.TriangleIndex)
}
```

需要按由近到远获取全部命中时使用 `PickAll`，底层基于 `cam.ScreenRay(screenX, screenY, viewW, viewH)`；将 3D 世界坐标投影回 2D 视口坐标请使用 `cam.WorldToScreen`。

## 盒投影 3D 贴花

使用 `Decal`、`DecalPool`、`ProjectDecal` 与 `GPUDecalRenderer` 在场景几何体表面投射弹痕、焦痕、圆形脚底阴影（blob shadow）与溅射贴花。贴花沿自身局部 `-Z` 方向投射到朝向 `+Z` 的表面，借助 BVH 盒体重叠查询与 6 平面 Sutherland–Hodgman 多边形裁剪直接生成前向渲染三角形网格——无需延迟渲染 G-Buffer、计算着色器或深度缓冲回读，因此在 WebGPU、Metal、Vulkan、OpenGL ES 3.0 和 WebGL 2 上行为完全一致。

- **法线约束与角度淡出**：法线夹角在 `AngleFadeStart` 以内的表面保持完整不透明度；介于 `AngleFadeStart` 与 `AngleFadeEnd` 之间按表面法线平滑淡出；垂直或背面三角形会被直接剔除，防止贴花穿透到侧壁或墙背。`DepthFade` 用于在投影盒前后两端平滑衰减。
- **图层掩码过滤**：`Decal.LayerMask` 按位匹配节点的 `Node.Layer`（`LayerAll` 匹配全部非零图层，`0` 排除全部对象），并自动跳过隐藏或禁用子树。挂载 `*MeshCollider` 或 `DecalReceiver`（如 `node.SetDecalReceiver(&scene3d.DecalReceiverMesh{...})`）的节点均可接收贴花。
- **定容对象池**：`NewDecalPool(maxDecals)` 严格限制存活贴花数量，满容量时按确定性规则淘汰（优先淘汰 `SortOrder` 最低者，同优先级淘汰最早生成者）；`Step(dt)` 推进年龄、计算 `FadeIn`/`FadeOut` 并自动清理过期贴花，稳态下的 `Spawn`、`Step` 与 `BuildGeometry` 复用内部缓存，实现零堆分配。

```go
pool := scene3d.NewDecalPool(64)

// 从射线命中点放置红色弹痕，仅投射到默认世界图层，排除角色图层：
mark := scene3d.NewDecalOnSurface(hit.Point, hit.Normal, math3d.Vec3Up(), math3d.Vec3{X: 0.35, Y: 0.35, Z: 0.25})
mark.Albedo = core.RGB(0xdd2222)
mark.LayerMask = scene3d.LayerDefault
mark.Lifetime = 10 * time.Second
mark.FadeOut = 2 * time.Second
pool.Spawn(mark)

// 每帧更新并重建贴花几何体：
pool.Step(dt)
var geom scene3d.DecalGeometry
pool.BuildGeometry(sc, &geom)
_ = decalRenderer.Upload(&geom)
// 在前向渲染通道绘制完不透明场景几何体后叠加贴花：
decalRenderer.DrawWithCamera(pass, camBuf.BindGroup())
```

在 `gpu.View` 的 `OnReady` 中通过 `NewGPUDecalRenderer(cx, dev, opts)` 创建一次渲染器即可。传入 `render3d.CameraBuffer.Layout()` 作为 `CameraLayout` 可直接共享帧相机绑定组（`DrawWithCamera`），留空则使用内部相机缓冲（`WriteCamera` + `Draw`）。通过 `SetTexture` 或 `UploadTextureRGBA` 按 `Decal.TextureID` 绑定贴图；相邻使用同一贴图的批次会自动合并为单次 `DrawIndexed` 调用。

## 各目标平台场景光源上限

通过 [`scenemodel.Renderer`](scenemodel.md#各目标平台光源上限) 构建并渲染 3D 场景文件时，可见方向光（最多 4 个）与首个投射阴影的点光和聚光保留在 `render3d.LightRig` uniform 块中，其余所有可见点光与聚光均经由 `render3d.ClusteredLightSystem` 着色：

- **支持存储缓冲的目标**（macOS/iOS Metal、WebGPU、Vulkan、Direct3D 11 与无头软件测试）：每帧最多 **512** 个可见无阴影局部光源（点光 + 聚光，每个视锥体集群单元最多 `64` 个），另加 `4` 个方向光与 `1` 个阴影点光 + `1` 个阴影聚光。
- **无存储缓冲的回退目标**（WebGL 2、OpenGL ES 3.0、Linux GtkGLArea、`nsgotest.NoGPUCompute`）：每帧最多 **16** 个优先可见无阴影局部光源（经视锥体裁剪后按光功率、作用半径与相机距离排序），另加 `4` 个方向光与 `1` 个阴影点光 + `1` 个阴影聚光。
- **非集群 `render3d.LightRig`**（`Built.LightRig()` / `Built.LightRigNear`）：最多 `4` 个方向光、`16` 个点光与 `8` 个聚光；超出时保留 0 号阴影光源并优先选取距相机最近的光源。

运行 `game check` 会在场景无阴影局部光源数量超过 16 光源回退上限时报告文件、行号与被舍弃的光源数。

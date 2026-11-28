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

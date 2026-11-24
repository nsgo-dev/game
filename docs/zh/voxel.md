# voxel

`game/voxel` 为沙盒游戏提供分块 3D 体素世界：致密立方体区块、直接输出到 [render3d](render3d.md) 缓冲的贪婪网格合并（greedy meshing）、跨区块邻接面剔除与环境光遮蔽（AO）、编辑时的最小脏区块追踪、3D DDA 体素射线拾取，以及确定性的带版本号 YAML 存档格式。

```go
import "github.com/nsgo-dev/game/packages/game/voxel"
```

## 方块定义与注册表

在 `Registry` 中注册方块类型，配置透明性、实体性与用于子网格分组的 `MaterialID`。方块 ID `0`（`voxel.Air`）保留给空气；未注册的非零 ID 默认按不透明、实体、`MaterialID: 0` 处理：

```go
reg := voxel.NewRegistry()
stone := reg.MustRegister(voxel.BlockDef{Name: "stone", MaterialID: 0})
grass := reg.MustRegister(voxel.BlockDef{Name: "grass", MaterialID: 1})
glass := reg.MustRegister(voxel.BlockDef{Name: "glass", Transparent: true, MaterialID: 2})
water := reg.MustRegister(voxel.BlockDef{Name: "water", Transparent: true, NonSolid: true, MaterialID: 3})
```

面的可见性剔除遵循标准的不透明/透明规则：
- 不透明方块朝向 `Air` 或透明方块时生成面。
- 透明方块朝向 `Air` 或*不同 ID* 的透明方块时生成面；紧贴不透明方块或相同 ID 透明方块的面会被剔除。

## 分块世界与脏区块追踪

使用 2 的幂次边长 `ChunkSize`（`4` 到 `64`，默认 `16`，即 16×16×16 区块）和世界空间 `VoxelSize`（默认 `1`）创建 `World`：

```go
w, err := voxel.NewWorld(voxel.WorldConfig{
	ChunkSize: 16,
	VoxelSize: 1,
	Registry:  reg,
})
if err != nil {
	return err
}

w.FillBox(voxel.BlockCoord{X: 0, Y: 0, Z: 0}, voxel.BlockCoord{X: 31, Y: 3, Z: 31}, stone)
w.SetBlock(voxel.BlockCoord{X: 8, Y: 4, Z: 8}, grass)
```

`SetBlock` 只会标记被编辑方块所在的区块为脏。仅当编辑位于区块边界且改变了相邻区块的面可见性或跨区块环境光遮蔽（例如把紧贴邻区块的不透明方块挖成空气）时，才会顺带标记已存在的相邻区块。将一个不透明方块替换为另一个不透明方块不会弄脏相邻区块。

重建网格后，可调用 `w.PruneEmptyChunks()` 释放已经完全变回 `Air` 的空区块。

## 贪婪网格合并

复用一个 `Mesher` 实例可避免每帧重建时的临时缓冲分配。`RemeshDirty` 按确定性的 `CompareChunkCoords` 顺序只重建脏区块，并清除其脏标记：

```go
mesher := voxel.NewMesher()
opts := voxel.MeshOptions{
	AmbientOcclusion: true, // 在 ChunkMesh.AO 中输出逐顶点 AO 并自动翻转对角线
	UVMode:           voxel.UVWorld,
}

for _, cm := range mesher.RemeshDirty(w, opts) {
	if cm.Empty() {
		releaseChunkGPU(cm.Coord)
		continue
	}
	// cm.Mesh 是逆时针正面的 render3d.Mesh，可直接传给 render3d.UploadMesh。
	// cm.SubMeshes 按 BlockDef.MaterialID 划分 cm.Mesh.Indices 区间。
	uploadChunkGPU(cm.Coord, cm.Origin, cm.Mesh, cm.SubMeshes)
}
```

`MeshOptions` 的主要选项：
- `AmbientOcclusion`：跨区块计算逐顶点 AO 等级（`0..3`，存入 `ChunkMesh.AO`，可用 `AOFactor` 换算为亮度系数），自动翻转四边形对角线避免插值接缝，且仅在四个角 AO 全部一致时合并面。
- `WorldSpace`：顶点坐标加上 `ChunkMesh.Origin` 偏移，输出世界空间位置；默认 `false` 输出区块局部坐标 `[0, ChunkSize*VoxelSize]`。
- `UVMode`：默认 `UVWorld` 按合并后四边形的宽高（体素单位）缩放 UV 以无缝平铺纹理；`UVUnit` 为每个四边形分配 `[0, 1]`。
- `DisableGreedy` / `IgnoreNeighbors`：关闭贪婪合并或跨区块面剔除，便于调试与性能对比。

## 体素射线拾取与编辑

使用 `World.Raycast`（3D Amanatides & Woo DDA 遍历）拾取方块并在命中面外侧放置新方块：

```go
if hit, ok := w.Raycast(ray, 64); ok {
	// 破坏命中的方块：
	// w.SetBlock(hit.Block, voxel.Air)

	// 或在命中面外侧放置新方块：
	w.SetBlock(hit.Adjacent, stone)
}
```

`Raycast` 默认跳过 `NonSolid` 方块（如水）；需要命中任意非空气体素时，向 `RaycastWith` 传入 `RaycastOptions{IncludeNonSolid: true}`。

## YAML 保存与加载

世界可序列化为确定性、带版本号的 YAML 文本：调色板按 ID 排序、非空区块按坐标排序，区块体素按 `(y, z, x)` 顺序游程编码（`"id*count"` 或 `"id"`）。未知 YAML 字段与尾随文档都会被拒绝，错误以 `*loaderr.Error` 返回并附带文件、行列号、字段路径与修复建议：

```go
data, err := voxel.SaveYAML(w, "Overworld")
if err != nil {
	return err
}

loaded, name, err := voxel.LoadYAML(data)
if err != nil {
	return err
}
_ = name
// 加载出的每个区块初始都标记为脏，可直接用 RemeshDirty 构建初始网格：
initialMeshes := mesher.RemeshDirty(loaded, opts)
_ = initialMeshes
```

保存出的文件以 `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/voxel.v1.json` 开头；参见 [YAML 文件的 Schema](schemas.md)。

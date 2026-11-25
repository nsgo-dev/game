# voxel

`game/voxel` 为沙盒游戏提供分块 3D 体素世界：致密立方体区块、跨区块逐体素天空光与方块光传播（`0..15`）、直接输出到 [render3d](render3d.md) 缓冲的贪婪网格合并（greedy meshing）、跨区块邻接面剔除与环境光遮蔽（AO）、编辑时的最小脏区块追踪、3D DDA 体素射线拾取，以及确定性的带版本号 YAML 存档格式。

```go
import "github.com/nsgo-dev/game/packages/game/voxel"
```

## 方块定义与注册表

在 `Registry` 中注册方块类型，配置透明性、实体性、用于子网格分组的 `MaterialID`、方块自发光等级 `Emissive`（`0..15`）以及透明方块的透光衰减 `LightOpacity`（`0..15`）。方块 ID `0`（`voxel.Air`）保留给空气；未注册的非零 ID 默认按不透明（有效透光衰减 `15`）、实体、不发光、`MaterialID: 0` 处理：

```go
reg := voxel.NewRegistry()
stone := reg.MustRegister(voxel.BlockDef{Name: "stone", MaterialID: 0})
grass := reg.MustRegister(voxel.BlockDef{Name: "grass", MaterialID: 1})
glass := reg.MustRegister(voxel.BlockDef{Name: "glass", Transparent: true, MaterialID: 2})
water := reg.MustRegister(voxel.BlockDef{Name: "water", Transparent: true, NonSolid: true, LightOpacity: 2, MaterialID: 3})
torch := reg.MustRegister(voxel.BlockDef{Name: "torch", Transparent: true, NonSolid: true, Emissive: 14, MaterialID: 4})
```

面的可见性剔除遵循标准的不透明/透明规则：
- 不透明方块朝向 `Air` 或透明方块时生成面。
- 透明方块朝向 `Air` 或*不同 ID* 的透明方块时生成面；紧贴不透明方块或相同 ID 透明方块的面会被剔除。

## 天空光与方块光传播

每个体素存储两个 4 位光照通道（各 `0..15`，打包为每体素 1 字节）：

- **天空光（SkyLight）**：上方无遮挡的体素列直接以等级 `15` 垂直向下穿透 `Air` 与 `LightOpacity: 0` 的透明方块（如玻璃）。遇到阻光方块（`LightOpacity > 0` 或有效衰减为 `15` 的不透明方块）后下方进入阴影区，并从更亮邻格按每步 `max(1, LightOpacity)` 向水平与垂直方向漫射。
- **方块光（BlockLight）**：`Emissive > 0` 的方块（如火把、萤石）以自身发光等级为源，跨区块边界洪水填充传播，每经过一格衰减 `max(1, LightOpacity)`。

`World.SetBlock` 通过跨区块的增量 BFS 扩散与移除同步更新两个通道——结果与 `World.RecomputeLight()` 完全一致，且仅将几何或边界光照确实发生变化的已有区块标脏。可通过 `w.SkyLight(pos)`、`w.BlockLight(pos)`、`w.Light(pos)` 查询任意世界坐标的光照，或通过 `ch.SkyLight`、`ch.BlockLight`、`ch.Light` 查询区块局部光照。

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

`SetBlock` 会标记被编辑方块所在的区块为脏；仅当编辑改变了相邻区块的面可见性、跨区块环境光遮蔽或跨区块天空光/方块光时，才会顺带标记已存在的相邻区块。将一个不透明不发光方块替换为另一个不透明不发光方块不会弄脏相邻区块。

重建网格后，可调用 `w.PruneEmptyChunks()` 释放已经完全变回 `Air` 的空区块（穿过已释放区块的非默认光照会被自动保留）。

## 贪婪网格合并

复用一个 `Mesher` 实例可避免每帧重建时的临时缓冲分配。`RemeshDirty` 按确定性的 `CompareChunkCoords` 顺序只重建脏区块，并清除其脏标记：

```go
mesher := voxel.NewMesher()
opts := voxel.MeshOptions{
	AmbientOcclusion: true, // 在 ChunkMesh.AO 中输出逐顶点 AO 并自动翻转对角线
	Lighting:         true, // 输出逐顶点 SkyLight、BlockLight 与线性 RGBA Colors
	SmoothLighting:   true, // 跨区块边界的 4 体素平滑角点插值
	UVMode:           voxel.UVWorld,
}

for _, cm := range mesher.RemeshDirty(w, opts) {
	if cm.Empty() {
		releaseChunkGPU(cm.Coord)
		continue
	}
	// cm.Mesh 是逆时针正面的 render3d.Mesh，可直接传给 render3d.UploadMesh。
	// cm.ColoredMesh() 返回携带线性 RGBA 光照+AO 顶点色的 render3d.ColoredMesh，
	// 可直接传给 render3d.UploadColoredMesh 与支持顶点色的 render3d 管线。
	// cm.SubMeshes 按 BlockDef.MaterialID 划分 cm.Mesh.Indices 区间。
	uploadChunkGPU(cm.Coord, cm.Origin, cm.ColoredMesh(), cm.SubMeshes)
}
```

`MeshOptions` 的主要选项：
- `AmbientOcclusion`：跨区块计算逐顶点 AO 等级（`0..3`，存入 `ChunkMesh.AO`，可用 `AOFactor` 换算为亮度系数），自动翻转四边形对角线避免插值接缝，且仅在四个角 AO 全部一致时合并面。
- `Lighting` / `SmoothLighting` / `LightStyle`：跨区块采样逐顶点 `ChunkMesh.SkyLight`（`0..15`）、`ChunkMesh.BlockLight`（`0..15`）以及线性 RGBA `ChunkMesh.Colors`（结合 `LightFactor`、`LightStyle` 与启用时的 `AOFactor`）。贪婪合并仅在光照一致时合并面，避免跨区块接缝；调用 `cm.ColoredMesh()` 即可直接交给 `render3d.UploadColoredMesh`（兼容 `render3d.NewUnlitPipeline` 及启用 `VertexColors: true` 的 `render3d.NewMaterialPipeline`）。
- `WorldSpace`：顶点坐标加上 `ChunkMesh.Origin` 偏移，输出世界空间位置；默认 `false` 输出区块局部坐标 `[0, ChunkSize*VoxelSize]`。
- `UVMode`：默认 `UVWorld` 按合并后四边形的宽高（体素单位）缩放 UV 以无缝平铺纹理；`UVUnit` 为每个四边形分配 `[0, 1]`。
- `DisableGreedy` / `IgnoreNeighbors`：关闭贪婪合并或跨区块面剔除与光照采样，便于调试与性能对比。

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

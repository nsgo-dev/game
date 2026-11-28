# gametest

`game/gametest` 在无窗口、无 GPU 的环境中将录制的 `core.DrawOp` 列表软件光栅化为图像，与黄金 PNG 参考图逐像素比对、生成可视化差异图，并支持提取绘制出的图像矩形做结构化断言。

```go
import "github.com/nsgo-dev/game/pkg/gametest"
```

## 软件渲染与黄金图比对

`RenderOps(width, height, ops, opts...)` 会按确定性规则回放录制的 `core.DrawOp`——包括变换、透明度、填充、描边、圆角矩形、圆弧、椭圆、虚线、多边形路径、点阵文字、清除、贴图与 `DrawAtlas`——输出预乘的 `*image.RGBA`。

`AssertGolden(t, name, got)` 将 `got` 与 `testdata/golden/<name>.png` 比对（`AssertOpsGolden` 将 `RenderOps` 与 `AssertGolden` 合二为一）。`AssertGoldenAt(t, goldenPath, got)` 支持任意黄金图路径，`CompareGolden(got, goldenPath, update)` 则直接返回差异图和失配像素数。

在逐像素比对之前，`CompareGolden` 会先将 `got` 经由 PNG 编解码往返一次，使比对在磁盘文件所存储的非预乘色彩空间中进行。因此，带透明背景或半透明像素的预乘 `*image.RGBA` 画面在写入黄金图后立即比对时不会产生误报失配。

### 更新黄金图

传入 `-update-golden` 或设置环境变量 `UPDATE_GOLDEN=1`（或 `true` / `yes`）即可生成或覆写黄金 PNG：

```sh
go tool nsgo test ./pkg/gametest -update-golden
UPDATE_GOLDEN=1 go tool nsgo test ./pkg/gametest
```

## 失败图像的输出位置

当 `AssertGolden` 或 `AssertGoldenAt` 检测到像素失配时，会写出两张在测试结束后仍然保留、便于排查的 PNG——实际输出图（`<name>.got.png`）与可视化差异图（`<name>.diff.png`，一致像素以变暗的灰度显示，差异像素以纯品红 `#ff00ff` 高亮）——并在失败信息中打印两者的路径：

- **默认位置**：写在黄金图同目录下，即 `<dir>/<name>.got.png` 与 `<dir>/<name>.diff.png`（例如 `AssertGolden(t, "scene", img)` 会写出 `testdata/golden/scene.got.png` 与 `testdata/golden/scene.diff.png`）。`*.got.png` 与 `*.diff.png` 均已加入 `.gitignore`。
- **自定义或 CI 目录**：设置环境变量 `GOLDEN_DIFF_DIR` 后，失败图像会改为写入该目录，便于 CI 统一归档上传。若黄金图位于 `testdata/golden/` 的子目录中，`GOLDEN_DIFF_DIR` 下会保留对应的相对子目录结构以避免重名覆盖。

当后续测试通过或执行 `-update-golden` 更新后，`AssertGoldenAt` 会自动清理对应路径下残留的 `<name>.got.png` 与 `<name>.diff.png`。

## 比较图像绘制

若测试只需验证精灵落点与属性而非逐像素比对，`ImageDraws(ops)` 会回放变换、透明度、混合模式与裁剪路径，返回每一条绘制出的图像矩形——无论它们是逐个 `DrawImageFrom` 录制，还是合批在 `DrawAtlas` 中。`AssertSameImages(t, got, want)` 在 `1e-3` 浮点容差内断言两份绘制操作序列输出相同的图像绘制列表。
# 光敏检查

在记录无障碍场景时使用闪光限制器测试作为回归保护。录制运行应在启用玩家“减少闪光”设置时覆盖重复的全屏闪光。

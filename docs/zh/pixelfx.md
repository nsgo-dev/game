# pixelfx

`game/pixelfx` 是 [pixelfx](https://github.com/patch-gg/pixelfx) 的 Go 移植。pixelfx 是一个 MIT 许可的像素风特效与动画库，每个特效都是一段小的程序化代码（火焰、爆炸、魔法、天气、界面、拾取物、转场、小生物，以及带有蓄力、命中停顿和收招的角色招式），接受调色板、种子和摆放选项，在一帧里画出后层、前层和叠加光。基于 NSGo 的游戏可以在运行时播放、换色、镜像这些特效，也可以把它们导出为精灵表，全程不需要 JavaScript。

```go
import (
	"github.com/nsgo-dev/game/packages/game/pixelfx"
	_ "github.com/nsgo-dev/game/packages/game/pixelfx/fire" // 注册 fire/torch 及该类别的其余特效
)
```

移植跟随 pixelfx 的提交 `5294518ce7c72e48b6100a744596e0acf57f2826`，用 float64、按相同顺序做相同的运算，因此画出相同的像素：每个类别的测试都会拿它的特效与 pixelfx 自己渲染的帧比较（见[与 pixelfx 一致](#与-pixelfx-一致)）。

## 包

| 包 | 内容 |
| --- | --- |
| `game/pixelfx` | 核心：帧与合成、画笔、随机数与噪声、粒子、模拟、颜色与调色板、两种像素字体、表演系统（骨架、姿势、表演、串联）、特效注册表、精灵表导出和视图辅助 |
| `game/pixelfx/fire` | 火焰：目前有火把 |
| `game/pixelfx/explosions` | 爆炸：目前有小爆炸 |
| `game/pixelfx/impacts` | 冲击：目前有砸地 |
| `game/pixelfx/moves` | 由角色表演的招式：目前有落地重击 |
| `game/pixelfx/nature` | 自然：酸液飞溅、巨石、地刺、治愈花开、落叶风暴、毒云、地震、根须缠绕、孢子爆裂、荆棘、龙卷风、剧毒泥潭与藤鞭 |
| `game/pixelfx/pickups` | 拾取物：开宝箱、旋转金币、收集爆发、水晶碎片、旋转宝石、红心拾取、钥匙闪光、战利品光柱、药水、能量星和经验球 |
| `game/pixelfx/projectiles` | 投射物（13 个特效）：`arrow`、`bullet-tracer`、`cannonball`、`energy-disc`、`flare`、`homing-missile`、`laser-bolt`、`magic-missile`、`muzzle-pistol`、`muzzle-shotgun`、`muzzle-smg`、`plasma-shot`、`rocket` |
| `game/pixelfx/scifi` | 科幻：`emp-blast`、`energy-core`、`force-field`、`glitch`、`hologram`、`jetpack`、`laser-grid`、`scanner`、`teleporter-pad`、`tractor-beam`、`warp-in`、`warp-out` |
| `game/pixelfx/shadow` | 暗影：黑洞、诅咒、暗黑爆炸、日蚀、幽暗之手、六芒法阵、生命窃取、亡灵骷髅、暗影之火、暗影触须、灵魂吸取、虚空法球 |
| `game/pixelfx/spawn` | 出生与死亡：金币掉落、分解消散、灼烧溶解、破壳孵化、噗烟消散、重生光束、冰碎死亡、灵魂升空、召唤传送门和召唤升起 |
| `game/pixelfx/status` | 角色身上的状态特效：流血、燃烧、魅惑、混乱、诅咒、冰冻、加速、石化、中毒、狂暴、睡眠、减速和眩晕 |

特效按类别各放在一个子包里，子包被导入时把它的特效注册为 `"<类别>/<名字>"`，所以游戏只链接它导入的类别。pixelfx 的其余类别（water、ice、lightning、melee、arcane、holy、weather、ui、transitions、critters）各自在一次改动中跟进，做法见[移植一个类别](#移植一个类别)。

## 播放特效

`Effect` 持有它的 `Meta`（名字、描述、标签、帧尺寸、其局部 (0, 0) 所在的原点、时长、循环或一次性、帧率、预览背景、配套的角色，以及各选项和默认值）和用来启动它的 `New`。`NewPlayer` 用 `Options` 启动一个特效：`X` 和 `Y` 决定原点在帧中的位置，`Flip` 镜像，`Angle` 旋转，`Light` 缩放光晕强度，`Values` 按名字覆盖特效自己的选项默认值，例如 `seed` 和 `palette`。长度与默认值不同的调色板会被重采样到默认长度，因此任何色阶都能给任何特效换色。`Set` 本身不做检查：选项不接受的类型的值（数字可以代替布尔值，单个颜色可以代替只含这一种颜色的调色板）以及空调色板都会被忽略、保留默认值，所以要报告错误，请先用 `Meta.Default` 比对。

```go
e, _ := pixelfx.Lookup("fire/torch")
p := pixelfx.NewPlayer(e, pixelfx.Options{X: 36, Y: 94}.
	Set("palette", pixelfx.RampOf(pixelfx.Ramps["ice"])).
	Set("seed", pixelfx.Num(9)))
frame := pixelfx.NewFrame(e.Meta.Size[0], e.Meta.Size[1], 0, 0)

// 游戏的每一帧：
p.Update(dt)
frame.Clear()
p.Draw(frame, pixelfx.LayerBoth)
```

无论游戏帧率多少，`Update` 都以 120 Hz 或更细的步长推进模拟，所以特效在任何帧率下看起来都一样。`Draw` 传 `LayerBack` 画角色身后的部分，传 `LayerFront` 画角色身前的部分，角色可以站在两者之间。`Stop` 让循环特效在 0.3 秒内淡出，让一次性特效提前结束；`Done` 报告是否已经结束。

一帧包含不透明颜色（`Color`）和叠加光（`Light`）。`Composite` 把两者合成为 RGBA 字节，背景可以是透明、纯色或一张背景图：在透明背景上，光会变成带透明度的颜色，叠在深色背景上看起来就是叠加光。

## 在 NSGo 中显示帧

`Image` 把帧变成 NSGo 的图片源：`Set` 把一帧在透明背景上合成为预乘的像素并递增图片源的版本号，于是同一个 `core.Image`、画布或着色器会原地显示每一个新帧。`View` 是一个画布，放大绘制图片源并关闭平滑，正是像素画需要的；任何倍数都可以，整数倍能让每个像素一样大：

```go
var img pixelfx.Image
src := core.NewSignalFunc(cx, core.ImageSource{}, nil)

// 在游戏的帧回调里，画完这一帧之后：
src.Set(img.Set(frame))

// 在界面里：
pixelfx.View{Source: src.Get, Scale: 4}
```

`DrawScaled` 以同样的方式在你自己的画布里画一个图片源。

## 精灵表

`Render` 按 pixelfx 渲染精灵表的方式渲染特效的每一帧：使用特效自己的帧率（或给定的帧率），共 round(时长 × 帧率) 帧，一次性特效多一帧以便最后一帧就是结尾；除非特效铺满整个屏幕，否则每条边最外 8 个像素内的光会渐隐。`Frames.Sheet` 把这些帧排成网格，并写出 `sprite.LoadAtlas` 能读取的 Aseprite JSON（json-array）：每帧的矩形、时长（1000 / 帧率 毫秒，取整）和位于特效原点的轴心，一个帧标签（循环特效循环播放，一次性特效只播一次），以及 pixelfx 自己的元数据（`id`、`fps`、`loop`、`frameSize`、`pivot`、`columns`，有表演时还有其时间信息）。精灵表与 PNG 一样存储非预乘的透明度。

```sh
go tool game pixelfx -out assets/fx fire/torch
go tool game pixelfx -set seed=4 -set palette=#102,#215,#43a,#8cf,#fff -flip -out assets/fx -name frost-torch fire/torch
go tool game pixelfx -act -out assets/fx moves/ground-pound
go tool game pixelfx -list
```

`game pixelfx` 把 `<name>.png` 和 `<name>.json` 写入 `-out`（名字默认取特效名，例如 `torch`，可用 `-name` 指定）。`-set name=value` 设置特效的一个选项：数字、`true` 或 `false`、颜色（如 `#4a3f69`）、用逗号分隔的一串颜色，或文本；`-list` 列出所有特效及其选项和默认值。`-act` 渲染由内置角色表演、带有靶子的版本，除非用 `-fps` 指定，否则帧率为 60。

```go
atlas, err := sprite.LoadAtlas(core.ImageSource{URI: "asset:fx/torch.png"}, torchJSON)
anim := atlas.Animations["torch"] // 循环播放，30 fps，轴心在火把的原点
```

## 表演

由角色表演的特效带有一个 `Act`：随时间变化的姿势（`Key`，每个关键帧可以平滑到达，也可以用 `"out"`、`"in"`、`"line"` 或 `"snap"` 到达）、让画面停顿、震动并推动稻草靶的命中、扬起的尘土，以及靶子站的位置。`Perform` 用内置角色播放它，角色由一副 3D 骨架（`Rig`、`Pose`）画成像素：姿势只说明手脚放在哪里、身体如何发力，求解器算出每个关节。四肢从每个关键帧起按 12 fps 的步长移动，看起来像一张张定格的手绘帧，脚在地面上的位置则平滑移动。

```go
rig := pixelfx.NewRig()
w, h, x, y := pixelfx.ActFrameSize(moves.GroundPound, rig)
frame := pixelfx.NewFrame(w, h, 0, 0)
pf := pixelfx.Perform(moves.GroundPound, pixelfx.Options{X: x, Y: y}, rig)
for !pf.Done() {
	pf.Update(1.0 / 60) // 命中停顿时保持不动
	frame.Clear()
	pf.Draw(frame, true, true)
	sx, sy := pf.Shake() // 画面要震动多远
	_, _ = sx, sy
}
```

有自己角色的游戏可以用 `ActTiming` 以纯数据的形式读取同样的时间信息：特效开始前的蓄力、结束后的收招、每次命中的停顿、震动和推力，以及各个姿势。`Rig` 像 pixelfx 一样缓存它画过的姿势，以每个值取整到像素后的结果为键，所以要把同一个 `Rig` 交给 `ActBounds` 和 `Perform`，`ActFrameSize` 和精灵表导出就是这样做的。

`Chain` 把几个特效合成一个，每个特效在各自的时间和位置开始，x 和 y 可以是数字，也可以是招式时间的函数：投射物就是一个 x 会移动的特效。moves 类别就是由其他类别的特效串联而成、再加上一段表演。

## 确定性

用相同选项、相同的更新播放同一个特效，每次运行都画出相同的帧：它的随机性来自 `Rng`（pixelfx 的 mulberry32，相同种子给出相同序列）和 `Hash`。每个类别的测试都会把每个特效渲染两次并逐字节比较。JavaScript 的 32 位转换（`x | 0`、`x >>> 0`、`Math.round`、`Math.hypot`）由 `ToInt32`、`ToUint32`、`Round` 和 `Hypot` 重现。

不同机器之间，帧可能在某个通道上相差一级：Go 在 arm64 上可能把一次乘法和一次加法融合成一条指令，amd64 上不会；Go 的 `math.Sin`、`math.Cos`、`math.Atan2`、`math.Exp` 和 `math.Pow` 也可能在最后一位上与 V8 不同。

## 与 pixelfx 一致

`tools/pixelfxref` 在固定的提交上用 pixelfx 本身把特效渲染成参考 PNG，`pixelfx/internal/pixelfxtest` 用移植版渲染同样的特效并与之比较。两边都为每个特效渲染三种情况：

- `default`：使用默认选项的特效，按精灵表的方式渲染，取播放到 25%、50% 和 75% 处的帧；
- `flip`：同上，但镜像；
- `act`，仅限由角色表演的特效：以 60 fps 渲染的表演，取时间首次到达从第一个姿势到结尾（循环特效为两个循环）的 25%、50% 和 75% 处的帧。

参考图为特效所在类别包的 `testdata/ref/<name>` 下的 `<case>-<frame>.png`，核心包也有自己的参考图：`testdata/ref/rig` 下是骨架画出的一组姿势（每种武器、双手、转身、腾空），`testdata/ref/core` 下是画笔的每种形状（正常摆放、镜像和旋转）以及模拟中的每种粒子、碎片和光。一帧中与参考图在任一通道上相差超过 2 的像素不超过 0.5% 时视为一致；这个容差就是 `pixelfxtest.MaxChannelDelta` 和 `pixelfxtest.MaxDifferingShare`，只在一处说明，只有写明理由才放宽。在 amd64 上，已移植的特效除个别像素外逐位一致；在 arm64 上，除融合指令把个别像素推过边界外，相差都在一级之内。把 `PIXELFXTEST_OUT` 设为一个目录，不一致的帧会写到那里。

## 移植一个类别

每个类别对应它的 issue 做一次改动。移植步骤：

1. 克隆 pixelfx 并切到固定的提交：`git clone https://github.com/patch-gg/pixelfx`，然后 `git -C pixelfx switch --detach 5294518ce7c72e48b6100a744596e0acf57f2826`。不要运行它的 npm 安装脚本；参考工具只需要 Node 18 或更新版本，pixelfx 的渲染代码没有任何依赖。
2. 把类别的 `_lib.js` 移植到 `packages/game/pixelfx/<category>` 的 `lib.go`（核心那次改动可能已经在子包里放了其中几个函数），每个特效各放一个以它命名的文件（`fire-ring.js` 对应 `fire_ring.go`），文件开头写明上游文件和提交、MIT 许可，以及原文件带有的 Sprite Fusion 声明。保持 pixelfx 的运算、浮点运算的顺序和调用随机数生成器的顺序：Go 与 JavaScript 一样从左到右求值参数和调用，但 JavaScript 循环条件里抽取的随机数每一轮都会重新抽取，而 `Math.PI + 0.07` 这样的常量表达式必须用变量表示 π（Go 在编译时精确折叠常量，JavaScript 每一步都会舍入）。
3. 把每个特效导出为包级变量（`var FireRing = &pixelfx.Effect{...}`），并在 `init` 中注册为 `"<category>/<name>"`。JavaScript 中作为函数的颜色或透明度对应画笔的 `...Fn` 方法；选项对应 `Meta.Options` 及其默认值；`act` 对应 `Act`，命中中那些省略时由 pixelfx 取默认值的数值用 `Opt` 表示。
4. 渲染参考图：`node tools/pixelfxref/render.mjs --pixelfx <克隆目录> <category>`，并添加类别的 `parity_test.go`，为每个特效调用 `pixelfxtest.CheckEffect` 和 `pixelfxtest.CheckDeterministic`，再加一个 `pixelfxtest.BenchmarkFrames` 基准测试，参照 `pixelfx/fire`。
5. 在 `packages/game/cmd/game/pixelfx.go` 中导入该类别，让 `game pixelfx` 能渲染它；保留它在稳定性表中的行和它的 `example_test.go`，更新上面的表格及其英文版，并添加一个更新日志片段。

某一帧超出容差时，用 `PIXELFXTEST_OUT` 把它写出来与参考图对比：形状错位是移植错误（通常是随机数抽取顺序不对），零星的一级差异则来自浮点运算。

## 许可

移植版遵循 pixelfx 的 MIT 许可：`packages/game/pixelfx/LICENSE` 保存其许可文本，`NOTICE.md` 保存其声明，包括 Sprite Fusion 对模拟及改编自其 VFX 的特效的声明，以及 Hollow Crown 对表演系统的声明。pixelfx 画廊中的 Patch Pixel 字体不在 MIT 许可之下，没有移植；代码里绘制的两种像素字体已移植。

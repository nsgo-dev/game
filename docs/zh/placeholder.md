# 占位素材

`game/placeholder` 是一套用于原型制作的小型素材包：精灵表、附带 Tiled 图块集和示例关卡的图块集、一款像素字体、音效和一段循环音乐，以及三个 glTF 模型，每张图片和每段声音都带有[导入设置旁车文件](cli.md#assets)。每个文件都是在本仓库中用 Go 绘制、合成或建模的原创作品，全部以 [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/deed.zh-hans) 贡献到公有领域：游戏可以使用、修改和发布它们，无论是否商用，都无需询问，也无需署名。

```go
import "github.com/nsgo-dev/game/pkg/placeholder"
```

示例和教程只使用这套素材包，或写明了许可证的自有素材；见[本仓库中的署名](#本仓库中的署名)。

## 包含内容

文件位于 `pkg/placeholder/pack`，由这个包嵌入，并由 `placeholder.FS()` 返回：

| 文件夹 | 文件 | 内容 |
| --- | --- | --- |
| `sprites/` | `player`、`slime`、`bat`、`coin`、`items`、`effects`、`ui`，各有 `.png`、`.json` 和 `.png.import.yaml` | 由 16×16 像素画帧排成的一行，每张的 Aseprite JSON 数组导出（动画为帧标签），以及按同一网格切分精灵表的旁车文件 |
| `tiles/` | `tileset.png`、`tileset.tsj`、`level.tmj`、`tileset.png.import.yaml` | 32 个 16×16 图块，记录每个图块类型、属性和动画的 Tiled 图块集，以及一个 40×15 的示例关卡 |
| `fonts/` | `pixel.png`、`pixel.fnt`、`pixel.png.import.yaml` | Placeholder Pixel，覆盖可打印 ASCII 的比例字体，格式为 AngelCode BMFont |
| `sounds/` | `jump`、`coin`、`hit`、`explosion`、`shoot`、`powerup`、`click`、`select`、`music_loop`，各有 `.wav` 和 `.wav.import.yaml` | 22050 Hz 单声道 WAV 音效和一段四小节的循环音乐 |
| `models/` | `crate.glb`、`robot_arm.gltf`、`gem.glb` | glTF 2.0 模型，+Y 向上，-Z 向前，立于原点 |
| | `CREDITS.md` | 列出每个文件的类型和许可证 |

精灵表及其动画：

| 精灵表 | 帧数 | 动画（帧标签） |
| --- | --- | --- |
| `player` | 8 | `idle`（0–1）、`run`（2–5）、`jump`（6）、`hurt`（7） |
| `slime` | 4 | `idle`（0–3） |
| `bat` | 4 | `fly`（0–3） |
| `coin` | 4 | `spin`（0–3） |
| `items` | 8 | `gem`、`heart`、`key`、`star`、`potion`、`bomb`、`shield`、`flag`，各一帧 |
| `effects` | 8 | `explosion`（0–4，只播放一次）、`sparkle`（5–6）、`bullet`（7） |
| `ui` | 4 | `panel`、`button`、`button_pressed`、`cursor`，各一帧；面板和两个按钮同时也是边框为 4 像素的九宫格 |

精灵表的第 `i` 帧名为 `<精灵表>_<i>`，例如 `player_2`。每一帧的轴心都在其正中。

图块集中的图块，按 id 排列（Tiled 的图块 id，比地图中的全局 id 小 1）：

| id | 图块 | 属性 |
| --- | --- | --- |
| 0–4 | 草地、泥土、石头、砖块、木头 | `solid` |
| 5 | 平台 | `oneWay` |
| 6 | 梯子 | `ladder` |
| 7 | 尖刺 | `hazard` |
| 8–11 | 水，在 8–11 间播放动画 | `water` |
| 12–15 | 岩浆，在 12–15 间播放动画 | `hazard` |
| 16、17 | 冰、箱子 | `solid` |
| 18–23 | 灌木、花、告示牌、门上半、门下半、墙 | |
| 24–27 | 金属、奖励砖块、沙子、雪 | `solid` |
| 28、29 | 火把，在 28–29 间播放动画 | |
| 30、31 | 锁链、窗户 | |

`tilemap` 包会把 `solid` 和 `oneWay` 属性变成图块标志 `TileSolid` 和 `TileOneWay`；其余属性由游戏通过 `Tilemap.TileProperties` 读取。关卡有 `decor` 和 `ground` 两个图块层以及一个 `objects` 层：类型为 `spawn`（名为 `player`）、`coin`、`gem`、`slime` 和 `bat` 的点，以及门上的一个 `exit` 矩形。

## 在代码中加载

每个函数读取嵌入的文件，并返回对应包加载出的结果：

```go
player, err := placeholder.Sheet("player")   // *sprite.Atlas，含 Animations["run"] 等
font, err := placeholder.Font()              // *text.Font
level, err := placeholder.Level()            // *tilemap.Tilemap，图块集已加载
crate, err := placeholder.Model("crate.glb") // *gltf.Document
jump, err := placeholder.Sound("jump")       // core.AudioSource，可交给 audio.LoadSound
```

- `Sheet`、`Font` 和 `Level` 给出的图片是 `core.ImageSource` 值，持有 PNG 的字节及其以点为单位的尺寸，一点对应一个像素。`Image` 对素材包中的任意 PNG 做同样的事。
- `FS()` 以只读 `fs.FS` 的形式返回整个素材包，用于其他用途，例如 `fs.ReadFile(placeholder.FS(), "CREDITS.md")`。
- `License` 是 `"CC0-1.0"`，即每个文件的 SPDX 标识符。

只有导入了这个包的游戏才会把素材包嵌入二进制文件，增加约 430 KB，其中三分之二是音乐。

## 复制到项目中

若要修改素材，或把它们构建进游戏自己的图集，可将 `pkg/placeholder/pack` 复制到项目中，例如复制为 `art/placeholder`，再像其他素材一样用 [`game assets`](cli.md#assets) 构建。下面这段 `game.yaml` 的 assets 部分按设计意图使用每个旁车文件：

```yaml
assets:
  atlases:
    - name: sprites
      in: art/placeholder/sprites
      out: assets
  textures:
    - in: art/placeholder/tiles/tileset.png
      out: assets/tileset.ktx2
    - in: art/placeholder/fonts/pixel.png
      out: assets/pixel.ktx2
  audio:
    - in: art/placeholder/sounds/jump.wav
      out: assets/jump.wav
    - in: art/placeholder/sounds/music_loop.wav
      out: assets/music_loop.wav
    # 用到的其他每段声音也照此添加
```

- 每张精灵表的旁车文件把它切成 16×16 的帧，图集将其命名为 `<精灵表>_<i>.png`。图集只包含帧；精灵表的动画仍保存在各自的 JSON 文件中。
- 图块集和字体页的旁车文件设置 `nearest` 过滤，让像素画缩放时保持清晰。
- 每个音效的旁车文件把峰值归一化到 -3 dBFS；音乐的旁车文件把它归一化到 -6 dBFS，保持 WAV 格式并循环整段，game assets 会把循环写进输出文件的 `smpl` 块。
- `game assets` 从 TrueType 和 OpenType 文件烘焙字体，而素材包里的字体已经是位图字体，所以字体的 `pixel.fnt` 与 Tiled 的 `tileset.tsj` 和 `level.tmj`、精灵表的 JSON 文件一样，都不是构建目标。需要自己把它们复制到游戏打包的素材中，放在它们所引用的 PNG 页面（`pixel.png`、`tileset.png`）旁边，并像 `Font` 和 `Level` 那样用 `text.LoadBMFontImage` 和 `tilemap.LoadTiled` 加载，后者的 `TilesetLoader.LoadImage` 把图块集的图片路径映射为图片源。KTX2 纹理带有 `nearest` 过滤，[`render3d.LoadKTX2`](render3d.md) 为 3D 加载纹理时会应用它；`text` 和 `tilemap` 从 PNG 页面绘制，所以 2D 游戏从这些页面绘制字体和关卡。
- 没有任何目标读取其素材的旁车文件会被拒绝，所以只用到部分素材的项目只需复制那些文件及其旁车文件。

## 重新生成

`pkg/placeholder/internal/gen` 中的生成器（模型来自 `internal/samplegltf`）绘制除 `CREDITS.md` 以外的每个文件，并且在每次运行、每种架构上写出相同的字节。有一个测试会把提交的素材包与它比较；修改生成器后，在仓库根目录运行以下命令重写素材包：

```sh
go tool nsgo test ./placeholder -run TestPackMatchesGenerator -update
```

并在 `pack/CREDITS.md` 中列出新增的文件，这一点也有测试检查。

## 本仓库中的署名

示例、教程、文档和项目模板只使用这套素材包，或写明了许可证的自有素材。`game/placeholder` 中的一个测试负责保证这一点：`examples/`、`docs/` 和 `template/` 下的每个图片、声音、字体、模型，其他工具导出的地图或图块集，以及放在图片旁边、以 JSON 导出的图集，都必须连同许可证列在其所在文件夹或上级文件夹的 `CREDITS.md` 中，写在带有 `License` 列的 Markdown 表格里。许可证是 `CC0-1.0`、`Apache-2.0` 这样的 SPDX 标识符，可以直接写，也可以写成链接的文字；`CREDITS.md` 列出的每个文件都必须存在：

```markdown
| File | Kind | Author | License |
| --- | --- | --- | --- |
| `assets/hero.png` | 64×16 sprite sheet | Your name | CC0-1.0 |
```

示例若需要无法用代码绘制的素材，就改用这套素材包，或者像 Cave Run 的 [`CREDITS.md`](../../examples/caverun/CREDITS.md) 那样用这样的表格列出自己的文件。

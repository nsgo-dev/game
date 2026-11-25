# roguelike2d 模板

`roguelike2d` 是一个小型回合制 roguelike：探索者下到微光深渊（Glimmerdeep）的五层，与栖居其中的怪物战斗，收集武器、护甲、药水、药剂和碎晶，最后从底层取走心之余烬（Heart Ember），而宝库守卫（Vault Warden）就站在它上面。每一层都由本局的种子生成，死亡是永久的：一局结束的那一刻，这一局的存档就被删除，同时保留本局总结和所有对局的记录。它基于 [`procgen`](procgen.md)、[`loot`](loot.md)、[`stats`](stats.md)、[`ability`](ability.md)、[`save`](save.md)、`random`、[`input`](input.md) 和 [`screen`](screen.md) 构建，美术由代码绘制。

```sh
go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new -template roguelike2d mygame
cd mygame
go tool nsgo dev
```

`nsgo init -template github.com/nsgo-dev/game/template/roguelike2d` 会写出同样的项目，见[项目模板](templates.md)。

## 它展示了什么

- 由 [`procgen`](procgen.md) 从本局种子分出的随机流生成的楼层：用走廊相连的 BSP 房间和元胞自动机洞穴，出口放在离起点最远的地面格上，所以同一个种子在每个平台上都生成同样的楼层。
- 回合：玩家每做一个动作，每个怪物随后各行动一次。怪物在感知距离内发现玩家，沿广度优先的距离图追猎，贴身时出手；走向怪物就是攻击它。玩家沿视线看见周围，见过的格子会继续画出，只是更暗。
- 来自 [`loot`](loot.md) 的加权掉落：每层的物品格和每个怪物的掉落都从 `data/loot.yaml` 掷出，药水有软保底、最好的武器有硬保底，保底计数在楼层之间延续。
- 来自 [`stats`](stats.md) 的属性：生命、攻击和防御，武器和护甲各占一个装备槽，由 `Sheet.Refresh` 替换；限时药剂的修饰符由 `Sheet.Step` 每回合倒数一次；升级会提高基础值。
- 来自 [`ability`](ability.md) 的技能：消耗一瓶药水的饮药，以及打击所有相邻敌人的横扫，受条件和冷却约束。施法器以每秒一步运行，所以 `data/abilities.yaml` 里的每个时长都按回合计。
- 基于 [`save`](save.md) 的永久死亡：玩家每到达一层就保存这一局，关掉应用后从那里继续；这一局胜利或失败时存档被删除。记录存档统计每一局，一局结束时的面板显示本局总结和记录。
- `game.yaml` 里的一份输入映射，同时覆盖键盘、手柄、触屏和鼠标；在手机和平板上，以及任何设备在游戏被触摸或点击过之后，会画出屏幕按键。
- 测试分别用键盘、手柄和触屏把游戏玩到获胜，再玩到死亡，检查这一局的存档已删除、记录计入了这一局，从存储中恢复一局，生成四十个种子的每一层，用两个机器人在这些种子上检查平衡，逐条检查规则，并检查每个数据文件都是规范格式。

## 操作

| | 键盘 | 手柄 | 触屏 |
| --- | --- | --- | --- |
| 移动，或攻击挡路的怪物 | 方向键、WASD | 左摇杆、方向键 | 左下角的箭头 |
| 等待一回合 | 空格、`.` | 下方按键 | 箭头中间的圆点 |
| 喝一瓶药水 | Q | 左方按键 | 右下角的 `!` |
| 横扫身边所有敌人 | E | 上方按键 | 右下角的 `C` |
| 一局结束后再来一局 | R、回车 | Start | “Play again”按钮 |

按住一个方向会持续行走：先走一步，之后每隔几帧再走一步。在屏幕按键上按住鼠标左键，效果和触摸它一样。

## 项目结构

| 文件 | 内容 |
| --- | --- |
| `game.yaml` | 项目文件：窗口、320×180 的虚拟屏幕、输入映射和导出预设。 |
| `data/dungeon.yaml` | 玩家的成长、楼层、怪物和物品，格式见下文。 |
| `data/stats.yaml` | 玩家的属性，以及武器、护甲和药剂施加的效果，格式见 [`stats`](stats.md)。 |
| `data/loot.yaml` | 楼层和怪物掷的掉落表，格式见 [`loot`](loot.md)。 |
| `data/abilities.yaml` | 饮药和横扫，格式见 [`ability`](ability.md)。 |
| `dungeon.go`、`data.go` | 地牢格式，以及加载四个文件并检查每个文件引用其他文件的名称。 |
| `floor.go` | 生成一层：格子、起点、出口、怪物和物品，以及距离图和视线。 |
| `run.go` | 一局的规则，一次一个回合，不涉及设备和绘制。 |
| `records.go` | 对局存档和记录存档，以及本局总结。 |
| `game.go`、`draw.go` | 根组件、固定步长循环、输入、HUD、一局结束时的面板和绘制。 |
| `*_test.go` | 通关测试、机器人、规则、楼层生成和数据文件。 |

地牢文件按一局下行的顺序列出楼层；除最后一层外每层都有下行楼梯，最后一层放着圣物：

```yaml
version: 1
name: The Glimmerdeep
player:
  weapon: rusty-pick
  draughts: 2
  draughtHeal: 15
  levelXP: 10
  levelHealth: 5
  levelAttack: 1
  regenTurns: 5
floors:
  - name: The Mossy Galleries
    generator: bsp
    width: 40
    height: 30
    monsters: 6
    items: 5
    loot: floor-shallow
monsters:
  - id: moss-rat
    name: Moss Rat
    glyph: r
    color: '#84cc16'
    health: 5
    attack: 3
    defense: 0
    xp: 2
    sense: 5
    drops: rat
    from: 1
    to: 2
    weight: 3
items:
  - id: rusty-pick
    name: rusty pick
    kind: weapon
    effect: rusty-pick
    rank: 1
    color: '#a8a29e'
```

| 字段 | 含义 |
| --- | --- |
| `player` | 起始武器和药水数、一瓶药水恢复的生命、每级所需的经验（第 n 级升级需要 n 乘以 `levelXP`）、每级给 `maxHealth` 和 `attack` 基础值增加多少，以及每恢复 1 点生命需要几回合（0 表示不恢复）。 |
| `floors` | 每层的 `generator`（`bsp` 或 `cave`）、以格计的尺寸（每边 16 到 96）、生成多少怪物、掷几次 `loot` 表，以及可选的 `guardian`：一个站在出口上、从不离开的怪物。 |
| `monsters` | 每种怪物的显示名称、绘制用的字母和 `#rrggbb` 颜色、战斗数值、给的经验、开始追猎的步行距离、可选的 `drops` 掉落表，以及它出现的楼层和在其中的权重；没有 `from` 和 `to` 的怪物只作为守卫出现。 |
| `items` | 每种物品的 `kind`：`weapon` 或 `armor` 在 `rank` 高于当前装备时装上并施加其属性 `effect`，`draught` 增加药水，`tonic` 施加其限时 `effect`，`shards` 是本局得分。掉落的数量就是一次掉落给多少药水或碎晶。 |

未知字段、未知的物品种类和生成器、未定义的名称以及 1 以外的版本都会被拒绝，游戏写回文件时逐字节不变，所以评审只看到真正的改动。`TestDataFilesAgree` 检查四个文件之间互相引用的名称。

## 从哪里改

- **楼层**：编辑 `data/dungeon.yaml` 的 `floors`。`TestFloorsAreSound` 生成四十个种子的每一层，报告不连通、出口太近或生成位置重叠的楼层；`generateFloor` 里的 `procgen.BSPConfig` 和 `procgen.CaveConfig` 可以进一步调整房间和洞穴的形状。
- **平衡**：怪物的数值、玩家的成长和掉落权重。`TestBotsWinAndLose` 在四十个种子上分别用一个清空每层的机器人和一个直奔出口的机器人游玩，前者不再获胜或后者不再死亡时失败，并在日志里记下两者的次数。
- **新的怪物或物品**：在 `data/dungeon.yaml` 加一条，装备和药剂在 `data/stats.yaml` 加它的属性效果，物品还要在掉落表里加一行。新的物品种类需要在 `run.take` 里加一个分支。
- **新的技能**：在 `data/abilities.yaml` 加一条带 `action` 的技能，在 `game.yaml` 为这个动作加绑定，在 `run.abilityEvent` 里加它的效果，在 `roguelikeGame.update` 里激活它。
- **更聪明的怪物**：`run.monstersAct` 沿一张距离图行走；[`ai`](ai.md) 提供行为树和寻路，可以做会逃跑、保持距离或巡逻的怪物。
- **操作**：`game.yaml` 的 `input` 部分；触屏区域以虚拟屏幕的点为单位。`game.go` 里的 `repeatDelay` 和 `repeatEvery` 决定按住方向时走多快。
- **美术**：`draw.go` 绘制字母和图形；可以改用 `sprite.LoadAtlas` 加载图集，并绘制用 `procgen.Grid.Paint` 填好的 `tilemap.Tilemap`。
- **存档**：`records.go` 里的 `runSave` 就是对局存档。它的结构改变时，提高 `saveVersion` 并给 `newSaveStore` 加上 `save.WithMigration`。

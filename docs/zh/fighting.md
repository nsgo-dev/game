# fighting

`github.com/nsgo-dev/game/packages/game/fighting` 按固定步长逐步缓冲格斗游戏输入，并识别 YAML 招式表中描述的指令输入（四分之一圈、升龙、蓄力、双击）、按键、同时按键与连招。

## 小键盘记法

方向采用小键盘记法，以面朝右的角色为准：

```text
7 8 9     后上   上    前上
4 5 6     后    中立    前
1 2 3     后下   下    前下
```

`Direction` 保存一个数字，`DirectionOf(x, y)` 由符号构造方向，`Mirror` 交换左右。角色面朝左（`FaceLeft`）时，识别器在匹配前先镜像每个方向，因此 `6` 始终表示朝向对手，同一份招式表在两侧都适用。

## 招式表

招式表是带版本号的 YAML 文件。未知字段会被拒绝，`Encode` 写出规范形式（块风格、字段按 schema 顺序、指令项写成带引号的字符串），因此解析 `Encode` 的输出后再编码会逐字节还原。

```yaml
version: 1
input:
  up: up
  down: down
  left: left
  right: right
  axisX: move_x
  axisY: move_y
moves:
  - name: lp
    press:
      - lp
  - name: hadoken
    motion:
      - "2"
      - "3"
      - "6"
    window: 12
    press:
      - lp
      - hp
    buffer: 8
    priority: 20
  - name: sonic_boom
    motion:
      - "6"
    charge:
      dirs: "147"
      steps: 40
      grace: 6
    press:
      - lp
    buffer: 6
    priority: 25
  - name: forward_dash
    motion:
      - "6"
      - "5"
      - "6"
    window: 12
    priority: 10
  - name: throw
    press:
      - lp
      - lk
    pressAll: true
    pressWindow: 2
    priority: 40
combos:
  - name: jab_fireball
    sequence:
      - lp
      - hadoken
    gap: 20
```

`input` 指定构成摇杆的动作与轴。数字动作给对应轴加 ±1，模拟轴加上其数值；某轴合计达到 `deadzone`（默认 `0.5`）即视为推向该方向，因此同时按住左右等于中立。`axisY` 默认正值向下，设置 `yUp` 后正值向上。

每个招式有 `name`，并有 `motion`、`press` 或两者兼有：

| 字段 | 含义 |
| --- | --- |
| `motion` | 在互不相同且递增的步上依次匹配的方向项。每项是一个或多个数字组成的字符串，任一数字即满足该项，例如 `"23"` 接受下或前下。 |
| `window` | 从第一个到最后一个指令项最多相隔的步数；只用于含 2 项及以上的指令。 |
| `charge` | `dirs` 至少连续保持 `steps` 步，且其结束不早于第一个指令项前一步之前 `grace` 步。 |
| `press` | 完成招式的按键动作；当前步按下其中任一即可。 |
| `pressAll`、`pressWindow` | 要求 `press` 中所有按键在 `pressWindow` 步内全部按下（同时按键）。 |
| `buffer` | 完成按键最多可在最后一个指令项之后多少步；需要同时有 `motion` 和 `press`。 |
| `priority` | 同一步完成多个招式时的排序，高者在前。 |

所有步数都包含端点，且不超过 `MaxSteps`（600）。没有 `press` 的指令在首次满足的那一步完成，所以一直按住最后一个方向不会重复触发。已经完成过某个招式的输入不能再次完成它：完成四分之一圆后在 `buffer` 内再按一个键不算第二发波动拳，下一发需要重新输入指令。一份招式表最多使用 32 个按键。

连招列出至少两个招式；每个招式必须在前一个之后 `gap` 步内完成，中间插入其他招式会中断连招。

## 识别输入

```go
set, err := fighting.LoadFile("moves.yaml")
if err != nil {
	return err
}
rec, err := fighting.NewRecognizer(set)
if err != nil {
	return err
}

loop := &game.Loop{
	Rate: 60,
	Update: func(dt time.Duration) {
		actions.Step()
		res := rec.Update(actions, player.Facing)
		if len(res.Moves) > 0 {
			player.Perform(res.Moves[0].Name)
		}
		for _, c := range res.Combos {
			hud.Announce(c.Name)
		}
	},
}
```

每个固定步在输入源前进之后调用一次 `Update`。`Result.Moves` 包含该步完成的所有招式，按优先级从高到低排列，`Moves[0]` 就是应当施放的招式；只有它会推进连招。每个 `Match` 记录其输入开始的步和完成的步。返回的切片会被下一次调用复用，不产生内存分配。

`Feed(raw, held, facing)` 用已解码的方向和按键掩码（第 `i` 位对应 `Buttons()[i]`）记录一步，例如来自网络同步的输入。识别结果只取决于输入的帧，因此用相同输入重新模拟会得到相同结果。回滚时，`AppendState(dst)` 把识别器的完整状态（缓冲的帧以及每个招式和连招的进度）追加到快照中，`LoadState` 在用同一份招式表构建的识别器上恢复它，然后再重新模拟；来自其他招式表的状态会被拒绝，识别器保持不变。

输入历史本身是 `Buffer`，即按 `MoveSet.Lookback` 定长的 `Frame` 环形缓冲（绝对与相对方向、朝向、按住与新按下的按键），可通过 `Recognizer.Buffer` 用于输入显示和训练模式。

## 用回放测试

`Recognizer.Update` 接受任何 `Source`（`Pressed` 与 `Axis`），`*input.Actions`、`*replay.Recorder` 与 `*replay.Player` 都满足它，所以 [replay](replay.md) 录制的输入可以直接驱动识别器：

```go
recording, _ := replay.LoadFile("testdata/session.replay.yaml")
p := replay.NewPlayer(recording)
for !p.Done() {
	p.Step()
	res := rec.Update(p, fighting.FaceRight)
	// 对 res.Step 和 res.Moves 做断言
}
```

本包的测试就是这样录制脚本输入，并对每个指令在窗口、缓冲、蓄力时间、宽限与连招间隔内外各一步分别检查。

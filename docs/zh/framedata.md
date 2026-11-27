# framedata

`github.com/nsgo-dev/game/pkg/framedata` 为格斗游戏的招式提供逐帧的攻击框、受击框和命中属性（伤害、硬直、防御硬直、击退、取消窗口）。角色的帧数据是一个 YAML 文件，由 `World` 在每个固定步长中把它解析为命中，每次结果都相同。

```go
import "github.com/nsgo-dev/game/pkg/framedata"
```

## 帧数据文件

一个文件列出一个角色的招式。一个招式持续 `length` 帧，每帧对应 [loop](loop.md) 的一个固定步长，和公开的帧数据表一样从 1 开始编号。每个框从第 `start` 帧到第 `end` 帧（含）生效。

```yaml
version: 1
moves:
  - name: idle
    animation: idle
    length: 8
    loop: true
    boxes:
      - kind: hurt
        start: 1
        end: 8
        x: -12
        "y": -80
        width: 24
        height: 80
  - name: jab
    animation: jab
    length: 14
    boxes:
      - kind: hurt
        start: 1
        end: 14
        x: -12
        "y": -80
        width: 24
        height: 80
      - kind: hit
        start: 4
        end: 6
        x: 12
        "y": -66
        width: 28
        height: 12
        damage: 30
        hitstun: 14
        blockstun: 9
        knockbackX: 2.5
    cancels:
      - start: 4
        end: 10
        when: contact
        into: [fireball]
```

| 字段 | 含义 |
| --- | --- |
| `version` | 结构版本，`1` |
| `moves[].name` | 唯一名称，供 `Start`、`Force`、`Neutral` 和 `into` 引用 |
| `moves[].animation` | 可选，游戏同时播放的精灵动画，例如 [sprite](sprite.md) 图集 `Animations` 中的一项 |
| `moves[].length` | 招式持续的帧数，1 到 3600 |
| `moves[].loop` | 最后一帧之后从头开始，用于待机、行走、蹲下等姿态 |
| `boxes[].kind` | `hurt`（可被击中）或 `hit`（发动攻击） |
| `boxes[].start`、`end` | 第一帧和最后一帧生效帧 |
| `boxes[].x`、`"y"`、`width`、`height` | 角色朝右时相对原点的矩形，单位为点，Y 向下增长 |
| `boxes[].group` | 命中组，0 到 63：同一次命中的攻击框共用一组，多段攻击的每一段各用一组 |
| `boxes[].damage`、`hitstun`、`blockstun` | 造成的伤害，以及命中或被防御时的硬直帧数 |
| `boxes[].knockbackX`、`knockbackY` | 命中时的击退，X 正方向为远离攻击者：指向受击者所在的攻击者一侧，因此逆向攻击（cross-up）也能推向正确方向；两者 X 相同时取攻击者朝向 |
| `cancels[].start`、`end` | 窗口开放的帧 |
| `cancels[].when` | 省略（始终）、`hit`、`block` 或 `contact`（命中或被防御） |
| `cancels[].into` | 该窗口可以取消成的招式 |

`Parse` 拒绝未知字段、版本不是 1、招式重名、框或窗口超出招式范围、尺寸不是正的有限值、受击框带命中属性，以及取消成不存在的招式。`Encode` 按上表顺序以 2 空格缩进写出字段，因此读入再保存不会改变文件。它会给键 `"y"` 加引号，因为 YAML 1.1 把不带引号的 `y` 读作布尔值；`Parse` 两种写法都接受。

`Move.FrameData` 像帧数据表一样概括一个招式：发生、持续和收招帧，以及最早的攻击框在其第一帧命中时的命中有利帧和防御有利帧。上面的 jab 为发生 4 帧、持续 3 帧、收招 8 帧，命中 +4，被防 -1。

## 角色与命中

```go
doc, err := framedata.Parse(data)
if err != nil { ... }
p1 := &framedata.Fighter{Moves: doc, Neutral: "idle", X: 100}
p2 := &framedata.Fighter{Moves: doc, Neutral: "idle", X: 160, FacingLeft: true}
world := &framedata.World{Fighters: []*framedata.Fighter{p1, p2}}

loop := &game.Loop{Rate: 60, Update: func(time.Duration) {
	if actions.JustPressed("punch") {
		p1.Start("jab")
	}
	p2.Blocking = actions.Pressed("back")
	for _, h := range world.Step() {
		health[h.Victim] -= h.Damage
		h.Victim.X += h.KnockbackX
		spawnSpark(h.X, h.Y)
	}
}}
```

- **`Start(name)`** 在角色可以行动（没有硬直，且当前招式循环或已到最后一帧）时，或当前招式在本帧开放的取消窗口列出了该招式时，开始这个招式。`CanStart` 只询问不开始。**`Force(name)`** 无条件开始招式，用于受击反应、投技和脚本招式。
- **`World.Step`** 让每个角色前进一帧，然后用每个生效的攻击框检测其他角色生效的受击框。不循环的招式结束后，角色回到 `Neutral` 招式。
- **命中**全部根据本步应用任何命中之前的状态求出，所以同一帧互相击中的角色会相杀。角色按切片顺序、框按文件顺序遍历，相同的输入总是以相同顺序产生相同的命中，[replay](replay.md) 和回滚 [netplay](netplay.md) 代码依赖这一点，并配合下文的保存与恢复对局。
- **每组一次**：一个招式实例对每个对手每个命中组只命中一次，每步最多一次；开始招式或循环一轮都会开始新的实例。
- **硬直**：受击者承受攻击框的 `hitstun`，在 `Blocking` 时承受 `blockstun`，并回到 `Neutral` 招式。新的命中替换剩余硬直；同一步内多次命中取最长者。`Stun()` 返回剩余步数，`Actionable()` 报告何时可以再次行动。
- **队伍**：`Team` 相同且不为 0 的角色互不命中。
- **接触**：`Contact()` 报告当前招式是否命中或被防御，用于打开 `when: hit`、`when: block` 和 `when: contact` 窗口。

`Step` 复用返回的切片，预热后不再分配内存：两个角色每步约 0.1 µs。`group` 不在 0 到 63 之间的攻击框永远不会命中，只有在 Go 中构建且未经 `Validate` 的 `Doc` 才可能出现这种情况。

## 保存与恢复

`World.Save` 把 `Step` 读取和修改的全部内容复制到 `WorldState`：每个角色的位置、朝向、`Blocking`、按名称记录的招式、帧、硬直、接触状态，以及其招式已命中的命中组，对手以其在 `Fighters` 中的下标表示。`World.Restore` 把它恢复回去。回滚会话每个 tick 保存一次，迟到的输入到达时恢复最后确认的状态，再次步进得到相同的命中：

```go
var state framedata.WorldState

func (s *sim) Save() []byte {
	s.world.Save(&state)
	data, _ := json.Marshal(state)
	return data
}

func (s *sim) Load(data []byte) error {
	var st framedata.WorldState
	if err := json.Unmarshal(data, &st); err != nil {
		return err
	}
	return s.world.Restore(&st)
}
```

`WorldState` 不含指针，可以编码为 JSON 或 YAML，也可以哈希为校验和。`Save` 复用传入状态的切片，预热后不再分配内存，复制结果与 `World` 不共享内存。`Restore` 复制状态；角色数量不同、招式不在该角色的 `Moves` 中、帧超出招式范围，或已命中的对手不是另一个角色时，它拒绝该状态且不做任何修改。角色的 `Moves`、`Neutral` 和 `Team` 属于配置，不在状态之内。

## 调试叠加层

[gamedebug](gamedebug.md) 叠加层把角色生效的框绘制为线框，先画受击框，攻击框画在上面，并按角色朝向镜像：

```go
overlay.DrawFighterBoxes(d, p1, core.RGB(0x38bdf8), core.RGB(0xef4444))
```

`Fighter.Boxes(dst)` 以世界坐标返回同样的框，供游戏自行绘制或测试使用。

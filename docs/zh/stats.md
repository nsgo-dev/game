# stats

`game/stats` 保存实体的玩法属性：带名字的基础值，由来自装备、增益等来源的加法、乘法和覆盖修饰符改变。任何改动都会立即重新计算数值；定时修饰符按固定步数计时，持续 300 步的增益恰好在第 300 步结束。

```go
import "github.com/nsgo-dev/game/pkg/stats"
```

## 属性表

`Sheet` 按定义顺序保存一个实体的属性：

```go
hero, err := stats.NewSheet(
	stats.Def{Name: "health", Base: 100, Min: stats.Float(0)},
	stats.Def{Name: "attack", Base: 12},
	stats.Def{Name: "speed", Base: 5, Min: stats.Float(0), Max: stats.Float(20)},
)

hero.Value("attack") // 12
hero.SetBase("attack", 14)
```

`Value` 读取缓存值，从不重新计算。`Lookup` 同时报告属性是否存在，`Base` 返回修饰前的值，`Stats` 列出属性名。零值 `Sheet` 可直接用 `Define` 定义属性。

## 修饰符及其叠加规则

`Modifier` 指定一个属性、一个 `Op` 和一个数值。每个修饰符都从一个来源添加，来源是自由格式的标签，例如 `"equip:weapon"` 或 `"buff:haste"`：

| Op | YAML | 作用 |
| --- | --- | --- |
| `OpAdd` | `add` | 把数值加到基础值上 |
| `OpMul` | `mul` | 在所有加法之后乘以该数值 |
| `OpOverride` | `override` | 替换结果，忽略所有加法和乘法 |

属性值为：

```
clamp((基础值 + 所有 add 之和) × 所有 mul 之积)
```

属性上存在任何覆盖修饰符时，值改为 `clamp(覆盖值)`：`Priority` 最高的覆盖修饰符生效，优先级相同时取最后添加的那个。`clamp` 在设置了 `Min`、`Max` 时应用这两个边界，对覆盖值同样生效。除了在优先级相同的覆盖修饰符之间决胜外，添加顺序只影响浮点舍入：加法和乘法按添加顺序计算，所以 `0.1 + 0.2 + 0.3` 与 `0.3 + 0.1 + 0.2` 的最后一位可能不同。需要比较时请带容差。溢出 `float64` 的和或积会饱和到同号的最大有限值，因此属性值始终有限，并且在边界之内。

| 基础值 | 修饰符 | 值 |
| --- | --- | --- |
| 10 | add 5、add 3、add −2 | 16 |
| 10 | mul 2、mul 1.5 | 30 |
| 10 | mul 2、add 5、mul 1.5、add 3 | (10 + 8) × 3 = 54 |
| 10 | add 5、override 7、mul 3 | 7 |
| 10 | override 1（优先级 5）、override 2（优先级 9） | 2 |
| 10，max 25 | mul 3 | 25 |

```go
sword, _ := hero.Add("equip:weapon", stats.Modifier{Stat: "attack", Op: stats.OpAdd, Value: 8})
hero.Add("equip:ring", stats.Modifier{Stat: "attack", Op: stats.OpMul, Value: 1.5})
hero.Value("attack") // (14 + 8) × 1.5 = 33

hero.Remove(sword)               // 按 ID 移除一个修饰符
hero.RemoveSource("equip:ring")  // 移除某个来源的全部修饰符
hero.Clear()                     // 移除全部修饰符
```

`Add` 拒绝未知属性（`ErrUnknownStat`）、未知运算（`ErrInvalidOp`）以及 NaN 或无穷大的数值（`ErrInvalidValue`）。`Modifiers` 按添加顺序列出表上的修饰符，包括各自的 ID、来源和剩余步数。

## 按固定步长计时的修饰符

`AddTimed` 添加一个持续若干次 `Step` 调用的修饰符。在每次 `game.Loop` 更新中调用一次 `Step`；修饰符恰好在添加后的第 n 次 `Step` 中被移除，属性也在同一步内重新计算：

```go
hero.AddTimed("buff:haste", stats.Modifier{Stat: "speed", Op: stats.OpMul, Value: 2}, 180)

loop := &game.Loop{Rate: 60, Update: func(dt time.Duration) {
	hero.Step() // 加速在第 180 次更新时结束：60 Hz 下为 3 秒
}}
```

由于按步数而非时长计时，循环暂停时所有增益一起暂停，回放时也在同一步结束。`Remaining(id)` 返回修饰符剩余的步数（无时限的为 0），`Steps` 返回 `Step` 已运行的次数。

## 效果

`Effect` 是一组一起应用的带名字修饰符，例如一件物品的加成，或同时影响多个属性的增益。它的 `Steps`（0 表示无时限）作用于其中所有修饰符：

```go
berserk := stats.Effect{Name: "berserk", Steps: 300, Modifiers: []stats.Modifier{
	{Stat: "attack", Op: stats.OpMul, Value: 1.5},
	{Stat: "speed", Op: stats.OpAdd, Value: 2},
}}
ids, err := hero.Apply("buff:berserk", berserk)
```

`Apply` 要么添加效果的全部修饰符，要么一个也不添加。再次应用同一效果会叠加第二份；若要刷新增益的持续时间，用它的来源调用 `Refresh`：

```go
hero.Refresh("buff:berserk", berserk) // 重新持续完整的 300 步
```

`Refresh` 移除该来源的全部修饰符并在原处应用效果，作为一次变化完成，所以 `OnChange` 只看到净变化，不会看到增益先结束再开始。先调用 `RemoveSource` 再调用 `Apply` 效果相同，但分成两次变化，监听者会在中间看到值先回落到没有增益时的值。

## 变化通知

每个值发生变化的属性，都会在引起变化的调用之后按定义顺序调用一次 `OnChange`：

```go
hero.OnChange = func(c stats.Change) {
	if c.Stat == "health" && c.New == 0 {
		die()
	}
}
```

修饰符变了但值没变的属性不会报告。`OnChange` 内部可以继续修改属性表，例如让一个属性跟随另一个属性；这些改动会在发起它们的调用返回之前完成重新计算和报告。

## 属性文件

属性定义和效果写在带版本号的 YAML 文件中，方便策划在评审中调整数值：

```yaml
version: 1
stats:
  - name: health
    base: 100
    min: 0
  - name: attack
    base: 12
  - name: speed
    base: 5
    min: 0
    max: 20
effects:
  - name: iron-sword
    modifiers:
      - stat: attack
        op: add
        value: 8
  - name: berserk
    steps: 300
    modifiers:
      - stat: attack
        op: mul
        value: 1.5
      - stat: speed
        op: add
        value: 2
  - name: frozen
    steps: 90
    modifiers:
      - stat: speed
        op: override
        value: 0
        priority: 10
```

```go
file, err := stats.Parse(data)
hero, err := file.NewSheet()
sword, _ := file.Effect("iron-sword")
hero.Apply("equip:weapon", sword)
```

`Parse` 拒绝未知字段、缺失或不同的 `version`、多余的 YAML 文档、重复或为空的名字、非有限数值、`min` 大于 `max`、负的 `steps`、没有修饰符的效果、未知运算以及作用于未定义属性的修饰符。`Encode` 以固定字段顺序和 2 空格缩进输出，并保持列表原有顺序，因此对已编码的文件解析后再编码，能逐字节还原。

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `version` | int | 模式版本，`1` |
| `stats[].name` | string | 唯一的属性名 |
| `stats[].base` | number | 修饰前的值 |
| `stats[].min`、`stats[].max` | number，可选 | 修饰后应用的边界 |
| `effects[].name` | string | 唯一的效果名 |
| `effects[].steps` | int，可选 | 效果持续的固定步数，0 或省略表示无时限 |
| `effects[].modifiers[].stat` | string | 上面定义的某个属性 |
| `effects[].modifiers[].op` | `add`、`mul` 或 `override` | 改变属性的方式 |
| `effects[].modifiers[].value` | number | 加数、乘数或覆盖值 |
| `effects[].modifiers[].priority` | int，可选 | 覆盖优先级，最高者生效 |
# 游戏效果

Stats effect 可声明 `kind`、`period`、`tickOnApply`、`grantTags` 和 `stacking` 元数据。`Sheet.ApplyEffect` 通过固定步长状态应用修饰器。

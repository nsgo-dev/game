# loot

`github.com/nsgo-dev/game/packages/game/loot` 掷取可嵌套的加权掉落表，支持保底掉落与保底计数（pity）。每次掷取都从 [`random.Stream`](determinism.md) 取随机数，所以同一个种子和同样的掷取顺序在所有平台、回放和专用服务器上都得到相同的掉落。

## 用 YAML 编写掉落表

游戏的掉落表是一份带版本号的 YAML 文档：

```yaml
version: 1
tables:
  - name: boss
    rolls: {min: 1, max: 3}
    guaranteed:
      - item: gold
        count: {min: 50, max: 120}
    entries:
      - table: common
        weight: 80
      - table: rare
        weight: 19
        count: 2
      - nothing: true
        weight: 1
  - name: common
    entries:
      - item: potion
        weight: 3
      - item: arrow
        weight: 1
        count: 10
  - name: rare
    entries:
      - item: amulet
        weight: 1
```

- `rolls` 是掷一次该表时进行的加权抽取次数（省略时为 1）。`guaranteed` 中的条目每次掷该表都会掉落，先于加权抽取。
- 条目掉落一个 `item`、掷另一张 `table`，或者什么都不掉（`nothing`）；`nothing` 条目的权重决定落空的概率。权重是正的相对值，条目的概率等于它的权重除以该表的总权重。
- `count` 是物品的数量，或表条目掷其子表的次数。`rolls` 和 `count` 写成整数，或写成均匀抽取的 `{min, max}` 区间。

`loot.Parse` 拒绝未知字段、1 以外的版本、重名、未知或循环的表引用，以及最坏情况下一次掷取会超过 `loot.MaxRollSteps`（65536 次抽取、子表掷取与掉落）的表。`loot.Encode` 把文档写回确定性的 YAML：字段顺序固定，两端相等的区间写成整数，输出的文本再解析得到相同的文档。

## 掷取

```go
doc, err := loot.Parse(data)
if err != nil { ... }
lib, err := loot.New(doc)
if err != nil { ... }

streams := random.NewStreams(worldSeed)
roller := loot.NewRoller(lib, streams.Stream("loot"))

drops, err := roller.Roll("boss") // []loot.Drop{{Item: "gold", Count: 87}, {Item: "potion", Count: 1}, ...}
```

`Library` 不可变，可以共享；`Roller` 属于一名玩家或一个存档，只在一个 goroutine 中使用。掉落按产生顺序返回，嵌套表的结果原地展开。`AppendRoll(dst[:0], name)` 复用切片，掷取时不分配内存。`Library.Chances(name)` 返回一次抽取中每个条目的概率，可用于概率公示界面。

## 保底

保底规则在连续多次没抽到表中某个加权条目后提高它的概率：

```yaml
  - name: banner
    entries:
      - table: five-star
        weight: 0.6
      - table: four-star
        weight: 5.1
      - item: three-star
        weight: 94.3
    pity:
      - name: five
        table: five-star
        softFrom: 74
        softStep: 6
        guaranteeAt: 90
      - name: four
        table: four-star
        guaranteeAt: 10
```

- `guaranteeAt: 90`：距上次抽到目标的第 90 次抽取必定是目标。
- `softFrom: 74` 加 `softStep: 6`：从第 74 次起，距上次抽到目标的第 n 次抽取时，目标权重增加 `(n - 74 + 1) * 6`。两者都设置时 `softFrom` 必须小于 `guaranteeAt`。
- 每条规则有一个未中计数：距上次抽到目标的抽取次数，目标无论是自然抽中还是被保底强制抽中都会清零。`roller.Misses("banner", "five")` 读取它，用于显示保底进度。
- 多条规则在同一次抽取同时触发时，列表中靠前的规则生效，其余规则继续计数，下一次抽取轮到下一条规则。

## 存档

保底计数与随机流一起保存，读档后继续得到相同的掉落：

```go
type LootSave struct {
	Pity   loot.State   `json:"pity"`
	Stream random.State `json:"stream"`
}

save := LootSave{Pity: roller.State(), Stream: roller.Stream().State()}

// 读档后：
stream := random.New(0)
stream.SetState(save.Stream)
roller = loot.NewRoller(lib, stream)
if err := roller.SetState(save.Pity); err != nil { ... }
```

`State` 只列出非零计数，按表名和规则名分组，序列化时键有序。`SetState` 忽略文档中已不存在的表或规则的计数，所以修改掉落文件不会让旧存档失效；负数计数会被拒绝。

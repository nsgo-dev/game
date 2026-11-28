# inventory

`game/inventory` 提供游戏道具：以带版本号的 YAML 定义道具，基于格子的堆叠容器，带类型的装备槽，以及可经 [`game/save`](save.md) 无损存取的快照。

```go
import "github.com/nsgo-dev/game/pkg/inventory"
```

## 道具定义

一个游戏的道具写在一个 YAML 文件里。每个定义都需要 `id`，其余字段可选：

```yaml
version: 1
items:
  - id: potion
    name: items.potion.name
    description: items.potion.description
    icon: asset:icons/potion.png
    maxStack: 20
    tags: [consumable, healing]
    stats:
      heal: 25
      price: 12.5
  - id: iron-helm
    name: items.iron-helm.name
    equip: head
    stats:
      armor: 3
  - id: gold-ring
    equip: ring
```

| 字段 | 含义 |
| --- | --- |
| `id` | 唯一 ID，堆叠与存档都引用它；不能含空白或控制字符。 |
| `name`、`description` | 显示文本或 [l10n](l10n.md) 键。 |
| `icon` | 道具图标的资源 URI。 |
| `maxStack` | 一个格子能放多少，范围 1 到 2^30；省略即为 1。 |
| `equip` | 道具适配的装备类别，如 `head` 或 `ring`；省略表示不能装备。 |
| `tags` | 不重复的自由标签；用 `ItemDef.HasTag` 检查。 |
| `stats` | 有限数值，如伤害、护甲、重量或价格。 |

`inventory.Parse(data)` 返回 `*Catalog`。缺少或不支持的 `version`、未知字段、重复的 ID 或标签、非法堆叠上限、非有限数值以及多余的 YAML 文档都会被拒绝。`Catalog.Encode` 输出确定的 2 空格缩进 YAML（定义保持文件顺序，数值名排序），`Parse` 读回后不变，因此编辑器或工具重写文件不会在评审中产生噪音。`inventory.NewCatalog(defs...)` 在代码中构建目录，校验规则相同。目录不可变：`Item(id)` 与 `Items()` 返回副本。

## 容器

`Container` 有固定数量的格子；每个格子为空，或放一个 `Stack{Item, Count}`，数量不超过该道具的 `maxStack`。

```go
cat, err := inventory.Parse(itemsYAML)
bag, err := inventory.NewContainer(cat, 24)

left, err := bag.Add("potion", 30) // 先补满已有的药水堆，再填空格；left 是放不下的数量
err = bag.AddAll("iron-helm", 1)    // 全部放入或一个不放：放不下时返回 ErrFull
err = bag.Remove("potion", 5)       // 从最后一个放药水的格子往前取；不够时返回 ErrNotEnough
n := bag.Count("potion")
room := bag.Room("potion")
```

移动道具遵循玩家对网格背包的预期：

| 调用 | 行为 |
| --- | --- |
| `Move(from, to)` / `MoveTo(from, dst, to)` | 目标格为空则整堆移入；目标为同一道具则尽量合并，剩余留在原格；目标为不同道具则两堆交换。 |
| `Split(from, to, n)` / `SplitTo(from, dst, to, n)` | 把 `n` 个移到空格，或移到能容纳全部 `n` 个的同种道具上；否则返回 `ErrSlotOccupied`。 |
| `Transfer(from, dst)` | 按 `Add` 的方式把一堆快速移入另一个容器；放不下的留在原格。 |
| `RemoveAt(slot, n)` | 从一个格子取出 `n` 个并返回。 |

`dst` 可以是用同一目录构建的另一个容器（箱子、商店）；目录不同的容器返回 `ErrCatalogMismatch`。任何返回错误的调用都不会改动两个容器。

## 装备

`Equipment` 是一组具名格子，每个格子接受一种类别，多个格子可以共用同一类别：

```go
gear, err := inventory.NewEquipment(cat,
	inventory.EquipSlot{Name: "head", Kind: "head"},
	inventory.EquipSlot{Name: "ring-left", Kind: "ring"},
	inventory.EquipSlot{Name: "ring-right", Kind: "ring"},
)

err = gear.Equip("head", bag, slot) // 从 bag[slot] 取一个道具
err = gear.Unequip("head", bag)
item, ok := gear.Equipped("head")
```

`Equip` 检查道具的 `equip` 与格子类别一致（否则返回 `ErrNotEquippable`），并从容器格子中取一个道具。原先装备的道具回到容器：取走后格子为空则放回该格，否则放到 `AddAll` 会放的位置。容器放不下时，`Equip` 返回 `ErrFull` 且不做任何改动；`Unequip` 同理。

## 存档

`Container.Snapshot` 与 `Equipment.Snapshot` 返回带 JSON 标签的普通值，只按格子顺序列出有道具的格子。用 `save.Store` 把它们存进存档数据，再 `Restore` 到按同样方式构建的容器或装备中：

```go
type Progress struct {
	Level int                      `json:"level"`
	Bag   inventory.ContainerState `json:"bag"`
	Gear  inventory.EquipmentState `json:"gear"`
}

err := store.Save("slot1", Progress{Level: 4, Bag: bag.Snapshot(), Gear: gear.Snapshot()})

var p Progress
if err := store.Load("slot1", &p); err != nil {
	return err
}
if err := bag.Restore(p.Bag); err != nil {
	return err
}
if err := gear.Restore(p.Gear); err != nil {
	return err
}
```

保存的 JSON 是确定的：

```json
{"level":4,"bag":{"slots":[{"slot":0,"item":"potion","count":20},{"slot":1,"item":"arrow","count":40}]},"gear":{"slots":[{"slot":"head","item":"iron-helm"}]}}
```

`Restore` 在替换任何内容之前先校验整个快照：未知道具、数量超过 `maxStack`、格子越界或重复、装备格类别不符，都会返回错误并保持容器或装备原样。更新中重命名或删除道具时，先用 `save.Migration` 迁移存档再恢复。

## 错误

| 错误 | 含义 |
| --- | --- |
| `ErrUnknownItem` | 目录中没有该道具。 |
| `ErrInvalidCount` | 数量不是正数，或超过堆叠数量或道具的 `maxStack`。 |
| `ErrSlotOutOfRange` | 容器格子索引不在 `[0, Len)` 内。 |
| `ErrEmptySlot` | 格子为空。 |
| `ErrSlotOccupied` | 目标格放着不同道具，或拆分时空间不足。 |
| `ErrFull` | 容器放不下全部道具。 |
| `ErrNotEnough` | 容器中的该道具少于所需数量。 |
| `ErrCatalogMismatch` | 容器或装备使用的目录不同。 |
| `ErrUnknownEquipSlot` | 装备中没有该名称的格子。 |
| `ErrNotEquippable` | 道具类别与装备格不符。 |

容器与装备是确定性的，不持有全局状态，同样的代码可以在无头[服务器](gameserver.md)上运行。它们不支持并发使用。

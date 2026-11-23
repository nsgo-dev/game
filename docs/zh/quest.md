# quest

`game/quest` 运行带目标（收集、到达、击败、对话）、任务状态与奖励的任务。任务定义在带版本号的 YAML 任务书中，由发布在 `flow.Bus` 上的游戏事件推进，并保存为一份可以经 `save.Store` 无损往返的小快照。

```go
import "github.com/nsgo-dev/game/packages/game/quest"
```

## 任务书

策划在一个 YAML 文件中写下所有任务：

```yaml
version: 1
quests:
  - id: welcome
    title: quest.welcome.title
    autoStart: true
    objectives:
      - id: meet-elder
        kind: talk
        target: elder
  - id: rat-problem
    title: quest.rats.title
    requires: [welcome]
    ordered: true
    objectives:
      - id: talk-innkeeper
        kind: talk
        target: innkeeper
      - id: find-cellar
        kind: reach
        target: cellar
      - id: kill-rats
        kind: defeat
        target: rat
        count: 5
      - id: tails
        kind: collect
        target: rat-tail
        count: 3
        optional: true
      - id: report
        kind: talk
        target: innkeeper
    rewards:
      - kind: gold
        id: coin
        amount: 50
      - kind: item
        id: cellar-key
```

| 字段 | 含义 |
| --- | --- |
| `version` | 结构版本号，为 `1`（`quest.FormatVersion`） |
| `id` | 任务 ID，在任务书内唯一，存档中也用它 |
| `title`、`description` | 显示文本或本地化键；本包不读取 |
| `requires` | 本任务开始前必须完成的任务 |
| `autoStart` | 前置任务全部完成后立即开始 |
| `repeatable` | 任务完成后允许再次 `Start` |
| `ordered` | 目标按列表顺序逐个推进 |
| `objectives` | 至少一个；所有非可选目标完成时任务完成 |
| `rewards` | 完成时报告给游戏，由游戏发放 |

每个目标包含 `id`（在所属任务内唯一）、`kind`（`collect`、`reach`、`defeat` 或 `talk`）、`target`（事件必须指明的物品、地点、敌人或角色 ID），以及可选的 `count`（默认 1）、`optional` 和 `text`。奖励包含可选的 `kind`（如 `item`、`xp` 或 `gold`）、`id` 和 `amount`（默认 1）。本包只报告奖励，因此奖励可以指向游戏定义的任何东西，包括背包物品或属性。

`quest.Parse` 与 `quest.LoadFS` 会拒绝未知字段、错误的版本号、重复或为空的 ID、未知的目标类型、负数计数、指向不存在任务的前置条件以及前置条件环；所有错误都包装 `quest.ErrInvalidBook`。`quest.Encode` 以规范形式写回任务书：2 空格缩进的 YAML，字段顺序固定，`requires` 用流式写法、其余用块式写法，省略空列表和默认值，不含生成值。像上面示例那样已是规范形式的任务书，加载后再保存不会改变任何内容，而且 `quest.Parse(quest.Encode(book))` 总是等于解析得到的任务书。其他形式的文件会被写成规范形式，YAML 注释不会保留，因此给设计者看的说明请写在 `description` 或 `text` 里。

## 运行任务

`quest.NewLog` 跟踪一名玩家在任务书中的进度。把它挂到游戏的事件总线上，在事件发生的地方发布事件即可：

```go
book, err := quest.LoadFS(assets, "quests.yaml")
if err != nil {
	return err
}
log, err := quest.NewLog(book)
if err != nil {
	return err
}
log.OnUpdate = func(u quest.Update) {
	if u.Kind == quest.QuestCompleted {
		for _, r := range u.Rewards {
			grant(r.Kind, r.ID, r.Quantity())
		}
	}
}
detach := log.Attach(&bus)
defer detach()
log.StartReady() // 开始没有前置条件的 autoStart 任务

flow.Publish(&bus, quest.Talked{Character: "elder"})
flow.Publish(&bus, quest.Defeated{Enemy: "rat", Amount: 2})
flow.Publish(&bus, quest.Collected{Item: "rat-tail"})
flow.Publish(&bus, quest.Reached{Place: "cellar"})
```

`Collected`、`Reached`、`Defeated` 和 `Talked` 是 `quest.Event{Kind, Target, Amount}` 的简写；`Amount` 小于 1 时按 1 计。不使用总线时，直接调用 `log.Handle(event)`。

任务有五种状态：

| 状态 | 含义 |
| --- | --- |
| `Inactive` | 尚未开始 |
| `Active` | 目标接收事件 |
| `Completed` | 所有必需目标都已完成 |
| `Failed` | 游戏调用了 `Fail`；可用 `Start` 重试 |
| `Abandoned` | 玩家调用了 `Abandon`；可用 `Start` 重新接取 |

- `Start(id)` 以零进度开始任务。前置任务未完成、任务正在进行，或任务已完成且不是 `repeatable` 时，返回 `ErrNotStartable`。
- `StartReady()` 开始所有从未开始过、且前置条件已满足的 `autoStart` 任务。每次任务完成后 Log 都会调用它，因此任务链会自动衔接。
- `Fail(id)` 和 `Abandon(id)` 结束进行中的任务；放弃会清空进度。`StartReady` 不会重新开始这两种状态的任务，因此玩家放弃的 `autoStart` 任务会一直保持放弃状态（存档读档后也一样），直到游戏调用 `Start`。
- `SetProgress(quest, objective, n)` 同步游戏已经在跟踪的数值，例如背包中某物品的数量。
- `State`、`Quests(state)`、`Objectives(id)` 和 `Objective(quest, objective)` 供任务日志或 HUD 使用。目标处于 `ObjectiveLocked`、`ObjectiveActive` 或 `ObjectiveDone` 之一。

在 `ordered` 任务中，只有第一个未完成的必需目标（以及它之前的可选目标）接收事件，而且完成某个目标的那个事件不会同时计入下一个目标，所以和旅店老板对话一次不会顺带完成最后的汇报。某个事件完成任务后自动开始的任务也是如此。

每次变化都以 `Update`（`QuestStarted`、`ObjectiveProgressed`、`ObjectiveCompleted`、`QuestCompleted`、`QuestFailed`、`QuestAbandoned`）报告，交给 `OnUpdate`，挂到总线期间还会发布到总线上。处理函数可以回调 Log，例如开始后续任务；这样产生的更新会在当前更新之后按顺序送达。

## 保存进度

`log.Snapshot()` 以纯数据返回进度：按任务书顺序列出所有非 Inactive 的任务，包括其状态和有进度的目标。把它和游戏的其他状态一起存入存档槽：

```go
type gameSave struct {
	Gold   int            `json:"gold"`
	Quests quest.Snapshot `json:"quests"`
}

err := store.Save("slot1", gameSave{Gold: gold, Quests: log.Snapshot()})
```

写出的 JSON 是稳定的，例如：

```json
{"version":1,"quests":[{"id":"welcome","state":"completed","progress":{"meet-elder":1}},{"id":"herbs","state":"active","progress":{"moonleaf":3,"sunroot":1}}]}
```

读档时，用同一份任务书创建 Log 再恢复：

```go
var s gameSave
if err := store.Load("slot1", &s); err != nil {
	return err
}
if err := log.Restore(s.Quests); err != nil {
	return err
}
log.StartReady()
```

`Restore` 在改动任何状态前先检查整个快照，并且不发送任何更新。它会拒绝（返回 `ErrInvalidSnapshot`）错误的版本号、任务书中已不存在的任务或目标、超出目标范围的进度，以及 Log 不可能达到的进度：前置任务不在快照中（或前置任务不是 `repeatable` 却未完成）的任务、带有进度的已放弃任务、`ordered` 任务中越过未完成必需目标的进度，以及必需目标已全部完成却仍为 Active 的任务。内容更新删除或重命名了任务或目标、调低了计数或新增了前置条件时，注册一个 `save.Migration` 改写旧快照。

在游戏开始记录任务之前写下的存档槽，缺失的字段会解码为零值 `quest.Snapshot`；`Restore` 把它当作没有任何进度，把所有任务重置为 `Inactive`。

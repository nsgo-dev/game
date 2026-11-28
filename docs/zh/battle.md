# battle

`github.com/nsgo-dev/game/pkg/battle` 解析队伍与敌群之间的回合制战斗。它不负责表现：每个回合返回一组事件，由游戏按自己的节奏播放动画；所有随机判定都从 [`random.Stream`](determinism.md) 取数，所以同一个种子和同样的指令在所有平台上都会重现同一场战斗。

## 一个回合

```go
r := battle.New(
	[]battle.Fighter{{ID: "hero", HP: 30, MaxHP: 30, MP: 6, Attack: 5, Agility: 7}},
	[]battle.Fighter{{ID: "slime", HP: 12, Defense: 1, Attack: 3, Agility: 4}},
	battle.Rules{CriticalChance: 0.1},
	random.New(seed),
)
slash := battle.Action{ID: "slash", Scope: battle.OneEnemy, Power: 4, Accuracy: 0.9}
bite := battle.Action{ID: "bite", Scope: battle.OneEnemy, Power: 2}
events := r.Resolve([]battle.Command{
	{Actor: battle.Ref{Side: battle.PartySide, Index: 0}, Action: slash, Target: 0},
	{Actor: battle.Ref{Side: battle.TroopSide, Index: 0}, Action: bite, Target: 0},
})
```

每个战斗者的指令都由游戏选择，敌群的也一样。`Resolve` 按 `Agility` 从高到低执行；速度相同时队伍先于敌群，再按下标从小到大，与切片里的顺序无关。指向不存在的战斗者的指令会被丢弃，本回合中先被击倒的战斗者不再行动。

## 行动与范围

| 范围 | 目标 | 效果 |
| --- | --- | --- |
| `Self` | 行动者自己 | 回复 `Power` |
| `OneAlly` | `Target` 处存活的友方 | 回复 `Power` |
| `AllAllies` | 所有存活的友方 | 回复 `Power` |
| `OneEnemy` | `Target` 处存活的对手 | 造成伤害 |
| `AllEnemies` | 所有存活的对手 | 造成伤害 |

伤害为 `Power + Attack - Defense`，至少为 1。敌对行动对每个目标先按 `Accuracy` 判定命中（0 和 1 都表示必中，不消耗随机数），再按 `Rules.CriticalChance` 判定暴击，暴击伤害翻倍。设置了 `MaxHP` 时，回复不会让 HP 超过它。带 `Cost` 的行动先消耗相应的 MP；MP 不足时只产生一个 `NoMP` 事件，不产生其他效果。

## 事件与结果

`Resolve` 返回 `Missed`、`Damaged`、`Healed`、`Defeated` 和 `NoMP` 事件，带有行动者与受影响的战斗者，`Amount` 是实际失去或回复的 HP。敌群最后一名成员倒下时追加 `Won` 并停止；队伍最后一名成员倒下时追加 `Lost` 并停止。此后 `Resolve` 返回 nil。

## 存档与回放

`Runner.Snapshot` 复制战斗者、回合计数、结果和随机流状态，可以序列化为 JSON 写入存档。`Runner.Restore` 恢复快照，之后同样的指令会产生与原战斗相同的事件。

## 尚未覆盖

这个运行器是 #1757 的第一部分。它还没有时间进度（gauge）模式、状态、属性倍率或自定义伤害公式、道具、逃跑、敌人 AI、敌群事件和奖励，rpg2d 模板也还没有使用它。

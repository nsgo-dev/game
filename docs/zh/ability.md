# ability

`github.com/nsgo-dev/game/pkg/ability` 运行玩法技能：消耗、冷却、充能、施法与引导时间以及条件，由 [input](input.md) 动作触发，并按 [`game.Loop`](loop.md) 的固定步长逐步推进。

```go
import "github.com/nsgo-dev/game/pkg/ability"
```

## 技能集

技能定义在带版本号的 YAML 文件中：

```yaml
version: 1
abilities:
  - id: fireball
    name: ability.fireball
    action: cast
    costs:
      - resource: mana
        amount: 20
    cooldown: 2s
    castTime: 500ms
    conditions: [grounded, '!silenced']
  - id: dash
    action: dash
    cooldown: 250ms
    charges: 3
    recharge: 4s
  - id: drain
    action: drain
    trigger: hold
    costs:
      - resource: mana
        amount: 5
    cooldown: 6s
    channelTime: 3s
    tickInterval: 500ms
```

| 字段 | 含义 |
| --- | --- |
| `id` | 在技能集内唯一：ASCII 字母、数字、`_`、`-` 和 `.`，最多 64 字节 |
| `name` | 可选的显示名称或本地化键 |
| `action` | 触发它的输入动作；为空表示只能由代码通过 `TryActivate` 触发 |
| `trigger` | `press`（默认）在动作刚按下的那一步尝试；`hold` 在按住的每一步都尝试，松开时取消施法或引导 |
| `costs` | 每次使用消耗的资源，每种资源只出现一次，`amount` 为正数 |
| `cooldown` | 两次使用之间的最短间隔，从技能生效的那一步开始计；即使还有充能也会生效 |
| `charges` | 可储存的使用次数，省略时为 1，最多 99；多于 1 时需要 `recharge` |
| `recharge` | 恢复一次已用充能所需的时间；已用的充能逐个恢复 |
| `castTime` | 技能生效前的施法时间 |
| `channelTime` | 技能生效后的引导时间 |
| `tickInterval` | 引导的跳数间隔；需要 `channelTime` 且不能超过它 |
| `conditions` | 开始以及持续施法或引导时必须全部成立的条件名；`!name` 表示该条件必须不成立 |

时长使用 Go 时长字符串（`500ms`、`1m30s`），范围 0 到 24h。`ability.Parse` 拒绝未知字段、重复键与重复 ID、其他版本和越界值；`ability.Encode` 按固定顺序写出字段，时长写成规范形式（`1500ms` 写为 `1.5s`），`Parse` 读回其输出得到相同的 `Set`。

## Caster

`Caster` 运行一个角色的技能。用技能集和循环的速率创建它，然后在每一步推进输入之后调用一次 `Update`：

```go
set, err := ability.Parse(data)
if err != nil {
	return err
}
hero, err := ability.NewCaster(set, 60) // 与 game.Loop 的 Rate 相同
if err != nil {
	return err
}
hero.Resources = ability.Pool{"mana": 100, "stamina": 50}
hero.Condition = func(name string) bool {
	switch name {
	case "grounded":
		return player.OnGround
	case "silenced":
		return player.Silenced
	}
	return false
}
hero.OnEvent = func(e ability.Event) {
	if e.Kind == ability.EventActivated && e.Ability == "fireball" {
		spawnFireball()
	}
}

loop := &game.Loop{Rate: 60, Update: func(dt time.Duration) {
	actions.Step()
	hero.Update(actions)
}}
```

`Update` 接受任何带 `Pressed` 和 `JustPressed` 的输入，例如 `*input.Actions`、回放或网络输入；传 `nil` 时只推进计时器、不读取输入。代码用 `TryActivate(id)` 触发技能，它返回 `ReasonOK` 或未能触发的原因：`ReasonUnknown`、`ReasonBusy`、`ReasonCooldown`、`ReasonNoCharges`、`ReasonCondition` 或 `ReasonCost`，按此顺序检查。`CanActivate(id)` 返回同样的原因但不改变任何状态，可用于把按钮置灰。

同一时间最多只有一个技能在施法或引导。它的过程是：

1. 触发时检查冷却、充能、条件和消耗，并发送 `EventStarted`。
2. 施法持续 `castTime`，此时还不消耗任何东西。条件不再成立会打断它（`EventInterrupted`，原因为 `ReasonCondition`），`Cancel` 会停止它（`EventCanceled`），两者都不消耗任何东西。
3. 施法完成时扣除消耗和一次充能，开始冷却，并发送 `EventActivated`；游戏在这里应用技能效果。若此时消耗已不够，则改为以 `ReasonCost` 打断。没有施法时间的技能在触发的那一步完成这一切。
4. 随后引导持续 `channelTime`，每隔 `tickInterval` 发送 `EventChannelTick`，结束时发送 `EventChannelEnded`。条件不成立、`Cancel` 以及松开 `hold` 动作会提前停止它。

`EventCooldownEnded` 和 `EventChargeRestored` 报告计时结束。`OnEvent` 中可以调用 `TryActivate` 或 `Cancel`，例如在技能生效时衔接后续技能；由此产生的事件排在当前事件之后。在 `OnEvent` 中调用 `Reset` 或 `Restore` 时，调用之前已经发生的事件仍会送达；若该调用发生在 `Update` 期间，`Update` 随后会按重置或恢复后的状态读取输入。

`Status(id)` 以步数报告充能、剩余冷却与恢复时间以及施法或引导进度，`Duration(steps)` 把步数换算成游戏时间，用于冷却条。`Active()` 报告当前正在施法或引导的技能。

### 消耗与条件

`Resources` 是带 `Amount` 和 `Spend` 的接口，因此消耗可以直接使用游戏自己的属性。`ability.Pool` 是现成的基于 map 的实现。一次使用的所有消耗都先检查，再统一扣除。`Resources` 为 nil 时，有消耗的技能无法触发；`Condition` 为 nil 时，所有条件都视为不成立。

## 确定性

`NewCaster` 按其速率把每个时长换算为整数步（向上取整），所有计时器都按整数步计数。60 Hz 时 `game.Loop` 的 `dt` 是 16,666,666 ns，累加它要 121 步才达到 2 s；而 `Caster` 中 2 s 的冷却恰好是 120 步。在一步之内，`Update` 先按技能集顺序推进所有冷却和充能，再推进当前的施法或引导，最后读取输入，因此冷却在某一步结束的技能在同一步就能再次使用。多个技能共用一个动作时，按技能集顺序尝试，第一个成功触发的生效，这样可以用条件在它们之间选择。

因此相同步上的相同输入在任何机器上都会在相同步产生相同事件。`Snapshot` 返回一个 `State`（以步数记录，带 JSON 和 YAML 标签），用于存档、回滚和回放，`Restore` 将其恢复，并拒绝来自其他技能集或速率的状态。`Update` 不分配内存。

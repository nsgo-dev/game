# rpgdata

`game/rpgdata` 读写 RPG 的数据库：带等级曲线和习得技能的职业、角色、技能、敌人和敌群，存放在一个版本化的 YAML 文件中，加载时检查 ID 和交叉引用。`Progression` 记录角色的等级和经验。

```go
import "github.com/nsgo-dev/game/pkg/rpgdata"
```

本包只提供记录和升级规则。战斗、技能公式和作用范围由游戏自行解释，rpg2d 和 roguelike2d 模板目前还没有使用它（[#1756](https://github.com/nsgo-dev/game/issues/1756)）。

## 加载数据库

```go
data, err := os.ReadFile("data/rpg.yaml")
if err != nil {
	return err
}
db, err := rpgdata.Parse(data)
if err != nil {
	return loaderr.WithFile(err, "data/rpg.yaml")
}
```

`Parse` 拒绝未知字段、不等于 `rpgdata.CurrentVersion` 的版本、第二个 YAML 文档以及 `Validate` 拒绝的任何记录，返回的 `*loaderr.Error` 指明字段及其所在行，如 `actors[0].class`。`Encode` 先校验，再写回与加载时字节相同的文件。

`Validate` 检查：

- ID 非空且在各自类别内唯一，因此角色和职业可以同名；
- 每个职业技能都指向一个技能，每个角色指向一个职业，每个敌群成员指向一个敌人；
- 角色的等级和技能的习得等级不小于 1，职业有 `maxLevel` 时不超过它；
- 每条属性曲线至少有一个值，敌人的 `hp` 不小于 1，消耗、经验和金币不为负；
- 每个敌群至少有一个成员。

## 等级和属性

职业的经验规则是线性的：从 n 级升到 n+1 级需要 n × `levelXP` 经验。`Progression` 按此计算：

```go
hero := db.Classes[0]
p := rpgdata.NewProgression(1, hero.MaxLevel, hero.LevelXP)
for _, level := range p.AddExperience(250) {
	for _, id := range hero.SkillsLearnedAt(level) {
		learn(id)
	}
}
attack := hero.StatAt("attack", p.Level)
```

`AddExperience` 按顺序返回达到的每个等级，剩余经验计入下一级；达到 `maxLevel` 后不再升级。`NeedForNext` 是下一级所需的总经验，达到上限或 `levelXP` 为 0 时为 0。`StatAt` 读取属性曲线，第一个值对应 1 级，超出曲线长度的等级沿用最后一个值。

## 文件格式

[文件格式](formats.md#rpg-数据库)中的条目给出完整示例。

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `version` | `1` | 模式版本 |
| `classes[].id` | string | 在职业中唯一 |
| `classes[].name` | string，可选 | 显示名称或本地化键 |
| `classes[].maxLevel` | int，可选 | 等级上限，0 或省略表示无上限 |
| `classes[].levelXP` | int，可选 | 升级规则的经验系数，0 表示不升级 |
| `classes[].stats` | 属性名到 int 列表的映射，可选 | 该属性在 1 级、2 级及之后的值 |
| `classes[].skills[].id` | string | `skills` 中列出的技能 |
| `classes[].skills[].level` | int | 习得该技能的等级 |
| `actors[].id` | string | 在角色中唯一 |
| `actors[].name` | string | 显示名称或本地化键 |
| `actors[].class` | string | `classes` 中列出的职业 |
| `actors[].level` | int，可选 | 初始等级，省略时为 1 |
| `skills[].id` | string | 在技能中唯一 |
| `skills[].name` | string | 显示名称或本地化键 |
| `skills[].scope` | string | 作用对象，如 `self`、`one_enemy` 或 `all_allies` |
| `skills[].formula` | string，可选 | 伤害或治疗公式，如 `a.attack * 2 - b.defense` |
| `skills[].cost` | int，可选 | 使用的消耗 |
| `enemies[].id` | string | 在敌人中唯一 |
| `enemies[].name` | string | 显示名称或本地化键 |
| `enemies[].hp` | int | 生命值，不小于 1 |
| `enemies[].experience` | int，可选 | 击败后获得的经验 |
| `enemies[].gold` | int，可选 | 击败后获得的金币 |
| `troops[].id` | string | 在敌群中唯一 |
| `troops[].members[].enemy` | string | `enemies` 中列出的敌人 |
| `troops[].members[].x`、`y` | int | 位置 |

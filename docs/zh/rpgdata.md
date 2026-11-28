# RPG 数据库

`github.com/nsgo-dev/game/pkg/rpgdata` 提供供模板和游戏共享的版本化 YAML RPG 数据库。数据模型与引擎无关，因此动作 RPG 或 Roguelike 可以使用职业和成长系统，而无需采用回合制战斗运行器。

## 记录

`Database` 包含 `Class`、`Actor`、`Skill`、`Enemy` 和 `Troop` 记录。职业保存升级经验、逐级属性曲线以及按等级学习的技能。角色引用职业，技能声明目标范围和可选公式与消耗，敌人保存生命、经验和金币，队伍把敌人引用放在坐标上。

`Parse` 使用严格 YAML 字段，并校验版本、ID、职业引用和队伍敌人引用。`Encode` 会先校验再写出确定性的 YAML。在编辑器数据表集成完成前，可以在项目数据目录中保存一个带 `version: 1` 的文档。

## 成长

`NewProgression` 创建等级和经验值。`AddExperience` 按顺序返回达到的每个等级，逐级消耗所需经验，并在 `MaxLevel` 停止。`Class.StatAt` 会把请求等级限制在已有曲线条目范围内。

rpg2d 与 roguelike2d 模板在后续接入共享格式前仍保留现有记录。战斗执行、状态效果和编辑器控件属于 RPG issue 的后续工作。

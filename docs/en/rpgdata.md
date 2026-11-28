# rpgdata

`game/rpgdata` reads and writes an RPG's database: classes with level curves and learned skills, actors, skills, enemies and troops, in one versioned YAML file whose IDs and cross-references are checked on load. `Progression` tracks a character's level and experience.

```go
import "github.com/nsgo-dev/game/pkg/rpgdata"
```

The package holds the records and the level rule. Battles, skill formulas and scopes are the game's to interpret, and the rpg2d and roguelike2d templates do not use it yet ([#1756](https://github.com/nsgo-dev/game/issues/1756)).

## Loading a database

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

`Parse` refuses unknown fields, a version other than `rpgdata.CurrentVersion`, a second YAML document and any record `Validate` refuses, with a `*loaderr.Error` that names the field and its line, such as `actors[0].class`. `Encode` validates and writes the file back with the same bytes it was loaded from.

`Validate` checks that:

- IDs are non-empty and unique within each category, so an actor and a class may share one;
- every class skill names a skill, every actor a class and every troop member an enemy;
- an actor's level and a learned skill's level are 1 or more and at most the class's `maxLevel` when it has one;
- each stat curve has at least one value, enemies have 1 or more `hp`, and costs, experience and gold are not negative;
- each troop has at least one member.

## Levels and stats

A class's experience rule is linear: going from level n to n+1 takes n × `levelXP`. `Progression` applies it:

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

`AddExperience` returns each level reached in order and keeps the experience left over toward the next level; at `maxLevel` it no longer levels up. `NeedForNext` is the total the next level takes, 0 at the cap or when `levelXP` is 0. `StatAt` reads a stat curve, whose first value is level 1; levels past its end keep its last value.

## File format

The format's entry on [File formats](formats.md#rpg-database) shows a full example.

| Field | Type | Meaning |
| --- | --- | --- |
| `version` | `1` | Schema version |
| `classes[].id` | string | Unique among classes |
| `classes[].name` | string, optional | Display name or localization key |
| `classes[].maxLevel` | int, optional | Level cap, 0 or absent for none |
| `classes[].levelXP` | int, optional | Experience scale of the level rule, 0 for none |
| `classes[].stats` | map of stat name to list of int, optional | The stat's value at level 1, 2 and onward |
| `classes[].skills[].id` | string | A skill listed under `skills` |
| `classes[].skills[].level` | int | Level the skill is learned at |
| `actors[].id` | string | Unique among actors |
| `actors[].name` | string | Display name or localization key |
| `actors[].class` | string | A class listed under `classes` |
| `actors[].level` | int, optional | Initial level, 1 when absent |
| `skills[].id` | string | Unique among skills |
| `skills[].name` | string | Display name or localization key |
| `skills[].scope` | string | Who it targets, such as `self`, `one_enemy` or `all_allies` |
| `skills[].formula` | string, optional | Damage or healing formula, such as `a.attack * 2 - b.defense` |
| `skills[].cost` | int, optional | What using it costs |
| `enemies[].id` | string | Unique among enemies |
| `enemies[].name` | string | Display name or localization key |
| `enemies[].hp` | int | Hit points, 1 or more |
| `enemies[].experience` | int, optional | Experience it gives |
| `enemies[].gold` | int, optional | Gold it gives |
| `troops[].id` | string | Unique among troops |
| `troops[].members[].enemy` | string | An enemy listed under `enemies` |
| `troops[].members[].x`, `y` | int | Position |

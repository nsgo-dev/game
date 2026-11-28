// Package rpgdata reads and writes the versioned YAML database an RPG's records live in: classes with level curves and learned skills, actors, skills, enemies and troops, with every ID and cross-reference checked on load. Progression tracks a character's level and experience against a class's linear experience rule.
package rpgdata

import (
	"bytes"
	"fmt"
	"io"
	"slices"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// CurrentVersion is the schema version Parse reads and Encode writes.
const CurrentVersion = 1

// fixDatabase is the fix a load error gets when no more specific one applies.
const fixDatabase = "write the RPG database as version: 1 YAML with classes, actors, skills, enemies and troops"

// Database is one RPG database file. IDs are unique within each category, and every reference names a record of the category it points at.
type Database struct {
	// Version is the schema version, CurrentVersion.
	Version int `yaml:"version"`
	// Classes are the classes actors belong to.
	Classes []Class `yaml:"classes,omitempty"`
	// Actors are the playable characters.
	Actors []Actor `yaml:"actors,omitempty"`
	// Skills are the skills classes teach.
	Skills []Skill `yaml:"skills,omitempty"`
	// Enemies are the foes troops are made of.
	Enemies []Enemy `yaml:"enemies,omitempty"`
	// Troops are the groups of enemies a battle starts with.
	Troops []Troop `yaml:"troops,omitempty"`
}

// Class is a class: its level cap, experience rule, stat curves and the skills it teaches.
type Class struct {
	// ID identifies the class among the database's classes.
	ID string `yaml:"id"`
	// Name is the display name or its localization key.
	Name string `yaml:"name,omitempty"`
	// MaxLevel is the highest level, or 0 for no cap.
	MaxLevel int `yaml:"maxLevel,omitempty"`
	// LevelXP scales the experience rule: going from level n to n+1 takes n × LevelXP experience. 0 means the class never levels up from experience.
	LevelXP int `yaml:"levelXP,omitempty"`
	// Stats maps a stat name to its value at each level, the first entry being level 1; levels past the end keep the last value.
	Stats map[string][]int `yaml:"stats,omitempty"`
	// Skills are the skills the class learns and the level each is learned at.
	Skills []LearnedSkill `yaml:"skills,omitempty"`
}

// LearnedSkill is a skill a class learns on reaching a level.
type LearnedSkill struct {
	// ID names a skill of the database.
	ID string `yaml:"id"`
	// Level is the level the skill is learned at, 1 or more.
	Level int `yaml:"level"`
}

// Actor is a playable character.
type Actor struct {
	// ID identifies the actor among the database's actors.
	ID string `yaml:"id"`
	// Name is the display name or its localization key.
	Name string `yaml:"name"`
	// Class names the actor's class.
	Class string `yaml:"class"`
	// Level is the initial level, 1 when 0, and at most the class's MaxLevel when it has one.
	Level int `yaml:"level,omitempty"`
}

// Skill is a skill's record. Scope and Formula are free-form text for the game's battle rules to interpret.
type Skill struct {
	// ID identifies the skill among the database's skills.
	ID string `yaml:"id"`
	// Name is the display name or its localization key.
	Name string `yaml:"name"`
	// Scope is who the skill targets, such as self, one_enemy or all_allies.
	Scope string `yaml:"scope"`
	// Formula is the damage or healing formula, such as a.attack * 2 - b.defense.
	Formula string `yaml:"formula,omitempty"`
	// Cost is what using the skill costs, 0 or more.
	Cost int `yaml:"cost,omitempty"`
}

// Enemy is a foe's record.
type Enemy struct {
	// ID identifies the enemy among the database's enemies.
	ID string `yaml:"id"`
	// Name is the display name or its localization key.
	Name string `yaml:"name"`
	// HP is the enemy's hit points, 1 or more.
	HP int `yaml:"hp"`
	// Experience is the experience defeating it gives, 0 or more.
	Experience int `yaml:"experience,omitempty"`
	// Gold is the gold defeating it gives, 0 or more.
	Gold int `yaml:"gold,omitempty"`
}

// Troop is a group of enemies a battle starts with.
type Troop struct {
	// ID identifies the troop among the database's troops.
	ID string `yaml:"id"`
	// Members are the troop's enemies and where they stand; a troop has at least one.
	Members []TroopMember `yaml:"members"`
}

// TroopMember is one enemy in a troop and its position.
type TroopMember struct {
	// Enemy names an enemy of the database.
	Enemy string `yaml:"enemy"`
	// X is the member's horizontal position.
	X int `yaml:"x"`
	// Y is the member's vertical position.
	Y int `yaml:"y"`
}

// Parse decodes and validates one database document. It refuses unknown fields, a version other than CurrentVersion, a trailing document and any record Validate refuses, returning a *loaderr.Error with the field and line of the problem.
func Parse(data []byte) (*Database, error) {
	d, err := parse(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixDatabase)
	}
	return d, nil
}

func parse(data []byte) (*Database, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", fixDatabase, "rpgdata: empty database file")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var d Database
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("rpgdata: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep one YAML document in the RPG database file; remove the trailing --- document", "rpgdata: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("rpgdata: decode: %w", err)
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

// Encode validates d and writes it as YAML with 2-space indentation: fields in declaration order, lists in their given order and stat names sorted, so Parse then Encode reproduces an encoded file byte for byte.
func Encode(d *Database) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(d); err != nil {
		return nil, fmt.Errorf("rpgdata: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("rpgdata: encode: %w", err)
	}
	return b.Bytes(), nil
}

// Validate checks the version, that IDs are non-empty and unique within each category, that numbers are in range and that every class skill, actor class and troop member names a record that exists. Its errors are *loaderr.Error values naming the field at fault.
func (d *Database) Validate() error {
	if d == nil {
		return loaderr.Errorf("", fixDatabase, "rpgdata: nil database")
	}
	if d.Version != CurrentVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version to %d", CurrentVersion), "rpgdata: unsupported version %d (want %d)", d.Version, CurrentVersion)
	}
	skills := map[string]bool{}
	for i, s := range d.Skills {
		at := loaderr.Path("skills", i)
		if err := uniqueID(at, "skill", s.ID, skills); err != nil {
			return err
		}
		if s.Scope == "" {
			return loaderr.Errorf(loaderr.Join(at, "scope"), "give the skill a scope, such as one_enemy", "rpgdata: skill %q has no scope", s.ID)
		}
		if s.Cost < 0 {
			return loaderr.Errorf(loaderr.Join(at, "cost"), "set cost to 0 or more", "rpgdata: skill %q has negative cost %d", s.ID, s.Cost)
		}
	}
	classes := map[string]Class{}
	classIDs := map[string]bool{}
	for i, c := range d.Classes {
		at := loaderr.Path("classes", i)
		if err := uniqueID(at, "class", c.ID, classIDs); err != nil {
			return err
		}
		classes[c.ID] = c
		if c.MaxLevel < 0 {
			return loaderr.Errorf(loaderr.Join(at, "maxLevel"), "set maxLevel to 0 for no cap or a positive level", "rpgdata: class %q has negative maxLevel %d", c.ID, c.MaxLevel)
		}
		if c.LevelXP < 0 {
			return loaderr.Errorf(loaderr.Join(at, "levelXP"), "set levelXP to 0 or more", "rpgdata: class %q has negative levelXP %d", c.ID, c.LevelXP)
		}
		names := make([]string, 0, len(c.Stats))
		for name := range c.Stats {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			if name == "" {
				return loaderr.Errorf(loaderr.Join(at, "stats"), "give each stat curve a name", "rpgdata: class %q has a stat curve without a name", c.ID)
			}
			if len(c.Stats[name]) == 0 {
				return loaderr.Errorf(loaderr.Join(at, "stats", name), "list the stat's value at level 1 and onward", "rpgdata: class %q stat %q has no values", c.ID, name)
			}
		}
		for j, s := range c.Skills {
			sat := loaderr.Join(at, "skills", j)
			if !skills[s.ID] {
				return loaderr.Errorf(loaderr.Join(sat, "id"), "name a skill listed under skills", "rpgdata: class %q learns unknown skill %q", c.ID, s.ID)
			}
			if s.Level < 1 || c.MaxLevel > 0 && s.Level > c.MaxLevel {
				return loaderr.Errorf(loaderr.Join(sat, "level"), "set level between 1 and the class's maxLevel", "rpgdata: class %q learns skill %q at invalid level %d", c.ID, s.ID, s.Level)
			}
		}
	}
	actors := map[string]bool{}
	for i, a := range d.Actors {
		at := loaderr.Path("actors", i)
		if err := uniqueID(at, "actor", a.ID, actors); err != nil {
			return err
		}
		c, ok := classes[a.Class]
		if !ok {
			return loaderr.Errorf(loaderr.Join(at, "class"), "name a class listed under classes", "rpgdata: actor %q references unknown class %q", a.ID, a.Class)
		}
		if a.Level < 0 || c.MaxLevel > 0 && a.Level > c.MaxLevel {
			return loaderr.Errorf(loaderr.Join(at, "level"), "set level between 1 and the class's maxLevel, or omit it for 1", "rpgdata: actor %q has invalid level %d", a.ID, a.Level)
		}
	}
	enemies := map[string]bool{}
	for i, e := range d.Enemies {
		at := loaderr.Path("enemies", i)
		if err := uniqueID(at, "enemy", e.ID, enemies); err != nil {
			return err
		}
		if e.HP < 1 {
			return loaderr.Errorf(loaderr.Join(at, "hp"), "set hp to 1 or more", "rpgdata: enemy %q has non-positive hp %d", e.ID, e.HP)
		}
		if e.Experience < 0 {
			return loaderr.Errorf(loaderr.Join(at, "experience"), "set experience to 0 or more", "rpgdata: enemy %q has negative experience %d", e.ID, e.Experience)
		}
		if e.Gold < 0 {
			return loaderr.Errorf(loaderr.Join(at, "gold"), "set gold to 0 or more", "rpgdata: enemy %q has negative gold %d", e.ID, e.Gold)
		}
	}
	troops := map[string]bool{}
	for i, t := range d.Troops {
		at := loaderr.Path("troops", i)
		if err := uniqueID(at, "troop", t.ID, troops); err != nil {
			return err
		}
		if len(t.Members) == 0 {
			return loaderr.Errorf(loaderr.Join(at, "members"), "add at least one enemy to the troop", "rpgdata: troop %q has no members", t.ID)
		}
		for j, m := range t.Members {
			if !enemies[m.Enemy] {
				return loaderr.Errorf(loaderr.Join(at, "members", j, "enemy"), "name an enemy listed under enemies", "rpgdata: troop %q references unknown enemy %q", t.ID, m.Enemy)
			}
		}
	}
	return nil
}

// uniqueID checks that id, the ID of the record of kind at path at, is non-empty and not in seen, and adds it.
func uniqueID(at, kind, id string, seen map[string]bool) error {
	field := loaderr.Join(at, "id")
	if id == "" {
		return loaderr.Errorf(field, "give the "+kind+" a non-empty id", "rpgdata: %s has an empty id", kind)
	}
	if seen[id] {
		return loaderr.Errorf(field, "give each "+kind+" a unique id", "rpgdata: duplicate %s id %q", kind, id)
	}
	seen[id] = true
	return nil
}

// StatAt returns the class's value of the named stat at level: the curve's entry for that level, its first entry below level 1, its last entry past its end, and 0 for a stat the class has no curve for.
func (c Class) StatAt(name string, level int) int {
	values := c.Stats[name]
	if len(values) == 0 {
		return 0
	}
	level = max(level, 1)
	level = min(level, len(values))
	return values[level-1]
}

// SkillsLearnedAt returns the IDs of the skills the class learns on reaching level, in the order the class lists them.
func (c Class) SkillsLearnedAt(level int) []string {
	var out []string
	for _, s := range c.Skills {
		if s.Level == level {
			out = append(out, s.ID)
		}
	}
	return out
}

// Progression is a character's level and the experience gathered toward the next one. Going from level n to n+1 takes n × LevelXP experience; at MaxLevel, when it is above 0, experience no longer levels up.
type Progression struct {
	// Level is the current level, 1 or more.
	Level int `yaml:"level"`
	// Experience is the experience gathered since reaching Level.
	Experience int `yaml:"experience"`
	// MaxLevel is the level cap, or 0 for none.
	MaxLevel int `yaml:"maxLevel"`
	// LevelXP scales the experience each level takes; 0 or less never levels up.
	LevelXP int `yaml:"levelXP"`
}

// NewProgression returns a progression at level, raised to 1 and lowered to maxLevel when it has one, with no experience gathered.
func NewProgression(level, maxLevel, levelXP int) Progression {
	level = max(level, 1)
	if maxLevel > 0 {
		level = min(level, maxLevel)
	}
	return Progression{Level: level, MaxLevel: maxLevel, LevelXP: levelXP}
}

// NeedForNext returns the experience the next level takes in total, or 0 when the progression cannot level up any more.
func (p Progression) NeedForNext() int {
	if p.LevelXP <= 0 || p.MaxLevel > 0 && p.Level >= p.MaxLevel {
		return 0
	}
	return p.Level * p.LevelXP
}

// AddExperience adds xp, levels up as many times as it pays for and returns each level reached, in order. Experience left over after the last level up stays toward the next one; at the level cap it keeps accumulating without effect. A nil progression or an xp of 0 or less changes nothing.
func (p *Progression) AddExperience(xp int) []int {
	if p == nil || xp <= 0 {
		return nil
	}
	p.Experience += xp
	var levels []int
	for need := p.NeedForNext(); need > 0 && p.Experience >= need; need = p.NeedForNext() {
		p.Experience -= need
		p.Level++
		levels = append(levels, p.Level)
	}
	return levels
}

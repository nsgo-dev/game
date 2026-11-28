// Package rpgdata defines versioned YAML records shared by RPG templates.
package rpgdata

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"

	"go.yaml.in/yaml/v3"
)

const CurrentVersion = 1

type Database struct {
	Version int     `yaml:"version"`
	Classes []Class `yaml:"classes,omitempty"`
	Actors  []Actor `yaml:"actors,omitempty"`
	Skills  []Skill `yaml:"skills,omitempty"`
	Enemies []Enemy `yaml:"enemies,omitempty"`
	Troops  []Troop `yaml:"troops,omitempty"`
}
type Class struct {
	ID       string           `yaml:"id"`
	Name     string           `yaml:"name,omitempty"`
	MaxLevel int              `yaml:"maxLevel,omitempty"`
	LevelXP  int              `yaml:"levelXP,omitempty"`
	Stats    map[string][]int `yaml:"stats,omitempty"`
	Skills   []LearnedSkill   `yaml:"skills,omitempty"`
}
type LearnedSkill struct {
	ID    string `yaml:"id"`
	Level int    `yaml:"level"`
}
type Actor struct {
	ID    string `yaml:"id"`
	Name  string `yaml:"name"`
	Class string `yaml:"class"`
	Level int    `yaml:"level,omitempty"`
}
type Skill struct {
	ID      string `yaml:"id"`
	Name    string `yaml:"name"`
	Scope   string `yaml:"scope"`
	Formula string `yaml:"formula,omitempty"`
	Cost    int    `yaml:"cost,omitempty"`
}
type Enemy struct {
	ID         string `yaml:"id"`
	Name       string `yaml:"name"`
	HP         int    `yaml:"hp"`
	Experience int    `yaml:"experience,omitempty"`
	Gold       int    `yaml:"gold,omitempty"`
}
type Troop struct {
	ID      string        `yaml:"id"`
	Members []TroopMember `yaml:"members"`
}
type TroopMember struct {
	Enemy string `yaml:"enemy"`
	X     int    `yaml:"x"`
	Y     int    `yaml:"y"`
}

func Parse(data []byte) (*Database, error) {
	var d Database
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != nil {
		if err != io.EOF {
			return nil, errors.New("rpgdata: trailing YAML document")
		}
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}
func Encode(d *Database) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	e.SetIndent(2)
	if err := e.Encode(d); err != nil {
		return nil, err
	}
	e.Close()
	return b.Bytes(), nil
}
func (d *Database) Validate() error {
	if d == nil || d.Version != CurrentVersion {
		return errors.New("rpgdata: unsupported version")
	}
	ids := map[string]bool{}
	for _, c := range d.Classes {
		if err := validID(c.ID, ids); err != nil {
			return fmt.Errorf("class: %w", err)
		}
		if c.MaxLevel < 0 || c.LevelXP < 0 {
			return fmt.Errorf("class %q has invalid progression", c.ID)
		}
		for _, s := range c.Skills {
			if s.Level < 1 {
				return fmt.Errorf("class %q skill %q has invalid level", c.ID, s.ID)
			}
		}
	}
	classes := map[string]bool{}
	for _, c := range d.Classes {
		classes[c.ID] = true
	}
	for _, a := range d.Actors {
		if err := validID(a.ID, ids); err != nil {
			return fmt.Errorf("actor: %w", err)
		}
		if !classes[a.Class] {
			return fmt.Errorf("actor %q references unknown class %q", a.ID, a.Class)
		}
	}
	for _, s := range d.Skills {
		if err := validID(s.ID, ids); err != nil {
			return fmt.Errorf("skill: %w", err)
		}
		if s.Scope == "" {
			return fmt.Errorf("skill %q has no scope", s.ID)
		}
	}
	enemies := map[string]bool{}
	for _, e := range d.Enemies {
		if err := validID(e.ID, ids); err != nil {
			return fmt.Errorf("enemy: %w", err)
		}
		if e.HP < 1 {
			return fmt.Errorf("enemy %q has non-positive hp", e.ID)
		}
		enemies[e.ID] = true
	}
	for _, t := range d.Troops {
		if err := validID(t.ID, ids); err != nil {
			return fmt.Errorf("troop: %w", err)
		}
		for _, m := range t.Members {
			if !enemies[m.Enemy] {
				return fmt.Errorf("troop %q references unknown enemy %q", t.ID, m.Enemy)
			}
		}
	}
	return nil
}
func validID(id string, seen map[string]bool) error {
	if id == "" || seen[id] {
		return fmt.Errorf("duplicate or empty id %q", id)
	}
	seen[id] = true
	return nil
}

type Progression struct {
	Level      int `yaml:"level"`
	Experience int `yaml:"experience"`
	MaxLevel   int `yaml:"maxLevel"`
	LevelXP    int `yaml:"levelXP"`
}

func NewProgression(level, maxLevel, levelXP int) Progression {
	if level < 1 {
		level = 1
	}
	return Progression{Level: level, MaxLevel: maxLevel, LevelXP: levelXP}
}
func (p Progression) NeedForNext() int {
	if p.LevelXP <= 0 || p.Level >= p.MaxLevel && p.MaxLevel > 0 {
		return 0
	}
	return p.Level * p.LevelXP
}
func (p *Progression) AddExperience(xp int) []int {
	if p == nil || xp <= 0 {
		return nil
	}
	p.Experience += xp
	var levels []int
	for p.NeedForNext() > 0 && p.Experience >= p.NeedForNext() {
		p.Experience -= p.NeedForNext()
		p.Level++
		levels = append(levels, p.Level)
	}
	return levels
}
func (c Class) StatAt(name string, level int) int {
	values := c.Stats[name]
	if len(values) == 0 {
		return 0
	}
	if level < 1 {
		level = 1
	}
	if level > len(values) {
		level = len(values)
	}
	return values[level-1]
}
func (d Database) SortedIDs() []string {
	out := make([]string, 0)
	for _, c := range d.Classes {
		out = append(out, c.ID)
	}
	sort.Strings(out)
	return slices.Compact(out)
}

var _ = math.MaxInt

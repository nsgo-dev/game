package main

import (
	"embed"
	"fmt"

	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/game/pkg/inventory"
	"github.com/nsgo-dev/game/pkg/quest"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/stats"
	"github.com/nsgo-dev/game/pkg/vn"
)

// dataFS holds the game's data files, which a designer edits without touching the code.
//
//go:embed data/*.yaml data/*.yarn
var dataFS embed.FS

// The data files under data/.
const (
	worldFile    = "data/world.yaml"
	itemsFile    = "data/items.yaml"
	statsFile    = "data/stats.yaml"
	questsFile   = "data/quests.yaml"
	castFile     = "data/cast.yaml"
	dialogueFile = "data/dialogue.yarn"
)

// mainQuest is the quest whose completion wins the game.
const mainQuest = "glowcaps"

// content is everything the data files and the art define, read once when the game starts and never changed.
type content struct {
	world    *World
	items    *inventory.Catalog
	stats    *stats.File
	quests   *quest.Book
	cast     *vn.Cast
	dialogue *flow.DialogueScript
	atlas    *sprite.Atlas
}

// loadContent reads and checks every data file, and checks that they agree: that the world's items, foes, people and art exist, that every person has a dialogue node and a cast entry, that every piece of equipment has a stats effect and that the quests name things the world has.
func loadContent() (*content, error) {
	c := &content{atlas: newAtlas()}
	var err error
	var data []byte
	if data, err = dataFS.ReadFile(worldFile); err == nil {
		c.world, err = ParseWorld(data)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", worldFile, err)
	}
	if data, err = dataFS.ReadFile(itemsFile); err == nil {
		c.items, err = inventory.Parse(data)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", itemsFile, err)
	}
	if data, err = dataFS.ReadFile(statsFile); err == nil {
		c.stats, err = stats.Parse(data)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", statsFile, err)
	}
	if data, err = dataFS.ReadFile(questsFile); err == nil {
		c.quests, err = quest.Parse(data)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", questsFile, err)
	}
	if data, err = dataFS.ReadFile(castFile); err == nil {
		c.cast, err = vn.ParseCast(data)
	}
	if err == nil {
		err = c.cast.CheckFrames(c.atlas)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", castFile, err)
	}
	if data, err = dataFS.ReadFile(dialogueFile); err == nil {
		c.dialogue, err = flow.ParseYarn(string(data))
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dialogueFile, err)
	}
	if err := c.check(); err != nil {
		return nil, err
	}
	return c, nil
}

// check reports the first reference from one data file to something another does not define.
func (c *content) check() error {
	if c.world.TileSize != 8*artScale {
		return fmt.Errorf("%s: tileSize is %d, but art.go draws tiles %d points wide", worldFile, c.world.TileSize, 8*artScale)
	}
	art := func(name, what string) error {
		if _, ok := c.atlas.Frames[name]; !ok {
			return fmt.Errorf("%s: %s names art %q, which art.go does not draw", worldFile, what, name)
		}
		return nil
	}
	for _, t := range c.world.Tiles {
		if err := art(t.Art, "tile "+t.Key); err != nil {
			return err
		}
	}
	foes := map[string]bool{}
	for _, f := range c.world.Foes {
		foes[f.ID] = true
		if err := art(f.Art, "foe "+f.ID); err != nil {
			return err
		}
	}
	people := map[string]bool{}
	for _, m := range c.world.Maps {
		for _, p := range m.People {
			people[p.ID] = true
			if err := art(p.Art, "person "+p.ID); err != nil {
				return err
			}
			if _, ok := c.cast.Character(p.ID); !ok {
				return fmt.Errorf("%s: person %q is not in %s", worldFile, p.ID, castFile)
			}
			if !c.dialogue.HasNode(p.Node) {
				return fmt.Errorf("%s: person %q starts node %q, which %s does not have", worldFile, p.ID, p.Node, dialogueFile)
			}
		}
		for _, it := range m.Items {
			if !c.items.Has(it.Item) {
				return fmt.Errorf("%s: item %q is %q, which %s does not define", worldFile, it.ID, it.Item, itemsFile)
			}
		}
	}
	for _, def := range c.items.Items() {
		if def.Equip == "" {
			continue
		}
		if _, ok := c.stats.Effect(def.ID); !ok {
			return fmt.Errorf("%s: %s %q has no effect of the same name in %s", itemsFile, def.Equip, def.ID, statsFile)
		}
		if def.Equip != slotWeapon && def.Equip != slotArmor && def.Equip != slotCharm {
			return fmt.Errorf("%s: item %q fits %q, which is not an equipment slot", itemsFile, def.ID, def.Equip)
		}
	}
	for _, s := range []string{statMaxHP, statAttack, statDefense} {
		if !statDefined(c.stats, s) {
			return fmt.Errorf("%s: stat %q is missing", statsFile, s)
		}
	}
	if c.quests.Quest(mainQuest) == nil {
		return fmt.Errorf("%s: quest %q is missing", questsFile, mainQuest)
	}
	for _, q := range c.quests.Quests {
		for _, o := range q.Objectives {
			var ok bool
			switch o.Kind {
			case quest.KindCollect:
				ok = c.items.Has(o.Target)
			case quest.KindDefeat:
				ok = foes[o.Target]
			case quest.KindTalk:
				ok = people[o.Target]
			default:
				ok = true
			}
			if !ok {
				return fmt.Errorf("%s: quest %q: objective %q targets %q, which the game does not have", questsFile, q.ID, o.ID, o.Target)
			}
		}
		for _, r := range q.Rewards {
			if r.Kind == "item" && !c.items.Has(r.ID) {
				return fmt.Errorf("%s: quest %q rewards item %q, which %s does not define", questsFile, q.ID, r.ID, itemsFile)
			}
		}
	}
	return nil
}

// statDefined reports whether f defines the stat name.
func statDefined(f *stats.File, name string) bool {
	for _, d := range f.Stats {
		if d.Name == name {
			return true
		}
	}
	return false
}

// itemArt returns the atlas frame that draws item id on a map: the frame of the same name, or "bundle".
func (c *content) itemArt(id string) string {
	if _, ok := c.atlas.Frames[id]; ok {
		return id
	}
	return "bundle"
}

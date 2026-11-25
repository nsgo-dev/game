package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/ability"
	"github.com/nsgo-dev/game/packages/game/loot"
	"github.com/nsgo-dev/game/packages/game/stats"
)

// TestDataFilesAreCanonical checks that each data file parses and is in the form its encoder writes, so a load and save changes nothing and a review shows only real edits.
func TestDataFilesAreCanonical(t *testing.T) {
	d, err := loadData()
	if err != nil {
		t.Fatal(err)
	}
	dungeon, err := encodeDungeon(d.dungeon)
	if err != nil {
		t.Fatal(err)
	}
	statsFile, err := stats.Encode(d.stats)
	if err != nil {
		t.Fatal(err)
	}
	lootFile, err := loot.Encode(d.lootDoc)
	if err != nil {
		t.Fatal(err)
	}
	abilities, err := ability.Encode(d.abilities)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct {
		name      string
		got, want []byte
	}{
		{"data/dungeon.yaml", dungeon, dungeonYAML},
		{"data/stats.yaml", statsFile, statsYAML},
		{"data/loot.yaml", lootFile, lootYAML},
		{"data/abilities.yaml", abilities, abilitiesYAML},
	} {
		if !bytes.Equal(f.got, f.want) {
			t.Errorf("%s is not canonical; its canonical form is:\n%s", f.name, f.got)
		}
	}
}

func TestParseDungeonRefusesInvalidFiles(t *testing.T) {
	valid := string(dungeonYAML)
	for _, tc := range []struct {
		name, doc, want string
	}{
		{"empty", "", "empty document"},
		{"unknown field", valid + "music: loud\n", "field music not found"},
		{"trailing document", valid + "---\nversion: 1\n", "trailing"},
		{"version", strings.Replace(valid, "version: 1", "version: 2", 1), "unsupported version 2"},
		{"generator", strings.Replace(valid, "generator: cave", "generator: maze", 1), `generator "maze"`},
		{"floor size", strings.Replace(valid, "width: 40", "width: 8", 1), "size 8x30"},
		{"guardian", strings.Replace(valid, "guardian: vault-warden", "guardian: dragon", 1), `unknown guardian "dragon"`},
		{"glyph", strings.Replace(valid, "glyph: r", "glyph: rr", 1), `glyph "rr"`},
		{"colour", strings.Replace(valid, "'#84cc16'", "'#84CC16'", 1), "is not #rrggbb"},
		{"monster floors", strings.Replace(valid, "to: 2\n", "to: 9\n", 1), "floors 1 to 9"},
		{"duplicate item", strings.Replace(valid, "id: iron-spike", "id: rusty-pick", 1), `duplicate id "rusty-pick"`},
		{"kind", strings.Replace(valid, "kind: tonic", "kind: scroll", 1), `kind "scroll"`},
		{"rankless weapon", strings.Replace(valid, "    rank: 2\n", "", 1), "needs an effect and a rank"},
		{"starting weapon", strings.Replace(valid, "weapon: rusty-pick", "weapon: draught", 1), "is not a weapon"},
		{"no spawns", strings.Replace(strings.Replace(valid, "from: 1\n    to: 2\n", "from: 2\n    to: 2\n", 1), "from: 1\n    to: 3\n", "from: 2\n    to: 3\n", 1), "no monster lives on floor 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDungeon([]byte(tc.doc))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseDungeon = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

// TestDataFilesAgree checks that loadData refuses a name one data file uses that another does not define.
func TestDataFilesAgree(t *testing.T) {
	for _, tc := range []struct {
		name                                            string
		dungeonFile, statsFile, lootFile, abilitiesFile string
		want                                            string
	}{
		{name: "item effect", statsFile: strings.Replace(string(statsYAML), "name: iron-spike", "name: iron-pike", 1), want: `no effect "iron-spike"`},
		{name: "untimed tonic", statsFile: strings.Replace(string(statsYAML), "    steps: 20\n", "", 1), want: "would never wear off"},
		{name: "floor loot", dungeonFile: strings.Replace(string(dungeonYAML), "loot: floor-deep", "loot: floor-abyss", 1), want: `no loot table "floor-abyss"`},
		{name: "monster drops", dungeonFile: strings.Replace(string(dungeonYAML), "drops: rat", "drops: rats", 1), want: `no loot table "rats"`},
		{name: "loot item", lootFile: strings.Replace(string(lootYAML), "item: hide-vest", "item: hide-cloak", 1), want: `drops "hide-cloak"`},
		{name: "ability", abilitiesFile: strings.Replace(string(abilitiesYAML), "id: cleave", "id: sweep", 1), want: "no cleave ability"},
		{name: "stat", statsFile: strings.Replace(string(statsYAML), "name: maxHealth", "name: health", 1), want: "no maxHealth stat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseData(or(tc.dungeonFile, dungeonYAML), or(tc.statsFile, statsYAML), or(tc.lootFile, lootYAML), or(tc.abilitiesFile, abilitiesYAML))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseData = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func or(edited string, file []byte) []byte {
	if edited != "" {
		return []byte(edited)
	}
	return file
}

// FuzzParseDungeon checks that any dungeon file parseDungeon accepts encodes to a file that parses back to the same encoding.
func FuzzParseDungeon(f *testing.F) {
	f.Add(dungeonYAML)
	f.Add([]byte("version: 1\nname: x\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := parseDungeon(data)
		if err != nil {
			return
		}
		once, err := encodeDungeon(d)
		if err != nil {
			t.Fatalf("encoding a parsed dungeon: %v", err)
		}
		again, err := parseDungeon(once)
		if err != nil {
			t.Fatalf("parsing an encoded dungeon: %v\n%s", err, once)
		}
		twice, err := encodeDungeon(again)
		if err != nil || !bytes.Equal(once, twice) {
			t.Fatalf("encoding is not stable (%v):\n%s\n---\n%s", err, once, twice)
		}
	})
}

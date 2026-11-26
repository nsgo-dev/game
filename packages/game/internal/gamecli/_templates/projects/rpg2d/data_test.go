package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/inventory"
	"github.com/nsgo-dev/game/packages/game/quest"
	"github.com/nsgo-dev/game/packages/game/stats"
	"github.com/nsgo-dev/game/packages/game/vn"
)

// TestContentLoads checks that every data file loads and that the files agree with each other and with the art.
func TestContentLoads(t *testing.T) {
	if _, err := loadContent(); err != nil {
		t.Fatal(err)
	}
}

// TestDataFilesAreCanonical checks that each YAML data file is in the form its package writes, so that loading and saving one, as the editor and tools do, changes nothing.
func TestDataFilesAreCanonical(t *testing.T) {
	for name, encode := range map[string]func([]byte) ([]byte, error){
		worldFile: func(b []byte) ([]byte, error) {
			w, err := ParseWorld(b)
			if err != nil {
				return nil, err
			}
			return EncodeWorld(w)
		},
		itemsFile: func(b []byte) ([]byte, error) {
			c, err := inventory.Parse(b)
			if err != nil {
				return nil, err
			}
			return c.Encode()
		},
		statsFile: func(b []byte) ([]byte, error) {
			f, err := stats.Parse(b)
			if err != nil {
				return nil, err
			}
			return stats.Encode(f)
		},
		questsFile: func(b []byte) ([]byte, error) {
			q, err := quest.Parse(b)
			if err != nil {
				return nil, err
			}
			return quest.Encode(q)
		},
		castFile: func(b []byte) ([]byte, error) {
			c, err := vn.ParseCast(b)
			if err != nil {
				return nil, err
			}
			return vn.EncodeCast(c)
		},
	} {
		src, err := dataFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		got, err := encode(src)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !bytes.Equal(got, src) {
			t.Errorf("%s is not in canonical form; it should read:\n%s", name, got)
		}
	}
}

// validWorld returns a small world that parses, for tests to break one field of.
func validWorld() string {
	return `version: 1
tileSize: 16
start:
  map: a
  at: [1, 1]
tiles:
  - key: .
    art: grass
  - key: T
    art: tree
    solid: true
foes:
  - id: slime
    name: Slime
    art: slime
    hp: 3
    attack: 1
    defense: 0
maps:
  - id: a
    name: A
    rows:
      - TTTT
      - T..T
      - T...
      - TTTT
    exits:
      - at: [3, 2]
        to:
          map: a
          at: [1, 1]
    foes:
      - id: s
        kind: slime
        at: [2, 1]
        patrol: [[2, 2]]
`
}

func TestParseWorldRoundTrips(t *testing.T) {
	w, err := ParseWorld([]byte(validWorld()))
	if err != nil {
		t.Fatal(err)
	}
	out, err := EncodeWorld(w)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != validWorld() {
		t.Fatalf("EncodeWorld wrote\n%s\nwant\n%s", out, validWorld())
	}
}

func TestParseWorldRefusesBrokenWorlds(t *testing.T) {
	for _, tc := range []struct {
		name, old, new, want string
	}{
		{"version", "version: 1", "version: 2", "version 2"},
		{"unknown field", "tileSize: 16", "tileSize: 16\ncolor: red", "field color not found"},
		{"tile size", "tileSize: 16", "tileSize: 0", "tileSize 0"},
		{"long key", "key: T", "key: TT", "one character"},
		{"repeated key", "key: T", "key: .", "used twice"},
		{"short row", "- T..T", "- T.T", "row 1"},
		{"unknown tile", "- T..T", "- T.xT", "not in tiles"},
		{"start on a tree", "at: [1, 1]\ntiles", "at: [0, 0]\ntiles", "solid"},
		{"missing map", "map: a\n          at", "map: b\n          at", `map "b"`},
		{"foe outside", "at: [2, 1]", "at: [9, 1]", "outside"},
		{"unknown foe", "kind: slime", "kind: ghost", `"ghost"`},
		{"patrol on a tree", "[[2, 2]]", "[[0, 2]]", "solid"},
		{"foe without hp", "hp: 3", "hp: 0", "positive hp"},
		{"second document", "patrol: [[2, 2]]", "patrol: [[2, 2]]\n---\nversion: 1", "second"},
		{"patrol through a tree", "patrol: [[2, 2]]", "patrol: [[3, 2]]", "the patrol of foe s at [3 1] is on a solid tile"},
		{"patrol through a still foe", "patrol: [[2, 2]]\n", "patrol: [[2, 2]]\n      - {id: t, kind: slime, at: [2, 2]}\n", `foe "t" stands still`},
		{"two foes on a tile", "patrol: [[2, 2]]\n", "patrol: [[2, 2]]\n      - {id: t, kind: slime, at: [2, 1], patrol: [[1, 1]]}\n", `starts on [2 1] with foe "s"`},
		{"two people on a tile", "    foes:\n      - id: s", "    people:\n      - {id: p, art: p, at: [1, 2], node: N}\n      - {id: q, art: q, at: [1, 2], node: N}\n    foes:\n      - id: s", `with "p"`},
		{"foe on a person", "    foes:\n      - id: s", "    people:\n      - {id: p, art: p, at: [2, 1], node: N}\n    foes:\n      - id: s", `foe s at [2 1] is where person "p" stands`},
		{"item under a person", "    foes:\n      - id: s", "    people:\n      - {id: p, art: p, at: [1, 2], node: N}\n    items:\n      - {id: i, item: x, at: [1, 2]}\n    foes:\n      - id: s", `item i at [1 2] is where person "p" stands`},
		{"exit into a person", "    foes:\n      - id: s", "    people:\n      - {id: p, art: p, at: [3, 2], node: N}\n    foes:\n      - id: s", `an exit at [3 2] is where person "p" stands`},
		{"patrol through a person", "    foes:\n      - id: s", "    people:\n      - {id: p, art: p, at: [2, 2], node: N}\n    foes:\n      - id: s", `the patrol of foe s at [2 2] is where person "p" stands`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(validWorld(), tc.old, tc.new, 1)
			if src == validWorld() {
				t.Fatalf("%q is not in the world", tc.old)
			}
			_, err := ParseWorld([]byte(src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseWorld = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

// emptyListWorlds returns worlds that write explicit empty lists, or leave out the foe kinds, which EncodeWorld writes differently, so the round trip has to load both forms as the same World.
func emptyListWorlds() []string {
	mapFoes := "    foes:\n      - id: s\n        kind: slime\n        at: [2, 1]\n        patrol: [[2, 2]]\n"
	exits := "    exits:\n      - at: [3, 2]\n        to:\n          map: a\n          at: [1, 1]\n"
	kinds := "foes:\n  - id: slime\n    name: Slime\n    art: slime\n    hp: 3\n    attack: 1\n    defense: 0\n"
	return []string{
		strings.Replace(validWorld(), mapFoes, "    people: []\n    foes: []\n    items: []\n", 1),
		strings.Replace(validWorld(), "patrol: [[2, 2]]", "patrol: []", 1),
		strings.NewReplacer(mapFoes, "", exits, "    exits: []\n", kinds, "").Replace(validWorld()),
		strings.NewReplacer(mapFoes, "", kinds, "foes: []\n").Replace(validWorld()),
	}
}

func TestParseWorldLoadsEmptyListsAsNone(t *testing.T) {
	for i, src := range emptyListWorlds() {
		if src == validWorld() {
			t.Fatalf("world %d changes nothing", i)
		}
		w, err := ParseWorld([]byte(src))
		if err != nil {
			t.Fatalf("world %d: %v\n%s", i, err, src)
		}
		out, err := EncodeWorld(w)
		if err != nil {
			t.Fatal(err)
		}
		again, err := ParseWorld(out)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(again, w) {
			t.Fatalf("world %d: the round trip changed it:\n%s", i, out)
		}
		if strings.Contains(string(out), "[]") {
			t.Fatalf("world %d: EncodeWorld wrote an empty list:\n%s", i, out)
		}
	}
}

// FuzzParseWorld checks that ParseWorld never panics and that every world it accepts encodes to a form that parses back to the same world and encodes the same again.
func FuzzParseWorld(f *testing.F) {
	f.Add([]byte(validWorld()))
	for _, src := range emptyListWorlds() {
		f.Add([]byte(src))
	}
	if src, err := dataFS.ReadFile(worldFile); err == nil {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		w, err := ParseWorld(data)
		if err != nil {
			return
		}
		out, err := EncodeWorld(w)
		if err != nil {
			t.Fatalf("EncodeWorld of a parsed world: %v", err)
		}
		again, err := ParseWorld(out)
		if err != nil {
			t.Fatalf("ParseWorld of EncodeWorld's output: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(again, w) {
			t.Fatalf("the round trip changed the world:\n%s", out)
		}
		if out2, _ := EncodeWorld(again); !bytes.Equal(out, out2) {
			t.Fatalf("EncodeWorld is not stable:\n%s\n---\n%s", out, out2)
		}
	})
}

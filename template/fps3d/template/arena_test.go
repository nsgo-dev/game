package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestArenaFileRoundTripsUnchanged(t *testing.T) {
	a, err := ParseArena(arenaYAML)
	if err != nil {
		t.Fatal(err)
	}
	out, err := EncodeArena(a)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, arenaYAML) {
		t.Fatalf("arena.yaml is not in the form EncodeArena writes; write this instead:\n%s", out)
	}
}

func TestParseArenaRefusesBadFiles(t *testing.T) {
	edit := func(old, new string) []byte {
		if !bytes.Contains(arenaYAML, []byte(old)) {
			t.Fatalf("arena.yaml has no %q to edit", old)
		}
		return bytes.Replace(arenaYAML, []byte(old), []byte(new), 1)
	}
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"unknown field", edit("countdown: 3", "countdown: 3\nboss: true"), "field boss not found"},
		{"version", edit("version: 1", "version: 2"), "version 2"},
		{"trailing document", append(bytes.Clone(arenaYAML), "---\nversion: 1\n"...), "trailing"},
		{"ragged row", edit("'#S......#......S#'", "'#S......#.....S#'"), "has 16 cells"},
		{"open border", edit("'#################'\nplayer", "'##.##############'\nplayer"), "border"},
		{"two players", edit("'#..c....A....c..#'", "'#..c....P....c..#'"), "2 player starts"},
		{"unknown cell", edit("'#..c....A....c..#'", "'#..c....?....c..#'"), "not a cell"},
		{"waypoint gap", edit("'#.#..4..c..3..#.#'", "'#.#..5..c..3..#.#'"), "4 is missing"},
		{"walled off", edit("'#.##.........##.#'\n  - '#S......#......S#'", "'#.##.........####'\n  - '#S......#.....#S#'"), "cannot be reached"},
		{"unknown enemy", edit("enemy: warden\n        count: 1", "enemy: titan\n        count: 1"), "unknown enemy"},
		{"weapon kind", edit("kind: hitscan", "kind: laser"), "kind \"laser\""},
		{"hitscan speed", edit("range: 60", "range: 60\n    speed: 4"), "no speed"},
		{"pickup effect", edit("effect: ammo", "effect: armor"), "effect \"armor\""},
		{"pickup symbol", edit("symbol: A", "symbol: S"), "symbol"},
		{"color", edit("[0.85, 0.32, 0.22]", "[1.5, 0.32, 0.22]"), "color"},
		{"no waves", noWaves(), "at least one wave"},
		{"huge amount", edit("amount: 35", "amount: 9223372036854775807"), "amount must be 1 to"},
		{"huge damage", edit("damage: 20", "damage: 100000"), "damage and magazine must be 1 to"},
		{"crowded wave", edit("count: 4", "count: 15"), "more than 16 enemies"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseArena(c.data)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ParseArena error = %v, want one mentioning %q", err, c.want)
			}
		})
	}
}

// noWaves returns arena.yaml with an empty wave list.
func noWaves() []byte {
	s := string(arenaYAML)
	return []byte(s[:strings.Index(s, "waves:")] + "waves: []\n" + s[strings.Index(s, "countdown:"):])
}

func TestArenaLayoutPlacesEverything(t *testing.T) {
	m := newMatch(arena)
	if len(m.spawns) != 4 || len(m.route) != 4 || len(m.pickups) != 2 {
		t.Fatalf("the arena has %d spawns, %d waypoints and %d pickups, want 4, 4 and 2", len(m.spawns), len(m.route), len(m.pickups))
	}
	if m.player.ctrl.Pos != m.cellCenter(gridCell{8, 6}) {
		t.Fatalf("the player starts at %+v, want the P cell", m.player.ctrl.Pos)
	}
}

func FuzzParseArena(f *testing.F) {
	f.Add(arenaYAML)
	f.Add([]byte("version: 1\nname: x\n"))
	f.Add([]byte("layout: ['###', '#P#', '###']\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		a, err := ParseArena(data)
		if err != nil {
			return
		}
		// Whatever ParseArena accepts encodes, parses back the same and starts a match.
		out, err := EncodeArena(a)
		if err != nil {
			t.Fatalf("EncodeArena of a parsed arena: %v", err)
		}
		b, err := ParseArena(out)
		if err != nil {
			t.Fatalf("ParseArena of EncodeArena's output: %v", err)
		}
		again, err := EncodeArena(b)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("the arena does not round-trip: %v", err)
		}
		newMatch(b).step(controls{fire: true, moveY: 1}, step)
	})
}

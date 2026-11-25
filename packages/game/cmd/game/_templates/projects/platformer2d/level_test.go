package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/platformer"
	"github.com/nsgo-dev/game/packages/game/rooms"
)

// TestDataFilesAreCanonical checks that each data file parses and is in the form its encoder writes, so a load and save changes nothing and a review shows only real edits.
func TestDataFilesAreCanonical(t *testing.T) {
	d, err := loadData()
	if err != nil {
		t.Fatal(err)
	}
	level, err := encodeLevel(d.level)
	if err != nil {
		t.Fatal(err)
	}
	roomMap, err := rooms.Encode(d.rooms)
	if err != nil {
		t.Fatal(err)
	}
	player, err := platformer.EncodeConfig(d.player)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct {
		name      string
		got, want []byte
	}{
		{"data/level.yaml", level, levelYAML},
		{"data/rooms.yaml", roomMap, roomsYAML},
		{"data/player.yaml", player, playerYAML},
	} {
		if !bytes.Equal(f.got, f.want) {
			t.Errorf("%s is not canonical; its canonical form is:\n%s", f.name, f.got)
		}
	}
}

// TestRoomsCoverTheLevel checks that every column of the level is in exactly one room, so the camera always has bounds and never shows two rooms at once.
func TestRoomsCoverTheLevel(t *testing.T) {
	d := mustLoadData()
	width := len(d.level.rows()[0]) * d.level.TileSize
	height := float32(len(d.level.rows()) * d.level.TileSize)
	for x := d.level.TileSize / 2; x < width; x += d.level.TileSize {
		n := 0
		for _, r := range d.rooms.Rooms {
			if float32(x) >= r.X && float32(x) < r.X+r.Width {
				n++
			}
		}
		if n != 1 {
			t.Errorf("x %d is in %d rooms, want 1", x, n)
		}
	}
	for _, r := range d.rooms.Rooms {
		if r.Y != 0 || r.Height != height {
			t.Errorf("room %q spans y %v to %v, want the level's 0 to %v", r.ID, r.Y, r.Y+r.Height, height)
		}
	}
}

const validLevel = "version: 1\nname: Test\ntileSize: 16\nlives: 3\ntiles: |\n  #P.G#\n  #####\n"

func TestParseLevelRefusesInvalidFiles(t *testing.T) {
	if _, err := parseLevel([]byte(validLevel)); err != nil {
		t.Fatalf("the valid level: %v", err)
	}
	for _, tc := range []struct {
		name, doc, want string
	}{
		{"empty", "", "empty document"},
		{"unknown field", validLevel + "music: loud\n", "field music not found"},
		{"trailing document", validLevel + "---\nversion: 1\n", "trailing"},
		{"version", strings.Replace(validLevel, "version: 1", "version: 2", 1), "unsupported version 2"},
		{"no name", strings.Replace(validLevel, "name: Test", "name: ' '", 1), "name is empty"},
		{"tile size", strings.Replace(validLevel, "tileSize: 16", "tileSize: 2", 1), "tileSize 2"},
		{"lives", strings.Replace(validLevel, "lives: 3", "lives: 0", 1), "lives 0"},
		{"no final newline", strings.Replace(validLevel, "tiles: |", "tiles: |-", 1), "end with a newline"},
		{"one row", "version: 1\nname: Test\ntileSize: 16\nlives: 3\ntiles: |\n  #PG#\n", "at least 2"},
		{"ragged rows", strings.Replace(validLevel, "  #####", "  ####", 1), "row 1 is 4 tiles wide"},
		{"unknown tile", strings.Replace(validLevel, "#P.G#", "#PxG#", 1), "unknown tile 'x'"},
		{"no start", strings.Replace(validLevel, "#P.G#", "#..G#", 1), "0 starts"},
		{"two goals", strings.Replace(validLevel, "#P.G#", "#PGG#", 1), "2 goals"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseLevel([]byte(tc.doc))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseLevel error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestEncodeLevelRoundTrips(t *testing.T) {
	l, err := parseLevel([]byte(validLevel))
	if err != nil {
		t.Fatal(err)
	}
	out, err := encodeLevel(l)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != validLevel {
		t.Fatalf("encodeLevel wrote:\n%s\nwant:\n%s", out, validLevel)
	}
	l.Lives = 0
	if _, err := encodeLevel(l); err == nil {
		t.Fatal("encodeLevel wrote a level with no lives")
	}
}

// FuzzParseLevel checks that any level parseLevel accepts encodes to a canonical form that parses back to the same level.
func FuzzParseLevel(f *testing.F) {
	f.Add([]byte(validLevel))
	f.Add(levelYAML)
	f.Add([]byte("version: 1\nname: x\ntileSize: 8\nlives: 1\ntiles: \"P=^o\\nC#.G\\n\"\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		l, err := parseLevel(data)
		if err != nil {
			return
		}
		out, err := encodeLevel(l)
		if err != nil {
			t.Fatalf("encodeLevel of a parsed level: %v", err)
		}
		again, err := parseLevel(out)
		if err != nil {
			t.Fatalf("parseLevel of encodeLevel's output: %v\n%s", err, out)
		}
		if *again != *l {
			t.Fatalf("round trip changed the level: %+v, then %+v", l, again)
		}
		out2, err := encodeLevel(again)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("encoding is not stable (%v):\n%s\nthen:\n%s", err, out, out2)
		}
	})
}

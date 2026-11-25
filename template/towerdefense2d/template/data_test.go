package main

import (
	"bytes"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/bullets"
)

// TestDataFilesAreCanonical checks that the data files load, refer to each other correctly and are in the form their encoders write, so saving one after loading it changes nothing.
func TestDataFilesAreCanonical(t *testing.T) {
	d, err := LoadData(dataFS)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct {
		path   string
		encode func() ([]byte, error)
	}{
		{"data/level.yaml", func() ([]byte, error) { return EncodeLevel(d.Level) }},
		{"data/towers.yaml", func() ([]byte, error) { return EncodeTowers(d.Towers) }},
		{"data/projectiles.yaml", func() ([]byte, error) { return bullets.Encode(d.Projectiles) }},
	} {
		want, err := dataFS.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := f.encode()
		if err != nil {
			t.Fatalf("%s: %v", f.path, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is not in canonical form; it encodes as:\n%s", f.path, got)
		}
	}
}

func TestParseLevelRefusesBadLevels(t *testing.T) {
	good, err := dataFS.ReadFile("data/level.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, old, new, want string
	}{
		{"unknown field", "lives: 10\n", "lives: 10\nscore: 3\n", "field score not found"},
		{"version", "version: 1\n", "version: 2\n", "version 2"},
		{"no lives", "lives: 10\n", "lives: 0\n", "lives 0"},
		{"huge tiles", "tileSize: 32\n", "tileSize: 32000\n", "at most 256"},
		{"ragged map", `  - "^.............."`, `  - "^............."`, "has 15 tiles, want 14"},
		{"unknown tile", `  - "^.............."`, `  - "^.............#"`, "unknown tile"},
		{"two spawns", `  - "^.............."`, `  - "S.............."`, "2 spawn"},
		{"cut road", `  - "....=.........."`, `  - "..............."`, "no road"},
		{"unknown enemy", "enemy: sprout\n", "enemy: dragon\n", "unknown enemy"},
		{"no speed", "      - name: speed\n        base: 36\n", "      - name: pace\n        base: 36\n", "no speed stat"},
		{"bad color", "'#a3e635'", "'lime-ish'", "color"},
		{"no interval", "interval: 1s\n", "interval: 0s\n", "positive interval"},
		{"second document", "version: 1\n", "version: 1\n", "trailing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(string(good), tc.old, tc.new, 1)
			if tc.name == "second document" {
				src += "---\nversion: 1\n"
			}
			if src == string(good) && tc.name != "second document" {
				t.Fatalf("the edit %q did not apply", tc.old)
			}
			_, err := ParseLevel([]byte(src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseLevel error %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestParseTowersRefusesBadTowers(t *testing.T) {
	good, err := dataFS.ReadFile("data/towers.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, old, new, want string
	}{
		{"unknown field", "refund: 0.7\n", "refund: 0.7\ntax: 1\n", "field tax not found"},
		{"version", "version: 1\n", "version: 0\n", "version 0"},
		{"refund", "refund: 0.7\n", "refund: 1.5\n", "refund 1.5"},
		{"free tower", "cost: 40\n", "cost: 0\n", "cost 0"},
		{"no range", "      - name: range\n        base: 80\n", "      - name: reach\n        base: 80\n", "no range stat"},
		{"upgrade of an unknown stat", "          - stat: damage\n            op: add\n            value: 4\n", "          - stat: power\n            op: add\n            value: 4\n", "power"},
		{"no pattern", "pattern: bolt\n", "pattern: \"\"\n", "no pattern"},
		{"upgrade to no range", "          - stat: range\n            op: add\n            value: 16\n", "          - stat: range\n            op: override\n            value: 0\n", "stay positive after upgrade 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(string(good), tc.old, tc.new, 1)
			if src == string(good) {
				t.Fatalf("the edit %q did not apply", tc.old)
			}
			_, err := ParseTowers([]byte(src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseTowers error %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func FuzzParseLevel(f *testing.F) {
	good, err := dataFS.ReadFile("data/level.yaml")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(good)
	f.Add([]byte("version: 1\nname: x\ntileSize: 8\nmoney: 0\nlives: 1\nmap: [\"S=G\"]\nenemies: [{name: a, color: red, radius: 1, reward: 0, leak: 1, stats: [{name: health, base: 1}, {name: speed, base: 1}]}]\nwaves: [{groups: [{enemy: a, count: 1, interval: 1s}]}]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		l, err := ParseLevel(data)
		if err != nil {
			return
		}
		out, err := EncodeLevel(l)
		if err != nil {
			t.Fatalf("a parsed level does not encode: %v", err)
		}
		again, err := ParseLevel(out)
		if err != nil {
			t.Fatalf("an encoded level does not parse: %v\n%s", err, out)
		}
		if out2, err := EncodeLevel(again); err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("encoding is not stable (%v):\n%s\n---\n%s", err, out, out2)
		}
	})
}

func FuzzParseTowers(f *testing.F) {
	good, err := dataFS.ReadFile("data/towers.yaml")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(good)
	f.Add([]byte("version: 1\nrefund: 0\ntowers: [{name: a, label: A, color: blue, cost: 1, pattern: p, stats: [{name: damage, base: 1}, {name: range, base: 1}, {name: cooldown, base: 1}]}]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		tw, err := ParseTowers(data)
		if err != nil {
			return
		}
		out, err := EncodeTowers(tw)
		if err != nil {
			t.Fatalf("parsed towers do not encode: %v", err)
		}
		again, err := ParseTowers(out)
		if err != nil {
			t.Fatalf("encoded towers do not parse: %v\n%s", err, out)
		}
		if out2, err := EncodeTowers(again); err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("encoding is not stable (%v):\n%s\n---\n%s", err, out, out2)
		}
	})
}

// TestLoadDataChecksTheFilesAgainstEachOther checks that a tower must fire a pattern the projectile file has, one that draws, fires one shot and moves.
func TestLoadDataChecksTheFilesAgainstEachOther(t *testing.T) {
	files := fstest.MapFS{}
	for _, name := range []string{"data/level.yaml", "data/towers.yaml", "data/projectiles.yaml"} {
		data, err := dataFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	if _, err := LoadData(files); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, file, old, new, want string
	}{
		{"missing pattern", "data/towers.yaml", "pattern: bolt\n", "pattern: laser\n", `pattern "laser"`},
		{"no frame", "data/projectiles.yaml", "    frame: bolt\n", "", "needs a frame"},
		{"a burst", "data/projectiles.yaml", "shots: 1\n", "shots: 3\n    interval: 50ms\n", "shots: 1"},
		{"missing file", "data/level.yaml", "", "", "level"},
		{"map unlike the display", "data/level.yaml", "tileSize: 32\n", "tileSize: 40\n", "game.yaml's display is 480 by 320"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			broken := fstest.MapFS{}
			for name, f := range files {
				broken[name] = f
			}
			if tc.old == "" && tc.new == "" {
				delete(broken, tc.file)
			} else {
				src := string(files[tc.file].Data)
				edited := strings.Replace(src, tc.old, tc.new, 1)
				if edited == src {
					t.Fatalf("the edit %q did not apply", tc.old)
				}
				broken[tc.file] = &fstest.MapFile{Data: []byte(edited)}
			}
			if _, err := LoadData(broken); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadData error %v, want one containing %q", err, tc.want)
			}
		})
	}
}

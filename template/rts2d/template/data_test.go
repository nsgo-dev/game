package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/rts"
)

func readData(t *testing.T, name string) []byte {
	t.Helper()
	src, err := dataFS.ReadFile("data/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// TestDataFilesLoad checks that the embedded data files parse and agree with each other.
func TestDataFilesLoad(t *testing.T) {
	d, err := loadGameData()
	if err != nil {
		t.Fatal(err)
	}
	if d.battleMap.cols*d.battleMap.rows == 0 || len(d.rules.Units) == 0 {
		t.Fatalf("empty data: %d by %d map, %d unit types", d.battleMap.cols, d.battleMap.rows, len(d.rules.Units))
	}
	if _, err := newComputer(d, opponent); err != nil {
		t.Fatal(err)
	}
}

// TestDataFilesAreCanonical checks that every data file is in the form its encoder writes, so loading and saving one changes nothing and a reviewer sees only real changes.
func TestDataFilesAreCanonical(t *testing.T) {
	for _, tc := range []struct {
		name   string
		encode func([]byte) ([]byte, error)
	}{
		{"rules.yaml", func(src []byte) ([]byte, error) {
			r, err := parseRules(src)
			if err != nil {
				return nil, err
			}
			return encodeRules(r)
		}},
		{"map.yaml", func(src []byte) ([]byte, error) {
			m, err := parseMap(src)
			if err != nil {
				return nil, err
			}
			return encodeMap(m)
		}},
		{"formations.yaml", func(src []byte) ([]byte, error) {
			f, err := rts.ParseFormations(src)
			if err != nil {
				return nil, err
			}
			return f.Encode()
		}},
		{"opponent.yaml", func(src []byte) ([]byte, error) {
			cfg, err := ai.ParseUtility(src)
			if err != nil {
				return nil, err
			}
			return ai.EncodeUtility(cfg)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := readData(t, tc.name)
			got, err := tc.encode(src)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, src) {
				t.Fatalf("data/%s is not in canonical form; it encodes as:\n%s", tc.name, got)
			}
		})
	}
}

// TestDataRefusesUnknownFieldsAndBadValues checks that a typo or an out-of-range value in a data file is an error that names it rather than a silent default.
func TestDataRefusesUnknownFieldsAndBadValues(t *testing.T) {
	rules := string(readData(t, "rules.yaml"))
	battle := string(readData(t, "map.yaml"))
	for _, tc := range []struct {
		name  string
		parse func(string) error
		src   string
		want  string
	}{
		{"unknown rules field", parseRulesErr, strings.Replace(rules, "maxUnits:", "maxUnitz:", 1), "maxUnitz"},
		{"future rules version", parseRulesErr, strings.Replace(rules, "version: 1", "version: 2", 1), "version 2"},
		{"second document", parseRulesErr, rules + "---\nversion: 1\n", "second YAML document"},
		{"empty", parseRulesErr, "", "empty file"},
		{"unit trained nowhere", parseRulesErr, strings.Replace(rules, "trainedAt: barracks", "trainedAt: castle", 1), "castle"},
		{"fighter without cooldown", parseRulesErr, strings.Replace(rules, "cooldown: 0.8", "cooldown: 0", 1), "cooldown"},
		{"start unit unknown", parseRulesErr, strings.Replace(rules, "unit: soldier", "unit: knight", 1), "knight"},
		{"zero speed", parseRulesErr, strings.Replace(rules, "speed: 36", "speed: 0", 1), "speed"},
		{"renamed unit the code trains", parseRulesErr, strings.Replace(rules, "name: ranger", "name: archer", 1), "no unit named ranger"},
		{"gatherer that fights", parseRulesErr, strings.Replace(rules, "    gathers: true\n", "    damage: 5\n    cooldown: 1\n    gathers: true\n", 1), "cannot deal damage"},
		{"duration beyond a day", parseRulesErr, strings.Replace(rules, "trainSeconds: 6", "trainSeconds: 1e300", 1), "trainSeconds"},
		{"unknown map field", parseMapErr, strings.Replace(battle, "cellSize:", "cellSise:", 1), "cellSise"},
		{"unknown tile", parseMapErr, strings.Replace(battle, "..", ".?", 1), "unknown tile"},
		{"ragged row", parseMapErr, strings.Replace(battle, "..\n", ".\n", 1), "cells wide"},
		{"two headquarters", parseMapErr, strings.Replace(battle, "...", ".A.", 1), "2 A markers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.parse(tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want one mentioning %q", err, tc.want)
			}
		})
	}

	// A headquarters needs ground under its whole footprint.
	m, err := parseMap([]byte(strings.Replace(battle, "A..", "A#.", 1)))
	if err != nil {
		t.Fatal(err)
	}
	r, err := parseRules([]byte(rules))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.fits(r); err == nil || !strings.Contains(err.Error(), "needs ground") {
		t.Fatalf("a headquarters on rock: error %v", err)
	}

	// Two footprints may not share a cell: a pad whose barracks would cover the headquarters' ground is refused.
	overlap := strings.Replace(battle, "\n  ................##......####", "\n  ...a............##......####", 1)
	if overlap == battle {
		t.Fatal("the overlap edit did not apply")
	}
	if m, err = parseMap([]byte(overlap)); err != nil {
		t.Fatal(err)
	}
	if err := m.fits(r); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("a pad over the headquarters: error %v", err)
	}
}

func parseRulesErr(src string) error { _, err := parseRules([]byte(src)); return err }

func parseMapErr(src string) error { _, err := parseMap([]byte(src)); return err }

// FuzzParseMap checks that any map parseMap accepts encodes to a file that parses back to the same tiles.
func FuzzParseMap(f *testing.F) {
	f.Add(readDataF(f, "map.yaml"))
	f.Add([]byte("version: 1\ncellSize: 8\ntiles: |\n  A.\n  .B\n"))
	f.Fuzz(func(t *testing.T, src []byte) {
		m, err := parseMap(src)
		if err != nil {
			return
		}
		out, err := encodeMap(m)
		if err != nil {
			t.Fatalf("encode an accepted map: %v", err)
		}
		back, err := parseMap(out)
		if err != nil {
			t.Fatalf("parse the encoded map: %v\n%s", err, out)
		}
		if !bytes.Equal(back.cells, m.cells) || back.cols != m.cols || back.CellSize != m.CellSize {
			t.Fatalf("round trip changed the map:\n%s", out)
		}
	})
}

func readDataF(f *testing.F, name string) []byte {
	src, err := dataFS.ReadFile("data/" + name)
	if err != nil {
		f.Fatal(err)
	}
	return src
}

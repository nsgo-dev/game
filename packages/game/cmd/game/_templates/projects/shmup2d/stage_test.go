package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/bullets"
	"github.com/nsgo-dev/game/packages/game/particles"
)

// readData returns the data file name, which the game embeds.
func readData(t testing.TB, name string) []byte {
	t.Helper()
	data, err := dataFS.ReadFile("data/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestDataFilesAreCanonical checks that each data file is in the form its encoder writes, so loading and saving one, as an editor or a tool does, changes nothing in review.
func TestDataFilesAreCanonical(t *testing.T) {
	stageYAML := readData(t, "stage.yaml")
	stage, err := parseStage(stageYAML)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := encodeStage(stage); err != nil || !bytes.Equal(got, stageYAML) {
		t.Errorf("data/stage.yaml is not in the form encodeStage writes (%v):\n%s", err, got)
	}

	patternsYAML := readData(t, "patterns.yaml")
	patterns, err := bullets.Parse(patternsYAML)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := bullets.Encode(patterns); err != nil || !bytes.Equal(got, patternsYAML) {
		t.Errorf("data/patterns.yaml is not in the form bullets.Encode writes (%v):\n%s", err, got)
	}

	explosionYAML := readData(t, "explosion.yaml")
	explosion, err := particles.UnmarshalConfigYAML(explosionYAML)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := particles.MarshalConfigYAML(explosion); err != nil || !bytes.Equal(got, explosionYAML) {
		t.Errorf("data/explosion.yaml is not in the form particles.MarshalConfigYAML writes (%v):\n%s", err, got)
	}
}

func TestParseStageRefusesInvalidStages(t *testing.T) {
	valid := string(readData(t, "stage.yaml"))
	for _, tc := range []struct {
		name, old, new, want string
	}{
		{"unknown field", "seed: 7\n", "seed: 7\nlevel: 2\n", "field level not found"},
		{"other version", "version: 1\n", "version: 2\n", "version 2"},
		{"trailing document", "", "---\nversion: 1\n", "trailing"},
		{"no scroll", "scrollSpeed: 60\n", "scrollSpeed: 0\n", "scrollSpeed"},
		{"no lives", "  lives: 3\n", "  lives: 0\n", "lives"},
		{"no weapons", "  weapons:\n    - shot-1\n    - shot-2\n    - shot-3\n    - shot-4\n", "  weapons: []\n", "no weapons"},
		{"unknown path", "path: dive", "path: loop", `path "loop"`},
		{"weave without period", "    period: 2.4s\n", "", "needs a period"},
		{"twice-defined enemy", "  - name: weaver\n", "  - name: dart\n", "defined twice"},
		{"unknown enemy", "    enemy: gunship\n    count: 1\n", "    enemy: carrier\n    count: 1\n", `no enemy "carrier"`},
		{"waves out of order", "  - at: 5s\n", "  - at: 1s\n", "before wave 0"},
		{"unknown drop", "drop: life", "drop: shield", `drop "shield"`},
		{"boss before the last wave", "  at: 37s\n", "  at: 30s\n", "not after the last wave"},
		{"first phase not full", "    - below: 1\n", "    - below: 0.9\n", "want 1"},
		{"phases not falling", "    - below: 0.33\n", "    - below: 0.7\n", "not under phase 1"},
		{"unknown move", "move: chase", "move: orbit", `move "orbit"`},
		{"too many in a wave", "    count: 5\n", "    count: 1001\n", "count 1001"},
		{"wave past the end of time", "    every: 250ms\n", "    every: 1000000h\n", "later than a time can hold"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := valid + tc.new
			if tc.old != "" {
				if !strings.Contains(valid, tc.old) {
					t.Fatalf("data/stage.yaml has no %q to replace", tc.old)
				}
				src = strings.Replace(valid, tc.old, tc.new, 1)
			}
			_, err := parseStage([]byte(src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseStage = %v, want an error naming %q", err, tc.want)
			}
		})
	}
	if _, err := parseStage([]byte(" \n")); err == nil {
		t.Error("parseStage accepted an empty file")
	}
}

// FuzzParseStage checks that every stage parseStage accepts encodes, and that its encoding parses back to the same encoding, so a round trip through a tool never changes a stage.
func FuzzParseStage(f *testing.F) {
	f.Add(readData(f, "stage.yaml"))
	f.Add([]byte("version: 1\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := parseStage(data)
		if err != nil {
			return
		}
		first, err := encodeStage(s)
		if err != nil {
			t.Fatalf("a parsed stage does not encode: %v", err)
		}
		again, err := parseStage(first)
		if err != nil {
			t.Fatalf("an encoded stage does not parse: %v\n%s", err, first)
		}
		second, err := encodeStage(again)
		if err != nil || !bytes.Equal(first, second) {
			t.Fatalf("a stage changed through a round trip (%v):\n%s\n---\n%s", err, first, second)
		}
	})
}

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/cards"
)

// TestDataFilesAreCanonical checks that rules.yaml and board.yaml are in the form their encoders write, so a load and a save of either changes nothing.
func TestDataFilesAreCanonical(t *testing.T) {
	rules, err := parseRules(rulesYAML)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := encodeRules(rules); err != nil || !bytes.Equal(out, rulesYAML) {
		t.Fatalf("rules.yaml is not canonical (%v); write it as:\n%s", err, out)
	}
	doc, err := cards.ParseBoardDoc(boardYAML)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := cards.EncodeBoardDoc(doc); err != nil || !bytes.Equal(out, boardYAML) {
		t.Fatalf("board.yaml is not canonical (%v); write it as:\n%s", err, out)
	}
	if _, err := newTable(rules, doc); err != nil {
		t.Fatal(err)
	}
}

func TestParseRulesRefusesBadFiles(t *testing.T) {
	for name, src := range map[string]string{
		"empty":            "",
		"unknown field":    strings.Replace(string(rulesYAML), "ranks: 9", "ranks: 9\njokers: 2", 1),
		"newer version":    strings.Replace(string(rulesYAML), "version: 1", "version: 2", 1),
		"no ranks":         strings.Replace(string(rulesYAML), "ranks: 9", "ranks: 0", 1),
		"too many ranks":   strings.Replace(string(rulesYAML), "ranks: 9", "ranks: 14", 1),
		"negative redeals": strings.Replace(string(rulesYAML), "redeals: 2", "redeals: -1", 1),
		"repeated suit":    strings.Replace(string(rulesYAML), "name: tide", "name: flame", 1),
		"bad suit name":    strings.Replace(string(rulesYAML), "name: tide", "name: Tide", 1),
		"bad color":        strings.Replace(string(rulesYAML), "'#2e86de'", "blue", 1),
		"short color":      strings.Replace(string(rulesYAML), "'#2e86de'", "'#28d'", 1),
		"no suits":         "version: 1\nranks: 9\nredeals: 2\n",
		"too many suits":   string(rulesYAML) + "  - name: wind\n    color: '#ffffff'\n",
		"second document":  string(rulesYAML) + "---\nversion: 1\n",
	} {
		if _, err := parseRules([]byte(src)); err == nil {
			t.Errorf("%s: parseRules accepted it", name)
		}
	}
}

// FuzzParseRules checks that whatever parseRules accepts encodes, and reads back the same.
func FuzzParseRules(f *testing.F) {
	f.Add(rulesYAML)
	f.Add([]byte("version: 1\nranks: 1\nredeals: 0\nsuits:\n  - name: a\n    color: '#000000'\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := parseRules(data)
		if err != nil {
			return
		}
		out, err := encodeRules(r)
		if err != nil {
			t.Fatalf("encodeRules refused rules parseRules accepted: %v", err)
		}
		again, err := parseRules(out)
		if err != nil {
			t.Fatalf("parseRules refused what encodeRules wrote: %v\n%s", err, out)
		}
		if out2, _ := encodeRules(again); !bytes.Equal(out, out2) {
			t.Fatalf("rules changed on a second round trip:\n%s\n%s", out, out2)
		}
	})
}
